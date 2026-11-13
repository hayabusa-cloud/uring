# Focused Boundary Proofs

These Lean proofs check models of five `uring` mechanisms. They use only Lean’s standard library. Building `uring` does not require Lean.

| Model | Go source | Checked property |
|-------|-----------|------------------|
| [ZeroCopy](ZeroCopy.lean) | [zc_tracker.go](../zc_tracker.go) | Completion and release signals occur at most once and require the right evidence |
| [Cancellation](Cancellation.lean) | [multishot.go](../multishot.go), [cancel_linux.go](../cancel_linux.go) | Publication and pending cancellation retain the original context; pool return occurs at most once |
| [Incremental](Incremental.lean) | [incremental_linux.go](../incremental_linux.go) | Per-ID offsets, fragment bounds, retention and buffer return |
| [CompletionQueue](CompletionQueue.lean) | [io_uring_linux.go](../io_uring_linux.go) | Exclusive copying yields a contiguous prefix before head publication; bounded counter wrap is sound |
| [Context](Context.lean) | [ctx.go](../ctx.go) | Direct-field recovery, signed FD bounds, flags replacement and pointer-tag separation |

## Check

Use the Lean version specified in [lean-toolchain](lean-toolchain). From this directory, run:

```sh
lean --version
lake build
lake env lean -DwarningAsError=true Audit.lean
```

[Audit.lean](Audit.lean) prints the axioms used by each listed theorem, including dependencies. Check that the output contains only Lean's standard logical axioms (`propext`, `Quot.sound`, `Classical.choice`), with no `sorryAx`, custom axioms or native-evaluation axioms. Inspect this output as well as the build result. See [Lean proof validation](https://lean-lang.org/doc/reference/latest/ValidatingProofs/).

## Zero-Copy Completion and Release

One trace concerns one initialized request lifetime. `UInt32` represents result bits, preserving negative errno bit patterns without interpreting them. `step` models a successful atomic transition or an observed no-op branch.

| Go | Model |
|----|-------|
| `zcStateSubmitted`, `zcStateEarlyNotified`, `zcStateCompleted`, `zcStateNotified` | The four `Phase` constructors |
| `markCompleted(result)` | `dataMore result`; outputs completion/release signals |
| `markTerminalData(result)` | `dataFinal result`; a successful transition emits both signals |
| `markNotification()` | `notification`; may emit release, never a data completion |
| `dispatchCQE` | Tests NOTIF first, then data MORE, otherwise terminal data |
| `History` | Proof-only event evidence and signal counts; no Go representation |

The transition table is transcribed from those functions. It does not assume the safety invariant it proves. Review this mapping whenever the Go functions, context lifetime or dispatch contract changes.

| Property | Theorem |
|----------|---------|
| At most one completion and release over any finite history | `at_most_once` |
| No release before a completion signal | `release_requires_completion` |
| Release requires data plus notification, or terminal data | `release_requires_evidence` |
| Retired state emits nothing and remains unchanged | `terminal_absorbing` |
| Accepted result remains stable through later events | `accepted_result_stable`, `result_stable_through_trace` |
| Both notification orders and terminal fallback finish | `notification_orders`, `terminal_fallback` |

Notification before data is a tolerated tracker input order. This does not claim that Linux publishes notification CQEs before data CQEs. Repeated modeled events are permitted to strengthen the signal-count theorem; real callers must never dispatch through a context after it has returned to the pool.

The [liburing SEND_ZC contract](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_send_zc.3) separates the data result from notification-based reuse. A terminal data CQE without MORE takes the tracker's no-notification path. Notification fields do not replace the original send result.

## Cancellation Retirement

One trace begins with an admitted original multishot request and its owned context.

| Source operation | Model event or state |
|------------------|----------------------|
| `cancelSubmit.CompareAndSwap`, active recheck | `acquire`, `recheck` |
| `cancel.prepare` and submission result | `prepare`, then `accept` or `reject` |
| Post-submit `tryCancel`, deferred gate release | `returned`, `leave` |
| Original terminal CQE, including suppressed callbacks | `originalCQE` |
| Accepted cancellation's CQE | `cancelCQE` |
| Retirement guard and `ext.Swap(nil)` | `retire`; `owned` and ghost `returns` |

`Cancel.prepared` and `outstanding` correspond to a pending marker; `outstanding` and `observed` also record admission evidence. An early cancellation CQE may occur between `accept` and `returned`. Rejected admission clears preparation without creating a CQE obligation. A delayed caller may acquire the gate after retirement, but cannot pass the active recheck.

`at_most_once`, `retirement_requires_observation` and `publisher_keeps_context` hold over arbitrary finite event histories. `failed_admission_has_no_credit` checks failure cleanup; `completion_orders` checks both CQE orders. Invalid events stutter. The first active read is deliberately omitted, admitting extra delayed gate acquisitions. The retirement guard and pool exchange are one abstract transition: their refinement from individual Go loads and the exchange remains unproved. Valid cancellation-CQE causality, matched identities and serialized completion dispatch are premises; callbacks and callback suppression affect delivery, not the modeled terminal evidence. See [Linux cancellation](https://github.com/torvalds/linux/blob/v6.18/io_uring/cancel.c).

## Incremental Buffers

`admissible` combines the positive-result branch in `handleResolvedCQE` with the metadata and remaining-space checks in `bufferFromCQE`; `fragment` describes the borrowed interval. `advance` follows both branches of `handleResolvedCQE`. With `F_BUF_MORE`, the selected offset advances by the result; without it, that ID resets. Zero/error results reset only when `F_BUFFER` is present, `F_BUF_MORE` is absent and the ID is valid.

`offsets_bounded` covers arbitrary finite histories from zero offsets, including interleaved IDs and rejected metadata. `other_ids_unchanged`, `fragment_bounds`, `selected_offset` and `retained_adjacency` establish isolation, allocation bounds and adjacent retained fragments. `nonpositive_return` and `nonpositive_retained` cover zero/error paths.

The model assumes one ordered dispatch per CQE, matching registration, and arithmetic representable in the Go fields. `entries * size ≤ backing` is explicit in the allocation theorem. Natural-number arithmetic does not verify machine overflow or pointer arithmetic. The source's final slice checks remain necessary. Kernel writes, application reads and republishing are outside the model. See [Linux incremental buffer consumption](https://github.com/torvalds/linux/blob/v6.18/io_uring/kbuf.c).

## CQ Claim, Copy and Publication

`produce` models kernel publication within ring capacity; `claim` selects `min(limit, available)` under exclusive ownership. `copy` appends that interval to a ghost history; `publish` advances the head only from the copied phase. These map to `claimCQ`, the copies in `wait`/`waitBatch`, and `publishCQ`. Shared consumers obtain exclusivity through the successful CAS; single-owner consumers supply it externally. Failed CAS and `spin` backoff stutter without a progress claim.

`copied_prefix` and `no_duplicate_copies` cover arbitrary finite histories. `publication_follows_copy` and `copies_are_published_by_kernel` bound head publication and copying. The batch copy and the two publication stores are abstract transitions; Go/kernel memory ordering, slot contents and instruction-level interleavings are not proved.

`distance_across_wrap` recovers the unsigned 32-bit distance when `head ≤ tail` and `tail - head < 2^32`. `claim_sequence_unique` excludes stale equality only when logical claim advances are less than `2^63`. The busy bit leaves a finite sequence space; the proof does not claim unbounded ABA freedom.

## Context Representation

| Bits | Direct mode | Pointer modes |
|------|-------------|---------------|
| 0–7 | Opcode | Address |
| 8–15 | Flags | Address |
| 16–31 | Buffer group | Address |
| 32–61 | Signed 30-bit FD | Address |
| 62–63 | Tag 0 | Tag 1 (indirect) or 2 (extended) |

`pack` and the projections model `PackDirect` and its getters; `withFlags` models direct-mode `WithFlags`. `direct_roundtrip`, `direct_injective` and `flags_replacement` establish recovery, distinct encodings and field isolation. `signed_fd_roundtrip` requires `-2^29 ≤ fd < 2^29`; it does not promise recovery of every `int32` value.

`pointer_roundtrip` and `modes_disjoint` cover addresses below `2^62`. Division, remainder and addition model masks, shifts and disjoint bitwise ORs; that arithmetic correspondence is reviewed, not mechanically translated from Go. The theorems establish neither pointer validity nor target lifetime.

## Shared Limits

The proved objects are the Lean models. Their mappings to Go are reviewed, not mechanically proved. Applying the results requires correctly matched identity, valid initialized storage, the stated atomic-step interpretation, serialized callback/pool ownership and kernel contracts. Caller-held typed roots and stable payload addresses remain necessary.

These proofs do not establish Go memory ordering, GC safety, callback ordering or reentrancy, CAS progress, eventual completion, Linux correctness or runtime cost. The Go/kernel tests and [caller contracts](../agents/core.md) retain their separate obligations.
