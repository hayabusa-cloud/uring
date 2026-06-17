# Resources and Lifecycle Lift

This file is the saved lift for resources, their lifecycle, and buffers: the checked judgment shape that caller-side Go must preserve when it owns, releases, recycles, or zero-copy-sends a resource. Use it as the reference for ownership epochs and buffer carriers on the Go surface, so a change can be checked against the affine and release discipline it must round-trip to. It records two lifts — resources-and-lifecycle and buffers.

```text
topic = resources_lifecycle
task_module = pkg("code.hybscloud.com/uring")
B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_resources_lifecycle) = ⟦J_resources_lifecycle⟧_U

complete(lift,J_resources_lifecycle) ⇔
  ∃ go_surface.
    go_surface ∈ GoSurface
    ∧ BoundaryClean(go_surface)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ ⟦go_surface⟧⁻¹_U ≃_B J_resources_lifecycle

∀ stage ∈ StageSave.
  complete(stage,J_resources_lifecycle)
    → saved(SaveLift(stage,task,J_resources_lifecycle))
complete(lift,J_resources_lifecycle)
  → saved(SaveLift(lift,task,J_resources_lifecycle))
```

## Judgment

```text
J_resources_lifecycle =
  Γ_rl | Θ_rl ⊢ lifecycle_action @ ring : result | Δ_rl ▷ Θ_rl'
    ; χ_rl ; Σ_rl ; Μ_rl ; Φ_rl ; Obs_rl ; Rel_rl ; Π_rl
Obs_rl ∈ OutcomePlane
Rel_rl ∈ ReleasePlane

lifecycle_action ∈
  {register, unregister, submit, observe, cancel, resize, recycle,
   release, close, stop}

support(Σ_rl) ⊆ CallerFrontier
support(Π_rl) ⊆ CallerPolicy
frontier_owner(Σ_rl) = C
policy_owner(Π_rl) = C

Epoch =
  { before_submit, pending, after_cqe, early_notification, nonterminal, terminal, released
  , recycled, closed, stopped
  }
BoundaryResourceKind =
  {Ring,Ctx,IndirectSQE,ExtSQE,CQEView,CQECopy,ListenerOp,
   IncrementalReceiver,ZCRXReceiver,ZCTracker,MultishotSubscription}
OperationCarrierKind =
  {Ctx,IndirectSQE,ExtSQE,ListenerOp,IncrementalReceiver,ZCRXReceiver,
   ZCTracker,MultishotSubscription}
ResultResourceKind = {FD,DirectFD,Buf,FixedBuf,ProvidedBuf}
ResourceKind = BoundaryResourceKind ∪ ResultResourceKind
Atom = countable set of syntactic atoms
TermName = finite subset of Atom
ResourceName = TermName
Resource = {res(k,n) | k ∈ ResourceKind ∧ n ∈ ResourceName}
resource_kind : Resource → ResourceKind
resource_kind(res(k,n)) = k
OperationCarrierKind ⊆ BoundaryResourceKind
pairwise_disjoint(BoundaryResourceKind,ResultResourceKind)
BoundaryResource(resource) ⇔
  resource ∈ Resource
  ∧ resource_kind(resource) ∈ BoundaryResourceKind
OperationCarrier(resource) ⇔
  resource ∈ Resource
  ∧ resource_kind(resource) ∈ OperationCarrierKind
ResultResource(resource) ⇔
  resource ∈ Resource
  ∧ resource_kind(resource) ∈ ResultResourceKind
ZeroCopyReleaseCarrierKind_rl = {ExtSQE}
ZeroCopySendOperation_rl = {op | zero_copy_send_opcode(op)}
zero_copy_send_ctx_rl(ctx) ⇔ op_of(ctx) ∈ ZeroCopySendOperation_rl
zero_copy_release_carrier_rl(ctx,resource) ⇔
  names(ctx,resource)
  ∧ OperationCarrier(resource)
  ∧ resource_kind(resource) ∈ ZeroCopyReleaseCarrierKind_rl
FDResultResource(resource) ⇔
  ResultResource(resource) ∧ resource_kind(resource) ∈ {FD,DirectFD}
fd_of : {resource | FDResultResource(resource)} ⇀ DescriptorResult
TeardownReady = {released, recycled}
ObservationHold = {after_cqe}
CQEReferenceLiveEpoch = {pending, after_cqe, early_notification, nonterminal, terminal}

Event = boundary_observation_events
next ⊆ Event × Event
≤ = reflexive_transitive_closure(next)
at : Fact × Event → Prop
current_epoch : Resource × Epoch × Event → Prop
current_cqe : Resource × Event ⇀ CQE
current_ctx : Resource × Event ⇀ Ctx
notification_release_obligation : Resource ⇀ Obligation
notification_release_required(resource) ⇔ resource ∈ dom(notification_release_obligation)
operation_cqe(cqe) ⇔ flag(NOTIF) ∉ flag_projection(cqe.Flags)
notification_cqe(cqe) ⇔ flag(NOTIF) ∈ flag_projection(cqe.Flags)
cqe_reference_live(resource,t) ⇔
  ∃ e ∈ CQEReferenceLiveEpoch. current_epoch(resource,e,t)
observed_cqe_for_resource(resource,ctx,cqe,t) ⇔
  cqe.user_data = raw(ctx)
  ∧ names(ctx,resource)
  ∧ ((cqe_reference_live(resource,t) ∧ emitted(cqe,t))
     ∨ current_cqe(resource,t) = cqe)
notification_release_evidence(resource,t) ⇔
  ∃ cqe,ctx.
    observed_cqe_for_resource(resource,ctx,cqe,t)
    ∧ notification_cqe(cqe)
    ∧ at(release_frontier(ctx,resource),t)
release_frontier_observed(resource,t) ⇔
  ∃ ctx.
    current_ctx(resource,t) = ctx
    ∧ at(release_frontier(ctx,resource),t)
result_resource(cqe,ctx,out) ⇔
  ResultResource(out)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource(out)
      ∧ defined(fd_of(out))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(out)))
     ∨ selected_buffer_result(cqe,out)
     ∨ explicit_result_resource(cqe,out))
emitted_result_owns(out,t) ⇔
  ∃ cqe,ctx.
    result_resource(cqe,ctx,out)
    ∧ (emitted(cqe,t) ∨ ∃ resource. current_cqe(resource,t) = cqe)

resource_step(resource,t,t') ⇔ next(t,t') ∧ names(t,resource) ∧ names(t',resource)
linear_on_resource(resource) ⇔
  ∀ t1,t2.
    names(t1,resource) ∧ names(t2,resource)
      → (t1 ≤ t2 ∨ t2 ≤ t1)
```

## Transition System

```text
OperationCarrier(resource)
  ∧ current_epoch(resource, before_submit, t)
  ∧ submit(resource)
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, pending, t')

OperationCarrier(resource)
  ∧ current_epoch(resource, pending, t)
  ∧ CQE_observed_for(cqe,ctx)
  ∧ names(ctx,resource)
  ∧ zero_copy_send_ctx_rl(ctx)
  ∧ zero_copy_release_carrier_rl(ctx,resource)
  ∧ notification_cqe(cqe)
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, early_notification, t')
  ∧ current_cqe(resource,t') = cqe
  ∧ current_ctx(resource,t') = ctx
  ∧ ¬at(release_frontier(ctx,resource),t')

OperationCarrier(resource)
  ∧ current_epoch(resource, pending, t)
  ∧ CQE_observed_for(cqe,ctx)
  ∧ names(ctx,resource)
  ∧ operation_cqe(cqe)
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, after_cqe, t')
  ∧ current_cqe(resource,t') = cqe
  ∧ current_ctx(resource,t') = ctx
  ∧ at(frontier_after_cqe(ctx,resource),t')

current_epoch(resource, after_cqe, t) ∧ current_cqe(resource,t) = cqe
  ∧ current_ctx(resource,t) = ctx
  ∧ operation_cqe(cqe) ∧ flag(MORE) ∈ flag_projection(cqe.Flags) ∧ cqe.Res ≥ 0
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, nonterminal, t')
  ∧ at(nonterminal_frontier(ctx,resource),t')

current_epoch(resource, after_cqe, t) ∧ current_cqe(resource,t) = cqe
  ∧ current_ctx(resource,t) = ctx
  ∧ operation_cqe(cqe) ∧ flag(MORE) ∈ flag_projection(cqe.Flags) ∧ cqe.Res < 0
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, nonterminal, t')
  ∧ at(nonterminal_frontier_after_failure(ctx,resource),t')

current_epoch(resource, after_cqe, t) ∧ current_cqe(resource,t) = cqe
  ∧ current_ctx(resource,t) = ctx
  ∧ operation_cqe(cqe) ∧ flag(MORE) ∉ flag_projection(cqe.Flags) ∧ cqe.Res ≥ 0
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, terminal, t')
  ∧ at(terminal_frontier(ctx,resource),t')

current_epoch(resource, after_cqe, t) ∧ current_cqe(resource,t) = cqe
  ∧ current_ctx(resource,t) = ctx
  ∧ operation_cqe(cqe) ∧ flag(MORE) ∉ flag_projection(cqe.Flags) ∧ cqe.Res < 0
  ∧ resource_step(resource,t,t')
  → current_epoch(resource, terminal, t')
  ∧ at(terminal_frontier_after_failure(ctx,resource),t')

current_epoch(resource, nonterminal, t)
  ∧ observed_cqe_for_resource(resource,ctx,cqe,t)
  ∧ notification_cqe(cqe) ∧ resource_step(resource,t,t')
  → current_epoch(resource, terminal, t')
  ∧ notification_release_evidence(resource,t')
  ∧ current_cqe(resource,t') = cqe
  ∧ current_ctx(resource,t') = ctx
  ∧ at(release_frontier(ctx,resource),t')

EarlyNotificationDataStep(resource,ctx,cqe,t,t') ⇔
  current_epoch(resource, early_notification, t)
  ∧ observed_cqe_for_resource(resource,ctx,cqe,t)
  ∧ zero_copy_send_ctx_rl(ctx)
  ∧ zero_copy_release_carrier_rl(ctx,resource)
  ∧ operation_cqe(cqe)
  ∧ resource_step(resource,t,t')

EarlyNotificationDataStep(resource,ctx,cqe,t,t') →
  (∃ t_notify ≤ t. notification_observed(resource,t_notify))
  ∧ current_cqe(resource,t') = cqe
  ∧ current_ctx(resource,t') = ctx
  ∧ at(release_frontier(ctx,resource),t')

EarlyNotificationDataStep(resource,ctx,cqe,t,t')
  ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
  → current_epoch(resource, nonterminal, t')
  ∧ at(nonterminal_frontier(ctx,resource),t')
  ∧ at(release_frontier(ctx,resource),t')

EarlyNotificationDataStep(resource,ctx,cqe,t,t')
  ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  → current_epoch(resource, terminal, t')
  ∧ at(terminal_frontier(ctx,resource),t')

EarlyNotificationDataStep(resource,ctx,cqe,t,t')
  ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
  → current_epoch(resource, nonterminal, t')
  ∧ at(nonterminal_frontier_after_failure(ctx,resource),t')
  ∧ at(release_frontier(ctx,resource),t')

EarlyNotificationDataStep(resource,ctx,cqe,t,t')
  ∧ cqe.Res < 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  → current_epoch(resource, terminal, t')
  ∧ at(terminal_frontier_after_failure(ctx,resource),t')

current_epoch(resource, after_cqe, t) ∧ current_cqe(resource,t) = cqe
  ∧ current_ctx(resource,t) = ctx
  ∧ result_resource(cqe,ctx,out) ∧ resource_step(resource,t,t')
  → emitted_result_owns(out,t')
  ∧ caller_may_use(out,t')
  ∧ ¬OperationCarrier(out)
  ∧ ¬names(nonterminal_frontier(ctx,resource),out)

release_frontier_satisfied(resource,t) ⇔
  current_epoch(resource, terminal, t)
  ∧ (¬notification_release_required(resource)
     ∨ (notification_terminal(resource,t)
        ∧ release_frontier_observed(resource,t))
     ∨ explicit_terminal_no_notification_fallback(resource,t))

release_allowed(resource,t) ⇔
  release_frontier_satisfied(resource,t)
  ∨ named_transfer(resource,t)

recycle_allowed(resource,t) ⇔
  current_epoch(resource, released, t)
  ∧ ¬∃ e ∈ ObservationHold. current_epoch(resource,e,t)

current_epoch(resource, terminal, t) ∧ release_allowed(resource,t) ∧ resource_step(resource,t,t')
  → current_epoch(resource, released, t')

current_epoch(resource, released, t) ∧ recycle_allowed(resource,t) ∧ resource_step(resource,t,t')
  → current_epoch(resource, recycled, t')

teardown_ready(resource,t) ⇔
  ∃ e ∈ TeardownReady. current_epoch(resource,e,t)
  ∨ (current_epoch(resource, terminal, t) ∧ no_pending_kernel_reference(resource))
observation_holds(resource,t) ⇔
  ∃ e ∈ ObservationHold. current_epoch(resource,e,t)

close_allowed(resource,t) ⇔
  ¬observation_holds(resource,t)
  ∧ (teardown_ready(resource,t)
     ∨ quiesced(resource)
     ∨ explicit_cancel_drain(resource))

stop_allowed(resource,t) ⇔
  ¬observation_holds(resource,t)
  ∧ (teardown_ready(resource,t)
     ∨ quiesced(resource)
     ∨ explicit_cancel_drain(resource))

current_epoch(resource, e, t) ∧ e ∈ Epoch
  ∧ closable(resource) ∧ close_allowed(resource,t) ∧ resource_step(resource,t,t')
  → current_epoch(resource, closed, t')

current_epoch(resource, e, t) ∧ e ∈ Epoch
  ∧ stoppable(resource) ∧ stop_allowed(resource,t) ∧ resource_step(resource,t,t')
  → current_epoch(resource, stopped, t')
```

```text
race({cancel, close}, visible_CQE_facts)
CQE_observed_for_at(cqe,ctx,t) ⇔
  CQE_observed_at(cqe,t) ∧ cqe.user_data = raw(ctx)
pending_at(ctx,t) ⇔
  ∃ resource. names(ctx,resource) ∧ current_epoch(resource, pending, t)
NoCompletion(ctx,t) ⇔
  pending_at(ctx,t)
  ∧ ¬∃ cqe,t'. t' ≤ t ∧ CQE_observed_for_at(cqe,ctx,t')
CancelAcceptedAt(ctx,t) ⇔ accepted_cancel(ctx,t)
CloseStartedAt(resource,t) ⇔ close_started(resource,t)
MissingMoreTrace(ctx,t) ⇔ missing_MORE(history(ctx,t))
NoFutureCQE(ctx,t) ⇔
  ¬∃ cqe,t'. t ≤ t' ∧ CQE_observed_for_at(cqe,ctx,t')
CancelAcceptedAt(ctx,t) ↛ NoCompletion(ctx,t)
CloseStartedAt(resource,t) ∧ names(ctx,resource) ↛ NoCompletion(ctx,t)
MissingMoreTrace(ctx,t) ↛ NoFutureCQE(ctx,t)

terminal_completion(ctx,resource,t) ⇔
  current_epoch(resource, terminal, t)
  ∧ current_ctx(resource,t) = ctx
terminal_completion(ctx,resource) ⇔
  ∃ t. terminal_completion(ctx,resource,t)

terminal_completion_sound(ctx,resource) ⇔
  ∀ t.
    terminal_completion(ctx,resource,t)
      → (terminal_CQE(ctx)
         ∨ notification_pair_complete(ctx,resource,t)
         ∨ explicit_drain_proof(ctx,resource)
         ∨ capability_failure(ctx,resource))

notification_observed(resource,t) ⇔
  ∃ cqe,ctx.
    observed_cqe_for_resource(resource,ctx,cqe,t)
    ∧ notification_cqe(cqe)

notification_pair_complete(ctx,resource,t) ⇔
  current_epoch(resource, terminal, t)
  ∧ current_ctx(resource,t) = ctx
  ∧ release_frontier_observed(resource,t)
  ∧ ∃ t_notify ≤ t. notification_observed(resource,t_notify)
  ∧ ∃ cqe.
      current_cqe(resource,t) = cqe
      ∧ operation_cqe(cqe)

notification_terminal(resource,t) ⇔
  current_epoch(resource, terminal, t)
  ∧ ∃ t_obs. t_obs ≤ t ∧ notification_observed(resource,t_obs)

notification_release_transition(resource,t,t') ⇔
  notification_terminal(resource,t)
  ∧ release_allowed(resource,t)
  ∧ resource_step(resource,t,t')
  ∧ current_epoch(resource, released, t')

terminal_fallback_release_transition(resource,t,t') ⇔
  current_epoch(resource, terminal, t)
  ∧ explicit_terminal_no_notification_fallback(resource,t)
  ∧ release_allowed(resource,t)
  ∧ resource_step(resource,t,t')
  ∧ current_epoch(resource, released, t')

release_transition(resource,t,t') ⇔
  notification_release_transition(resource,t,t')
  ∨ terminal_fallback_release_transition(resource,t,t')

notification_release_once(resource) ⇔
  ∀ t1,t1',t2,t2'.
    (notification_release_transition(resource,t1,t1')
     ∧ notification_release_transition(resource,t2,t2'))
      → (t1 = t2 ∧ t1' = t2')

release_once(resource) ⇔
  ∀ t1,t1',t2,t2'.
    (release_transition(resource,t1,t1')
     ∧ release_transition(resource,t2,t2'))
      → (t1 = t2 ∧ t1' = t2')
```

## Ownership Obligations

```text
current_epoch_unique(resource) ⇔
  ∀ t. ∀ e1,e2 ∈ Epoch.
    current_epoch(resource,e1,t) ∧ current_epoch(resource,e2,t) → e1 = e2

ownership_sound(J_resources_lifecycle) ⇔
  affine(Θ_rl,Θ_rl')
  ∧ ∀ resource ∈ each_resource(Θ_rl ∪ Θ_rl'). current_epoch_unique(resource)
  ∧ ∀ resource ∈ each_resource(Θ_rl ∪ Θ_rl').
      (OperationCarrier(resource) →
        linear_on_resource(resource)
        ∧ (∀ t.
            observation_holds(resource,t)
              → (¬recycle_allowed(resource,t)
                 ∧ ¬close_allowed(resource,t)
                 ∧ ¬stop_allowed(resource,t)))
        ∧ (recycle(resource) → ∃ t. current_epoch(resource, released, t))
        ∧ (unregister(resource) → no_pending_kernel_reference(resource))
        ∧ (close(resource) → ∃ t. close_allowed(resource,t))
        ∧ (stop(resource) → ∃ t. stop_allowed(resource,t)))
  ∧ ∀ ctx,resource. OperationCarrier(resource) → terminal_completion_sound(ctx,resource)
  ∧ ∀ resource. OperationCarrier(resource) → release_once(resource)
  ∧ ∀ out,t. emitted_result_owns(out,t) → (ResultResource(out) ∧ caller_may_use(out,t))

frontier_visible(resource) ⇔
  ∃ name,args.
    frontier(name,args) ∈ Φ_rl
    ∧ names(args,resource)
    ∧ name ∈ {pending, nonterminal, terminal, release, after_cqe,
              nonterminal_after_failure, terminal_after_failure}
```

## Proof Obligations

```text
PO_resources_lifecycle =
  ownership_sound(J_resources_lifecycle)
  ∧ ∀ resource ∈ each_resource(Θ_rl ∪ Θ_rl').
      OperationCarrier(resource) → frontier_visible(resource)
  ∧ all_terminal_paths_named
  ∧ all_release_paths_named
  ∧ ¬hidden(completion_pool_policy, support(B))
  ∧ ¬hidden(completion_gc_root_policy, support(B))
  ∧ ¬hidden(lifecycle_policy, support(B))
```
## Buffer Lift

This second lift records the buffer carriers — the checked shape that caller-side Go must preserve when it registers, provides, selects, consumes, recycles, or zero-copy-sends a buffer.

```text
topic = buffers
task_module = pkg("code.hybscloud.com/uring")
B = boundary(task_module)
A = above(task_module)
D = beyond(task_module)
C = caller(task_module) = A ∪ D
task = initial_lift_for_caller_code(task_module)
StageSave = {formalize, typecheck, reduce, prove, lift}
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J_buffers) = ⟦J_buffers⟧_U

complete(lift,J_buffers) ⇔
  ∃ go_surface.
    go_surface ∈ GoSurface
    ∧ BoundaryClean(go_surface)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ ⟦go_surface⟧⁻¹_U ≃_B J_buffers

∀ stage ∈ StageSave.
  complete(stage,J_buffers)
    → saved(SaveLift(stage,task,J_buffers))
complete(lift,J_buffers)
  → saved(SaveLift(lift,task,J_buffers))
```

## Judgment

```text
J_buffers =
  Γ_buf | Θ_buf ⊢ buffer_action @ ring : result | Δ_buf ▷ Θ_buf'
    ; χ_buf ; Σ_buf ; Μ_buf ; Φ_buf ; Obs_buf ; Rel_buf ; Π_buf
Obs_buf ∈ OutcomePlane
Rel_buf ∈ ReleasePlane

buffer_action ∈
  {register, unregister, provide, select, submit, observe, recycle, release}

BufKind =
  { caller_buffer, fixed_buffer, provided_buffer, buffer_ring, bundle
  , zero_copy_region, zcrx_area
  }

kind : Buf → BufKind
BufferLifecycleEvent = {allocate, own, publish, select, consume, recycle, release}
buffer_lifecycle_fact(buf,e) ∈ Fact ⇔ e ∈ BufferLifecycleEvent
BufferLifecycle(buf) =
  {buffer_lifecycle_fact(buf,e) | e ∈ BufferLifecycleEvent}
∀ buf.
  BufferLifecycle(buf) ⊆ dom(pkg_owner)
  ∧ ∀ f ∈ BufferLifecycle(buf). pkg_owner(f) = pkg("code.hybscloud.com/iobuf")
PkgBufferFact(pkg("code.hybscloud.com/iobuf")) =
  { pool_index_owner, aligned_region, iovec_view, array_copy
  , typed_slice_view, reset_no_zero
  }
∀ f ∈ PkgBufferFact(pkg("code.hybscloud.com/iobuf")).
  pkg_owner(f) = pkg("code.hybscloud.com/iobuf")
BufferType(pkg("code.hybscloud.com/iobuf")) =
  { PicoBuffer, NanoBuffer, MicroBuffer, SmallBuffer, MediumBuffer
  , BigBuffer, LargeBuffer, GreatBuffer, HugeBuffer, VastBuffer
  , GiantBuffer, TitanBuffer, RegisterBuffer
  }
sizeof : BufferType(pkg("code.hybscloud.com/iobuf")) → ℕ
∀ T ∈ BufferType(pkg("code.hybscloud.com/iobuf")). sizeof(T) > 0
ArrayFromSliceCopy(s,off,T,out) ⇔
  T ∈ BufferType(pkg("code.hybscloud.com/iobuf"))
  ∧ 0 ≤ off
  ∧ off + sizeof(T) ≤ len(s)
  ∧ copied(out, slice(s,off,off + sizeof(T)))
  ∧ ¬aliases(out,s)
SliceOfArrayView(s,off,n,T,v) ⇔
  T ∈ BufferType(pkg("code.hybscloud.com/iobuf"))
  ∧ n ≥ 1
  ∧ 0 ≤ off
  ∧ off + n * sizeof(T) ≤ len(s)
  ∧ aliases(v, slice(s,off,off + n * sizeof(T)))
IoVecView(vec,bufs) ⇔
  points_to(vec,bufs)
  ∧ ¬copies_payload(vec,bufs)
ResetNoZero(buf) ⇔ reset(buf) ↛ zeroed(buf)
ZCWindow(buf,off,n) ⇔
  len(buf) > 0
  ∧ 0 ≤ off
  ∧ 1 ≤ n
  ∧ off ≤ len(buf)
  ∧ n ≤ len(buf) - off
FixedZCWindow(buf,base,len_fixed,off,n) ⇔
  0 ≤ base
  ∧ 0 ≤ len_fixed
  ∧ base ≤ len(buf)
  ∧ len_fixed ≤ len(buf) - base
  ∧ 0 ≤ off
  ∧ 1 ≤ n
  ∧ off ≤ len_fixed
  ∧ n ≤ len_fixed - off
  ∧ base + off ≤ len(buf)
  ∧ n ≤ len(buf) - (base + off)
ZCWindow(buf,off,n) → valid_send_zc_range(buf,off,n)
FixedZCWindow(buf,base,len_fixed,off,n) →
  valid_send_zc_fixed_range(buf,base + off,n)
support(Σ_buf) ⊆ CallerFrontier
support(Π_buf) ⊆ CallerPolicy
frontier_owner(Σ_buf) = C
policy_owner(Π_buf) = C
```

## Buffer Epochs

```text
BufEpoch =
  { owned_by_caller, registered, provided, selected_by_kernel, pending_kernel
  , data_visible, early_notification_visible, notification_visible
  , release_ready, recycled
  }

Event = boundary_observation_events
next ⊆ Event × Event
≤ = reflexive_transitive_closure(next)
buf_epoch : Buf × BufEpoch × Event → Prop
lifetime(bufs) ⊆ Event
pending_kernel_interval(x,t) ⊆ Event

buffer_step(buf,t,t') ⇔ next(t,t') ∧ names(t,buf) ∧ names(t',buf)
linear_on_buffer(buf) ⇔
  ∀ t1,t2.
    names(t1,buf) ∧ names(t2,buf)
      → (t1 ≤ t2 ∨ t2 ≤ t1)

buf_epoch_unique(buf) ⇔
  ∀ t. ∀ e1,e2 ∈ BufEpoch.
    buf_epoch(buf,e1,t) ∧ buf_epoch(buf,e2,t) → e1 = e2

buf_epoch(buf, owned_by_caller, t) ∧ register(buf) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, registered, t')

(buf_epoch(buf, owned_by_caller, t) ∨ buf_epoch(buf, registered, t))
  ∧ provide(buf,gid,bid) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, provided, t')

buf_epoch(buf, provided, t) ∧ select_by_kernel(buf,gid,bid) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, selected_by_kernel, t')
  ∧ visible(selected_buffer(gid,bid))

(buf_epoch(buf, owned_by_caller, t)
 ∨ buf_epoch(buf, registered, t)
 ∨ buf_epoch(buf, selected_by_kernel, t))
  ∧ submit_with_buf(buf) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, pending_kernel, t')

buf_epoch(buf, pending_kernel, t) ∧ observe_data_CQE(buf,cqe) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, data_visible, t')

buf_epoch(buf, pending_kernel, t) ∧ observe_notification_CQE(buf,cqe) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, early_notification_visible, t')

buf_epoch(buf, early_notification_visible, t) ∧ observe_data_CQE(buf,cqe) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, notification_visible, t')

buf_epoch(buf, data_visible, t) ∧ observe_notification_CQE(buf,cqe) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, notification_visible, t')

data_seen(buf,t) ⇔
  ∃ t_data ≤ t.
    (buf_epoch(buf, data_visible, t_data)
     ∨ buf_epoch(buf, notification_visible, t_data))

notification_seen(buf,t) ⇔
  ∃ t_notify ≤ t.
    (buf_epoch(buf, early_notification_visible, t_notify)
     ∨ buf_epoch(buf, notification_visible, t_notify))

release_source(buf,t) ⇔
  buf_epoch(buf, data_visible, t) ∨ buf_epoch(buf, notification_visible, t)

requires_release_notification(buf) ⇔
  kind(buf) ∈ {zero_copy_region, zcrx_area}

terminal_no_notification_fallback(buf,t) ⇔
  data_seen(buf,t)
  ∧ terminal_no_notification(named_no_notification_fallback)
  ∧ names(named_no_notification_fallback,buf)

release_notification_ok(buf,t) ⇔
  ¬requires_release_notification(buf)
  ∨ notification_seen(buf,t)
  ∨ terminal_no_notification_fallback(buf,t)

release_allowed(buf,t) ⇔
  data_seen(buf,t)
  ∧ release_notification_ok(buf,t)

release_allowed_sound(buf) ⇔
  release(buf) →
    ∃ t. release_source(buf,t) ∧ release_allowed(buf,t)

release_source(buf,t) ∧ release_allowed(buf,t)
  ∧ release(buf) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, release_ready, t')

release_transition(buf,t,t') ⇔
  release_source(buf,t)
  ∧ release_allowed(buf,t)
  ∧ release(buf)
  ∧ buffer_step(buf,t,t')
  ∧ buf_epoch(buf, release_ready, t')

release_once(buf) ⇔
  ∀ t1,t1',t2,t2'.
    (release_transition(buf,t1,t1')
     ∧ release_transition(buf,t2,t2'))
      → (t1 = t2 ∧ t1' = t2')

buf_epoch(buf, release_ready, t) ∧ recycle(buf) ∧ buffer_step(buf,t,t')
  → buf_epoch(buf, recycled, t')
```

## Selection and Result Evidence

```text
has_BUFFER(cqe.Flags)
  ∧ buffer_group(ctx) = gid
  ∧ selected_buffer_id(cqe.Flags) = bid
  → visible(selected_buffer(gid,bid))
  ∧ names(resource_frontier(Θ_buf,Θ_buf'), {gid,bid})

has_BUFFER(cqe.Flags) ⇔ IORING_CQE_F_BUFFER ∈ cqe.Flags
IOSQE_BUFFER_SELECT ∈ SQE.Flags ∧ SQE.bufIndex = gid
  → may_select_from_buffer_group(gid)
¬has_BUFFER(cqe.Flags) ↛ selected_buffer(gid,bid)

operation_cqe(cqe) ⇔ flag(NOTIF) ∉ flag_projection(cqe.Flags)
notification_cqe(cqe) ⇔ flag(NOTIF) ∈ flag_projection(cqe.Flags)

(operation_cqe(cqe) ∧ cqe.Res ≥ 0) →
  visible(decode_result(op,cqe.Res))
notification_cqe(cqe) ↛ visible(decode_result(op,cqe.Res))

(operation_cqe(cqe) ∧ decode_result(op,cqe.Res) = count(cqe.Res) ∧ cqe.Res > 0) →
  byte_progress(cqe.Res)
ByteProgress ⇔ ∃ n. n > 0 ∧ byte_progress(n)

(Obs_buf ∈ OutcomeProduct ∧ ctrl(Obs_buf) = errMore) ↛ ByteProgress

submitted_iovec(vec,bufs,t) ∧ IoVecView(vec,bufs)
  → pending_kernel_interval(vec,t) ⊆ lifetime(bufs)
submitted_typed_slice_view(v,s,t) ∧ SliceOfArrayView(s,off,n,T,v)
  → pending_kernel_interval(v,t) ⊆ lifetime(s)
ArrayFromSliceCopy(s,off,T,out) ∧ submit_with_buf(out)
  → kernel_view(out) ∧ ¬kernel_view(s)
ResetNoZero(buf) ∧ requires_clear(buf)
  → buffer_clear_policy ∈ support(Π_buf)
```

## Safety Obligations

```text
PO_buffers =
  (∀ buf ∈ each_buffer(Θ_buf ∪ Θ_buf'). buf_epoch_unique(buf))
  ∧ (∀ buf ∈ each_buffer(Θ_buf ∪ Θ_buf'). linear_on_buffer(buf))
  ∧ borrowed_buffer_view_does_not_escape
  ∧ selected_buffer_id_preserved
  ∧ registered_index_preserved
  ∧ iovec_view_lifetime_preserved
  ∧ typed_slice_view_lifetime_preserved
  ∧ array_copy_not_misclassified_as_view
  ∧ reset_not_used_as_clear
  ∧ (malformed_buffer_metadata → fail(named_failure(malformed_buffer_metadata)))
  ∧ ∀ buf. release_allowed_sound(buf)
  ∧ ∀ buf. release_once(buf)
  ∧ ∀ buf. (recycle(buf) → ∃ t. buf_epoch(buf, release_ready, t))
  ∧ ∀ f ∈ {buffer_pooling, alignment, recycle_facts, iovec_view,
             array_copy, typed_slice_view, reset_no_zero}.
      pkg_owner(f) = pkg("code.hybscloud.com/iobuf")
  ∧ ∀ buf. ∀ f ∈ BufferLifecycle(buf).
      projection(f, B) preserves {gid,bid,index,epoch}
      ∧ preserves(projection(f, B), pkg_owner(f))
```
