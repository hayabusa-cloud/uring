# Core Contracts

These contracts describe each boundary action's responsibilities. Choose runtime packages by need and follow the linked details and assumptions.

## Boundary

Linux executes requests; `uring` exposes kernel facts and SQE/CQE mechanics. The caller owns admission policy, retry, scheduling, routing, parsing, protocol, cancellation, and service lifetime. Split roles only for distinct obligations. [Package ownership](boundary/integration.md)

## Observations

Keep counts, errors, flags, identity, and release evidence separate. Preserve the owning API's `nil`, `iox.ErrWouldBlock`, `iox.ErrMore`, and failure meanings; consume positive progress even when another step is required. Negative `CQE.Res` is `-errno`; `MORE` cannot make it successful. Distinguish request continuation from notification. [Completion contract](boundary/sqe-cqe.md#observation)

## Ownership

Give each context, descriptor, buffer, and borrow one owner and validity interval. Retain asynchronous memory through typed roots and stable addresses. Integer addresses and descriptor copies do not retain referents; copy needed facts before target invalidation. [Resource lifetimes](boundary/resources.md)

## Identity

Match the request instance, including context/generation, through all required original, cancellation, and notification observations. Numeric identity alone is insufficient. [Retention](boundary/sqe-cqe.md#completion-retention) · [Cancellation](boundary/resources.md#managed-cancellation)

## Admission

Retry only unaccepted work; retain its owners on retryable paths. Never replay an accepted prefix. Dispatch and asynchronous submission have different [admission contracts](runtime/takt.md#choose-the-execution-surface). [Boundary](boundary/sqe-cqe.md)

## Capabilities and Domains

Check opcode domains, reserved fields, setup flags, and feature evidence. Kernel support does not establish wrapper support. Explicit single-issuer mode requires its pinned Linux-task lifetime; serialized goroutines can migrate. [Encoding and capabilities](boundary/encoding.md)

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
