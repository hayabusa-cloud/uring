# One-Shot I/O

Admit one operation, match its completion, and deliver once. Optional roles: [kont](../runtime/kont.md), [takt](../runtime/takt.md), [cove](../runtime/cove.md).

## Establish Owners

Choose the ring owner and context mode. Retain descriptors, payloads, submission metadata, and typed roots through their final use. Use Direct context when its inline fields suffice; give each request an unambiguous live identity.

Check [opcode and setup requirements](../boundary/encoding.md). Go serialization does not pin a Linux task. Explicit single-issuer mode requires OS-thread affinity from before `New` through `Stop`, with serialized submit, enter/wait, resize, and teardown.

## Admission

1. Retain the exact pending action and operands; submit once.
2. On pre-admission would-block, keep the action and owners, drive its progress source, then retry. Bound pending storage and define its full-capacity outcome.
3. For batches, record the accepted prefix immediately. Retry only the suffix; accepted items retain completion obligations.

SQ pressure is not completion. `takt.Advance` retains its suspension on would-block/failure; `takt.Loop` discards on every submission error. Retryable asynchronous admission needs explicit pending state. Replaying a computation prefix can duplicate earlier effects.

## Observe and Deliver

Drive submission and CQ reap under caller cadence. Empty `Wait` or would-block uses `iox.Backoff`; reset after `n > 0`, before dispatch. Empty CQ does not discharge deferred kernel work or service obligations.

Match the request instance; retain result, flags, and count. Copy Indirect/Extended facts before target mutation, reuse, or pool return, and retain payloads independently. Check dynamic context before protocol use or resumption.

Deliver success/failure once. Consume a `kont` suspension only once and retain its successor. Release each resource at its own final use. A short stream write leaves an unsent suffix; do not replay the accepted request.

## Validate

[File examples](../../examples/file_io_test.go) cover read/write and `TestBatchedNops`, which retries unaccepted IDs under SQ pressure. [Context examples](../../examples/context_test.go) cover all three modes.

Check success/failure, pre-admission would-block, partial admission, stale identity, retained completion, and [teardown with accepted work](lifecycle.md).
