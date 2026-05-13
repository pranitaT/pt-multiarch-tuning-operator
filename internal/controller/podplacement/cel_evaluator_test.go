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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

func TestNewCELEvaluator(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}
	if evaluator == nil {
		t.Fatal("NewCELEvaluator() returned nil")
	}
	if evaluator.env == nil {
		t.Error("CELEvaluator.env is nil")
	}
	if evaluator.programs == nil {
		t.Error("CELEvaluator.programs is nil")
	}
}

func TestCELEvaluator_CompileExpression(t *testing.T) {
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
			name:       "Valid expression - pod name equals",
			ruleName:   "test-rule-1",
			expression: "self.metadata.name == 'test-pod'",
			wantError:  false,
		},
		{
			name:       "Valid expression - pod name starts with",
			ruleName:   "test-rule-2",
			expression: "self.metadata.name.startsWith('nginx-')",
			wantError:  false,
		},
		{
			name:       "Valid expression - label exists",
			ruleName:   "test-rule-3",
			expression: "self.metadata.labels.exists(l, l.key == 'app' && l.value == 'web')",
			wantError:  false,
		},
		{
			name:       "Valid expression - complex condition",
			ruleName:   "test-rule-4",
			expression: "self.metadata.labels.exists(l, l.key == 'tier' && l.value == 'frontend') && self.metadata.namespace == 'production'",
			wantError:  false,
		},
		{
			name:       "Invalid expression - syntax error",
			ruleName:   "test-rule-5",
			expression: "self.metadata.name ==",
			wantError:  true,
		},
		{
			name:       "Invalid expression - non-boolean return",
			ruleName:   "test-rule-6",
			expression: "self.metadata.name",
			wantError:  true,
		},
		{
			name:       "Expression with undefined field - compiles but will fail at runtime",
			ruleName:   "test-rule-7",
			expression: "self.metadata.nonexistent == 'value'",
			wantError:  false, // DynType doesn't validate fields at compile time
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

func TestCELEvaluator_EvaluateExpression(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	// Compile test expressions
	testExpressions := map[string]string{
		"name-equals":      "self.metadata.name == 'test-pod'",
		"name-starts-with": "self.metadata.name.startsWith('nginx-')",
		"has-label":        "has(self.metadata.labels) && 'app' in self.metadata.labels && self.metadata.labels['app'] == 'web'",
		"namespace-check":  "self.metadata.namespace == 'production'",
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
			name:     "Pod name matches exactly",
			ruleName: "name-equals",
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
			name:     "Pod name does not match",
			ruleName: "name-equals",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-pod",
					Namespace: "default",
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name:     "Pod name starts with nginx",
			ruleName: "name-starts-with",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nginx-deployment-12345",
					Namespace: "default",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod has matching label",
			ruleName: "has-label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Pod label value does not match",
			ruleName: "has-label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "db-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app": "database",
					},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name:     "Pod in production namespace",
			ruleName: "namespace-check",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prod-pod",
					Namespace: "production",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "Expression not compiled",
			ruleName: "non-existent-rule",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			want:    false,
			wantErr: true,
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

func TestCELEvaluator_EvaluateRules(t *testing.T) {
	evaluator, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator() error = %v", err)
	}

	tests := []struct {
		name      string
		pod       *corev1.Pod
		celPlugin *plugins.CELArchitecturePlacement
		want      []string
		wantErr   bool
	}{
		{
			name: "First rule matches",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-db",
					Namespace: "production",
					Labels: map[string]string{
						"app.kubernetes.io/component": "database",
						"app.kubernetes.io/part-of":   "postgresql",
					},
				},
			},
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
			want:    []string{"ppc64le"},
			wantErr: false,
		},
		{
			name: "Second rule matches",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "redis-cache",
					Namespace: "production",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "postgres-on-ppc64le",
						Expression:    "has(self.metadata.labels) && 'app.kubernetes.io/component' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/component'] == 'database'",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "redis-on-amd64",
						Expression:    "self.metadata.name.startsWith('redis-')",
						Architectures: []string{"amd64", "arm64"},
					},
				},
			},
			want:    []string{"amd64", "arm64"},
			wantErr: false,
		},
		{
			name: "No rules match - use fallback",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "generic-app",
					Namespace: "default",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64", "arm64"},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "postgres-on-ppc64le",
						Expression:    "has(self.metadata.labels) && 'app.kubernetes.io/component' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/component'] == 'database'",
						Architectures: []string{"ppc64le"},
					},
				},
			},
			want:    []string{"amd64", "arm64"},
			wantErr: false,
		},
		{
			name: "Empty rules - use fallback",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			celPlugin: &plugins.CELArchitecturePlacement{
				FallbackArchitectures: []string{"s390x"},
				Rules:                 []plugins.ArchitectureRule{},
			},
			want:    []string{"s390x"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.EvaluateRules(tt.pod, tt.celPlugin)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRules() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("EvaluateRules() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("EvaluateRules()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetCELEvaluator(t *testing.T) {
	// Test singleton pattern
	eval1, err1 := GetCELEvaluator()
	if err1 != nil {
		t.Fatalf("GetCELEvaluator() first call error = %v", err1)
	}

	eval2, err2 := GetCELEvaluator()
	if err2 != nil {
		t.Fatalf("GetCELEvaluator() second call error = %v", err2)
	}

	// Both calls should return the same instance
	if eval1 != eval2 {
		t.Error("GetCELEvaluator() should return the same instance (singleton)")
	}
}
