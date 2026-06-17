# Policy Separation Lift

This file is the saved lift for the one separation that cuts across every other: caller-owned policy — retry, backoff, route, parser, scheduler, cancellation, timeout, service — must never live inside the `code.hybscloud.com/uring` boundary. Use it as the reference for where a policy decision belongs on the Go surface, so a change can be checked against the rule that the boundary states facts while the caller decides what to do about them.

```text
topic = policy_separation
task_module = pkg("code.hybscloud.com/uring")
B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_policy_separation) = ⟦J_policy_separation⟧_U

complete(lift,J_policy_separation) ⇔
  ∃ go_surface.
    go_surface ∈ GoSurface
    ∧ BoundaryClean(go_surface)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ ⟦go_surface⟧⁻¹_U ≃_B J_policy_separation

∀ stage ∈ StageSave.
  complete(stage,J_policy_separation)
    → saved(SaveLift(stage,task,J_policy_separation))
complete(lift,J_policy_separation)
  → saved(SaveLift(lift,task,J_policy_separation))
```

## Judgment

```text
J_policy_separation =
  Γ_ps | Θ_ps ⊢ caller_boundary_step @ ring : result | Δ_ps ▷ Θ_ps'
    ; χ_ps ; Σ_ps ; Μ_ps ; Φ_ps ; Obs_ps ; Rel_ps ; Π_ps
Obs_ps ∈ OutcomePlane
Rel_ps ∈ ReleasePlane

support(Π_ps) ⊆ CallerPolicy
support(Σ_ps) ⊆ CallerFrontier
frontier_owner(Σ_ps) = C
policy_owner(Π_ps) = C
```

## Separation Law

```text
BoundaryOnly =
  { ring_setup, SQE_encode, CQE_observe, ownership, capability
  , boundary_lifecycle
  }

PolicyName = finite subset of Atom
BufferName = finite subset of Atom
BackoffPolicy =
  {backoff_policy(bo) | bo : type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")}
MailboxPolicy =
  {mailbox_policy(q) | q : type_symbol(pkg("code.hybscloud.com/lfq"),"Queue")}
CompletionMemoryPolicy =
  {completion_memory_policy(m) |
     m : type_symbol(pkg("code.hybscloud.com/takt"),"CompletionMemory")}
SpinWaitPolicy =
  {spin_wait_policy(sw) | sw : type_symbol(pkg("code.hybscloud.com/spin"),"Wait")}
ProtocolPolicy =
  {parser_state(p), parser_branch_policy(p), route_lookup(p), route_retirement(p)
     | p ∈ PolicyName}
BufferPolicy =
  {buffer_wait_policy(buf), buffer_recycle_policy(buf) | buf ∈ BufferName}
CallerPolicy =
  BackoffPolicy ∪ MailboxPolicy ∪ CompletionMemoryPolicy ∪ SpinWaitPolicy ∪
  ProtocolPolicy ∪ BufferPolicy ∪
  { retry, poll_cadence, parking, scheduler,
    route_lookup, route_retirement, completion_routing,
    mailbox_handoff,
    runner_policy, parser_state, parser_branch_policy,
    safety_policy, callback_policy, service_lifecycle,
    completion_pool_policy, completion_gc_root_policy,
    metadata_route, lifecycle_policy, buffer_clear_policy,
    retirement_policy, resubscribe, cancel_timing, completion_buffer_policy }

BoundaryOnly ∩ CallerPolicy = ∅
BoundaryOnly ∩ CallerFrontier = ∅
support(BoundaryOnly) = BoundaryOnly

reject(hidden(CallerPolicy, support(B)))
reject(hidden(CallerFrontier, support(B)))
```

## Backoff Policy

```text
bo : type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")
uses_backoff(J_policy_separation, bo) → backoff_policy(bo) ∈ support(Π_ps)
uses_backoff(J_policy_separation, bo) →
  caller_state_owner(state(bo)) = C
state(bo) ∉ support(B)

q : type_symbol(pkg("code.hybscloud.com/lfq"),"Queue")
uses_mailbox(J_policy_separation, q) →
  mailbox_policy(q) ∈ support(Π_ps)
uses_mailbox(J_policy_separation, q) →
  caller_state_owner(state(q)) = C
state(q) ∉ support(B)

sw : type_symbol(pkg("code.hybscloud.com/spin"),"Wait")
uses_spin_wait(J_policy_separation, sw) →
  spin_wait_policy(sw) ∈ support(Π_ps)
uses_spin_wait(J_policy_separation, sw) →
  caller_state_owner(state(sw)) = C
state(sw) ∉ support(B)

Event = boundary_observation_events
next ⊆ Event × Event
≤ = reflexive_transitive_closure(next)
at : Fact × Event → Prop

CQE_observed_for_at(cqe, ctx, t) ⇔
  CQE_observed_at(cqe, t) ∧ cqe.user_data = raw(ctx)
CQE_observed_for(cqe, ctx) ⇔ ∃ t. CQE_observed_for_at(cqe, ctx, t)
terminal_frontier_at(args, t) ⇔
  at(terminal_frontier(args), t)
terminal_frontier_after_failure_at(args, t) ⇔
  at(terminal_frontier_after_failure(args), t)
terminal_completion_at(args, t) ⇔
  terminal_frontier_at(args, t)
  ∨ terminal_frontier_after_failure_at(args, t)
pending_at(ctx, x, t) ⇔
  at(pending(ctx, x), t)
ByteProgressAt(t) ⇔ ∃ n. n > 0 ∧ at(byte_progress(n), t)
ByteProgress ⇔ ∃ t. ByteProgressAt(t)
NoCompletion(ctx, t) ⇔
  ∃ x. pending_at(ctx, x, t)
  ∧ ¬∃ cqe, t'. t' ≤ t ∧ CQE_observed_for_at(cqe, ctx, t')
  ∧ ¬∃ args, t'. t' ≤ t ∧ terminal_completion_at(args, t') ∧ names(args, ctx)
NoCompletion(ctx, t) ↛ ByteProgressAt(t)
NoCompletion(ctx, t) →
  ¬∃ args, t'. t' ≤ t ∧ terminal_completion_at(args, t') ∧ names(args, ctx)

EventProgressAt(t) ⇔
  (∃ ctx, cqe. CQE_observed_for_at(cqe, ctx, t))
  ∨ (∃ bs. boundary_state_transition_observed_at(bs, t))
boundary_state_transition_observed_at(bs, t) ⇔
  at(boundary_state_transition(bs), t)
TerminalProgressAt(t) ⇔ ∃ args. terminal_completion_at(args, t)
LiveFrontierProgressAt(t) ⇔
  ∃ args.
    at(nonterminal_frontier(args), t)
    ∨ at(nonterminal_frontier_after_failure(args), t)
CallerStateProgressAt(t) ⇔ ∃ s. caller_visible_state_transition_at(s, t)
caller_visible_state_transition_at(s, t) ⇔
  at(caller_visible_state_transition(s), t)
ProgressEvidenceAt(t) ⇔
  ByteProgressAt(t)
  ∨ EventProgressAt(t)
  ∨ LiveFrontierProgressAt(t)
  ∨ TerminalProgressAt(t)
  ∨ CallerStateProgressAt(t)

WouldBlockObserved_ps ⇔ Obs_ps ∈ OutcomeProduct ∧ ctrl(Obs_ps) = wouldBlock
uses_backoff(J_policy_separation, bo) ∧ (WouldBlockObserved_ps ∨ NoCompletion(ctx, t)) →
  policy_step(Π_ps, bo.Wait)

visible_progress_at(t) ⇔ ProgressEvidenceAt(t)
uses_backoff(J_policy_separation, bo) ∧ visible_progress_at(t) →
  policy_step(Π_ps, bo.Reset)

∀ x ∈ CallerPolicy ∪ CallerFrontier.
  boundary_adapter ↛ hidden(x,support(B))
```

## Proof Obligations

```text
exposes(B, {wouldBlock, boundary_state_evidence})
decides(C,
  { spin_wait(pkg("code.hybscloud.com/spin"))
  , spin_yield(pkg("code.hybscloud.com/spin"))
  , parking
  , retry
  , route_lookup
  , route_retirement
  , lifecycle_policy
  })

PO_policy_separation =
  (∀ p ∈ CallerPolicy. p ∈ support(Π_ps) → policy_owner(p) = C)
  ∧ (∀ f ∈ CallerFrontier. f ∈ support(Σ_ps) → frontier_owner(f) = C)
  ∧ projection_to_boundary_preserves_only_boundary_facts
  ∧ policy_projection_to_boundary_rejected
  ∧ ¬hidden(completion_pool_policy, boundary_adapter)
  ∧ ¬hidden(completion_gc_root_policy, boundary_adapter)
  ∧ ¬hidden({route_lookup, route_retirement, lifecycle_policy}, boundary_adapter)
```
