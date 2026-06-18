# Handler

`code.hybscloud.com/uring` is a *shallow* effect handler: it observes one boundary action and hands control straight back to the caller. It never owns the loop, never reinstalls itself to interpret the next completion, and never captures caller policy. This file is the operational heart of that claim. Read it to know exactly how a boundary action steps, what counts as a normal form, and how a suspension is resumed at most once per observation — because the moment a change makes the handler *deep* (a hidden runner loop, a retained continuation, policy baked into the step), every separation the guide promises collapses.

The three sections move from the small-step calculus, to the reduction that says when stepping is done, to the suspend/resume boundary where a kernel observation is delivered to the caller's continuation.

## Operational Calculus

The block gives the small-step semantics of a boundary action: how an encoded action reaches a completion, and the one-action / one-observation shape that keeps the handler shallow.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
K = below(module)
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

EnvSurface = { env | environment_assumptions(module,env) }
KernelSurface = { kern | linux_kernel_state(module,kern) }
CQSurface = { cq | completion_queue_state(module,cq) }
Time = boundary_observation_events
State =
  {⟨J,env,kern,cq,t⟩ | J ∈ JudgmentSurface
                     ∧ env ∈ EnvSurface
                     ∧ kern ∈ KernelSurface
                     ∧ cq ∈ CQSurface
                     ∧ t ∈ Time}
Step ⊆ State × State
→ = Step
→* = reflexive_transitive_closure(→)

WF_state(⟨J,env,kern,cq,t⟩) ⇔
  WF(J)
  ∧ env ∈ EnvSurface
  ∧ kern ∈ KernelSurface
  ∧ cq ∈ CQSurface
  ∧ t ∈ Time
  ∧ kernel_assumptions(env,kern)
  ∧ cq_well_formed(cq)
denote_state(⟨J,env,kern,cq,t⟩,w) ⇔
  w ∈ World_U
  ∧ J ∈ JudgmentSurface
  ∧ defined(⟦J⟧^U_w)
  ∧ WF_state(⟨J,env,kern,cq,t⟩)

BoundaryStep(J,J') ⇔
  single_boundary_action(action(J))
  ∧ preserves_owner_split(J,J')
  ∧ preserves_package_owner(J,J')
  ∧ Obs(J') ∈ OutcomePlane
  ∧ Rel(J') ∈ ReleasePlane

Normalize(J) = J_nf
normal_form(J_nf) ⇔
  ¬hidden(CallerPolicy ∪ CallerFrontier, support(B))
  ∧ one_boundary_action(action(J_nf))
  ∧ explicit(Θ(J_nf),Θ'(J_nf),χ(J_nf),Μ(J_nf),Φ(J_nf),Obs(J_nf),Rel(J_nf))

frontier_fact(X,F) ⇔ F ∈ frontier_set(X)
terminal_completion(ctx,r,J) ⇔
  frontier_fact(Obs(J), terminal_frontier(ctx,r))
  ∨ frontier_fact(Obs(J), terminal_frontier_after_failure(ctx,r))
terminal_observation(J) ⇔
  ∃ ctx,r. terminal_completion(ctx,r,J)
wouldBlock_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct ∧ ctrl(Obs(J)) = wouldBlock
errMore_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct
  ∧ ctrl(Obs(J)) = errMore
  ∧ ∃ ctx,r.
      frontier_fact(Obs(J),nonterminal_frontier(ctx,r))
nonterminal_success_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct
  ∧ ctrl(Obs(J)) = ok
  ∧ ∃ ctx,r.
      frontier_fact(Obs(J),nonterminal_frontier(ctx,r))
nonterminal_failure_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct
  ∧ ∃ e ∈ E. ctrl(Obs(J)) = fail(e)
  ∧ ∃ ctx,r.
      frontier_fact(Obs(J),nonterminal_frontier_after_failure(ctx,r))
live_frontier_visible(J) ⇔
  nonterminal_success_visible(J)
  ∨ errMore_visible(J)
  ∨ nonterminal_failure_visible(J)
no_operation_outcome_visible(J) ⇔
  Obs(J) = none

Progress(J) ⇔
  WF(J) →
    (terminal_observation(J)
     ∨ wouldBlock_visible(J)
     ∨ live_frontier_visible(J)
     ∨ no_operation_outcome_visible(J)
     ∨ ∃ J'. BoundaryStep(J,J'))
Preservation(J,J') ⇔ (WF(J) ∧ BoundaryStep(J,J')) → WF(J')

Confluence_relative(J) ⇔
  deterministic_boundary_projection(J)
  ∧ caller_policy_order_external(J)

SoundReduction(J,J_nf) ⇔
  J →* J_nf
  ∧ normal_form(J_nf)
  ∧ observational_equivalent(J,J_nf)
```

## Reduction And Normalization

Reduction says when a judgment has finished stepping and that normalization preserves what an agent can observe. Use it to check that a rewrite of a sequence of boundary actions is *observationally equivalent* to the original — same outcomes, same release evidence — not merely structurally similar.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

J_red =
  Γ | Θ ⊢ action @ ring : result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π
Obs(J_red) ∈ OutcomePlane
Rel(J_red) ∈ ReleasePlane
denote_reduction_subject(w) ⇔
  w ∈ World_U
  ∧ J_red ∈ JudgmentSurface
  ∧ defined(⟦J_red⟧^U_w)

J → J' = one_reduction_step(J,J')
J →* J_nf = reflexive_transitive_closure(→)(J,J_nf)

HiddenPolicy(J) =
  hidden(CallerPolicy ∪ CallerFrontier, support(B))

R_split_policy:
  HiddenPolicy(J)
  ─────────────────────────────────────────────
  J → split(J, boundary_part(J), caller_policy_part(J))

R_expose_outcome:
  collapsed({ok,wouldBlock,errMore,fail(e)},Obs(J))
  ─────────────────────────────────────────────
  J → expose_disjoint_control(J)

R_name_owner:
  unnamed(owner_epoch(r),J)
  ─────────────────────────────────────────────
  J → name(owner_epoch(r),J)

R_name_capability:
  implicit(capability_claim(c),J)
  ─────────────────────────────────────────────
  J → add(χ,evidence(c))

R_single_action:
  action(J)=a;b
  ─────────────────────────────────────────────
  J → sequence(caller_context, J_a, J_b)

normal_form(J) ⇔
  ¬HiddenPolicy(J)
  ∧ one_boundary_action(action(J))
  ∧ named(Θ(J),Θ'(J),χ(J),Μ(J),Φ(J),Obs(J),Rel(J))
  ∧ support(Σ(J)) ⊆ CallerFrontier
  ∧ support(Π(J)) ⊆ CallerPolicy

ReductionSound(J,J_nf) ⇔
  J →* J_nf
  ∧ normal_form(J_nf)
  ∧ observational_equivalent(J,J_nf)
  ∧ ∀ w ∈ World_U.
      defined(⟦J⟧^U_w) → defined(⟦J_nf⟧^U_w)
```

## Suspend, Resume, And The Observation Boundary

This is where one kernel observation crosses into the caller's continuation. A suspension is resumed at most once per observation, the continuation is owned by the caller layer (not the boundary), and the runner loop is the caller's, not the package's. The two rejections at the end — no hidden runner loop, no boundary-owned caller frontier — are what keep `code.hybscloud.com/uring` shallow rather than silently deep.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

RunnerOwner = pkg("code.hybscloud.com/takt")
ContinuationOwner = pkg("code.hybscloud.com/kont")
SessionOwner = pkg("code.hybscloud.com/sess")

RunnerStep(J,J') ⇔
  realizes_one_boundary_action(J,J')
  ∧ observes(Obs(J'))
  ∧ preserves(Θ(J),Θ'(J'))
  ∧ support(Π(J')) ⊆ CallerPolicy
  ∧ support(Σ(J')) ⊆ CallerFrontier
  ∧ ∀ w ∈ World_U.
      defined(⟦J⟧^U_w) → defined(⟦J'⟧^U_w)

runner_movement ∈ CallerFrontier
suspension_frontier ∈ CallerFrontier
runner_policy ∈ CallerPolicy
runner_movement ∉ support(B)
suspension_frontier ∉ support(B)
runner_policy ∉ support(B)

suspension_binding(J,ctx,k) ⇔
  J ∈ JudgmentSurface
  ∧ suspension_frontier ∈ support(Σ(J))
  ∧ binds(Σ(J),suspension_frontier,(ctx,k))

suspend(J,ctx,k) ⇔
  J ∈ JudgmentSurface
  ∧ owned_by(k,ContinuationOwner)
  ∧ suspension_binding(J,ctx,k)

resume(J,ctx,k,Obs) ⇔
  J ∈ JudgmentSurface
  ∧ suspension_binding(J,ctx,k)
  ∧ Obs ∈ OutcomeProduct
  ∧ caller_transition(k,Obs)

park_or_yield(J,y) ⇔
  J ∈ JudgmentSurface
  ∧ owned_by(y,pkg("code.hybscloud.com/spin"))
  ∧ y ∈ support(Π(J))

reject(hidden_runner_loop(B))
reject(boundary_owns(CallerFrontier ∪ {runner_policy}))
```
