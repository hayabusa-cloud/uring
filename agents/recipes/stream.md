# Repeated Completion Streams

Retain the request through repeated events, terminality, and dependent release. Optional role: [takt](../runtime/takt.md).

## Establish the Route

Validate multishot support, setup, groups, and zero fields. Retain identity/generation while live. Bound event storage and define the slow-consumer response.

Give the request a persistent route. A caller may use `takt.SubscriptionLoop` for this role. The backend maps the original route to the kernel request. Reserve one-shot suspensions for individual resumptions.

## Dispatch Each Observation

1. Match the [completion origin](../boundary/sqe-cqe.md#completion-origin) and route generation. Copy required context fields while valid.
2. Interpret the result separately from `MORE`, buffer flags, and releases. Negative results with `MORE` remain failure/would-block observations with a live successor.
3. Deliver each accepted value once, including a value on the final CQE. Give every acquired FD, buffer or credit an owner even if the caller discards the event.
4. Keep the native request identifiable through its final CQE and any outstanding cancellation that refers to it.

With `takt.SubscriptionLoop`, copy retained events before the next `Poll` or `Drain` and keep referenced payloads through their last use. If a full `sess` endpoint delays the final value, retain its pending send after the native route retires. When the peer frees a slot, recheck any delivery permission and retry that same send. [Result storage](../runtime/takt.md#completion-results) · [Endpoint retry](../runtime/sess.md#outputs-and-ownership)

## Rearm a Managed Accept

Record the terminal result in `OnStop`. With submission and dispatch serialized on the ring owner, the helper still holds its Extended context during the callback. A pending cancellation can keep it in use after the callback returns. If the result warrants another accept, mark a retry pending. Call `servicePending` at the start of each owner turn, before `Wait`, even if the last poll found the CQ empty.

```go
servicePending := func(ring *uring.Uring) (bool, error) {
	switch {
	case !retryPending:
		return false, nil
	case !accepting:
		retryPending = false
		return true, nil
	}
	next, err := ring.AcceptMultishot(acceptCtx, handler.Handler())
	if err != nil {
		return false, err
	}
	trackSubscription(next)
	retryPending = false
	return true, nil
}
```

On `iox.ErrWouldBlock`, keep the retry pending and still call `Wait` that turn. Back off only when the helper reports no progress and the poll reaps zero CQEs. Route an admitted replacement through its own `HandleCQE` and keep the old subscription available for any outstanding cancellation CQE. [Waiting and backoff](../boundary/integration.md#waiting-and-backoff)

## Cancel and Retire

Cancellation races with natural completion. A raw `AsyncCancel` uses the caller's control context. With `IOSQE_CQE_SKIP_SUCCESS` clear, an admitted cancellation yields its own CQE. A refused offer or a helper's documented no-op adds no control reply to await. After local route retirement, dispatch the original through its terminal CQE and reap any outstanding control replies. [Poll cancellation](lifecycle.md#request-and-observe) · [Subscription drain](../runtime/takt.md#drain-and-ownership)

For an ordinary `AcceptMultishot`, withdraw on the serialized completion owner between callbacks. Choose where to handle remaining [accepted sockets](../boundary/files.md#accepted-sockets).

| Native delivery | Caller action |
|-----------------|---------------|
| Keep the step callback | Stop application admission, then call `Cancel`. The handler closes each subsequent success. If cancellation submission fails, continue dispatch and apply the caller's retry policy. |
| Suppress callbacks with `Unsubscribe` | On the known ordinary-accept route, copy the opcode and result before `HandleCQE` can retire the context. Dispatch both requests. Close each matched original success whose callback was suppressed. An in-flight callback can already own its socket. |

`HandleCQE` returns true for both the original and its cancellation. `Unsubscribe` makes one best-effort cancellation attempt, and an active original can still be cancelled with `Cancel`. Dispatch through original terminality and each admitted cancellation's CQE. The helper returns its own contexts. [Managed cancellation](../boundary/resources.md#managed-cancellation) · [Lifecycle](lifecycle.md)

## Validate

[Multishot examples](../../examples/multishot_test.go) cover accept, selected-buffer receive, and callback-requested cancellation. The [accept withdrawal test](../../multishot_test.go) closes a reaped socket after callback suppression and checks EOF at its peer. See also [cancellation](../../cancel_linux_test.go) and [helper](../../multishot_internal_linux_test.go) tests.

Check live success/error, terminal events, stale generations, both completion/cancellation orders, late dispatch, and bounded consumer capacity. Progress claims require kernel, peer, fairness, and service assumptions.

On UDP loopback, queue an empty datagram followed by `X`. With the local endpoint full, reap the final empty message. Free one endpoint slot and check that the peer receives the empty message exactly once. A fresh receive on the socket should then deliver `X`. [Datagram receives](../boundary/protocols.md#datagram-receives)
