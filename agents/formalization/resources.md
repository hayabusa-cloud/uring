# Resources

Every ring context, submission entry, file descriptor, buffer, and zero-copy tracker the kernel touches is a *resource* with one owner and a definite lifetime. Using `code.hybscloud.com/uring` correctly means never consuming one twice, never releasing one early, and never letting a borrowed view escape the observation that produced it. This file is the inventory and the discipline.

It enumerates every resource kind the boundary can produce, the result resources handed back to the caller (FDs, buffers), and the evidence, frontier, and lifecycle kinds that track their state — together with `OwnerPkg`, which fixes exactly which package owns each kind (`dom(OwnerPkg) = DenotedKind`). The affine frontiers `Θ`/`Θ'` make linear consumption checkable, and the epoch order turns "no early release" into a property you can verify rather than hope for.

In plain terms, the rules an agent follows here are:

- Every kind has one owner. The resource, evidence, frontier, and lifecycle kinds are enumerated once, and `OwnerPkg` fixes the owning package for each (`dom(OwnerPkg) = DenotedKind`), so a change cites only kinds named here.
- Resources are affine. The frontiers `Θ`/`Θ'` carry each linear token at most once, so a resource is consumed exactly once and never duplicated.
- The epoch order is monotone. A resource advances `owned → pending → … → terminal → released`/`recycled`/`closed`/`stopped`, and no rule moves it backwards or releases it early.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }

BoundaryResourceKind =
  {Ring,Ctx,IndirectSQE,ExtSQE,CQEView,CQECopy,ListenerOp,
   IncrementalReceiver,ZCRXReceiver,ZCTracker,MultishotSubscription}
OperationCarrierKind =
  {Ctx,IndirectSQE,ExtSQE,ListenerOp,IncrementalReceiver,ZCRXReceiver,
   ZCTracker,MultishotSubscription}
ResultResourceKind = {FD,DirectFD,Buf,FixedBuf,ProvidedBuf}
ResourceKind = BoundaryResourceKind ∪ ResultResourceKind
EvidenceKind =
  {SocketAddr,ContextEvidence,RequirementEvidence,SafetyEvidence,SpinEvidence,
   FrameBoundaryEvidence}
FrontierKind =
  {pending,nonterminal,terminal,release,after_cqe,after_failure,
   pending_after_failure,nonterminal_after_failure,terminal_after_failure}
LifecycleKind = {released,recycled,closed,stopped}
Atom = countable set of syntactic atoms
ℕ = {0,1,2,...}
FiniteTuple(X) = ⋃_{n∈ℕ} X^n
TermName = finite subset of Atom
ResourceName = TermName
EvidenceName = TermName
FrontierArgs = FiniteTuple(Atom)
LifecycleArgs = FiniteTuple(Atom)
Resource = {res(k,n) | k ∈ ResourceKind ∧ n ∈ ResourceName}
resource_kind : Resource → ResourceKind
resource_kind(res(k,n)) = k
Evidence = {evd(k,n) | k ∈ EvidenceKind ∧ n ∈ EvidenceName}
evidence_kind : Evidence → EvidenceKind
evidence_kind(evd(k,n)) = k
Frontier = FrontierKind ∪ {frontier(k,args) | k ∈ FrontierKind ∧ args ∈ FrontierArgs}
frontier_kind : Frontier → FrontierKind
∀ k ∈ FrontierKind. frontier_kind(k) = k
∀ k ∈ FrontierKind. ∀ args ∈ FrontierArgs. frontier_kind(frontier(k,args)) = k
LifecycleToken = {lifecycle(k,args) | k ∈ LifecycleKind ∧ args ∈ LifecycleArgs}
lifecycle_kind : LifecycleToken → LifecycleKind
∀ k ∈ LifecycleKind. ∀ args ∈ LifecycleArgs. lifecycle_kind(lifecycle(k,args)) = k
released_token(args) = lifecycle(released,args)
recycled_token(args) = lifecycle(recycled,args)
closed_token(args) = lifecycle(closed,args)
stopped_token(args) = lifecycle(stopped,args)
DenotedEntity = Resource ∪ Evidence ∪ Frontier ∪ LifecycleToken
⟦·⟧^{U,res}_w : DenotedEntity ⇀ Denotation_U(w)
denote_resource_surface(x,w) ⇔
  w ∈ World_U
  ∧ x ∈ DenotedEntity
  ∧ defined(⟦x⟧^{U,res}_w)
DenotedKind = ResourceKind ∪ EvidenceKind ∪ FrontierKind ∪ LifecycleKind
OperationCarrierKind ⊆ BoundaryResourceKind
pairwise_disjoint(BoundaryResourceKind,ResultResourceKind)
EvidenceKind ∩ ResourceKind = ∅
FrontierKind ∩ ResourceKind = ∅
FrontierKind ∩ EvidenceKind = ∅
LifecycleKind ∩ ResourceKind = ∅
LifecycleKind ∩ EvidenceKind = ∅
LifecycleKind ∩ FrontierKind = ∅

OwnerPkg =
  { Ring ↦ pkg("code.hybscloud.com/uring")
  , Ctx ↦ pkg("code.hybscloud.com/uring")
  , IndirectSQE ↦ pkg("code.hybscloud.com/uring")
  , ExtSQE ↦ pkg("code.hybscloud.com/uring")
  , CQEView ↦ pkg("code.hybscloud.com/uring")
  , CQECopy ↦ pkg("code.hybscloud.com/uring")
  , ListenerOp ↦ pkg("code.hybscloud.com/uring")
  , IncrementalReceiver ↦ pkg("code.hybscloud.com/uring")
  , ZCRXReceiver ↦ pkg("code.hybscloud.com/uring")
  , ZCTracker ↦ pkg("code.hybscloud.com/uring")
  , MultishotSubscription ↦ pkg("code.hybscloud.com/uring")
  , FD ↦ pkg("code.hybscloud.com/iofd")
  , DirectFD ↦ pkg("code.hybscloud.com/iofd")
  , Buf ↦ pkg("code.hybscloud.com/iobuf")
  , FixedBuf ↦ pkg("code.hybscloud.com/iobuf")
  , ProvidedBuf ↦ pkg("code.hybscloud.com/iobuf")
  , SocketAddr ↦ pkg("code.hybscloud.com/sock")
  , ContextEvidence ↦ pkg("code.hybscloud.com/cove")
  , RequirementEvidence ↦ pkg("code.hybscloud.com/cove")
  , SafetyEvidence ↦ pkg("code.hybscloud.com/cove")
  , SpinEvidence ↦ pkg("code.hybscloud.com/spin")
  , FrameBoundaryEvidence ↦ pkg("code.hybscloud.com/framer")
  , pending ↦ pkg("code.hybscloud.com/uring")
  , nonterminal ↦ pkg("code.hybscloud.com/uring")
  , terminal ↦ pkg("code.hybscloud.com/uring")
  , release ↦ pkg("code.hybscloud.com/uring")
  , after_cqe ↦ pkg("code.hybscloud.com/uring")
  , after_failure ↦ pkg("code.hybscloud.com/uring")
  , pending_after_failure ↦ pkg("code.hybscloud.com/uring")
  , nonterminal_after_failure ↦ pkg("code.hybscloud.com/uring")
  , terminal_after_failure ↦ pkg("code.hybscloud.com/uring")
  , released ↦ pkg("code.hybscloud.com/uring")
  , recycled ↦ pkg("code.hybscloud.com/uring")
  , closed ↦ pkg("code.hybscloud.com/uring")
  , stopped ↦ pkg("code.hybscloud.com/uring")
  }
dom(OwnerPkg) = DenotedKind

Epoch =
  {owned,pending,early_notification,after_cqe,nonterminal,terminal,
   released,recycled,closed,stopped}
Time = boundary_observation_events
next ⊆ Time × Time
≤ = reflexive_transitive_closure(next)
epoch : Resource × Time → Epoch

AffineResource(r) ⇔ multiplicity(r,Θ) ≤ 1
OperationCarrier(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ OperationCarrierKind
ResultResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ ResultResourceKind
FDResultResource(r) ⇔ ResultResource(r) ∧ resource_kind(r) ∈ {FD,DirectFD}
fd_of : {r | FDResultResource(r)} ⇀ DescriptorResult
frontier_subject(r) ⇔ OperationCarrier(r)
KernelOwns(r,t) ⇔
  frontier_subject(r)
  ∧ epoch(r,t) ∈ {pending,early_notification,nonterminal}
CallerOwns(r,t) ⇔
  epoch(r,t) = owned
  ∨ epoch(r,t) = released
  ∨ caller_retains(r,t)
  ∨ EmittedResultOwns(r,t)
ObservationHolds(r,t) ⇔ epoch(r,t) = after_cqe
after_cqe(r,t) ⇔ epoch(r,t) = after_cqe
early_notification(r,t) ⇔ epoch(r,t) = early_notification
terminal(r,t) ⇔ epoch(r,t) = terminal
released(r,t) ⇔ epoch(r,t) = released
stopped(r,t) ⇔ epoch(r,t) = stopped
CQEReferenceLive(r,t) ⇔
  epoch(r,t) ∈ {pending,early_notification,after_cqe,nonterminal,terminal}
operation_cqe(cqe) ⇔ flag(NOTIF) ∉ flag_projection(cqe.Flags)
notification_cqe(cqe) ⇔ flag(NOTIF) ∈ flag_projection(cqe.Flags)
observed_for(r,cqe,t) ⇔
  ObservationHolds(r,t)
  ∧ emitted(cqe,t)
  ∧ ∃ ctx. cqe.user_data = raw(ctx) ∧ names(ctx,r)
operation_observed_for(r,cqe,t) ⇔ observed_for(r,cqe,t) ∧ operation_cqe(cqe)
notification_observed_for(r,cqe,t) ⇔
  CQEReferenceLive(r,t)
  ∧ emitted(cqe,t)
  ∧ notification_cqe(cqe)
  ∧ ∃ ctx. cqe.user_data = raw(ctx) ∧ names(ctx,r)
notification_observed(r,t) ⇔
  ∃ cqe. notification_observed_for(r,cqe,t)
notification_terminal(r,t) ⇔
  terminal(r,t)
  ∧ ∃ t_obs. t_obs ≤ t ∧ notification_observed(r,t_obs)
ReleaseWitness =
  { release_frontier(ctx,r) | r ∈ Resource ∧ names(ctx,r) }
  ∪ { explicit_terminal_no_notification_fallback(ctx,r) | r ∈ Resource ∧ names(ctx,r) }
  ∪ { named_transfer(r,t) | r ∈ Resource ∧ t ∈ Time }
observed_release_witness : ReleaseWitness × Time → Prop
zero_copy_release_frontier(ctx,r) ⇔
  resource_kind(r) = ExtSQE
  ∧ names(ctx,r)
  ∧ ∃ t.
      (observed_release_witness(release_frontier(ctx,r),t)
       ∨ observed_release_witness(explicit_terminal_no_notification_fallback(ctx,r),t))
release_allowed(r,t) ⇔
  ∃ w ∈ ReleaseWitness.
    names(w,r)
    ∧ observed_release_witness(w,t)
notification_release_transition(r,t,t') ⇔
  notification_terminal(r,t)
  ∧ release_allowed(r,t)
  ∧ next(t,t')
  ∧ released(r,t')
terminal_fallback_release_transition(r,t,t') ⇔
  terminal(r,t)
  ∧ ∃ ctx. observed_release_witness(explicit_terminal_no_notification_fallback(ctx,r),t)
  ∧ release_allowed(r,t)
  ∧ next(t,t')
  ∧ released(r,t')
release_transition(r,t,t') ⇔
  notification_release_transition(r,t,t')
  ∨ terminal_fallback_release_transition(r,t,t')
NotificationReleaseOnce(r) ⇔
  ∀ t1,t1',t2,t2'.
    (notification_release_transition(r,t1,t1')
     ∧ notification_release_transition(r,t2,t2'))
      → (t1 = t2 ∧ t1' = t2')
ReleaseOnce(r) ⇔
  ∀ t1,t1',t2,t2'.
    (release_transition(r,t1,t1')
     ∧ release_transition(r,t2,t2'))
      → (t1 = t2 ∧ t1' = t2')
CQEResultResource(cqe,ctx,r) ⇔
  ResultResource(r)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource(r)
      ∧ defined(fd_of(r))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(r)))
     ∨ selected_buffer_result(cqe,r)
     ∨ explicit_result_resource(cqe,r))
EmittedResultOwns(r,t) ⇔
  ∃ cqe,ctx. emitted(cqe,t) ∧ CQEResultResource(cqe,ctx,r)
frontier_from : CQE × Ctx × Resource ⇀ Frontier
defined(frontier_from(cqe,ctx,r)) →
  frontier_subject(r) ∧ operation_cqe(cqe)
CQEResultResource(cqe,ctx,r) → ¬frontier_subject(r)

ReleaseOrRetain(r,t) ⇔ released(r,t) ∨ caller_retains(r,t)
∀ r,t. KernelOwns(r,t) → ¬caller_mutates(r,t)
∀ r,t. KernelOwns(r,t) → ¬recycle(r,t)
∀ r,t. early_notification(r,t) → ¬caller_mutates(r,t)
∀ r,t. early_notification(r,t) → ¬recycle(r,t)
∀ r,t. early_notification(r,t) → ¬close_owner(r,t)
∀ r,t. ObservationHolds(r,t) → ¬caller_mutates(r,t)
∀ r,t. ObservationHolds(r,t) → ¬recycle(r,t)
∀ r,t. ObservationHolds(r,t) → ¬close_owner(r,t)
∀ r,t. terminal(r,t) ↛ CallerOwns(r,t)
∀ r,t. terminal(r,t) → ∃ t'. (t ≤ t' ∧ ReleaseOrRetain(r,t'))
∀ r,t. EmittedResultOwns(r,t) → (caller_may_use(r,t) ∧ ¬frontier_subject(r))
∀ r,t. stopped(r,t) → ¬kernel_may_own(r,t)

MultishotSubscriptionResource(r) ⇔
  r ∈ Resource ∧ resource_kind(r) = MultishotSubscription
ZCTrackerResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) = ZCTracker
IndirectSQEResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) = IndirectSQE
ListenerOpResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) = ListenerOp
IncrementalReceiverResource(r) ⇔
  r ∈ Resource ∧ resource_kind(r) = IncrementalReceiver
ZCRXReceiverResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) = ZCRXReceiver
BoundaryResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ BoundaryResourceKind
CarrierEvidence(e) ⇔ e ∈ Evidence ∧ evidence_kind(e) ∈ EvidenceKind

BoundaryResource(r) →
  resource_owner(r) = B
MultishotSubscriptionResource(r) →
  resource_owner(r) = B
  ∧ OperationCarrier(r)
ZCTrackerResource(r) →
  r ∈ Θ
  ∧ lifecycle_obligation(r) ∈ Φ
  ∧ ∀ ctx,ext.
      (names(ctx,r) ∧ names(ctx,ext) ∧ resource_kind(ext) = ExtSQE
       ∧ zero_copy_release_frontier(ctx,ext))
      → ReleaseOnce(ext)
IndirectSQEResource(r) →
  r ∈ Θ
  ∧ OperationCarrier(r)
ListenerOpResource(r) →
  r ∈ Θ
  ∧ lifecycle_obligation(r) ∈ Φ
  ∧ OperationCarrier(r)
IncrementalReceiverResource(r) →
  r ∈ Θ
  ∧ lifecycle_obligation(r) ∈ Φ
  ∧ OperationCarrier(r)
ZCRXReceiverResource(r) →
  r ∈ Θ
  ∧ lifecycle_obligation(r) ∈ Φ
  ∧ OperationCarrier(r)

resource_projection_allowed(r,B) ⇔
  r ∈ Resource
  ∧ resource_kind(r) ∈ ResourceKind
  ∧ read_complete(OwnerPkg(resource_kind(r)))
  ∧ explicit_projection(OwnerPkg(resource_kind(r)),B)
evidence_projection_allowed(e,B) ⇔
  CarrierEvidence(e)
  ∧ evidence_kind(e) ∈ EvidenceKind
  ∧ read_complete(OwnerPkg(evidence_kind(e)))
  ∧ explicit_projection(OwnerPkg(evidence_kind(e)),B)

reject(hidden_resource_table(B))
reject(hidden_completion_gc_root(B))
```
