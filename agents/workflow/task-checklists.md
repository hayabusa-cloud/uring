# Task Checklists

Not every task needs every file. This is the map from a task's shape — an SQE/CQE change, a lifecycle or buffer change, an integration point, a protocol, a runtime-package integration, a reference claim, or a broad guide edit — to exactly the guide files you must read and the lift records you must check before you start. Read what the task requires; for a broad guide edit or broad caller-side work, read everything.

## What every task reads

Before any task, read the standalone guide entrypoint, the section indexes, and the workflow and overview baseline:

- Entry point: [uring/agents/INDEX.md](../INDEX.md).
- Package overview: [uring/README.md](../../README.md).
- Indexes: [uring/agents/INDEX.md](../INDEX.md), [uring/agents/workflow/INDEX.md](INDEX.md), [uring/agents/boundary/INDEX.md](../boundary/INDEX.md), [uring/agents/formalization/INDEX.md](../formalization/INDEX.md), [uring/agents/lift/INDEX.md](../lift/INDEX.md), and [uring/agents/runtime/INDEX.md](../runtime/INDEX.md).
- Baseline: [uring/agents/workflow/boundary-gates.md](boundary-gates.md), [uring/agents/workflow/proof-obligations.md](proof-obligations.md), [uring/agents/workflow/task-loop.md](task-loop.md), and [uring/agents/formalization/overview.md](../formalization/overview.md).

## What each task shape adds

Classify the task by shape and add the topic files and lift records for every shape that applies — a task may have more than one shape:

- SQE/CQE change: read [uring/agents/boundary/sqe-cqe.md](../boundary/sqe-cqe.md) and [uring/agents/formalization/kernel-boundary.md](../formalization/kernel-boundary.md); check the lift in [uring/agents/lift/boundary-core.md](../lift/boundary-core.md).
- Lifecycle change: read [uring/agents/boundary/resources.md](../boundary/resources.md) and [uring/agents/formalization/resources.md](../formalization/resources.md); check the lift in [uring/agents/lift/resources-lifecycle.md](../lift/resources-lifecycle.md).
- Buffer change: read [uring/agents/boundary/resources.md](../boundary/resources.md) and [uring/agents/formalization/resources.md](../formalization/resources.md); check the lift in [uring/agents/lift/resources-lifecycle.md](../lift/resources-lifecycle.md).
- Integration point: read [uring/agents/boundary/integration.md](../boundary/integration.md) and [uring/agents/formalization/go-mapping.md](../formalization/go-mapping.md); check the lifts in [uring/agents/lift/boundary-core.md](../lift/boundary-core.md) and [uring/agents/lift/policy-separation.md](../lift/policy-separation.md).
- Protocol: read [uring/agents/boundary/protocols.md](../boundary/protocols.md) and [uring/agents/formalization/sessions.md](../formalization/sessions.md); check the lift in [uring/agents/lift/protocols-runtime.md](../lift/protocols-runtime.md).
- Runtime-package integration (`code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, `code.hybscloud.com/sess`): read [uring/agents/runtime/kont.md](../runtime/kont.md), [uring/agents/runtime/cove.md](../runtime/cove.md), [uring/agents/runtime/takt.md](../runtime/takt.md), [uring/agents/runtime/sess.md](../runtime/sess.md), [uring/agents/formalization/handler.md](../formalization/handler.md), and [uring/agents/formalization/sessions.md](../formalization/sessions.md); check the lifts in [uring/agents/lift/protocols-runtime.md](../lift/protocols-runtime.md) and [uring/agents/lift/policy-separation.md](../lift/policy-separation.md).
- Reference claim: read [uring/agents/references.md](../references.md); there is no lift record to check.

When the task touches the model — every shape above except a pure reference claim — also read all of the formalization files, starting from [uring/agents/formalization/INDEX.md](../formalization/INDEX.md).

For a broad guide edit or broad caller-side work, read every indexed topic file, check every lift record, and read the complete current source for every package whose facts the edited guide relies on. When the edit touches the runtime, integration, protocol, ownership, or outcome model, the minimum package-source set is `code.hybscloud.com/iox`, `code.hybscloud.com/iofd`, `code.hybscloud.com/sock`, `code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, and `code.hybscloud.com/sess`; add `code.hybscloud.com/zcall`, `code.hybscloud.com/iobuf`, `code.hybscloud.com/spin`, `code.hybscloud.com/lfq`, or `code.hybscloud.com/framer` whenever the edited guide uses syscall, buffer, spin/yield, mailbox, or framing facts.

## Before you start

Confirm the task passes its checklist: you have read every file required above, and the judgment for the change is well-formed, places caller frontier and policy in the caller layer, keeps ownership affine, separates the operation outcome from release evidence, keeps caller policy out of the boundary, has a defined denotation, and can discharge its obligations. These checks are the gates in [uring/agents/workflow/boundary-gates.md](boundary-gates.md) and the obligations in [uring/agents/workflow/proof-obligations.md](proof-obligations.md); the staged order in which to apply them is in [uring/agents/workflow/task-loop.md](task-loop.md).
