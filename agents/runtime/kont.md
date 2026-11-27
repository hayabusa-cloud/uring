# One-Shot Suspension with kont

## Use When

A computation stops at an effect and needs one resumption. Repeated CQEs require a stream route.

## Owns

`kont` owns affine suspensions and continuation frames. The caller owns requests, payloads, contexts, and retirement.

<a id="operational-contract"></a>

## Inputs and Preconditions

Use `Step` for a `Cont` or `StepExpr` for an `Expr`. A nil suspension marks completion, even for a nil result. Share the returned `*kont.Suspension` pointer. Copying the pending struct lets both copies claim the same continuation. Resume with the matched operation result.

For deferred resumption, copy the fields the continuation needs or keep the borrowed SQE target live and its fields stable through decoding. Retain referenced payloads independently. [Completion retention](../boundary/sqe-cqe.md#completion-retention)

## Outputs and Ownership

| Operation | Effect |
|-----------|--------|
| `Resume` | Returns the result or next suspension. Panics if the handle was already claimed. |
| `TryResume` | A winning claim returns `(value, next, true)`. A lost claim returns `(zero, nil, false)`. |
| `Discard` | Releases a pending continuation without running it. A call on a claimed handle has no effect. |

The combined call count for these methods must fit in a `uintptr` on each handle. If another resume wins, its callback may still be reading the buffer when `TryResume` loses or `Discard` returns. Reuse the buffer after its last callback use and its kernel release condition. The caller owns cancellation and draining. [Suspension contract](https://github.com/hayabusa-cloud/kont/blob/v0.2.0/step.go#L13-L86) · [One-shot recipe](../recipes/oneshot.md)

## Cost Obligations

Inspect captures, frames, suspension allocation, pooling, first use, and cleanup. Prepare dispatch state before hot loops when required. `Reify`/`Reflect` correspondence establishes meaning, not equal cost.

## Sources

Read `code.hybscloud.com/kont`, especially `step.go`, `frame.go`, `pool.go`, `bridge.go`, `effect.go`, and `resource.go`. [Public API](https://pkg.go.dev/code.hybscloud.com/kont) · [Revision requirements](../verify.md#read-sources)
