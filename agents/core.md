# Core Contracts

For each operation, identify what submission commits, which observation permits the next caller action, and who owns each resource until release.

## Boundary

Linux executes requests. `uring` handles setup, registration, SQE publication and CQE copying. The caller drives the ring, routes completions and sets retry, cancellation and protocol policy. Managed helpers advance when the caller dispatches their CQEs. [Package ownership](boundary/integration.md)

## Capabilities and Domains

Check field widths and reserved bits. Verify support for the opcode, ring setup and registrations in both kernel and wrapper. The default ring requires one owner goroutine locked to its OS thread from before `Start` through `Stop`. Select `MultiIssuers` for shared submission or thread migration. [Input domains](boundary/encoding.md#input-domains) · [Ring setup](boundary/encoding.md#setup-and-capabilities)

## Ownership

Identify who owns each descriptor, context and byte range through its last use. Keep Go storage reachable and address-stable during kernel access. `Wait` copies CQE fields before releasing the slot. Indirect and Extended contexts still borrow their targets. [Resource lifetimes](boundary/resources.md)

## Identity

Choose the smallest context that distinguishes requests needing different actions. On a mixed CQ, match raw `user_data` to its source before decoding a local context. Preserve that binding through deferred dispatch, notifications and cancellation lookups. [Request identity](boundary/sqe-cqe.md#request-identity) · [Managed cancellation](boundary/resources.md#managed-cancellation)

## Admission

Publishing an SQE admits the request. A full SQ refuses new work. Preserve a batch's accepted prefix on error. For independent requests, retry from the first unaccepted item. Linked SQEs must reach Linux in one submission. Account for effects before reissuing admitted work. `MsgRingFD` can install a file before reporting notification failure. Synchronous `RegisterFilesUpdate` can return a short count with a nil error. [Submission](boundary/sqe-cqe.md#submission) · [Linked deadlines](recipes/lifecycle.md#timers-and-linked-deadlines) · [File updates and transfers](boundary/files.md)

## Observations

Choose success reporting before submission when the result matters. Interpret `Res` by origin and opcode. A negative `MsgRing` value can be application data. For socket I/O, identify the transport. Advance a stream's byte offset by the matched positive send count. A UDP receive can fill its buffer on either an exact fit or truncation. `F_MORE` indicates that another CQE may follow. Preserve the API's `nil`, `iox.ErrWouldBlock`, `iox.ErrMore` and failure outcomes. [Completion dispatch](boundary/sqe-cqe.md#completion-origin) · [Datagram receives](boundary/protocols.md#datagram-receives)

An empty poll supplies no new result. When `Wait` returns `n > 0`, all `n` entries have left the CQ. If dispatch pauses, account for the unfinished action and every unhandled entry before reusing the destination. [Batch handling](boundary/sqe-cqe.md#batch-and-hot-path-obligations)

## Context

Use `tempo` for decisions that depend on earlier events. Use `cove` to check the current route, ownership or permission at each guarded attempt. Keep the condition valid through the effect. After revocation, continue accounting for earlier admissions and any resources they return. [Cancellation replies](recipes/lifecycle.md#request-and-observe) · [Guarded receipt delivery](recipes/protocol.md#deliver-a-write-receipt) · [Context checks](runtime/cove.md)

## Execution

A one-shot reply can resume a continuation once and yield a successor to drive. Repeated CQEs need a live route. On retry, continue from the first unfinished action. If listener setup consumes a CQE and its next submission blocks, retry the prepared SQE. If receipt enqueue would block after a completed write, retain the pending endpoint send for the next permitted attempt. [Listener setup](recipes/listener.md) · [Write receipt](recipes/protocol.md#deliver-a-write-receipt) · [Runtime roles](runtime/INDEX.md)

## Release

Retire each request, cancellation and buffer loan on its own evidence. Outstanding I/O still needs service after continuation disposal or session close. Drain requests, dispatch outstanding CQEs and quiesce subscriptions before serialized `Stop`. [Resources](boundary/resources.md) · [Lifecycle](recipes/lifecycle.md)

### One Observation, Several Obligations

A `SEND_ZC` data CQE with `F_MORE` reports the send result and promises a notification. Account for both before reusing the payload. On incremental receive, `F_BUF_MORE` keeps the selected buffer in use independently of request liveness. [Zero-copy send](recipes/buffers.md#zero-copy) · [Buffer ownership](boundary/resources.md#provided-buffers) · [Completion retention](boundary/sqe-cqe.md#completion-retention)

## Cost

Compare allocations, CPU and tail latency with direct code under the same admission, error, ownership, identity, cancellation and backpressure contracts. Measure first use, pool misses and steady state. Preserve required ordering, contention backoff and caller waits. [Cost checks](verify.md#cost)
