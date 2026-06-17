# Task Loop

This is the loop you actually run, in two directions. For design you start from the caller's goal and go `analyze → formalize → typecheck → reduce → prove → compile_to_go → lift → verify`; for review you start from existing Go and `lift` it first. Each stage has a completion condition, and a stage invariant keeps SQE/CQE identity, ownership, outcome, release, and owners stable as you move between stages — so no step silently changes the meaning of the one before it.

## The two directions

- Design work starts from the caller's goal and runs the stages in order: `analyze → formalize → typecheck → reduce → prove → compile_to_go → lift → verify`.
- Review work starts from existing Go, lifts that Go first, and then formalizes: `analyze → lift → formalize → typecheck → reduce → prove → compile_to_go → verify`.

## The stages, and when each is done

- analyze — close the requirements: the goal, the action, the resources, the completion outcomes, the release observations, the frontier and policy owners, and the checks. Read the complete `code.hybscloud.com/iox` package and the owner package of every package-owned fact the task uses. Done when the requirement set is closed and every required owner package has been read.
- formalize — write the change as a judgment `J` whose outcome plane and release plane are both set. Done when `J` is fully named.
- typecheck — confirm `J` is well-formed and that the resource frontiers before and after are affine, each linear token used at most once. Done when both hold.
- reduce — drive `J` to normal form, with no caller policy and no caller frontier hidden inside the boundary. Done when `J` is in normal form.
- prove — discharge every obligation the change owes. Done when the obligations are dischargeable and discharged. The obligations are in [uring/agents/workflow/proof-obligations.md](proof-obligations.md), and the placement gates are in [uring/agents/workflow/boundary-gates.md](boundary-gates.md).
- compile_to_go — turn the proved judgment into exactly one boundary action of Go.
- lift — record the saved lift checkpoint for the abstraction (see "Saving the lift"). In review work this stage comes first and lifts the existing Go surface.
- verify — produce the full evidence (see "What verification requires").

## The stage invariant

Moving from one stage to the next must preserve the meaning of the change: the same SQE identity, CQE identity, resource ownership, completion outcome, release observation, capability evidence, frontier owner, and policy owner. When you repair a judgment, the repaired judgment must keep all of those the same, reach normal form, and discharge its obligations.

## Saving the lift

After each of the saving stages — `formalize`, `typecheck`, `reduce`, `prove`, and `lift` — write or update a topic-specific lift record and its index under the lift path (by default [uring/agents/lift](../lift), unless the task specifies another path). The record captures the stage, the judgment, the boundary facts, the observed Go-surface facts, the obligations, any decisions or fixes, the verification evidence, and the residual assumptions. Advance to the next saving stage only once the predecessor stage's lift is saved and its denotation is preserved — that is, its boundary and caller projections match the judgment's. Compiling to Go is allowed only after `prove` is saved and complete, the outcome and release planes are set, the action is one boundary action, the frontier and policy are caller-owned, and no caller policy or frontier is hidden in the boundary.

## What verification requires

Verification is more than passing tests. A change is verified when, for some Go surface, it has all of:

- a faithful round trip — compiling `J` to Go and lifting it back returns to `J` up to boundary equivalence, and lifting the Go surface and compiling it back returns to that surface up to public equivalence;
- denotation correspondence — in some world, the lifted Go surface and the judgment share the same boundary and caller projections, and the same outcome and release;
- passing tests — the package tests and the focused boundary tests; and
- passing static checks — `gofmt`, `go vet` where available, an allocation and escape check on hot paths, and link reachability for guide edits.

The precise forms of the round-trip and denotation-correspondence evidence are in [uring/agents/formalization/verification.md](../formalization/verification.md) and [uring/agents/formalization/guarantees.md](../formalization/guarantees.md).

## Waiting and races

When the task waits, the no-progress policy is the caller's: use `Backoff` from `code.hybscloud.com/iox`, whose state the caller owns and which never lives inside the boundary. Step the backoff's wait after `ErrWouldBlock` or after observing no completion, and reset it after visible forward progress — a reaped CQE, a boundary-state transition, a live frontier, a terminal completion, or another caller-visible state transition. Cancel, timeout, close, and multishot-stop all race against visible CQE facts: an accepted cancel does not by itself prove that a terminal completion will not arrive, and a trace missing the `MORE` flag does not by itself prove that no completion arrives.
