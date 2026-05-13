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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

// TestCELEvaluator_ErrorRecovery tests error handling and recovery
func TestCELEvaluator_ErrorRecovery(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	tests := []struct {
		name      string
		celPlugin *plugins.CELArchitecturePlacement
		pod       *corev1.Pod
		wantArchs []string
		wantErr   bool
	}{
		{
			name: "Compilation error in first rule - continue to second",
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "invalid-rule",
						Expression:    "self.metadata.name ==", // Invalid syntax
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "valid-rule",
						Expression:    "self.metadata.name == 'test-pod'",
						Architectures: []string{"arm64"},
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			wantArchs: nil,
			wantErr:   true, // First rule compilation fails
		},
		{
			name: "All rules have compilation errors - use fallback",
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"s390x"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "invalid-rule-1",
						Expression:    "self.metadata.name ==",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "invalid-rule-2",
						Expression:    "self.metadata.name",
						Architectures: []string{"arm64"},
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			wantArchs: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := evaluator.EvaluateRules(tt.pod, tt.celPlugin)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRules() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCELEvaluator_BoundaryValues tests boundary conditions
func TestCELEvaluator_BoundaryValues(t *testing.T) {
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
			name:       "Rule name at max length (253 chars)",
			ruleName:   strings.Repeat("a", 253),
			expression: "self.metadata.name == 'test'",
			wantError:  false,
		},
		{
			name:       "Very long expression",
			ruleName:   "long-expression",
			expression: strings.Repeat("self.metadata.name == 'test' && ", 100) + "true",
			wantError:  false,
		},
		{
			name:       "Single character rule name",
			ruleName:   "a",
			expression: "self.metadata.name == 'test'",
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

// TestCELEvaluator_MaxRules tests with maximum number of rules
func TestCELEvaluator_MaxRules(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	// Create 50 rules (max limit)
	rules := make([]plugins.ArchitectureRule, 50)
	for i := 0; i < 50; i++ {
		rules[i] = plugins.ArchitectureRule{
			Name:          "rule-" + string(rune(i)),
			Expression:    "self.metadata.name == 'pod-" + string(rune(i)) + "'",
			Architectures: []string{"amd64"},
		}
	}

	celPlugin := &plugins.CELArchitecturePlacement{
		FallbackArchitectures: []string{"arm64"},
		Rules:                 rules,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	// Should use fallback since no rules match
	architectures, err := evaluator.EvaluateRules(pod, celPlugin)
	if err != nil {
		t.Errorf("EvaluateRules() error = %v", err)
	}
	if len(architectures) != 1 || architectures[0] != "arm64" {
		t.Errorf("EvaluateRules() = %v, want [arm64]", architectures)
	}
}

// TestCELEvaluator_RealWorldScenarios tests realistic use cases
func TestCELEvaluator_RealWorldScenarios(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	tests := []struct {
		name      string
		pod       *corev1.Pod
		celPlugin *plugins.CELArchitecturePlacement
		wantArchs []string
	}{
		{
			name: "StatefulSet pod with ordinal index",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-0",
					Namespace: "database",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "postgresql",
						"app.kubernetes.io/component": "database",
					},
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "statefulset-postgres",
						Expression:    "self.metadata.name.matches('^postgres-[0-9]+$') && has(self.metadata.labels) && 'app.kubernetes.io/name' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/name'] == 'postgresql'",
						Architectures: []string{"ppc64le"},
					},
				},
			},
			wantArchs: []string{"ppc64le"},
		},
		{
			name: "Job pod with generated name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-job-abc123",
					Namespace: "default",
					Labels: map[string]string{
						"job-name": "backup-job",
					},
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "backup-jobs",
						Expression:    "self.metadata.name.startsWith('backup-job-') && has(self.metadata.labels) && 'job-name' in self.metadata.labels",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantArchs: []string{"arm64"},
		},
		{
			name: "System namespace pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "coredns-abc123",
					Namespace: "kube-system",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "system-pods",
						Expression:    "self.metadata.namespace == 'kube-system'",
						Architectures: []string{"amd64", "arm64"},
					},
				},
			},
			wantArchs: []string{"amd64", "arm64"},
		},
		{
			name: "Multi-label matching",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "frontend-app",
					Namespace: "production",
					Labels: map[string]string{
						"tier":        "frontend",
						"environment": "production",
						"version":     "v2.0",
					},
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "production-frontend-v2",
						Expression:    "has(self.metadata.labels) && 'tier' in self.metadata.labels && self.metadata.labels['tier'] == 'frontend' && 'environment' in self.metadata.labels && self.metadata.labels['environment'] == 'production' && 'version' in self.metadata.labels && self.metadata.labels['version'] == 'v2.0'",
						Architectures: []string{"arm64", "amd64"},
					},
				},
			},
			wantArchs: []string{"arm64", "amd64"},
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

// TestCELEvaluator_Idempotency tests that repeated evaluations produce same results
func TestCELEvaluator_Idempotency(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	celPlugin := &plugins.CELArchitecturePlacement{
		FallbackArchitectures: []string{"amd64"},
		Rules: []plugins.ArchitectureRule{
			{
				Name:          "test-rule",
				Expression:    "self.metadata.name == 'test-pod'",
				Architectures: []string{"ppc64le"},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	// Evaluate multiple times
	for i := 0; i < 10; i++ {
		architectures, err := evaluator.EvaluateRules(pod, celPlugin)
		if err != nil {
			t.Errorf("Iteration %d: EvaluateRules() error = %v", i, err)
		}
		if len(architectures) != 1 || architectures[0] != "ppc64le" {
			t.Errorf("Iteration %d: EvaluateRules() = %v, want [ppc64le]", i, architectures)
		}
	}

	// Recompile same expression - should use cached version
	err = evaluator.CompileExpression("test-rule", "self.metadata.name == 'test-pod'")
	if err != nil {
		t.Errorf("Recompile error = %v", err)
	}
}

// TestCELEvaluator_ComplexBooleanLogic tests advanced boolean expressions
func TestCELEvaluator_ComplexBooleanLogic(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	testExpressions := map[string]string{
		"complex-and-or":    "(self.metadata.name.startsWith('app-') && self.metadata.namespace == 'prod') || (self.metadata.name.startsWith('svc-') && self.metadata.namespace == 'staging')",
		"nested-not":        "!(self.metadata.name.startsWith('test-') || self.metadata.namespace == 'dev')",
		"multiple-parens":   "((self.metadata.name == 'pod1' || self.metadata.name == 'pod2') && (self.metadata.namespace == 'ns1' || self.metadata.namespace == 'ns2'))",
		"short-circuit-and": "has(self.metadata.labels) && 'app' in self.metadata.labels && self.metadata.labels['app'] == 'web'",
		"short-circuit-or":  "self.metadata.name == 'pod1' || self.metadata.name == 'pod2' || self.metadata.name == 'pod3'",
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
			name:     "Complex AND-OR: first condition true",
			ruleName: "complex-and-or",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-frontend",
					Namespace: "prod",
				},
			},
			want: true,
		},
		{
			name:     "Complex AND-OR: second condition true",
			ruleName: "complex-and-or",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-backend",
					Namespace: "staging",
				},
			},
			want: true,
		},
		{
			name:     "Complex AND-OR: both conditions false",
			ruleName: "complex-and-or",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "db-server",
					Namespace: "dev",
				},
			},
			want: false,
		},
		{
			name:     "Nested NOT: condition false (result true)",
			ruleName: "nested-not",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prod-app",
					Namespace: "production",
				},
			},
			want: true,
		},
		{
			name:     "Multiple parentheses: all conditions match",
			ruleName: "multiple-parens",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "ns1",
				},
			},
			want: true,
		},
		{
			name:     "Short-circuit AND: all conditions true",
			ruleName: "short-circuit-and",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			want: true,
		},
		{
			name:     "Short-circuit OR: first condition true",
			ruleName: "short-circuit-or",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
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

// TestCELEvaluator_AnnotationBasedRules tests rules based on annotations
func TestCELEvaluator_AnnotationBasedRules(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	testExpressions := map[string]string{
		"annotation-exists":      "has(self.metadata.annotations) && 'deployment.kubernetes.io/revision' in self.metadata.annotations",
		"annotation-value-match": "has(self.metadata.annotations) && 'version' in self.metadata.annotations && self.metadata.annotations['version'] == 'v2.0'",
		"annotation-pattern":     "has(self.metadata.annotations) && 'build.id' in self.metadata.annotations && self.metadata.annotations['build.id'].matches('^[0-9]+$')",
		"multiple-annotations":   "has(self.metadata.annotations) && 'app' in self.metadata.annotations && 'version' in self.metadata.annotations",
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
			name:     "Annotation exists",
			ruleName: "annotation-exists",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"deployment.kubernetes.io/revision": "3",
					},
				},
			},
			want: true,
		},
		{
			name:     "Annotation value matches",
			ruleName: "annotation-value-match",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"version": "v2.0",
					},
				},
			},
			want: true,
		},
		{
			name:     "Annotation pattern matches",
			ruleName: "annotation-pattern",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"build.id": "12345",
					},
				},
			},
			want: true,
		},
		{
			name:     "Multiple annotations present",
			ruleName: "multiple-annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"app":     "myapp",
						"version": "v1.0",
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
