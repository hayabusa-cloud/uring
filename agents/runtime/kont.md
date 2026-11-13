# One-Shot Suspension with kont

## Use When

A computation stops at an effect and needs one resumption. Repeated CQEs require a stream route.

## Owns

`kont` owns affine suspensions and continuation frames. The caller owns requests, payloads, contexts, and retirement.

<a id="operational-contract"></a>

## Inputs and Preconditions

Choose `Cont` or `Expr`. `Step`/`StepExpr` returns a nil suspension on completion; inspect the suspension because the result may itself be nil. Resume only with the matching operation result.

Copy needed CQE/context facts before their targets expire; retain payload roots independently. Captures must own their state or contain durable copies. A borrowed `SQEView` cannot support deferred decoding.

## Outputs and Ownership

| Operation | Effect |
|-----------|--------|
| `Resume` | Consumes the suspension; retain its returned successor |
| `TryResume` | Reports whether it consumed the suspension |
| `Discard` | Abandons continuation state; I/O, payload, and cancellation obligations remain |

The [one-shot recipe](../recipes/oneshot.md) covers step, admission, matching, resumption, and cleanup. Resuming the same suspension for multiple stream events violates affinity.

## Cost Obligations

Inspect captures, frames, suspension allocation, pooling, first use, and cleanup. Prepare dispatch state before hot loops when required. `Reify`/`Reflect` correspondence establishes meaning, not equal cost.

## Sources

Read `code.hybscloud.com/kont`, especially `step.go`, `frame.go`, `pool.go`, `bridge.go`, `effect.go`, and `resource.go`. [Public API](https://pkg.go.dev/code.hybscloud.com/kont) · [Revision requirements](../verify.md#read-sources)
