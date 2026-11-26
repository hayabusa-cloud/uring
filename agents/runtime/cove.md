# Context Checks with cove

## Use When

Use `cove` before a native submission, a resumption or an enqueue whose permission can change. Keep the checked condition valid through that action.

## Owns

`cove` carries context values, checks requirements and context transitions, and reports diagnostics. The caller chooses requirements, checks them at use, and handles rejection.

<a id="operational-contract"></a>

## Inputs and Preconditions

Carry copied completion facts and independently owned resources. Use `Need` for a predicate or `NeedExpr` for a requirement expression.

Check route generation, buffer epoch, FD liveness, protocol phase, and shutdown state when used. Cache only facts preserved under the selected `Preorder` relation between contexts. Historical registration does not establish current validity; capability evidence retains its ring/environment scope.

## Guard a Send

A pending write keeps its original `takt.RouteID` and unsent suffix. The serialized ring owner reads the current route for each offer. On route retirement it clears the binding or installs a new generation, even if the numeric descriptor is reused. It processes permission changes and offers under that same ownership. [Route identity](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/subscription_backend.go#L12-L42)

```go
type sendRoute struct {
	id      takt.RouteID
	fd      iofd.FD
	granted bool
}

func (r sendRoute) offer(
	ring *uring.Uring, original takt.RouteID, request uring.SQEContext, suffix []byte,
) (attempted bool, err error) {
	if !cove.Need(r, func(current sendRoute) bool {
		return !original.IsZero() && current.id == original && current.granted
	}) {
		return false, nil
	}
	return true, ring.SendFD(request, r.fd, suffix)
}
```

A refused guard returns `(false, nil)`. If the grant is revoked, retain the suffix for the original route. If that route has been replaced, end the old write under caller policy. A full SQ can return `(true, iox.ErrWouldBlock)`. Drive the ring, then check a fresh route snapshot before retrying. Handle other submission errors separately. [Requirement check](https://github.com/hayabusa-cloud/cove/blob/v0.2.0/req.go#L14-L21)

For this ordinary stream send, enable `NotifySucceed` and leave `IOSQE_CQE_SKIP_SUCCESS` and `IOSQE_FIXED_FILE` clear. `(true, nil)` queues the request. The caller keeps its process fd open and its payload rooted, address-stable and immutable through the matching CQE. Match the submitted request identity, advance the offset by its positive byte count and check the next offer again. A revocation after admission still leaves that CQE to reap. [Request identity](../boundary/sqe-cqe.md#request-identity) · [Storage lifetime](../boundary/resources.md#retain-before-submit)

## Outputs and Ownership

`CheckSuspension(ctx, susp, req)` tests the context even when `susp` is nil. Check that an action is pending before dispatch. Refusal returns a zero view and leaves the original suspension and outstanding I/O with the caller. On success, every copy of the view shares the same one-shot handle. [Guard contract](https://github.com/hayabusa-cloud/cove/blob/v0.2.0/step.go#L15-L41)

`ObserveSuspension` pairs a context with a suspension. After a wait, recheck mutable requirements in the current context and keep them valid through dispatch. Keep the successor view after resuming. `Resume` preserves the context and `ResumeWith` maps it after the continuation runs. [Receipt delivery](../recipes/protocol.md#deliver-a-write-receipt)

## Strict Verification Pattern

1. Copy the matched completion facts and identify the submission and current contexts.
2. Check permission in the current context. For a named `RuleExpr`, `CheckRuleExpr(ctx, rule)` returns a report with `OK()` and `FailedRule()`. [Named rules](https://github.com/hayabusa-cloud/cove/blob/v0.2.0/rule_expr.go#L7-L35)
3. `CheckTransition` tests one edge of the chosen preorder. An epoch can increase while a grant is revoked. `TotalWorlds` accepts every edge. Use `CheckForcingTransition` for a requirement at both endpoints. If an intervening revocation matters, track it as an event. [Transition checks](https://github.com/hayabusa-cloud/cove/blob/v0.2.0/kripke.go#L61-L125) · [Event history with tempo](tempo.md#track-a-write-receipt)
4. Start with `StepWithIndex` or `StepExprWithIndex`. `ResumeTo` requires a pending suspension, an allowed edge and positive fuel. Check these first when refusal should return normally. A successful call spends one step. [Indexed resumption](https://github.com/hayabusa-cloud/cove/blob/v0.2.0/kripke.go#L305-L323)
5. Define the result predicate with `Relate`. `CheckCompletedRelation` returns false on a pending view. On completion it tests the result at the final context and remaining index. [Completion check](https://github.com/hayabusa-cloud/cove/blob/v0.2.0/kripke.go#L338-L349)

## Cost Obligations

Count context copies, captures, diagnostics, and dynamic checks in both implementations. A formal guard does not establish erasure of its runtime counterpart.

## Sources

Read `code.hybscloud.com/cove`, especially `step.go`, `req_expr.go`, `rule_expr.go`, and `kripke.go`. [Public API](https://pkg.go.dev/code.hybscloud.com/cove) · [Revision requirements](../verify.md#read-sources)
