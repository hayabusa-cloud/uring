# Buffers and Registered Resources

Retain storage through every access; return it under its own protocol. [Boundary details](../boundary/resources.md)

## Retain and Publish

Choose ordinary, registered, provided, incremental, or zero-copy storage. Retain typed roots, stable addresses, and owning descriptors/registrations. SQ consumption can release copied submission metadata while payload access continues. `SendMsgHeader` and `RecvMsg` headers/referenced regions and `FilesUpdate` arrays remain caller-owned through completion; send storage remains immutable.

Synchronous registration borrows metadata only until method return; follow the [registration lifetimes](../boundary/resources.md#registration) for arrays, descriptors, and backing memory.

For provided buffers, `Add` stages; matching `Advance` publishes and transfers ownership. Retain backing from staging through return. Do not mutate or republish kernel-owned storage; returned buffers need not be recycled immediately.

## Hand Off a Receive Bundle

At stream EOF or on error, recycle any slot returned by `ring.BundleIterator`. For an empty UDP datagram, keep an independently owned empty value for delivery and recycle any selected slot. For a nonempty bundle, choose how to hand off its bytes. [Bundle completions](../boundary/resources.md#bundles) · [Datagram receives](../boundary/protocols.md#datagram-receives)

- **Copy the payload.** Give `it.CopyTo(dst)` independent storage of at least `it.TotalBytes()` bytes to retain the whole bundle. A shorter destination receives only a prefix. Once the copy finishes and all borrows end, recycle the bundle. A blocked delivery can retry with the same owned bytes.
- **Lend the payload.** Slices from `it.All()` or `it.Collect()` borrow the returned buffers. Keep them through a blocked send, time in the queue and the consumer's last read. Have the consumer signal when it finishes so the ring owner can recycle once. [Session delivery](../runtime/sess.md#outputs-and-ownership)

## Retained Completions

`Wait` copies CQE descriptors. Direct context is inline; Indirect/Extended targets remain borrowed. Copy needed facts before target mutation, reuse, release, or failed resubmission that prepared the target. A copied `SQEView` is borrowed; a base-SQE copy neither roots payloads nor contains an SQE128 extension.

## Incremental Receive

Create one `IncrementalReceiver` for the registration before its first receive. Dispatch every consuming CQE once, serially in CQ order. Track offsets per ID; interleaving does not reset another ID.

`F_BUF_MORE` retains the buffer. Without it, locate the final fragment before resetting that ID. `OnData` borrows until return; copy retained bytes. Zero/negative results can return buffers: inspect `F_BUFFER` and `F_BUF_MORE`. Zero bytes alone grant no recycle authority. Finish borrows before republishing.

## Zero-Copy

For SEND_ZC, consume the data result separately from notification history. Data `MORE` promises notification: retain the payload through its release evidence. Early notification alone cannot retire the request; terminal no-notification fallback is separate. Track each request even when sends share bytes. [SEND_ZC contract](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_send_zc.3)

For ZCRX, publish refills only after kernel delivery. `ZCRXBuffer.Release` returning `iox.ErrWouldBlock` preserves the view. Nil consumes it and pools the wrapper: stop accessing bytes and never release that wrapper again. Zero-length EOF has no refill identity but requires wrapper release. Bound credits and check wrapper/CQE-format support; refill and SEND_ZC notification are distinct protocols.

## Multiple Targets

Submit `Multicast` or `MulticastZeroCopy` with Direct context. Advance the target list by the accepted count even on error. When retrying fewer targets, check each CQE's opcode and flags because the selected send operation can change. For a short stream send, retry that target's unsent byte suffix. Preserve datagram boundaries. Keep shared bytes intact while any target still needs them for submission or any accepted send still borrows them. [Multicast selection and release](../boundary/resources.md#multicast) · [One group reply in takt](../runtime/takt.md#submit-multicast-as-one-operation)

## Validate

Use [fixed-buffer](../../examples/fixed_buffers_test.go), [bundle/incremental](../../examples/buffer_ring_test.go), and [zero-copy](../../examples/zerocopy_test.go) examples. Check interleaved IDs, zero/error buffer returns, delayed notifications, blocked refill, partial target admission, retained facts, and teardown. Feature skips leave that runtime evidence open.

To check recycling, shut down the peer's write side and repeat `ReceiveBundle` with `IOSQE_ASYNC` more times than the group has slots. Recycle any selected slot. Each receive should still report EOF. On an unconnected stream socket, each should report `-ENOTCONN`. [Kernel regression](../../bundle_test.go#L541)
