# Cancellation, Drain, and Teardown

Retire each owner without losing late observations. [Resource contract](../boundary/resources.md#cancellation-and-teardown)

## Establish the Obligations

Account for accepted requests, live subscriptions, pending cancellations, borrowed contexts/payloads, ZC notifications, and ZCRX credits. Reuse existing owner state; this requires no additional runtime table.

Stop accepting new application work. Keep driving accepted prefixes and submit the cancellations and file closes that teardown requires. Retain any unaccepted suffixes and pending protocol actions until their owner retries or discards them.

## Request and Observe

Request cancellation where permitted, then keep driving and dispatching. Cancellation races with its target and cannot replace the target's completion. [Cancellation contract](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_cancel.3)

A successful bulk cancellation reports how many requests it matched. Dispatch the cancel reply and each target’s CQEs by their own identities, even after the application route retires. A managed helper owns its cancellation context through dispatch. A helper call that queues nothing adds no reply to await. [Cancellation tests](../../integration_linux_test.go#L2098)

The example tracks one terminal CQE for a multishot poll with a fixed Direct ID and one for its cancellation. If the poll ID changes, match each CQE to the ID it carries. [Poll ID changes](../boundary/sqe-cqe.md#change-a-poll-id)

```go
type terminalSeen struct {
	request uring.SQEContext
	seen    bool
}

func (s terminalSeen) Step(cqe uring.CQEView) terminalSeen {
	if cqe.Context() == s.request && !cqe.HasMore() {
		s.seen = true
	}
	return s
}

func (s terminalSeen) Observe() bool { return s.seen }
```

Cancellation matches the poll's submitted `user_data`. For a poll on `fd` with zero SQE flags, initialize both identities with their submitted opcodes before reaping.

```go
poll := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(fd))
cancel := poll.WithOp(uring.IORING_OP_ASYNC_CANCEL)
watch := tempo.Both(terminalSeen{request: poll}, terminalSeen{request: cancel})
```

Submit `ring.PollAddMultishot(poll, uring.EPOLLIN)`. Leave `IOSQE_CQE_SKIP_SUCCESS` clear on both requests. If `ring.AsyncCancel(cancel, poll.Raw())` is admitted, dispatch its reply and the poll's terminal CQE. Keep both identities reserved until those replies are routed. [Request identity](../boundary/sqe-cqe.md#request-identity)

For each reaped CQE, assign `watch = watch.Step(cqe)` and dispatch its result. Readiness with `MORE` leaves the poll live. If the cancel reply arrives first, only `watch.Observe().Right` is true, whether cancellation succeeded or failed. Both fields together record that both terminal CQEs were observed. Release each resource at its own final use. [Temporal product](https://github.com/hayabusa-cloud/tempo/blob/v0.1.1/product.go#L21-L45)

Keep results, failures, terminality, and releases distinct. Neither terminal callbacks nor empty CQ establish that all work ended. Continue required service/wakeup paths; caller no-progress waits use adaptive backoff.

## Timers and Linked Deadlines

For an independent one-shot timer, call `ring.Timeout(ctx, d, uring.WithCount(0))` to wait for time alone. The default count is one. An unrelated completion, such as a NOP, can satisfy that count and produce a timer CQE with `Res == 0`. Clock expiry reports `-ETIME`. If another request is still pending when the timer fires, keep driving it or cancel it and account for its terminal CQE. [Timeout defaults](../../interface_internal.go#L135) · [Poll with an independent timer](../../examples/timeout_linux_test.go#L139)

For a periodic timer, pass `WithTimeoutMultishot()` and a relative interval. The default is one expiry. A positive `WithCount(n)` sets a limit of `n` expiries. Use `WithCount(0)` for no expiry limit. Each expiry reports `-ETIME`. `IORING_CQE_F_MORE` means the timer remains active. Reap its terminal CQE before reusing its identity. [Linux timer completion](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c#L57-L90)

To give a request a deadline, check that two SQ slots are available on an exclusively driven ring. Queue the target with `IOSQE_IO_LINK`, immediately followed by `LinkTimeout`, in the same kernel submission. Once Linux consumes the target, a later standalone `LinkTimeout` completes with `-EINVAL`. Leave `IOSQE_CQE_SKIP_SUCCESS` clear on both SQEs. For a target such as `SendFD` that suppresses success by default, enable `NotifySucceed` when creating the ring. Reap both replies. Target completion can disarm the timer with `-ECANCELED`. Expiry attempts cancellation, and the target's own CQE reports its outcome. [Linked example](../../examples/timeout_linux_test.go#L259) · [Linux link and timer handling](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c)

Give each overlapping pair a distinct live `pairID`. The README's `queueReceiveWithTimeout` packs that `uint16` into both Direct contexts. The opcodes distinguish the receive from its timer, even when several pairs share a socket. Reuse the ID after handling both terminal CQEs and all outstanding control replies that reference the pair. [README helper](../../README.md#deadline-composition) · [Direct fields](../../ctx_unix.go#L79-L88) · [Context encoding model](../../proofs/README.md#context-representation)

Link direction also matters. If an ordinary `Timeout` itself carries `IOSQE_IO_LINK`, expiry normally cancels the following request. `WithTimeoutEtimeSuccess()` lets that successor run on expiry. The timer's own CQE still reports `-ETIME`. [Linux timer expiry](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c#L254-L276)

### Update or Remove a Timer

Pass the timer’s submitted `user_data` to `TimeoutUpdate` or `TimeoutRemove`. Give each outstanding update or removal its own `IORING_OP_TIMEOUT_REMOVE` context. Leave `IOSQE_CQE_SKIP_SUCCESS` clear to observe its zero result on success. Successful removal gives the timer a separate terminal `-ECANCELED`. If the control reports `-ENOENT` or `-EALREADY` during expiry, keep routing the original timer through its terminal CQE. [Update and removal tests](../../integration_linux_test.go#L357) · [Control results](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_timeout_update.3#L61-L78)

To reschedule an armed `LinkTimeout`, target the deadline’s ID and pass `uring.WithTimeoutFlags(uring.IORING_LINK_TIMEOUT_UPDATE)` to `TimeoutUpdate`. Account for the update, the linked deadline and the operation it guards. [Linked timeout lookup](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c#L494-L502)

## Release Each Owner

| Owner | Required release evidence |
|-------|---------------------------|
| Continuation | Resumed or discarded once; I/O remains independent |
| Request context | Original/cancellation observations and helper publication/dispatch complete |
| Provided/incremental buffer | Matching return, no kernel retention, caller borrows finished |
| SEND_ZC payload | Complete data/notification history or valid no-notification fallback |
| ZCRX reference | Refill publication or documented EOF path; blocked release stays owned |
| Process fd | Its remaining direct users have finished or ownership has transferred |
| [Registered file slot](../boundary/files.md#fixed-file-lookup) | Every request that needs this binding has acquired its file or finished |
| [Tagged registration](../boundary/files.md#retirement-tags) | Its own matched retirement CQE, when using nonzero Linux resource tags |
| [Registered backing memory](../boundary/resources.md#registration) | All registrations and kernel uses of that memory have ended |
| [Accepted socket](../boundary/files.md#accepted-sockets) | Transfer to an owner or close each successful result, including after application delivery stops |
| Ring | Work drained, CQEs reaped, subscriptions quiesced, caller serialized |

Keep backend dispatch available for late CQEs after a `takt` loop drains or a session signals protocol termination. [Loop drain](../runtime/takt.md#drain-and-ownership) · [Session close](../runtime/sess.md#outputs-and-ownership)

When closing a file through `Uring.Close`, leave `IOSQE_CQE_SKIP_SUCCESS` clear. After admission, stop using the fd or slot and reap the close result before the final `Uring.Stop`. Keep single-issuer task affinity through `Stop`. [Close and ring teardown](../boundary/resources.md#cancellation-and-teardown)

`ZCRXReceiver.Stop` requests cancellation. Call `ZCRXReceiver.Close` after the receiver retires and its ring has stopped.

## Validate

Use [managed receive](../../examples/multishot_test.go), [poll cancellation](../../examples/poll_test.go), and [zero-copy](../../examples/zerocopy_test.go) cases. Check completion-before-cancel, late cancellation, delayed release, partial admission, failed startup, and repeated finalization only where idempotence is promised. Record feature skips under [verification](../verify.md).

To check overlapping deadlines, arm two receives on the same socket with different `pairID` values. Update the second timer and reap both CQEs for its pair. Then send one byte from the peer. Match the update, both receives and both deadlines by `user_data`.
