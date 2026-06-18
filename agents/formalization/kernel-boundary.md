# Kernel Boundary

This is the half of the world an agent does not control: what the kernel does, and which stratum owns which fact. To use `code.hybscloud.com/uring` correctly you must keep *kernel facts* (an SQE was submitted; a CQE arrived with these `Res` / `Flags` / `user_data`) strictly apart from *caller policy* (whether to retry, back off, or route). The two sections below give you (1) the boundary / below / caller stratum model and the per-package ownership of each carrier, and (2) the exact correspondence between an SQE action you encode and the CQE observation you later decode. If a change lets a kernel fact carry policy, or lets a borrowed CQE outlive its observation, it is wrong here before it is wrong anywhere else.

## Kernel Facts And Stratum Ownership

The block fixes the kernel boundary `K = below(module)`, the disjoint strata, and `CarrierOwnerPackages` — the packages allowed to own carriers used by the guide. Read it as the authority on *who owns what*: nothing the kernel reports is caller policy, nothing the caller decides is a kernel fact, and `code.hybscloud.com/cove` context or requirement evidence guards caller decisions rather than becoming a boundary projection.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
K = below(module)
Atom = countable set of syntactic atoms
Fact = finite subset of Atom
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)
BoundaryProjectableCarrier =
  { outcome_contract(pkg("code.hybscloud.com/iox"))
  , fd_authority(pkg("code.hybscloud.com/iofd"))
  , socket_authority(pkg("code.hybscloud.com/sock"))
  , buffer_authority(pkg("code.hybscloud.com/iobuf"))
  , spin_authority(pkg("code.hybscloud.com/spin"))
  , frame_boundary(pkg("code.hybscloud.com/framer"))
  }
BoundaryProjectableCarrier ⊆ Fact
CallerCarrier =
  { continuation_authority(pkg("code.hybscloud.com/kont"))
  , protocol_authority(pkg("code.hybscloud.com/sess"))
  , runner_authority(pkg("code.hybscloud.com/takt"))
  , context_evidence(pkg("code.hybscloud.com/cove"))
  , requirement_evidence(pkg("code.hybscloud.com/cove"))
  , safety_evidence(pkg("code.hybscloud.com/cove"))
  }
CallerCarrier ⊆ Fact
Carrier = BoundaryProjectableCarrier ∪ CallerCarrier
Carrier ⊆ Fact
BoundaryProjectableCarrier ∩ CallerCarrier = ∅
CarrierOwnerPackages = {pkg("code.hybscloud.com/iox"),
                        pkg("code.hybscloud.com/iofd"),
                        pkg("code.hybscloud.com/sock"),
                        pkg("code.hybscloud.com/iobuf"),
                        pkg("code.hybscloud.com/spin"),
                        pkg("code.hybscloud.com/framer"),
                        pkg("code.hybscloud.com/cove"),
                        pkg("code.hybscloud.com/kont"),
                        pkg("code.hybscloud.com/sess"),
                        pkg("code.hybscloud.com/takt")}
CarrierStratum = {carrier(p) | p ∈ CarrierOwnerPackages}
Strata = {B,A,K} ∪ CarrierStratum
stratum_owner : Fact ⇀ Strata
single_stratum_owner(f) ⇔
  f ∈ dom(stratum_owner) ∧ ∃! S ∈ Strata. stratum_owner(f)=S
BoundaryWitnessFact = {context_identity,SQE_facts,CQE_facts,field_witnesses,completion_facts}
BoundaryWitnessFact ⊆ Fact
∀ f ∈ BoundaryWitnessFact. stratum_owner(f)=B
BoundaryOwnerPackages = {module}
OwnerPackages = CarrierOwnerPackages ∪ BoundaryOwnerPackages ∪ {pkg("code.hybscloud.com/zcall")}
carrier_pkg : Carrier → CarrierOwnerPackages
pkg_owner : Fact ⇀ Pkg
package_owner : dom(pkg_owner) → Pkg
∀ fact ∈ dom(pkg_owner). package_owner(fact) = pkg_owner(fact)
∀ fact ∈ dom(pkg_owner). pkg_owner(fact) ∈ OwnerPackages
package_owned(fact) ⇔ fact ∈ dom(pkg_owner)
carrier_pkg(outcome_contract(pkg("code.hybscloud.com/iox")))        = pkg("code.hybscloud.com/iox")
carrier_pkg(fd_authority(pkg("code.hybscloud.com/iofd")))           = pkg("code.hybscloud.com/iofd")
carrier_pkg(socket_authority(pkg("code.hybscloud.com/sock")))       = pkg("code.hybscloud.com/sock")
carrier_pkg(buffer_authority(pkg("code.hybscloud.com/iobuf")))      = pkg("code.hybscloud.com/iobuf")
carrier_pkg(spin_authority(pkg("code.hybscloud.com/spin")))         = pkg("code.hybscloud.com/spin")
carrier_pkg(frame_boundary(pkg("code.hybscloud.com/framer")))       = pkg("code.hybscloud.com/framer")
carrier_pkg(context_evidence(pkg("code.hybscloud.com/cove")))       = pkg("code.hybscloud.com/cove")
carrier_pkg(requirement_evidence(pkg("code.hybscloud.com/cove")))   = pkg("code.hybscloud.com/cove")
carrier_pkg(safety_evidence(pkg("code.hybscloud.com/cove")))        = pkg("code.hybscloud.com/cove")
carrier_pkg(continuation_authority(pkg("code.hybscloud.com/kont"))) = pkg("code.hybscloud.com/kont")
carrier_pkg(protocol_authority(pkg("code.hybscloud.com/sess")))     = pkg("code.hybscloud.com/sess")
carrier_pkg(runner_authority(pkg("code.hybscloud.com/takt")))       = pkg("code.hybscloud.com/takt")

support(B) =
  {ring_setup,SQE_encode,CQE_observe,ownership,capability,boundary_lifecycle}
support(C) = CallerFrontier ∪ CallerPolicy
support(K) =
  {linux_io_uring_abi,zcall_result(pkg("code.hybscloud.com/zcall")),
   zcall_errno(pkg("code.hybscloud.com/zcall")),device_memory_order_reality}

KernelAxiom =
  pairwise_disjoint(support(B),support(C),support(K))
  ∧ ∀ f ∈ dom(stratum_owner). single_stratum_owner(f)
  ∧ ∀ c ∈ Carrier. stratum_owner(c)=carrier(carrier_pkg(c))
  ∧ ∀ c ∈ Carrier. ∃! p ∈ CarrierOwnerPackages. carrier_pkg(c)=p
  ∧ ∀ f ∈ dom(pkg_owner). package_owner(f)=pkg_owner(f)
  ∧ ∀ f ∈ dom(pkg_owner). pkg_owner(f) ∈ OwnerPackages

project(BoundaryProjectableCarrier,B) =
  {authority_projection,outcome_projection,frame_boundary_projection}
project(K,B) =
  {zcall_result_projection,zcall_errno_projection,ABI_fact_projection}

allowed_projection(X,B) ⇔ X ∈ {BoundaryProjectableCarrier,K}
reject(project(CallerCarrier,B))
reject(project(A,B))
∀ e. reject(project(context_evidence(pkg("code.hybscloud.com/cove"),e),B))
∀ e. reject(project(requirement_evidence(pkg("code.hybscloud.com/cove"),e),B))
∀ e. reject(project(safety_evidence(pkg("code.hybscloud.com/cove"),e),B))

policy_free(J) ⇔
  support(Σ(J)) ∩ support(B) = ∅
  ∧ support(Π(J)) ∩ support(B) = ∅

KernelWF(J) ⇔
  KernelAxiom
  ∧ WF(J)
  ∧ policy_free(J)
  ∧ ∀ f ∈ boundary_facts(J). single_stratum_owner(f)

denote_kernel_boundary(J,w) ⇔
  w ∈ World_U
  ∧ J ∈ JudgmentSurface
  ∧ KernelWF(J)
  ∧ defined(⟦J⟧^U_w)
```

## SQE And CQE Correspondence

Every operation is one encoded SQE action and, for one-shot operations, one decoded CQE observation. The rules below pin that one-to-one shape: how an action names its completion through `user_data`, what a CQE view exposes, and why a borrowed view must be copied before it escapes the observation that produced it. Get this correspondence wrong and everything the outcome plane decodes downstream is wrong too.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }

SQE = finite_record(fields,user_data,flags,opcode)
CQE = finite_record(Res,Flags,user_data)
Ctx = completion_identity
SQEVal = { sqe | sqe : SQE }
CQEVal = { cqe | cqe : CQE }
SQECQESurface = SQEVal ∪ CQEVal ∪ JudgmentSurface
⟦·⟧^{U,sqe}_w : SQECQESurface ⇀ Denotation_U(w)
OperationCarrier(r) ⇔
  r ∈ Resource
  ∧ resource_kind(r) ∈ {Ctx,IndirectSQE,ExtSQE,ListenerOp,
                        IncrementalReceiver,ZCRXReceiver,ZCTracker,
                        MultishotSubscription}
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

Encode : Request × Ctx → SQE
Encode(req,ctx).user_data = raw(ctx)
Encode(req,ctx).opcode = opcode(req)
Encode(req,ctx).fields = total_fields(req)

EncodingSound(req,ctx,sqe) ⇔
  sqe = Encode(req,ctx)
  ∧ decodable(sqe)
  ∧ no_policy_field(sqe)

Submit(req,ctx,r,Θ,Θ') ⇔
  EncodingSound(req,ctx,Encode(req,ctx))
  ∧ OperationCarrier(r)
  ∧ ctx ∈ Θ
  ∧ r ∈ Θ
  ∧ fresh(pending(ctx,r), Θ - {ctx,r})
  ∧ Θ' = Θ - {ctx,r} + {pending(ctx,r)}

Submitted(req,ctx,r) ⇔
  ∃ Θ Θ'. Submit(req,ctx,r,Θ,Θ')

CQEIdentity(cqe,ctx) ⇔ cqe.user_data = raw(ctx)
CQEProjection(cqe,ctx,r,χ) = CQEToObs(cqe,ctx,r,χ)
ReleaseObservationProjection(cqe,ctx,r,χ,H) = ReleaseProjection(cqe,ctx,r,χ,H)
ReleaseObservationProjection(cqe,ctx,r,χ) = ReleaseProjection(cqe,ctx,r,χ)
CQEFlagProjection(cqe) = flag_projection(cqe.Flags)

ProjectionSound(cqe,ctx,r,χ,H) ⇔
  CQEIdentity(cqe,ctx)
  ∧ OperationCarrier(r)
  ∧ ((operation_cqe(cqe)
      ∧ CQEProjection(cqe,ctx,r,χ) ∈ OutcomeProduct
      ∧ preserves(CQEProjection(cqe,ctx,r,χ), {cqe.Res,cqe.Flags,cqe.user_data}))
     ∨ (release_ready_carrier_at(cqe,ctx,r,H)
        ∧ ReleaseObservationProjection(cqe,ctx,r,χ,H) ∈ ReleaseObservation
        ∧ preserves(ReleaseObservationProjection(cqe,ctx,r,χ,H),
                    {cqe.Flags,cqe.user_data,release_frontier(ctx,r)}))
     ∨ (UnexpectedNotificationAt(cqe,ctx,r,H)
        ∧ NoOutcomeProjection(cqe,ctx,H) = none
        ∧ ¬defined(CQEProjection(cqe,ctx,r,χ))
        ∧ ¬defined(ReleaseObservationProjection(cqe,ctx,r,χ,H))))
  ∧ (flag(NOTIF) ∈ CQEFlagProjection(cqe) → notification_cqe(cqe))
ResultProjectionSound(cqe,ctx,out) ⇔
  result_resource(cqe,ctx,out)
  ∧ caller_owns(out)
  ∧ ¬OperationCarrier(out)

BorrowLaw(view) ⇔ borrowed(view) → lifetime(view) ⊆ current_cq_epoch
CopyLaw(copy) ⇔ copied(copy,{Res,Flags,user_data}) → stable(copy)

SQE_CQE_Roundtrip(req,ctx,r,Θ,Θ',cqe,χ,H) ⇔
  Submit(req,ctx,r,Θ,Θ')
  ∧ CQEIdentity(cqe,ctx)
  ∧ ProjectionSound(cqe,ctx,r,χ,H)
  ∧ ∀ out. result_resource(cqe,ctx,out) → ResultProjectionSound(cqe,ctx,out)
  ∧ ∀ w ∈ World_U.
      defined(⟦Encode(req,ctx)⟧^{U,sqe}_w)
      ∧ defined(⟦cqe⟧^{U,sqe}_w)
```
