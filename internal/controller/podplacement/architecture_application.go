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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// applyArchitectureNodeAffinity applies new architecture constraints to a pod's node affinity
// by updating in-place the requiredDuringSchedulingIgnoredDuringExecution matchExpressions for
// kubernetes.io/arch using the In operator.
// It initializes the affinity structure if needed and merges the architecture constraint
// into existing terms to avoid Kubernetes rejecting updates due to NodeSelectorTerms modifications.
// The architectures list must not be empty.
//
// This function updates architecture constraints in-place by:
// 1. Removing any existing kubernetes.io/arch matchExpressions from all terms
// 2. Adding the new architecture requirement to each term (or creating a new term if none exist)
// This approach avoids the Kubernetes API rejection: "no additions/deletions to non-empty NodeSelectorTerms list are allowed"
func applyArchitectureNodeAffinity(pod *corev1.Pod, architectures []string) {
	log := ctrllog.Log.WithName("podplacement")

	if len(architectures) == 0 {
		log.Info("applyArchitectureNodeAffinity: No architectures provided", "pod", pod.Name, "namespace", pod.Namespace)
		return
	}

	log.Info("applyArchitectureNodeAffinity: Starting", "pod", pod.Name, "namespace", pod.Namespace, "architectures", architectures)

	// Initialize affinity structure if needed
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}

	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}

	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
	}

	// Create the architecture requirement
	architectureRequirement := corev1.NodeSelectorRequirement{
		Key:      utils.ArchLabel,
		Operator: corev1.NodeSelectorOpIn,
		Values:   architectures,
	}

	existingTerms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	log.Info("applyArchitectureNodeAffinity: Before modification",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"existingTermCount", len(existingTerms))

	// If no terms exist, create a single architecture-only term
	if len(existingTerms) == 0 {
		log.Info("applyArchitectureNodeAffinity: No existing terms - creating new term", "pod", pod.Name)
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{architectureRequirement},
			},
		}
		log.Info("applyArchitectureNodeAffinity: Created new term", "pod", pod.Name, "architectures", architectures)
		return
	}

	// Update in-place: remove old arch constraints and add new ones to each term
	// This preserves the NodeSelectorTerms array structure to avoid Kubernetes API rejection
	for i := range existingTerms {
		originalExprCount := len(existingTerms[i].MatchExpressions)

		// Remove any existing architecture matchExpressions from this term
		var cleanedExpressions []corev1.NodeSelectorRequirement
		for _, expr := range existingTerms[i].MatchExpressions {
			if expr.Key != utils.ArchLabel {
				cleanedExpressions = append(cleanedExpressions, expr)
			}
		}

		log.Info("applyArchitectureNodeAffinity: Processing term",
			"pod", pod.Name,
			"termIndex", i,
			"originalMatchExpressions", originalExprCount,
			"afterArchRemoval", len(cleanedExpressions))

		// Add the new architecture requirement
		cleanedExpressions = append(cleanedExpressions, architectureRequirement)
		existingTerms[i].MatchExpressions = cleanedExpressions

		log.Info("applyArchitectureNodeAffinity: Updated term",
			"pod", pod.Name,
			"termIndex", i,
			"finalMatchExpressions", len(cleanedExpressions))
	}

	// Write modified terms back to pod
	// This is required because while slice element modifications persist,
	// we need to ensure the slice header is synchronized
	pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = existingTerms

	log.Info("applyArchitectureNodeAffinity: Completed",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"finalTermCount", len(existingTerms),
		"affinity", pod.Spec.Affinity)
}

// applyArchitectureConstraints applies architecture constraints to a pod by updating
// the node affinity in-place. It also removes architecture from nodeSelector if present.
// This ensures the plugin's architecture selection takes full effect while avoiding
// Kubernetes API rejections for NodeSelectorTerms modifications.
// Returns true if any changes were made to the pod.
func applyArchitectureConstraints(pod *corev1.Pod, architectures []string) bool {
	log := ctrllog.Log.WithName("podplacement")

	if len(architectures) == 0 {
		log.Info("applyArchitectureConstraints: No architectures to apply", "pod", pod.Name)
		return false
	}

	log.Info("applyArchitectureConstraints: Starting",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"architectures", architectures)

	// DIAGNOSTIC: Dump state BEFORE modification
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		beforeJSON, _ := json.Marshal(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
		log.Info("[DIAGNOSTIC] BEFORE applyArchitectureNodeAffinity",
			"pod", pod.Name,
			"terms", string(beforeJSON))
	}

	// Log current state
	if pod.Spec.NodeSelector != nil {
		log.Info("applyArchitectureConstraints: Current nodeSelector", "pod", pod.Name, "nodeSelector", pod.Spec.NodeSelector)
	}
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		log.Info("applyArchitectureConstraints: Current node affinity exists", "pod", pod.Name)
	}

	// Remove architecture from nodeSelector (this is safe and doesn't cause API rejections)
	removedFromNodeSelector := removeArchitectureFromNodeSelector(pod)
	if removedFromNodeSelector {
		log.Info("applyArchitectureConstraints: Removed architecture from nodeSelector", "pod", pod.Name)
	}

	// Update architecture constraints in-place within node affinity
	applyArchitectureNodeAffinity(pod, architectures)

	// DIAGNOSTIC: Dump state AFTER modification
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		afterJSON, _ := json.Marshal(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
		log.Info("[DIAGNOSTIC] AFTER applyArchitectureNodeAffinity",
			"pod", pod.Name,
			"terms", string(afterJSON))
	}

	log.Info("applyArchitectureConstraints: Completed", "pod", pod.Name, "architectures", architectures)

	return removedFromNodeSelector || true // Always return true if we applied architectures
}
