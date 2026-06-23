# OPENSHIFTP-636 Phase 5: Deprecated Code Removal Checklist

## Overview

This document provides a detailed checklist for removing deprecated CEL reconciler code after one full release cycle of observation.

## Prerequisites

Before proceeding with Phase 5 removal, verify:

1. ✅ **Metric Verification**: `deprecated_cel_reconcile_path_total` has been **zero** for entire release cycle
2. ✅ **Time Elapsed**: At least **one full release cycle** (typically 3-6 months) has passed since Phase 4 deployment
3. ✅ **No Active Issues**: No open bugs related to CEL processing in webhook
4. ✅ **Feature Gate Stable**: `CEL_WEBHOOK_NO_GATE=true` has been default for at least one release

## Verification Commands

```bash
# 1. Check deprecated path metric (must be zero)
kubectl port-forward -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator 8080:8080
curl http://localhost:8080/metrics | grep deprecated_cel_reconcile_path_total
# Expected: deprecated_cel_reconcile_path_total 0

# 2. Check CEL webhook metrics (should show activity)
curl http://localhost:8080/metrics | grep cel_processed_pods_webhook
# Expected: cel_processed_pods_webhook > 0

# 3. Verify no CEL pods in reconciler queue
kubectl get pods --all-namespaces -l multiarch.openshift.io/scheduling-gate=gated | \
  grep -v "multiarch.openshift.io/cel-processed=processed"
# Expected: Empty or only non-CEL pods

# 4. Check feature gate status
kubectl get deployment -n openshift-multiarch-tuning-operator multiarch-tuning-operator -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="CEL_WEBHOOK_NO_GATE")].value}'
# Expected: true
```

## Code Removal Steps

### Step 1: Remove CEL Evaluation from applyMatchingPPCs

**File**: `internal/controller/podplacement/pod_reconciler.go`

**Current Code** (lines ~356-373):
```go
// Phase 4: DEPRECATED - CEL evaluation in reconciler
// This code path is deprecated and will be removed in a future release.
// CEL evaluation now happens in the webhook (Phase 1-3).
// This code remains only for backward compatibility with pods that were gated before the webhook changes.
celApplied := false
for _, ppc := range matchingPPCs {
    if r.applyCELArchitecturePlacement(ctx, ppc, pod) {
        log.Info("[DEPRECATED] CEL applied in reconciler - this code path is deprecated",
            "PodPlacementConfig", ppc.Name,
            "pod", pod.Name,
            "message", "CEL evaluation should happen in webhook, not reconciler")
        metrics.DeprecatedCELReconcilePathTotal.Inc()
        celApplied = true
        // CEL plugin was applied, it takes precedence over image-based detection
        // Continue to allow NodeAffinityScoring to run (coexistence per enhancement)
        break
    }
}
```

**New Code**:
```go
// CEL evaluation removed - now handled in webhook only
// NodeAffinityScoring continues to work in reconciler for non-CEL pods
```

**Changes**:
- Remove entire CEL evaluation loop
- Remove `celApplied` variable
- Update function signature to remove `bool` return value (if only used for CEL)

### Step 2: Remove applyCELArchitecturePlacement Function

**File**: `internal/controller/podplacement/cel_integration.go`

**Function to Remove**:
```go
func (r *PodReconciler) applyCELArchitecturePlacement(ctx context.Context, ppc multiarchv1beta1.PodPlacementConfig, pod *Pod) bool
```

**Verification**:
```bash
# Search for any remaining calls to this function
grep -r "applyCELArchitecturePlacement" internal/controller/podplacement/
# Should only find definition, no calls
```

### Step 3: Evaluate evaluateCELArchitecturePlacement Function

**File**: `internal/controller/podplacement/cel_evaluator.go`

**Decision Point**: This function is **REUSED by webhook**, so:
- ✅ **KEEP** if webhook uses it
- ❌ **REMOVE** only if webhook has its own implementation

**Verification**:
```bash
# Check if webhook uses this function
grep -r "evaluateCELArchitecturePlacement" internal/controller/podplacement/scheduling_gate_mutating_webhook.go
# If found: KEEP the function
# If not found: REMOVE the function
```

**Current Status**: **KEEP** - Webhook reuses this function (line 131 in webhook)

### Step 4: Remove Deprecated Metric

**File**: `internal/controller/podplacement/metrics/controller.go`

**Metric to Remove**:
```go
DeprecatedCELReconcilePathTotal = prometheus.NewCounter(
    prometheus.CounterOpts{
        Name: "deprecated_cel_reconcile_path_total",
        Help: "Total number of times the deprecated CEL reconciler path was used",
    },
)
```

**Also Remove**:
- Metric registration in `init()` function
- Any references to this metric in code

**Verification**:
```bash
# Search for metric usage
grep -r "DeprecatedCELReconcilePathTotal" internal/
# Should find no references after removal
```

### Step 5: Update processPod Function

**File**: `internal/controller/podplacement/pod_reconciler.go`

**Current Code** (lines ~260-295):
```go
// Skip preferred affinity processing if the user has already configured architecture-related preferred affinity
// or if the reconcile loop has already applied the PPCs/CPPC (e.g., due to a retry or re-reconciliation)
celApplied := false
if !pod.isPreferredAffinityConfiguredForArchitecture() {
    log.Info("processPod: Applying matching PPCs", "pod", pod.Name, "ppcCount", len(matchingPPCs))
    celApplied = r.applyMatchingPPCs(ctx, matchingPPCs, pod)
    log.Info("processPod: CEL applied status", "celApplied", celApplied, "pod", pod.Name)

    if cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
        log.Info("processPod: Applying CPPC NodeAffinityScoring", "pod", pod.Name)
        pod.SetPreferredArchNodeAffinity(cppc.Spec.Plugins.NodeAffinityScoring, multiarchv1beta1.ClusterPodPlacementConfigKind)
    }
} else {
    log.Info("processPod: Pod already has architecture-related preferred affinity - skipping PPC/CPPC processing", "pod", pod.Name)
    // Track that configs were skipped due to user-defined preferences
    r.trackSkippedMatchingConfigs(ctx, pod, cppc, matchingPPCs)
}

// "celArchitecturePlacement takes precedence over image-based detection"
// When CEL plugin successfully applies, return immediately without executing:
// - image-based architecture detection
// - fallback architecture logic
// - default architecture append logic
// This ensures ONLY the matched rule architectures are applied as per enhancement doc:
// "existing architecture constraints are removed and replaced"
if celApplied {
    log.Info("processPod: CEL architecture placement applied - skipping image-based detection", "pod", pod.Name)
    // If no preferred node affinity was set by any config, log and publish an event
    if pod.Labels[utils.PreferredNodeAffinityLabel] == utils.LabelValueNotSet {
        pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareNodeAffinitySet,
            ArchitecturePreferredPredicateSkippedMsg)
        log.Info("processPod: No preferred node affinity was set", "pod", pod.Name)
    }
    log.Info("processPod: Removing scheduling gate after CEL application", "pod", pod.Name)
    pod.RemoveSchedulingGate()
    return
}
```

**New Code**:
```go
// Apply NodeAffinityScoring from PPCs/CPPC if not already configured
if !pod.isPreferredAffinityConfiguredForArchitecture() {
    log.Info("processPod: Applying matching PPCs", "pod", pod.Name, "ppcCount", len(matchingPPCs))
    r.applyMatchingPPCs(ctx, matchingPPCs, pod)

    if cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
        log.Info("processPod: Applying CPPC NodeAffinityScoring", "pod", pod.Name)
        pod.SetPreferredArchNodeAffinity(cppc.Spec.Plugins.NodeAffinityScoring, multiarchv1beta1.ClusterPodPlacementConfigKind)
    }
} else {
    log.Info("processPod: Pod already has architecture-related preferred affinity - skipping PPC/CPPC processing", "pod", pod.Name)
    r.trackSkippedMatchingConfigs(ctx, pod, cppc, matchingPPCs)
}

// Continue with image-based architecture detection for non-CEL pods
// CEL pods were already processed in webhook and use fast-path
```

**Changes**:
- Remove `celApplied` variable and logic
- Remove early return for CEL pods (fast-path handles this)
- Simplify to just NodeAffinityScoring + image-based detection

### Step 6: Update Tests

**Files to Update**:
- `internal/controller/podplacement/pod_reconciler_test.go`
- `internal/controller/podplacement/cel_integration_test.go`
- Any other test files that test reconciler CEL behavior

**Changes**:
1. Remove tests for `applyCELArchitecturePlacement` in reconciler
2. Keep tests for `evaluateCELArchitecturePlacement` (used by webhook)
3. Update integration tests to verify CEL only happens in webhook
4. Add tests for fast-path behavior

**Example Test to Remove**:
```go
func TestReconciler_ApplyCELArchitecturePlacement(t *testing.T) {
    // This test is no longer relevant - CEL happens in webhook
}
```

**Example Test to Keep**:
```go
func TestWebhook_ApplyCELArchitecturePlacement(t *testing.T) {
    // This test is still relevant - CEL happens in webhook
}
```

### Step 7: Update Documentation

**Files to Update**:
- `README.md`
- `docs/enhancements/MTO-0001.md`
- Any architecture diagrams

**Changes**:
- Remove references to reconciler CEL evaluation
- Update flow diagrams to show webhook-only CEL
- Add note about Phase 5 completion date

## Testing After Removal

### Unit Tests
```bash
# Run all unit tests
make test

# Verify no test failures
echo $?  # Should be 0
```

### Integration Tests
```bash
# Deploy to test cluster
make deploy

# Test CEL pod creation
kubectl apply -f test/manifests/cel-test-pod.yaml

# Verify CEL processed in webhook only
kubectl get pod cel-test-pod -o jsonpath='{.metadata.labels.multiarch\.openshift\.io/cel-processed}'
# Expected: processed

# Verify fast-path in reconciler
kubectl logs -n openshift-multiarch-tuning-operator deployment/multiarch-tuning-operator | \
  grep "FAST-PATH - CEL already processed in webhook"
# Should see log entries

# Verify no deprecated path usage
curl http://localhost:8080/metrics | grep deprecated_cel_reconcile_path_total
# Metric should not exist (removed)
```

### E2E Tests
```bash
# Run full E2E test suite
make deploy-and-e2e

# Verify all tests pass
echo $?  # Should be 0
```

## Rollback Plan

If issues are discovered after Phase 5 removal:

### Option 1: Revert Commit
```bash
# Identify removal commit
git log --oneline | grep "Phase 5"

# Revert the commit
git revert <commit-hash>

# Deploy reverted version
make deploy
```

### Option 2: Restore from Backup
```bash
# Checkout previous release branch
git checkout release-v1.x.x

# Deploy previous version
make deploy
```

## Post-Removal Verification

After Phase 5 removal, verify:

1. ✅ **No Compilation Errors**: Code compiles successfully
2. ✅ **All Tests Pass**: Unit, integration, and E2E tests pass
3. ✅ **Metrics Clean**: No deprecated metrics in output
4. ✅ **Webhook Works**: CEL evaluation works in webhook
5. ✅ **Fast-Path Works**: CEL pods use fast-path in reconciler
6. ✅ **Image-Based Works**: Non-CEL pods use image-based detection

## Timeline

| Milestone | Date | Status |
|-----------|------|--------|
| Phase 4 Deployed | TBD | ✅ Complete |
| Observation Period Start | TBD | 🔄 In Progress |
| One Release Cycle Complete | TBD | ⏳ Pending |
| Metric Verification | TBD | ⏳ Pending |
| Phase 5 Removal | TBD | ⏳ Pending |
| Post-Removal Verification | TBD | ⏳ Pending |

## Sign-Off

Before proceeding with Phase 5 removal, obtain sign-off from:

- [ ] Engineering Lead
- [ ] QE Lead
- [ ] Product Manager
- [ ] SRE Team

## References

- [OPENSHIFTP-636 Migration Guide](./OPENSHIFTP-636-migration-guide.md)
- [Phase 4 Implementation PR](TBD)
- [Phase 5 Removal PR](TBD)