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
	"sync"

	"github.com/google/cel-go/cel"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

// CELEvaluator handles CEL expression compilation and evaluation for architecture placement rules
type CELEvaluator struct {
	env      *cel.Env
	programs map[string]cel.Program
	mu       sync.RWMutex
}

// NewCELEvaluator creates a new CEL evaluator with the Pod type registered
func NewCELEvaluator() (*CELEvaluator, error) {
	// Create CEL environment with a dynamic type for Pod
	// We use cel.DynType to allow flexible access to pod fields
	env, err := cel.NewEnv(
		cel.Variable("self", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELEvaluator{
		env:      env,
		programs: make(map[string]cel.Program),
	}, nil
}

// CompileExpression compiles a CEL expression and caches it
func (e *CELEvaluator) CompileExpression(name, expression string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if already compiled
	if _, exists := e.programs[name]; exists {
		return nil
	}

	// Parse the expression
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("failed to compile CEL expression %q: %w", name, issues.Err())
	}

	// Check that the expression returns a boolean
	if ast.OutputType() != cel.BoolType {
		return fmt.Errorf("CEL expression %q must return a boolean, got %v", name, ast.OutputType())
	}

	// Create the program
	program, err := e.env.Program(ast)
	if err != nil {
		return fmt.Errorf("failed to create CEL program for %q: %w", name, err)
	}

	e.programs[name] = program
	return nil
}

// EvaluateExpression evaluates a compiled CEL expression against a pod
func (e *CELEvaluator) EvaluateExpression(name string, pod *corev1.Pod) (bool, error) {
	e.mu.RLock()
	program, exists := e.programs[name]
	e.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("CEL expression %q not compiled", name)
	}

	// Convert Pod to a map structure that CEL can work with
	podMap := podToMap(pod)

	// Evaluate the expression
	val, _, err := program.Eval(map[string]interface{}{
		"self": podMap,
	})
	if err != nil {
		return false, fmt.Errorf("failed to evaluate CEL expression %q: %w", name, err)
	}

	// Convert to boolean
	boolVal, ok := val.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL expression %q did not return a boolean", name)
	}

	return boolVal, nil
}

// podToMap converts a Pod to a map structure that CEL can evaluate
func podToMap(pod *corev1.Pod) map[string]interface{} {
	labels := make(map[string]string)
	if pod.Labels != nil {
		labels = pod.Labels
	}

	annotations := make(map[string]string)
	if pod.Annotations != nil {
		annotations = pod.Annotations
	}

	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":        pod.Name,
			"namespace":   pod.Namespace,
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": map[string]interface{}{
			"nodeName": pod.Spec.NodeName,
		},
	}
}

// EvaluateRules evaluates all rules in order and returns the architectures from the first matching rule
// If no rules match, returns the fallback architectures
func (e *CELEvaluator) EvaluateRules(pod *corev1.Pod, celPlugin *plugins.CELArchitecturePlacement) ([]string, error) {
	// Compile all rules if not already compiled
	for _, rule := range celPlugin.Rules {
		if err := e.CompileExpression(rule.Name, rule.Expression); err != nil {
			return nil, err
		}
	}

	// Evaluate rules in order
	for _, rule := range celPlugin.Rules {
		matches, err := e.EvaluateExpression(rule.Name, pod)
		if err != nil {
			// Log error but continue to next rule
			continue
		}
		if matches {
			return rule.Architectures, nil
		}
	}

	// No rules matched, return fallback architectures
	return celPlugin.FallbackArchitectures, nil
}

// celEvaluatorSingleton is the global CEL evaluator instance
var (
	celEvaluatorInstance *CELEvaluator
	celEvaluatorOnce     sync.Once
	celEvaluatorErr      error
)

// GetCELEvaluator returns the singleton CEL evaluator instance
func GetCELEvaluator() (*CELEvaluator, error) {
	celEvaluatorOnce.Do(func() {
		celEvaluatorInstance, celEvaluatorErr = NewCELEvaluator()
	})
	return celEvaluatorInstance, celEvaluatorErr
}
