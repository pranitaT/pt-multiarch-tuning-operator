# OPENSHIFTP-636: Minimal Design Review and Implementation Proposal

## Problem Statement

**Issue**: Kubernetes API rejects pod updates that modify `spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms` after pod creation:

```
Error: only additions are allowed (no mutations or deletions)
```

**Root Cause**: The reconciler evaluates CEL rules and attempts to narrow architecture constraints after pod creation:
- Pod created with: `[amd64, ppc64le, s390x]`
- Reconciler evaluates CEL and tries to change to: `[ppc64le]`
- Kubernetes rejects the mutation

**Logs Prove**:
```
[DIAGNOSTIC] BEFORE applyArchitectureNodeAffinity: values=[amd64,ppc64le,s390x]
[DIAGNOSTIC] AFTER applyArchitectureNodeAffinity: values=[ppc64le]
[DIAGNOSTIC] BEFORE r.Update(): values=[ppc64le]
[UPDATE] FAILED: only additions are allowed (no mutations or deletions)
```

## Proposed Minimal Solution

Move CEL evaluation from reconciler (post-creation) to webhook (pre-creation):

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
Kubernetes REJECTS ❌
```

### After (Fixed)
```
Pod CREATE
  ↓
Webhook evaluates CEL
  ↓
Webhook applies [ppc64le]
  ↓
Webhook adds scheduling gate
  ↓
Pod persisted with [ppc64le] ✅
  ↓
Reconciler removes gate
```

## Minimal Implementation

### 1. Webhook Changes (scheduling_gate_mutating_webhook.go)

**Add CEL evaluation before adding scheduling gate**:

```go
func (a *PodSchedulingGateMutatingWebHook) Handle(ctx context.Context, req admission.Request) admission.Response {
    // ... existing code ...
    
    // NEW: Evaluate CEL rules in webhook before pod persistence
    // This avoids Kubernetes immutability violations on NodeSelectorTerms
    a.applyCELInWebhook(ctx, pod, matchingPPCs)
    
    // Existing: Add scheduling gate
    pod.ensureSchedulingGate()
    
    // ... existing code ...
}

// NEW: Minimal CEL evaluation in webhook
func (a *PodSchedulingGateMutatingWebHook) applyCELInWebhook(ctx context.Context, pod *Pod, ppcs []PodPlacementConfig) {
    for _, ppc := range ppcs {
        if !ppc.PluginsEnabled(common.CelArchitecturePlacementPluginName) {
            continue
        }
        
        // Reuse existing evaluator
        result, err := evaluateCELArchitecturePlacement(
            ppc.Spec.Plugins.CelArchitecturePlacement.Rules,
            ppc.Spec.Plugins.CelArchitecturePlacement.FallbackArchitectures,
            pod.PodObject(),
        )
        
        if err != nil {
            continue
        }
        
        // Reuse existing constraint application
        applyArchitectureConstraints(pod.PodObject(), result.architectures)
        return
    }
}
```

### 2. Reconciler Changes (pod_reconciler.go)

**Keep reconciler CEL code for backward compatibility**:

```go
func (r *PodReconciler) applyMatchingPPCs(...) bool {
    // Existing CEL evaluation code remains unchanged
    // This handles pods that were gated before webhook changes
    celApplied := false
    for _, ppc := range matchingPPCs {
        if r.applyCELArchitecturePlacement(ctx, ppc, pod) {
            celApplied = true
            break
        }
    }
    
    // Existing NodeAffinityScoring code remains unchanged
    // ...
    
    return celApplied
}
```

**No changes needed** - reconciler continues to work for:
- Pods gated before webhook deployment
- Non-CEL pods (image-based detection)
- NodeAffinityScoring (preferred affinity)

## What This Minimal Fix Does NOT Include

❌ **No NodeAffinityScoring changes** - Preferred affinity can be modified after creation, so it stays in reconciler
❌ **No feature gates** - CEL always runs in webhook once deployed
❌ **No CEL labels** - Not needed for minimal fix
❌ **No fast-path** - Reconciler handles all pods the same way
❌ **No immediate scheduling** - All pods get scheduling gate
❌ **No excessive metrics** - Only basic observability
❌ **No phase-based logic** - Single deployment, no gradual rollout

## Files Modified (Minimal)

### 1. scheduling_gate_mutating_webhook.go
- Add `applyCELInWebhook()` function (~30 lines)
- Call it in `Handle()` before adding gate (~1 line)

### 2. No reconciler changes needed
- Existing CEL code provides backward compatibility
- Works for pods gated before webhook deployment

### 3. Optional: Add basic metric
- `cel_evaluated_in_webhook_total` - Track webhook CEL usage

## Testing

### Unit Tests
- Test CEL evaluation in webhook
- Test backward compatibility in reconciler

### Integration Tests
- Create pod with CEL rule
- Verify architecture constraints applied before persistence
- Verify no immutability errors

### E2E Tests
- Deploy operator
- Create pods with various CEL rules
- Verify correct scheduling

## Rollout Strategy

### Single Deployment
1. Deploy operator with webhook changes
2. New pods: CEL evaluated in webhook ✅
3. Old gated pods: CEL evaluated in reconciler (backward compatible) ✅
4. No feature gates or gradual rollout needed

### Rollback
If issues occur:
```bash
kubectl rollout undo deployment/multiarch-tuning-operator
```

## Why This is Minimal

1. **Single Root Cause**: Only fixes NodeSelectorTerms immutability
2. **Reuses Existing Code**: `evaluateCELArchitecturePlacement()` and `applyArchitectureConstraints()`
3. **No Behavior Changes**: NodeAffinityScoring, image-based detection unchanged
4. **No Complex Rollout**: Single deployment, no feature gates
5. **Backward Compatible**: Reconciler handles old gated pods
6. **Small Code Change**: ~30 lines in webhook, 0 lines in reconciler

## Comparison: Minimal vs Full Implementation

| Aspect | Minimal Fix | Full Implementation |
|--------|-------------|---------------------|
| **Lines of Code** | ~30 | ~1,200 |
| **Files Modified** | 1 | 10 |
| **New Metrics** | 1 | 10 |
| **Feature Gates** | 0 | 1 |
| **Rollout Phases** | 1 | 6 |
| **Observation Periods** | 0 | 2 |
| **NodeAffinityScoring Changes** | 0 | Moved to webhook |
| **Fast-Path Logic** | 0 | Added |
| **Immediate Scheduling** | No | Yes (with gate) |
| **Complexity** | Low | High |

## Recommendation

**Use the minimal fix** because:
1. ✅ Solves OPENSHIFTP-636 completely
2. ✅ No unnecessary changes to working code
3. ✅ Simple to understand and maintain
4. ✅ Easy to rollback if needed
5. ✅ No complex rollout procedures

The full implementation adds optimizations (fast-path, immediate scheduling, NodeAffinityScoring in webhook) that are **nice-to-have** but not required to fix the bug.

## Implementation Code (Minimal)

```go
// In scheduling_gate_mutating_webhook.go

// applyCELInWebhook evaluates CEL rules in the webhook before pod persistence.
// This avoids Kubernetes immutability violations on NodeSelectorTerms which
// cannot be modified after pod creation.
func (a *PodSchedulingGateMutatingWebHook) applyCELInWebhook(
    ctx context.Context,
    pod *Pod,
    matchingPPCs []multiarchv1beta1.PodPlacementConfig,
) {
    log := ctrllog.FromContext(ctx)
    
    // Sort by priority (highest first)
    sort.Slice(matchingPPCs, func(i, j int) bool {
        return matchingPPCs[i].Spec.Priority > matchingPPCs[j].Spec.Priority
    })
    
    // Find first PPC with CEL plugin enabled
    for _, ppc := range matchingPPCs {
        if !ppc.PluginsEnabled(common.CelArchitecturePlacementPluginName) {
            continue
        }
        
        celPlugin := ppc.Spec.Plugins.CelArchitecturePlacement
        if celPlugin == nil {
            continue
        }
        
        // Reuse existing CEL evaluator
        result, err := evaluateCELArchitecturePlacement(
            celPlugin.Rules,
            celPlugin.FallbackArchitectures,
            pod.PodObject(),
        )
        
        if err != nil {
            log.Error(err, "CEL evaluation failed in webhook", "ppc", ppc.Name)
            continue
        }
        
        // Reuse existing constraint application
        applyArchitectureConstraints(pod.PodObject(), result.architectures)
        
        log.Info("CEL evaluated in webhook",
            "pod", pod.Name,
            "ppc", ppc.Name,
            "architectures", result.architectures)
        
        return
    }
}

// In Handle() method, add before ensureSchedulingGate():
a.applyCELInWebhook(ctx, pod, matchingPPCs)
```

That's it. **30 lines of code** to fix OPENSHIFTP-636.