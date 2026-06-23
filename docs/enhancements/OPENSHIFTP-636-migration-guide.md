# OPENSHIFTP-636: CEL Architecture Placement Migration Guide

## Overview

This document describes the migration of CEL architecture placement from the reconciler to the mutating admission webhook to resolve Kubernetes immutability constraints on `NodeSelectorTerms`.

## Problem Statement

**Issue**: Kubernetes API rejects pod updates that modify `spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms` after pod creation with error:
```
only additions are allowed (no mutations or deletions)
```

**Root Cause**: The reconciler attempted to narrow architecture constraints (e.g., `[amd64,ppc64le,s390x]` → `[ppc64le]`) after pod creation, which violates Kubernetes immutability rules.

**Solution**: Move CEL evaluation and architecture constraint application from reconciler (post-creation) to mutating webhook (pre-creation).

## Architecture Changes

### Before (Broken)
```
Pod CREATE
  ↓
Webhook adds scheduling gate
  ↓
Pod persisted with [amd64,ppc64le,s390x]
  ↓
Reconciler evaluates CEL
  ↓
Reconciler tries to narrow to [ppc64le]
  ↓
Kubernetes REJECTS update ❌
```

### After (Fixed)
```
Pod CREATE
  ↓
Webhook evaluates CEL
  ↓
Webhook applies [ppc64le]
  ↓
Webhook adds scheduling gate (optional)
  ↓
Pod persisted with [ppc64le] ✅
  ↓
Reconciler removes gate only
```

## Implementation Phases

### Phase 0: Metrics + Safety Infrastructure ✅
**Status**: Complete  
**Files Modified**:
- `pkg/featuregates/featuregates.go` (NEW)
- `pkg/featuregates/featuregates_test.go` (NEW)
- `pkg/utils/const.go`
- `internal/controller/podplacement/metrics/webhook.go`
- `internal/controller/podplacement/metrics/controller.go`

**Changes**:
- Added feature gate package with `CELWebhookNoGate` constant
- Added 10 new Prometheus metrics for observability
- Added `CELProcessedLabel` constant for pod labeling
- Added panic recovery infrastructure

### Phase 1: CEL Evaluation in Webhook ✅
**Status**: Complete  
**Files Modified**:
- `internal/controller/podplacement/scheduling_gate_mutating_webhook.go`

**Changes**:
- Added `withPanicRecovery()` wrapper function
- Added `applyCELArchitecturePlacementInWebhook()` function
- Integrated CEL evaluation into `Handle()` method
- **Backward Compatible**: Still adds scheduling gate for ALL pods

**Behavior**:
- CEL rules evaluated in webhook before pod persistence
- Architecture constraints applied immediately
- Scheduling gate still added (reconciler still processes all pods)
- Metrics track webhook CEL processing

### Phase 2: NodeAffinityScoring in Webhook ✅
**Status**: Complete  
**Files Modified**:
- `internal/controller/podplacement/scheduling_gate_mutating_webhook.go`

**Changes**:
- Added `applyNodeAffinityScoringInWebhook()` function
- Integrated into `Handle()` method after CEL evaluation
- **Backward Compatible**: Still adds scheduling gate for ALL pods

**Behavior**:
- Preferred affinity (NodeAffinityScoring) applied in webhook
- Coexists with CEL per enhancement document
- Scheduling gate still added for all pods

### Phase 3A: Add CEL Label ✅
**Status**: Complete  
**Files Modified**:
- `internal/controller/podplacement/scheduling_gate_mutating_webhook.go`

**Changes**:
- Added `multiarch.openshift.io/cel-processed=processed` label to CEL pods
- Incremented `CELImmediatePodsWH` metric
- **Backward Compatible**: Still adds scheduling gate for ALL pods

**Behavior**:
- CEL-processed pods labeled for identification
- Scheduling gate still added (no behavior change)
- Allows observation of CEL pod population

**Observation Period**: 1 week + 10,000 pods minimum

### Phase 3B: Remove Gate for CEL Pods ✅
**Status**: Complete  
**Files Modified**:
- `internal/controller/podplacement/scheduling_gate_mutating_webhook.go`

**Changes**:
- Added feature gate check: `CEL_WEBHOOK_NO_GATE=true`
- Skip scheduling gate for CEL pods when feature gate enabled
- Track fallback with `CELFallbackToGateWH` metric

**Behavior**:
- **Feature Gate Disabled** (default): CEL pods get scheduling gate (backward compatible)
- **Feature Gate Enabled**: CEL pods skip gate, schedule immediately
- Non-CEL pods always get scheduling gate

**Rollout**:
1. Deploy with feature gate disabled (default)
2. Observe metrics for 1 week
3. Enable feature gate: `CEL_WEBHOOK_NO_GATE=true`
4. Monitor for issues
5. If problems occur, disable feature gate to rollback

### Phase 4: Simplify Reconciler ✅
**Status**: Complete  
**Files Modified**:
- `internal/controller/podplacement/pod_reconciler.go`

**Changes**:
- Added fast-path for CEL-processed pods (check label, remove gate, return)
- Added deprecation warnings to reconciler CEL code path
- Incremented `DeprecatedCELReconcilePathTotal` metric for deprecated path
- Incremented `CELGateRemovalFailuresCtrl` metric for gate removal failures

**Behavior**:
- CEL-processed pods: Fast-path removes gate only (no CEL evaluation)
- Non-CEL pods: Full processing (image-based detection)
- Deprecated reconciler CEL path still works for backward compatibility

### Phase 5: Remove Deprecated Code 🔄
**Status**: Pending (after one release cycle)  
**Timeline**: Remove after observing `DeprecatedCELReconcilePathTotal` metric = 0 for one release

**Files to Modify**:
- `internal/controller/podplacement/pod_reconciler.go`
- `internal/controller/podplacement/cel_evaluator.go`
- `internal/controller/podplacement/cel_integration.go`

**Changes to Make**:
1. Remove CEL evaluation from `applyMatchingPPCs()` function
2. Remove `applyCELArchitecturePlacement()` function
3. Remove `evaluateCELArchitecturePlacement()` function (if not used by webhook)
4. Remove `DeprecatedCELReconcilePathTotal` metric
5. Update tests to remove reconciler CEL test cases

**Verification Before Removal**:
```bash
# Check metric shows zero usage
kubectl port-forward -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator 8080:8080
curl http://localhost:8080/metrics | grep deprecated_cel_reconcile_path_total
# Should show: deprecated_cel_reconcile_path_total 0
```

### Phase 6: Tests & Documentation ✅
**Status**: Complete  
**Deliverables**:
- This migration guide
- Deployment guide (see below)
- Rollback procedures (see below)

## Deployment Guide

### Prerequisites
- Kubernetes 1.24+ (scheduling gates support)
- Multiarch Tuning Operator v1.x.x+

### Step-by-Step Deployment

#### 1. Deploy Phase 0-4 (Backward Compatible)
```bash
# Deploy operator with all phases 0-4
kubectl apply -f deploy/

# Verify webhook is running
kubectl get pods -n openshift-multiarch-tuning-operator
kubectl logs -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator

# Check metrics
kubectl port-forward -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator 8080:8080
curl http://localhost:8080/metrics | grep cel_processed_pods_webhook
```

#### 2. Observe Phase 3A (1 week minimum)
```bash
# Monitor CEL pod population
kubectl get pods --all-namespaces -l multiarch.openshift.io/cel-processed=processed

# Check metrics
curl http://localhost:8080/metrics | grep -E "(cel_immediate_pods_webhook|cel_processed_pods_webhook)"

# Verify at least 10,000 CEL pods processed
# cel_processed_pods_webhook should be >= 10000
```

#### 3. Enable Phase 3B (Feature Gate)
```bash
# Enable feature gate
kubectl set env -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator CEL_WEBHOOK_NO_GATE=true

# Verify feature gate is active
kubectl logs -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator | grep "CEL_WEBHOOK_NO_GATE"

# Monitor for immediate scheduling
kubectl get pods --all-namespaces -l multiarch.openshift.io/cel-processed=processed -w

# Check fallback metric (should be zero)
curl http://localhost:8080/metrics | grep cel_fallback_to_gate_webhook
```

#### 4. Monitor Phase 4 (Reconciler Fast-Path)
```bash
# Check deprecated path usage (should decrease to zero)
curl http://localhost:8080/metrics | grep deprecated_cel_reconcile_path_total

# Monitor gate removal failures
curl http://localhost:8080/metrics | grep cel_gate_removal_failures_controller
```

#### 5. Plan Phase 5 (After One Release)
```bash
# After one full release cycle, verify deprecated path is unused
curl http://localhost:8080/metrics | grep deprecated_cel_reconcile_path_total
# Should show: deprecated_cel_reconcile_path_total 0

# If zero for entire release, proceed with code removal in next release
```

## Rollback Procedures

### Rollback Phase 3B (Disable Feature Gate)
```bash
# Disable feature gate to restore scheduling gate for CEL pods
kubectl set env -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator CEL_WEBHOOK_NO_GATE=false

# Verify rollback
kubectl logs -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator | grep "Adding scheduling gate for CEL pod"
```

### Rollback to Previous Version
```bash
# If critical issues occur, rollback to previous operator version
kubectl rollout undo -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator

# Verify rollback
kubectl rollout status -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator
```

## Monitoring & Alerts

### Key Metrics to Monitor

#### Webhook Metrics
- `cel_processed_pods_webhook`: Total CEL pods processed in webhook
- `cel_immediate_pods_webhook`: CEL pods that skipped scheduling gate
- `cel_skipped_pods_webhook`: Pods without CEL rules
- `cel_evaluation_errors_webhook`: CEL evaluation failures
- `cel_fallback_to_gate_webhook`: CEL pods that got gate (feature gate disabled)
- `cel_webhook_duration_seconds`: CEL evaluation latency

#### Controller Metrics
- `image_based_processed_pods_controller`: Image-based pods processed
- `deprecated_cel_reconcile_path_total`: Deprecated reconciler CEL usage
- `cel_gate_removal_failures_controller`: Gate removal failures for CEL pods

### Recommended Alerts

```yaml
# Alert if CEL evaluation errors spike
- alert: CELEvaluationErrorsHigh
  expr: rate(cel_evaluation_errors_webhook[5m]) > 0.1
  annotations:
    summary: "High CEL evaluation error rate in webhook"

# Alert if deprecated path is still used after migration
- alert: DeprecatedCELPathUsed
  expr: deprecated_cel_reconcile_path_total > 0
  for: 1h
  annotations:
    summary: "Deprecated CEL reconciler path still in use"

# Alert if gate removal fails frequently
- alert: CELGateRemovalFailuresHigh
  expr: rate(cel_gate_removal_failures_controller[5m]) > 0.05
  annotations:
    summary: "High CEL gate removal failure rate"
```

## Testing

### Unit Tests
```bash
# Run all unit tests
make test

# Run specific test suites
go test ./internal/controller/podplacement/... -v
go test ./pkg/featuregates/... -v
```

### E2E Tests
```bash
# Deploy test environment
make deploy-and-e2e

# Test CEL evaluation in webhook
kubectl apply -f test/manifests/cel-test-pod.yaml
kubectl get pod cel-test-pod -o jsonpath='{.metadata.labels.multiarch\.openshift\.io/cel-processed}'
# Should output: processed

# Test immediate scheduling (feature gate enabled)
kubectl set env -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator CEL_WEBHOOK_NO_GATE=true
kubectl apply -f test/manifests/cel-test-pod-2.yaml
kubectl get pod cel-test-pod-2 -o jsonpath='{.spec.schedulingGates}'
# Should output: [] (no gates)
```

## Troubleshooting

### Issue: CEL pods still getting scheduling gate
**Symptom**: `cel_fallback_to_gate_webhook` metric increasing  
**Cause**: Feature gate `CEL_WEBHOOK_NO_GATE` not enabled  
**Solution**:
```bash
kubectl set env -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator CEL_WEBHOOK_NO_GATE=true
```

### Issue: Deprecated reconciler path still used
**Symptom**: `deprecated_cel_reconcile_path_total` > 0  
**Cause**: Old pods gated before webhook changes  
**Solution**: Wait for old pods to be deleted naturally, or force recreation

### Issue: Gate removal failures
**Symptom**: `cel_gate_removal_failures_controller` increasing  
**Cause**: API server issues or pod conflicts  
**Solution**: Check controller logs for specific errors

## References

- [OPENSHIFTP-636 JIRA Issue](https://issues.redhat.com/browse/OPENSHIFTP-636)
- [Kubernetes Pod Scheduling Readiness](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/)
- [Kubernetes Admission Webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [MTO Enhancement: CEL Architecture Placement](docs/enhancements/MTO-0001.md)