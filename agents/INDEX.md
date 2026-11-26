# uring Guide

<a id="uring-boundary-guide"></a>

[Package entry](../AGENTS.md) · [API overview](../README.md)

## Choose a Task

Read [core contracts](core.md), select a task and its required sources, then follow [verification](verify.md). The runtime guides describe optional integrations; choose the packages your caller needs.

<a id="choose-the-task"></a>

| Task | Read |
|------|------|
| Add or repair an operation adapter | [One-shot](recipes/oneshot.md), [encoding](boundary/encoding.md) |
| Drive a ring directly | [One-shot](recipes/oneshot.md), [file write/read example](../examples/file_io_test.go#L30) |
| Resize a live ring | [Resize requirements, effective sizes and failure](boundary/encoding.md#resize-a-live-ring) |
| Choose a wait, suppress success CQEs or dispatch a mixed CQ | [Wait modes](boundary/sqe-cqe.md#observation), [suppressed completions](boundary/sqe-cqe.md#suppressed-completions), [completion origin](boundary/sqe-cqe.md#completion-origin) |
| Bind a CQE to a request or retain it for later | [Request identity](boundary/sqe-cqe.md#request-identity), [changing a poll ID](boundary/sqe-cqe.md#change-a-poll-id), [retained fields](boundary/sqe-cqe.md#completion-retention) |
| Set up a listener | [Listener](recipes/listener.md) |
| Cancel or drain a subscription | [Stream](recipes/stream.md), [lifecycle](recipes/lifecycle.md) |
| Set a deadline or start, update or remove a timer | [Independent timers and linked deadlines](recipes/lifecycle.md#timers-and-linked-deadlines), [updates and removal](recipes/lifecycle.md#update-or-remove-a-timer) |
| Use buffers, registered files, or zero-copy | [Buffers](recipes/buffers.md), [receive-bundle handoff](recipes/buffers.md#hand-off-a-receive-bundle) |
| Use incremental receive | [Incremental receive](recipes/buffers.md#incremental-receive) |
| Retain completion data | [Retained completions](recipes/buffers.md#retained-completions) |
| Build a continuation adapter | [One-shot](recipes/oneshot.md), [kont](runtime/kont.md), [takt](runtime/takt.md) |
| Check context across suspension | [Protocol](recipes/protocol.md), [cove](runtime/cove.md) |
| Deliver a framed message or a write receipt | [Protocol recipe](recipes/protocol.md), [stream framing](boundary/protocols.md#stream-framing), [write receipt](recipes/protocol.md#deliver-a-write-receipt) |
| Receive UDP datagrams and their metadata | [Truncation, empty datagrams and header reuse](boundary/protocols.md#datagram-receives), [deliver a final value under backpressure](recipes/stream.md#dispatch-each-observation) |
| Submit to multiple targets | [Multiple targets](recipes/buffers.md#multiple-targets) |
| Review performance | [Cost](verify.md#cost), [waiting](boundary/integration.md#waiting-and-backoff) |
| Change zero-copy, cancellation, bundle or incremental buffers, CQ ownership or context packing | [Models, proofs and source mappings](../proofs/README.md) |
| Rewrite or audit the guide | [Review the guide](#review-the-guide) |

<a id="establish-scope"></a>

Name the boundary action, caller decision, retained resources, and release evidence. Select runtime roles by need; [integration](boundary/integration.md) identifies supporting package owners.

<a id="complete-the-task"></a>

Verify the selected contract, including failed guards, exhausted capacity, and unavailable features. The recipe supplies its acceptance cases.

## Review the Guide

Read core, verification, [boundary contracts](boundary/INDEX.md), [runtime guides](runtime/INDEX.md), [recipes](recipes/INDEX.md), and [sources](references.md). Preserve each contract's assumptions and repair incoming links when consolidating pages. Read [proof scope](../proofs/README.md) before changing formal claims; documented requirements alone establish no proof.
