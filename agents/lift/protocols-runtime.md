# Protocols and Runtime Carriers Lift

This file is the saved lift for multishot protocol frontiers and the caller-layer runtime carriers (`code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, `code.hybscloud.com/sess`): the checked judgment shape that caller-side Go must preserve when it drives a multishot stream or wires a runtime package onto a ring operation. Use it as the reference for how protocol frontiers and the runtime carriers attach to the boundary, so a change can be checked against the routing, suspension, and policy-placement discipline it must round-trip to. It records two lifts — multishot/protocols and runtime carriers.

```text
topic = multishot_protocols
task_module = pkg("code.hybscloud.com/uring")
B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_multishot_protocols) = ⟦J_multishot_protocols⟧_U

complete(lift,J_multishot_protocols) ⇔
  ∃ go_surface.
    go_surface ∈ GoSurface
    ∧ BoundaryClean(go_surface)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ ⟦go_surface⟧⁻¹_U ≃_B J_multishot_protocols

∀ stage ∈ StageSave.
  complete(stage,J_multishot_protocols)
    → saved(SaveLift(stage,task,J_multishot_protocols))
complete(lift,J_multishot_protocols)
  → saved(SaveLift(lift,task,J_multishot_protocols))
```

## Judgment

```text
J_multishot_protocols =
  Γ_mp | Θ_mp ⊢ multishot_action @ ring : result | Δ_mp ▷ Θ_mp'
    ; χ_mp ; Σ_mp ; Μ_mp ; Φ_mp ; Obs_mp ; Rel_mp ; Π_mp
Obs_mp ∈ OutcomePlane
Rel_mp ∈ ReleasePlane
frontier_fact(X,F) ⇔ F ∈ frontier_set(X)

multishot_action ∈ {submit, observe, decode, cancel, release}
support(Σ_mp) ⊆ {protocol_frontier, stream_frontier, route_frontier,
                 subscription_frontier, suspension_frontier, runner_movement}
support(Σ_mp) ⊆ CallerFrontier
support(Π_mp) ⊆ CallerPolicy
frontier_owner(Σ_mp) = C
policy_owner(Π_mp) = C
frontier_carrier_mp(ctx) =
  operation_carrier_named_by(ctx,Θ_mp,Θ_mp',Σ_mp,Μ_mp,Obs_mp)
frontier_subject_mp(r) ⇔ ∃ ctx. r = frontier_carrier_mp(ctx)
∀ ctx. OperationCarrier(frontier_carrier_mp(ctx))
ZeroCopyReleaseCarrierKind_mp = {ExtSQE}
ZeroCopySendOperation_mp = {op | zero_copy_send_opcode(op)}
zero_copy_send_ctx_mp(ctx) ⇔ op_of(ctx) ∈ ZeroCopySendOperation_mp
zero_copy_release_carrier_mp(ctx,r) ⇔
  names(ctx,r)
  ∧ OperationCarrier(r)
  ∧ resource_kind(r) ∈ ZeroCopyReleaseCarrierKind_mp
ResultResource_mp(out) ⇔
  out ∈ Resource
  ∧ resource_kind(out) ∈ {FD,DirectFD,Buf,FixedBuf,ProvidedBuf}
FDResultResource_mp(out) ⇔
  ResultResource_mp(out) ∧ resource_kind(out) ∈ {FD,DirectFD}
fd_of : {out | FDResultResource_mp(out)} ⇀ DescriptorResult
result_resource_mp(cqe,ctx,out) ⇔
  ResultResource_mp(out)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource_mp(out)
      ∧ defined(fd_of(out))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(out)))
     ∨ selected_buffer_result(cqe,out)
     ∨ explicit_result_resource(cqe,out))
```

## Frontier Semantics

```text
operation_cqe(cqe) ⇔ flag(NOTIF) ∉ flag_projection(cqe.Flags)
notification_cqe(cqe) ⇔ flag(NOTIF) ∈ flag_projection(cqe.Flags)
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
early_notification_cqe_mp(cqe,ctx,H) ⇔
  notification_cqe(cqe)
  ∧ cqe.user_data = raw(ctx)
  ∧ ¬data_frontier_seen(ctx,H)
terminal_fallback_carrier_mp(ctx,r) ⇔
  zero_copy_send_ctx_mp(ctx)
  ∧ zero_copy_release_carrier_mp(ctx,r)
EarlyNotification_mp(cqe,ctx) ⇔
  early_notification_cqe_mp(cqe,ctx,history(ctx))
  ∧ zero_copy_send_ctx_mp(ctx)
  ∧ zero_copy_release_carrier_mp(ctx,frontier_carrier_mp(ctx))
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
  ∧ terminal_fallback_carrier_mp(ctx,r)
  ∧ ¬data_frontier_seen(ctx,H)
  ∧ ¬early_notification_seen(ctx,H)
release_ready_carrier_at_mp(cqe,ctx,r,H) ⇔
  cqe.user_data = raw(ctx)
  ∧ zero_copy_send_ctx_mp(ctx)
  ∧ zero_copy_release_carrier_mp(ctx,r)
  ∧ (notification_release_ready_cqe(cqe,ctx,H)
     ∨ terminal_no_notification_cqe(cqe,ctx,r,H))
release_ready_cqe(cqe,ctx,H) ⇔
  ∃ r. release_ready_carrier_at_mp(cqe,ctx,r,H)

operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags) →
  Obs_mp ∈ OutcomeProduct
  ∧ ctrl(Obs_mp) = ok
  ∧ has(flag(MORE),Obs_mp)
  ∧ frontier_fact(Obs_mp, nonterminal_frontier(ctx,frontier_carrier_mp(ctx)))
  ∧ successor_observation_possible(ctx)

operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags) →
  Obs_mp ∈ OutcomeProduct
  ∧ ctrl(Obs_mp) = ok
  ∧ frontier_fact(Obs_mp, terminal_frontier(ctx,frontier_carrier_mp(ctx)))

operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags) →
  Obs_mp ∈ OutcomeProduct
  ∧ ctrl(Obs_mp) = fail(typed_error(-cqe.Res))
  ∧ frontier_fact(Obs_mp,
                   nonterminal_frontier_after_failure(ctx,frontier_carrier_mp(ctx)))
  ∧ has(flag(MORE),Obs_mp)

operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags) →
  Obs_mp ∈ OutcomeProduct
  ∧ ctrl(Obs_mp) = fail(typed_error(-cqe.Res))
  ∧ frontier_fact(Obs_mp,
                   terminal_frontier_after_failure(ctx,frontier_carrier_mp(ctx)))

release_ready_carrier_at_mp(cqe,ctx,frontier_carrier_mp(ctx),history(ctx)) →
  Rel_mp ∈ ReleaseObservation
  ∧ frontier_fact(Rel_mp, release_frontier(ctx,frontier_carrier_mp(ctx)))
  ∧ ¬defined(ctrl(Rel_mp))
release_ready_carrier_at_mp(cqe,ctx,frontier_carrier_mp(ctx),history(ctx))
∧ notification_release_ready_cqe(cqe,ctx,history(ctx)) →
  has(flag(NOTIF),Rel_mp)
release_ready_carrier_at_mp(cqe,ctx,frontier_carrier_mp(ctx),history(ctx))
∧ terminal_no_notification_cqe(cqe,ctx,frontier_carrier_mp(ctx),history(ctx)) →
  ¬has(flag(NOTIF),Rel_mp)

notification_cqe(cqe)
∧ ¬release_ready_carrier_at_mp(cqe,ctx,frontier_carrier_mp(ctx),history(ctx)) →
  Rel_mp = none
EarlyNotification_mp(cqe,ctx) →
  Obs_mp = none
  ∧ Rel_mp = none
  ∧ ¬defined(ctrl(Obs_mp))

result_resource_mp(cqe,ctx,out) →
  caller_owns(out) ∧ ¬frontier_subject_mp(out)
```

```text
terminal_completion(ctx,r) ⇔
  frontier_fact(Obs_mp, terminal_frontier(ctx,r))
  ∨ frontier_fact(Obs_mp, terminal_frontier_after_failure(ctx,r))

(Obs_mp ∈ OutcomeProduct ∧ has(flag(MORE),Obs_mp)) →
  frontier_fact(Obs_mp, nonterminal_frontier(ctx,frontier_carrier_mp(ctx)))
(Obs_mp ∈ OutcomeProduct ∧ has(flag(MORE),Obs_mp)) ↛ ByteProgress
(Obs_mp ∈ OutcomeProduct ∧ has(flag(MORE),Obs_mp)) ↛
  terminal_completion(ctx,frontier_carrier_mp(ctx))
(operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)) ↛
  (Obs_mp ∈ OutcomeProduct ∧ ctrl(Obs_mp) = errMore)
```

## Caller-Layer Placement

```text
pkg_owner(stream_contract)            = pkg("code.hybscloud.com/iox")
pkg_owner(stream_frontier_vocabulary) = pkg("code.hybscloud.com/iox")
pkg_owner(datagram_contract)          = pkg("code.hybscloud.com/iox")
pkg_owner(outcome_control_vocabulary) = pkg("code.hybscloud.com/iox")
pkg_owner(byte_progress_contract)     = pkg("code.hybscloud.com/iox")
pkg_owner(suspension_frontier_vocabulary) = pkg("code.hybscloud.com/kont")
pkg_owner(protocol_frontier_vocabulary) = pkg("code.hybscloud.com/sess")
pkg_owner(one_shot_resumption)        = pkg("code.hybscloud.com/kont")
pkg_owner(route_frontier_vocabulary) = pkg("code.hybscloud.com/takt")
pkg_owner(subscription_frontier_vocabulary) = pkg("code.hybscloud.com/takt")
pkg_owner(runner_movement_vocabulary) = pkg("code.hybscloud.com/takt")
CompletionMemoryName_mp = finite set of atoms
CompletionMemoryFact_mp = {completion_memory_fact(memory) | memory ∈ CompletionMemoryName_mp}
CompletionMemoryPolicy_mp = {completion_memory_policy(memory) | memory ∈ CompletionMemoryName_mp}
∀ memory ∈ CompletionMemoryName_mp.
  pkg_owner(completion_memory_fact(memory)) = pkg("code.hybscloud.com/takt")
∀ memory ∈ CompletionMemoryName_mp.
  policy_owner(completion_memory_policy(memory)) = C
pkg_owner(context_evidence)           = pkg("code.hybscloud.com/cove")
pkg_owner(requirement_evidence)       = pkg("code.hybscloud.com/cove")
pkg_owner(safety_evidence)            = pkg("code.hybscloud.com/cove")
frontier_vocabulary(suspension_frontier) = suspension_frontier_vocabulary
frontier_vocabulary(stream_frontier) = stream_frontier_vocabulary
frontier_owner(stream_frontier) = C
frontier_vocabulary(protocol_frontier) = protocol_frontier_vocabulary
frontier_vocabulary(route_frontier) = route_frontier_vocabulary
frontier_vocabulary(subscription_frontier) = subscription_frontier_vocabulary
frontier_vocabulary(runner_movement) = runner_movement_vocabulary
policy_owner(route_retirement)              = C
policy_owner(completion_pool_policy)        = C
policy_owner(completion_gc_root_policy)     = C

place(Σ_mp, C)
place(Π_mp, C)
reject(place(parser_state, B))
reject(place(parser_branch_policy, B))
reject(place(route_lookup, B))
reject(place(completion_routing, B))
reject(place(completion_pool_policy, B))
reject(place(completion_gc_root_policy, B))
∀ memory ∈ CompletionMemoryName_mp.
  reject(place(completion_memory_policy(memory), B))
reject(place(scheduler, B))

route_retirement_caller_boundary ⇔
  ∀ route_state.
    retire(route_state) →
      caller_state_owner(route_state) = C
      ∧ policy_owner(route_retirement) = C
      ∧ ¬hidden(route_retirement, support(B))

completion_memory_caller_boundary ⇔
  ∀ memory.
    uses_completion_memory(J_multishot_protocols,memory) →
      completion_memory_fact(memory) ∈ CompletionMemoryFact_mp
      ∧ completion_memory_policy(memory) ∈ CompletionMemoryPolicy_mp
      ∧ policy_owner(completion_memory_policy(memory)) = C
      ∧ policy_owner(completion_pool_policy) = C
      ∧ policy_owner(completion_gc_root_policy) = C
      ∧ ¬hidden({completion_memory_fact(memory),completion_memory_policy(memory),
                 completion_pool_policy,completion_gc_root_policy},
                support(B))
```

## Proof Obligations

```text
PO_multishot_protocols =
  (may_arrive(cqe,ctx) → stable_identity(ctx))
  ∧ nonterminal_frontier_visible_after_each_MORE
  ∧ terminal_completion_visible_after_final_CQE
  ∧ protocol_state_not_hidden_in_boundary_adapter
  ∧ continuation_resumption_affine_when_used
  ∧ route_retirement_caller_boundary
  ∧ completion_memory_caller_boundary
```
## Runtime Carriers Lift

This second lift records the runtime carriers — the checked shape that caller-side Go must preserve when it drives operations through `kont`, `cove`, `takt`, and `sess`.

```text
topic = runtime_carriers
task_module = pkg("code.hybscloud.com/uring")
B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_runtime_carriers) = ⟦J_runtime_carriers⟧_U

complete(lift,J_runtime_carriers) ⇔
  ∃ go_surface.
    go_surface ∈ GoSurface
    ∧ BoundaryClean(go_surface)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ ⟦go_surface⟧⁻¹_U ≃_B J_runtime_carriers

J_runtime_carriers =
  Γ_rc | Θ_rc ⊢ runtime_carrier_surface
    : CallerRuntimeBoundary
    | Δ_rc ▷ Θ_rc'
    ; χ_rc ; Σ_rc ; Μ_rc ; Φ_rc ; Obs_rc ; Rel_rc ; Π_rc

∀ stage ∈ StageSave.
  complete(stage,J_runtime_carriers)
    → saved(SaveLift(stage,task,J_runtime_carriers))
complete(lift,J_runtime_carriers)
  → saved(SaveLift(lift,task,J_runtime_carriers))

RuntimeOwner =
  { pkg("code.hybscloud.com/kont")
  , pkg("code.hybscloud.com/cove")
  , pkg("code.hybscloud.com/takt")
  , pkg("code.hybscloud.com/sess")
  }
support(RuntimeOwner) ⊆ C
RuntimeOwner ∩ support(B) = ∅

RuntimeTheoryRoute_rc =
  { pkg("code.hybscloud.com/kont")
      ↦ {algebraic_effect, handler, shallow_handler, resumption}
  , pkg("code.hybscloud.com/cove")
      ↦ {coeffect_requirement, modal_world, modal_later}
  , pkg("code.hybscloud.com/takt")
      ↦ {coalgebraic_observation, temporal_obligation}
  , pkg("code.hybscloud.com/sess")
      ↦ {session_frontier}
}
∀ p ∈ RuntimeOwner. RuntimeTheoryRoute_rc[p] ⊆ TheoryConcept
RuntimeTheoryUse_rc =
  {runtime_concept_use(p,c) |
     p ∈ RuntimeOwner ∧ c ∈ RuntimeTheoryRoute_rc[p]}
RuntimeTheoryUse_rc ⊆ RuntimeConceptUse
support(RuntimeTheoryUse_rc) ⊆ C
project(RuntimeTheoryUse_rc,B) = ∅
∀ c ∈ ⋃ {RuntimeTheoryRoute_rc[p] | p ∈ RuntimeOwner}.
  concept_utilized_U(J_runtime_carriers,c)

KontCarrier =
  {Eff[A], Expr[A], Cont[R,A], Suspension[A], Reify, Reflect,
   resume, try_resume, discard}
CoveCarrier =
  {Context, Requirement, View, Req, ReqExpr, SuspensionView,
   World, StepIndex, Relation, Later}
TaktCarrier =
  {Dispatcher, Backend, Loop, SubscriptionBackend, SubscriptionLoop,
   Completion, StreamCompletion, StreamEvent, Token, RouteID,
   CompletionMemory, HeapMemory, BoundedMemory,
   Reify, Reflect, Exec, ExecExpr, Step, Advance, AdvanceSuspension,
   ExecError, ExecErrorExpr, StepError, AdvanceError}
PendingTable_rc = finite_map(Token,Suspension[A])
RouteState_rc = {active,canceling}
RouteTable_rc = finite_map(RouteID,RouteState_rc)
token_rc(c) defined → token_rc(c) ∈ Token
route_rc(c) defined → route_rc(c) ∈ RouteID
state_pre_rc(route) =
  RouteTable_rc_pre[route] if route ∈ dom(RouteTable_rc_pre)
state_post_rc(route) =
  RouteTable_rc_post[route] if route ∈ dom(RouteTable_rc_post)
current_suspension_rc(c) =
  PendingTable_rc[token_rc(c)] if token_rc(c) ∈ dom(PendingTable_rc)
current_suspension_rc(op) =
  ιs. s ∈ OneShotSuspension ∧ pending_op(s)=op
  if ∃!s. s ∈ OneShotSuspension ∧ pending_op(s)=op
more_flag_rc(c) = c.More
more_flag_rc(c) ∈ Bool
More_rc(c) ⇔ more_flag_rc(c) = true
boundary_more_rc(cqe) ⇔ flag(MORE) ∈ flag_projection(cqe.Flags)
stream_projection_required_facts_rc(cqe) = {cqe.Flags, cqe.user_data}
derived_from_cqe_rc(c,cqe) ⇔
  cqe_source_rc(c) = cqe
  ∧ stream_projection_required_facts_rc(cqe) ⊆ copied_facts(c)
  ∧ copied_facts(c) ⊆
    {cqe.Res, cqe.Flags, cqe.user_data, selected_buffer_metadata(cqe)}
boundary_stream_completion_rc(c,cqe) ⇔
  c ∈ StreamCompletion
  ∧ derived_from_cqe_rc(c,cqe)
  ∧ cqe.user_data = route_identity(route_rc(c))
boundary_stream_completion_rc(c,cqe) →
  (More_rc(c) ⇔ boundary_more_rc(cqe))
event_error_rc(c) = c.EventErr
payload_failure_rc(c) ⇔ event_error_rc(c) ≠ nil
ErrMore_rc = err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore")
ErrWouldBlock_rc = err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock")
ErrUnsupportedMultishot_rc =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrUnsupportedMultishot")
ErrLiveTokenReuse_rc =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrLiveTokenReuse")
ErrLiveRouteReuse_rc =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrLiveRouteReuse")
ErrInvalidRouteID_rc =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrInvalidRouteID")
ErrUnknownSubscription_rc =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrUnknownSubscription")
ErrDisposed_rc =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrDisposed")
ErrMore_rc ∈ ErrorSymbol
ErrWouldBlock_rc ∈ ErrorSymbol
TaktFatalSymbol_rc =
  {ErrUnsupportedMultishot_rc, ErrLiveTokenReuse_rc,
   ErrLiveRouteReuse_rc, ErrInvalidRouteID_rc, ErrDisposed_rc}
TaktFatalSymbol_rc ⊆ ErrorSymbol
DispatchErrMore_rc = site(dispatch_boundary,ErrMore_rc)
CompletionErrMore_rc = site(completion_boundary,ErrMore_rc)
StreamMore_rc(c) ⇔ c ∈ StreamCompletion ∧ More_rc(c)
Bool ∩ ErrorSymbol = ∅
DispatchErrMore_rc ≠ CompletionErrMore_rc
live_route_rc(route) ⇔ route ∈ dom(RouteTable_rc)
retire_route_rc(route) ⇔
  route ∈ dom_pre(RouteTable_rc) ∧ route ∉ dom_post(RouteTable_rc)
cancel_route_rc(route) ∧ state_pre_rc(route)=active →
  backend_cancel(route) ∧ state_post_rc(route)=canceling
cancel_route_rc(route) ∧ state_pre_rc(route)=canceling →
  no_duplicate_cancel(route) ∧ state_post_rc(route)=canceling
SessCarrier =
  {Endpoint, Send[T], Recv[T], Close, SelectL, SelectR, Offer,
   Reify, Reflect, Step, Advance, StepError, AdvanceError,
   Run, RunExpr, RunError, RunErrorExpr,
   Exec, ExecExpr, ExecError, ExecErrorExpr, Loop, ExprLoop}

Carrier_rc = KontCarrier ∪ CoveCarrier ∪ TaktCarrier ∪ SessCarrier
support(Carrier_rc) ⊆ C
project(Carrier_rc,B) = ∅
support({PendingTable_rc,RouteTable_rc}) ⊆ C
project({PendingTable_rc,RouteTable_rc},B) = ∅

CoveRequirementGate(ctx,s,guard) ⇔
  owner(ctx) = pkg("code.hybscloud.com/cove")
  ∧ s : type_symbol(pkg("code.hybscloud.com/kont"),"Suspension[A]")
  ∧ guard ∈ (Req[Ctx] ⊎ ReqExpr[Ctx])
  ∧ (∀ req. guard = inl(req)
       → owner(req) = pkg("code.hybscloud.com/cove")
       ∧ CheckSuspension(ctx,s,req) = (SuspensionView(ctx,s), true))
  ∧ (∀ χ. guard = inr(χ)
       → owner(χ) = pkg("code.hybscloud.com/cove")
       ∧ CheckSuspensionExpr(ctx,s,χ) = (SuspensionView(ctx,s), true))
resume(SuspensionView(ctx,s),v) defined ⇔
  s ≠ nil ∧ ¬resumed(s) ∧ ¬discarded(s)
resume(SuspensionView(ctx,s),v) ↛ check(CoveRequirementGate(ctx,s,guard))
guard_required(ctx,s,guard) →
  require(CoveRequirementGate(ctx,s,guard))
CoveRequirementGate(ctx,s,guard) ∈ support(C)
CoveRequirementGate(ctx,s,guard) ∉ support(B)

⟦Reify(Eff[A])⟧ = Expr[A]
⟦Reflect(Expr[A])⟧ = Eff[A]
⟦Reflect(Reify(x))⟧ ≃ x modulo representation_cost
ReifyReflect ∈ support(C)
ReifyReflect ∉ support(B)

OneShotSuspension =
  {s | s : Suspension[A] ∧ affine(s) ∧ resume_count(s) ≤ 1}
BoundarySuspension(s,op) ⇔
  s ∈ OneShotSuspension ∧ op ∈ boundary_action(B) ∧ pending_op(s)=op
∀ s ∈ OneShotSuspension.
  ∀ op. BoundarySuspension(s,op) → identity(s)=user_data(op)
∀ op.
  multishot(op) → reject(carrier(op) = OneShotSuspension)
∀ op.
  one_shot(op) → carrier(op) = Loop(pkg("code.hybscloud.com/takt"))
∀ op.
  multishot(op) → carrier(op) = SubscriptionLoop(pkg("code.hybscloud.com/takt"))

OutcomeCtrl = {ok, wouldBlock, errMore} ∪ {fail(e) | e ∈ FailureEvidence}
byte_progress ∉ OutcomeCtrl
flag(MORE) ∉ OutcomeCtrl
classify_takt_dispatch(err) =
  ok          if err=nil
  errMore     if err=ErrMore_rc
  wouldBlock  if err=ErrWouldBlock_rc
  fail(err)   otherwise
classify_takt_completion_loop(err) =
  unsupported_multishot if err=ErrMore_rc
  wouldBlock            if err=ErrWouldBlock_rc
  ok                    if err=nil
  fail(err)             otherwise
classify_sess_transport(err) =
  ok          if err=nil
  wouldBlock  if err=ErrWouldBlock_rc
  unexpected(ErrMore_rc) if err=ErrMore_rc
  unexpected(err) otherwise

StepError ∈ ErrorCarrier
AdvanceError ∈ ErrorCarrier
ExecError ∈ ErrorCarrier
ExecErrorExpr ∈ ErrorCarrier
RunError ∈ ErrorCarrier
RunErrorExpr ∈ ErrorCarrier
ErrorCarrier ⊆ C
ErrorCarrier ∩ support(B) = ∅
DispatchError(op) ∧ thrown(e) →
  discard(current_suspension_rc(op))
  ∧ return(Left(e))
  ∧ no_boundary_action(B)

Loop(pkg("code.hybscloud.com/takt")) =
  one_shot_runner_carrier
SubscriptionLoop(pkg("code.hybscloud.com/takt")) =
  route_indexed_stream_carrier
completion_err(Loop(pkg("code.hybscloud.com/takt")), c)
  = ErrMore_rc
  → fail(ErrUnsupportedMultishot_rc)
    ∧ discard(current_suspension_rc(c))
    ∧ no_resume(current_suspension_rc(c))
poll_err(Loop(pkg("code.hybscloud.com/takt")))
  = ErrWouldBlock_rc
  → idle(Loop(pkg("code.hybscloud.com/takt")))
    ∧ no_mutation(PendingTable_rc)
stream_completion(SubscriptionLoop(pkg("code.hybscloud.com/takt")), c) ∧ More_rc(c)
  → emit(stream_event(c))
    ∧ live_route_rc(route_rc(c))
    ∧ ¬retire_route_rc(route_rc(c))
stream_completion(SubscriptionLoop(pkg("code.hybscloud.com/takt")), c) ∧ ¬More_rc(c)
  → emit(stream_event(c))
    ∧ retire_route_rc(route_rc(c))
zero_route_rc = zero(RouteID)
subscribe_route_rc(route,op) ∧ route=zero_route_rc
  → fail(ErrInvalidRouteID_rc)
    ∧ fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),
             ErrInvalidRouteID_rc)
    ∧ drain(RouteTable_rc)
subscribe_route_rc(route,op) ∧ live_route_rc(route)
  → fail(ErrLiveRouteReuse_rc)
    ∧ fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),
             ErrLiveRouteReuse_rc)
    ∧ drain(RouteTable_rc)
cancel_route_rc(route) ∧
  (route=zero_route_rc ∨ route ∉ dom_pre(RouteTable_rc))
  → fail(ErrUnknownSubscription_rc)
    ∧ no_mutation(RouteTable_rc)
fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),e) →
  subscribe_route_rc(_,_) = fail(e)
  ∧ poll(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) = fail(e)
  ∧ cancel_route_rc(_) = fail(e)
drain(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) ∧
  fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))=nil
  → fatal_post(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
      ErrDisposed_rc
drain(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) ∧
  fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))≠nil
  → fatal_post(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
      fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))
Loop(pkg("code.hybscloud.com/sess")) =
  recursive_protocol_carrier
ExprLoop(pkg("code.hybscloud.com/sess")) =
  recursive_protocol_carrier
Loop(pkg("code.hybscloud.com/takt")) ≠ Loop(pkg("code.hybscloud.com/sess"))
SubscriptionLoop(pkg("code.hybscloud.com/takt")) ≠ ExprLoop(pkg("code.hybscloud.com/sess"))

BackoffPolicy =
  {backoff_policy(bo) | bo : type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")}
CompletionMemoryPolicy =
  {completion_memory_policy(m) |
     m : type_symbol(pkg("code.hybscloud.com/takt"),"CompletionMemory")}
ProtocolPolicy =
  {branch_choice, terminal_policy, parser_state, parser_branch_policy}
RuntimePolicy =
  BackoffPolicy ∪ CompletionMemoryPolicy ∪ ProtocolPolicy ∪
  {poll_cadence, route_retirement, completion_routing,
   cancel_timing, resubscribe, service_lifecycle}
support(RuntimePolicy) ⊆ C
RuntimePolicy ∩ support(B) = ∅

BoundaryFacts =
  {sqe_fields, cqe_res, cqe_flags, user_data, selected_buffer_id,
   capability_evidence, ownership_epoch, release_frontier}
support(BoundaryFacts) ⊆ B
∀ x ∈ Carrier_rc ∪ RuntimePolicy.
  reject(hidden(x,support(B)))

Preservation_rc =
  support(Carrier_rc ∪ RuntimePolicy) ⊆ C
  ∧ support(BoundaryFacts) ⊆ B
  ∧ project(Carrier_rc ∪ RuntimePolicy,B)=∅
  ∧ project(RuntimeTheoryUse_rc,B)=∅
  ∧ classify_takt_dispatch(ErrMore_rc)
       ≠ classify_takt_completion_loop(ErrMore_rc)
  ∧ classify_takt_dispatch(ErrMore_rc)
       ≠ classify_sess_transport(ErrMore_rc)
  ∧ sort(more_flag_rc(c)) = Bool
  ∧ sort(ErrMore_rc) = ErrorSymbol
  ∧ (boundary_stream_completion_rc(c,cqe)
       → (More_rc(c) ⇔ boundary_more_rc(cqe)))
  ∧ (payload_failure_rc(c) ∧ More_rc(c)
       → live_route_rc(route_rc(c)) ∧ ¬retire_route_rc(route_rc(c)))
  ∧ (subscribe_route_rc(zero_route_rc,op)
       → fail(ErrInvalidRouteID_rc))
  ∧ (cancel_route_rc(route) ∧ route ∉ dom_pre(RouteTable_rc)
       → fail(ErrUnknownSubscription_rc))
  ∧ (drain(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) ∧
       fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))=nil
       → fatal_post(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
          ErrDisposed_rc)
  ∧ Bool ∩ ErrorSymbol = ∅
  ∧ flag(MORE) ≠ ErrMore_rc
  ∧ byte_progress ≠ ErrMore_rc
```
