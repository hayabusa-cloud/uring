# Runtime Utilization Index

This directory explains how caller-side code combines `code.hybscloud.com/uring` with four caller-layer support packages: `code.hybscloud.com/kont` for suspension and one-shot resumption, `code.hybscloud.com/cove` for explicit context and requirement evidence, `code.hybscloud.com/takt` for runner movement, completion routing, and error-aware stepping, and `code.hybscloud.com/sess` for session-typed protocol frontiers, paired execution, and endpoint stepping. The packages themselves stay in the caller layer. A boundary helper may receive copied facts derived from them, but it must not import their policy, route state, endpoint state, completion memory, or long-lived control logic into `code.hybscloud.com/uring`.

Each file describes one package: which boundary facts it consumes, which caller-owned decisions it carries, and which obligations apply when its carriers wrap a `code.hybscloud.com/uring` boundary action. The packages remain general caller-layer tools; the formulas in this directory constrain their boundary-wrapped use, not every possible use of those packages. The shallow handler discipline defined in [uring/agents/boundary/protocols.md](../boundary/protocols.md) is restated inside each file for the package it covers: every carrier handles one boundary observation and returns control to the caller. Each runtime file also names the package source evidence and a `CodingObligation_*` predicate so code review can check the guide against the current public package surface. Read the package source named by a file before relying on its facts, following the checklist in [uring/agents/workflow/task-checklists.md](../workflow/task-checklists.md).

## Reading Order

For suspension, context, runner, event-loop, or protocol-stack work above or beyond `code.hybscloud.com/uring`, read these files in order:

1. [uring/agents/runtime/kont.md](kont.md)
2. [uring/agents/runtime/cove.md](cove.md)
3. [uring/agents/runtime/takt.md](takt.md)
4. [uring/agents/runtime/sess.md](sess.md)

The order follows the layering: suspension shape first, then the context carried across suspension points, then the runner that advances suspensions against the ring, then the protocol layer expressed over all of it. Each layer restates the shallow handler discipline for its own carriers.

## Files

- [uring/agents/runtime/kont.md](kont.md): `code.hybscloud.com/kont` one-shot suspension and resumption around pending ring operations; affine discipline matched to operation identity.
- [uring/agents/runtime/cove.md](cove.md): `code.hybscloud.com/cove` ambient context, requirement checks, and epoch-monotone evidence across suspension boundaries.
- [uring/agents/runtime/takt.md](takt.md): `code.hybscloud.com/takt` backend, loop, bridge, and error-aware stepping integration; token identity, completion classification, multishot routing, and drain.
- [uring/agents/runtime/sess.md](sess.md): `code.hybscloud.com/sess` session protocol frontiers above completions; Cont/Expr bridging, paired execution, recursive protocol carriers, error-aware stepping, backpressure alignment, and frontier separation.

All four files preserve the same separations as the rest of this guide: kernel-facing facts stay at the `code.hybscloud.com/uring` boundary, caller policy stays in the caller layer, operation outcomes stay separate from release evidence, and owned resources stay distinct from borrowed observations.
