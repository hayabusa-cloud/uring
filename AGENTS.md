# code.hybscloud.com/uring Agent Guide

This is the entry point to the guide set for using `code.hybscloud.com/uring` correctly. Use it when generating or reviewing caller-side code around the package boundary: runtimes, protocol stacks, services, and review work that needs a precise Linux `io_uring` boundary.

Read [`uring/README.md`](README.md) first for the package overview and API examples. Then use this guide to decide which facts belong at the boundary, which policy belongs in the caller layer, and which lifecycle transitions must remain visible.

## Purpose

Treat `code.hybscloud.com/uring` as a narrow Linux `io_uring` boundary. It sets up rings, encodes SQEs, observes CQEs, carries `user_data` identity, exposes capability evidence, and records ownership transitions visible at that boundary.

Keep caller policy in the layer that invokes `code.hybscloud.com/uring`: retry/backoff, poll cadence, scheduling, parking, routing, route retirement, parser state, protocol branching, completion routing, cancellation and timeout policy, safety checks, and service lifecycle. Use this guide for caller-side design and review, not as a package-maintenance manual.

The `INDEX.md` files under [`agents/`](agents/) are readable navigation: use them to choose the guide files required by a task. [`agents/references.md`](agents/references.md) is a natural-language research guide, and the [`agents/workflow/`](agents/workflow/) files are readable procedure: the gates, obligations, checklists, and staged loop you apply to a task. The remaining topic files under `boundary/`, `formalization/`, `lift/`, and `runtime/` carry the formal notation for tasks that need it, including abstraction from caller-side Go into the guide notation and compilation from checked notation back into Go. Theory terms in those files are useful only when they classify a concrete guide obligation: effect and resumption, context and coeffect, session frontier, runner observation, denotation, compilation, or verification evidence. This file is the readable entrypoint.

```text
beyond code.hybscloud.com/uring = broader systems built on the layer above: protocol stacks, services, applications
above code.hybscloud.com/uring  = the layer that calls the package directly: runtimes, event loops, adapters
caller layer = above ∪ beyond   = policy + protocol + routing + poll cadence + scheduling + cancellation/timeout policy
code.hybscloud.com/uring        = ring setup + SQE encode + CQE observe + ownership/capability facts
below code.hybscloud.com/uring  = Linux io_uring ABI + syscall/device/memory-order constraints
```

The caller layer has two parts. The layer *above* `code.hybscloud.com/uring` is the code that calls the package directly: runtimes, event loops, and adapters that ask the package to encode, submit, wait, observe, decode, recycle, or release one boundary action at a time. The layer *beyond* `code.hybscloud.com/uring` is the broader set of systems built on that layer, such as protocol stacks, services, and applications that never touch a ring directly. This guide and every file under `agents/` serve the whole caller layer: the same boundary facts, formalization, abstraction from caller-side Go, and compilation back to Go apply both to the direct above layer and to the wider beyond layer. The topic files abbreviate that caller layer as `C = caller(module) = A ∪ D`, with `A = above(module)` and `D = beyond(module)`.

## Required Reading

For code generation, review, or repair work above or beyond `code.hybscloud.com/uring`, read the complete `code.hybscloud.com/iox` package first. The outcome rules in this guide rely on that package's model.

When a task uses a fact owned by another package, read that package before using the fact:

- `code.hybscloud.com/iox` for stream, datagram, outcome, byte-progress, and `Backoff` contracts.
- `code.hybscloud.com/zcall` for Linux entry results and errno facts below the boundary.
- `code.hybscloud.com/iofd` for descriptor ownership and close lifecycle.
- `code.hybscloud.com/sock` for socket descriptors and address facts.
- `code.hybscloud.com/iobuf` for buffer pools, alignment, buffer views, and recycle ownership.
- `code.hybscloud.com/spin` for spin wait, yield, and spin lock primitives.
- `code.hybscloud.com/lfq` for caller-owned bounded FIFO mailbox and queue handoff when work crosses goroutines before reaching the serialized ring owner.
- `code.hybscloud.com/framer` for frame-boundary and codec facts when framed byte streams are part of the task.
- `code.hybscloud.com/cove` for explicit context, requirements, and safety evidence.
- `code.hybscloud.com/kont` for suspension and one-shot resumption.
- `code.hybscloud.com/sess` for protocol frontiers, branching, endpoint transitions, and session close semantics.
- `code.hybscloud.com/takt` for runner movement, polling, resumption, completion memory, completion routing, and route-indexed stream carriers.

For broad guide edits, runtime-utilization edits, or review work that changes the cross-package model, read the complete current source of every package whose facts the edited guide relies on. In practice, this usually means `code.hybscloud.com/iox`, `code.hybscloud.com/iofd`, `code.hybscloud.com/sock`, `code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, and `code.hybscloud.com/sess` before changing `uring/AGENTS.md` or any file under `uring/agents/`. Extend that set with `code.hybscloud.com/zcall`, `code.hybscloud.com/iobuf`, `code.hybscloud.com/spin`, `code.hybscloud.com/lfq`, or `code.hybscloud.com/framer` whenever the edited guide cites syscall, buffer, spin/yield, mailbox, or framing facts.

Before generating or reviewing code, read every guide file needed by the task. A file is needed when its topic names a fact, action, resource, outcome, frontier, policy, or obligation used by that task.

Read in this order:

1. Guide index: [`agents/INDEX.md`](agents/INDEX.md)
2. Workflow intake: [`agents/workflow/INDEX.md`](agents/workflow/INDEX.md), [`agents/workflow/boundary-gates.md`](agents/workflow/boundary-gates.md), and [`agents/workflow/task-checklists.md`](agents/workflow/task-checklists.md)
3. Formalization: [`agents/formalization/INDEX.md`](agents/formalization/INDEX.md), then every indexed file under [`agents/formalization/`](agents/formalization/)
4. Obligation checks: [`agents/workflow/proof-obligations.md`](agents/workflow/proof-obligations.md) and [`agents/workflow/task-loop.md`](agents/workflow/task-loop.md)
5. Boundary topics: [`agents/boundary/INDEX.md`](agents/boundary/INDEX.md), [`agents/boundary/sqe-cqe.md`](agents/boundary/sqe-cqe.md), [`agents/boundary/resources.md`](agents/boundary/resources.md), [`agents/boundary/integration.md`](agents/boundary/integration.md), and [`agents/boundary/protocols.md`](agents/boundary/protocols.md)
6. Runtime utilization: [`agents/runtime/INDEX.md`](agents/runtime/INDEX.md), then every indexed file under [`agents/runtime/`](agents/runtime/) when the task uses `code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, or `code.hybscloud.com/sess`
7. Saved lift: [`agents/lift/INDEX.md`](agents/lift/INDEX.md), then every indexed file under [`agents/lift/`](agents/lift/)
8. References when a claim needs external verification: [`agents/references.md`](agents/references.md)

For guide edits or broad caller-side work above or beyond `code.hybscloud.com/uring`, read every indexed topic file before changing the guide or generating code.

## Outcome Discipline

Check visible result evidence before classifying the control outcome. Counts, returned descriptors, selected buffers, flags, identity, ownership transfer, and capability facts are evidence; the `error` value is the control component.

Use `code.hybscloud.com/iox` for outcome classification:

- `nil` means the observed operation step completed at this boundary.
- `ErrWouldBlock` from `code.hybscloud.com/iox` means no progress is visible now; caller code decides how to wait, yield, back off, or poll again.
- `ErrMore` from `code.hybscloud.com/iox` means a successor observation remains possible for the same live operation; it does not by itself prove positive byte progress.
- Treat any other error as a boundary failure; preserve the source fact, including kernel `-errno` when it came from `CQE.Res < 0`.

Keep `IORING_CQE_F_MORE` separate from `ErrMore`. If `CQE.Res < 0` and `IORING_CQE_F_MORE` is present, classify the operation as failure from `CQE.Res` and keep the `MORE` flag as frontier evidence. Do not convert that observation into `ErrMore`, byte progress, or terminal completion.

For a non-negative operation CQE, `IORING_CQE_F_MORE` means the observed step completed and the operation frontier remains live. Caller code may project that live frontier into `ErrMore` from `code.hybscloud.com/iox` only when its own protocol or outcome contract explicitly says so.

Keep release evidence separate from operation-control evidence. Zero-copy notification CQEs and terminal no-notification fallback release a carrier through the release frontier; they do not redefine the operation outcome.

## Boundary Placement

Write each caller-facing adapter around one explicit kernel-facing action: encode, submit, wait, observe, decode, cancel, resize, provide, recycle, release, close, or stop. The adapter should expose that boundary step without taking over the caller's loop.

When a helper starts choosing a route, retry/backoff step, poll cadence, scheduler action, parser branch, completion route, cancellation or timeout policy, callback policy, or service policy, split the helper. Pass only the boundary facts into `code.hybscloud.com/uring`, and keep the caller-owned decision above it.

Use `Backoff` from `code.hybscloud.com/iox` in caller-owned no-progress policy. A typical caller waits after `ErrWouldBlock` or after observing no CQEs, then resets the backoff after a caller-visible forward signal: a reaped CQE, a boundary-state transition, a live frontier, terminal completion, or another caller-visible state transition.

Use `code.hybscloud.com/spin` for spin wait, yield, and spin lock primitives. Do not replace it with ad hoc runtime-yield loops in caller-side code built on this boundary.

## Ownership And Lifecycle

Model ownership as affine: a resource is either still owned by the caller, pending in the kernel, returned by a visible observation, released, recycled, closed, or stopped. Do not duplicate ownership by convenience API shape.

Keep `user_data` stable while any CQE for the submitted operation may still arrive. For multishot operations, identity remains live until the terminal frontier is observed.

Borrowed CQE views are observations, not owned payloads. If a completion needs to outlive the reap loop, copy the facts that need to survive: `CQE.Res`, flags, `user_data`, selected-buffer metadata, and any decoded identity needed by caller code.

Buffers, fixed buffers, provided buffers, context entries, listener operations, zero-copy trackers, zero-copy receive carriers, and multishot subscriptions need explicit release or recycle points. Do not hide completion pools, GC-root tables, metadata routes, or lifecycle policy behind a caller-facing adapter.

## Protocol And Runtime Integration

Use the caller-layer packages according to the role they own:

- Use `code.hybscloud.com/kont` when caller code needs explicit one-shot suspension and resumption. Do not represent a multishot `code.hybscloud.com/uring` operation as one reusable `Suspension` from `code.hybscloud.com/kont`; route stream frontiers through caller-owned stream carriers instead.
- Use `code.hybscloud.com/cove` when explicit context, requirements, safety evidence, or step-indexed observation must cross a suspension boundary. Keep requirement checks explicit with the caller; a context-carrying suspension does not make a hidden boundary policy decision.
- Use `code.hybscloud.com/takt` when caller code needs runner movement, completion memory, polling, error-aware stepping, or route-indexed stream carriers. `Advance` and `AdvanceSuspension` resume on dispatcher-level `ErrMore`; generic `Loop` rejects completion-level `ErrMore` as unsupported multishot; `SubscriptionLoop` is the carrier for live stream routes.
- Use `code.hybscloud.com/sess` when protocol branching, endpoint frontiers, paired protocol execution, or error-aware endpoint stepping sits above the completion stream. Its transport backpressure is `ErrWouldBlock` from `code.hybscloud.com/iox`; `ErrMore` is outside the endpoint transport domain, so live multishot routing must be classified before it reaches a session endpoint.

These packages are general caller-layer tools. The obligations in this guide apply when caller code uses them to wrap or drive a `code.hybscloud.com/uring` boundary action. Error-aware forms from `code.hybscloud.com/takt` and `code.hybscloud.com/sess` carry caller protocol policy; they do not change what a `code.hybscloud.com/uring` boundary helper may own. The detailed utilization guides for these packages live under [`agents/runtime/`](agents/runtime/), starting from [`agents/runtime/INDEX.md`](agents/runtime/INDEX.md); those files also name the package source files and coding obligations used for review.

Protocol stacks built above or beyond `code.hybscloud.com/uring` can treat the package as a shallow boundary handler: it exposes kernel observations and returns control to the caller. Protocol parsing, retries, route state, and terminal policy stay in the caller's protocol or runtime layer. The handler discipline is defined in [`agents/boundary/protocols.md`](agents/boundary/protocols.md) and restated in each file under [`agents/runtime/`](agents/runtime/) for the package it covers.

In Go, the caller layer expresses this discipline through small handlers and closures. A handler consumes exactly one visible event and returns control to its caller; it does the protocol work for that one event and nothing more. Keep handler registration and dispatch tables caller-owned. Three rules keep closures shallow: capture copied completion facts or caller-owned state, never a borrowed CQE view; build handler tables before the event loop starts, so hot-path dispatch performs no allocation and builds no closures; and keep repetition over successive completions as an explicit caller loop, never a hidden loop inside the boundary.

For original network protocol stacks, define the protocol frontier first, then bind each kernel-facing action to one `code.hybscloud.com/uring` boundary action. Preserve packet/frame boundaries through `code.hybscloud.com/framer` when the stream transport needs message boundaries.

## Workflow

The detailed workflow lives in [`agents/workflow/task-loop.md`](agents/workflow/task-loop.md). The short version is:

```text
analyze -> formalize -> typecheck -> reduce -> prove -> compile_to_go -> lift -> verify
```

When reviewing existing Go code, lift the public Go surface first:

```text
analyze -> lift -> formalize -> typecheck -> reduce -> prove -> compile_to_go -> verify
```

After each completed `formalize`, `typecheck`, `reduce`, `prove`, and `lift` stage, save or update the task lift under `path("uring/agents/lift")` unless the user specifies another lift path. In the lift files, `lift(go_surface) = ⟦go_surface⟧⁻¹_U`; the completed `lift` stage is the saved checkpoint for that abstraction. Keep each lift file topic-specific and indexed.

Before accepting code generated or reviewed with this guide, check these points:

- The code reads all package-owned facts it uses.
- SQE/CQE mechanics stay at the `code.hybscloud.com/uring` boundary.
- Caller policy stays in the caller layer outside the `code.hybscloud.com/uring` boundary.
- `nil`, `ErrWouldBlock`, `ErrMore`, failures, byte/count evidence, flags, and release observations remain distinct.
- `user_data` identity is stable until the operation is no longer live.
- Ownership epochs are visible before submit, while pending, after each CQE, after terminal frontier, and before recycle/release/close/stop.
- Hot boundary paths do not hide allocation, wait loops, route state, parser policy, cancellation or timeout policy, or service policy.

## References

Use [`agents/references.md`](agents/references.md) for web research and external-knowledge verification. Prefer primary sources for Linux ABI behavior, `io_uring` feature details, kernel implementation claims, Go language/toolchain facts, and formal-verification theory. Reduce external claims back into the local boundary model before using them in code or guide edits.
