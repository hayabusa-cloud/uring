# Resource Lifetimes

Name each resource's owner, remaining kernel access, and release evidence. An observed result, request terminality, and permission to reuse storage are separate facts.

## Retain Before Submit

Keep asynchronous storage reachable through typed Go roots, address-stable, and valid through final use. Integer addresses, raw overlays, and base-SQE snapshots do not root referenced memory.

| Storage | Retain through |
|---------|----------------|
| Submission metadata | Its kernel consumption; payload use may continue |
| `SendMsgHeader` / `RecvMsg` header and referenced regions | Target completion; send storage remains immutable |
| Context facts | Copy before target mutation, reuse, or release; see [completion retention](sqe-cqe.md#completion-retention) |

`SendMsgHeader` submits caller-prepared metadata; `SendMsg` builds it from buffers and an address. Enable `NotifySucceed` without explicit success suppression when completion governs reuse. SQ consumption and a cancellation CQE do not release the target's storage.

## Registration

Registration methods return results synchronously. Separate the metadata passed to a call from the resources it registers:

| Storage | Retain through |
|---------|----------------|
| `RegisterFiles` / `RegisterFilesUpdate` array | Return from the synchronous method |
| Asynchronous `FilesUpdate` array | The operation's completion. Keep the array rooted, address-stable and unmodified by the caller. Allocation mode writes indexes back. |
| `RegisterMemRegion` / `RegisterZCRXIfq` descriptors | Method return; read kernel-written fields then |
| Caller-provided registered backing | All registrations and kernel uses; keep it reachable and address-stable |

Integer fields such as `RegionDesc.UserAddr` and `ZCRXAreaReg.Addr` do not retain backing memory. With `IORING_MEM_REGION_REG_WAIT_ARG`, call `RegisterMemRegion` before `Start` enables the ring.

With `replace` set, `CloneBuffers` and `CloneBuffersFromRegistered` install the source buffers in the requested destination slots and preserve registrations outside that range. [Buffer cloning](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_clone_buffers.3#L128-L132)

## File Lifetimes

| Resource | Caller action |
|----------|---------------|
| <a id="accepted-sockets"></a>[Accepted socket](files.md#accepted-sockets) | Transfer or close every successful accept, including after callback withdrawal. Use the accept mode to identify the process fd or ring slot. |
| <a id="fixed-file-lookup"></a>[Fixed-file slot](files.md#fixed-file-lookup) | Preserve the intended binding until the dependent requests have acquired it or finished. |
| <a id="inter-ring-file-transfers"></a>[`MsgRingFD` target](files.md#inter-ring-file-transfers) | Account for the installed reference. On a source `-EOVERFLOW`, reconcile the target before retrying. |

## Provided Buffers

`Add` stages; matching `Advance` publishes and transfers ownership to the kernel. Keep backing rooted and address-stable from staging through return. Do not mutate or reuse kernel-owned buffers. After ordinary return, the caller may retain the buffer; consuming data does not require immediate recycling.

Match the current request, submitted group, registration and selected ID before slicing. Ordinary selected-buffer `RECV` uses an offset-zero slice only without bundle/incremental mode or `MSG_TRUNC`, with `F_BUFFER`, no `F_BUF_MORE`, and `0 ≤ Res ≤ buffer length`. Other modes require their own layout and ownership interpretation. Copies of buffer views and iovecs borrow backing; a copied payload has its own lifetime.

### Bundles

For a matched ordinary bundle with `Res > 0`, check that `F_BUFFER` is set and `F_BUF_MORE` is clear. With uniform slots of `s > 0` bytes filled in order, integer division gives `1 + (Res - 1) / s` consumed slots, including an exactly full final slot. Pass the receive's registered group to `ring.BundleIterator`. Its `AllWithSlotID()` yields each ID and its valid bytes in publication order, starting at the CQE's first ID. IDs can be nonconsecutive. [Receive-bundle manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_recv.3#L85-L104) · [Reordered buffers](../../operation_facts_linux_test.go#L26)

`Collect()` allocates the outer slice and borrows the payload bytes. After every borrower finishes, call `it.Recycle(ring)` exactly once across all copies of the managed iterator. Use the originating ring and serialize recycling with its other buffer returns and publication. [Bundle handoff](../recipes/buffers.md#hand-off-a-receive-bundle)

At stream EOF, on an empty datagram, or on a receive error, inspect `F_BUFFER` before leaving the receive path. An empty datagram still delivers a message. An ordinary group can return one slot with zero payload bytes. With `F_BUFFER` set and `F_BUF_MORE` clear, `ring.BundleIterator` gives its ID, `Count() == 1` and `TotalBytes() == 0`. Recycle it once. A clear `F_BUFFER` returns no slot. `CQEView.BundleCount` counts payload segments, so it reports zero in both cases. [Linux buffer selection](https://github.com/gregkh/linux/blob/v6.18.15/io_uring/kbuf.c#L158-L209) · [Receive regression](../../bundle_test.go#L541) · [Slot accounting](../../proofs/README.md#ordinary-receive-bundles)

On Linux, `NewBundleIterator` assumes buffer ID `i` starts at byte `i*bufSize` in the backing slice and IDs were published consecutively modulo the ring size. Calling `Recycle` on that standalone view returns `ErrInvalidParam`. [Iterator contracts](../../bundle_iter.go)

### Incremental Receive

Create one `IncrementalReceiver` before the registration's first receive; use it for every receive consuming that group. Dispatch CQEs once, serially in CQ order. Track offsets per buffer ID: kernel reservation can precede completion, so IDs can interleave even with one caller.

| Evidence | Action |
|----------|--------|
| Valid positive CQE | Deliver at the selected ID's offset and account for consumed bytes |
| `F_BUF_MORE` | Keep the buffer kernel-owned and carry its offset forward; never republish it |
| `F_BUFFER` without `F_BUF_MORE` | Return the buffer and reset only its offset, after locating any final fragment |
| Zero or negative result | Still inspect both flags; zero bytes alone grant no recycle authority |
| `OnComplete` | Ends one successful receive, even if its buffer remains retained |

Apply every relevant row. `IncrementalHandler.OnData` borrows until return; copy retained bytes. Copy needed context facts before dispatch returns the pooled request context. Finish borrows before republishing. Byte layout, request `F_MORE`, and whole-buffer reuse remain separate.

## Zero-Copy Send and Receive

SEND_ZC data reports the result; notification history governs reuse. An early notification alone cannot retire the request. Data without `MORE` supplies the no-notification fallback. Retain identity and buffers through all required evidence.

The [tracker proof](../../proofs/README.md) establishes signal-count and release-evidence properties of the four-state model. Actual dispatch additionally requires matched identities, typed roots, valid borrows and serialized callback/pool ownership.

Managed ZC helpers retain success evidence regardless of `NotifySucceed` and reject explicit `IOSQE_CQE_SKIP_SUCCESS`. Raw SQEs require a caller-defined completion/lifetime contract.

ZCRX starts with an empty refill ring; publish only kernel-delivered references. Release would-block preserves retry ownership; successful release consumes the view. EOF has no refill identity but still requires wrapper release. Check actual wrapper setup and CQE-format support, separately from kernel capability.

## Multicast

`Multicast` selects SEND/SEND_ZC by payload size and target count; `MulticastZeroCopy` uses lower thresholds. Inspect opcode and flags per CQE, including after resuming fewer targets. Selected SEND_ZC and both `MulticastZeroCopy` paths retain completion evidence regardless of `NotifySucceed`.

Both require Direct context and return `(accepted, err)`, stopping at the first submission error. An accepted prefix can precede would-block: resume at the first unaccepted target and retain every accepted operation's resources.

Track the accepted target prefix and each send’s byte result separately. Retry only the unsent suffix of a short stream send. Preserve datagram boundaries. Direct context records the target FD. Overlapping sends with identical packed fields have the same `user_data`. If the caller needs to match each result to a specific send, serialize those sends or submit them individually with distinct live contexts.

Keep shared bytes immutable until every accepted send releases them. An ordinary send releases on completion. A zero-copy send requires terminal data without `MORE`, or both data and its matching notification.

## Cancellation and Teardown

Cancellation and timeout race with the original operation. Observe their results and any remaining original CQEs before releasing the original request context.

`Uring.Close` submits a close for one process fd, or one registered slot with `IOSQE_FIXED_FILE`. Submit it after the target's last use. Leave `IOSQE_CQE_SKIP_SUCCESS` clear to observe the result. On a full SQ, the caller still owns the close. Drive the ring and retry. A nil return admits the request. Stop using the target and reap the matched close CQE. Success reports zero. [Close encoding](../../operations_linux.go#L431-L450) · [File close example](../../examples/file_lifecycle_test.go#L150-L174)

Call `Uring.Stop` on the serialized ring owner after draining accepted work, reaping outstanding CQEs and quiescing subscriptions. It tears down the ring without driving pending closes or invoking terminal callbacks. [Ring teardown](../../io_uring_linux.go#L602-L618)

`ZCRXReceiver.Stop` requests cancellation. Its `Close` releases the mapped area only after receiver retirement and owning-ring teardown.

### Managed Cancellation

Each queued cancellation has a CQE and helper-owned extended context. Dispatch original and cancellation CQEs; never pool the helper's cancellation context yourself.

| Helper | Dispatch | Completion condition |
|--------|----------|----------------------|
| `MultishotSubscription` | Its `HandleCQE`, or each CQE's extended handler | Original terminal CQE and every queued cancellation CQE handled; `SubscriptionStopped` and `OnMultishotStop` describe only the original request |
| `ZCRXReceiver` | Each CQE's extended handler | `Stopped()` becomes true after original and cancellation retirement |

A failed submission or no-op `Cancel` on a stopped subscription creates no cancellation CQE obligation. Dispatch already queued cancellations even after `Unsubscribe` suppresses callbacks.

Retain the original pooled identity through both lifetimes and cancellation publication. CQ arrival order does not establish drain; SQ order alone does not protect reuse because an earlier `IO_DRAIN` can defer cancellation to workers. The [receive example](../../examples/multishot_test.go) covers natural completion and handler-requested cancellation.
