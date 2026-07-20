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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// removeArchitectureFromNodeSelector removes the kubernetes.io/arch key from the pod's nodeSelector
// Returns true if the key was present and removed, false otherwise
func removeArchitectureFromNodeSelector(pod *corev1.Pod) bool {
	// TODO(debug): remove after issue resolved
	archDebugLog.Info("[REMOVE] removeArchitectureFromNodeSelector entry",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"nodeSelector", pod.Spec.NodeSelector,
	)

	if pod.Spec.NodeSelector == nil {
		// TODO(debug): remove after issue resolved
		archDebugLog.Info("[REMOVE] removeArchitectureFromNodeSelector — nodeSelector is nil, nothing to remove",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		)
		return false
	}

	if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
		delete(pod.Spec.NodeSelector, utils.ArchLabel)
		// TODO(debug): remove after issue resolved
		archDebugLog.Info("[REMOVE] removeArchitectureFromNodeSelector — arch label removed from nodeSelector",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			"removedKey", utils.ArchLabel,
			"postNodeSelector", pod.Spec.NodeSelector,
		)
		return true
	}

	// TODO(debug): remove after issue resolved
	archDebugLog.Info("[REMOVE] removeArchitectureFromNodeSelector — arch label not present in nodeSelector, nothing removed",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
	)
	return false
}

// removeArchitectureFromNodeAffinity removes architecture-based match expressions from
// requiredDuringSchedulingIgnoredDuringExecution node affinity.
// It removes matchExpressions with key kubernetes.io/arch from all node selector terms.
// Empty node selector terms are removed after cleanup.
// preferredDuringSchedulingIgnoredDuringExecution is preserved as per enhancement doc.
// Returns true if any architecture constraints were removed, false otherwise
func removeArchitectureFromNodeAffinity(pod *corev1.Pod) bool {
	// TODO(debug): remove after issue resolved
	var preTerms interface{}
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		preTerms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	}
	archDebugLog.Info("[REMOVE] removeArchitectureFromNodeAffinity entry",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"preRequiredTerms", preTerms,
	)

	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		// TODO(debug): remove after issue resolved
		archDebugLog.Info("[REMOVE] removeArchitectureFromNodeAffinity — affinity or nodeAffinity is nil, nothing to remove",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		)
		return false
	}

	nodeAffinity := pod.Spec.Affinity.NodeAffinity
	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		// TODO(debug): remove after issue resolved
		archDebugLog.Info("[REMOVE] removeArchitectureFromNodeAffinity — RequiredDuringSchedulingIgnoredDuringExecution is nil, nothing to remove",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		)
		return false
	}

	removed := false
	var cleanedTerms []corev1.NodeSelectorTerm

	// Iterate through all node selector terms
	for _, term := range nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		var cleanedExpressions []corev1.NodeSelectorRequirement

		// Filter out architecture match expressions
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				removed = true
				// Skip this expression (remove it)
				continue
			}
			cleanedExpressions = append(cleanedExpressions, expr)
		}

		// Only keep the term if it has remaining match expressions or match fields
		if len(cleanedExpressions) > 0 || len(term.MatchFields) > 0 {
			term.MatchExpressions = cleanedExpressions
			cleanedTerms = append(cleanedTerms, term)
		} else {
			// Empty term after removing architecture expressions
			removed = true
		}
	}

	// Update the node selector terms
	nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = cleanedTerms

	// Clean up empty structures
	if len(cleanedTerms) == 0 {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nil
	}

	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil &&
		nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		pod.Spec.Affinity.NodeAffinity = nil
	}

	if pod.Spec.Affinity.NodeAffinity == nil &&
		pod.Spec.Affinity.PodAffinity == nil &&
		pod.Spec.Affinity.PodAntiAffinity == nil {
		pod.Spec.Affinity = nil
	}

	// TODO(debug): remove after issue resolved
	var postTerms interface{}
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		postTerms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	}
	archDebugLog.Info("[REMOVE] removeArchitectureFromNodeAffinity exit",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"removed", removed,
		"postRequiredTerms", postTerms,
		"affinityNilAfterCleanup", pod.Spec.Affinity == nil,
	)

	return removed
}

// removeAllArchitectureConstraints removes all existing architecture constraints from a pod
// This includes:
// - kubernetes.io/arch from nodeSelector
// - kubernetes.io/arch matchExpressions from requiredDuringSchedulingIgnoredDuringExecution
// Returns true if any constraints were removed, false otherwise
func removeAllArchitectureConstraints(pod *corev1.Pod) bool {
	// TODO(debug): remove after issue resolved
	archDebugLog.Info("[REMOVE] removeAllArchitectureConstraints entry",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"nodeSelector", pod.Spec.NodeSelector,
	)

	removedFromNodeSelector := removeArchitectureFromNodeSelector(pod)
	removedFromNodeAffinity := removeArchitectureFromNodeAffinity(pod)

	result := removedFromNodeSelector || removedFromNodeAffinity
	// TODO(debug): remove after issue resolved
	archDebugLog.Info("[REMOVE] removeAllArchitectureConstraints exit",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"removedFromNodeSelector", removedFromNodeSelector,
		"removedFromNodeAffinity", removedFromNodeAffinity,
		"anyRemoved", result,
	)
	return result
}
