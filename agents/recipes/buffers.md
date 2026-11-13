# Buffers and Registered Resources

Retain storage through every access; return it under its own protocol. [Boundary details](../boundary/resources.md)

## Retain and Publish

Choose ordinary, registered, provided, incremental, or zero-copy storage. Retain typed roots, stable addresses, and owning descriptors/registrations. SQ consumption can release copied submission metadata while payload access continues. `RecvMsg` headers/referenced regions and `FilesUpdate` arrays remain caller-owned through completion.

For provided buffers, `Add` stages; matching `Advance` publishes and transfers ownership. Retain backing from staging through return. Do not mutate or republish kernel-owned storage; returned buffers need not be recycled immediately.

Bundles follow descriptor publication order. The standalone constructor requires consecutive IDs/compact backing and grants no managed recycle authority. Preserve application-selected tiers and group ownership.

## Retained Completions

`Wait` copies CQE descriptors. Direct context is inline; Indirect/Extended targets remain borrowed. Copy needed facts before target mutation, reuse, release, or failed resubmission that prepared the target. A copied `SQEView` is borrowed; a base-SQE copy neither roots payloads nor contains an SQE128 extension.

## Incremental Receive

Create one `IncrementalReceiver` for the registration before its first receive. Dispatch every consuming CQE once, serially in CQ order. Track offsets per ID; interleaving does not reset another ID.

`F_BUF_MORE` retains the buffer. Without it, locate the final fragment before resetting that ID. `OnData` borrows until return; copy retained bytes. Zero/negative results can return buffers: inspect `F_BUFFER` and `F_BUF_MORE`. Zero bytes alone grant no recycle authority. Finish borrows before republishing.

## Zero-Copy

For SEND_ZC, consume the data result separately from notification history. Data `MORE` promises notification: retain the payload through its release evidence. Early notification alone cannot retire the request; terminal no-notification fallback is separate. Track each request even when sends share bytes. [SEND_ZC contract](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_send_zc.3)

For ZCRX, publish refills only after kernel delivery. `ZCRXBuffer.Release` returning `iox.ErrWouldBlock` preserves the view. Nil consumes it and pools the wrapper: stop accessing bytes and never release that wrapper again. Zero-length EOF has no refill identity but requires wrapper release. Bound credits and check wrapper/CQE-format support; refill and SEND_ZC notification are distinct protocols.

## Multiple Targets

`Multicast` selects copy/zero-copy automatically; `MulticastZeroCopy` favors zero-copy at lower thresholds. Both require Direct context. Inspect opcode/flags; a smaller resumed target set can change selection. Advance by the accepted count even with an error; retry only the unaccepted targets. Track byte progress per target; a short stream send leaves its own unsent suffix. Preserve datagram message boundaries. Shared payload reuse waits for every applicable release.

## Validate

Use [fixed-buffer](../../examples/fixed_buffers_test.go), [bundle/incremental](../../examples/buffer_ring_test.go), and [zero-copy](../../examples/zerocopy_test.go) examples. Check interleaved IDs, zero/error buffer returns, delayed notifications, blocked refill, partial target admission, retained facts, and teardown. Feature skips leave that runtime evidence open.
