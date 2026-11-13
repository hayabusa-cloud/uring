# Protocol Endpoints with sess

## Use When

Local actions follow a protocol: send/receive, choice, recursion, and close. A caller adapter determines which kernel observations permit each action.

## Owns

`sess` owns endpoint operations and bounded local SPSC transport. Network I/O, parsing, retries, timeouts, cancellation, and service lifetime remain caller-owned.

<a id="operational-contract"></a>

## Inputs and Preconditions

Construct compatible peers with one owner per endpoint. Local channel capacity is four; preserve SPSC ownership across goroutines.

`Step` exposes a suspension; `Advance` consumes it after successful dispatch. On full/empty transport, `iox.ErrWouldBlock` preserves the pending action. Drive the peer or wait under caller policy. `ErrMore` is outside this transport domain; classify kernel stream events before choosing endpoint actions.

## Outputs and Ownership

Retain the successor after `Advance`. Protocol `Close` leaves descriptor, request, buffer, and ring retirement independent.

| Driver | Scope |
|--------|-------|
| `Exec`/`ExecExpr` | One endpoint |
| `Run`/`RunExpr` | A pair; inspect the outer error before possibly unresolved peer results |

Error-aware forms carry protocol errors. Recursive `sess.Loop` differs from `takt.Loop`.

A [protocol adapter](../recipes/protocol.md) matches I/O, checks context, then advances the chosen action. Retain that action and its payload under backpressure; do not replay a consumed suspension or treat endpoint close as I/O drain.

## Cost Obligations

Count queue storage, endpoint construction, pending values, frames/captures, and wait cadence. Compatible types remove neither capacity nor scheduling/peer-service obligations and establish no arbitrary global progress guarantee.

## Sources

Read `code.hybscloud.com/sess`, especially `session.go`, `op.go`, `step.go`, `exec.go`, `run.go`, `error.go`, `fused_expr.go`, and `rec.go`. [Public API](https://pkg.go.dev/code.hybscloud.com/sess) · [Revision requirements](../verify.md#read-sources)
