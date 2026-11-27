# Buffers and Registered Resources

Retain storage through every access; return it under its own protocol. [Boundary details](../boundary/resources.md)

## Retain and Publish

Choose ordinary, registered, provided, incremental or zero-copy storage. Keep typed roots, stable addresses and owning descriptors through use. Synchronous registration metadata can be reused on return. For `SendMsgHeader`, `RecvMsg` and asynchronous `FilesUpdate`, keep the caller-supplied storage through completion. [Submission storage](../boundary/resources.md#retain-before-submit) · [Registration lifetimes](../boundary/resources.md#registration)

For provided buffers, stage entries with `BufferRing.Add`, then publish that count with `Advance`. Keep each backing live from staging through return and finish any application borrow before republishing. [Provided buffers](../boundary/resources.md#provided-buffers)

## Hand Off a Receive Bundle

At stream EOF or on error, recycle any slot returned by `ring.BundleIterator`. For an empty UDP datagram, keep an independently owned empty value for delivery and recycle any selected slot. For a nonempty bundle, choose how to hand off its bytes. [Bundle completions](../boundary/resources.md#bundles) · [Datagram receives](../boundary/protocols.md#datagram-receives)

- **Copy the payload.** Give `it.CopyTo(dst)` independent storage of at least `it.TotalBytes()` bytes to retain the whole bundle. A shorter destination receives only a prefix. Once the copy finishes and all borrows end, recycle the bundle. A blocked delivery can retry with the same owned bytes.
- **Lend the payload.** Slices from `it.All()` or `it.Collect()` borrow the returned buffers. Keep them through a blocked send, time in the queue and the consumer's last read. Have the consumer signal when it finishes so the ring owner can recycle once. [Session delivery](../runtime/sess.md#outputs-and-ownership)

## Retained Completions

Before recycling a context, save the fields the consumer needs. A copied fd, slot or buffer ID retains its numeric value. For later file I/O, preserve the intended file binding. For a retained payload, copy the bytes or hold the returned buffer through the borrow. [Borrowed SQEs](../boundary/sqe-cqe.md#completion-retention) · [Fixed-file lookup](../boundary/files.md#fixed-file-lookup) · [Provided-buffer return](../boundary/resources.md#provided-buffers)

## Incremental Receive

1. Register the incremental group, add each backing slice with its matching ID and publish the entries. Create one `IncrementalReceiver` before the first receive on that group.
2. Submit with `Recv`. Dispatch each consuming CQE once, serially in CQ order. Copy needed context fields before calling `HandleCQE`, which returns the context to its pool.
3. Copy any `OnData` bytes needed after the callback. Account for returned buffers even on zero or failure, using `F_BUFFER` and `F_BUF_MORE`. The receiver tracks each ID's offset across interleaved CQEs. [Incremental buffer contract](../boundary/resources.md#incremental-receive) · [Receiver](../../incremental_linux.go)

## Zero-Copy

For `ZCTracker`, record the submitted byte range on admission. Use `OnCompleted` to advance a stream by the bytes actually sent. A retry borrows its own suffix. Remove each send’s range at its `OnNotification`. If handing out independent slots within an array, use `buf[lo:hi:hi]` to limit each slot’s capacity and account for any wider aliases. [Send release](../boundary/resources.md#zero-copy-send-and-receive) · [Shared send buffers](../boundary/resources.md#shared-send-buffers)

For ZCRX, publish refills only after kernel delivery. `ZCRXBuffer.Release` returning `iox.ErrWouldBlock` preserves the view. Nil consumes it and pools the wrapper: stop accessing bytes and never release that wrapper again. Zero-length EOF has no refill identity but requires wrapper release. Bound credits and check wrapper/CQE-format support; refill and SEND_ZC notification are distinct protocols.

## Multiple Targets

Submit `Multicast` or `MulticastZeroCopy` with Direct context. Advance the target list by the accepted count even on error. When retrying fewer targets, check each CQE's opcode and flags because the selected send operation can change. For a short stream send, retry that target's unsent byte suffix. Preserve datagram boundaries. Keep shared bytes intact while any target still needs them for submission or any accepted send still borrows them. [Multicast selection and release](../boundary/resources.md#multicast) · [One group reply in takt](../runtime/takt.md#submit-multicast-as-one-operation)

## Validate

Use [fixed-buffer](../../examples/fixed_buffers_test.go), [bundle/incremental](../../examples/buffer_ring_test.go), and [zero-copy](../../examples/zerocopy_test.go) examples. Check interleaved IDs, zero/error buffer returns, delayed notifications, blocked refill, partial target admission, retained facts, and teardown. Feature skips leave that runtime evidence open.

To check recycling, shut down the peer's write side and repeat `ReceiveBundle` with `IOSQE_ASYNC` more times than the group has slots. Recycle any selected slot. Each receive should still report EOF. On an unconnected stream socket, each should report `-ENOTCONN`. [Kernel regression](../../bundle_test.go#L541)
