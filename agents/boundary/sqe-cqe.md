# SQE/CQE Boundary

An adapter exposes one kernel-facing action and its observations. Apply the [encoding contract](encoding.md); preserve the independent completion facts below.

## Submission

Check public and opcode-specific input domains before narrowing, indexing, or writing the SQE. Initialize required fields and clear rejected fields. Retain identity through completion, release, and outstanding [cancellation lookups](resources.md#managed-cancellation).

Ordinary Go ownership uses the default task-migratable mode. Explicit single-issuer mode requires one locked OS thread from before `New` through `Stop`, with serialized submit-state operations. Goroutine serialization does not establish Linux task identity.

## Observation

Copy the CQE descriptor before advancing the CQ head. Interpret its fields independently:

| Evidence | Meaning |
|----------|---------|
| Nonnegative operation `Res` | The observed step succeeded; the value retains its opcode-specific meaning |
| `Res == -EAGAIN` | `iox.ErrWouldBlock`, even when `MORE` is present |
| Other negative operation `Res` | The corresponding failure |
| `IORING_CQE_F_MORE` | Same-request successor evidence; it does not change result classification or prove byte progress |
| Notification CQE | Release evidence interpreted with the matching request history |

Only an explicit caller contract may translate request liveness into `iox.ErrMore`. Match the current request instance and individual completion; reused `user_data` alone is insufficient.

## Completion Retention

`Wait` copies `Res`, `Flags`, and packed `SQEContext`. Retention depends on context mode:

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
