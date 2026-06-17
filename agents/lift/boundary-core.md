# Boundary Core Lift

This file is the saved lift for the core boundary and the SQE/CQE correspondence: the checked judgment shape that real caller-side Go must preserve when it submits an operation and decodes a completion. Use it as the reference for what `Ctx`, the SQE carriers, and a decoded CQE *mean* in the model, so a change to the Go surface can be checked against the shape it must round-trip to. It records two lifts — the core boundary judgment and the SQE/CQE judgment — each with its initial lift and residual assumptions.

```text
topic = boundary_core
task_module = pkg("code.hybscloud.com/uring")
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_boundary_core) = ⟦J_boundary_core⟧_U

∀ stage ∈ StageSave.
  complete(stage,J_boundary_core)
    → saved(SaveLift(stage,task,J_boundary_core))
complete(lift,J_boundary_core)
  → saved(SaveLift(lift,task,J_boundary_core))
```

## Judgment

```text
J_boundary_core =
  Γ_core | Θ_core ⊢ boundary_action @ ring : result | Δ_core ▷ Θ_core'
    ; χ_core ; Σ_core ; Μ_core ; Φ_core ; Obs_core ; Rel_core ; Π_core

B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
K = below(task_module)

GuideEntrypoint = path("uring/agents/INDEX.md")
imports(GuideEntrypoint) =
  {support(B), support(K), Carrier, CallerFrontier, CallerPolicy, OutcomePlane,
   ReleaseObservation, ReleasePlane}
support(C) = CallerFrontier ∪ CallerPolicy

NameAtoms(X) = finite set of names occurring in X
NameAtoms(none) = ∅
facts(X) = {f | atomic_fact(f) ∧ NameAtoms(f) ⊆ NameAtoms(X)}
facts(none) = ∅
boundary_facts(J_boundary_core) =
  ⋃ {facts(X) | X ∈ {Δ_core, Θ_core, Θ_core', χ_core, Μ_core, Φ_core, Obs_core, Rel_core}}

WF(J_boundary_core) ⇔
  pairwise_disjoint(support(B), support(C), support(K))
  ∧ affine(Θ_core,Θ_core')
  ∧ Obs_core ∈ OutcomePlane
  ∧ Rel_core ∈ ReleasePlane
  ∧ frontier_owner(Σ_core) = C
  ∧ policy_owner(Π_core) = C
  ∧ support(Σ_core) ⊆ CallerFrontier
  ∧ support(Π_core) ⊆ CallerPolicy
  ∧ policy_free(J_boundary_core,B)

policy_free(J_boundary_core,B) ⇔
  support(Σ_core) ∩ support(B) = ∅
  ∧ support(Π_core) ∩ support(B) = ∅

BoundaryConceptAllowed =
  { value_structure, type_structure, abstraction, compilation, denotation
  , logical_relation, Hoare_projection, verification_evidence
  , linear_resource, temporal_obligation, outcome_quotient
  , coalgebraic_observation
  }
BoundaryConceptForbidden =
  { algebraic_effect, handler, shallow_handler, resumption
  , coeffect_requirement, modal_world, modal_later, session_frontier
  }
boundary_concept_ok(J_boundary_core,c) ⇔
  c ∈ BoundaryConceptAllowed
  ∧ admissible_concept_U(J_boundary_core,c)
  ∧ concept_utilized_U(J_boundary_core,c)
  ∧ ConceptRoute_U(c) ⊆ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
CallerConceptUse_boundary_core =
  {concept_use(ConceptOwner_U(c),c) |
     c ∈ BoundaryConceptForbidden
     ∧ c ∈ dom(ConceptOwner_U)
     ∧ used(c,J_boundary_core)}
∀ c ∈ BoundaryConceptForbidden.
  used(c,J_boundary_core) →
    c ∈ dom(ConceptOwner_U)
    ∧ concept_use(ConceptOwner_U(c),c) ∈ ConceptUse
    ∧ support(concept_use(ConceptOwner_U(c),c)) ⊆ C
    ∧ project(concept_use(ConceptOwner_U(c),c),B)=∅
```

## Initial Lift

```text
Action_boundary =
  {setup, register, unregister, encode, submit, wait, observe, decode,
   cancel, resize, provide, recycle, release, close, stop}

formalize_ok(J_boundary_core,boundary_action) ⇔
  boundary_action ∈ Action_boundary
  ∧ single_boundary_step(boundary_action)
  ∧ named(Δ_core, Θ_core, Θ_core', χ_core, Μ_core, Φ_core, Obs_core, Rel_core)

typecheck_ok(J_boundary_core,boundary_action) ⇔
  WF(J_boundary_core)
  ∧ ∀ fact ∈ boundary_facts(J_boundary_core). single_stratum_owner(fact)

reduce_ok(J_boundary_core,boundary_action) ⇔
  ¬hidden(support(Σ_core) ∪ support(Π_core), support(B))

prove_ok(J_boundary_core,boundary_action) ⇔
  stable_identity(Μ_core)
  ∧ explicit_ownership_epoch(Θ_core,Θ_core')
  ∧ capability_honesty(χ_core)
  ∧ precise_outcome_projection(Obs_core)
  ∧ precise_release_projection(Rel_core)
  ∧ ¬hidden({alloc, wait_loop, route_lookup, parser_branch_policy}, support(B))

lift_ok(J_boundary_core,boundary_action,go_surface) ⇔
  go_surface ∈ GoSurface
  ∧ BoundaryClean(go_surface)
  ∧ defined(⟦go_surface⟧⁻¹_U)
  ∧ ⟦go_surface⟧⁻¹_U ≃_B J_boundary_core
  ∧ preserves(go_surface, boundary_facts(J_boundary_core))

complete(formalize,J_boundary_core) ⇔
  ∃ boundary_action. formalize_ok(J_boundary_core,boundary_action)
complete(typecheck,J_boundary_core) ⇔
  ∃ boundary_action. typecheck_ok(J_boundary_core,boundary_action)
complete(reduce,J_boundary_core) ⇔
  ∃ boundary_action. reduce_ok(J_boundary_core,boundary_action)
complete(prove,J_boundary_core) ⇔
  ∃ boundary_action. prove_ok(J_boundary_core,boundary_action)
complete(lift,J_boundary_core) ⇔
  ∃ boundary_action,go_surface. lift_ok(J_boundary_core,boundary_action,go_surface)
```

## Residual Assumptions

```text
RA_boundary_core =
  { kernel_feature_truth → evidence(kernel_capability_probe ∨ documented_linux_abi)
  , errno_truth → evidence(projection(pkg("code.hybscloud.com/zcall")))
  , ∀ f. package_owned(f) → read_complete(pkg_owner(f))
  , explicit_caller(CallerPolicy,B)
  }

sound_initial_lift(J_boundary_core) ⇔
  WF(J_boundary_core)
  ∧ RA_boundary_core named
  ∧ valid_lift(lift,LiftRecord(lift,task,J_boundary_core),task,J_boundary_core)
  ∧ ∀ stage ∈ StageSave.
      complete(stage,J_boundary_core) → saved(SaveLift(stage,task,J_boundary_core))
```
## SQE/CQE Lift

This second lift records the SQE/CQE correspondence — the checked shape that caller-side Go must preserve when it encodes an SQE and decodes the matching CQE.

```text
topic = sqe_cqe
task_module = pkg("code.hybscloud.com/uring")
B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_sqe_cqe) = ⟦J_sqe_cqe⟧_U

complete(lift,J_sqe_cqe) ⇔
  ∃ go_surface.
    go_surface ∈ GoSurface
    ∧ BoundaryClean(go_surface)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ ⟦go_surface⟧⁻¹_U ≃_B J_sqe_cqe

∀ stage ∈ StageSave.
  complete(stage,J_sqe_cqe)
    → saved(SaveLift(stage,task,J_sqe_cqe))
complete(lift,J_sqe_cqe)
  → saved(SaveLift(lift,task,J_sqe_cqe))
```

## Judgment

```text
J_sqe_cqe =
  Γ_sqc | Θ_sqc ⊢ sqe_cqe_action @ ring : result | Δ_sqc ▷ Θ_sqc'
    ; χ_sqc ; Σ_sqc ; Μ_sqc ; Φ_sqc ; Obs_sqc ; Rel_sqc ; Π_sqc

sqe_cqe_action ∈ {encode, submit, wait, observe, decode}
BackoffPolicy =
  {backoff_policy(bo) | bo : type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")}
support(Σ_sqc) ⊆ CallerFrontier
support(Π_sqc) ⊆ CallerPolicy
frontier_owner(Σ_sqc) = C
policy_owner(Π_sqc) = C
Obs_sqc ∈ OutcomePlane
Rel_sqc ∈ ReleasePlane
frontier_fact(X,F) ⇔ F ∈ frontier_set(X)
frontier_carrier_sqc(ctx) =
  operation_carrier_named_by(ctx,Θ_sqc,Θ_sqc',Σ_sqc,Μ_sqc,Obs_sqc)
∀ ctx. OperationCarrier(frontier_carrier_sqc(ctx))
ZeroCopyReleaseCarrierKind_sqc = {ExtSQE}
ZeroCopySendOperation_sqc = {op | zero_copy_send_opcode(op)}
zero_copy_send_ctx_sqc(ctx) ⇔ op_of(ctx) ∈ ZeroCopySendOperation_sqc
zero_copy_release_carrier_sqc(ctx,r) ⇔
  names(ctx,r)
  ∧ OperationCarrier(r)
  ∧ resource_kind(r) ∈ ZeroCopyReleaseCarrierKind_sqc
ResultResource_sqc(out) ⇔
  out ∈ Resource
  ∧ resource_kind(out) ∈ {FD,DirectFD,Buf,FixedBuf,ProvidedBuf}
FDResultResource_sqc(out) ⇔
  ResultResource_sqc(out) ∧ resource_kind(out) ∈ {FD,DirectFD}
fd_of : {out | FDResultResource_sqc(out)} ⇀ DescriptorResult
result_resource_sqc(cqe,ctx,out) ⇔
  ResultResource_sqc(out)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource_sqc(out)
      ∧ defined(fd_of(out))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(out)))
     ∨ selected_buffer_result(cqe,out)
     ∨ explicit_result_resource(cqe,out))
```

## SQE Encoding

```text
T-Encode:
  Γ_sqc ⊢ opcode(op)
  Γ_sqc ⊢ raw(ctx) : UserData
  Γ_sqc ⊢ fields_well_formed(op,fields)
  Θ_sqc ⊢ owns(resources(fields))
  χ_sqc ⊢ capability_required(op)
  ------------------------------------------------------------
  Γ_sqc | Θ_sqc ⊢ encode(op,fields,ctx) @ ring : SQE
    | demand(op,fields,ctx) ▷ Θ_sqc
    ; χ_sqc ; Σ_sqc ; identity(ctx) ; Φ_sqc ; Obs_encode(ctx,fields) ; none ; Π_sqc

encode_preserves_identity ⇔
  encode(op,fields,ctx) → user_data(SQE) = raw(ctx)

encode_rejects_policy ⇔
  support(fields) ∩ (CallerFrontier ∪ CallerPolicy) = ∅

Obs_encode(ctx,fields) =
  obs(ev(empty, flags(raw_flags_none), identity(ctx),
         resource_frontier(Θ_sqc,Θ_sqc), capability(χ_sqc),
         boundary_state(none)), ok)
```

## CQE Observation

```text
CQE_observed_for(cqe,ctx) ⇔ CQE_observed(cqe) ∧ cqe.user_data = raw(ctx)

T-Observe:
  may_arrive(cqe,ctx)
  CQE_observed_for(cqe,ctx)
  ------------------------------------------------------------
observe(cqe,ctx) ↦ Μ_sqc ⊇ {identity(ctx), cqe_fact(cqe)}

project_res(op,res) =
  typed_error(-res) if res < 0
  decode_result(op,res)    if res ≥ 0

project_flags(raw_flags) = flag_projection(raw_flags)
has_MORE(raw_flags) → flag(MORE) ∈ project_flags(raw_flags)
has_NOTIF(raw_flags) → flag(NOTIF) ∈ project_flags(raw_flags)

project_cqe(op,cqe,ctx,Θ_sqc,Θ_sqc',χ_sqc) =
  ev(project_res(op,cqe.Res),
     project_flags(cqe.Flags),
     identity(ctx),
     resource_frontier(Θ_sqc,Θ_sqc'),
     capability(χ_sqc),
     boundary_state(none))

Hist(ctx) =
  { H | finite_seq(H)
        ∧ ∀ cqe ∈ H. cqe.user_data = raw(ctx) }
history(ctx) ∈ Hist(ctx)
data_frontier_seen(ctx,H) ⇔
  ∃ cqe_data ∈ H.
    cqe_data.user_data = raw(ctx)
    ∧ operation_cqe(cqe_data)
    ∧ flag(MORE) ∈ flag_projection(cqe_data.Flags)
early_notification_seen(ctx,H) ⇔
  ∃ cqe_notify ∈ H.
    cqe_notify.user_data = raw(ctx)
    ∧ notification_cqe(cqe_notify)
    ∧ ¬data_frontier_seen(ctx,prefix(H,cqe_notify))
terminal_fallback_carrier_sqc(ctx,r) ⇔
  zero_copy_send_ctx_sqc(ctx)
  ∧ zero_copy_release_carrier_sqc(ctx,r)
early_notification_cqe(cqe,ctx,H) ⇔
  notification_cqe(cqe)
  ∧ cqe.user_data = raw(ctx)
  ∧ ¬data_frontier_seen(ctx,H)
EarlyNotification_sqc(cqe,ctx) ⇔
  early_notification_cqe(cqe,ctx,history(ctx))
  ∧ zero_copy_send_ctx_sqc(ctx)
  ∧ zero_copy_release_carrier_sqc(ctx,frontier_carrier_sqc(ctx))
notification_release_ready_cqe(cqe,ctx,H) ⇔
  (notification_cqe(cqe)
   ∧ cqe.user_data = raw(ctx)
   ∧ data_frontier_seen(ctx,H))
  ∨ (operation_cqe(cqe)
     ∧ cqe.user_data = raw(ctx)
     ∧ early_notification_seen(ctx,H))
terminal_no_notification_cqe(cqe,ctx,r,H) ⇔
  operation_cqe(cqe)
  ∧ cqe.user_data = raw(ctx)
  ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  ∧ terminal_fallback_carrier_sqc(ctx,r)
  ∧ ¬data_frontier_seen(ctx,H)
  ∧ ¬early_notification_seen(ctx,H)
release_ready_carrier_at_sqc(cqe,ctx,r,H) ⇔
  cqe.user_data = raw(ctx)
  ∧ zero_copy_send_ctx_sqc(ctx)
  ∧ zero_copy_release_carrier_sqc(ctx,r)
  ∧ (notification_release_ready_cqe(cqe,ctx,H)
     ∨ terminal_no_notification_cqe(cqe,ctx,r,H))
release_ready_cqe(cqe,ctx,H) ⇔
  ∃ r. release_ready_carrier_at_sqc(cqe,ctx,r,H)
flag(f) ∈ release_flags(cqe,ctx,H) ⇔
  flag(f) ∈ project_flags(cqe.Flags)
  ∨ (f = NOTIF ∧ early_notification_seen(ctx,H))
notification_release_ready_cqe(cqe,ctx,H) →
  flag(NOTIF) ∈ release_flags(cqe,ctx,H)
terminal_no_notification_cqe(cqe,ctx,r,H) →
  ¬(flag(NOTIF) ∈ release_flags(cqe,ctx,H))

project_release_cqe(cqe,ctx,H,r,χ_sqc) =
  ev(empty,
     release_flags(cqe,ctx,H),
     identity(ctx),
     release_frontier(ctx,r),
     capability(χ_sqc),
     boundary_state(none))

operation_cqe(cqe) ⇔ flag(NOTIF) ∉ flag_projection(cqe.Flags)
notification_cqe(cqe) ⇔ flag(NOTIF) ∈ flag_projection(cqe.Flags)

q_cqe(cqe) =
  ok                          if CQE_case_success_more(cqe)
  ok                          if CQE_case_success_terminal(cqe)
  fail(typed_error(-cqe.Res)) if CQE_case_failure_more(cqe)
  fail(typed_error(-cqe.Res)) if CQE_case_failure_terminal(cqe)

CQE_case_success_more(cqe)     ⇔ operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
CQE_case_success_terminal(cqe) ⇔ operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
CQE_case_failure_more(cqe)     ⇔ operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
CQE_case_failure_terminal(cqe) ⇔ operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
CQE_case_partition(cqe) ⇔
  (CQE_case_success_more(cqe)
   ∨ CQE_case_success_terminal(cqe)
   ∨ CQE_case_failure_more(cqe)
   ∨ CQE_case_failure_terminal(cqe)
   ∨ notification_cqe(cqe))
  ∧ ¬(CQE_case_success_more(cqe) ∧ CQE_case_success_terminal(cqe))
  ∧ ¬(CQE_case_success_more(cqe) ∧ CQE_case_failure_more(cqe))
  ∧ ¬(CQE_case_success_more(cqe) ∧ CQE_case_failure_terminal(cqe))
  ∧ ¬(CQE_case_success_more(cqe) ∧ notification_cqe(cqe))
  ∧ ¬(CQE_case_success_terminal(cqe) ∧ CQE_case_failure_more(cqe))
  ∧ ¬(CQE_case_success_terminal(cqe) ∧ CQE_case_failure_terminal(cqe))
  ∧ ¬(CQE_case_success_terminal(cqe) ∧ notification_cqe(cqe))
  ∧ ¬(CQE_case_failure_more(cqe) ∧ CQE_case_failure_terminal(cqe))
  ∧ ¬(CQE_case_failure_more(cqe) ∧ notification_cqe(cqe))
  ∧ ¬(CQE_case_failure_terminal(cqe) ∧ notification_cqe(cqe))
q_cqe_total(cqe) ⇔ operation_cqe(cqe) → ∃! o ∈ Ctrl[E]. q_cqe(cqe) = o
notification_cqe(cqe) → ¬defined(q_cqe(cqe))

defined(ObsProjection_sqc(cqe,ctx)) ⇔ operation_cqe(cqe)
ObsProjection_sqc(cqe,ctx) =
  obs(project_cqe(op,cqe,ctx,Θ_sqc,Θ_sqc',χ_sqc), q_cqe(cqe))
    if operation_cqe(cqe)
Obs_sqc = ObsProjection_sqc(cqe,ctx)
  if defined(ObsProjection_sqc(cqe,ctx))
Obs_sqc = none
  if EarlyNotification_sqc(cqe,ctx)

defined(RelProjection_sqc(cqe,ctx)) ⇔
  release_ready_carrier_at_sqc(cqe,ctx,frontier_carrier_sqc(ctx),history(ctx))
RelProjection_sqc(cqe,ctx) =
  release_obs(project_release_cqe(cqe,ctx,history(ctx),frontier_carrier_sqc(ctx),χ_sqc))
    if release_ready_carrier_at_sqc(cqe,ctx,frontier_carrier_sqc(ctx),history(ctx))
Rel_sqc = RelProjection_sqc(cqe,ctx)
  if defined(RelProjection_sqc(cqe,ctx))
```

## Control Projection

```text
terminal_completion(ctx,r) ⇔
  frontier_fact(Obs_sqc, terminal_frontier(ctx,r))
  ∨ frontier_fact(Obs_sqc, terminal_frontier_after_failure(ctx,r))

operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags) →
  Obs_sqc ∈ OutcomeProduct
  ∧ ctrl(Obs_sqc) = ok
  ∧ has(flag(MORE),Obs_sqc)
  ∧ frontier_fact(Obs_sqc, nonterminal_frontier(ctx,frontier_carrier_sqc(ctx)))
  ∧ successor_observation_possible(ctx)

(Obs_sqc ∈ OutcomeProduct ∧ has(flag(MORE),Obs_sqc)) ↛
  terminal_completion(ctx,frontier_carrier_sqc(ctx))

operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags) →
  Obs_sqc ∈ OutcomeProduct
  ∧ ctrl(Obs_sqc) = ok
  ∧ frontier_fact(Obs_sqc, terminal_frontier(ctx,frontier_carrier_sqc(ctx)))

operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags) →
  Obs_sqc ∈ OutcomeProduct
  ∧ has(flag(MORE),Obs_sqc)
  ∧ frontier_fact(Obs_sqc,
                   nonterminal_frontier_after_failure(ctx,frontier_carrier_sqc(ctx)))
  ∧ ctrl(Obs_sqc) = fail(typed_error(-cqe.Res))

operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags) →
  Obs_sqc ∈ OutcomeProduct
  ∧ ctrl(Obs_sqc) = fail(typed_error(-cqe.Res))
  ∧ frontier_fact(Obs_sqc,
                   terminal_frontier_after_failure(ctx,frontier_carrier_sqc(ctx)))

release_ready_carrier_at_sqc(cqe,ctx,frontier_carrier_sqc(ctx),history(ctx)) →
  RelProjection_sqc(cqe,ctx) ∈ ReleaseObservation
  ∧ ¬defined(ctrl(RelProjection_sqc(cqe,ctx)))
  ∧ frontier_fact(RelProjection_sqc(cqe,ctx),
                  release_frontier(ctx,frontier_carrier_sqc(ctx)))
notification_release_ready_cqe(cqe,ctx,history(ctx)) →
  flag(NOTIF) ∈ release_flags(cqe,ctx,history(ctx))
terminal_no_notification_cqe(cqe,ctx,frontier_carrier_sqc(ctx),history(ctx)) →
  ¬(flag(NOTIF) ∈ release_flags(cqe,ctx,history(ctx)))

EarlyNotification_sqc(cqe,ctx) →
  Obs_sqc = none
  ∧ ¬defined(ObsProjection_sqc(cqe,ctx))
  ∧ ¬defined(q_cqe(cqe))
  ∧ ¬defined(RelProjection_sqc(cqe,ctx))
  ∧ Rel_sqc = none

result_resource_sqc(cqe,ctx,out) →
  caller_owns(out)
  ∧ ∀ ctx2. ¬frontier_fact(Obs_sqc, nonterminal_frontier(ctx2,out))

(Obs_sqc ∈ OutcomeProduct ∧ ctrl(Obs_sqc) = errMore) ↛ ByteProgress
(decode_result(op,n) = count(n) ∧ n > 0) → byte_progress(n)
ByteProgress ⇔ ∃ n. n > 0 ∧ byte_progress(n)
```

## Proof Obligations

```text
PO_sqe_cqe =
  (may_arrive(cqe,ctx) → stable(user_data(ctx)))
  ∧ CQE_case_partition(cqe)
  ∧ (operation_cqe(cqe) → q_cqe_total(cqe))
  ∧ (release_ready_carrier_at_sqc(cqe,ctx,frontier_carrier_sqc(ctx),history(ctx))
     → RelProjection_sqc(cqe,ctx) ∈ ReleaseObservation)
  ∧ (EarlyNotification_sqc(cqe,ctx)
     → Obs_sqc = none ∧ Rel_sqc = none ∧ ¬defined(RelProjection_sqc(cqe,ctx)))
  ∧ preserves_exact(cqe.Res)
  ∧ preserves_separate(cqe.Flags, Ctrl[E])
  ∧ ((operation_cqe(cqe) ∧ cqe.Res < 0) → preserves(typed_error(-cqe.Res)))
  ∧ preserves(IORING_CQE_F_MORE, frontier_evidence)
  ∧ (release_ready_carrier_at_sqc(cqe,ctx,frontier_carrier_sqc(ctx),history(ctx))
      ∧ notification_release_ready_cqe(cqe,ctx,history(ctx))
      → preserves(IORING_CQE_F_NOTIF, RelProjection_sqc(cqe,ctx)))
  ∧ ¬hidden(route_lookup, decode)
  ∧ ¬hidden(BackoffPolicy ∪ {retry, parking, scheduler}, encode ∪ decode)
```
