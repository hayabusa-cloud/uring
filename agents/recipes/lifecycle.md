# Cancellation, Drain, and Teardown

Retire each owner without losing late observations. [Resource contract](../boundary/resources.md#cancellation-and-teardown)

## Establish the Obligations

Account for accepted requests, live subscriptions, pending cancellations, borrowed contexts/payloads, ZC notifications, and ZCRX credits. Reuse existing owner state; this requires no additional runtime table.

Stop admission before teardown. Retain unaccepted suffixes and pending protocol actions; accepted prefixes remain live after later submission failure.

## Request and Observe

Request cancellation where permitted, then keep driving and dispatching. Cancellation races with its target and cannot replace the target's completion. [Cancellation contract](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_cancel.3)

Dispatch original terminal and successfully queued cancellation CQEs, including after route retirement. Helpers own cancellation contexts; never return them manually. No-op/failed submission creates no new CQE obligation.

For one target and one queued cancellation, a caller can pair two `tempo` contracts over the same CQE history. One records the target's terminal CQE and the other records the cancellation CQE. The caller matches and dispatches both. [`tempo.Both`](https://pkg.go.dev/code.hybscloud.com/tempo@v0.1.0#Both)

Keep results, failures, terminality, and releases distinct. Neither terminal callbacks nor empty CQ establish that all work ended. Continue required service/wakeup paths; caller no-progress waits use adaptive backoff.

## Timers and Linked Deadlines

For an independent one-shot timer, call `ring.Timeout(ctx, d, uring.WithCount(0))` to wait for time alone. The default count is one. An unrelated completion, such as a NOP, can satisfy that count and produce a timer CQE with `Res == 0`. Clock expiry reports `-ETIME`. If another request is still pending when the timer fires, keep driving it or cancel it and account for its terminal CQE. [Timeout defaults](../../interface_internal.go#L135) · [Poll with an independent timer](../../examples/timeout_linux_test.go#L139)

For a periodic timer, pass `WithTimeoutMultishot()` and a relative interval. An unmodified timer fires once by default, at most `n` times with a positive `WithCount(n)`, or repeatedly with `WithCount(0)`. Each expiry reports `-ETIME`. `IORING_CQE_F_MORE` means the timer remains active. Reap its terminal CQE before reusing its identity. [Linux timer completion](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c#L57-L90)

To give a request a deadline, check that two SQ slots are available on an exclusively driven ring. Queue the target with `IOSQE_IO_LINK`, immediately followed by `LinkTimeout`, in the same kernel submission. Once Linux consumes the target, a later standalone `LinkTimeout` completes with `-EINVAL`. Enable success reporting and leave `IOSQE_CQE_SKIP_SUCCESS` clear on both. Reap both replies. Target completion can disarm the timer with `-ECANCELED`. Expiry attempts cancellation, and the target's own CQE reports its outcome. [Linked example](../../examples/timeout_linux_test.go#L259) · [Linux link and timer handling](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c)

Link direction also matters. If an ordinary `Timeout` itself carries `IOSQE_IO_LINK`, expiry normally cancels the following request. `WithTimeoutEtimeSuccess()` lets that successor run on expiry. The timer's own CQE still reports `-ETIME`. [Linux timer expiry](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c#L254-L276)

### Update or Remove a Timer

Pass the timer’s submitted `user_data` to `TimeoutUpdate` or `TimeoutRemove`. Give each outstanding update or removal its own `IORING_OP_TIMEOUT_REMOVE` context. Leave `IOSQE_CQE_SKIP_SUCCESS` clear to observe its zero result on success. Successful removal gives the timer a separate terminal `-ECANCELED`. If the control reports `-ENOENT` or `-EALREADY` during expiry, keep routing the original timer through its terminal CQE. [Update and removal tests](../../integration_linux_test.go#L357) · [Control results](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_timeout_update.3#L61-L78)

On Linux 6.18.15 and 6.19.14, updating a standalone `Timeout` clears its count. A one-shot then waits for time alone. A periodic timer can continue past its original limit. To preserve a finite total, remove the old timer and count all expiries through its terminal CQE. Start a new timer only if a positive count remains. [Linux 6.18.15 update](https://github.com/gregkh/linux/blob/v6.18.15/io_uring/timeout.c#L420-L440) · [Linux 6.19.14 update](https://github.com/gregkh/linux/blob/v6.19.14/io_uring/timeout.c#L424-L444)

To reschedule an armed `LinkTimeout`, target the deadline’s ID and pass `uring.WithTimeoutFlags(uring.IORING_LINK_TIMEOUT_UPDATE)` to `TimeoutUpdate`. Account for the update, the linked deadline and the operation it guards. [Linked timeout lookup](https://github.com/gregkh/linux/blob/v6.18.15/io_uring/timeout.c#L488-L502)

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

Keep backend dispatch available for late CQEs after a [`takt` loop drains](../runtime/takt.md#drain-and-ownership) or `sess.Close` closes the local endpoint.

`Uring.Stop` is the final destructor and synthesizes no terminal callbacks. `ZCRXReceiver.Stop` requests cancellation; `Close` additionally requires receiver retirement and owning-ring teardown. Keep single-issuer task affinity through final `Stop`.

## Validate

Use [managed receive](../../examples/multishot_test.go), [poll cancellation](../../examples/poll_test.go), and [zero-copy](../../examples/zerocopy_test.go) cases. Check completion-before-cancel, late cancellation, delayed release, partial admission, failed startup, and repeated finalization only where idempotence is promised. Record feature skips under [verification](../verify.md).
