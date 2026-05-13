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
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	. "github.com/onsi/gomega"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
)

// Integration tests for CEL Architecture Placement Plugin

func TestPod_RemoveArchitectureConstraints_Integration(t *testing.T) {
	tests := []struct {
		name                     string
		pod                      *v1.Pod
		expectArchInNodeSelector bool
		expectArchInAffinity     bool
	}{
		{
			name: "Remove architecture from nodeSelector",
			pod: NewPod().
				WithNodeSelectors(utils.ArchLabel, "amd64", "region", "us-west").
				Build(),
			expectArchInNodeSelector: false,
			expectArchInAffinity:     false,
		},
		{
			name: "Remove architecture from nodeAffinity",
			pod: NewPod().
				WithNodeSelectorTermsMatchExpressions([]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
					{
						Key:      "region",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"us-east"},
					},
				}).
				Build(),
			expectArchInNodeSelector: false,
			expectArchInAffinity:     false,
		},
		{
			name: "Remove architecture from both nodeSelector and nodeAffinity",
			pod: NewPod().
				WithNodeSelectors(utils.ArchLabel, "arm64", "zone", "a").
				WithNodeSelectorTermsMatchExpressions([]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"arm64"},
					},
				}).
				Build(),
			expectArchInNodeSelector: false,
			expectArchInAffinity:     false,
		},
		{
			name: "Pod with no architecture constraints",
			pod: NewPod().
				WithNodeSelectors("region", "us-central").
				Build(),
			expectArchInNodeSelector: false,
			expectArchInAffinity:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			pod := newPod(tt.pod, ctx, record.NewFakeRecorder(10))

			pod.RemoveArchitectureConstraints()

			// Check nodeSelector
			if pod.Spec.NodeSelector != nil {
				_, hasArch := pod.Spec.NodeSelector[utils.ArchLabel]
				g.Expect(hasArch).To(Equal(tt.expectArchInNodeSelector))
			}

			// Check nodeAffinity
			if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
				pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				hasArchInAffinity := false
				for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == utils.ArchLabel {
							hasArchInAffinity = true
							break
						}
					}
				}
				g.Expect(hasArchInAffinity).To(Equal(tt.expectArchInAffinity))
			}
		})
	}
}

func TestPod_SetCELArchitectureAffinity_Integration(t *testing.T) {
	tests := []struct {
		name           string
		pod            *v1.Pod
		architectures  []string
		ruleName       string
		wantLabel      bool
		wantAnnotation bool
	}{
		{
			name: "Set single architecture",
			pod: NewPod().
				WithName("test-pod").
				WithNamespace("default").
				Build(),
			architectures:  []string{"ppc64le"},
			ruleName:       "test-rule",
			wantLabel:      true,
			wantAnnotation: true,
		},
		{
			name: "Set multiple architectures",
			pod: NewPod().
				WithName("multi-arch-pod").
				WithNamespace("production").
				Build(),
			architectures:  []string{"amd64", "arm64"},
			ruleName:       "multi-arch-rule",
			wantLabel:      true,
			wantAnnotation: true,
		},
		{
			name: "Replace existing architecture constraint",
			pod: NewPod().
				WithName("existing-pod").
				WithNamespace("default").
				WithNodeSelectors(utils.ArchLabel, "amd64").
				Build(),
			architectures:  []string{"s390x"},
			ruleName:       "replacement-rule",
			wantLabel:      true,
			wantAnnotation: true,
		},
		{
			name: "Set architectures with empty rule name",
			pod: NewPod().
				WithName("no-rule-name-pod").
				WithNamespace("default").
				Build(),
			architectures:  []string{"arm64"},
			ruleName:       "",
			wantLabel:      true,
			wantAnnotation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			pod := newPod(tt.pod, ctx, record.NewFakeRecorder(10))

			pod.SetCELArchitectureAffinity(tt.architectures, tt.ruleName)

			// Check that architecture constraints were set
			g.Expect(pod.Spec.Affinity).ToNot(BeNil())
			g.Expect(pod.Spec.Affinity.NodeAffinity).ToNot(BeNil())
			g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).ToNot(BeNil())

			// Find the architecture requirement
			found := false
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						found = true
						g.Expect(expr.Operator).To(Equal(v1.NodeSelectorOpIn))
						g.Expect(expr.Values).To(ConsistOf(tt.architectures))
					}
				}
			}
			g.Expect(found).To(BeTrue(), "Architecture requirement not found in node affinity")

			// Check label
			if tt.wantLabel {
				g.Expect(pod.Labels).To(HaveKey(utils.CELArchitecturePlacementLabel))
				g.Expect(pod.Labels[utils.CELArchitecturePlacementLabel]).To(Equal("true"))
			}

			// Check annotation
			if tt.wantAnnotation {
				g.Expect(pod.Annotations).To(HaveKey(utils.CELArchitecturePlacementRuleAnnotation))
				g.Expect(pod.Annotations[utils.CELArchitecturePlacementRuleAnnotation]).To(Equal(tt.ruleName))
			}
		})
	}
}

func TestPod_CELArchitecturePlacement_EndToEnd_Integration(t *testing.T) {
	tests := []struct {
		name         string
		pod          *v1.Pod
		celPlugin    *plugins.CELArchitecturePlacement
		wantArchs    []string
		wantRuleName string
	}{
		{
			name: "Pod matches first rule - postgres on ppc64le",
			pod: NewPod().
				WithName("postgres-db").
				WithNamespace("production").
				WithLabels(
					"app.kubernetes.io/component", "database",
					"app.kubernetes.io/part-of", "postgresql",
				).
				Build(),
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "postgres-on-ppc64le",
						Expression:    "has(self.metadata.labels) && 'app.kubernetes.io/component' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/component'] == 'database' && 'app.kubernetes.io/part-of' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/part-of'] == 'postgresql'",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "redis-on-amd64",
						Expression:    "self.metadata.name.startsWith('redis-')",
						Architectures: []string{"amd64", "arm64"},
					},
				},
			},
			wantArchs:    []string{"ppc64le"},
			wantRuleName: "postgres-on-ppc64le",
		},
		{
			name: "Pod matches second rule - redis on multi-arch",
			pod: NewPod().
				WithName("redis-cache").
				WithNamespace("production").
				Build(),
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "postgres-on-ppc64le",
						Expression:    "has(self.metadata.labels) && 'app.kubernetes.io/component' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/component'] == 'database'",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "redis-on-multi-arch",
						Expression:    "self.metadata.name.startsWith('redis-')",
						Architectures: []string{"amd64", "arm64"},
					},
				},
			},
			wantArchs:    []string{"amd64", "arm64"},
			wantRuleName: "redis-on-multi-arch",
		},
		{
			name: "Pod matches no rules - use fallback",
			pod: NewPod().
				WithName("generic-app").
				WithNamespace("default").
				Build(),
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"s390x", "ppc64le"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "specific-rule",
						Expression:    "self.metadata.name == 'specific-pod'",
						Architectures: []string{"amd64"},
					},
				},
			},
			wantArchs:    []string{"s390x", "ppc64le"},
			wantRuleName: "",
		},
		{
			name: "Pod with existing constraints - replaced by CEL rule",
			pod: NewPod().
				WithName("nginx-web").
				WithNamespace("production").
				WithNodeSelectors(utils.ArchLabel, "amd64", "region", "us-west").
				WithNodeSelectorTermsMatchExpressions([]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				}).
				Build(),
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "nginx-on-arm64",
						Expression:    "self.metadata.name.startsWith('nginx-')",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantArchs:    []string{"arm64"},
			wantRuleName: "nginx-on-arm64",
		},
		{
			name: "Complex label matching",
			pod: NewPod().
				WithName("frontend-app").
				WithNamespace("production").
				WithLabels(
					"tier", "frontend",
					"environment", "production",
				).
				Build(),
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "frontend-production",
						Expression:    "has(self.metadata.labels) && 'tier' in self.metadata.labels && self.metadata.labels['tier'] == 'frontend' && 'environment' in self.metadata.labels && self.metadata.labels['environment'] == 'production'",
						Architectures: []string{"arm64", "amd64"},
					},
				},
			},
			wantArchs:    []string{"arm64", "amd64"},
			wantRuleName: "frontend-production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			pod := newPod(tt.pod, ctx, record.NewFakeRecorder(10))

			// Get CEL evaluator
			evaluator, err := GetCELEvaluator()
			g.Expect(err).ToNot(HaveOccurred())

			// Evaluate rules
			architectures, err := evaluator.EvaluateRules(tt.pod, tt.celPlugin)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(architectures).To(ConsistOf(tt.wantArchs))

			// Find matching rule name
			ruleName := ""
			for _, rule := range tt.celPlugin.Rules {
				err := evaluator.CompileExpression(rule.Name, rule.Expression)
				g.Expect(err).ToNot(HaveOccurred())

				matches, err := evaluator.EvaluateExpression(rule.Name, tt.pod)
				if err == nil && matches {
					ruleName = rule.Name
					break
				}
			}

			// Apply the architecture affinity
			pod.SetCELArchitectureAffinity(architectures, ruleName)

			// Verify the result
			g.Expect(pod.Spec.Affinity).ToNot(BeNil())
			g.Expect(pod.Spec.Affinity.NodeAffinity).ToNot(BeNil())

			// Check that old architecture constraints were removed from nodeSelector
			if pod.Spec.NodeSelector != nil {
				_, hasArch := pod.Spec.NodeSelector[utils.ArchLabel]
				g.Expect(hasArch).To(BeFalse(), "Architecture should be removed from nodeSelector")
			}

			// Check that new architecture constraints were set
			found := false
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						found = true
						g.Expect(expr.Values).To(ConsistOf(tt.wantArchs))
					}
				}
			}
			g.Expect(found).To(BeTrue(), "Architecture requirement should be set in node affinity")

			// Check labels and annotations
			g.Expect(pod.Labels).To(HaveKey(utils.CELArchitecturePlacementLabel))
			g.Expect(pod.Labels[utils.CELArchitecturePlacementLabel]).To(Equal("true"))

			if tt.wantRuleName != "" {
				g.Expect(pod.Annotations).To(HaveKey(utils.CELArchitecturePlacementRuleAnnotation))
				g.Expect(pod.Annotations[utils.CELArchitecturePlacementRuleAnnotation]).To(Equal(tt.wantRuleName))
			}
		})
	}
}
