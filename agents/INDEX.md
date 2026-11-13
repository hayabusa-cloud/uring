# uring Guide

<a id="uring-boundary-guide"></a>

[Package entry](../AGENTS.md) · [API overview](../README.md)

## Choose a Task

Read [core contracts](core.md), select a task and its required sources, then follow [verification](verify.md). The runtime guides describe optional integrations; choose the packages your caller needs.

<a id="choose-the-task"></a>

| Task | Read |
|------|------|
| Add or repair an operation adapter | [One-shot](recipes/oneshot.md), [encoding](boundary/encoding.md) |
| Drive a ring | [One-shot](recipes/oneshot.md), [takt](runtime/takt.md) |
| Set up a listener | [Listener](recipes/listener.md) |
| Cancel or drain a subscription | [Stream](recipes/stream.md), [lifecycle](recipes/lifecycle.md) |
| Use buffers, registered files, or zero-copy | [Buffers](recipes/buffers.md) |
| Use incremental receive | [Incremental receive](recipes/buffers.md#incremental-receive) |
| Retain completion data | [Retained completions](recipes/buffers.md#retained-completions) |
| Build a continuation adapter | [One-shot](recipes/oneshot.md), [kont](runtime/kont.md), [takt](runtime/takt.md) |
| Check context across suspension | [Protocol](recipes/protocol.md), [cove](runtime/cove.md) |
| Build a protocol or service | [Protocol](recipes/protocol.md), [sess](runtime/sess.md) |
| Submit to multiple targets | [Multiple targets](recipes/buffers.md#multiple-targets) |
| Review performance | [Cost](verify.md#cost), [waiting](boundary/integration.md#waiting-and-backoff) |
| Change zero-copy, cancellation, incremental buffers, CQ ownership or context packing | [Models, proofs and source mappings](../proofs/README.md) |
| Rewrite or audit the guide | [Review the guide](#review-the-guide) |

<a id="establish-scope"></a>

Name the boundary action, caller decision, retained resources, and release evidence. Select runtime roles by need; [integration](boundary/integration.md) identifies supporting package owners.

<a id="complete-the-task"></a>

Verify the selected contract, including failed guards, exhausted capacity, and unavailable features. The recipe supplies its acceptance cases.

## Review the Guide

Read core, verification, [boundary contracts](boundary/INDEX.md), [runtime guides](runtime/INDEX.md), [recipes](recipes/INDEX.md), and [sources](references.md). Preserve each contract's assumptions and repair incoming links when consolidating pages. Read [proof scope](../proofs/README.md) before changing formal claims; documented requirements alone establish no proof.
