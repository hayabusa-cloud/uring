# One-Shot I/O

Admit one operation, match its completion, and deliver once. Optional roles: [kont](../runtime/kont.md), [takt](../runtime/takt.md), [cove](../runtime/cove.md).

## Establish Owners

Choose the ring owner and context mode. Retain descriptors, payloads, submission metadata, and typed roots through their final use. Use Direct context when its inline fields suffice; give each request an unambiguous live identity.

Check [opcode and setup requirements](../boundary/encoding.md). Go serialization does not pin a Linux task. Default single-issuer mode requires OS-thread affinity from before `Start` through `Stop`, with serialized submit, enter/wait, resize, and teardown.

## Admission

1. Retain the exact pending action and operands; submit once.
2. On pre-admission would-block, keep the action and owners, drive its progress source, then retry. Bound pending storage and define its full-capacity outcome.
3. For batches, record the accepted prefix immediately. Retry only the suffix; accepted items retain completion obligations.

If a computation has already run to suspension, retry from that suspension only while the adapter preserves it. Restarting the computation can repeat earlier effects. Check the [selected `takt` surface](../runtime/takt.md#choose-the-execution-surface) before relying on retry.

## Observe and Deliver

Drive submission and reap into a nonempty destination. An empty poll leaves deferred work with the caller. Revisit due submissions before the next poll. Use the caller's `iox.Backoff` when the turn advances no work and reaps zero CQEs. [Wait modes](../boundary/sqe-cqe.md#observation) · [Backoff](../boundary/integration.md#waiting-and-backoff)

Match the request instance; retain result, flags, and count. Copy Indirect/Extended facts before target mutation, reuse, or pool return, and retain payloads independently. Check dynamic context before protocol use or resumption.

Deliver success/failure once. Consume a `kont` suspension only once and retain its successor. Release each resource at its own final use. A short stream write leaves an unsent suffix; do not replay the accepted request.

## Validate

[File examples](../../examples/file_io_test.go) cover read/write and `TestBatchedNops`, which retries unaccepted IDs under SQ pressure. [Context examples](../../examples/context_test.go) cover all three modes.

Check success/failure, pre-admission would-block, partial admission, stale identity, retained completion, and [teardown with accepted work](lifecycle.md).
