# Proof Obligations

A gate tells you *that* something is wrong; an obligation is *what you must prove* to show it is right. This file lists the obligations a boundary change discharges: outcome separated from release, ownership unique and affine, lifecycle and terminality respected, and no caller frontier or policy hidden in the boundary. Use it as the checklist your judgment must satisfy before you compile back to Go.

The obligations a boundary change must discharge are:

- `O_split` — the boundary, caller, and kernel strata are disjoint, and the caller's support is exactly its frontier plus its policy.
- `O_planes` — each judgment plane carries what it should: reusable assumptions in `Γ`, the visible operation demand in `Δ`, the caller frontier in `Σ`, and the caller policy in `Π`.
- `O_spec` — the judgment is closed: every slot is named, with no free names left dangling.
- `O_package_owner` — every package-owned fact has a named owner package and is either placed with that owner or explicitly projected across the boundary.
- `O_outcome` — completions decode through `q_iox` to exactly one of `ok`, `wouldBlock`, `errMore`, or `fail(e)`, and an early notification decodes to no outcome and no release.
- `O_typing` — the change's derivation uses the typing rules (submit, observe, copy-CQE, decode-frontier, decode-result, release, decode-release) and no forbidden derivation.
- `O_temporal` — terminality and frontier facts respect time: a pending carrier is not prematurely terminal, and cancel, close, and stop race visible CQE facts rather than truncating them.
- `O_policy` — no caller policy or caller frontier is hidden inside the boundary, and every policy in `Π` is caller-owned.
- `O_denotation` — the judgment has a defined denotation in some world.
- `O_refinement` — the Go surface refines the judgment: it is boundary-clean and preserves the ownership epochs.
- `O_complete` — all of the above hold together for the change and its Go surface.

Discharge all of these together — that is `O_complete` — before you compile the change back to Go. The judgment, its planes, the outcome decode `q_iox`, and the typing rules these obligations name are defined in the formalization model: see [notation](../formalization/notation.md) for the judgment and the typing rules, [outcomes](../formalization/outcomes.md) for the outcome and release decode, and [verification](../formalization/verification.md) with [guarantees](../formalization/guarantees.md) for the round-trip and denotation evidence behind `O_refinement` and `O_denotation`. The gates that detect a violation are in [boundary-gates.md](boundary-gates.md), and the staged order in which to discharge these obligations is in [task-loop.md](task-loop.md).
