# Context Evidence Utilization

Because handling a completion is shallow, the world can advance between the moment you submit an operation and the moment you resume on its CQE — so the requirement evidence you relied on must still hold at the point of use. `code.hybscloud.com/cove` is how you carry and re-check that evidence across a suspension boundary. This file is the rule for using `code.hybscloud.com/cove` with `code.hybscloud.com/uring`: requirement and safety evidence is caller-owned, epoch-monotone where it may be cached and re-checked at the use epoch where it may not, and never projected into the boundary.

In plain terms, the rules an agent follows here are:

- Carry context across the shallow gap, and re-check at use. Between observe and resume the world may advance, so cache only monotone facts (capability present, registration done, terminal observed) and re-check non-monotone facts (frontier live, buffer owned, fd open, ring not closed, token live) at the resume or dispatch epoch.
- Resume does not validate for you. Resuming a suspension view performs no requirement check; guard it explicitly with a requirement (`Need`/`NeedExpr`), and if a requirement fails, the wait, reroute, or abandon decision is the caller's.
- Copy completion facts into the context, never a borrowed pointer. A context extension carries copied `Res`/`Flags`/`user_data`, metadata, capability, and ownership facts; a borrowed CQE pointer is never in the world or the context.
- Keep all carriers from `code.hybscloud.com/cove` in the caller layer. Context, requirement, rule, suspension-view, and Kripke carriers are caller-owned, never projected into the boundary, and never used to mutate the boundary frontiers `Θ`/`Σ`/`Μ`.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

owner(evidence_context) = pkg("code.hybscloud.com/cove")
owner(requirement_rule) = pkg("code.hybscloud.com/cove")
owner(suspension_view) = pkg("code.hybscloud.com/cove")
owner(world_transition) = pkg("code.hybscloud.com/cove")
owner(step_indexed_relation) = pkg("code.hybscloud.com/cove")
owner(checked_value) = pkg("code.hybscloud.com/cove")
owner(kernel_boundary_facts) = pkg("code.hybscloud.com/uring")

CoveTheoryConcept =
  {coeffect_requirement, modal_world, modal_later}
∀ c ∈ CoveTheoryConcept.
  runtime_concept_use(pkg("code.hybscloud.com/cove"),c) ∈ RuntimeConceptUse
  ∧ use_pkg(runtime_concept_use(pkg("code.hybscloud.com/cove"),c))
      = pkg("code.hybscloud.com/cove")
  ∧ support(runtime_concept_use(pkg("code.hybscloud.com/cove"),c)) ⊆ C
  ∧ project(runtime_concept_use(pkg("code.hybscloud.com/cove"),c),B) = ∅

allow(C, import(pkg("code.hybscloud.com/cove")))
reject(B, import(pkg("code.hybscloud.com/cove")))

Ctx = caller_chosen_context
CtxCarrier =
  {View[Ctx,A], Cmd[Ctx,A,R], SuspensionView[Ctx,A],
   Req[Ctx], Rule[Ctx], Report, Checked[Ctx,A], Guarded[Ctx,A]}
ExprCarrier =
  {ReqExpr[Ctx], RuleExpr[Ctx], CheckedExpr[Ctx,A], GuardedExpr[Ctx,A]}
KripkeCarrier =
  {StepIndex, Preorder[Ctx], Transition[Ctx], Forcing[Ctx],
   ForcingExpr[Ctx], Relation[Ctx,A], IndexedView[Ctx,A],
   IndexedSuspensionView[Ctx,A]}
MonotoneFacts =
  {capability_present, registration_done, terminal_observed}
NonMonotoneFacts =
  {frontier_live, buffer_owned, fd_open, ring_not_closed, token_live}
Source_cove =
  {path("cove/constraint.go"), path("cove/view.go"), path("cove/cmd.go"),
   path("cove/req.go"), path("cove/req_expr.go"), path("cove/rule.go"),
   path("cove/rule_expr.go"), path("cove/checked.go"),
   path("cove/checked_expr.go"), path("cove/step.go"),
   path("cove/kripke.go"), path("cove/bridge.go")}
source_checked(Source_cove) ⇔ ∀ f ∈ Source_cove. read_current_source(f)
World = (MonotoneFactsState, EphemeralFactsState, Requirements, Epoch)
monotone_facts(w) ⊆ MonotoneFacts
ephemeral_facts(w) ⊆ NonMonotoneFacts
requirements(w) ⊆ Requirements
epoch(w) ∈ Epoch
facts(w) = monotone_facts(w) ∪ ephemeral_facts(w)
WorldPreorder(w0,w1) ⇔
  monotone_facts(w0) ⊆ monotone_facts(w1)
  ∧ requirements(w0) ⊆ requirements(w1)
  ∧ epoch(w0) ≤ epoch(w1)
holds(F,w) ⇔ F ∈ facts(w)
MonotoneFact(F) ⇔
  ∀w0,w1. WorldPreorder(w0,w1) ∧ holds(F,w0) → holds(F,w1)
F ∈ MonotoneFacts → MonotoneFact(F)
F ∈ NonMonotoneFacts → ¬assume(MonotoneFact(F))

⟦Need(w,req)⟧ = checked(req,w) ⊎ missing(req,w)
⟦NeedExpr(w,χ)⟧ = checked(χ,w) ⊎ missing(χ,w)
checked(req,w) → req(w)
F ∈ NonMonotoneFacts → check_at_use_epoch(F)
F ∈ NonMonotoneFacts → reject(cache_across(F,suspension_frontier))

SuspensionView(ctx,s) →
  ctx ∈ World ∧ s ∈ type_symbol(pkg("code.hybscloud.com/kont"),"Suspension[A]")
pending_suspension(s) ⇔ s ≠ nil ∧ ¬resumed(s) ∧ ¬discarded(s)
resume(SuspensionView(ctx,s),v) defined ⇔ pending_suspension(s)
resume(SuspensionView(ctx,s),v) ↛ check(requirements_hold(ctx))
CheckSuspension(ctx,s,req) =
  (SuspensionView(ctx,s), true) if ⟦Need(ctx,req)⟧ = checked(req,ctx)
  (⊥, false)                   if ⟦Need(ctx,req)⟧ = missing(req,ctx)
CheckSuspensionExpr(ctx,s,χ) =
  (SuspensionView(ctx,s), true) if ⟦NeedExpr(ctx,χ)⟧ = checked(χ,ctx)
  (⊥, false)                   if ⟦NeedExpr(ctx,χ)⟧ = missing(χ,ctx)
Guard = Req[Ctx] ⊎ ReqExpr[Ctx]
guard_holds(ctx,inl(req)) ⇔ ⟦Need(ctx,req)⟧ = checked(req,ctx)
guard_holds(ctx,inr(χ))   ⇔ ⟦NeedExpr(ctx,χ)⟧ = checked(χ,ctx)
GuardedSuspensionView(ctx,s,guard) ⇔
  SuspensionView(ctx,s)
  ∧ guard ∈ Guard
  ∧ guard_holds(ctx,guard)
CodingObligation_cove(ctx,s,guard,cqe) ⇔
  source_checked(Source_cove)
  ∧ GuardedSuspensionView(ctx,s,guard)
  ∧ support(ContextExtension(ctx,cqe)) ⊆ C
  ∧ borrowed_cqe_pointer ∉ ContextExtension(ctx,cqe)
  ∧ ∀ F ∈ NonMonotoneFacts. check_at_use_epoch(F)
  ∧ ∀ F ∈ NonMonotoneFacts. reject(cache_across(F,suspension_frontier))
  ∧ project(CtxCarrier ∪ ExprCarrier ∪ KripkeCarrier ∪ {World},B) = ∅
resume(GuardedSuspensionView(ctx,s,guard),v) →
  requirement_evidence(guard,ctx) ∈ C
¬requirements_hold(ctx) → decision_owner({wait, reroute, abandon}) = C

BoundaryContextFact(cqe) = ⟦observe_cqe(cqe)⟧_B
BoundaryContextFact(cqe) =
  copy({cqe.Res, cqe.Flags, cqe.user_data, selected_buffer_metadata(cqe),
        capability_fact(cqe), ownership_fact(cqe)})
ContextExtension(ctx, cqe) =
  extend(ctx, BoundaryContextFact(cqe))
support(ContextExtension(ctx, cqe)) ⊆ C
project(ContextExtension(ctx, cqe), B) = ∅
borrowed_cqe_pointer ∉ World
borrowed_cqe_pointer ∉ BoundaryContextFact(cqe)

defined_at(shallow_effect_handler) = path("uring/agents/boundary/protocols.md")
shallow_gap(s) = (observe_epoch(s), resume_epoch(s))
observe_epoch(s) ≤ resume_epoch(s)
use_epoch(resume(SuspensionView(ctx,s),v)) = resume_epoch(s)
ResumeWith(SuspensionView(ctx,s),v,f) →
  next_ctx = if f=nil then ctx else f(ctx)
  ∧ support(f) ⊆ C
ResumeWith(SuspensionView(ctx,s),v,f)
  ↛ validate(next_ctx)
guard_required(next_ctx) →
  CheckSuspension(next_ctx,next_suspension,req)
  ⊎ CheckSuspensionExpr(next_ctx,next_suspension,χ)
ResumeTo(IndexedSuspensionView(ctx,i,s),leq,v,next_ctx) →
  leq(ctx,next_ctx)
  ∧ i > 0
  ∧ next_index = i - 1

Need : Ctx × Req[Ctx] → bool
NeedExpr : Ctx × ReqExpr[Ctx] → bool
ExprAll, ExprAny : ReqExpr[Ctx]* → ReqExpr[Ctx]
ExprNot : ReqExpr[Ctx] → ReqExpr[Ctx]
ExprPullback : ReqExpr[Ctx2] × (Ctx → Ctx2) → ReqExpr[Ctx]
handler_guard(h) ∈ ReqExpr[Ctx]
guard_check_epoch(h) = dispatch_epoch
¬NeedExpr(ctx, handler_guard(h)) → decision_owner({suppress, close, reroute}) = C

Relation[Ctx,A] = Ctx × StepIndex × A → Prop
Later(Relation[Ctx,A]) = guarded_step_delay(Relation[Ctx,A])
WeakenIndexedView(v,i') defined ⇔ index(v) ≥ i'
WeakenIndexedSuspension(v,i') defined ⇔ index(v) ≥ i'
CheckCompletedRelation(v,a,rel) defined only if completed(v)
KripkeCarrier ↛ mutate(Θ)
KripkeCarrier ↛ mutate(Σ)
KripkeCarrier ↛ mutate(Μ)
KripkeCarrier ↛ prove(endpoint_progress)

support(CtxCarrier ∪ ExprCarrier ∪ KripkeCarrier
        ∪ {World, Need, NeedExpr, force, rule_report}) ⊆ C
support(kernel_boundary_facts) ⊆ B
project(CtxCarrier ∪ ExprCarrier ∪ KripkeCarrier ∪ {World}, B) = ∅
reject(project({Need, NeedExpr, force, rule_report, ResumeTo}, B))
```
