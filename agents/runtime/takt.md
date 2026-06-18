# Runner Utilization

The loop that drives operations against the ring is the caller's, not the package's, and `code.hybscloud.com/takt` is how you build that loop correctly. This file is the rule for using `code.hybscloud.com/takt` with `code.hybscloud.com/uring`: a token *is* the `user_data` operation identity, a one-shot completion routes through `Loop` while a multishot stream routes through `SubscriptionLoop` keyed by a `RouteID` of token and generation, drain corresponds to the stop and drain epoch, and runner movement, completion routing, and completion memory all stay caller-owned and out of the boundary. A reused live token or route is rejected.

In plain terms, the rules an agent follows here are:

- A token is the operation identity. A token from `code.hybscloud.com/takt` *is* the `user_data` of a boundary operation; reusing a still-live token is rejected (`ErrLiveTokenReuse`), and a completion value must be copied facts, never a borrowed CQE view.
- Route one-shot and multishot differently. A one-shot operation runs through `Loop`; a multishot operation runs through `SubscriptionLoop` keyed by a `RouteID = (Token, Generation)`. A multishot completion arriving in a one-shot `Loop` fails as `ErrUnsupportedMultishot`, and reusing a live route is rejected (`ErrLiveRouteReuse`).
- Distinguish dispatcher-level from completion-level `ErrMore`. `Advance`/`AdvanceSuspension` resume on dispatcher `ErrMore` and return a live frontier; the generic `Loop` rejects completion-level `ErrMore` as unsupported multishot; a stream completion's `More` flag means the route stays live, and its absence means the route may retire.
- Back off and wait in the caller. On `ErrWouldBlock` or no completion, the park, spin-wait, or backoff decision and its state are caller-owned; the loop never hides them.
- The repetition loop is the caller's, never the boundary's. `loop_step` observes one completion and returns control; the repetition carrier (`Loop` or `SubscriptionLoop`) is caller-owned and out of the boundary, a hidden runner loop is rejected, and whether to take the next observation is the caller's decision.
- Drain is the stop-and-drain epoch. Draining a loop stops submission, discards pending suspensions, and releases completion memory, but does not prove every token reached terminal; a live backend after drain leaves cancel, close, or ignore to the caller, and runner movement, completion routing, and completion memory stay caller-owned and out of the boundary.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

owner(dispatch) = pkg("code.hybscloud.com/takt")
owner(backend_loop) = pkg("code.hybscloud.com/takt")
owner(runtime_token) = pkg("code.hybscloud.com/takt")
owner(pending_token_table) = pkg("code.hybscloud.com/takt")
owner(completion_memory) = pkg("code.hybscloud.com/takt")
owner(subscription_route) = pkg("code.hybscloud.com/takt")
owner(route_table) = pkg("code.hybscloud.com/takt")
owner(reify_reflect_bridge) = pkg("code.hybscloud.com/takt")
owner(dispatch_error_short_circuit) = pkg("code.hybscloud.com/takt")
owner(kernel_boundary_facts) = pkg("code.hybscloud.com/uring")

TaktTheoryConcept =
  {coalgebraic_observation, temporal_obligation}
∀ c ∈ TaktTheoryConcept.
  runtime_concept_use(pkg("code.hybscloud.com/takt"),c) ∈ RuntimeConceptUse
  ∧ use_pkg(runtime_concept_use(pkg("code.hybscloud.com/takt"),c))
      = pkg("code.hybscloud.com/takt")
  ∧ support(runtime_concept_use(pkg("code.hybscloud.com/takt"),c)) ⊆ C
  ∧ project(runtime_concept_use(pkg("code.hybscloud.com/takt"),c),B) = ∅

allow(C, import(pkg("code.hybscloud.com/takt")))
reject(B, import(pkg("code.hybscloud.com/takt")))

RunnerCarrier =
  {Dispatcher, Backend, Loop, Token, Completion, SuspensionLike,
   Reify, Reflect, Exec, ExecExpr, Step, Advance, AdvanceSuspension,
   ExecError, ExecErrorExpr, StepError, AdvanceError,
   SubscriptionBackend, SubscriptionLoop, RouteID, StreamEvent,
   CompletionMemory, HeapMemory, BoundedMemory, StreamCompletion}
EffectCarrier = {Eff[A], Expr[A], Either[E,A], Suspension[A]}

Completion = {Token, Value, Err}
StreamCompletion = {RouteID, Value, HasValue, EventErr, More}
ErrMore_takt = err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore")
ErrWouldBlock_takt = err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock")
ErrUnsupportedMultishot_takt =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrUnsupportedMultishot")
ErrLiveTokenReuse_takt =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrLiveTokenReuse")
ErrLiveRouteReuse_takt =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrLiveRouteReuse")
ErrInvalidRouteID_takt =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrInvalidRouteID")
ErrUnknownSubscription_takt =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrUnknownSubscription")
ErrDisposed_takt =
  err_symbol(pkg("code.hybscloud.com/takt"),"ErrDisposed")
ErrMore_takt ∈ ErrorSymbol
ErrWouldBlock_takt ∈ ErrorSymbol
TaktFatalSymbol =
  {ErrUnsupportedMultishot_takt, ErrLiveTokenReuse_takt,
   ErrLiveRouteReuse_takt, ErrInvalidRouteID_takt,
   ErrDisposed_takt}
TaktFatalSymbol ⊆ ErrorSymbol
Backoff_takt = type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")
SpinWait_takt = type_symbol(pkg("code.hybscloud.com/spin"),"Wait")
DispatchErrMore_takt = site(dispatch_boundary, ErrMore_takt)
CompletionErrMore_takt = site(completion_boundary, ErrMore_takt)
more_flag_takt(c) = c.More
more_flag_takt(c) ∈ Bool
Bool ∩ ErrorSymbol = ∅
StreamMore_takt(c) ⇔ c ∈ StreamCompletion ∧ more_flag_takt(c) = true
DispatchErrMore_takt ≠ CompletionErrMore_takt
meaning(DispatchErrMore_takt, AdvanceSuspension) =
  resume_and_return_live_frontier
meaning(CompletionErrMore_takt, Loop(pkg("code.hybscloud.com/takt"))) =
  unsupported_multishot_for_one_shot_carrier
meaning(more_flag_takt(c) = true, SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
  route_remains_live(route(c))
meaning(more_flag_takt(c) = false, SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
  route_may_retire_after_event(route(c))
PendingTable = finite_map(Token, Suspension[A])
CompletionMemoryStore_takt = loop_slab_storage([]Completion)
storage_realizer(CompletionMemory, CompletionMemoryStore_takt)
RouteTable = finite_map(RouteID, RouteState)
RouteState = {active, canceling}
current_suspension_takt(token) =
  PendingTable[token] if token ∈ dom(PendingTable)
current_suspension_takt(op) =
  ιs. s ∈ type_symbol(pkg("code.hybscloud.com/kont"),"Suspension[A]")
      ∧ pending_op(s)=op
  if ∃!s. s ∈ type_symbol(pkg("code.hybscloud.com/kont"),"Suspension[A]")
           ∧ pending_op(s)=op
BoundaryDispatch(op) ⇔ op ∈ boundary_action(B)
BoundaryCompletionValue(v) ⇔
  copied_facts(v) ∧ borrowed_cqe_view ∉ v

boundary_token_correlates(token,op) ⇔
  BoundaryDispatch(op)
  ∧ token_indexes(token,user_data(op))
  ∧ identity(op)=user_data(op)
backend_submit(token,op) =
  BoundaryDispatch(op) ∧ boundary_token_correlates(token,op)
live(token) ⇔ ∃cqe op. may_arrive(cqe,op) ∧ boundary_token_correlates(token,op)
submit(token',op') ∧ live(token')
  → fail(ErrLiveTokenReuse_takt)
    ∧ fatal(Loop(pkg("code.hybscloud.com/takt")),
             ErrLiveTokenReuse_takt)
    ∧ drain(PendingTable)
completion(token,v) ∧ ∃op. boundary_token_correlates(token,op)
  → BoundaryCompletionValue(v)
terminal(Completion(token)) → retire(token)
retire(token) → delete(PendingTable[token])
poll(Loop(pkg("code.hybscloud.com/takt"))) =
  idle ⊎ advanced ⊎ completed([]Result) ⊎ failed(error)
poll(Loop(pkg("code.hybscloud.com/takt"))) = idle →
  result=nil ∧ err=nil ∧ progress=false
poll_err = ErrWouldBlock_takt
  → poll(Loop(pkg("code.hybscloud.com/takt"))) = idle
    ∧ no_mutation(PendingTable)
fatal(Loop(pkg("code.hybscloud.com/takt")),e) ∧ submit(token,op)
  → fail(e)
fatal(Loop(pkg("code.hybscloud.com/takt")),e)
  → poll(Loop(pkg("code.hybscloud.com/takt"))) = fail(e)
drain(Loop(pkg("code.hybscloud.com/takt"))) ∧
  fatal_pre(Loop(pkg("code.hybscloud.com/takt")))=nil
  → fatal_post(Loop(pkg("code.hybscloud.com/takt"))) =
      ErrDisposed_takt
drain(Loop(pkg("code.hybscloud.com/takt"))) ∧
  fatal_pre(Loop(pkg("code.hybscloud.com/takt")))≠nil
  → fatal_post(Loop(pkg("code.hybscloud.com/takt"))) =
      fatal_pre(Loop(pkg("code.hybscloud.com/takt")))

⟦Reify(Eff[A])⟧ = Expr[A]
⟦Reflect(Expr[A])⟧ = Eff[A]
⟦Reflect(Reify(m))⟧ ≃ m modulo representation_cost
support({Reify, Reflect}) ⊆ C
project({Reify, Reflect}, B) = ∅

⟦Exec(d,m)⟧ =
  repeat dispatch(d,op) until
    err ∈ {nil, ErrMore_takt}
    ∨ err ∈ FailureEvidence
exec_dispatch_err = ErrWouldBlock_takt
  → wait_with(Backoff_takt)
    ∧ policy_owner(backoff_policy(Backoff_takt)) = C
exec_dispatch_err ∈ FailureEvidence → panic_or_fail(exec_dispatch_err)
⟦ExecExpr(d,e)⟧ = ⟦Exec(d,Reflect(e))⟧ modulo ReifyReflect

Step(Expr[A]) = done(A) ⊎ suspended(Suspension[A])
Advance(d,SuspensionLike[S,A]) =
  advanced(A,S') ⊎ no_progress(S) ⊎ failed(error)
advance_dispatch_err = nil
  → resume(current_suspension_takt(op)) ∧ return_next_frontier
advance_dispatch_err = ErrMore_takt
  → resume(current_suspension_takt(op))
    ∧ return_error(ErrMore_takt)
    ∧ return_next_frontier
advance_dispatch_err = ErrWouldBlock_takt
  → return_original_suspension ∧ no_progress
advance_dispatch_err ∈ FailureEvidence
  → return_original_suspension ∧ fail(advance_dispatch_err)

StepError_{E,A}(Expr[A]) =
  done(Either[E,A]) ⊎ suspended(Suspension[Either[E,A]])
AdvanceError_{E,A}(d,Suspension[Either[E,A]]) =
  advanced(Either[E,A], Suspension[Either[E,A]] ⊎ nil)
  ⊎ no_progress(Suspension[Either[E,A]])
  ⊎ failed(error)
DispatchError(op) ∧ thrown(e:E)
  → discard(current_suspension_takt(op))
    ∧ return(Left_{E,A}(e))
    ∧ no_dispatch_to_boundary(B)
¬DispatchError(op) ∧ AdvanceError_{E,A}(d,s)
  → same_dispatch_classification_as(Advance(d,s))
⟦ExecError_{E,A}(d,m)⟧ = Either[E,A]
⟦ExecErrorExpr_{E,A}(d,e)⟧ = Either[E,A]
error_short_circuit_owner = C
project({ExecError, ExecErrorExpr, StepError, AdvanceError}, B) = ∅

one_shot(op) → route_via(Loop(pkg("code.hybscloud.com/takt")))
multishot(op) → route_via(SubscriptionLoop(pkg("code.hybscloud.com/takt")))
multishot_completion_in(Loop(pkg("code.hybscloud.com/takt")))
  → fail(ErrUnsupportedMultishot_takt)

route_id = (Token, Generation)
route_id ∈ RouteID
zero_route = zero(RouteID)
live(route_id) ⇔ ∃cqe op. may_arrive(cqe,op) ∧ route(op)=route_id
subscribe(route',op') ∧ route'=zero_route
  → fail(ErrInvalidRouteID_takt)
    ∧ fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),
             ErrInvalidRouteID_takt)
    ∧ drain(RouteTable)
subscribe(route',op') ∧ live(route')
  → fail(ErrLiveRouteReuse_takt)
    ∧ fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),
             ErrLiveRouteReuse_takt)
    ∧ drain(RouteTable)
boundary_more(cqe) = has_flag(cqe, IORING_CQE_F_MORE)
has_flag(cqe, IORING_CQE_F_MORE) ⇔ flag(MORE) ∈ flag_projection(cqe.Flags)
stream_projection_required_facts(cqe) = {cqe.Flags, cqe.user_data}
derived_from_cqe(sc,cqe) ⇔
  cqe_source(sc) = cqe
  ∧ stream_projection_required_facts(cqe) ⊆ copied_facts(sc)
  ∧ copied_facts(sc) ⊆
    {cqe.Res, cqe.Flags, cqe.user_data, selected_buffer_metadata(cqe)}
boundary_stream_completion(route,cqe,sc) ⇔
  stream_completion(route,cqe,sc)
  ∧ derived_from_cqe(sc,cqe)
  ∧ cqe.user_data = route_identity(route)
boundary_stream_completion(route,cqe,sc) →
  (sc.More ⇔ boundary_more(cqe))
event_error(sc) = sc.EventErr
payload_failure(sc) ⇔ event_error(sc) ≠ nil
payload_failure(sc) ∧ sc.More
  → stream_event(route,cqe) ∧ live(route) ∧ ¬retire(route)
payload_failure(sc) ∧ ¬sc.More ∧ observes(route,cqe)
  → stream_event(route,cqe) ∧ retire(route)
stream_completion(route,cqe,sc) ∧ sc.More
  → stream_event(route,cqe) ∧ live(route)
stream_completion(route,cqe,sc) ∧ ¬sc.More ∧ observes(route,cqe)
  → retire(route)
retire(route) ⇔
  route ∈ dom_pre(RouteTable) ∧ route ∉ dom_post(RouteTable)
cancel(route) ∧ state_pre(RouteTable,route)=active →
  backend_cancel(route) ∧ state_post(RouteTable,route)=canceling
cancel(route) ∧ state_pre(RouteTable,route)=canceling →
  no_duplicate_cancel(route) ∧ state_post(RouteTable,route)=canceling
cancel(route) ∧ (route=zero_route ∨ route ∉ dom_pre(RouteTable)) →
  fail(ErrUnknownSubscription_takt) ∧ no_mutation(RouteTable)
cancel_success(route) ⇔ cancel(route) ∧ route ∈ dom_pre(RouteTable)
cancel_success(route) →
  route ∈ dom_pre(RouteTable)
  ∧ route_remains_live_until_terminal_or_drain(route)
fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),e) ∧
  subscribe(route,op)
  → fail(e)
fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),e) ∧
  cancel(route)
  → fail(e)
fatal(SubscriptionLoop(pkg("code.hybscloud.com/takt")),e)
  → poll(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) = fail(e)
drain(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) →
  ∀ route ∈ dom_pre(RouteTable).
    retire(route)
    ∧ (state_pre(RouteTable,route)=active → backend_cancel(route))
    ∧ (state_pre(RouteTable,route)=canceling → no_duplicate_cancel(route))
drain(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) ∧
  fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))=nil
  → fatal_post(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
      ErrDisposed_takt
drain(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) ∧
  fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))≠nil
  → fatal_post(SubscriptionLoop(pkg("code.hybscloud.com/takt"))) =
      fatal_pre(SubscriptionLoop(pkg("code.hybscloud.com/takt")))

completion_err = ErrMore_takt ∧
  carrier = Loop(pkg("code.hybscloud.com/takt"))
  → fail(ErrUnsupportedMultishot_takt)
advance_suspension_err = ErrMore_takt
  → resume(current_suspension_takt(op))
    ∧ return_error(ErrMore_takt)
    ∧ return_next_frontier
advance_suspension_err = ErrWouldBlock_takt
  → return_original_suspension ∧ no_progress
advance_suspension_err ∈ FailureEvidence
  → return_original_suspension ∧ fail(advance_suspension_err)
AdvanceSuspension(SuspensionLike,s) →
  preserves_outer_suspension_type(s)
AdvanceSuspension(type_symbol(pkg("code.hybscloud.com/cove"),"SuspensionView"), s) →
  preserves_context_carrier(s)
poll_no_completion(Loop(pkg("code.hybscloud.com/takt"))) →
  no_progress
  ∧ decision_owner({park, spin_wait_policy(SpinWait_takt),
                    backoff_policy(Backoff_takt)}) = C
backoff_policy(Backoff_takt) ∈ CallerPolicy
policy_owner(backoff_policy(Backoff_takt)) = C
spin_wait_primitive(SpinWait_takt)
pkg_owner(spin_wait_yield_lock) = pkg("code.hybscloud.com/spin")
spin_wait_policy(SpinWait_takt) ∈ CallerPolicy
policy_owner(spin_wait_policy(SpinWait_takt)) = C
m_loop : type_symbol(pkg("code.hybscloud.com/takt"),"CompletionMemory")
completion_memory_policy(m_loop) ∈ CallerPolicy

{retirement_policy, resubscribe, cancel_timing, poll_cadence,
 completion_buffer_policy, completion_memory_policy(m_loop)} ⊆ CallerPolicy
drain(loop) =
  stop_submit(loop)
  ∧ discard(∀token ∈ dom(PendingTable). PendingTable[token])
  ∧ release(completion_memory(loop))
  ∧ ¬owns(loop, backend_cancel_carrier)
  ∧ ¬proves(∀token. live(token) → terminal_observed(token))

live_backend_after_drain(token) →
  decision_owner({cancel_backend, close_backend, ignore_stale_completion}) = C

drain(subscription_loop) =
  stop_subscribe(subscription_loop)
  ∧ cancel_or_retire(∀route. live(route))
  ∧ clear(stream_completion_buffer(subscription_loop))
  ∧ clear(stream_event_buffer(subscription_loop))

defined_at(shallow_effect_handler) = path("uring/agents/boundary/protocols.md")
loop_step(loop) = observe_one_completion(loop) ∧ return_control(loop,C)
handles_one_visible_frontier(loop_step(loop))
one_shot(op) → repetition_carrier(op) = Loop(pkg("code.hybscloud.com/takt"))
multishot(op) →
  repetition_carrier(op) = SubscriptionLoop(pkg("code.hybscloud.com/takt"))
repetition_carrier(op) ∉ support(B)
reject(hidden_runner_loop(B))
successor_observation_possible(ctx) → decision_owner(next_observation) = C

support(RunnerCarrier ∪ EffectCarrier ∪ {PendingTable, RouteTable,
                         CompletionMemoryStore_takt,
                         backoff_policy(Backoff_takt), spin_wait_policy(SpinWait_takt),
                         poll_cadence}) ⊆ C
support(kernel_boundary_facts) ⊆ B
project(RunnerCarrier, B) = ∅
reject(project({Dispatcher, Backend, Loop, SubscriptionLoop,
                Reify, Reflect, Exec, ExecExpr, Step, Advance,
                AdvanceSuspension, ExecError, ExecErrorExpr,
                StepError, AdvanceError,
                CompletionMemory, PendingTable, RouteTable,
                backoff_policy(Backoff_takt), poll_cadence,
                completion_memory_policy(m_loop)}, B))
```
