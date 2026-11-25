# SQE/CQE Boundary

An adapter exposes one kernel-facing action and its observations. Apply the [encoding contract](encoding.md); preserve the independent completion facts below.

## Submission

Check public and opcode-specific input domains before narrowing, indexing, or writing the SQE. Initialize required fields and clear rejected fields. Retain identity through completion, release, and outstanding [cancellation lookups](resources.md#managed-cancellation).

The default single-issuer mode requires one locked OS thread from before `Start` through `Stop`, with serialized ring operations. Select `MultiIssuers=true` for shared submission or OS-thread migration. Goroutine serialization alone does not establish Linux task identity.

## Observation

Choose the wait by the contexts that can arrive on the ring. All three waits drive pending submissions on Linux. After a successful enter, a zero-length destination returns `(0, nil)` and consumes no CQEs. Supply a nonempty destination to reap completions. On Linux, each wait copies its records before advancing the CQ head. [Partial-drain test](../../cq_pending_linux_test.go)

| Wait | Incoming CQEs | What the caller receives |
|------|---------------|--------------------------|
| [Wait](../../interface_linux.go) | Local contexts, message IDs or Linux resource tags | Raw result, flags and all 64 bits of `user_data`. A local pointer-mode context borrows its target. |
| [WaitDirect](../../cqe_direct_linux.go) | Direct contexts only | Result, CQE flags, opcode, SQE flags, buffer group and signed descriptor, all copied by value. |
| [WaitExtended](../../cqe_extended_linux.go) | Extended contexts only | Result, CQE flags and a borrowed `ExtSQE`. Return a pooled context to its originating pool after final use. |

### Completion Origin

On a mixed ring, reserve distinct live IDs for sources that require different actions. Use `Wait` and match `cqe.Context().Raw()` before using request accessors or dispatching to a typed handler. A sender can choose an ID whose mode bits resemble a local context. [Mixed-ring test](../../integration_linux_test.go#L1663)

| Matched source | How to handle the CQE |
|----------------|-----------------------|
| Local request | Decode its live submission context, then interpret the operation result or notification below. |
| `MsgRing` target | Deliver `Res` as the sender's signed value, including negative values. |
| [`MsgRingFD` target](resources.md#inter-ring-file-transfers) | Use the installation result for the selected destination mode. |
| [Resource retirement tag](https://github.com/torvalds/linux/blob/v6.18/io_uring/rsrc.c#L512-L529) | Mark the identified registration retired. Its zero result and flags can also occur on a successful NOP. |

### Local Results and Flags

For a matched local request, interpret operation results, notifications and liveness separately.

| Evidence | Meaning |
|----------|---------|
| Nonnegative operation `Res` | The observed step succeeded; the value retains its opcode-specific meaning |
| Operation `Res == -EAGAIN` | `iox.ErrWouldBlock`, even when `MORE` is present |
| Other negative operation `Res` | The corresponding failure |
| `IORING_CQE_F_MORE` | Same-request successor evidence; it does not change result classification or prove byte progress |
| SEND_ZC notification (`IORING_CQE_F_NOTIF`) | Release evidence for the matched send. With `IORING_SEND_ZC_REPORT_USAGE`, test `uint32(Res) & IORING_NOTIF_USAGE_ZC_COPIED` for copy fallback or unused zero-copy. [Send buffer lifetime](resources.md#zero-copy-send-and-receive) |

Only an explicit caller contract may translate request liveness into `iox.ErrMore`.

## Request Identity

If two completions require different actions, the caller must be able to tell which submission produced each one. For example, two `FutexWait` requests on different words can produce identical Direct contexts, results and flags. Serialize those requests or give each a distinct live context. CQ order alone cannot distinguish them.

For deferred dispatch, bind the event to its originating request when reaping or preserve the original token binding through delivery. Reuse a token only once late CQEs, notifications and outstanding cancellation lookups can no longer resolve to the new request. Copy borrowed fields while their target is valid. [Completion retention](#completion-retention) · [Managed cancellation](resources.md#managed-cancellation)

## Completion Retention

`Wait` copies `Res`, `Flags`, and `user_data`. For a local request, retention depends on its context mode.

| Mode | Retained submission facts |
|------|---------------------------|
| Direct | Compact inline context fields; `FullSQE()` is false |
| Indirect | Borrowed 64-byte base SQE, copied into the target before publication |
| Extended | Borrowed 64-byte base SQE plus 64 bytes of caller user data; submission preserves those user bytes |

`FullSQE()` promises the base SQE without SQE128 extension bytes; `SQE()` borrows its target. Copying the view or CQE does not copy target fields, retain payload roots, or transfer ownership.

| Change | Retention obligation |
|--------|----------------------|
| `WithOp`, `WithFlags`, `WithBufGroup`, `WithFD`, `OrFlags` | These mutate pointer-backed targets; copy needed facts first |
| Later submission | Replaces the retained base SQE, even without pool return |
| Target reuse or release | Copy needed facts before invalidation; a packed context copy shares storage |

Keep a live request's target address, identity, and submitted base-SQE fields stable through all required CQEs and release evidence. Update caller user data under its own ownership/synchronization contract.

For example, save a READ's `Off()` before preparing a NOP on the same target. Retaining `cqe.SQE()` preserves neither that offset nor an independent snapshot, for pooled or caller-allocated targets.

Sources: [publication](../../submit_state.go), [context setters](../../ctx.go), [CQE views](../../cqe_view.go), [SQE views](../../sqe_view.go). [Resource lifetimes](resources.md) govern retention.

## Batch and Hot-Path Obligations

A batch reap with capacity and a visible CQE makes positive progress once it claims the copy. Shared consumers claim through CAS and `spin.Wait`, independently of submission locking. Copy before publishing the kernel head; publish before admitting the next consumer. Bound counts before narrowing, handle wrap without skips/repeats, and report overflow at the empty boundary.

For the exclusively claimed CQ snapshot, let `v = (tail - head) mod 2^32`. A batch with nonempty destination copies `n = min(len(dst), v)` entries and publishes `(head + n) mod 2^32`. This is a local copy/publication obligation; it supplies no scheduler or eventual-kernel-progress guarantee.

Measure allocation for the actual call shape: defaults, options, collected results, and stored closures can differ. The caller owns scheduling and retry.
