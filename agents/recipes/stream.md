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

## Cancel and Retire

Cancellation races with natural completion. A queued cancellation owns a separate completion and helper context. Dispatch original and cancellation CQEs even after the original route ends. Failed submission or documented no-op cancellation creates no new CQE obligation.

For `MultishotSubscription`, use `HandleCQE` or its extended handler. `SubscriptionStopped`/`OnMultishotStop` report original terminality and permit no manual pool return. `Unsubscribe` suppresses callbacks without eliminating queued cancellation dispatch. Follow [managed cancellation](../boundary/resources.md#managed-cancellation) and [lifecycle](lifecycle.md).

## Validate

[Multishot examples](../../examples/multishot_test.go) cover accept, selected-buffer receive, and callback-requested cancellation. See [cancellation](../../cancel_linux_test.go) and [helper](../../multishot_internal_linux_test.go) tests.

Check live success/error, terminal events, stale generations, both completion/cancellation orders, late dispatch, and bounded consumer capacity. Progress claims require kernel, peer, fairness, and service assumptions.

On UDP loopback, queue an empty datagram followed by `X`. With the local endpoint full, reap the final empty message. Free one endpoint slot and check that the peer receives the empty message exactly once. A fresh receive on the socket should then deliver `X`. [Datagram receives](../boundary/protocols.md#datagram-receives)
