# Protocol Boundary

This is the file for everything you build *above* `code.hybscloud.com/uring`: protocol stacks, parsers, routers, and the handlers and closures around them. Its one message is that the package is a *shallow* effect handler — it surfaces one kernel observation and hands control straight back to you, never reinstalling itself to interpret the next completion. So the protocol transition, the route lookup, the retry, and the parser state are all yours (in `Π_proto`), never the boundary's; a handler that kept control over successor observations would be *deep*, and a deep handler is exactly a hidden runner loop, which the boundary rejects.

The block carries the same discipline onto the Go surface: a `GoHandler` is shallow when it consumes one visible frontier and resumes at most once, a handler closure may capture only copied completion facts or caller-owned state (never a borrowed CQE view), and a `HandlerTable` keyed by caller-owned event keys is registered before dispatch and kept out of the boundary.

In plain terms, the rules an agent follows here are:

- Own the protocol; the package only observes. Stream, datagram, outcome, and byte-progress vocabulary belong to `code.hybscloud.com/iox`; suspension and one-shot resumption to `code.hybscloud.com/kont`; protocol frontiers to `code.hybscloud.com/sess`; route, subscription, runner-movement, and completion-memory vocabulary to `code.hybscloud.com/takt`; safety evidence to `code.hybscloud.com/cove`; spin, yield, and lock to `code.hybscloud.com/spin`. The protocol frontier and policy are caller-owned, and the protocol transition taken after an observation is caller-defined and never lives inside the boundary.
- Read multishot completions as frontier facts, using the same four-way decode. A non-negative multishot CQE with `MORE` is a live success that keeps the frontier non-terminal and signals that a successor observation may arrive; a negative multishot CQE with `MORE` is a non-terminal failure. A failure with `MORE` is never `errMore`, never byte progress, and never terminal.
- Decode zero-copy release separately. A release-ready CQE decodes to a release observation with no control value, carrying the `NOTIF` flag when the release is notification-driven and not when it is the terminal no-notification fallback; a notification that is not release-ready, and an early notification, both produce no outcome and no release.
- Hand result resources to the caller. A returned descriptor or selected buffer is owned by the caller and never appears on a non-terminal frontier.
- Treat the package as a shallow handler, never a deep one. A shallow handler handles one visible frontier, resumes at most once per observation, stores no boundary borrow, and leaves policy in the caller; a deep handler that keeps control over successor observations is a hidden runner loop, which is rejected, so repetition is the caller's own explicit loop.
- Discharge the shallow obligations. A shallow handler must satisfy the outcome, owner, and policy gates and the support-split obligation — defined in [workflow/boundary-gates.md](../workflow/boundary-gates.md) and [workflow/proof-obligations.md](../workflow/proof-obligations.md) — plus the copy-CQE rule.
- Keep Go handlers, closures, and tables shallow and caller-owned. A Go handler is shallow when it consumes one visible frontier and resumes at most once; a handler closure may capture only copied completion facts or caller-owned state, never a borrowed CQE view; a handler table is keyed by caller-owned event keys, every handler in it is shallow, it is built before dispatch starts, and hot-path dispatch allocates nothing and builds no closures.
- Keep parser, route, and completion-memory policy in the caller. Parser state and branch policy, route lookup and retirement, completion pool and GC-root policy, and completion-memory facts and policy are all caller-owned (completion-memory facts owned by `code.hybscloud.com/takt`), and none of them may hide inside the boundary.

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

J_proto =
  Γ_proto | Θ_proto ⊢ protocol_boundary_action @ ring : result | Δ_proto ▷ Θ_proto'
    ; χ_proto ; Σ_proto ; Μ_proto ; Φ_proto ; Obs_proto ; Rel_proto ; Π_proto
denote_protocol_boundary(w) ⇔
  w ∈ World_U
  ∧ J_proto ∈ JudgmentSurface
  ∧ defined(⟦J_proto⟧^U_w)

ProtocolOwner =
  { stream_contract ↦ pkg("code.hybscloud.com/iox")
  , stream_frontier_vocabulary ↦ pkg("code.hybscloud.com/iox")
  , datagram_contract ↦ pkg("code.hybscloud.com/iox")
  , outcome_control_vocabulary ↦ pkg("code.hybscloud.com/iox")
  , byte_progress_contract ↦ pkg("code.hybscloud.com/iox")
  , suspension_frontier_vocabulary ↦ pkg("code.hybscloud.com/kont")
  , one_shot_resumption ↦ pkg("code.hybscloud.com/kont")
  , affine_resumption ↦ pkg("code.hybscloud.com/kont")
  , protocol_frontier_vocabulary ↦ pkg("code.hybscloud.com/sess")
  , route_frontier_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , subscription_frontier_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , safety_evidence ↦ pkg("code.hybscloud.com/cove")
  , runner_movement_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , completion_memory_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , spin_wait_yield_lock ↦ pkg("code.hybscloud.com/spin")
  }

support(Σ_proto) ⊆ CallerFrontier
support(Π_proto) ⊆ CallerPolicy
frontier_owner(Σ_proto) = C
policy_owner(Π_proto) = C
frontier_vocabulary(suspension_frontier) = suspension_frontier_vocabulary
frontier_vocabulary(stream_frontier) = stream_frontier_vocabulary
frontier_owner(stream_frontier) = C
frontier_vocabulary(protocol_frontier) = protocol_frontier_vocabulary
frontier_vocabulary(route_frontier) = route_frontier_vocabulary
frontier_vocabulary(subscription_frontier) = subscription_frontier_vocabulary
frontier_vocabulary(runner_movement) = runner_movement_vocabulary
frontier_fact(X,F) ⇔ F ∈ frontier_set(X)
Obs_proto ∈ OutcomePlane
Rel_proto ∈ ReleasePlane
FrontierCarrier(ctx) =
  operation_carrier_named_by(ctx,Θ_proto,Θ_proto',Σ_proto,Μ_proto,Obs_proto)
∀ ctx. OperationCarrier(FrontierCarrier(ctx))
ZeroCopyReleaseCarrierKind = {ExtSQE}
ZeroCopySendOperation = {op | zero_copy_send_opcode(op)}
zero_copy_send_ctx(ctx) ⇔ op_of(ctx) ∈ ZeroCopySendOperation
zero_copy_release_carrier(ctx,r) ⇔
  names(ctx,r)
  ∧ OperationCarrier(r)
  ∧ resource_kind(r) ∈ ZeroCopyReleaseCarrierKind
ResultResource(out) ⇔
  out ∈ Resource
  ∧ resource_kind(out) ∈ {FD,DirectFD,Buf,FixedBuf,ProvidedBuf}
FDResultResource(out) ⇔
  ResultResource(out) ∧ resource_kind(out) ∈ {FD,DirectFD}
fd_of : {out | FDResultResource(out)} ⇀ DescriptorResult
result_resource(cqe,ctx,out) ⇔
  ResultResource(out)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource(out)
      ∧ defined(fd_of(out))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(out)))
     ∨ selected_buffer_result(cqe,out)
     ∨ explicit_result_resource(cqe,out))

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
early_notification_cqe(cqe,ctx,H) ⇔
  notification_cqe(cqe)
  ∧ cqe.user_data = raw(ctx)
  ∧ ¬data_frontier_seen(ctx,H)
terminal_fallback_carrier(ctx,r) ⇔
  zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
EarlyNotification_proto(cqe,ctx) ⇔
  early_notification_cqe(cqe,ctx,history(ctx))
  ∧ zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,FrontierCarrier(ctx))
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
  ∧ terminal_fallback_carrier(ctx,r)
  ∧ ¬data_frontier_seen(ctx,H)
  ∧ ¬early_notification_seen(ctx,H)
release_ready_carrier_at(cqe,ctx,r,H) ⇔
  cqe.user_data = raw(ctx)
  ∧ zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
  ∧ (notification_release_ready_cqe(cqe,ctx,H)
     ∨ terminal_no_notification_cqe(cqe,ctx,r,H))
release_ready_cqe(cqe,ctx,H) ⇔
  ∃ r. release_ready_carrier_at(cqe,ctx,r,H)

multishot(ctx) ∧ operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags) →
  Obs_proto ∈ OutcomeProduct
  ∧ ctrl(Obs_proto) = ok
  ∧ has(flag(MORE),Obs_proto)
  ∧ frontier_fact(Obs_proto, nonterminal_frontier(ctx,FrontierCarrier(ctx)))
  ∧ successor_observation_possible(ctx)

multishot(ctx) ∧ operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags) →
  Obs_proto ∈ OutcomeProduct
  ∧ ctrl(Obs_proto) = fail(typed_error(-cqe.Res))
  ∧ frontier_fact(Obs_proto, nonterminal_frontier_after_failure(ctx,FrontierCarrier(ctx)))
  ∧ has(flag(MORE),Obs_proto)

release_ready_carrier_at(cqe,ctx,FrontierCarrier(ctx),history(ctx)) →
  Rel_proto ∈ ReleaseObservation
  ∧ frontier_fact(Rel_proto, release_frontier(ctx,FrontierCarrier(ctx)))
  ∧ ¬defined(ctrl(Rel_proto))
release_ready_carrier_at(cqe,ctx,FrontierCarrier(ctx),history(ctx))
∧ notification_release_ready_cqe(cqe,ctx,history(ctx)) →
  has(flag(NOTIF),Rel_proto)
release_ready_carrier_at(cqe,ctx,FrontierCarrier(ctx),history(ctx))
∧ terminal_no_notification_cqe(cqe,ctx,FrontierCarrier(ctx),history(ctx)) →
  ¬has(flag(NOTIF),Rel_proto)

notification_cqe(cqe)
∧ ¬release_ready_carrier_at(cqe,ctx,FrontierCarrier(ctx),history(ctx)) →
  Rel_proto = none
EarlyNotification_proto(cqe,ctx) →
  Obs_proto = none
  ∧ Rel_proto = none
  ∧ ¬defined(ctrl(Obs_proto))

result_resource(cqe,ctx,out) →
  caller_owns(out)
  ∧ ∀ ctx2. ¬frontier_fact(Obs_proto, nonterminal_frontier(ctx2,out))

(operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)) ↛
  (Obs_proto ∈ OutcomeProduct ∧ ctrl(Obs_proto) = errMore)

byte_progress(n) ⇔ n > 0 ∧ ∃ op. decode_result(op,n) = count(n)
ByteProgress ⇔ ∃ n. n > 0 ∧ byte_progress(n)
(Obs_proto ∈ OutcomeProduct ∧ ctrl(Obs_proto) = errMore) ↛ ByteProgress
terminal_completion(ctx,r) ⇔
  frontier_fact(Obs_proto, terminal_frontier(ctx,r))
  ∨ frontier_fact(Obs_proto, terminal_frontier_after_failure(ctx,r))
(Obs_proto ∈ OutcomeProduct ∧ ctrl(Obs_proto) = errMore) ↛
  terminal_completion(ctx,FrontierCarrier(ctx))

protocol_step(Σ_proto,Obs_proto) =
  caller_defined_transition(Σ_proto,Obs_proto)
protocol_step(Σ_proto,Obs_proto) ∉ support(B)

shallow_effect_handler(H) ⇔
  handles_one_visible_frontier(H)
  ∧ resumes_at_most_once_per_observation(H)
  ∧ stores_no_boundary_borrow(H)
  ∧ leaves_policy_in(Π_proto)

caller_boundary_handler = handler_view(C,B)
caller_boundary_handler ∈ support(C)
caller_boundary_handler ∉ support(B)
shallow_effect_handler(caller_boundary_handler)
deep(H) ⇔ retains_control_over_successor_observations(H)
shallow_effect_handler(H) → ¬deep(H)
deep(caller_boundary_handler) → hidden_runner_loop(B)
reject(hidden_runner_loop(B))
handler_repetition ∈ CallerFrontier
handler_repetition ∉ support(B)

ShallowObligation(J_proto) =
  { G_outcome(J_proto)
  , G_owner(J_proto)
  , G_policy(J_proto)
  , O_split(J_proto)
  , T_CopyCQE
  }
shallow_handler_pass(H,J_proto) ⇔
  shallow_effect_handler(H)
  ∧ ∀ x ∈ ShallowObligation(J_proto). holds(x)

GoHandler = { h ∈ GoSurface | caller_event_handler(h) }
handler_shallow(h) ⇔ h ∈ GoHandler ∧ shallow_effect_handler(h)
GoClosureHandler = { h ∈ GoHandler | closure(h) }
EventKey = finite set of caller-owned event keys
owner(EventKey) = C
HandlerTable = finite_map(EventKey,GoHandler)
support(HandlerTable) = dom(HandlerTable) ∪ range(HandlerTable) ∪ {HandlerTable}
reject(project(GoHandler ∪ GoClosureHandler ∪ support(HandlerTable), B))
∀ h ∈ range(HandlerTable). handler_shallow(h)
owner(HandlerTable) = C
∀ event ∈ EventKey. dispatch_choice(HandlerTable,event) ∈ support(C)
handler_closure_capture(h) ⊆ copied_completion_facts ∪ caller_owned_state
handler_closure_capture(h) ∩ borrowed_cqe_view = ∅
registration_epoch(HandlerTable) ≺ dispatch_epoch
hot_path(dispatch) → {alloc, closure} ∩ effects(dispatch) = ∅

original_protocol_stack(P) ⇔
  parser_state(P) ∈ support(Π_proto)
  ∧ parser_branch_policy(P) ∈ support(Π_proto)
  ∧ route_lookup(P) ∈ support(Π_proto)
  ∧ route_retirement(P) ∈ support(Π_proto)
  ∧ boundary_action(P) ∈ support(B)
  ∧ ¬hidden({parser_state(P),parser_branch_policy(P),route_lookup(P),route_retirement(P)}, support(B))

CompletionMemoryName = finite set of atoms
CompletionMemoryFact = {completion_memory_fact(m) | m ∈ CompletionMemoryName}
CompletionMemoryPolicy = {completion_memory_policy(m) | m ∈ CompletionMemoryName}
∀ m ∈ CompletionMemoryName.
  pkg_owner(completion_memory_fact(m)) = pkg("code.hybscloud.com/takt")
∀ m ∈ CompletionMemoryName.
  policy_owner(completion_memory_policy(m)) = C
∀ m.
  uses_completion_memory(P,m) →
    completion_memory_fact(m) ∈ CompletionMemoryFact
    ∧ completion_memory_policy(m) ∈ CompletionMemoryPolicy
    ∧ completion_pool_policy ∈ support(Π_proto)
    ∧ completion_gc_root_policy ∈ support(Π_proto)
    ∧ policy_owner(completion_memory_policy(m)) = C
    ∧ policy_owner(completion_pool_policy) = C
    ∧ policy_owner(completion_gc_root_policy) = C
    ∧ ¬hidden({completion_memory_fact(m),completion_memory_policy(m),
               completion_pool_policy,completion_gc_root_policy}, support(B))
```
