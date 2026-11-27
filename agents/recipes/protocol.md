# Protocol Composition

Use a matched completion to choose the next protocol action. The caller owns payloads and pending actions across retries. [Runtime roles](../runtime/INDEX.md)

## Establish the Protocol

Declare compatible peer actions, payload ownership, choices, recursion, and termination. Give each endpoint and parser one owner. Select framing where byte streams need message boundaries. Keep network I/O, parsing, local transport, and service lifecycle distinct.

`sess` provides bounded local SPSC transport. Define which completion enables an endpoint action and which facts the caller passes to it.

## Drive One Action

1. Match the request and copy the context fields needed later. For a stream `ReceiveFD` into caller storage, decode the first `Res` bytes of the submitted range when the result is positive. A call with `WithOffset(4)` and `Res == 3` supplies `buf[4:7]`. For a positive short stream write, retain the unsent bytes and omit any Unix rights already queued with the prefix. [Receive shape](../boundary/encoding.md#receive-shape) · [Sending descriptors](../boundary/files.md#send-file-descriptors-on-a-unix-stream)
2. For [framed input](../boundary/protocols.md#stream-framing), keep one decoder per ordered stream. At each `framer.Decoder.Decode`, advance by `consumed` and handle the returned chunk even on `ErrMore`. Accumulate payload until `Chunk.End` for a whole-message endpoint. Copy borrowed bytes or retain their backing through the last use. [Decoder contract](https://github.com/hayabusa-cloud/framer/blob/v0.1.7/decoder.go#L59-L75)
3. Before each endpoint attempt, check the route, buffer ownership, descriptor, protocol phase and shutdown state that the action requires. Keep those conditions valid through dispatch.
4. On local `iox.ErrWouldBlock`, keep the same pending action, payload and unread input suffix. After peer progress, recheck the conditions and retry that action. A successful `sess.Advance` consumes the suspension. Keep any successor and continue with the unread bytes. [Session step](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/step.go#L18-L38)

When a protocol requires checked context transitions or a completed trace, the caller handles refused guards, invalid transitions, exhausted fuel and unfinished traces. [Strict verification pattern](../runtime/cove.md#strict-verification-pattern)

### Hand Off Received File Descriptors

After a Unix-socket `RecvMsg`, account for the fds in the returned control data before checking permission to deliver them. An in-process `sess.Send[[]int]` queues a slice of the existing fd numbers. If the protocol transfers ownership at enqueue, keep the slice intact and the fds open while the send is blocked. Recheck any delivery grant on retry. If delivery ends before enqueue, discard the pending send and close its fds. After enqueue, the peer owns the list and may use or close the fds before `sess.Advance` returns. [Received fds and truncation](../boundary/files.md#receive-file-descriptors-on-a-unix-socket) · [Endpoint visibility](../runtime/sess.md#outputs-and-ownership)

## Deliver a Write Receipt

Suppose command 7 writes `PING` to a stream socket. The caller may enqueue a receipt for a local peer while publication is granted and shutdown has not begun.

1. Submit the `kont` computation with `takt.Loop.SubmitExpr` (v0.3.0). The backend issues one ordinary `SendFD` at a time with `NotifySucceed` enabled and `IOSQE_CQE_SKIP_SUCCESS` clear. A matched result of 2 for `PI` leaves `NG` to send. A second 2 completes the write. Copy the receipt from the borrowed result before the next `Poll`, `Run` or `Drain`. [One-shot loop](../runtime/takt.md#completion-results)
2. For receipt `r`, obtain the pending send once with `_, pending := sess.Step[struct{}](kont.ExprPerform(sess.Send[receipt]{Value: r}))`. The receipt policy tracks the current grant on the left and shutdown on the right. One owner applies each event with `policy = policy.Step(event)`. Before each attempt, it calls `tryReceipt(ep, pending, policy.Observe())` with the current observation. [Receipt policy and worked retry](../runtime/tempo.md#track-a-write-receipt)

```go
func tryReceipt(
	ep *sess.Endpoint, pending *kont.Suspension[struct{}], current tempo.Pair[bool, bool],
) (next *kont.Suspension[struct{}], attempted bool, err error) {
	view, ok := cove.CheckSuspension(current, pending, func(p tempo.Pair[bool, bool]) bool {
		return p.Left && !p.Right
	})
	if !ok {
		return pending, false, nil
	}
	_, next, err = sess.Advance(ep, view.Extract())
	return next, true, err
}
```

| Outcome | Next action |
|---------|-------------|
| Guard refused | Keep the returned suspension and receipt. Wait for a policy change or discard the suspension if delivery ends. |
| Attempted, `iox.ErrWouldBlock` | Keep the same suspension. If this owner also drives the peer, give its receive a turn to free a slot. Recheck the grant before retrying. |
| Attempted, `nil` error | This send enqueued the receipt. The local peer may already have received it. |

The four socket bytes are accounted for in all three cases. Confirming that the remote application processed command 7 requires an acknowledgment from that application.

## Bound Backpressure and Failure

Bound the bytes and pending actions retained for a slow peer. Identify who can free each queue slot. `sess.Run` and `RunExpr` return two local results. `RunError` and `RunErrorExpr` also return a `*E` for the first uncaught throw. If it is non-nil, the peer's result may still be unresolved. [Paired drivers](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/run.go#L12-L31) · [Error-aware drivers](https://github.com/hayabusa-cloud/sess/blob/v0.2.0/error.go#L65-L86)

After frame delivery, endpoint close or continuation disposal, keep each descriptor and buffer through its final I/O use. [Lifecycle](lifecycle.md)

## Validate

Exercise the adapter with the module revisions it uses. The network examples cover TCP, UDP and framed broadcast. The `takt` example checks write permission before each dispatch to a bounded in-memory buffer. [Network examples](../../examples/echo_test.go) · [Guarded partial write](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/write_session_example_test.go) · [Runtime contracts](../runtime/INDEX.md)

Check a frame split across receives, several frames in one receive, an empty frame, and EOF inside a header or body. Fill the endpoint between two frames and check that the peer receives each message once and in order. Also check context changes during suspension, peer failure and close with outstanding I/O. Name the scheduler, fairness and peer assumptions for progress claims.

For [UDP input](../boundary/protocols.md#datagram-receives), check an empty datagram, an exact fit and a truncated payload. When the caller reuses a header, follow a receive with no control data by one that needs it.
