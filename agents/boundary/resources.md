# Resource Lifetimes

Name each resource's owner, remaining kernel access, and release evidence. An observed result, request terminality, and permission to reuse storage are separate facts.

## Retain Before Submit

Keep asynchronous storage reachable through typed Go roots, address-stable, and valid through final use. Integer addresses, raw overlays, and base-SQE snapshots do not root referenced memory.

| Storage | Retain through |
|---------|----------------|
| Submission metadata | Its kernel consumption; payload use may continue |
| `RecvMsg` header and referenced regions | Operation completion |
| `FilesUpdate` array | Operation completion; allocation mode can write indexes |
| Context facts | Copy before target mutation, reuse, or release; see [completion retention](sqe-cqe.md#completion-retention) |

## Provided Buffers

`Add` stages; matching `Advance` publishes and transfers ownership to the kernel. Keep backing rooted and address-stable from staging through return. Do not mutate or reuse kernel-owned buffers. After ordinary return, the caller may retain the buffer; consuming data does not require immediate recycling.

Match the current request, submitted group, registration and selected ID before slicing. Ordinary selected-buffer `RECV` uses an offset-zero slice only without bundle/incremental mode or `MSG_TRUNC`, with `F_BUFFER`, no `F_BUF_MORE`, and `0 ≤ Res ≤ buffer length`. Other modes require their own layout and ownership interpretation. Copies of buffer views and iovecs borrow backing; a copied payload has its own lifetime.

### Bundles

A bundle follows descriptor publication order, not necessarily consecutive IDs. Managed traversal records that order; the standalone constructor requires consecutive IDs and compact backing, and grants no recycle authority.

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

Track target admission, per-target bytes, and release independently. Retry only each short stream send's unsent suffix; preserve datagram message boundaries. Shared bytes retain separate request identities and release conditions; await matching ZC evidence before mutation or recycling.

## Cancellation and Teardown

Cancellation and timeout race with the original operation. Observe their results and any remaining original CQEs before releasing the original request context.

Serialize final `Close` and `Uring.Stop` after draining work, reaping CQEs, and quiescing subscriptions. `Uring.Stop` neither synthesizes terminal callbacks nor drains live operations. Requesting shutdown grants no early destruction permission.

`ZCRXReceiver.Stop` requests cancellation. Its `Close` releases the mapped area only after receiver retirement and owning-ring teardown.

### Managed Cancellation

Each queued cancellation has a CQE and helper-owned extended context. Dispatch original and cancellation CQEs; never pool the helper's cancellation context yourself.

| Helper | Dispatch | Completion condition |
|--------|----------|----------------------|
| `MultishotSubscription` | Its `HandleCQE`, or each CQE's extended handler | Original terminal CQE and every queued cancellation CQE handled; `SubscriptionStopped` and `OnMultishotStop` describe only the original request |
| `ZCRXReceiver` | Each CQE's extended handler | `Stopped()` becomes true after original and cancellation retirement |

A failed submission or no-op `Cancel` on a stopped subscription creates no cancellation CQE obligation. Dispatch already queued cancellations even after `Unsubscribe` suppresses callbacks.

Retain the original pooled identity through both lifetimes and cancellation publication. CQ arrival order does not establish drain; SQ order alone does not protect reuse because an earlier `IO_DRAIN` can defer cancellation to workers. The [receive example](../../examples/multishot_test.go) covers natural completion and handler-requested cancellation.
