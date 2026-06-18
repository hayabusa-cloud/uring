# Boundary Gates

Before a change counts as correct, it must pass the gates in this file — the mechanical pass/fail checks that decide whether a fact may cross the `code.hybscloud.com/uring` boundary and whether each decision was left in the caller layer where it belongs. Run them on every boundary change: read-completeness, correct placement, single owner, outcome separation, honest capability, lifecycle soundness, policy separation, and hot-path cleanliness. A gate that fails marks a change that is wrong, not a check to be relaxed.

Each gate checks one thing, and a gate that fails means the change is wrong:

- `G_read` — read-completeness: you have read the complete `code.hybscloud.com/iox` package and the owner package of every package-owned fact the change uses.
- `G_place` — correct placement: the caller frontier and caller policy both sit in the caller layer, owned by the caller, and neither overlaps the boundary.
- `G_owner` — single owner: the resource frontiers are affine, every boundary fact has a single-stratum owner, and every resource has a named owner epoch.
- `G_outcome` — outcome separation: the outcome and release planes are kept apart, the control symbol is one of `ok`/`wouldBlock`/`errMore`/`fail(e)`, and the CQE evidence (`Res`, `Flags`, `user_data`, frontier) is preserved.
- `G_capability` — honest capability: every capability claim is backed by evidence — a kernel probe, documented Linux ABI, or a build constraint.
- `G_lifecycle` — lifecycle soundness: a pending or post-CQE carrier is not recycled or closed, and a terminal or release-ready resource may eventually be released or retained.
- `G_policy` — policy separation: no caller frontier or caller policy is hidden inside the boundary.
- `G_hotpath` — hot-path cleanliness: a hot-path action allocates nothing and does no reflection, boxing, closure building, worker spawning, or metadata routing.

A change passes only when all eight gates hold and its judgment has a defined denotation in some world; a gate that fails rejects the change. These gates read the judgment and its planes — outcome, release, caller frontier, caller policy, ownership, capability, and lifecycle — which are defined in the formalization model, starting from [the model overview](../formalization/overview.md) and [its notation](../formalization/notation.md). The obligations a passing change must also discharge are in [proof-obligations.md](proof-obligations.md), and the staged order in which to apply the gates is in [task-loop.md](task-loop.md).
