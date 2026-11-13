# Repeated Completion Streams

Retain the request through repeated events, terminality, and dependent release. Optional role: [takt](../runtime/takt.md).

## Establish the Route

Validate multishot support, setup, groups, and zero fields. Retain identity/generation while live. Bound event storage and define the slow-consumer response.

Use a subscription such as `takt.SubscriptionLoop`; one `kont.Suspension` cannot serve repeated events. The caller owns route policy; the backend maps routes to kernel identities.

## Dispatch Each Observation

1. Match identity/generation; copy required context facts while valid.
2. Interpret the result separately from `MORE`, buffer flags, and releases. Negative results with `MORE` remain failure/would-block observations with a live successor.
3. Deliver once; account separately for acquired FDs, buffers, and credits.
4. Retain live routes. At original terminality, follow runtime retirement rules and preserve outstanding backend dispatch obligations.

`SubscriptionLoop.Poll` returns borrowed storage; copy retained events before poll/run reuse. Keep retention bounded under deliberate caller service policy.

## Cancel and Retire

Cancellation races with natural completion. A queued cancellation owns a separate completion and helper context. Dispatch original and cancellation CQEs even after the original route ends. Failed submission or documented no-op cancellation creates no new CQE obligation.

For `MultishotSubscription`, use `HandleCQE` or its extended handler. `SubscriptionStopped`/`OnMultishotStop` report original terminality and permit no manual pool return. `Unsubscribe` suppresses callbacks without eliminating queued cancellation dispatch. Follow [managed cancellation](../boundary/resources.md#managed-cancellation) and [lifecycle](lifecycle.md).

## Validate

[Multishot examples](../../examples/multishot_test.go) cover accept, selected-buffer receive, and callback-requested cancellation. See [cancellation](../../cancel_linux_test.go) and [helper](../../multishot_internal_linux_test.go) tests.

Check live success/error, terminal events, stale generations, both completion/cancellation orders, late dispatch, and bounded consumer capacity. Progress claims require kernel, peer, fairness, and service assumptions.
