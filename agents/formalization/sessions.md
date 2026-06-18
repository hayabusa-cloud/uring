# Sessions

A protocol is a caller-side state machine driven by boundary observations, and `code.hybscloud.com/uring` correctly refuses to own any of it. This file gives the frontier model: how a session frontier `Σ` steps when a completion arrives, and which steps exist at all. Every observation drives exactly one of six transitions — success (live or terminal), `errMore`, failure (live or terminal), `wouldBlock` (which returns control unchanged), suspension, or runner movement — and the whole stepping relation `ObsStepRel` lives outside the boundary (`ObsStepRel ∉ support(B)`). Parser state, branch policy, and the frontier itself are caller-owned; if a change lets any of them hide inside `B`, the protocol is no longer the caller's.

Two asymmetries matter for using `code.hybscloud.com/uring` correctly: a failure on a live frontier is not terminal and must not carry a terminal frontier fact, and a multishot frontier is advanced by the caller's loop, never by the boundary re-entering itself.

In plain terms, the rules an agent follows here are:

- One observation drives exactly one frontier step. The six step relations — success (live or terminal), `errMore`, failure (live or terminal), `wouldBlock`, suspension, and runner movement — make up the whole stepping relation `ObsStepRel`.
- The frontier is the caller's. `ObsStepRel`, parser state, and branch policy live outside the boundary (`ObsStepRel ∉ support(B)`); a step that hides any of them inside `B` is wrong.
- Two asymmetries hold. A failure on a live frontier is not terminal and carries no terminal frontier fact, and a multishot frontier is advanced by the caller's loop, never by the boundary re-entering itself.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
B = boundary(module)
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }

SessionFrontier = {protocol_frontier}
StreamFrontier = {stream_frontier}
RouteFrontier = {route_frontier}
SubscriptionFrontier = {subscription_frontier}
SuspensionFrontier = {suspension_frontier}
RunnerMovementFrontier = {runner_movement}
SessionSurface =
  SessionFrontier ∪ StreamFrontier ∪ RouteFrontier ∪ SubscriptionFrontier
  ∪ SuspensionFrontier ∪ RunnerMovementFrontier ∪ CallerPolicy
⟦·⟧^{U,pkg("code.hybscloud.com/sess")}_w : SessionSurface ⇀ Denotation_U(w)

Σ_frontier =
  SessionFrontier ∪ StreamFrontier ∪ RouteFrontier ∪ SubscriptionFrontier
  ∪ SuspensionFrontier ∪ RunnerMovementFrontier
support(Σ_frontier) ⊆ CallerFrontier
frontier_owner(Σ_frontier) = C
denote_session_frontier(w) ⇔
  w ∈ World_U
  ∧ defined(⟦Σ_frontier⟧^{U,pkg("code.hybscloud.com/sess")}_w)

FrontierState = { σ | support(σ) ⊆ CallerFrontier }
ObsStepRel ⊆ FrontierState × OutcomeProduct × FrontierState
ObsStep(Σ,Obs,Σ') ⇔ (Σ,Obs,Σ') ∈ ObsStepRel
ObsStepRel ∉ support(B)
frontier_fact(Obs,F) ⇔ F ∈ frontier_set(Obs)
NonterminalFrontierFact =
  { F | ∃ ctx,r. F = nonterminal_frontier(ctx,r) }
NonterminalFailureFrontierFact =
  { F | ∃ ctx,r. F = nonterminal_frontier_after_failure(ctx,r) }
TerminalFrontierFact =
  { F | ∃ ctx,r. F = terminal_frontier(ctx,r) }
TerminalFailureFrontierFact =
  { F | ∃ ctx,r. F = terminal_frontier_after_failure(ctx,r) }
disjoint(NonterminalFrontierFact,TerminalFrontierFact)
disjoint(NonterminalFailureFrontierFact,TerminalFailureFrontierFact)

errMore_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ ctrl(Obs)=errMore
  ∧ ∃ F ∈ NonterminalFrontierFact. frontier_fact(Obs,F)
  ∧ Σ' = caller_transition(Σ,Obs)

failure_live_frontier_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ ∃ e ∈ E.
      ctrl(Obs)=fail(e)
      ∧ ∃ F ∈ NonterminalFailureFrontierFact. frontier_fact(Obs,F)
      ∧ Σ' = caller_live_failure_transition(Σ,e,Obs)

failure_terminal_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ ∃ e ∈ E.
      ctrl(Obs)=fail(e)
      ∧ ∃ F ∈ TerminalFailureFrontierFact. frontier_fact(Obs,F)
      ∧ Σ' = caller_terminal_failure_transition(Σ,e,Obs)

failure_step(Σ,Obs,Σ') ⇔
  failure_live_frontier_step(Σ,Obs,Σ')
  ∨ failure_terminal_step(Σ,Obs,Σ')

ok_live_frontier_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ ctrl(Obs)=ok
  ∧ ∃ F ∈ NonterminalFrontierFact. frontier_fact(Obs,F)
  ∧ Σ' = caller_live_success_transition(Σ,Obs)

ok_terminal_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ ctrl(Obs)=ok
  ∧ ∃ F ∈ TerminalFrontierFact. frontier_fact(Obs,F)
  ∧ Σ' = caller_success_transition(Σ,Obs)

ok_step(Σ,Obs,Σ') ⇔
  ok_live_frontier_step(Σ,Obs,Σ')
  ∨ ok_terminal_step(Σ,Obs,Σ')

wouldBlock_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ ctrl(Obs)=wouldBlock
  ∧ Σ' = Σ

suspension_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ suspension_frontier ∈ support(Σ)
  ∧ Σ' = caller_suspension_transition(Σ,Obs)

runner_movement_step(Σ,Obs,Σ') ⇔
  Σ ∈ FrontierState
  ∧ Obs ∈ OutcomeProduct
  ∧ Σ' ∈ FrontierState
  ∧ runner_movement ∈ support(Σ)
  ∧ Σ' = caller_runner_transition(Σ,Obs)

ObsStepRel =
  { (Σ,Obs,Σ') |
      ok_step(Σ,Obs,Σ')
      ∨ errMore_step(Σ,Obs,Σ')
      ∨ failure_step(Σ,Obs,Σ')
      ∨ wouldBlock_step(Σ,Obs,Σ')
      ∨ suspension_step(Σ,Obs,Σ')
      ∨ runner_movement_step(Σ,Obs,Σ') }

duality(role_l,role_r) ⇔
  local_dual(role_l,role_r)
  ∧ boundary_projection(role_l)=boundary_projection(role_r)

protocol_frontier ∈ SessionFrontier
stream_frontier ∈ StreamFrontier
route_frontier ∈ RouteFrontier
subscription_frontier ∈ SubscriptionFrontier
suspension_frontier ∈ SuspensionFrontier
runner_movement ∈ RunnerMovementFrontier
parser_state ∈ CallerPolicy
parser_branch_policy ∈ CallerPolicy
¬hidden(Σ_frontier ∪ {parser_state,parser_branch_policy},support(B))
failure_live_frontier_step(Σ,Obs,Σ') →
  ¬∃ F ∈ TerminalFailureFrontierFact. frontier_fact(Obs,F)
failure_terminal_step(Σ,Obs,Σ') →
  ¬∃ F ∈ NonterminalFailureFrontierFact. frontier_fact(Obs,F)
```
