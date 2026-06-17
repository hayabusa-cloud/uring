# SQE CQE Boundary

Every operation you run is one SQE you encode and submit, and — for a one-shot operation — one CQE you later observe and decode. This file pins that correspondence so it cannot drift: an SQE names its completion through `user_data = raw(ctx)`, carries no caller policy in its fields, and the CQE that comes back decodes to exactly one outcome by the sign of `Res` and the `MORE` flag. It also fixes the two rules behind the most subtle bugs: a borrowed CQE view lives only for the current completion-queue epoch and must be copied before it is stored, and the encode/submit/observe/decode actions are hot-path and must not allocate, reflect, or do route or parser work.

In plain terms, the rules an agent follows here are:

- Encode completely, and identify by context. Fill every SQE field before submit, set the SQE's `user_data` to the raw `ctx` value that names its completion, and keep all caller policy out of the SQE — no retry, route lookup, parser state, or scheduler choice belongs in a field.
- Submit only what you own, then mark it pending. Submitting requires that both the `ctx` and its operation carrier are owned and not already pending; afterwards they move into the pending epoch.
- Observe only a pending operation, and only by matching identity. A CQE is observed against a pending context and carrier whose `user_data` matches it, and observation records `Res`, `Flags`, and `user_data` as facts.
- Decode each operation CQE to exactly one outcome from two bits. The sign of `Res` and the presence of the `MORE` flag pick one of four cases: non-negative without `MORE` is a terminal success; non-negative with `MORE` is a live success on a non-terminal frontier; negative without `MORE` is a terminal failure; negative with `MORE` is a non-terminal failure. A failure never turns into `errMore` or byte progress.
- Decode release separately from outcome. Zero-copy notification CQEs, and the terminal no-notification fallback, decode through a release observation that has no control value; an early notification that arrives before the data frontier yields neither an outcome nor a release, and is instead recorded in history and waited on.
- Return result resources to the caller. A returned descriptor or selected buffer is owned by the caller after decode, and is not itself an operation carrier.
- Copy before you keep. A borrowed CQE view lives only for the current completion-queue epoch; copy `Res`, `Flags`, `user_data`, and any needed metadata before storing anything that must outlive the reap loop, and never store the borrowed view itself.
- Keep the hot path clean. Encode, submit, wait, observe, and decode must not allocate, reflect, box, build closures, spawn workers, or do route or parser work, and caller frontier and policy never live inside the boundary.

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

J_sqe_cqe =
  Γ_sc | Θ_sc ⊢ sqe_cqe_action @ ring : result | Δ_sc ▷ Θ_sc'
    ; χ_sc ; Σ_sc ; Μ_sc ; Φ_sc ; Obs_sc ; Rel_sc ; Π_sc
Obs_sc ∈ OutcomePlane
Rel_sc ∈ ReleasePlane
denote_sqe_cqe_boundary(w) ⇔
  w ∈ World_U
  ∧ J_sqe_cqe ∈ JudgmentSurface
  ∧ defined(⟦J_sqe_cqe⟧^U_w)

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
fresh(x,S) ⇔ x ∉ S
result_resource(cqe,ctx,out) ⇔
  ResultResource(out)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource(out)
      ∧ defined(fd_of(out))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(out)))
     ∨ selected_buffer_result(cqe,out)
     ∨ explicit_result_resource(cqe,out))

SQEAction =
  { encode_sqe, submit_sqe, wait_cqe, peek_cqe, observe_cqe, decode_cqe }
sqe_cqe_action ∈ SQEAction

Encode(sqe,req,ctx) ⇔
  total(sqe.fields)
  ∧ sqe.user_data = raw(ctx)
  ∧ fields(sqe) = encode_fields(req)
  ∧ no_field_policy(sqe,{retry,route_lookup,parser_state,scheduler})

Submit(sqe,ctx,r) ⇔
  Encode(sqe,req,ctx)
  ∧ OperationCarrier(r)
  ∧ owned(ctx)
  ∧ owned(r)
  ∧ fresh(pending(ctx,r), Θ_sc - {ctx,r})
  ∧ Θ_sc' = Θ_sc - {ctx,r} + {pending(ctx,r)}

Observe(cqe,ctx,r) ⇔
  pending(ctx,r)
  ∧ emitted(cqe)
  ∧ cqe.user_data = raw(ctx)
  ∧ Μ_sc ⊇ {identity(ctx), cqe.Res, cqe.Flags, cqe.user_data}

flag_projection(raw_flags) =
  flags(raw_flags)
  ∪ {flag(MORE) | has_MORE(raw_flags)}
  ∪ {flag(NOTIF) | has_NOTIF(raw_flags)}

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

Decode(cqe,ctx,r,Obs_sc) ⇔
  Observe(cqe,ctx,r)
  ∧ OperationCarrier(r)
  ∧ ((operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
        ∧ Obs_sc = obs(ev(decode_result(op_of(ctx),cqe.Res), flag_projection(cqe.Flags), identity(ctx),
                          terminal_frontier(ctx,r), capability(χ_sc), boundary_state(none)), ok))
     ∨ (operation_cqe(cqe) ∧ cqe.Res ≥ 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
        ∧ Obs_sc = obs(ev(decode_result(op_of(ctx),cqe.Res), flag_projection(cqe.Flags), identity(ctx),
                          nonterminal_frontier(ctx,r), capability(χ_sc), boundary_state(none)), ok))
     ∨ (operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
        ∧ Obs_sc = obs(ev(typed_error(-cqe.Res), flag_projection(cqe.Flags), identity(ctx),
                          terminal_frontier_after_failure(ctx,r), capability(χ_sc), boundary_state(none)),
                       fail(typed_error(-cqe.Res))))
     ∨ (operation_cqe(cqe) ∧ cqe.Res < 0 ∧ flag(MORE) ∈ flag_projection(cqe.Flags)
        ∧ Obs_sc = obs(ev(typed_error(-cqe.Res), flag_projection(cqe.Flags), identity(ctx),
                          nonterminal_frontier_after_failure(ctx,r), capability(χ_sc), boundary_state(none)),
                       fail(typed_error(-cqe.Res)))))

DecodeRelease(cqe,ctx,r,Rel_sc) ⇔
  Observe(cqe,ctx,r)
  ∧ OperationCarrier(r)
  ∧ release_ready_carrier_at(cqe,ctx,r,history(ctx))
  ∧ Rel_sc = release_obs(ev(empty, release_flags(cqe,ctx,history(ctx)), identity(ctx),
                            release_frontier(ctx,r), capability(χ_sc), boundary_state(none)))
EarlyNotification(cqe,ctx,r) ⇔
  Observe(cqe,ctx,r)
  ∧ zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
  ∧ early_notification_cqe(cqe,ctx,history(ctx))

Decode(cqe,ctx,r,Obs_sc) → Obs_sc ∈ OutcomeProduct
DecodeRelease(cqe,ctx,r,Rel_sc) → Rel_sc ∈ ReleaseObservation
DecodeRelease(cqe,ctx,r,Rel_sc) → ¬defined(ctrl(Rel_sc))
EarlyNotification(cqe,ctx,r) → ¬∃ Obs_any. Decode(cqe,ctx,r,Obs_any)
EarlyNotification(cqe,ctx,r) → ¬∃ Rel_any. DecodeRelease(cqe,ctx,r,Rel_any)
EarlyNotification(cqe,ctx,r) → Obs_sc = none ∧ Rel_sc = none

DecodeResult(cqe,ctx,out) ⇔
  result_resource(cqe,ctx,out)
  ∧ caller_owns(out)
  ∧ ¬OperationCarrier(out)

BorrowedCQE(view) ⇔ borrowed(view) ∧ lifetime(view) ⊆ current_cq_epoch
CopiedCQE(c) ⇔ copied({Res,Flags,user_data},c) ∧ stable(c)
store_allowed(x) ⇔ CopiedCQE(x)
reject(store(BorrowedCQE(view)))

HotPath =
  { encode_sqe, submit_sqe, wait_cqe, observe_cqe, decode_cqe }
hot_path(a) ⇔ a ∈ HotPath
hot_path(a) →
  effects(a) ∩ {alloc,reflect,box,closure,worker_spawn,
                 route_lookup,parser_branch_policy} = ∅

support(Π_sc) ⊆ CallerPolicy
support(Σ_sc) ⊆ CallerFrontier
support(Π_sc) ∩ support(B) = ∅
support(Σ_sc) ∩ support(B) = ∅
```
