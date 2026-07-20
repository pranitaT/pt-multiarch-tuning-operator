/*
Copyright 2026 Red Hat, Inc.

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
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
)

// applyCELArchitecturePlacement evaluates and applies celArchitecturePlacement plugin rules
// Returns true if the plugin was applied, false otherwise
func (r *PodReconciler) applyCELArchitecturePlacement(ctx context.Context, ppc multiarchv1beta1.PodPlacementConfig, pod *Pod) bool {
	log := ctrllog.FromContext(ctx).WithName("celArchitecturePlacement")

	// TODO(debug): remove after issue resolved
	celStart := time.Now()
	p := pod.PodObject()
	preNodeSelector := p.Spec.NodeSelector
	var preRequiredTerms interface{}
	if p.Spec.Affinity != nil && p.Spec.Affinity.NodeAffinity != nil &&
		p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		preRequiredTerms = p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	}
	log.Info("[CEL] applyCELArchitecturePlacement entry",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"uid", p.UID,
		"ppcName", ppc.Name,
		"ppcPriority", ppc.Spec.Priority,
		"preNodeSelector", preNodeSelector,
		"preRequiredNodeAffinityTerms", preRequiredTerms,
		"schedulingGates", p.Spec.SchedulingGates,
	)

	// Check if plugin is enabled
	if !ppc.PluginsEnabled(common.CelArchitecturePlacementPluginName) {
		log.Info("CEL plugin not enabled", "PodPlacementConfig", ppc.Name, "pod", pod.Name)
		// TODO(debug): remove after issue resolved
		log.Info("[CEL] early return — plugin not enabled",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			"uid", p.UID,
			"ppcName", ppc.Name,
		)
		return false
	}

	log.Info("CEL plugin enabled", "PodPlacementConfig", ppc.Name, "pod", pod.Name)

	// Access plugin directly, following existing pattern for NodeAffinityScoring
	celPlugin := ppc.Spec.Plugins.CelArchitecturePlacement
	if celPlugin == nil {
		log.Info("celArchitecturePlacement plugin is nil", "PodPlacementConfig", ppc.Name, "pod", pod.Name)
		// TODO(debug): remove after issue resolved
		log.Info("[CEL] early return — celPlugin is nil (plugin enabled but config is nil)",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			"uid", p.UID,
			"ppcName", ppc.Name,
		)
		return false
	}

	// TODO(debug): remove after issue resolved
	log.Info("[CEL] plugin config",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"uid", p.UID,
		"ppcName", ppc.Name,
		"ruleCount", len(celPlugin.Rules),
		"fallbackArchitectures", celPlugin.FallbackArchitectures,
	)

	log.Info("Evaluating CEL rules", "PodPlacementConfig", ppc.Name, "pod", pod.Name, "ruleCount", len(celPlugin.Rules))

	// Evaluate CEL rules
	result, err := evaluateCELArchitecturePlacement(celPlugin.Rules, celPlugin.FallbackArchitectures, pod.PodObject())
	if err != nil {
		log.Error(err, "Failed to evaluate CEL rules", "PodPlacementConfig", ppc.Name, "pod", pod.Name)
		pod.PublishEvent(corev1.EventTypeWarning, "CELEvaluationError", fmt.Sprintf("Failed to evaluate CEL rules: %v", err))
		// TODO(debug): remove after issue resolved
		log.Error(err, "[CEL] early return — CEL evaluation failed",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			"uid", p.UID,
			"ppcName", ppc.Name,
		)
		return false
	}

	// Apply the architecture constraints
	if result.matched {
		log.Info("CEL rule matched",
			"PodPlacementConfig", ppc.Name,
			"pod", pod.Name,
			"ruleName", result.ruleName,
			"architectures", result.architectures)
	} else {
		log.Info("No CEL rules matched - using fallback",
			"PodPlacementConfig", ppc.Name,
			"pod", pod.Name,
			"fallbackArchitectures", result.architectures)
	}

	// TODO(debug): remove after issue resolved
	log.Info("[CEL][APPLY] about to call applyArchitectureConstraints",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"uid", p.UID,
		"ppcName", ppc.Name,
		"architectures", result.architectures,
		"matched", result.matched,
		"ruleName", result.ruleName,
		"architecturesEmpty", len(result.architectures) == 0,
	)

	log.Info("Applying architecture constraints from CEL",
		"pod", pod.Name,
		"architectures", result.architectures)

	// Remove existing architecture constraints and apply new ones
	applyArchitectureConstraints(pod.PodObject(), result.architectures)

	// TODO(debug): remove after issue resolved
	p = pod.PodObject()
	var postRequiredTerms interface{}
	if p.Spec.Affinity != nil && p.Spec.Affinity.NodeAffinity != nil &&
		p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		postRequiredTerms = p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	}
	log.Info("[CEL][APPLY] post-applyArchitectureConstraints state",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"uid", p.UID,
		"ppcName", ppc.Name,
		"postNodeSelector", p.Spec.NodeSelector,
		"postRequiredNodeAffinityTerms", postRequiredTerms,
		"architecturesApplied", result.architectures,
	)

	// Publish event
	configSource := fmt.Sprintf("%s-%s", multiarchv1beta1.PodPlacementConfigKind, ppc.Name)
	if result.matched {
		pod.PublishEvent(corev1.EventTypeNormal, "CELArchitecturePlacementApplied",
			fmt.Sprintf("Applied CEL rule '%s' from %s, architectures: %v", result.ruleName, configSource, result.architectures))
	} else {
		pod.PublishEvent(corev1.EventTypeNormal, "CELArchitecturePlacementFallback",
			fmt.Sprintf("No CEL rules matched, using fallback architectures from %s: %v", configSource, result.architectures))
	}

	log.Info("CEL architecture placement applied successfully",
		"pod", pod.Name,
		"PodPlacementConfig", ppc.Name)

	// TODO(debug): remove after issue resolved
	log.Info("[CEL][SUMMARY] applyCELArchitecturePlacement exit",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"uid", p.UID,
		"ppcName", ppc.Name,
		"pluginEnabled", true,
		"ruleMatched", result.matched,
		"ruleName", result.ruleName,
		"fallbackUsed", !result.matched,
		"architectures", result.architectures,
		"applied", true,
		"duration", time.Since(celStart).String(),
	)

	return true
}
