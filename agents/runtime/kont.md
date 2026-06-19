# Suspension Utilization

When you suspend on a pending ring operation, `code.hybscloud.com/kont` is how you do it correctly: one suspension, one resume, matched to one operation identity. This file is the rule for wiring `code.hybscloud.com/kont` onto a `code.hybscloud.com/uring` boundary action. A one-shot operation maps to an affine `Suspension` resumed at most once with *copied* completion facts (never a borrowed CQE view); a multishot operation is rejected here and routed through `code.hybscloud.com/takt` instead; and `code.hybscloud.com/kont` itself stays entirely in the caller layer, its carriers never projected into the boundary. This is the package-level instance of the shallow handler: resume on one observation, hand control back.

In plain terms, the rules an agent follows here are:

- One suspension, one resume, one identity. A one-shot operation maps to an affine `Suspension` resumed at most once; resume consumes both the suspension and the operation identity, and a second resume is rejected.
- Resume on copied facts, never a borrowed view. The resume value copies `Res`, `Flags`, `user_data`, and selected-buffer metadata; a borrowed CQE view is never stored, and a continuation closure captures only copied facts or caller-owned state.
- Multishot does not belong here. A multishot operation is rejected from a `Suspension` and routed through `SubscriptionLoop` from `code.hybscloud.com/takt` instead.
- Discarding is not cancelling. Discarding a suspension consumes it but does not cancel the pending operation; cancellation is a separate visible boundary action, and the discard decision is the caller's.
- Step index is proof fuel only. It bounds reasoning, and never changes runtime affinity or creates progress evidence.
- Keep all carriers from `code.hybscloud.com/kont` in the caller layer. Effect, computation, suspension, and step carriers are caller-owned and never projected into the boundary.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

owner(suspension_frontier) = pkg("code.hybscloud.com/kont")
owner(affine_resumption) = pkg("code.hybscloud.com/kont")
owner(effect_operation_shape) = pkg("code.hybscloud.com/kont")
owner(reify_reflect_bridge) = pkg("code.hybscloud.com/kont")
owner(step_index_witness) = pkg("code.hybscloud.com/kont")
owner(nil_completion_convention) = pkg("code.hybscloud.com/kont")
owner(kernel_boundary_facts) = pkg("code.hybscloud.com/uring")

KontTheoryConcept =
  {algebraic_effect, handler, shallow_handler, resumption}
∀ c ∈ KontTheoryConcept.
  runtime_concept_use(pkg("code.hybscloud.com/kont"),c) ∈ RuntimeConceptUse
  ∧ use_pkg(runtime_concept_use(pkg("code.hybscloud.com/kont"),c))
      = pkg("code.hybscloud.com/kont")
  ∧ support(runtime_concept_use(pkg("code.hybscloud.com/kont"),c)) ⊆ C
  ∧ project(runtime_concept_use(pkg("code.hybscloud.com/kont"),c),B) = ∅

allow(C, import(pkg("code.hybscloud.com/kont")))
reject(B, import(pkg("code.hybscloud.com/kont")))

EffectCarrier = {Operation, Op[O,A], Phantom[A], Resumed}
ComputationCarrier = {Cont[R,A], Eff[A], Expr[A], Reify, Reflect}
StepCarrier = {Step, StepExpr, StepIndex}
SuspCarrier = {Suspension[A], Affine[R,A], EffectFrame[A]}
BoundaryObservation =
  {cqe_res, cqe_flags, user_data, selected_buffer, capability_fact,
   ownership_fact}
Source_kont =
  {path("kont/effect.go"), path("kont/cont.go"), path("kont/frame.go"),
   path("kont/bridge.go"), path("kont/step.go"), path("kont/trampoline.go"),
   path("kont/index.go"), path("kont/affine.go"), path("kont/marker_pool.go"),
   path("kont/pool.go"), path("kont/dispatch.go")}
source_checked(Source_kont) ⇔ ∀ f ∈ Source_kont. read_current_source(f)
BoundaryWrapped(s) ⇔
  s ∈ Suspension[A] ∧ pending_op(s) ∈ boundary_action(B)
CodingObligation_kont(op,s,cqe) ⇔
  source_checked(Source_kont)
  ∧ BoundaryWrapped(s)
  ∧ pending_op(s)=op
  ∧ one_shot(op)
  ∧ affine(s)
  ∧ resume_value(cqe) =
      copy({cqe.Res, cqe.Flags, cqe.user_data, selected_buffer_metadata(cqe)})
  ∧ resume_value(cqe) ∩ borrowed_cqe_view = ∅
  ∧ closure_capture(continuation_of(s))
      ⊆ copied_completion_facts ∪ caller_owned_state
  ∧ project({s,resume,try_resume,discard},B) = ∅
multishot(op) → ¬CodingObligation_kont(op,s,cqe)
multishot(op) → route_via(SubscriptionLoop(pkg("code.hybscloud.com/takt")))

⟦Perform(op)⟧ = suspended(effect_operation_shape(op))
⟦ExprPerform(op)⟧ = suspended(effect_operation_shape(op))
⟦Reify(Cont[Resumed,A])⟧ = Expr[A]
⟦Reflect(Expr[A])⟧ = Cont[Resumed,A]
⟦Reflect(Reify(m))⟧ ≃ m modulo representation_cost

⟦Step(comp)⟧ = done(a,nil_suspension) ⊎ suspended(s)
⟦StepExpr(expr)⟧ = done(a,nil_suspension) ⊎ suspended(s)
done(a,nil_suspension) → completion_witness = nil_suspension
done(a,nil_suspension) → value(a) ∈ A
nilable(A) → reject(use_nil_value_as_completion_witness)
suspended(s) → s ∈ Suspension[A] ∧ completion_witness ≠ nil_suspension
BoundaryWrapped(s) → pending_op(s) ∈ boundary_action(B)
BoundaryWrapped(s) ∧ pending_op(s) = action(op) → one_boundary_action(action(op))

one_shot(op) ⇔ ∃! terminal_frontier(op)
one_shot(op) → at_most_one(resumption_value(op))
one_shot(op) ∧ BoundaryWrapped(s) ∧ pending_op(s)=op
  → admissible_map(op,Suspension[A])
multishot(op) → reject(map_to(op,Suspension[A]))
multishot(op) → route_via(SubscriptionLoop(pkg("code.hybscloud.com/takt")))

StepIndex = finite_proof_fuel
StepIndex ↛ change_runtime_affinity(Suspension[A])
StepIndex ↛ create_progress_evidence
consume(StepIndex) ∈ proof_obligation(C)

affine(s) ⇔ resume_count(s) ≤ 1
resume(s,v) defined ⇔ ¬resumed(s) ∧ ¬discarded(s)
resume(s,v) → consumed(s)
resumed(s) ∧ resume(s,v') → reject
discard(s) → consumed(s) ∧ decision_owner(discard) = C
discard(s) ↛ cancel(pending_op(s))

terminal_cqe(cqe,op) ∧ BoundaryWrapped(s) ∧ pending_op(s)=op ∧ resume(s,resume_value(cqe))
  → consumed(identity(op)) ∧ consumed(s)
resume_value(cqe) =
  copy({cqe.Res, cqe.Flags, cqe.user_data, selected_buffer_metadata(cqe)})
resume_value(cqe) ∩ borrowed_cqe_view = ∅

defined_at(shallow_effect_handler) = path("uring/agents/boundary/protocols.md")
resume_step(s,cqe) = resume(s, resume_value(cqe))
one_shot(op) ∧ BoundaryWrapped(s) ∧ pending_op(s) = op →
  handles_one_visible_frontier(resume_step(s,cqe))
affine(s) → resumes_at_most_once_per_observation(resume_step(s,cqe))
resume_value(cqe) ∩ borrowed_cqe_view = ∅ →
  stores_no_boundary_borrow(resume_step(s,cqe))
continuation_of(s) = go_closure
closure_capture(continuation_of(s)) ⊆ copied_completion_facts ∪ caller_owned_state
closure_capture(continuation_of(s)) ∩ borrowed_cqe_view = ∅

release_policy({Bracket, OnError}) ⊆ CallerPolicy
support(EffectCarrier ∪ ComputationCarrier ∪ SuspCarrier ∪ StepCarrier
        ∪ {resume, try_resume, discard}) ⊆ C
support(BoundaryObservation) ⊆ B
project(EffectCarrier ∪ ComputationCarrier ∪ SuspCarrier ∪ StepCarrier, B) = ∅
reject(project({Suspension, Affine, Step, StepExpr, StepIndex,
                Reify, Reflect, resume, try_resume, discard}, B))
```
