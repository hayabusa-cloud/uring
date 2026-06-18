# Resources

A `code.hybscloud.com/uring` resource is something the kernel can touch on your behalf — a ring context, an operation carrier, a file descriptor, a buffer, a zero-copy window — and the boundary's job is to make its ownership and lifetime explicit so you never use it twice, release it early, or hand the kernel something it may still be reading. This file states those facts in two halves: the ownership epochs every resource moves through, and the buffer and zero-copy rules layered on top.

## Ownership Epochs And Lifecycle

In plain terms, the rules an agent follows here are:

- Know the kinds and who owns them. Boundary resources — rings, contexts, the SQE carriers, CQE views and copies, listeners, receivers, trackers, subscriptions — are owned by the boundary; result resources (descriptors and buffers) pass to the caller; evidence (socket address, context, requirement, safety, spin, frame boundary) is owned by its neighbour package.
- Move every resource through one epoch order. A resource is `owned`, then `pending`, then possibly `early_notification`, `after_cqe`, `nonterminal`, and `terminal`, and finally `released`, `recycled`, `closed`, or `stopped`.
- Never recycle or close an operation carrier that is still in flight. While a carrier is pending, in early-notification, or after a CQE but not yet terminal, it may not be recycled or close-owned.
- Always release or explicitly retain a terminal completion. After the terminal epoch the resource is eventually released or the caller explicitly retains it; once released or stopped, the kernel may no longer own it.
- Treat cancel, close, and stop as caller requests that race visible CQE facts. An accepted cancel does not by itself mean no completion arrives, a close request does not by itself release, and a stop request does not by itself make the carrier terminal; a stop together with drain eventually reaches the stopped epoch.
- Keep lifecycle policy in the caller. Lifecycle policy, route retirement, and completion routing are caller-owned and never live inside the boundary.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

J_life =
  Γ_life | Θ_life ⊢ lifecycle_action @ ring : result | Δ_life ▷ Θ_life'
    ; χ_life ; Σ_life ; Μ_life ; Φ_life ; Obs_life ; Rel_life ; Π_life
Obs_life ∈ OutcomePlane
Rel_life ∈ ReleasePlane
denote_lifecycle_boundary(w) ⇔
  w ∈ World_U
  ∧ J_life ∈ JudgmentSurface
  ∧ defined(⟦J_life⟧^U_w)

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
OperationCarrierKind ⊆ BoundaryResourceKind
pairwise_disjoint(BoundaryResourceKind,ResultResourceKind)
EvidenceKind ∩ ResourceKind = ∅
Atom = countable set of syntactic atoms
TermName = finite subset of Atom
ResourceName = TermName
EvidenceName = TermName
Resource = {res(k,n) | k ∈ ResourceKind ∧ n ∈ ResourceName}
resource_kind : Resource → ResourceKind
resource_kind(res(k,n)) = k
Evidence = {evd(k,n) | k ∈ EvidenceKind ∧ n ∈ EvidenceName}
evidence_kind : Evidence → EvidenceKind
evidence_kind(evd(k,n)) = k
BoundaryResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ BoundaryResourceKind
OperationCarrier(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ OperationCarrierKind
ResultResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ ResultResourceKind
FDResultResource(r) ⇔ ResultResource(r) ∧ resource_kind(r) ∈ {FD,DirectFD}
fd_of : {r | FDResultResource(r)} ⇀ DescriptorResult
BoundaryResource(r) → resource_owner(r) = B
∀ e ∈ Evidence.
  evidence_kind(e) = SocketAddr → evidence_owner(e) = pkg("code.hybscloud.com/sock")
∀ e ∈ Evidence.
  evidence_kind(e) = ContextEvidence → evidence_owner(e) = pkg("code.hybscloud.com/cove")
∀ e ∈ Evidence.
  evidence_kind(e) = RequirementEvidence → evidence_owner(e) = pkg("code.hybscloud.com/cove")
∀ e ∈ Evidence.
  evidence_kind(e) = SafetyEvidence → evidence_owner(e) = pkg("code.hybscloud.com/cove")
∀ e ∈ Evidence.
  evidence_kind(e) = SpinEvidence → evidence_owner(e) = pkg("code.hybscloud.com/spin")
∀ e ∈ Evidence.
  evidence_kind(e) = FrameBoundaryEvidence → evidence_owner(e) = pkg("code.hybscloud.com/framer")
Epoch =
  {owned,pending,early_notification,after_cqe,nonterminal,terminal,
   released,recycled,closed,stopped}
Time = boundary_observation_events
next ⊆ Time × Time
≤ = reflexive_transitive_closure(next)
epoch : Resource × Time → Epoch

pending(r,t) ⇔ OperationCarrier(r) ∧ epoch(r,t) = pending
early_notification(r,t) ⇔ OperationCarrier(r) ∧ epoch(r,t) = early_notification
after_cqe(r,t) ⇔ OperationCarrier(r) ∧ epoch(r,t) = after_cqe
nonterminal(r,t) ⇔ OperationCarrier(r) ∧ epoch(r,t) = nonterminal
terminal(r,t) ⇔ OperationCarrier(r) ∧ epoch(r,t) = terminal
released(r,t) ⇔ epoch(r,t) = released
stopped(r,t) ⇔ epoch(r,t) = stopped
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

∀ r,t. pending(r,t) → ¬recycle(r,t)
∀ r,t. pending(r,t) → ¬close_owner(r,t)
∀ r,t. early_notification(r,t) → ¬recycle(r,t)
∀ r,t. early_notification(r,t) → ¬close_owner(r,t)
∀ r,t. after_cqe(r,t) → ¬recycle(r,t)
∀ r,t. after_cqe(r,t) → ¬close_owner(r,t)
∀ r,t. terminal(r,t) → ∃ t'. (t ≤ t' ∧ (released(r,t') ∨ caller_retains(r,t')))
∀ r,t. released(r,t) → ¬kernel_may_own(r,t)
∀ r,t. stopped(r,t) → ¬kernel_may_own(r,t)
∀ r,t. EmittedResultOwns(r,t) → (caller_may_use(r,t) ∧ ¬nonterminal(r,t))

CQEEvent(ctx,t) ⇔
  ∃ cqe. emitted(cqe,t) ∧ cqe.user_data = raw(ctx)
NoCompletion(ctx,t) ⇔
  ∃ r. OperationCarrier(r)
  ∧ names(ctx,r)
  ∧ pending(r,t)
  ∧ ¬∃ t' ≤ t. CQEEvent(ctx,t')
  ∧ ¬∃ t' ≤ t. terminal(r,t')

Cancel(ctx,t) ⇔ submitted_cancel(ctx,t)
CancelAccepted(ctx,t) ⇔ accepted_cancel(ctx,t)
CloseRequested(r,t) ⇔ caller_close_request(r,t)
Close(r,t) ⇔ CloseRequested(r,t)
StopRequested(r,t) ⇔ caller_stop_request(r,t)
Stop(r,t) ⇔ StopRequested(r,t)
race({Cancel,Close,Stop}, visible_CQE_facts)

CancelAccepted(ctx,t) ↛ NoCompletion(ctx,t)
CloseRequested(r,t) ↛ released(r,t)
StopRequested(r,t) ↛ terminal(r,t)
StopRequested(r,t) ∧ Drain(r) → ∃ t'. (t ≤ t' ∧ stopped(r,t'))

Drain(r) ⇔
  quiesce_submitters(r)
  ∧ observe_all_visible_cqes(r)
  ∧ retire_subscriptions(r)
  ∧ release_terminal_resources(r)

used_lifecycle_policy(Π_life) ⇔
  support(Π_life) ∩ {lifecycle_policy,route_retirement,completion_routing} ≠ ∅
used_lifecycle_policy(Π_life) → support(Π_life) ⊆ CallerPolicy
∀ p ∈ {lifecycle_policy,route_retirement,completion_routing}.
  p ∈ support(Π_life) → policy_owner(p) = C
support(Σ_life) ⊆ CallerFrontier
support(Π_life) ⊆ CallerPolicy
frontier_owner(Σ_life) = C
policy_owner(Π_life) = C
support(Σ_life) ∩ support(B) = ∅
support(Π_life) ∩ support(B) = ∅
Φ_life ⊇ {drain,quiesce,cancel,recycle,release,retire,close}
```
## Buffers, Selection, And Zero-Copy

Buffers are resources with a lifecycle of their own — `allocate → own → publish → select → consume → recycle → release` — and zero-copy adds a window the kernel may still own until both the data and notification frontiers are observed.

In plain terms, the rules an agent follows here are:

- A buffer has its own lifecycle, owned by `code.hybscloud.com/iobuf`. It moves `allocate → own → publish → select → consume → recycle → release`, and the buffer-shaping facts (pool index, aligned region, iovec view, array copy, typed slice view, reset-without-zero) belong to `code.hybscloud.com/iobuf`.
- While the kernel may own a buffer, you may not touch it. Reuse, return, mutate, unregister, and recycle are all disabled for a buffer the kernel may still be reading or writing.
- Register, provide, select, and consume under explicit preconditions. Registration needs an owned, aligned, stable-address buffer and a valid slot; a provided buffer is published to a valid group and id; a selected buffer comes from a CQE carrying the buffer-select flag and a selected-buffer frontier; after consuming the first `n` bytes the buffer is recycled, copied-then-recycled, or released.
- Release a zero-copy window exactly once, and only when it is safe. The release fires after both the data and notification frontiers are observed, or through an explicit terminal no-notification fallback, and never more than once; while pending it stays kernel-owned.
- Keep the buffer alive for the whole kernel interval. An iovec or typed-slice view submitted to the kernel must stay valid for the entire pending-kernel interval; a copy breaks aliasing, so only the copy is kernel-visible.
- Return ownership on short and error I/O. On a short read or write the caller still owns the unfilled remainder; on an error the buffer's owner epoch is unchanged or moves to a failure frontier, and ownership returns to the caller without a full transfer.
- Keep buffer wait and recycle policy in the caller. Backoff, poll cadence, parking, recycle policy, and any clear-on-reset policy are caller-owned, and their state never lives inside the boundary.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

J_buf =
  Γ_buf | Θ_buf ⊢ buffer_action @ ring : result | Δ_buf ▷ Θ_buf'
    ; χ_buf ; Σ_buf ; Μ_buf ; Φ_buf ; Obs_buf ; Rel_buf ; Π_buf
Obs_buf ∈ OutcomePlane
Rel_buf ∈ ReleasePlane
denote_buffer_boundary(w) ⇔
  w ∈ World_U
  ∧ J_buf ∈ JudgmentSurface
  ∧ defined(⟦J_buf⟧^U_w)

BufferLifecycleEvent = {allocate,own,publish,select,consume,recycle,release}
buffer_lifecycle_fact(buf,e) ∈ Fact ⇔ e ∈ BufferLifecycleEvent
BufferLifecycle(buf) =
  {buffer_lifecycle_fact(buf,e) | e ∈ BufferLifecycleEvent}
buffer_action ∈ {register,unregister,provide,select,consume,recycle,release,zc_send}

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

owned_by_pool(buf) →
  BufferLifecycle(buf) ⊆ dom(pkg_owner)
  ∧ ∀ f ∈ BufferLifecycle(buf). pkg_owner(f) = pkg("code.hybscloud.com/iobuf")
ForbiddenKernelOwnAction(buf) =
  {reuse(buf),return(buf),mutate(buf),unregister(buf),recycle(buf)}
kernel_may_own(buf) → ∀ a ∈ ForbiddenKernelOwnAction(buf). disabled(a)

RegisterPre(buf,slot) ⇔
  owned(buf)
  ∧ aligned(buf)
  ∧ stable_address(buf)
  ∧ valid(slot)
RegisterPost(buf,slot) ⇔
  (success → registered(buf,slot))
  ∧ (failure → owner_epoch(buf)' = owner_epoch(buf))
Register(buf,slot) ⇔ RegisterPre(buf,slot) ∧ RegisterPost(buf,slot)

Provided(buf,group,id) ⇔
  valid(group) ∧ valid(id) ∧ owned(buf) ∧ published_to_kernel(buf,group,id)

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

frontier_fact(Obs,F) ⇔ F ∈ frontier_set(Obs)

Selected(cqe,group,id,n) ⇔
  has_buffer_select(cqe.Flags)
  ∧ frontier_fact(Obs_buf, selected_buffer(group,id))
  ∧ 0 ≤ n ≤ len(buffer(group,id))

Consume(buf,n) ⇔
  Selected(cqe,group,id,n)
  ∧ slice(buf,0,n)
  ∧ after_consume(buf) ∈ {recycle(buf),copy_then_recycle(buf),release(buf)}

Time = boundary_observation_events
next ⊆ Time × Time
≤ = reflexive_transitive_closure(next)
t1 < t2 ⇔ t1 ≤ t2 ∧ t1 ≠ t2
at : Fact × Time → Prop
lifetime(bufs) ⊆ Time
pending_kernel_interval(x,t) ⊆ Time

ZCId = {zc_id | zero_copy_identity(zc_id)}
ZCState = {pending_data,pending_notification,early_notification,terminal_fallback,release_ready}
zc_state : ZCId × Time → ZCState
ZCTracker(buf,zc_id,t) ⇔
  zc_state(zc_id,t) ∈ {pending_data,pending_notification,early_notification}
  ∧ kernel_may_own(buf)
data_frontier(zc_id) ⇔ data_CQE_observed(zc_id)
notification_frontier(zc_id) ⇔ notification_CQE_observed(zc_id)
data_frontier_at(zc_id,t) ⇔ at(data_frontier(zc_id),t)
notification_frontier_at(zc_id,t) ⇔ at(notification_frontier(zc_id),t)
notification_observed_before_data(zc_id) ⇔
  ∃ t_notify,t_data.
    notification_frontier_at(zc_id,t_notify)
    ∧ data_frontier_at(zc_id,t_data)
    ∧ t_notify < t_data
early_notification_frontier(zc_id) ⇔
  notification_observed_before_data(zc_id)
terminal_no_notification_frontier(buf,zc_id) ⇔
  explicit_terminal_no_notification_fallback(buf,zc_id)
release_zc(buf,zc_id) ⇔
  (data_frontier(zc_id) ∧ notification_frontier(zc_id))
  ∨ terminal_no_notification_frontier(buf,zc_id)
release_zc_at(buf,zc_id,t) ⇔ at(release_zc(buf,zc_id),t)
release_zc_once(buf,zc_id) ⇔
  ∀ t1,t2.
    (release_zc_at(buf,zc_id,t1) ∧ release_zc_at(buf,zc_id,t2)) → t1 = t2

ShortIO(op,n,buf) ⇔
  0 ≤ n < requested_len(op,buf)
  ∧ decode_result(op,n) = count(n)
  ∧ caller_owns(remaining(buf,n))

ErrorIO(errno,buf) ⇔
  Obs_buf ∈ OutcomeProduct
  ∧ ctrl(Obs_buf) = fail(typed_error(errno))
  ∧ (owner_epoch(buf)' = owner_epoch(buf)
     ∨ ∃ ctx.
         names(ctx,buf,Obs_buf)
         ∧ (frontier_fact(Obs_buf, terminal_frontier_after_failure(ctx,buf))
            ∨ frontier_fact(Obs_buf, nonterminal_frontier_after_failure(ctx,buf))))

support(Π_buf) ⊆ CallerPolicy
support(Σ_buf) ⊆ CallerFrontier
bo_buf : type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")
buffer_wait_policy(buf) ∈ {backoff_policy(bo_buf), poll_cadence, parking}
buffer_recycle_policy(buf) ∈ {lifecycle_policy}
uses_buffer_wait_policy(buf,J_buf) → buffer_wait_policy(buf) ∈ support(Π_buf)
uses_buffer_recycle_policy(buf,J_buf) → buffer_recycle_policy(buf) ∈ support(Π_buf)
uses_backoff(J_buf,bo_buf) → backoff_policy(bo_buf) ∈ support(Π_buf)
uses_backoff(J_buf,bo_buf) → caller_state_owner(state(bo_buf)) = C
state(bo_buf) ∉ support(B)
{buffer_wait_policy(buf),buffer_recycle_policy(buf),state(bo_buf)} ∩ support(B) = ∅

submitted_iovec(vec,bufs,t) ∧ IoVecView(vec,bufs)
  → pending_kernel_interval(vec,t) ⊆ lifetime(bufs)
submitted_typed_slice_view(v,s,t) ∧ SliceOfArrayView(s,off,n,T,v)
  → pending_kernel_interval(v,t) ⊆ lifetime(s)
ArrayFromSliceCopy(s,off,T,out) ∧ submit_with_buf(out)
  → kernel_view(out) ∧ ¬kernel_view(s)
ResetNoZero(buf) ∧ requires_clear(buf)
  → buffer_clear_policy ∈ support(Π_buf)
```
