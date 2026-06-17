# Outcomes

A completion tells you two independent things, and conflating them is the most common io_uring mistake. This file holds them apart. The *outcome plane* carries the control result of an operation — exactly one of `ok`, `wouldBlock`, `errMore`, or `fail(e)`, drawn from `Ctrl[E]` and kept distinct so the cases never blur: `wouldBlock` is not terminal, and `errMore` carries no byte progress in the control value. The *release plane* carries the separate fact that a buffer or zero-copy window may now be released, and it deliberately has no control value at all. The two planes meet only at `none`.

When you decode a CQE you read its evidence — `Res`, flags, identity, frontier, capability, boundary state — and map its iox control symbol through `q_iox` into exactly one outcome. Get this wrong and every downstream decision (retry, release, route) is built on a category error.

In plain terms, the rules an agent follows here are:

- Decode to exactly one control outcome. Map the iox control symbol through `q_iox`: `nil → ok`, `ErrWouldBlock → wouldBlock`, `ErrMore → errMore`, and any failure evidence `e → fail(e)`; those four are the whole control plane.
- Read evidence before control. The evidence `Ev` (result, flags, identity, frontier, capability, boundary state) is what you inspect; the control symbol is decided from the sign of `Res` and the `MORE` flag.
- Keep the outcome and release planes apart. A release observation carries evidence but no control value, the two planes share only `none`, and an early notification arriving before the data frontier projects to `none` — neither an outcome nor a release.
- Respect the asymmetries. `errMore` is never byte progress, `wouldBlock` is never terminal, a failure may still carry the `MORE` flag, and the `MORE` flag is never by itself `errMore`.

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
ℕ = {0,1,2,...}

E = FailureEvidence
Ctrl[E] = {ok, wouldBlock, errMore} ∪ {fail(e) | e ∈ E}
IoxErrorName = {"ErrWouldBlock", "ErrMore"}
∀ name ∈ IoxErrorName. iox_err(name) = err_symbol(pkg("code.hybscloud.com/iox"),name)
IoxControlSymbol = {nil} ∪ {iox_err(name) | name ∈ IoxErrorName}
q_iox : IoxControlSymbol ∪ E → Ctrl[E]
E ∩ IoxControlSymbol = ∅
Ev =
  ResultEvidence
  × flag_evidence
  × identity_evidence
  × resource_frontier_evidence
  × capability_evidence
  × BoundaryStateEvidence
ev(re,fl,id,rf,cap,bs) ∈ Ev ⇔
  re ∈ ResultEvidence
  ∧ fl ∈ flag_evidence
  ∧ id ∈ identity_evidence
  ∧ rf ∈ resource_frontier_evidence
  ∧ cap ∈ capability_evidence
  ∧ bs ∈ BoundaryStateEvidence
π_result(ev(re,fl,id,rf,cap,bs)) = re
π_flags(ev(re,fl,id,rf,cap,bs)) = fl
π_identity(ev(re,fl,id,rf,cap,bs)) = id
π_frontier(ev(re,fl,id,rf,cap,bs)) = rf
π_capability(ev(re,fl,id,rf,cap,bs)) = cap
π_boundary_state(ev(re,fl,id,rf,cap,bs)) = bs
Obs[Ev,E] = Ev × Ctrl[E]
OutcomeProduct = Obs[Ev,E]
OutcomePlane = OutcomeProduct ∪ {none}
ReleaseObservation = {release_obs(ev) | ev ∈ Ev}
ReleasePlane = ReleaseObservation ∪ {none}
BoundaryObservation = OutcomePlane ∪ ReleasePlane
none ∉ OutcomeProduct ∪ ReleaseObservation
disjoint(OutcomeProduct,ReleaseObservation)
OutcomePlane ∩ ReleasePlane = {none}
denote_boundary_observation(X,w) ⇔
  w ∈ World_U
  ∧ X ∈ BoundaryObservation
  ∧ ∃ J ∈ JudgmentSurface.
      defined(⟦J⟧^U_w)
      ∧ X ∈ {Obs(J),Rel(J),none}
obs(ev,o) ∈ OutcomeProduct ⇔ ev ∈ Ev ∧ o ∈ Ctrl[E]
release_obs(ev) ∈ ReleaseObservation ⇔ ev ∈ Ev
none ∈ OutcomePlane
OutcomeProduct ⊂ OutcomePlane
π_Ev(obs(ev,o)) = ev
π_Ev(release_obs(ev)) = ev
ctrl : OutcomeProduct ⇀ Ctrl[E]
ctrl(obs(ev,o)) = o
¬defined(ctrl(release_obs(ev)))
¬defined(ctrl(none))
ev_of(X) = π_Ev(X) if X ∈ OutcomeProduct ∪ ReleaseObservation
flag_set(X) = π_flags(ev_of(X)) if X ∈ OutcomeProduct ∪ ReleaseObservation
flag_set(none) = ∅
frontier_set(X) = {π_frontier(ev_of(X))} if X ∈ OutcomeProduct ∪ ReleaseObservation
frontier_set(none) = ∅
identity_set(X) = {π_identity(ev_of(X))} if X ∈ OutcomeProduct ∪ ReleaseObservation
identity_set(none) = ∅
has(a,X) ⇔ a ∈ flag_set(X)
frontier_fact(X,F) ⇔ F ∈ frontier_set(X)
NameCarrier = {x | finite(NameAtoms(x))}
NameIncidence ⊆ NameCarrier × NameCarrier
names ⊆ NameCarrier × NameCarrier
∀ x,y ∈ NameCarrier. y ∉ BoundaryObservation → (names(x,y) ⇔ (x,y) ∈ NameIncidence)
∀ X ∈ BoundaryObservation. ∀ args ∈ NameCarrier.
  names(args,X) ⇔
    NameAtoms(args) ⊆ NameAtoms(identity_set(X) ∪ frontier_set(X))
∀ n ≥ 1. X ∈ BoundaryObservation →
  names(x1,...,xn,X) = names(frontier_tuple(x1,...,xn),X)

q_iox(nil) = ok
q_iox(iox_err("ErrWouldBlock")) = wouldBlock
q_iox(iox_err("ErrMore")) = errMore
∀ e ∈ E. q_iox(e) = fail(e)

CQEFlag = finite symbolic CQE flag names
{MORE,NOTIF} ⊆ CQEFlag
RawFlags = ℕ
raw_flags_none = 0
RawFlagAtom = {raw_flags(n) | n ∈ RawFlags ∧ n ≠ raw_flags_none}
FlagAtom = RawFlagAtom ∪ {flag(f) | f ∈ CQEFlag}
flag_evidence = {S | finite(S) ∧ S ⊆ FlagAtom}
flags : RawFlags → flag_evidence
flags(raw_flags_none) = ∅
∀ n ∈ RawFlags. n ≠ raw_flags_none → flags(n) = {raw_flags(n)}
flag_projection(raw_flags) =
  flags(raw_flags)
  ∪ {flag(MORE) | has_MORE(raw_flags)}
  ∪ {flag(NOTIF) | has_NOTIF(raw_flags)}

OperationCarrier(r) ⇔
  r ∈ Resource
  ∧ resource_kind(r) ∈ {Ctx,IndirectSQE,ExtSQE,ListenerOp,
                        IncrementalReceiver,ZCRXReceiver,ZCTracker,
                        MultishotSubscription}
ZeroCopyReleaseCarrierKind = {ExtSQE}
ZeroCopySendOperation = {op | zero_copy_send_opcode(op)}
zero_copy_send_ctx(ctx) ⇔ op_of(ctx) ∈ ZeroCopySendOperation
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

decode_result(op,n) =
  typed_error(-n) if n < 0
  count(n) if n ≥ 0 ∧ byte_count_result(op)
  fd_result(fd(n)) if n ≥ 0 ∧ fd_result_opcode(op)
  status(n) if n ≥ 0 ∧ status_result_opcode(op)
  raw_result(op,n) if n ≥ 0
                    ∧ ¬byte_count_result(op)
                    ∧ ¬fd_result_opcode(op)
                    ∧ ¬status_result_opcode(op)

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
zero_copy_release_carrier(ctx,r) ⇔
  names(ctx,r)
  ∧ OperationCarrier(r)
  ∧ resource_kind(r) ∈ ZeroCopyReleaseCarrierKind
terminal_fallback_carrier(ctx,r) ⇔
  zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
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
early_notification_cqe(cqe,ctx,H) ⇔
  notification_cqe(cqe)
  ∧ cqe.user_data = raw(ctx)
  ∧ ¬data_frontier_seen(ctx,H)
flag(f) ∈ release_flags(cqe,ctx,H) ⇔
  flag(f) ∈ flag_projection(cqe.Flags)
  ∨ (f = NOTIF ∧ early_notification_seen(ctx,H))
notification_release_ready_cqe(cqe,ctx,H) →
  flag(NOTIF) ∈ release_flags(cqe,ctx,H)
terminal_no_notification_cqe(cqe,ctx,r,H) →
  ¬(flag(NOTIF) ∈ release_flags(cqe,ctx,H))
release_ready_carrier(cqe,ctx,r) ⇔
  release_ready_carrier_at(cqe,ctx,r,history(ctx))

frontier_from : CQE × Ctx × Resource ⇀ Frontier
CQEToObs(cqe,ctx,r,χ) =
  obs(ev(decode_result(op_of(ctx),cqe.Res),
         flag_projection(cqe.Flags),
         identity(ctx),
         frontier_from(cqe,ctx,r),
         capability(χ),
         boundary_state(none)),
      ctrl_from(cqe))
defined(CQEToObs(cqe,ctx,r,χ)) ⇔
  operation_cqe(cqe)
  ∧ defined(frontier_from(cqe,ctx,r))
defined(NoOutcomeProjection(cqe,ctx,H)) ⇔
  early_notification_cqe(cqe,ctx,H)
NoOutcomeProjection(cqe,ctx,H) = none
  if early_notification_cqe(cqe,ctx,H)

notification_frontier_from(cqe,ctx,r,H) = release_frontier(ctx,r)
  if release_ready_carrier_at(cqe,ctx,r,H)
defined(notification_frontier_from(cqe,ctx,r,H)) ⇔ release_ready_carrier_at(cqe,ctx,r,H)

ReleaseProjection(cqe,ctx,r,χ,H) =
  release_obs(ev(empty,
                 release_flags(cqe,ctx,H),
                 identity(ctx),
                 notification_frontier_from(cqe,ctx,r,H),
                 capability(χ),
                 boundary_state(none)))
defined(ReleaseProjection(cqe,ctx,r,χ,H)) ⇔
  release_ready_carrier_at(cqe,ctx,r,H)
ReleaseProjection(cqe,ctx,r,χ) =
  ReleaseProjection(cqe,ctx,r,χ,history(ctx))
defined(ReleaseProjection(cqe,ctx,r,χ)) ⇔
  release_ready_carrier(cqe,ctx,r)
UnexpectedNotificationAt(cqe,ctx,r,H) ⇔
  early_notification_cqe(cqe,ctx,H)
  ∧ zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
UnexpectedNotification(cqe,ctx,r) ⇔
  UnexpectedNotificationAt(cqe,ctx,r,history(ctx))

ctrl_from(cqe) =
  ok if operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  ok if operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
  fail(typed_error(-cqe.Res)) if operation_cqe(cqe) ∧ cqe.Res < 0

frontier_from(cqe,ctx,r) =
  terminal_frontier(ctx,r) if operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  nonterminal_frontier(ctx,r) if operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
  terminal_frontier_after_failure(ctx,r)
    if operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  nonterminal_frontier_after_failure(ctx,r)
    if operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
defined(frontier_from(cqe,ctx,r)) →
  OperationCarrier(r) ∧ operation_cqe(cqe)
UnexpectedNotificationAt(cqe,ctx,r,H) → ¬defined(ReleaseProjection(cqe,ctx,r,χ,H))
UnexpectedNotification(cqe,ctx,r) → ¬defined(ReleaseProjection(cqe,ctx,r,χ))
early_notification_cqe(cqe,ctx,H) →
  NoOutcomeProjection(cqe,ctx,H) = none
  ∧ ¬defined(CQEToObs(cqe,ctx,r,χ))
  ∧ ¬defined(ctrl(NoOutcomeProjection(cqe,ctx,H)))
result_resource(cqe,ctx,out) → ¬OperationCarrier(out)

terminal_completion(ctx,r,X) ⇔
  X ∈ BoundaryObservation
  ∧ (frontier_fact(X, terminal_frontier(ctx,r))
     ∨ frontier_fact(X, terminal_frontier_after_failure(ctx,r)))

byte_progress(n) ⇔ n > 0 ∧ ∃ op. decode_result(op,n) = count(n)
ByteProgress ⇔ ∃ n. n > 0 ∧ byte_progress(n)
∀ Obs ∈ OutcomeProduct.
  (ctrl(Obs) = errMore) ↛ ByteProgress
∀ Obs ∈ OutcomeProduct.
  (ctrl(Obs) = wouldBlock) ↛ terminal_completion(ctx,r,Obs)
∀ Obs ∈ OutcomeProduct. ∀ e ∈ E.
  (ctrl(Obs) = fail(e)) ↛ ¬has(flag(MORE),Obs)
∀ Obs ∈ OutcomeProduct.
  has(flag(MORE),Obs) ↛ (ctrl(Obs) = errMore)
symbolic_flags(X) = {flag(f) | f ∈ CQEFlag ∧ has(flag(f),X)}
flag_only_NOTIF(X) ⇔ X ∈ ReleaseObservation ∧ symbolic_flags(X) = {flag(NOTIF)}
flag_only_NOTIF(X) → ¬defined(ctrl(X))
notification_cqe(cqe) ↛ defined(ctrl_from(cqe))
```
