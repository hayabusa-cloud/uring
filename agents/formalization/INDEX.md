# Formalization Index

Use these files when the task needs the formal notation behind the public `code.hybscloud.com/uring` agent guide. They cover caller code that directly invokes the package and broader systems built above that caller layer. They also describe abstraction from Go into the guide notation and compilation from checked notation back into Go. The files are ordered from notation basics to verification closure.

The formalization guide family is checked as a composed guide: each topic file is checked by `GuideInternalWell_U(f)`, defined in [uring/agents/formalization/guarantees.md](guarantees.md), and the topic judgments must remain provably composable under the closure conditions in [uring/agents/formalization/overview.md](overview.md), [uring/agents/formalization/guarantees.md](guarantees.md), and [uring/agents/formalization/verification.md](verification.md).

The formal vocabulary stays explicit in `code.hybscloud.com/uring` boundary terms, but this index remains a navigation file. When editing a topic file, preserve the vocabulary group it owns:

- Kernel and judgment vocabulary: core boundary facts, `Ctrl`, `noHidden`, and `canonicalLevel`.
- Syntax vocabulary: `NameAtoms`, facts, `Γ`, `Δ`, `Θ`, `χ`, `Σ`, `Μ`, `Φ`, `Obs`, `Rel`, and `Π`.
- Typing vocabulary: typing, resource, session, effect, and coeffect judgments.
- Reduction vocabulary: `wouldBlock`, `errMore`, shallow-handler reduction, and coeffect reduction.
- Runner and bridge vocabulary: `Reify`, `Reflect`, trampoline, runner, `Suspension`, and coalgebra.
- Compilation vocabulary: `compile_to_go`, outcome compilation, handler compilation, and contextual compilation.
- Session and outcome vocabulary: duality, projection, `Outcome`, `Π`, `Backoff`, `ErrWouldBlock`, and `ErrMore`.
- Metatheory vocabulary: soundness, scope-relative completeness, consistency, preservation, and progress.
- Workflow vocabulary: `analyze`, `lift`, `formalize`, `typecheck`, `reduce`, `prove`, `compile_to_go`, and `verify`, together with saved-stage records.
- Middleware vocabulary: shallow-handler composition across `code.hybscloud.com/iox`, `code.hybscloud.com/kont`, `code.hybscloud.com/cove`, and `code.hybscloud.com/takt`.
- Property vocabulary: `Structural_Soundness`, `Conservativity`, `Composability`, `Parametricity`, and `extension_admissibility`.

Read the formalization files in this order:

1. [uring/agents/formalization/overview.md](overview.md)
2. [uring/agents/formalization/notation.md](notation.md)
3. [uring/agents/formalization/kernel-boundary.md](kernel-boundary.md)
4. [uring/agents/formalization/outcomes.md](outcomes.md)
5. [uring/agents/formalization/resources.md](resources.md)
6. [uring/agents/formalization/sessions.md](sessions.md)
7. [uring/agents/formalization/handler.md](handler.md)
8. [uring/agents/formalization/go-mapping.md](go-mapping.md)
9. [uring/agents/formalization/guarantees.md](guarantees.md)
10. [uring/agents/formalization/verification.md](verification.md)

## Files

- [uring/agents/formalization/overview.md](overview.md): states the goal, the three separations, the judgment shape, and the concept and composability maps.
- [uring/agents/formalization/notation.md](notation.md): names the syntax, planes, and projections, and classifies actions, resources, observations, and release evidence.
- [uring/agents/formalization/kernel-boundary.md](kernel-boundary.md): describes the Linux-facing boundary model and the SQE/CQE roundtrip correspondence.
- [uring/agents/formalization/outcomes.md](outcomes.md): separates successful completion, `ErrWouldBlock`, `ErrMore`, failure, byte/count evidence, CQE flags, and release observations.
- [uring/agents/formalization/resources.md](resources.md): checks ownership, affinity, release, recycle, and terminality.
- [uring/agents/formalization/sessions.md](sessions.md): connects boundary observations to caller-owned protocol and session frontiers.
- [uring/agents/formalization/handler.md](handler.md): gives the shallow-handler calculus, the reduction that normalizes boundary actions, and the suspend and resume boundary that keeps runner movement and scheduling policy outside the `code.hybscloud.com/uring` boundary.
- [uring/agents/formalization/go-mapping.md](go-mapping.md): lifts public Go surfaces into the guide notation and compiles checked notation back into Go shape.
- [uring/agents/formalization/guarantees.md](guarantees.md): records consistency, soundness, and correspondence checks, and lists the named properties a checked task may need to prove.
- [uring/agents/formalization/verification.md](verification.md): describes verification evidence and closure requirements.
