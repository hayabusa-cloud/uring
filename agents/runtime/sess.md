# Session Protocol Utilization

A session is a caller-side protocol state machine expressed over completions, and `code.hybscloud.com/sess` is how you build one on top of `code.hybscloud.com/uring` without letting the protocol leak into the boundary. This file is the rule for using `sess`: each protocol step advances on one boundary observation, `wouldBlock` returns control with the endpoint and frontier preserved, duality and post-`Close` terminality are caller checks, and the session frontier and parser state stay caller-owned. The `errMore`-to-`fail` asymmetry from the outcome plane carries through unchanged.

In plain terms, the rules an agent follows here are:

- One protocol step, one boundary observation. Each `Advance` of an endpoint binds one boundary action; an endpoint has a single owner and a single-producer, single-consumer transport, and concurrent use is rejected.
- `wouldBlock` returns control unchanged. Transport backpressure surfaces as `ErrWouldBlock` (the backpressure outcome owned by `iox`), makes no protocol progress, and preserves the endpoint and frontier; the wait, backoff, or drive-peer decision is the caller's.
- `errMore` is outside the session transport domain. `ErrMore` is not a session transport outcome; it preserves the endpoint and is the caller's to classify as a stream frontier (routed through `takt`'s `SubscriptionLoop`) or to fail dispatch — a multishot frontier is never a session carrier.
- Failure preserves the endpoint, and the asymmetry carries through. Any failure evidence leaves the endpoint preserved, a thrown error in a paired run aborts the session globally, and the `errMore`-to-`fail` asymmetry from the outcome plane holds unchanged.
- Close is a protocol transition, not a kernel close. `Close` advances the protocol frontier; it does not close the fd or stop the ring, and finishing a session service makes cancel/drain, release/recycle, and descriptor close visible.
- Duality, terminality, and parser state are caller checks. They are caller-owned, never boundary validation, and absent from the hot path; the session frontier and protocol carriers stay caller-owned and out of the boundary.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

owner(session_endpoint) = pkg("code.hybscloud.com/sess")
owner(protocol_frontier) = pkg("code.hybscloud.com/sess")
owner(session_close) = pkg("code.hybscloud.com/sess")
owner(session_transport_site) = pkg("code.hybscloud.com/sess")
owner(session_transport_backpressure_outcome) = pkg("code.hybscloud.com/iox")
owner(protocol_bridge) = pkg("code.hybscloud.com/sess")
owner(error_protocol_frontier) = pkg("code.hybscloud.com/sess")
owner(recursive_protocol_frontier) = pkg("code.hybscloud.com/sess")
owner(kernel_boundary_facts) = pkg("code.hybscloud.com/uring")

SessTheoryConcept =
  {session_frontier}
∀ c ∈ SessTheoryConcept.
  runtime_concept_use(pkg("code.hybscloud.com/sess"),c) ∈ RuntimeConceptUse
  ∧ use_pkg(runtime_concept_use(pkg("code.hybscloud.com/sess"),c))
      = pkg("code.hybscloud.com/sess")
  ∧ support(runtime_concept_use(pkg("code.hybscloud.com/sess"),c)) ⊆ C
  ∧ project(runtime_concept_use(pkg("code.hybscloud.com/sess"),c),B) = ∅

allow(C, import(pkg("code.hybscloud.com/sess")))
reject(B, import(pkg("code.hybscloud.com/sess")))

SessCarrier =
  {Endpoint, Send[T], Recv[T], Close, SelectL, SelectR, Offer,
   Reify, Reflect, Step, Advance, StepError, AdvanceError,
   Run, RunExpr, RunError, RunErrorExpr,
   Exec, ExecExpr, ExecError, ExecErrorExpr, Loop, ExprLoop}
ProtocolCarrier = {Eff[A], Expr[A], Either[E,A], Suspension[A]}
Endpoint = SessionState × Transport × Serial
Transport = bounded_spsc_queues(capacity=4)
Op = Send(v) ⊎ Recv ⊎ Close ⊎ SelectL ⊎ SelectR ⊎ Offer
current_suspension_sess(op) =
  ιs. s ∈ type_symbol(pkg("code.hybscloud.com/kont"),"Suspension[A]")
      ∧ pending_op(s)=op
  if ∃!s. s ∈ type_symbol(pkg("code.hybscloud.com/kont"),"Suspension[A]")
           ∧ pending_op(s)=op
E_sess = FailureEvidence
E_sess ∩ {err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock"),
          err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore")} = ∅
Unexpected_sess = {err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore")} ∪ E_sess
Unexpected_sess_outcome = { unexpected(e) | e ∈ Unexpected_sess }
SessTransportCtrl = {ok, wouldBlock}
q_sess_transport(nil) = ok
q_sess_transport(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock")) =
  wouldBlock
q_sess_transport(err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore")) =
  undefined
dom(q_sess_transport) =
  {nil, err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock")}
err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore")
  ∉ dom(q_sess_transport)

protocol_frontier(session) ∈ CallerFrontier
⟦Reify(Eff[A])⟧ = Expr[A]
⟦Reflect(Expr[A])⟧ = Eff[A]
⟦Reflect(Reify(p))⟧ ≃ p modulo representation_cost
support({Reify, Reflect}) ⊆ C
project({Reify, Reflect}, B) = ∅
BoundaryProtocolStep(step) ⇔
  defined(bind(step)) ∧ bind(step) ∈ boundary_action(B)
BoundaryProtocolStep(protocol_io_step) → one_boundary_action(bind(protocol_io_step))
single_owner(Endpoint)
single_producer_single_consumer(Transport)
concurrent_use(Endpoint) → reject

⟦Advance(Endpoint,Op)⟧ =
  advanced(Endpoint',Value)
  ⊎ would_block(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock"))
  ⊎ Unexpected_sess_outcome

would_block(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock"))
  → no_protocol_progress ∧ endpoint_preserved
would_block(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock"))
  → backpressure(Transport)
  ∧ owner(session_transport_site)=pkg("code.hybscloud.com/sess")
  ∧ owner(session_transport_backpressure_outcome)=pkg("code.hybscloud.com/iox")
  ∧ decision_owner({wait, backoff, drive_peer}) = C
unexpected(err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore"))
  → outside_session_transport_domain
    ∧ endpoint_preserved
    ∧ decision_owner({classify_as_stream_frontier, fail_dispatch}) = C
unexpected(e) ∧ e ∈ E_sess → endpoint_preserved
live_frontier(err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore"))
  ∉ ⟦Advance(Endpoint,Op)⟧
multishot_frontier ∉ SessCarrier
classify_as_stream_frontier(err_symbol(pkg("code.hybscloud.com/iox"),"ErrMore"))
  → route_via(SubscriptionLoop(pkg("code.hybscloud.com/takt")))
    ∧ StreamEvent_to_SessionObservation ∈ CallerPolicy

defined_at(shallow_effect_handler) = path("uring/agents/boundary/protocols.md")
advance_step = ⟦Advance(Endpoint,Op)⟧
handles_one_visible_frontier(advance_step)
Step(protocol) = done(value) ⊎ suspended(session_op)
Advance(Endpoint,suspended(session_op)) =
  advanced(Endpoint',value)
    ⊎ would_block(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock"))
    ⊎ unexpected(error)
Advance(Endpoint,suspended(session_op)) ∧ advanced(Endpoint',value)
  → consumes_one_suspension_frontier
would_block(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock")) →
  returns_control(advance_step,C) ∧ endpoint_preserved
{branch_choice, terminal_policy} ⊆ CallerPolicy → leaves_policy_in(Π_proto)

Exec(Endpoint,Eff[A]) = run_one_endpoint_until_terminal
ExecExpr(Endpoint,Expr[A]) = run_one_endpoint_until_terminal
Run(Eff[A],Eff[B]) = paired_endpoint_execution
RunExpr(Expr[A],Expr[B]) = paired_endpoint_execution
Loop_{S,A}(S, S -> Eff[Either[S,A]]) = recursive_protocol_carrier
ExprLoop_{S,A}(S, S -> Expr[Either[S,A]]) = recursive_protocol_carrier
recursive_protocol_carrier ∈ C
recursive_protocol_carrier ∉ B
Loop(pkg("code.hybscloud.com/sess")) ≠ Loop(pkg("code.hybscloud.com/takt"))
carrier_pkg(ExprLoop(pkg("code.hybscloud.com/sess"))) = pkg("code.hybscloud.com/sess")
carrier_pkg(ExprLoop(pkg("code.hybscloud.com/sess"))) ≠ pkg("code.hybscloud.com/takt")

StepError_{E,A}(Expr[A]) =
  done(Either[E,A]) ⊎ suspended(session_op_error)
AdvanceError_{E,A}(Endpoint,session_op_error) =
  advanced(Endpoint',Either[E,A])
  ⊎ would_block(err_symbol(pkg("code.hybscloud.com/iox"),"ErrWouldBlock"))
  ⊎ unexpected(error)
DispatchError(op) ∧ thrown(e:E)
  → discard(current_suspension_sess(op))
    ∧ return(Left_{E,A}(e))
    ∧ endpoint_preserved
    ∧ no_boundary_action(B)
ExecError_{E,A}(Endpoint,Eff[A]) = Either[E,A]
ExecErrorExpr_{E,A}(Endpoint,Expr[A]) = Either[E,A]
RunError_{E,A,B}(Eff[A],Eff[B]) =
  (Either[E,A], Either[E,B], thrown(E) ⊎ nil)
RunErrorExpr_{E,A,B}(Expr[A],Expr[B]) =
  (Either[E,A], Either[E,B], thrown(E) ⊎ nil)
thrown(e:E) ∈ result(RunErrorExpr_{E,A,B}) →
  global_session_abort(e)
  ∧ inspect(thrown(e)) precedes inspect(peer_Either)
  ∧ peer_Either may_be locally_unresolved
error_protocol_frontier ∈ C
error_protocol_frontier ∉ B

StreamEvent(pkg("code.hybscloud.com/takt")) ⇀ SessionObservation
StreamEvent_to_SessionObservation ∈ CallerPolicy
SessionObservation ∉ Endpoint
SessionObservation ∉ BoundaryHelper(B)

Close(session) = protocol_frontier_transition
Close(session) ↛ close(fd)
Close(session) ↛ stop(ring)
finish(session_service) →
  visible(cancel_or_drain)
  ∧ visible(release_or_recycle)
  ∧ visible(close_descriptors)

CallerCheck = {duality_check, terminality_check, parser_state}
∀ x ∈ CallerCheck. decision_owner(x) = C
∀ x ∈ CallerCheck. x ∉ BoundaryValidation
HotPath(Advance) → checks(Advance) ∩ CallerCheck = ∅

support(SessCarrier ∪ ProtocolCarrier ∪ CallerCheck ∪ {SessionObservation}) ⊆ C
support(kernel_boundary_facts) ⊆ B
project(SessCarrier, B) = ∅
reject(project({protocol_frontier, protocol_bridge, error_protocol_frontier,
                recursive_protocol_frontier,
                branch_choice, terminal_policy}
               ∪ CallerCheck, B))
```
