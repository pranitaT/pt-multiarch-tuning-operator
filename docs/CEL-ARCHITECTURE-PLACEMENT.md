# CEL Architecture Placement Plugin

## Overview

The **CEL Architecture Placement Plugin** is a powerful new feature in the Multiarch Tuning Operator that enables dynamic, rule-based architecture selection for Kubernetes pods using Common Expression Language (CEL). This plugin allows you to define flexible rules that automatically determine which CPU architectures (amd64, arm64, ppc64le, s390x) should be used for pod placement based on pod metadata, labels, annotations, and other attributes.

## Table of Contents

- [What is CEL?](#what-is-cel)
- [Why CEL Architecture Placement?](#why-cel-architecture-placement)
- [Architecture Overview](#architecture-overview)
- [Key Features](#key-features)
- [Configuration](#configuration)
- [CEL Expression Syntax](#cel-expression-syntax)
- [Examples](#examples)
- [Integration Flow](#integration-flow)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

---

## What is CEL?

**Common Expression Language (CEL)** is a non-Turing complete expression language designed for simplicity, speed, and safety. It's used extensively in Kubernetes for validation, admission control, and policy enforcement.

### CEL Benefits:
- ✅ **Safe**: No side effects, loops, or recursion
- ✅ **Fast**: Compiled expressions with caching
- ✅ **Simple**: Easy-to-read syntax similar to C/JavaScript
- ✅ **Powerful**: Rich set of built-in functions

---

## Why CEL Architecture Placement?

Traditional architecture placement relies on static node selectors or affinity rules. The CEL Architecture Placement Plugin provides:

1. **Dynamic Decision Making**: Rules evaluated at runtime based on pod attributes
2. **Centralized Policy**: Define architecture policies in one place
3. **Flexibility**: Complex logic without custom code
4. **Namespace Isolation**: Different rules per namespace
5. **Fallback Safety**: Always has a default architecture set

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Pod Creation Request                          │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│              Multiarch Tuning Operator Webhook                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  1. Check if PodPlacementConfig exists in namespace       │  │
│  │  2. Check if CELArchitecturePlacement plugin is enabled   │  │
│  └───────────────────────────────────────────────────────────┘  │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      CEL Evaluator                               │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  3. Compile CEL expressions (cached)                      │  │
│  │  4. Convert Pod to CEL-compatible map structure           │  │
│  │  5. Evaluate rules in order                               │  │
│  │  6. Return architectures from first matching rule         │  │
│  │  7. Use fallback if no rules match                        │  │
│  └───────────────────────────────────────────────────────────┘  │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                  Pod Architecture Mutation                       │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  8. Remove existing architecture constraints              │  │
│  │  9. Set new architecture node affinity                    │  │
│  │  10. Add labels/annotations for tracking                  │  │
│  └───────────────────────────────────────────────────────────┘  │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│              Pod Scheduled with Architecture Constraints         │
└─────────────────────────────────────────────────────────────────┘
```

---

## Key Features

### 1. **Rule-Based Architecture Selection**
Define multiple rules that are evaluated in order. The first matching rule determines the architecture.

### 2. **Fallback Architectures**
Always specify fallback architectures for when no rules match, ensuring pods can always be scheduled.

### 3. **Expression Caching**
CEL expressions are compiled once and cached for performance.

### 4. **Thread-Safe Evaluation**
Concurrent pod evaluations are handled safely with proper locking.

### 5. **Constraint Replacement**
Automatically removes existing architecture constraints before applying new ones.

### 6. **Namespace-Scoped**
Each namespace can have its own CEL rules via PodPlacementConfig.

---

## Configuration

### PodPlacementConfig with CEL Plugin

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: cel-placement-config
  namespace: my-namespace
spec:
  labelSelector:
    matchLabels:
      app: my-app
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
        - arm64
      rules:
        - name: high-memory-workloads
          expression: 'self.metadata.labels["workload-type"] == "memory-intensive"'
          architectures:
            - amd64
        - name: batch-jobs
          expression: 'self.metadata.labels["job-type"] == "batch"'
          architectures:
            - arm64
        - name: production-namespace
          expression: 'self.metadata.namespace.startsWith("prod-")'
          architectures:
            - amd64
            - arm64
```

### Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | boolean | Yes | Enable/disable the plugin |
| `fallbackArchitectures` | []string | Yes | Architectures to use when no rules match (1-4 items) |
| `rules` | []ArchitectureRule | No | List of CEL rules (max 50) |

### ArchitectureRule Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Descriptive name for the rule (1-253 chars) |
| `expression` | string | Yes | CEL expression that returns boolean |
| `architectures` | []string | Yes | Target architectures (1-4 items) |

### Valid Architectures

- `amd64` - x86-64 architecture
- `arm64` - ARM 64-bit architecture
- `ppc64le` - PowerPC 64-bit little-endian
- `s390x` - IBM System z architecture

---

## CEL Expression Syntax

### Available Pod Fields

The pod is available as `self` in CEL expressions:

```javascript
self.metadata.name          // Pod name
self.metadata.namespace     // Pod namespace
self.metadata.labels        // Map of labels
self.metadata.annotations   // Map of annotations
self.spec.nodeName          // Assigned node name
```

### CEL Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `==` | Equality | `self.metadata.name == "my-pod"` |
| `!=` | Inequality | `self.metadata.namespace != "kube-system"` |
| `&&` | Logical AND | `condition1 && condition2` |
| `\|\|` | Logical OR | `condition1 \|\| condition2` |
| `!` | Logical NOT | `!condition` |
| `in` | Membership | `"key" in self.metadata.labels` |

### CEL Built-in Functions

| Function | Description | Example |
|----------|-------------|---------|
| `startsWith()` | String prefix check | `self.metadata.namespace.startsWith("prod-")` |
| `endsWith()` | String suffix check | `self.metadata.name.endsWith("-worker")` |
| `contains()` | Substring check | `self.metadata.name.contains("batch")` |
| `matches()` | Regex matching | `self.metadata.name.matches("^app-[0-9]+$")` |
| `size()` | Collection size | `size(self.metadata.labels) > 5` |

---

## Examples

### Example 1: Namespace-Based Architecture Selection

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: namespace-based-placement
  namespace: multi-tenant
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
      rules:
        - name: production-workloads
          expression: 'self.metadata.namespace.startsWith("prod-")'
          architectures:
            - amd64
        - name: development-workloads
          expression: 'self.metadata.namespace.startsWith("dev-")'
          architectures:
            - arm64
        - name: staging-workloads
          expression: 'self.metadata.namespace == "staging"'
          architectures:
            - amd64
            - arm64
```

**Diagram:**
```
┌─────────────────────────────────────────────────────────────┐
│                    Pod in Namespace                          │
└────────────────────┬────────────────────────────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
        ▼            ▼            ▼
   prod-app     dev-app      staging-app
        │            │            │
        ▼            ▼            ▼
     amd64        arm64      amd64+arm64
```

### Example 2: Label-Based Workload Classification

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: workload-classification
  namespace: compute-intensive
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
        - arm64
      rules:
        - name: gpu-workloads
          expression: 'self.metadata.labels["gpu-required"] == "true"'
          architectures:
            - amd64
        - name: ml-training
          expression: 'self.metadata.labels["workload"] == "ml-training"'
          architectures:
            - amd64
        - name: web-services
          expression: 'self.metadata.labels["tier"] == "frontend"'
          architectures:
            - arm64
        - name: cost-optimized
          expression: 'self.metadata.labels["cost-priority"] == "high"'
          architectures:
            - arm64
```

**Flow Diagram:**
```
                    ┌─────────────┐
                    │   Pod       │
                    └──────┬──────┘
                           │
                    Check Labels
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
  gpu-required=true   workload=ml      tier=frontend
        │                  │                  │
        ▼                  ▼                  ▼
      amd64              amd64              arm64
```

### Example 3: StatefulSet Ordinal-Based Placement

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: statefulset-placement
  namespace: databases
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
      rules:
        - name: primary-replicas
          expression: 'self.metadata.name.matches(".*-[0-2]$")'
          architectures:
            - amd64
        - name: secondary-replicas
          expression: 'self.metadata.name.matches(".*-[3-9]$")'
          architectures:
            - arm64
```

**Example:**
```
StatefulSet: postgres-cluster

postgres-cluster-0  → amd64 (primary)
postgres-cluster-1  → amd64 (primary)
postgres-cluster-2  → amd64 (primary)
postgres-cluster-3  → arm64 (secondary)
postgres-cluster-4  → arm64 (secondary)
postgres-cluster-5  → arm64 (secondary)
```

### Example 4: Annotation-Based Configuration

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: annotation-based-placement
  namespace: flexible-workloads
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
        - arm64
      rules:
        - name: explicit-architecture
          expression: '"architecture" in self.metadata.annotations'
          architectures:
            - amd64  # Will be overridden by annotation value
        - name: performance-critical
          expression: 'self.metadata.annotations["performance"] == "critical"'
          architectures:
            - amd64
        - name: cost-optimized
          expression: 'self.metadata.annotations["cost-optimize"] == "true"'
          architectures:
            - arm64
```

### Example 5: Complex Multi-Condition Rules

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: complex-rules
  namespace: enterprise-apps
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
      rules:
        - name: production-critical
          expression: |
            self.metadata.namespace.startsWith("prod-") &&
            self.metadata.labels["tier"] == "critical" &&
            self.metadata.labels["sla"] == "high"
          architectures:
            - amd64
        - name: dev-or-test
          expression: |
            self.metadata.namespace.startsWith("dev-") ||
            self.metadata.namespace.startsWith("test-")
          architectures:
            - arm64
        - name: batch-jobs
          expression: |
            self.metadata.labels["job-type"] == "batch" &&
            !self.metadata.name.contains("urgent")
          architectures:
            - arm64
            - ppc64le
```

**Decision Tree:**
```
                        Pod Created
                            │
                            ▼
              ┌─────────────────────────┐
              │ prod-* && tier=critical │
              │    && sla=high?         │
              └────────┬────────────────┘
                       │
              Yes ─────┤───── No
                       │         │
                       ▼         ▼
                     amd64   ┌──────────────┐
                             │ dev-* or     │
                             │ test-*?      │
                             └──┬───────────┘
                                │
                       Yes ─────┤───── No
                                │         │
                                ▼         ▼
                              arm64   ┌──────────────┐
                                      │ batch job && │
                                      │ !urgent?     │
                                      └──┬───────────┘
                                         │
                                Yes ─────┤───── No
                                         │         │
                                         ▼         ▼
                                   arm64+ppc64le  amd64
                                                (fallback)
```

### Example 6: System vs User Workloads

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: system-user-separation
  namespace: platform
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures:
        - amd64
      rules:
        - name: system-components
          expression: |
            self.metadata.namespace in ["kube-system", "openshift-system"] ||
            self.metadata.labels["component"] == "system"
          architectures:
            - amd64
        - name: monitoring-stack
          expression: 'self.metadata.labels["app.kubernetes.io/part-of"] == "monitoring"'
          architectures:
            - amd64
        - name: user-applications
          expression: 'self.metadata.labels["app.kubernetes.io/managed-by"] == "user"'
          architectures:
            - arm64
```

---

## Integration Flow

### Complete Pod Placement Flow

```
┌──────────────────────────────────────────────────────────────────┐
│ 1. Pod Creation                                                   │
│    User creates pod in namespace with PodPlacementConfig         │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 2. Webhook Intercept                                              │
│    MutatingWebhook catches pod creation                          │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 3. Config Lookup                                                  │
│    Find PodPlacementConfig in pod's namespace                    │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 4. Plugin Check                                                   │
│    Is CELArchitecturePlacement enabled?                          │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                    Yes  │  No → Skip CEL processing
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 5. CEL Evaluation                                                 │
│    a. Get CEL evaluator singleton                                │
│    b. Compile expressions (if not cached)                        │
│    c. Convert pod to CEL-compatible map                          │
│    d. Evaluate rules in order                                    │
│    e. Return architectures from first match or fallback          │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 6. Architecture Constraint Removal                                │
│    Remove existing kubernetes.io/arch from:                      │
│    - nodeSelector                                                │
│    - nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution │
│    - nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution│
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 7. New Architecture Affinity                                      │
│    Set nodeAffinity with selected architectures:                 │
│    - Create matchExpressions with In operator                    │
│    - Add to requiredDuringSchedulingIgnoredDuringExecution       │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 8. Metadata Update                                                │
│    Add labels/annotations:                                       │
│    - multiarch.openshift.io/scheduling-gate: removed            │
│    - Record which rule matched                                   │
└────────────────────────┬─────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│ 9. Pod Scheduling                                                 │
│    Kubernetes scheduler places pod on matching node             │
└──────────────────────────────────────────────────────────────────┘
```

---

## Best Practices

### 1. **Rule Ordering**
Place more specific rules before general ones:
```yaml
rules:
  - name: specific-app-override
    expression: 'self.metadata.name == "critical-app-pod"'
    architectures: [amd64]
  - name: general-namespace-rule
    expression: 'self.metadata.namespace.startsWith("prod-")'
    architectures: [amd64, arm64]
```

### 2. **Always Define Fallback**
Ensure pods can be scheduled even if all rules fail:
```yaml
fallbackArchitectures:
  - amd64
  - arm64  # Multiple architectures for flexibility
```

### 3. **Use Descriptive Rule Names**
Make rules self-documenting:
```yaml
- name: gpu-ml-workloads-require-amd64
  expression: 'self.metadata.labels["gpu"] == "true"'
  architectures: [amd64]
```

### 4. **Test Expressions**
Validate CEL expressions before deployment:
```bash
# Use kubectl to test expressions
kubectl create --dry-run=client -f pod.yaml
```

### 5. **Monitor Performance**
- Expressions are cached after first compilation
- Keep expressions simple for faster evaluation
- Avoid very complex regex patterns

### 6. **Label Conventions**
Use consistent label naming:
```yaml
labels:
  workload-type: "compute-intensive"
  cost-priority: "high"
  performance-tier: "critical"
```

### 7. **Documentation**
Document your rules in the PodPlacementConfig:
```yaml
metadata:
  annotations:
    description: "Routes GPU workloads to amd64, batch jobs to arm64"
```

---

## Troubleshooting

### Common Issues

#### 1. **Rule Not Matching**

**Symptom**: Pod uses fallback architectures instead of expected rule.

**Debug Steps**:
```bash
# Check pod labels
kubectl get pod <pod-name> -o jsonpath='{.metadata.labels}'

# Check PodPlacementConfig
kubectl get podplacementconfig -n <namespace> -o yaml

# Check operator logs
kubectl logs -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator
```

**Common Causes**:
- Typo in label/annotation name
- Case sensitivity in string comparisons
- Missing labels on pod

#### 2. **CEL Compilation Error**

**Symptom**: Error in operator logs about CEL compilation.

**Example Error**:
```
failed to compile CEL expression "invalid-rule": ERROR: <input>:1:5: Syntax error
```

**Solution**:
- Validate CEL syntax
- Check for unmatched quotes/parentheses
- Ensure expression returns boolean

#### 3. **Architecture Not Applied**

**Symptom**: Pod scheduled on wrong architecture.

**Debug Steps**:
```bash
# Check pod's node affinity
kubectl get pod <pod-name> -o jsonpath='{.spec.affinity.nodeAffinity}'

# Check node architecture
kubectl get node <node-name> -o jsonpath='{.metadata.labels.kubernetes\.io/arch}'
```

**Common Causes**:
- Plugin not enabled
- PodPlacementConfig in wrong namespace
- Existing architecture constraints not removed

#### 4. **Performance Issues**

**Symptom**: Slow pod creation.

**Solutions**:
- Simplify CEL expressions
- Reduce number of rules
- Check for regex patterns that backtrack

### Debugging Commands

```bash
# List all PodPlacementConfigs
kubectl get podplacementconfig --all-namespaces

# Describe specific config
kubectl describe podplacementconfig <name> -n <namespace>

# Check operator status
kubectl get deployment -n openshift-multiarch-tuning-operator

# View operator logs
kubectl logs -n openshift-multiarch-tuning-operator \
  deployment/multiarch-tuning-operator --tail=100

# Check webhook configuration
kubectl get mutatingwebhookconfiguration

# Test pod creation with dry-run
kubectl create -f pod.yaml --dry-run=server
```

### Validation Checklist

- [ ] PodPlacementConfig exists in correct namespace
- [ ] CELArchitecturePlacement plugin is enabled
- [ ] Fallback architectures are defined
- [ ] CEL expressions are syntactically correct
- [ ] CEL expressions return boolean values
- [ ] Architecture names are valid (amd64, arm64, ppc64le, s390x)
- [ ] Rule names are unique and descriptive
- [ ] Pod has required labels/annotations for rules
- [ ] Operator is running and healthy

---

## Advanced Topics

### Expression Caching

The CEL evaluator caches compiled expressions for performance:

```go
// First evaluation: compiles and caches
result1 := evaluator.EvaluateExpression("rule1", pod1)

// Subsequent evaluations: uses cached program
result2 := evaluator.EvaluateExpression("rule1", pod2)  // Fast!
```

### Thread Safety

The evaluator is thread-safe and can handle concurrent pod evaluations:

```go
// Multiple goroutines can safely evaluate simultaneously
go evaluator.EvaluateRules(pod1, celPlugin)
go evaluator.EvaluateRules(pod2, celPlugin)
go evaluator.EvaluateRules(pod3, celPlugin)
```

### Pod to Map Conversion

Pods are converted to a CEL-compatible map structure:

```go
{
  "metadata": {
    "name": "my-pod",
    "namespace": "default",
    "labels": {"app": "web"},
    "annotations": {"version": "1.0"}
  },
  "spec": {
    "nodeName": "node-1"
  }
}
```

---

## Testing

### Unit Tests

The feature includes comprehensive test coverage:

- **140+ test cases** across 6 test files
- Expression compilation and evaluation
- Architecture validation
- Concurrency testing
- Real-world scenarios
- Integration tests

Run tests:
```bash
# All CEL tests
go test -v ./api/common/plugins ./internal/controller/podplacement -run "CEL"

# Specific test categories
go test -v ./internal/controller/podplacement -run "ExpressionEdgeCases"
go test -v ./api/common/plugins -run "ArchitectureListEdgeCases"
```

See [cel-test-coverage.md](cel-test-coverage.md) for complete test documentation.

---

## Migration Guide

### From Static Node Selectors

**Before:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  nodeSelector:
    kubernetes.io/arch: amd64
```

**After:**
```yaml
# PodPlacementConfig
apiVersion: multiarch.openshift.io/v1beta1
kind: PodPlacementConfig
metadata:
  name: dynamic-placement
  namespace: default
spec:
  plugins:
    celArchitecturePlacement:
      enabled: true
      fallbackArchitectures: [amd64]
      rules:
        - name: production-apps
          expression: 'self.metadata.labels["env"] == "prod"'
          architectures: [amd64]

---
# Pod (no nodeSelector needed)
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    env: prod
spec:
  containers:
    - name: app
      image: myapp:latest
```

---

## Related Documentation

- [Test Coverage Report](cel-test-coverage.md)
- [Multiarch Tuning Operator README](../README.md)
- [CEL Language Specification](https://github.com/google/cel-spec)
- [Kubernetes Node Affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/)

---

## Support

For issues, questions, or contributions:

- **GitHub Issues**: [openshift/multiarch-tuning-operator](https://github.com/openshift/multiarch-tuning-operator/issues)
- **Documentation**: [docs/](../docs/)
- **Slack**: OpenShift community channels

---
