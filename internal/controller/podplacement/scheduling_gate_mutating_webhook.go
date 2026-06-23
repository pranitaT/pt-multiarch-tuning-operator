/*
Copyright 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podplacement

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/panjf2000/ants/v2"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/featuregates"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// [disabled:operator]kubebuilder:webhook:path=/add-pod-scheduling-gate,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=ignore,groups="",resources=pods,verbs=create,versions=v1,name=pod-placement-scheduling-gate.multiarch.openshift.io

// PodSchedulingGateMutatingWebHook annotates Pods
type PodSchedulingGateMutatingWebHook struct {
	client     client.Client
	clientSet  *kubernetes.Clientset
	decoder    admission.Decoder
	once       sync.Once
	scheme     *runtime.Scheme
	recorder   record.EventRecorder
	workerPool *ants.MultiPool
}

// withPanicRecovery wraps a function with panic recovery to prevent webhook crashes.
// This is critical for webhook stability since panics in admission webhooks can block
// pod creation cluster-wide. If a panic occurs, the function returns false and increments
// the fallback metric, allowing the pod to proceed with a scheduling gate for reconciler processing.
// Returns true if the function executed successfully, false if it panicked.
func withPanicRecovery(ctx context.Context, podName, podNamespace string, fn func() bool) (applied bool) {
	defer func() {
		if r := recover(); r != nil {
			log := ctrllog.FromContext(ctx)
			log.Error(nil, "Recovered from panic during CEL evaluation",
				"panic", r,
				"pod", podName,
				"namespace", podNamespace,
				"stack", string(debug.Stack()))
			metrics.CELFallbackToGateWH.Inc()
			applied = false
		}
	}()

	applied = fn()
	return
}

// applyCELArchitecturePlacementInWebhook evaluates CEL rules and applies architecture constraints in the webhook.
// CEL evaluation must occur in the webhook (before pod persistence) rather than the reconciler (after persistence)
// because Kubernetes API rejects modifications to spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms
// after pod creation with error: "only additions are allowed (no mutations or deletions)".
// By evaluating CEL rules here, we can set the correct architecture constraints before the pod is persisted,
// avoiding the immutability constraint violation.
// Returns true if CEL was successfully applied, false otherwise.
func (a *PodSchedulingGateMutatingWebHook) applyCELArchitecturePlacementInWebhook(
	ctx context.Context,
	sortedPPCs []multiarchv1beta1.PodPlacementConfig,
	pod *Pod,
) bool {
	log := ctrllog.FromContext(ctx).WithName("celArchitecturePlacement")

	// Check context before starting
	select {
	case <-ctx.Done():
		log.V(1).Info("CEL evaluation aborted due to context cancellation",
			"pod", pod.Name,
			"namespace", pod.Namespace)
		metrics.CELContextTimeoutWH.Inc()
		return false
	default:
	}

	// Find first PPC with CEL plugin (already sorted by priority)
	for _, ppc := range sortedPPCs {
		// Check context inside loop
		select {
		case <-ctx.Done():
			log.V(1).Info("CEL evaluation aborted due to context cancellation during PPC iteration",
				"pod", pod.Name,
				"namespace", pod.Namespace)
			metrics.CELContextTimeoutWH.Inc()
			return false
		default:
		}

		if !ppc.PluginsEnabled(common.CelArchitecturePlacementPluginName) {
			continue
		}

		celPlugin := ppc.Spec.Plugins.CelArchitecturePlacement
		if celPlugin == nil {
			continue
		}

		log.Info("Evaluating CEL rules in webhook",
			"PodPlacementConfig", ppc.Name,
			"pod", pod.Name)

		// Reuse existing CEL evaluator to maintain consistency with reconciler behavior.
		// This evaluator handles rule matching, fallback architectures, and error cases.
		result, err := evaluateCELArchitecturePlacement(
			celPlugin.Rules,
			celPlugin.FallbackArchitectures,
			pod.PodObject(),
		)

		if err != nil {
			log.Error(err, "CEL evaluation failed in webhook",
				"PodPlacementConfig", ppc.Name,
				"pod", pod.Name)
			metrics.CELEvaluationErrorsWH.Inc()
			continue
		}

		log.Info("Applying CEL architecture constraints in webhook",
			"pod", pod.Name,
			"architectures", result.architectures,
			"matched", result.matched)

		// Apply architecture constraints before pod persistence to avoid Kubernetes immutability violations.
		// This function modifies spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution,
		// which cannot be changed after pod creation.
		applyArchitectureConstraints(pod.PodObject(), result.architectures)

		metrics.CELProcessedPodsWH.Inc()
		return true
	}

	// No CEL plugin found or enabled
	metrics.CELSkippedPodsWH.Inc()
	return false
}

// applyNodeAffinityScoringInWebhook applies NodeAffinityScoring (preferred affinity) from matching PPCs and CPPC.
// Unlike required affinity (CEL), preferred affinity can be safely modified after pod creation, but applying it
// in the webhook ensures consistency with CEL processing and reduces reconciler workload.
// This function is called after CEL evaluation to allow both plugins to coexist per the enhancement document.
// Returns true if NodeAffinityScoring was applied successfully (or skipped), false if panic occurred.
func (a *PodSchedulingGateMutatingWebHook) applyNodeAffinityScoringInWebhook(ctx context.Context, pod *Pod, matchingPPCs []multiarchv1beta1.PodPlacementConfig, cppc *multiarchv1beta1.ClusterPodPlacementConfig) bool {
	return withPanicRecovery(ctx, pod.Namespace, pod.Name, func() bool {
		log := ctrllog.FromContext(ctx)

		// Respect user-defined preferred affinity to avoid conflicts.
		// If the user has already configured architecture-related preferred affinity,
		// we skip operator-managed NodeAffinityScoring to preserve user intent.
		if pod.isPreferredAffinityConfiguredForArchitecture() {
			log.Info("NodeAffinityScoring skipped - user-defined preferred affinity exists",
				"pod", pod.Name,
				"namespace", pod.Namespace)
			return true
		}

		// Sort PPCs by descending priority to ensure higher-priority configs are applied first.
		// This maintains consistency with reconciler behavior.
		sort.Slice(matchingPPCs, func(i, j int) bool {
			return matchingPPCs[i].Spec.Priority > matchingPPCs[j].Spec.Priority
		})

		// Apply NodeAffinityScoring from namespace-scoped PPCs.
		// Each PPC can contribute preferred affinity terms for different architectures.
		for _, ppc := range matchingPPCs {
			if !ppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
				log.V(1).Info("Skipping PPC - NodeAffinityScoring disabled",
					"ppc", ppc.Name,
					"namespace", ppc.Namespace)
				continue
			}

			log.Info("Applying namespace-scoped NodeAffinityScoring",
				"ppc", ppc.Name,
				"pod", pod.Name)
			configSource := fmt.Sprintf("%s-%s", multiarchv1beta1.PodPlacementConfigKind, ppc.Name)
			pod.SetPreferredArchNodeAffinity(ppc.Spec.Plugins.NodeAffinityScoring, configSource)
		}

		// Apply NodeAffinityScoring from cluster-scoped CPPC if enabled.
		// CPPC provides cluster-wide defaults that apply when no namespace-scoped config matches.
		if cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
			log.Info("Applying cluster-scoped NodeAffinityScoring",
				"cppc", cppc.Name,
				"pod", pod.Name)
			pod.SetPreferredArchNodeAffinity(cppc.Spec.Plugins.NodeAffinityScoring, multiarchv1beta1.ClusterPodPlacementConfigKind)
		}

		return true
	})
}

func (a *PodSchedulingGateMutatingWebHook) patchedPodResponse(pod *corev1.Pod, req admission.Request) admission.Response {
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *PodSchedulingGateMutatingWebHook) Handle(ctx context.Context, req admission.Request) admission.Response {
	responseTimeStart := time.Now()
	defer utils.HistogramObserve(responseTimeStart, metrics.ResponseTime)
	metrics.ProcessedPodsWH.Inc()

	// Generate TraceID for this webhook invocation
	traceID := uuid.NewString()

	a.once.Do(func() {
		a.decoder = admission.NewDecoder(a.scheme)
	})
	pod := newPod(&corev1.Pod{}, ctx, a.recorder)

	err := a.decoder.Decode(req, &pod.Pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	log := ctrllog.FromContext(ctx).WithValues("traceID", traceID, "namespace", pod.Namespace, "name", pod.Name)

	log.Info("[WEBHOOK] ENTER",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"traceID", traceID,
		"existingSchedulingGates", pod.Spec.SchedulingGates,
		"labels", pod.Labels,
		"ownerReferences", pod.OwnerReferences)

	cppc := clusterpodplacementconfig.GetClusterPodPlacementConfig()

	// List existing PodPlacementConfigs in the same namespace
	ppcList := &multiarchv1beta1.PodPlacementConfigList{}
	if err := a.client.List(ctx, ppcList, client.InNamespace(pod.Namespace)); err != nil {
		log.Error(err, "Failed to list existing PodPlacementConfigs in namespace")
		// On error, proceed without PPC filtering - fail open
		ppcList.Items = []multiarchv1beta1.PodPlacementConfig{}
	}

	// Filter to only PPCs that match this pod's labels - do this once for efficiency
	matchingPPCs := pod.filterMatchingPPCs(ppcList)

	// Set label to indicate if preferred affinity will be set by CPPC or any matching PPC
	if (cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName)) ||
		pod.hasMatchingPPCWithPlugin(matchingPPCs) {
		pod.EnsureLabel(utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet)
	}
	pod.EnsureLabel(utils.NodeAffinityLabel, utils.LabelValueNotSet)
	pod.EnsureLabel(utils.SchedulingGateLabel, utils.LabelValueNotSet)

	if pod.shouldIgnorePod(cppc, matchingPPCs) {
		log.Info("[WEBHOOK] RETURN - skipping pod",
			"reason", "does not match criteria for processing",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID)
		return a.patchedPodResponse(pod.PodObject(), req)
	}

	// Check if scheduling gate already exists
	if pod.HasSchedulingGate() {
		log.Info("[WEBHOOK] RETURN - gate already exists",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID,
			"existingGates", pod.Spec.SchedulingGates)
		return a.patchedPodResponse(pod.PodObject(), req)
	}

	// Phase 1: Try CEL evaluation in webhook with panic recovery and latency tracking
	// Clone and sort PPCs by priority
	sortedPPCs := slices.Clone(matchingPPCs)
	slices.SortFunc(sortedPPCs, func(a, b multiarchv1beta1.PodPlacementConfig) int {
		// Sort in descending order (higher priority first)
		if a.Spec.Priority > b.Spec.Priority {
			return -1
		}
		if a.Spec.Priority < b.Spec.Priority {
			return 1
		}
		return 0
	})

	// Try CEL evaluation with latency tracking
	timer := prometheus.NewTimer(metrics.CELWebhookDurationSeconds)
	celApplied := withPanicRecovery(ctx, pod.Name, pod.Namespace, func() bool {
		return a.applyCELArchitecturePlacementInWebhook(ctx, sortedPPCs, pod)
	})
	timer.ObserveDuration()

	if celApplied {
		log.Info("CEL applied in webhook",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID)
		// Label CEL-processed pods for identification by the reconciler fast-path.
		// This label allows the reconciler to skip CEL evaluation and only remove the scheduling gate,
		// significantly reducing reconciler processing time for CEL pods.
		pod.EnsureLabel(utils.CELProcessedLabel, utils.CELProcessedLabelValue)
		metrics.CELImmediatePodsWH.Inc()
		log.Info("CEL label added",
			"pod", pod.Name,
			"label", utils.CELProcessedLabel,
			"value", utils.CELProcessedLabelValue)
	}

	// Apply NodeAffinityScoring (preferred affinity) in webhook.
	// This runs regardless of whether CEL was applied, allowing both required (CEL) and
	// preferred (NodeAffinityScoring) affinity to coexist per the enhancement document.
	scoringApplied := a.applyNodeAffinityScoringInWebhook(ctx, pod, matchingPPCs, cppc)
	if scoringApplied {
		log.Info("NodeAffinityScoring applied in webhook",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID)
	}

	// Skip scheduling gate for CEL pods if feature gate is enabled.
	// When CEL_WEBHOOK_NO_GATE=true, CEL-processed pods can be scheduled immediately
	// since all architecture constraints have been applied in the webhook.
	// This reduces latency by eliminating the reconciler round-trip for CEL pods.
	if celApplied && featuregates.Enabled(featuregates.CELWebhookNoGate) {
		log.Info("CEL processed, skipping gate (feature gate enabled)",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID,
			"featureGate", featuregates.CELWebhookNoGate)
		// No scheduling gate needed - pod can be scheduled immediately
		return a.patchedPodResponse(pod.PodObject(), req)
	}

	// Add scheduling gate for non-CEL pods OR when feature gate is disabled.
	// The scheduling gate prevents pod scheduling until the reconciler completes processing.
	// For CEL pods with feature gate disabled, this provides backward compatibility and
	// allows gradual rollout of the immediate scheduling behavior.
	if celApplied {
		log.Info("Adding scheduling gate for CEL pod (feature gate disabled)",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID,
			"gate", utils.SchedulingGateName,
			"featureGate", featuregates.CELWebhookNoGate)
		metrics.CELFallbackToGateWH.Inc()
	} else {
		log.Info("Adding scheduling gate for non-CEL pod",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"traceID", traceID,
			"gate", utils.SchedulingGateName)
	}
	// Add scheduling gate to prevent pod scheduling until reconciler processing completes.
	// For non-CEL pods, the reconciler performs image-based architecture detection.
	// For CEL pods (when feature gate is disabled), the reconciler removes the gate after verification.
	pod.ensureSchedulingGate()
	// We also add a label to the pod to indicate that the scheduling gate was added
	// and this pod expects processing by the operator. That's useful for testing and debugging, but also gives the user
	// an indication that the pod is waiting for processing and can support kubectl queries to find out which pods are
	// waiting for processing, for example when the operator is being uninstalled.
	pod.Labels[utils.SchedulingGateLabel] = utils.SchedulingGateLabelValueGated
	// we don't care about this goroutine, it's informational,
	// we know it will finish eventually by design, and we don't need to block the response as we
	// are right in the admission pipeline, before the pod is persisted.
	log.Info("[WEBHOOK] EXIT - gate added successfully",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"traceID", traceID,
		"resultingSchedulingGates", pod.Spec.SchedulingGates)
	a.delayedSchedulingGatedEvent(ctx, pod.DeepCopy())
	metrics.GatedPods.Inc()
	metrics.GatedPodsGauge.Inc()
	return a.patchedPodResponse(pod.PodObject(), req)
}

func (a *PodSchedulingGateMutatingWebHook) delayedSchedulingGatedEvent(ctx context.Context, pod *corev1.Pod) {
	err := a.workerPool.Submit(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		log := ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name,
			"function", "delayedSchedulingGatedEvent")
		// We try to get the pod from the API with exponential backoff until we find it or a timeout is reached
		err := wait.ExponentialBackoff(wait.Backoff{
			// The maximum time, excluding the time for the execution of the request,
			// is the sum of a geometric series with factor != 1.
			// maxTime = duration * (factor^steps - 1) / (factor - 1)
			// maxTime = 2e-3s * (2^15 - 1) = 65.534s
			Duration: 2 * time.Millisecond,
			Factor:   2,
			Steps:    15,
		}, func() (bool, error) {
			createdPod, err := a.clientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err == nil {
				log.V(2).Info("Pod was found", "namespace", pod.Namespace, "name", pod.Name)
				a.recorder.Event(createdPod, corev1.EventTypeNormal, ArchitectureAwareSchedulingGateAdded, SchedulingGateAddedMsg)
				// Pod was found, return true to stop retrying
				return true, nil
			}
			if apierrors.IsNotFound(err) {
				log.V(3).Info("Pod not found yet", "namespace", pod.Namespace, "name", pod.Name)
				// Pod not found yet, continue retrying
				return false, nil
			}
			// Stop retrying
			log.V(3).Info("Failed to get pod", "error", err)
			return false, err
		})
		if err != nil {
			log.V(2).Info("Failed to get a scheduling gated Pod after retries",
				"error", err)
		}
	})
	if err != nil {
		ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name,
			"function", "delayedSchedulingGatedEvent").Error(err, "Failed to submit the delayedSchedulingGatedEvent job")
	}
}

func NewPodSchedulingGateMutatingWebHook(client client.Client, clientSet *kubernetes.Clientset,
	scheme *runtime.Scheme, recorder record.EventRecorder, workerPool *ants.MultiPool) *PodSchedulingGateMutatingWebHook {
	a := &PodSchedulingGateMutatingWebHook{
		client:     client,
		clientSet:  clientSet,
		scheme:     scheme,
		recorder:   recorder,
		workerPool: workerPool,
	}
	metrics.InitWebhookMetrics()
	return a
}
