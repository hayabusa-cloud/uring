# Protocol Endpoints with sess

## Use When

Use an endpoint for local sends, receives, choices and close. Use `sess.Loop` or `sess.ExprLoop` for protocol recursion. The caller determines which kernel observations permit each action. [Protocol loops](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/rec.go#L11-L30)

## Owns

`sess` owns endpoint operations and bounded local SPSC transport. Network I/O, parsing, retries, timeouts, cancellation, and service lifetime remain caller-owned.

<a id="operational-contract"></a>

## Inputs and Preconditions

Construct compatible peers with one goroutine owning each endpoint. In `sess v0.2.0` each direction has four data slots and a separate four-slot choice queue. [Queue capacity](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/session.go#L14-L25) · [Endpoint pair](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/session.go#L105-L119)

Classify kernel stream events before choosing an endpoint action. Session transport reports success or `iox.ErrWouldBlock`. [Transport outcomes](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/session.go#L54-L69)

## Choose a Driver

| Driver | Who makes progress |
|--------|--------------------|
| `Step` and `Advance` | Use `sess.Step` for an `Expr` role or `kont.Step` for a `Cont` role. When either yields a suspension, the owner can try one action with `sess.Advance` and return to driving the CQ or the peer. [Stepping a role](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/step.go#L11-L26) |
| `Exec` / `ExecExpr` | Drive one endpoint and wait with backoff when blocked. The peer must be able to run independently. [Single-endpoint drivers](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/exec.go#L11-L25) |
| `Run` / `RunExpr` | Create a pair, drive both protocols on the calling goroutine and return the two results. [Paired drivers](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/run.go#L12-L31) |
| `RunError` / `RunErrorExpr` | Return two local `Either` values and a `*E` for the first uncaught `Throw`. Inspect that pointer first. If set, the peer's result may still be unresolved. [Paired error handling](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/error.go#L65-L86) |

## Outputs and Ownership

`Step` returns a nil suspension on completion. The endpoint owner serializes `Advance` and `Discard` on a live suspension. A successful send enqueues its value before resuming the continuation. The peer can receive it before `Advance` returns. Continue with the returned successor, if any. [Step and advance](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/step.go#L11-L38)

On `iox.ErrWouldBlock`, `Advance` returns the same suspension. Keep it and its payload. Give the peer a turn or let it run independently. Its `Recv` frees a data slot for `Send`. Its `Offer` frees a choice slot for `SelectL` or `SelectR`. Recheck any publication grant before retrying. [Choice operations](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/op.go#L89-L131) · [Write-receipt example](../recipes/protocol.md#deliver-a-write-receipt)

`sess.Send` enqueues a slice by value. A peer receiving `[]byte` shares the backing bytes. With `[][]byte` it also shares the outer array of slice headers. Keep borrowed storage intact through the last consumer's use. [Send and receive](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/op.go#L11-L50) · [Bundle handoff](../recipes/buffers.md#hand-off-a-receive-bundle)

`sess.Close{}` signals protocol termination by incrementing the pair's shared close counter. The caller enforces the protocol after close and retires descriptors, requests, buffers and the ring through their own release conditions. [Close operation](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/op.go#L53-L66) · [I/O release conditions](../recipes/lifecycle.md#release-each-owner)

## Cost Obligations

Account for queue storage, endpoint construction, pending values, captured frames and retry frequency.

## Sources

Read `code.hybscloud.com/sess`, especially `session.go`, `op.go`, `step.go`, `exec.go`, `run.go`, `error.go`, `fused_expr.go`, and `rec.go`. [Public API](https://pkg.go.dev/code.hybscloud.com/sess) · [Revision requirements](../verify.md#read-sources)
