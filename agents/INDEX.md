# code.hybscloud.com/uring Agent Guide Index

This directory is the guide set for using `code.hybscloud.com/uring` correctly when you generate or review caller-side code around it. The caller layer has two parts: the layer *above* the package, which calls it directly (runtimes, event loops, and adapters that invoke one package boundary action at a time), and the broader systems *beyond* it (protocol stacks, services, and applications built on that layer). The same boundary facts, model, Go mapping, and workflow apply to both parts.

This index is the standalone entrypoint for the guide set. Use it to choose the detailed guide files needed for the task; narrow tasks should read only their required topics, while guide edits and broad caller-side work should read every listed file.

## Reading Order

For ordinary caller-side code generation or review, read these files in order:

1. [uring/agents/workflow/INDEX.md](workflow/INDEX.md)
2. [uring/agents/workflow/boundary-gates.md](workflow/boundary-gates.md)
3. [uring/agents/workflow/task-checklists.md](workflow/task-checklists.md)
4. [uring/agents/formalization/INDEX.md](formalization/INDEX.md)
5. [uring/agents/workflow/proof-obligations.md](workflow/proof-obligations.md)
6. [uring/agents/workflow/task-loop.md](workflow/task-loop.md)
7. [uring/agents/boundary/INDEX.md](boundary/INDEX.md)
8. [uring/agents/runtime/INDEX.md](runtime/INDEX.md)
9. [uring/agents/lift/INDEX.md](lift/INDEX.md)

Use [uring/agents/references.md](references.md) when a claim needs external verification.

## Workflow Guides

- [uring/agents/workflow/INDEX.md](workflow/INDEX.md): how to choose and apply the workflow files.
- [uring/agents/workflow/boundary-gates.md](workflow/boundary-gates.md): placement gates for boundary facts and caller policy.
- [uring/agents/workflow/task-checklists.md](workflow/task-checklists.md): checklist selection by task shape.
- [uring/agents/workflow/proof-obligations.md](workflow/proof-obligations.md): obligations for outcome, release, ownership, and closure checks.
- [uring/agents/workflow/task-loop.md](workflow/task-loop.md): staged work loop for analysis, formalization, checking, compilation, lift saving, and verification.

## Formalization Guides

- [uring/agents/formalization/INDEX.md](formalization/INDEX.md): reading order for the formalization files.
- [uring/agents/formalization/overview.md](formalization/overview.md): goal, three separations, judgment shape, and concept and composability maps.
- [uring/agents/formalization/notation.md](formalization/notation.md): notation, syntactic categories, and the typing and resource discipline.
- [uring/agents/formalization/kernel-boundary.md](formalization/kernel-boundary.md): kernel-facing boundary model and SQE/CQE correspondence.
- [uring/agents/formalization/outcomes.md](formalization/outcomes.md): outcome, release, and control planes.
- [uring/agents/formalization/resources.md](formalization/resources.md): resource and lifecycle calculus.
- [uring/agents/formalization/sessions.md](formalization/sessions.md): protocol and frontier interpretation.
- [uring/agents/formalization/handler.md](formalization/handler.md): shallow-handler calculus, reduction, and the runner and observation boundary.
- [uring/agents/formalization/go-mapping.md](formalization/go-mapping.md): Go-to-guide abstraction and compilation back to Go.
- [uring/agents/formalization/guarantees.md](formalization/guarantees.md): metatheoretic consistency checks and the named properties an accepted change must prove.
- [uring/agents/formalization/verification.md](formalization/verification.md): verification workflow and evidence classes.

## Boundary Guides

- [uring/agents/boundary/INDEX.md](boundary/INDEX.md): reading order for boundary topics.
- [uring/agents/boundary/sqe-cqe.md](boundary/sqe-cqe.md): SQE encoding, CQE observation, and decode separation.
- [uring/agents/boundary/resources.md](boundary/resources.md): ownership epochs (pending, terminal, release, recycle, close, stop) together with fixed, provided, selected, and zero-copy buffer rules.
- [uring/agents/boundary/integration.md](boundary/integration.md): package integration boundaries.
- [uring/agents/boundary/protocols.md](boundary/protocols.md): protocol and runtime boundary placement above or beyond `code.hybscloud.com/uring`, including the shallow handler discipline and its Go handler and closure rules.

## Runtime Utilization Guides

- [uring/agents/runtime/INDEX.md](runtime/INDEX.md): reading order for caller-layer runtime packages used with `code.hybscloud.com/uring`.
- [uring/agents/runtime/kont.md](runtime/kont.md): `code.hybscloud.com/kont` one-shot suspension and resumption around pending ring operations.
- [uring/agents/runtime/cove.md](runtime/cove.md): `code.hybscloud.com/cove` ambient context and requirement evidence across suspension boundaries.
- [uring/agents/runtime/takt.md](runtime/takt.md): `code.hybscloud.com/takt` backend, loop, error-aware stepping, token identity, multishot routing, and drain.
- [uring/agents/runtime/sess.md](runtime/sess.md): `code.hybscloud.com/sess` session protocol frontiers, paired execution, error-aware endpoint stepping, and backpressure above completions.

## Saved Lift Records

- [uring/agents/lift/INDEX.md](lift/INDEX.md): saved lift directory guide.
- [uring/agents/lift/boundary-core.md](lift/boundary-core.md): saved core boundary and SQE/CQE lift.
- [uring/agents/lift/resources-lifecycle.md](lift/resources-lifecycle.md): saved resource, lifecycle, and buffer lift.
- [uring/agents/lift/protocols-runtime.md](lift/protocols-runtime.md): saved multishot/protocol lift and runtime carrier lift for `code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, and `code.hybscloud.com/sess`.
- [uring/agents/lift/policy-separation.md](lift/policy-separation.md): saved caller-policy separation lift.

## External References

- [uring/agents/references.md](references.md): primary-source research guide for Linux, Go, `io_uring`, package facts, and formal-methods background.
