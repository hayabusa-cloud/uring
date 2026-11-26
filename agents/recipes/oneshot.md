# One-Shot I/O

Admit one operation, match its completion and deliver the result once.

## Establish Owners

Choose the ring owner and the smallest context that identifies the live request. Direct mode carries inline fields. Retain descriptors, payloads, submission metadata and typed roots through their final use. [Request identity](../boundary/sqe-cqe.md#request-identity) · [Resource lifetimes](../boundary/resources.md#retain-before-submit)

For a default ring, lock the owner to its OS thread before `Start` and serialize ring operations through `Stop`. Check the opcode and setup requirements for the chosen ring. [Encoding and setup](../boundary/encoding.md#setup-and-capabilities)

## Admission

Keep the pending action, operands and original route together. Bound pending storage and decide how to handle a full queue. For a guarded submission, check the current binding with `cove.Need` before each offer, including a short write's suffix. [Guard a send](../runtime/cove.md#guard-a-send)

| Observation | Next action |
|-------------|-------------|
| A single submission returns `iox.ErrWouldBlock` on a full SQ | Drive the ring, then offer the same action. |
| A batch returns `(accepted, err)` | Advance past the accepted prefix before handling `err`. Dispatch its required CQEs. When retrying independent items, start with the first unaccepted item. |
| A matched local operation CQE has `Res == -EAGAIN` | This is the result of an admitted request. Check the opcode's effects before issuing a new attempt. |

If a computation already reached a suspension, restarting it can repeat earlier effects. Check who retains the suspension on the selected execution path. [Submission contract](../boundary/sqe-cqe.md#submission) · [Execution with takt](../runtime/takt.md#choose-the-execution-surface)

## Observe and Deliver

Drive submission and reap into a nonempty destination. An empty poll leaves deferred work with the caller. Revisit due submissions before the next poll. Use the caller's `iox.Backoff` when the turn advances no work and reaps zero CQEs. [Wait modes](../boundary/sqe-cqe.md#observation) · [Backoff](../boundary/integration.md#waiting-and-backoff)

Dispatch every entry to its owner. For a matched reply, keep the result and flags. Copy borrowed fields before reusing their target. If delivery requires permission, check the pending suspension with `cove.CheckSuspension`. On refusal, keep the suspension and any resource the completion returned. [Completion origin](../boundary/sqe-cqe.md#completion-origin) · [Completion retention](../boundary/sqe-cqe.md#completion-retention) · [Guarded dispatch](../runtime/cove.md#outputs-and-ownership)

For a positive short stream write, advance by the reported byte count.

Deliver success or failure once. If resuming a suspension, keep its successor. Release each resource at its own final use. [One-shot suspension](../runtime/kont.md)

## Validate

[File examples](../../examples/file_io_test.go) cover read/write and `TestBatchedNops`, which retries unaccepted IDs under SQ pressure. [Context examples](../../examples/context_test.go) cover all three modes.

Check success/failure, pre-admission would-block, partial admission, stale identity, retained completion, and [teardown with accepted work](lifecycle.md).

For a guarded write on a socket pair, offer `PI` and retain `NG`. Revoke after admission and reap the two-byte result. A renewed grant on the original route permits the peer to receive `PING` once. Replacing the generation before retry leaves the peer with `PI`.
