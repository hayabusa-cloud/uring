# uring Guide

<a id="uring-boundary-guide"></a>

[Package entry](../AGENTS.md) · [API overview](../README.md)

## Choose a Task

<a id="choose-the-task"></a>
<a id="establish-scope"></a>

Read the [core contracts](core.md). Identify the request, caller decision and release evidence, then choose a task.

| Task | Start here |
|------|------------|
| Drive a ring directly | [One-shot I/O](recipes/oneshot.md), [file write/read example](../examples/file_io_test.go#L30) |
| Decide what to retry after would-block | [Submission or CQE](recipes/oneshot.md#admission), [local or backend refusal in takt](runtime/takt.md#drive-a-deferred-request), [blocked endpoint action](runtime/sess.md#outputs-and-ownership) |
| Resize a live ring | [Resize requirements, effective sizes and failure](boundary/encoding.md#resize-a-live-ring) |
| Add or repair an operation adapter | [SQE encoding](boundary/encoding.md), [submission](boundary/sqe-cqe.md#submission) |
| Choose a wait, suppress success CQEs or dispatch a mixed CQ | [Wait modes](boundary/sqe-cqe.md#observation), [suppressed completions](boundary/sqe-cqe.md#suppressed-completions), [completion origin](boundary/sqe-cqe.md#completion-origin) |
| Bind a CQE to a request or retain it for later | [Request identity](boundary/sqe-cqe.md#request-identity), [changing a poll ID](boundary/sqe-cqe.md#change-a-poll-id), [retained fields](boundary/sqe-cqe.md#completion-retention) |
| Set up a listener or start a receive on an accepted socket | [Listener setup](recipes/listener.md), [accepted connections](recipes/listener.md#accept-connections) |
| Dispatch repeated completions or withdraw an accept handler | [Stream](recipes/stream.md), [accept results](boundary/files.md#accepted-sockets) |
| Cancel a request, drain a subscription or stop the ring | [Lifecycle](recipes/lifecycle.md), [managed cancellation](boundary/resources.md#managed-cancellation) |
| Set a deadline or start, update or remove a timer | [Independent timers and linked deadlines](recipes/lifecycle.md#timers-and-linked-deadlines), [updates and removal](recipes/lifecycle.md#update-or-remove-a-timer) |
| Use a fixed file, replace its slot or pass it to another ring | [Fixed-file lookup](boundary/files.md#fixed-file-lookup), [slot updates](boundary/files.md#updating-slots), [inter-ring transfers](boundary/files.md#inter-ring-file-transfers) |
| Use buffers, registered files, or zero-copy | [Buffers](recipes/buffers.md), [receive-bundle handoff](recipes/buffers.md#hand-off-a-receive-bundle) |
| Use incremental receive | [Incremental receive](recipes/buffers.md#incremental-receive) |
| Retain completion data | [Retained completions](recipes/buffers.md#retained-completions) |
| Submit to multiple targets | [Multiple targets](recipes/buffers.md#multiple-targets) |
| Deliver a framed message or a write receipt | [Protocol recipe](recipes/protocol.md), [stream framing](boundary/protocols.md#stream-framing), [write receipt](recipes/protocol.md#deliver-a-write-receipt) |
| Receive UDP datagrams and their metadata | [Truncation, empty datagrams and header reuse](boundary/protocols.md#datagram-receives), [deliver a final value under backpressure](recipes/stream.md#dispatch-each-observation) |
| Review performance | [Cost](verify.md#cost), [waiting](boundary/integration.md#waiting-and-backoff) |
| Change zero-copy, cancellation, bundle or incremental buffers, CQ ownership or context packing | [Models, proofs and source mappings](../proofs/README.md) |
| Rewrite or audit the guide | [Review the guide](#review-the-guide) |

[Suspend once with kont](runtime/kont.md) · [Track receipt eligibility with tempo](runtime/tempo.md#track-a-write-receipt) · [Guard a native send with cove](runtime/cove.md#guard-a-send) · [Drive a backend or stream with takt](runtime/takt.md) · [Advance an endpoint with sess](runtime/sess.md)

<a id="complete-the-task"></a>

Run the selected recipe's cases and follow the [verification requirements](verify.md).

## Review the Guide

Review the [core contracts](core.md) and every page in the three inventories. [Boundary definitions](boundary/INDEX.md) · [Caller recipes](recipes/INDEX.md) · [Runtime roles](runtime/INDEX.md)

Check the [source catalog](references.md), any affected [proof assumptions and source mappings](../proofs/README.md), and the [guide checks](verify.md#guide-checks).
