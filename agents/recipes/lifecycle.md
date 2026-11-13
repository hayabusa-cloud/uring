# Cancellation, Drain, and Teardown

Retire each owner without losing late observations. [Resource contract](../boundary/resources.md#cancellation-and-teardown)

## Establish the Obligations

Account for accepted requests, live subscriptions, pending cancellations, borrowed contexts/payloads, ZC notifications, and ZCRX credits. Reuse existing owner state; this requires no additional runtime table.

Stop admission before teardown. Retain unaccepted suffixes and pending protocol actions; accepted prefixes remain live after later submission failure.

## Request and Observe

Request cancellation where permitted, then keep driving and dispatching. Cancellation races with its target and cannot replace the target's completion. [Cancellation contract](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_cancel.3)

Dispatch original terminal and successfully queued cancellation CQEs, including after route retirement. Helpers own cancellation contexts; never return them manually. No-op/failed submission creates no new CQE obligation.

Keep results, failures, terminality, and releases distinct. Neither terminal callbacks nor empty CQ establish that all work ended. Continue required service/wakeup paths; caller no-progress waits use adaptive backoff.

## Release Each Owner

| Owner | Required release evidence |
|-------|---------------------------|
| Continuation | Resumed or discarded once; I/O remains independent |
| Request context | Original/cancellation observations and helper publication/dispatch complete |
| Provided/incremental buffer | Matching return, no kernel retention, caller borrows finished |
| SEND_ZC payload | Complete data/notification history or valid no-notification fallback |
| ZCRX reference | Refill publication or documented EOF path; blocked release stays owned |
| Descriptor/registration | No operation or borrow can access it |
| Ring | Work drained, CQEs reaped, subscriptions quiesced, caller serialized |

Finalizers retain their own prerequisites. `takt.Loop.Drain` discards suspensions; `takt.SubscriptionLoop.Drain` requests cancellation and retires routes. Neither waits for kernel completion. Preserve backend dispatch for late CQEs. The `sess.Close` effect closes only the local protocol endpoint.

`Uring.Stop` is the final destructor and synthesizes no terminal callbacks. `ZCRXReceiver.Stop` requests cancellation; `Close` additionally requires receiver retirement and owning-ring teardown. Keep explicit single-issuer task affinity through final `Stop`.

## Validate

Use [managed receive](../../examples/multishot_test.go), [poll cancellation](../../examples/poll_test.go), and [zero-copy](../../examples/zerocopy_test.go) cases. Check completion-before-cancel, late cancellation, delayed release, partial admission, failed startup, and repeated finalization only where idempotence is promised. Record feature skips under [verification](../verify.md).
