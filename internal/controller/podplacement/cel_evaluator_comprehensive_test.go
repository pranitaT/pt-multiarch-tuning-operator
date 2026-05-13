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
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

// TestCELEvaluator_ExpressionEdgeCases tests edge cases in CEL expressions
func TestCELEvaluator_ExpressionEdgeCases(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	tests := []struct {
		name       string
		ruleName   string
		expression string
		wantError  bool
	}{
		{
			name:       "Empty expression",
			ruleName:   "empty-expr",
			expression: "",
			wantError:  true,
		},
		{
			name:       "Very long expression",
			ruleName:   "long-expr",
			expression: strings.Repeat("self.metadata.name == 'test' && ", 50) + "true",
			wantError:  false,
		},
		{
			name:       "Expression with unicode",
			ruleName:   "unicode-expr",
			expression: "self.metadata.name == '测试-pod'",
			wantError:  false,
		},
		{
			name:       "Nested conditions (3 levels)",
			ruleName:   "nested-expr",
			expression: "(self.metadata.name == 'test' && (self.metadata.namespace == 'prod' || (self.metadata.namespace == 'staging' && has(self.metadata.labels))))",
			wantError:  false,
		},
		{
			name:       "CEL size() function",
			ruleName:   "size-func",
			expression: "has(self.metadata.labels) && size(self.metadata.labels) > 0",
			wantError:  false,
		},
		{
			name:       "CEL matches() function",
			ruleName:   "matches-func",
			expression: "self.metadata.name.matches('^[a-z]+-[0-9]+$')",
			wantError:  false,
		},
		{
			name:       "CEL contains() function",
			ruleName:   "contains-func",
			expression: "self.metadata.name.contains('test')",
			wantError:  false,
		},
		{
			name:       "Logical OR operator",
			ruleName:   "or-operator",
			expression: "self.metadata.name == 'pod1' || self.metadata.name == 'pod2'",
			wantError:  false,
		},
		{
			name:       "Logical AND operator",
			ruleName:   "and-operator",
			expression: "self.metadata.name == 'test' && self.metadata.namespace == 'default'",
			wantError:  false,
		},
		{
			name:       "Logical NOT operator",
			ruleName:   "not-operator",
			expression: "!(self.metadata.name == 'excluded')",
			wantError:  false,
		},
		{
			name:       "Comparison operators",
			ruleName:   "comparison-ops",
			expression: "has(self.metadata.labels) && size(self.metadata.labels) >= 2",
			wantError:  false,
		},
		{
			name:       "String endsWith()",
			ruleName:   "ends-with",
			expression: "self.metadata.name.endsWith('-deployment')",
			wantError:  false,
		},
		{
			name:       "Complex boolean logic",
			ruleName:   "complex-bool",
			expression: "(self.metadata.name.startsWith('app-') && self.metadata.namespace == 'prod') || (self.metadata.name.startsWith('svc-') && self.metadata.namespace == 'staging')",
			wantError:  false,
		},
		{
			name:       "Negated complex condition",
			ruleName:   "negated-complex",
			expression: "!(self.metadata.name.startsWith('test-') || self.metadata.namespace == 'dev')",
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evaluator.CompileExpression(tt.ruleName, tt.expression)
			if (err != nil) != tt.wantError {
				t.Errorf("CompileExpression() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestCELEvaluator_PodFieldAccess tests accessing various pod fields
func TestCELEvaluator_PodFieldAccess(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	// Compile test expressions
	testExpressions := map[string]string{
		"nodename-check":    "self.spec.nodeName == 'worker-1'",
		"empty-labels":      "!has(self.metadata.labels) || size(self.metadata.labels) == 0",
		"empty-annotations": "!has(self.metadata.annotations) || size(self.metadata.annotations) == 0",
		"label-exists":      "has(self.metadata.labels) && 'app' in self.metadata.labels",
		"annotation-exists": "has(self.metadata.annotations) && 'version' in self.metadata.annotations",
		"annotation-value":  "has(self.metadata.annotations) && 'version' in self.metadata.annotations && self.metadata.annotations['version'] == 'v1.0'",
	}

	for name, expr := range testExpressions {
		if err := evaluator.CompileExpression(name, expr); err != nil {
			t.Fatalf("Failed to compile expression %s: %v", name, err)
		}
	}

	tests := []struct {
		name     string
		ruleName string
		pod      *corev1.Pod
		want     bool
		wantErr  bool
	}{
		{
			name:     "Pod with nodeName set",
			ruleName: "nodename-check",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "worker-1",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod with empty labels",
			ruleName: "empty-labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels:    map[string]string{},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod with nil labels",
			ruleName: "empty-labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod with empty annotations",
			ruleName: "empty-annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod with annotation exists",
			ruleName: "annotation-exists",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"version": "v1.0",
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod with annotation value match",
			ruleName: "annotation-value",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"version": "v1.0",
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod with annotation value mismatch",
			ruleName: "annotation-value",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"version": "v2.0",
					},
				},
			},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.EvaluateExpression(tt.ruleName, tt.pod)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateExpression() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCELEvaluator_NamespacePatterns tests namespace-based rules
func TestCELEvaluator_NamespacePatterns(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	testExpressions := map[string]string{
		"ns-starts-with": "self.metadata.namespace.startsWith('kube-')",
		"ns-ends-with":   "self.metadata.namespace.endsWith('-system')",
		"ns-contains":    "self.metadata.namespace.contains('prod')",
		"ns-multiple":    "self.metadata.namespace.startsWith('app-') && self.metadata.namespace.endsWith('-prod')",
	}

	for name, expr := range testExpressions {
		if err := evaluator.CompileExpression(name, expr); err != nil {
			t.Fatalf("Failed to compile expression %s: %v", name, err)
		}
	}

	tests := []struct {
		name      string
		ruleName  string
		namespace string
		want      bool
	}{
		{
			name:      "Namespace starts with kube-",
			ruleName:  "ns-starts-with",
			namespace: "kube-system",
			want:      true,
		},
		{
			name:      "Namespace does not start with kube-",
			ruleName:  "ns-starts-with",
			namespace: "default",
			want:      false,
		},
		{
			name:      "Namespace ends with -system",
			ruleName:  "ns-ends-with",
			namespace: "monitoring-system",
			want:      true,
		},
		{
			name:      "Namespace contains prod",
			ruleName:  "ns-contains",
			namespace: "app-production",
			want:      true,
		},
		{
			name:      "Namespace matches multiple conditions",
			ruleName:  "ns-multiple",
			namespace: "app-service-prod",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: tt.namespace,
				},
			}
			got, err := evaluator.EvaluateExpression(tt.ruleName, pod)
			if err != nil {
				t.Errorf("EvaluateExpression() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateExpression() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCELEvaluator_MultipleRulesPriority tests rule evaluation order
func TestCELEvaluator_MultipleRulesPriority(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	tests := []struct {
		name         string
		pod          *corev1.Pod
		celPlugin    *plugins.CELArchitecturePlacement
		wantArchs    []string
		wantRuleName string
	}{
		{
			name: "First rule matches - others ignored",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "production",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "rule1",
						Expression:    "self.metadata.name == 'test-pod'",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "rule2",
						Expression:    "self.metadata.name == 'test-pod'",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantArchs:    []string{"ppc64le"},
			wantRuleName: "rule1",
		},
		{
			name: "All rules fail - use fallback",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-pod",
					Namespace: "default",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"s390x"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "rule1",
						Expression:    "self.metadata.name == 'test-pod'",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "rule2",
						Expression:    "self.metadata.namespace == 'production'",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantArchs:    []string{"s390x"},
			wantRuleName: "",
		},
		{
			name: "Second rule matches after first fails",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-pod",
					Namespace: "production",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "rule1",
						Expression:    "self.metadata.name == 'test-pod'",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "rule2",
						Expression:    "self.metadata.namespace == 'production'",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantArchs:    []string{"arm64"},
			wantRuleName: "rule2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			architectures, err := evaluator.EvaluateRules(tt.pod, tt.celPlugin)
			if err != nil {
				t.Errorf("EvaluateRules() error = %v", err)
				return
			}
			if len(architectures) != len(tt.wantArchs) {
				t.Errorf("EvaluateRules() = %v, want %v", architectures, tt.wantArchs)
				return
			}
			for i := range architectures {
				if architectures[i] != tt.wantArchs[i] {
					t.Errorf("EvaluateRules()[%d] = %v, want %v", i, architectures[i], tt.wantArchs[i])
				}
			}
		})
	}
}

// TestCELEvaluator_ConcurrentAccess tests thread safety
func TestCELEvaluator_ConcurrentAccess(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	// Compile some expressions
	expressions := map[string]string{
		"rule1": "self.metadata.name == 'pod1'",
		"rule2": "self.metadata.name == 'pod2'",
		"rule3": "self.metadata.namespace == 'default'",
	}

	for name, expr := range expressions {
		if err := evaluator.CompileExpression(name, expr); err != nil {
			t.Fatalf("Failed to compile expression %s: %v", name, err)
		}
	}

	// Test concurrent evaluation
	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
				},
			}
			_, err := evaluator.EvaluateExpression("rule1", pod)
			if err != nil {
				t.Errorf("Goroutine %d: EvaluateExpression() error = %v", id, err)
			}
		}(i)
	}

	wg.Wait()
}

// TestCELEvaluator_LabelSpecialCases tests labels with special characters
func TestCELEvaluator_LabelSpecialCases(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	testExpressions := map[string]string{
		"label-with-dot":   "has(self.metadata.labels) && 'app.kubernetes.io/name' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/name'] == 'myapp'",
		"label-with-slash": "has(self.metadata.labels) && 'app.kubernetes.io/component' in self.metadata.labels",
	}

	for name, expr := range testExpressions {
		if err := evaluator.CompileExpression(name, expr); err != nil {
			t.Fatalf("Failed to compile expression %s: %v", name, err)
		}
	}

	tests := []struct {
		name     string
		ruleName string
		pod      *corev1.Pod
		want     bool
	}{
		{
			name:     "Label with dots in key",
			ruleName: "label-with-dot",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "myapp",
					},
				},
			},
			want: true,
		},
		{
			name:     "Label with slashes",
			ruleName: "label-with-slash",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/component": "database",
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.EvaluateExpression(tt.ruleName, tt.pod)
			if err != nil {
				t.Errorf("EvaluateExpression() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateExpression() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCELEvaluator_StringMatchingPatterns tests various string matching patterns
func TestCELEvaluator_StringMatchingPatterns(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	testExpressions := map[string]string{
		"regex-match":    "self.metadata.name.matches('^app-[0-9]+-[a-z]+$')",
		"prefix-match":   "self.metadata.name.startsWith('nginx-') || self.metadata.name.startsWith('apache-')",
		"suffix-match":   "self.metadata.name.endsWith('-deployment') || self.metadata.name.endsWith('-statefulset')",
		"contains-match": "self.metadata.name.contains('prod') || self.metadata.name.contains('staging')",
	}

	for name, expr := range testExpressions {
		if err := evaluator.CompileExpression(name, expr); err != nil {
			t.Fatalf("Failed to compile expression %s: %v", name, err)
		}
	}

	tests := []struct {
		name     string
		ruleName string
		podName  string
		want     bool
	}{
		{
			name:     "Regex pattern matches",
			ruleName: "regex-match",
			podName:  "app-123-abc",
			want:     true,
		},
		{
			name:     "Regex pattern does not match",
			ruleName: "regex-match",
			podName:  "app-abc-123",
			want:     false,
		},
		{
			name:     "Prefix matches first option",
			ruleName: "prefix-match",
			podName:  "nginx-web-server",
			want:     true,
		},
		{
			name:     "Prefix matches second option",
			ruleName: "prefix-match",
			podName:  "apache-http-server",
			want:     true,
		},
		{
			name:     "Suffix matches",
			ruleName: "suffix-match",
			podName:  "myapp-deployment",
			want:     true,
		},
		{
			name:     "Contains matches",
			ruleName: "contains-match",
			podName:  "myapp-prod-v1",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.podName,
					Namespace: "default",
				},
			}
			got, err := evaluator.EvaluateExpression(tt.ruleName, pod)
			if err != nil {
				t.Errorf("EvaluateExpression() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateExpression() = %v, want %v", got, tt.want)
			}
		})
	}
}
