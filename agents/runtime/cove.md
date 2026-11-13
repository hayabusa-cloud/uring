# Context Checks with cove

## Use When

Requirements or context can change between submission and completion.

## Owns

`cove` carries context values, checks requirements and context transitions, and reports diagnostics. The caller chooses requirements, checks them at use, and handles rejection.

<a id="operational-contract"></a>

## Inputs and Preconditions

Carry copied completion facts and independently owned resources. `Need`/`NeedExpr` check requirements; `RuleExpr` reports diagnostics. A context-carrying suspension performs no automatic check.

Check route generation, buffer epoch, FD liveness, protocol phase, and shutdown state when used. Cache only facts preserved under the selected `Preorder` relation between contexts. Historical registration does not establish current validity; capability evidence retains its ring/environment scope.

## Outputs and Ownership

`Resume`/`ResumeWith` consume the current suspension, preserving or mapping context; retain the returned suspension view. They do not recheck requirements. Check current context before resuming with copied completion facts.

A failed guard leaves unconsumed suspensions and I/O caller-owned. It is neither kernel errno nor an automatic retry/close decision. Revalidate mutable generations after suspension.

## Strict Verification Pattern

When the property requires more than a local guard:

1. Copy completion facts and identify the submission and current contexts.
2. Check requirements with `NeedExpr` or `RuleExpr`.
3. Establish the selected `Preorder`/`Transition` through `CheckTransition` or forcing evidence.
4. Use `StepWithIndex`/`StepExprWithIndex` and `ResumeTo` for checked movement.
5. Use `Relate` and `CheckCompletedRelation` when acceptance requires a completed finite trace.

`TotalWorlds` does not establish freshness. Check transition and fuel before `ResumeTo` when policy needs an ordinary error; contract violations panic. An unfinished trace is not a completed proof.

## Cost Obligations

Count context copies, captures, diagnostics, and dynamic checks in both implementations. A formal guard does not establish erasure of its runtime counterpart.

## Sources

Read `code.hybscloud.com/cove`, especially `step.go`, `req_expr.go`, `rule_expr.go`, and `kripke.go`. [Public API](https://pkg.go.dev/code.hybscloud.com/cove) · [Revision requirements](../verify.md#read-sources)
