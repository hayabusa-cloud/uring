# Core Contracts

These contracts describe each boundary action's responsibilities. Choose runtime packages by need and follow the linked details and assumptions.

## Boundary

Linux executes requests; `uring` exposes kernel facts and SQE/CQE mechanics. The caller owns admission policy, retry, scheduling, routing, parsing, protocol, cancellation, and service lifetime. Split roles only for distinct obligations. [Package ownership](boundary/integration.md)

## Observations

Match the CQE's origin first. A negative result can be a local operation error or the sender's message value. For a local operation, interpret count, error, `MORE` and release evidence independently. Preserve positive progress and the owning API's `nil`, `iox.ErrWouldBlock`, `iox.ErrMore` and failure meanings. [Completion dispatch](boundary/sqe-cqe.md#completion-origin)

## Ownership

Give each context, descriptor, buffer, and borrow one owner and validity interval. Retain asynchronous memory through typed roots and stable addresses. Integer addresses and descriptor copies do not retain referents; copy needed facts before target invalidation. [Resource lifetimes](boundary/resources.md)

## Identity

Give each request-specific action an unambiguous submission identity. A numeric token suffices while lookups for queued events, notifications and cancellations still select the originating request. [Request identity](boundary/sqe-cqe.md#request-identity) · [Managed cancellation](boundary/resources.md#managed-cancellation)

## Admission

Retry only unaccepted work and retain its owners. Once a request is admitted, its result and documented effects govern whether it is safe to issue another. For example, `MsgRingFD` can report an error after installing the target file. [Batch admission](recipes/oneshot.md#admission) · [Inter-ring file transfers](boundary/resources.md#inter-ring-file-transfers)

## Capabilities and Domains

Check opcode domains, reserved fields, setup flags, and feature evidence. Kernel support does not establish wrapper support. Default single-issuer mode requires its pinned Linux-task lifetime; serialized goroutines can migrate. [Encoding and capabilities](boundary/encoding.md)

## Context

Check mutable route, buffer, FD, protocol, and shutdown facts at use. Carrying context performs no check. Reusing an earlier check requires a valid relation between the old and current context, including generation checks; rejection returns to caller policy. [cove](runtime/cove.md)

## Execution

Choose a one-shot continuation, stream route, or protocol endpoint; retain successors. Distinguish dispatch, admission, poll status, and completion outcomes. Local endpoint compatibility alone establishes no global progress. [Runtime roles](runtime/INDEX.md) · [Protocol boundary](boundary/protocols.md)

## Release

Release each SQ slot, context, payload, selected buffer, request, cancellation context, notification, and refill credit under its own condition. Continuation disposal and session close do not cancel I/O. Serialize final destruction after required drain. [Resources](boundary/resources.md) · [Lifecycle](recipes/lifecycle.md)

## Cost

Compare equivalent direct code under identical admission, error, context, ownership, cancellation, and backpressure contracts. Measure the [actual costs](verify.md#cost), including first use and pool misses. Preserve contention and caller waiting backoff. Static representation and pooling prove no cost result.

## One Observation, Several Obligations

SEND_ZC data reports a result and may promise notification. Consume the result once; retain the payload through release evidence. Copying the CQE releases neither context nor payload. [Buffers](recipes/buffers.md#zero-copy) · [Runtime roles](runtime/INDEX.md)
