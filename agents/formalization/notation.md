# Notation

This is the notation an agent reasons in when changing or reviewing `code.hybscloud.com/uring`. To use `code.hybscloud.com/uring` correctly you must read three things precisely: the *judgment* `J` that records what a boundary action does, the *syntactic categories* that name every carrier the kernel and the caller can touch, and the *typing discipline* that decides which carriers are linear (used once) and which evidence must be copied rather than borrowed. None of this is ceremony — every later guarantee (outcomes, ownership, sessions, the shallow handler) is stated against these symbols, so an edit that breaks the notation silently breaks the obligations that depend on it.

Keep the judgment shape in view as you read; every other topic file projects onto one of its slots:

```text
J = Γ | Θ ⊢ action @ ring : result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π
```

Read the syntactic categories first, then the typing rules.

## Notation And Syntactic Categories

The block below fixes the module strata (boundary `B`, caller `C = A ∪ D`, below `K`), the Go surface and judgment surface, the twenty-one theory concepts an agent may invoke, and every resource / evidence / witness / frontier / lifecycle category. When a change cites a carrier, that carrier must be one named here — there is no "other" category.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
K = below(module)

ASCIIAlpha = { c | "A" ≤ c ≤ "Z" ∨ "a" ≤ c ≤ "z" }
ASCIIDigit = { c | "0" ≤ c ≤ "9" }
ModuleChar = ASCIIAlpha ∪ ASCIIDigit ∪ {"_", "-"}
ModuleSegment ::= ModuleChar⁺
ModuleName ::= ModuleSegment ("/" ModuleSegment)*
Pkg ::= pkg("code.hybscloud.com/" ++ ModuleName)
Stratum ::= boundary(Pkg) | above(Pkg) | beyond(Pkg) | caller(Pkg)
          | below(Pkg) | carrier(Pkg)
caller(Pkg) = above(Pkg) ∪ beyond(Pkg)
CallerStratum(Pkg) = {above(Pkg), beyond(Pkg)}
SymbolName ::= ModuleChar⁺
PkgSymbol = {pkg_symbol(p,name) | p ∈ Pkg ∧ name ∈ SymbolName}
type_symbol(p,name)  = pkg_symbol(p,name)
value_symbol(p,name) = pkg_symbol(p,name)
func_symbol(p,name)  = pkg_symbol(p,name)
err_symbol(p,name)   = pkg_symbol(p,name)
TheoryConcept ::= value_structure | type_structure | abstraction | compilation
                | denotation | logical_relation | Hoare_projection
                | verification_evidence | algebraic_effect | handler
                | shallow_handler | resumption | coeffect_requirement
                | modal_world | modal_later | coalgebraic_observation
                | session_frontier | linear_resource | temporal_obligation
                | outcome_quotient | composability
ConceptPlane ::= Γ | Δ | Θ | χ | Σ | Μ | Φ | Obs | Rel | Π
ConceptRoute = finite_map(TheoryConcept, finite_set(ConceptPlane))
ConceptOwner : TheoryConcept ⇀ Pkg
TheoryConceptUsed : JudgmentSurface → finite_set(TheoryConcept)
ConceptUse = {concept_use(p,c) | p ∈ Pkg ∧ c ∈ TheoryConcept}
RuntimeConceptUse = {runtime_concept_use(p,c) | p ∈ Pkg ∧ c ∈ TheoryConcept}
GuideObligation ::= name_public_carrier | name_public_operation
                  | route_to_plane | read_owner_package | keep_in_caller_layer
                  | keep_out_of_boundary | preserve_outcome_partition
                  | preserve_release_partition | preserve_ownership_epoch
                  | preserve_denotation | preserve_roundtrip
                  | preserve_composability | prove_composability
                  | verify_evidence_record | enforce_affine_resource
                  | enforce_temporal_frontier | enforce_stream_route
                  | enforce_context_requirement | enforce_session_frontier
                  | enforce_handler_shallow
ConceptObligation = finite_map(TheoryConcept, finite_set(GuideObligation))
obligation_named : JudgmentSurface × GuideObligation → Bool
obligation_routes_to_guide_check : GuideObligation → Bool
use_pkg(concept_use(p,c)) = p
use_pkg(runtime_concept_use(p,c)) = p
use_concept(concept_use(p,c)) = c
use_concept(runtime_concept_use(p,c)) = c
projection_defined : JudgmentSurface × ConceptPlane → Bool
Atom = countable set of syntactic atoms
ℕ = {0,1,2,...}
FiniteTuple(X) = ⋃_{n∈ℕ} X^n
Formula = { P | well_formed_meta_formula(P) }
∀ P,Q ∈ Formula. (P ↛ Q) ⇔ ¬(P → Q)

GoSurface =
  { g | public_go_term_caller(module, g) }
boundary_go : GoSurface ⇀ GoSurface
caller_go    : GoSurface ⇀ GoSurface
support(go_surface) =
  support(boundary_go(go_surface)) ∪ support(caller_go(go_surface))
BoundaryClean(go_surface) ⇔
  go_surface ∈ GoSurface
  ∧ support(boundary_go(go_surface)) ∩ (CallerFrontier ∪ CallerPolicy) = ∅
  ∧ support(caller_go(go_surface)) ⊆ CallerFrontier ∪ CallerPolicy
  ∧ ¬hidden(CallerFrontier ∪ CallerPolicy, support(go_surface))
JudgmentSurface =
  { J | WF(J)
        ∧ Obs(J) ∈ OutcomePlane
        ∧ Rel(J) ∈ ReleasePlane
        ∧ support(Σ(J)) ⊆ CallerFrontier
        ∧ support(Π(J)) ⊆ CallerPolicy }
World_U = { w | world_for(module, w) }
Denotation_U(w) =
  { d | denotation_at(module, w, d) }

⟦·⟧⁻¹_U : GoSurface ⇀ JudgmentSurface
⟦·⟧_U   : JudgmentSurface ⇀ GoSurface
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

Judgment ::=
  Γ | Θ ⊢ Action @ Ring : Result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π

J = Γ | Θ ⊢ Action @ Ring : Result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π ⇒
Γ(J) = Γ
Θ(J) = Θ
Action(J) = Action
Ring(J) = Ring
Result(J) = Result
action(J) = Action(J)
ring(J) = Ring(J)
result(J) = Result(J)
Δ(J) = Δ
Θ'(J) = Θ'
χ(J) = χ
Σ(J) = Σ
Μ(J) = Μ
Φ(J) = Φ
Obs(J) = Obs
Rel(J) = Rel
Π(J) = Π

Action ::= setup | register | unregister | encode | submit | wait | observe
         | decode | cancel | resize | provide | recycle | release | close | stop
Ring ::= ring_handle | direct_ring_handle
Result ::= count(n) | fd_result(fd) | status(n) | raw_result(op,n) | typed_error(errno)

ResourceKind ::= Ring | FD | DirectFD | Buf | FixedBuf | ProvidedBuf
               | Ctx | IndirectSQE | ExtSQE | CQEView | CQECopy
               | ListenerOp | IncrementalReceiver | ZCRXReceiver
               | MultishotSubscription | ZCTracker
EvidenceKind ::= SocketAddr | ContextEvidence | RequirementEvidence
               | SafetyEvidence | SpinEvidence | FrameBoundaryEvidence
WitnessKind ::= CompletionFact | ContextIdentity | SQEFact | CQEFact | FieldWitness
FrontierKind ::= pending | nonterminal | terminal | release | after_cqe
               | after_failure | pending_after_failure
               | nonterminal_after_failure | terminal_after_failure
Frontier ::= FrontierKind | frontier(FrontierKind, FiniteTuple(Atom))
frontier_kind : Frontier → FrontierKind
∀ k ∈ FrontierKind. frontier_kind(k) = k
∀ k ∈ FrontierKind. ∀ args ∈ FiniteTuple(Atom). frontier_kind(frontier(k,args)) = k
FrontierAtom = {protocol_frontier, stream_frontier, route_frontier,
                subscription_frontier, suspension_frontier, runner_movement}
CallerFrontier = FrontierAtom
LifecycleKind ::= released | recycled | closed | stopped
LifecycleToken ::= lifecycle(LifecycleKind, FiniteTuple(Atom))
lifecycle_kind : LifecycleToken → LifecycleKind
∀ k ∈ LifecycleKind. ∀ args ∈ FiniteTuple(Atom). lifecycle_kind(lifecycle(k,args)) = k
released_token(args) = lifecycle(released,args)
recycled_token(args) = lifecycle(recycled,args)
closed_token(args) = lifecycle(closed,args)
stopped_token(args) = lifecycle(stopped,args)
DenotedKind ::= ResourceKind | EvidenceKind | FrontierKind | LifecycleKind
TermName = finite subset of Atom
ResourceName = TermName
EvidenceName = TermName
WitnessName = TermName
Resource = {res(k,n) | k ∈ ResourceKind ∧ n ∈ ResourceName}
resource_kind : Resource → ResourceKind
resource_kind(res(k,n)) = k
Evidence = {evd(k,n) | k ∈ EvidenceKind ∧ n ∈ EvidenceName}
evidence_kind : Evidence → EvidenceKind
evidence_kind(evd(k,n)) = k
Witness = {wit(k,n) | k ∈ WitnessKind ∧ n ∈ WitnessName}
witness_kind : Witness → WitnessKind
witness_kind(wit(k,n)) = k
BackoffPolicy =
  {backoff_policy(bo) | bo : type_symbol(pkg("code.hybscloud.com/iox"),"Backoff")}
MailboxPolicy =
  {mailbox_policy(q) | q : type_symbol(pkg("code.hybscloud.com/lfq"),"Queue")}
CompletionMemoryPolicy =
  {completion_memory_policy(m) |
     m : type_symbol(pkg("code.hybscloud.com/takt"),"CompletionMemory")}
SpinWaitPolicy =
  {spin_wait_policy(sw) | sw : type_symbol(pkg("code.hybscloud.com/spin"),"Wait")}
ProtocolPolicy =
  {parser_state(p), parser_branch_policy(p), route_lookup(p), route_retirement(p)
     | p ∈ TermName}
BufferPolicy =
  {buffer_wait_policy(buf), buffer_recycle_policy(buf) | buf ∈ ResourceName}
PolicyAtom =
  { retry, poll_cadence, parking, scheduler, route_lookup,
    route_retirement, completion_routing, runner_policy, parser_state,
    parser_branch_policy, safety_policy, callback_policy, service_lifecycle,
    completion_pool_policy, completion_gc_root_policy, metadata_route,
    mailbox_handoff, lifecycle_policy, buffer_clear_policy,
    retirement_policy, resubscribe, cancel_timing, completion_buffer_policy,
    branch_choice, terminal_policy }
DecisionAtom =
  { wait, backoff, drive_peer, reroute, abandon, suppress, close, park,
    classify_as_stream_frontier, fail_dispatch, cancel_backend, close_backend,
    ignore_stale_completion, next_observation, discard }
CallerPolicy =
  BackoffPolicy ∪ MailboxPolicy ∪ CompletionMemoryPolicy ∪ SpinWaitPolicy ∪
  ProtocolPolicy ∪ BufferPolicy ∪ PolicyAtom
CallerDecision = CallerPolicy ∪ DecisionAtom
Policy ::= p where p ∈ CallerPolicy
Fact = finite subset of Atom
State = finite subset of Atom indexed by caller-owned policy state names
Owner ::= Pkg | Stratum
FrontierPlane ::= finite set over CallerFrontier
PolicyPlane ::= finite set over CallerPolicy
DecisionPlane ::= finite set over CallerDecision
pkg_owner : Fact ⇀ Pkg
package_owner : dom(pkg_owner) → Pkg
resource_owner : Resource ⇀ Owner
evidence_owner : Evidence ⇀ Pkg
frontier_owner : CallerFrontier ∪ FrontierPlane ⇀ {C}
policy_owner : CallerPolicy ∪ PolicyPlane ⇀ {C}
decision_owner : CallerDecision ∪ DecisionPlane ⇀ {C}
caller_state_owner : State ⇀ {C}
package_owned(fact) ⇔ fact ∈ dom(pkg_owner)

term(J) =
  {Γ(J),Θ(J),Action(J),Ring(J),Result(J),Δ(J),Θ'(J),χ(J),
   Σ(J),Μ(J),Φ(J),Obs(J),Rel(J),Π(J)}
free(J) = free_names(term(J))
closed(J) ⇔ free(J) = ∅

substitution_preserves_owner([x↦v]J) ⇔
  owner(x)=owner(v) → owner_preserved(J,[x↦v]J)

α_equiv(J1,J2) ⇔ same_structure(J1,J2) ∧ bijective_rename(bound_names(J1),bound_names(J2))
```

### Metavariable Convention

The convention below keeps the notation readable: sorts, kinds, and identifiers are written capitalized, bound instances are lowercase, and a CQE is only ever read through its `Res`, `Flags`, and `user_data` fields. Hold to it so that a name's case tells you at a glance whether it denotes a sort or a single instance.

```text
sorts / kinds / identifiers : capital   (CQE, SQE, Ctx, Ring, R[κ], CQEView, CQECopy,
                                          CQEFact, CQEResultResource, CQEEvent, ...)
bound instances             : lowercase (cqe, sqe, ctx, r)
field access                : cqe ↦ {cqe.Res, cqe.Flags, cqe.user_data}

InstanceCase =
  { cqe, sqe, ctx, r }

SortOrKind =
  { CQE, SQE, Ctx, Ring, R[κ], CQEView, CQECopy,
    CQEFact, CQEResultResource, CQEEvent }

IdentifierCapital =
  { CQE_facts, CQE_observed_for, CQE_observed_at, CQE_case_*,
    observe_data_CQE, observe_notification_CQE, IORING_CQE_*,
    IOSQE_CQE_SKIP_SUCCESS }

lowercase_bound_instance(x) ⇔ x ∈ InstanceCase
capital_sort_or_identifier(x) ⇔ x ∈ SortOrKind ∪ IdentifierCapital

∀x. bound_instance(x) → lowercase_bound_instance(x)
∀x. bound_instance(x) → x ∉ SortOrKind ∧ x ∉ IdentifierCapital
∀cqe. field_access(cqe) ⊆ {cqe.Res, cqe.Flags, cqe.user_data}

PathName = {p | finite_string(p)}
Path = {path(p) | p ∈ PathName}
SourceFile = {f ∈ Path | checked_out_source_file(f)}
read_current_source : SourceFile → Bool
read_current_source(f) ⇔
  f ∈ SourceFile ∧ file_exists(f) ∧ read_at_current_revision(f)

capital_allowed(Ctx) ⇔ kind_position(Ctx) ∨ sort_annotation(Ctx)
capital_allowed(CQE) ⇔ sort_position(CQE) ∨ type_annotation(CQE)
capital_allowed(R[κ]) ⇔ kind_family(R[κ])
¬bound_instance(Ctx)
¬bound_instance(CQE)
¬bound_instance(R[κ])

GuideCaseInvariant ⇔
  holds(path("uring/agents/INDEX.md"))
  ∧ holds(path("uring/agents/formalization"))
  ∧ holds(path("uring/agents/boundary"))
  ∧ holds(path("uring/agents/workflow"))
  ∧ holds(path("uring/agents/runtime"))
  ∧ holds(path("uring/agents/lift"))
  ∧ ∀x. bound_completion_instance(x) → x ∈ {cqe,ctx,r}
  ∧ ∀cqe. field_access(cqe) ⊆ {cqe.Res,cqe.Flags,cqe.user_data}
```

## Typing And Resource Discipline

Typing is where "use it once, never let a borrow escape" becomes checkable. The rules below split carriers into boundary resources and result resources, mark the affine (linear) tokens that may be consumed exactly once, and force a completion view to be *copied* before it can outlive the observation that produced it. When you add or change an operation, its derivation must appear here, and it must avoid every forbidden derivation listed at the end of the block.

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
WitnessKind = {CompletionFact,ContextIdentity,SQEFact,CQEFact,FieldWitness}
Atom = countable set of syntactic atoms
ℕ = {0,1,2,...}
FiniteTuple(X) = ⋃_{n∈ℕ} X^n
TermName = finite subset of Atom
ResourceName = TermName
EvidenceName = TermName
WitnessName = TermName
FrontierArgs = FiniteTuple(Atom)
LifecycleArgs = FiniteTuple(Atom)
Resource = {res(k,n) | k ∈ ResourceKind ∧ n ∈ ResourceName}
resource_kind : Resource → ResourceKind
resource_kind(res(k,n)) = k
Evidence = {evd(k,n) | k ∈ EvidenceKind ∧ n ∈ EvidenceName}
evidence_kind : Evidence → EvidenceKind
evidence_kind(evd(k,n)) = k
Witness = {wit(k,n) | k ∈ WitnessKind ∧ n ∈ WitnessName}
witness_kind : Witness → WitnessKind
witness_kind(wit(k,n)) = k
FrontierKind =
  {pending,nonterminal,terminal,release,after_cqe,after_failure,
   pending_after_failure,nonterminal_after_failure,terminal_after_failure}
Frontier = FrontierKind ∪ {frontier(k,args) | k ∈ FrontierKind ∧ args ∈ FrontierArgs}
frontier_kind : Frontier → FrontierKind
∀ k ∈ FrontierKind. frontier_kind(k) = k
∀ k ∈ FrontierKind. ∀ args ∈ FrontierArgs. frontier_kind(frontier(k,args)) = k
LifecycleKind = {released,recycled,closed,stopped}
LifecycleToken = {lifecycle(k,args) | k ∈ LifecycleKind ∧ args ∈ LifecycleArgs}
lifecycle_kind : LifecycleToken → LifecycleKind
∀ k ∈ LifecycleKind. ∀ args ∈ LifecycleArgs. lifecycle_kind(lifecycle(k,args)) = k
released_token(args) = lifecycle(released,args)
recycled_token(args) = lifecycle(recycled,args)
closed_token(args) = lifecycle(closed,args)
stopped_token(args) = lifecycle(stopped,args)
LinearToken = Resource ∪ Frontier ∪ LifecycleToken
OperationCarrierKind ⊆ BoundaryResourceKind
pairwise_disjoint(BoundaryResourceKind,ResultResourceKind)
EvidenceKind ∩ ResourceKind = ∅
WitnessKind ∩ ResourceKind = ∅
WitnessKind ∩ EvidenceKind = ∅
OperationCarrier(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ OperationCarrierKind
ResultResource(r) ⇔ r ∈ Resource ∧ resource_kind(r) ∈ ResultResourceKind
FDResultResource(r) ⇔ ResultResource(r) ∧ resource_kind(r) ∈ {FD,DirectFD}
fd_of : {r | FDResultResource(r)} ⇀ DescriptorResult

Type ::= RingStarted | Resource | Ring | FD | DirectFD | SocketAddr
       | Buf | FixedBuf | ProvidedBuf | Ctx | IndirectSQE | ExtSQE
       | CQEViewBorrowed | CQECopy | ListenerOp | IncrementalReceiver
       | ZCRXReceiver | MultishotSubscription | ZCTracker
       | ContextEvidence | RequirementEvidence
       | SafetyEvidence | SpinEvidence | FrameBoundaryEvidence | Cap
       | CompletionFact | Outcome | OutcomeProduct | OutcomePlane | ReleaseObservation
       | ReleasePlane | Frontier | LifecycleToken | Policy

Γ ⊢ ring : RingStarted
Γ ⊢ fd : FD
Γ ⊢ slot : DirectFD
Γ ⊢ buf : Buf
Γ ⊢ ctx : Ctx
Γ ⊢ cap : Cap

Affine(Θ) ⇔ finite(Θ) ∧ Θ ⊆ LinearToken ∧ ∀ x. multiplicity(x,Θ) ≤ 1
fresh(x,S) ⇔ x ∉ S
Γ;Θ ⊢ x : LinearToken → x ∈ Θ
Γ;Θ ⊢ r : Resource → r ∈ Θ
Γ;Θ ⊢ r : κ → (r ∈ Θ ∧ κ ∈ ResourceKind ∧ resource_kind(r)=κ)
Γ;Θ ⊢ f : Frontier → f ∈ Θ
Γ;Θ ⊢ l : LifecycleToken → l ∈ Θ
Γ;χ ⊢ e : ε → (e ∈ χ ∧ ε ∈ EvidenceKind ∧ evidence_kind(e)=ε)
Γ;Μ ⊢ m : ω → (m ∈ Μ ∧ ω ∈ WitnessKind ∧ witness_kind(m)=ω)
frontier_from : CQE × Ctx × Resource ⇀ Frontier
pending(ctx,r) ∈ Frontier ⇔
  frontier_kind(pending(ctx,r)) = pending
  ∧ submitted(ctx,r)
after_cqe(ctx,r,cqe) ∈ Frontier ⇔
  frontier_kind(after_cqe(ctx,r,cqe)) = after_cqe
  ∧ observed(ctx,r,cqe)
  ∧ cqe.user_data = raw(ctx)
completion_fact(ctx,r,cqe) ∈ Witness ⇔
  witness_kind(completion_fact(ctx,r,cqe)) = CompletionFact
  ∧ observed(ctx,r,cqe)
  ∧ cqe.user_data = raw(ctx)
terminal_token(ctx,r,f) ⇔
  f = terminal_frontier(ctx,r) ∨ f = terminal_frontier_after_failure(ctx,r)
release_frontier_token(ctx,r,f) ⇔ f = release_frontier(ctx,r)
release_observation_ready(ctx,r,J) ⇔
  Rel(J) ∈ ReleaseObservation
  ∧ frontier_fact(Rel(J), release_frontier(ctx,r))
release_token(ctx,r,f,J,Θ) ⇔
  (terminal_token(ctx,r,f) ∧ f ∈ Θ)
  ∨ (release_frontier_token(ctx,r,f) ∧ release_observation_ready(ctx,r,J))
FrontierContext = { Ξ | finite(Ξ) ∧ Ξ ⊆ Frontier }
terminal_completion(ctx,r,Ξ) ⇔
  ∃ f. terminal_token(ctx,r,f) ∧ f ∈ Ξ
decoded_frontier(ctx,r,cqe,f) ⇔
  OperationCarrier(r)
  ∧ defined(frontier_from(cqe,ctx,r))
  ∧ f = frontier_from(cqe,ctx,r)
result_resource(cqe,ctx,out) ⇔
  ResultResource(out)
  ∧ cqe.user_data = raw(ctx)
  ∧ ((FDResultResource(out)
      ∧ defined(fd_of(out))
      ∧ decode_result(op_of(ctx),cqe.Res) = fd_result(fd_of(out)))
     ∨ selected_buffer_result(cqe,out)
     ∨ explicit_result_resource(cqe,out))
cqe_named(cqe,ctx) ⇔ cqe.user_data = raw(ctx)
operation_cqe(cqe) ⇔ flag(NOTIF) ∉ flag_projection(cqe.Flags)
notification_cqe(cqe) ⇔ flag(NOTIF) ∈ flag_projection(cqe.Flags)
operation_cqe_for(cqe,ctx) ⇔ cqe_named(cqe,ctx) ∧ operation_cqe(cqe)
notification_cqe_for(cqe,ctx) ⇔ cqe_named(cqe,ctx) ∧ notification_cqe(cqe)
data_frontier_seen(ctx,H) ⇔
  ∃ d ∈ H. operation_cqe_for(d,ctx) ∧ flag(MORE) ∈ flag_projection(d.Flags)
early_notification_seen(ctx,H) ⇔
  ∃ d ∈ H. notification_cqe_for(d,ctx) ∧ ¬data_frontier_seen(ctx,prefix(H,d))
Hist(ctx) =
  { H | finite_seq(H)
        ∧ ∀ cqe ∈ H. cqe.user_data = raw(ctx) }
history(ctx) ∈ Hist(ctx)
d ∈ history(ctx) ⇔ ∃ r. observed(ctx,r,d)
ZeroCopyReleaseCarrierKind = {ExtSQE}
ZeroCopySendOperation = {op | zero_copy_send_opcode(op)}
zero_copy_send_ctx(ctx) ⇔ op_of(ctx) ∈ ZeroCopySendOperation
zero_copy_release_carrier(ctx,r) ⇔
  names(ctx,r)
  ∧ OperationCarrier(r)
  ∧ resource_kind(r) ∈ ZeroCopyReleaseCarrierKind
terminal_fallback_carrier(ctx,r) ⇔
  zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
notification_release_ready_cqe(cqe,ctx,H) ⇔
  (notification_cqe_for(cqe,ctx) ∧ data_frontier_seen(ctx,H))
  ∨ (operation_cqe_for(cqe,ctx) ∧ early_notification_seen(ctx,H))
terminal_no_notification_cqe(cqe,ctx,r,H) ⇔
  operation_cqe_for(cqe,ctx)
  ∧ flag(MORE) ∉ flag_projection(cqe.Flags)
  ∧ terminal_fallback_carrier(ctx,r)
  ∧ ¬data_frontier_seen(ctx,H)
  ∧ ¬early_notification_seen(ctx,H)
release_ready_carrier_at(cqe,ctx,r,H) ⇔
  (operation_cqe_for(cqe,ctx) ∨ notification_cqe_for(cqe,ctx))
  ∧ zero_copy_send_ctx(ctx)
  ∧ zero_copy_release_carrier(ctx,r)
  ∧ (notification_release_ready_cqe(cqe,ctx,H)
     ∨ terminal_no_notification_cqe(cqe,ctx,r,H))
release_ready_cqe(cqe,ctx,H) ⇔
  ∃ r. release_ready_carrier_at(cqe,ctx,r,H)

T_Submit:
  Γ;Θ ⊢ ring:RingStarted ∧ Γ;Θ ⊢ ctx:Ctx ∧ Γ;Θ ⊢ r:Resource ∧ OperationCarrier(r)
  ∧ fresh(pending(ctx,r), Θ - {ctx,r})
  ─────────────────────────────────────────────────────────────────
  Γ;Θ ⊢ submit(ctx,r) : pending(ctx,r) ▷ Θ - {ctx,r} + {pending(ctx,r)}

T_Observe:
  Γ;Θ ⊢ pending(ctx,r) : Frontier ∧ emitted(cqe) ∧ cqe.user_data = raw(ctx)
  ∧ fresh(after_cqe(ctx,r,cqe), Θ - {pending(ctx,r)})
  ─────────────────────────────────────────────────────────────────
  Γ;Θ ⊢ observe(cqe,ctx,r) : CQEViewBorrowed
    ▷ Θ - {pending(ctx,r)} + {after_cqe(ctx,r,cqe)}

T_CopyCQE:
  Γ;Θ ⊢ view:CQEViewBorrowed
  ─────────────────────────────────────────────────────────────────
  Γ;Θ ⊢ copy(view.{Res,Flags,user_data}) : CQECopy ▷ Θ

T_DecodeFrontier:
  Γ;Θ ⊢ after_cqe(ctx,r,cqe) : Frontier ∧ f ∈ Frontier
  ∧ decoded_frontier(ctx,r,cqe,f)
  ∧ fresh(f, Θ - {after_cqe(ctx,r,cqe)})
  ─────────────────────────────────────────────────────────────────
  Γ;Θ;Μ ⊢ decode_frontier(cqe,ctx,r) : f
    ▷ Θ - {after_cqe(ctx,r,cqe)} + {f}
    ; Μ + {completion_fact(ctx,r,cqe)}

T_DecodeResult:
  Γ;Μ ⊢ completion_fact(ctx,r,cqe) : CompletionFact ∧ result_resource(cqe,ctx,out)
  ∧ fresh(out, Θ)
  ─────────────────────────────────────────────────────────────────
  Γ;Θ;Μ ⊢ decode_result_resource(cqe,out) : out
    ▷ Θ + {out}
    ; Μ

T_Release:
  Γ;Θ ⊢ f:Frontier ∧ release_token(ctx,r,f,J,Θ)
  ∧ fresh(released_token(r), Θ - ({f} ∩ Θ))
  ─────────────────────────────────────────────────────────────────
  Γ;Θ ⊢ release(r) : released_token(r)
    ▷ Θ - ({f} ∩ Θ) + {released_token(r)}

T_DecodeRelease:
  Γ;Μ ⊢ completion_fact(ctx,r,cqe) : CompletionFact ∧ OperationCarrier(r)
  ∧ release_ready_carrier_at(cqe,ctx,r,history(ctx))
  ─────────────────────────────────────────────────────────────────
  Γ;Θ;Μ ⊢ decode_release(cqe,ctx,r) : ReleaseObservation
    ▷ Θ
    ; Μ

T_Block:
  empty_cq ∨ no_progress
  ─────────────────────────────────────────────────────────────────
  Γ;Θ ⊢ block_boundary : Outcome ▷ Θ

T_SkipSuccess:
  Γ;Θ ⊢ pending(ctx,r) : Frontier ∧ IOSQE_CQE_SKIP_SUCCESS
  ∧ success_cqe_skipped(ctx)
  ∧ fresh(terminal_frontier(ctx,r), Θ - {pending(ctx,r)})
  ─────────────────────────────────────────────────────────────────
  Γ;Θ ⊢ skip_success(ctx,r) : terminal_frontier(ctx,r)
    ▷ Θ - {pending(ctx,r)} + {terminal_frontier(ctx,r)}

ForbiddenDerivation =
  { pending(ctx,r) ↛ recycle(r) | ctx:Ctx, r:Resource }
  ∪ { flag(MORE) ↛ terminal_completion(ctx,r,Ξ) | ctx:Ctx, r:Resource, Ξ:FrontierContext }
  ∪ { nonterminal_frontier(ctx,r) ∧ ¬release_observation_ready(ctx,r,J) ↛ release(r)
    | ctx:Ctx, r:Resource }
  ∪ { nonterminal_frontier_after_failure(ctx,r) ∧ ¬release_observation_ready(ctx,r,J) ↛ release(r)
    | ctx:Ctx, r:Resource }
  ∪ { result_resource(cqe,ctx,out) ↛ nonterminal_frontier(ctx,out) | cqe:CQE, ctx:Ctx, out:Resource }
  ∪ { CQEViewBorrowed ↛ store_after_epoch
    , Policy ↛ boundary_fact
    }

∀ p ∈ ForbiddenDerivation. require(p)
RuleSurface =
  {T_Submit,T_Observe,T_CopyCQE,T_DecodeFrontier,T_DecodeResult,T_Release,T_DecodeRelease,
   T_Block,T_SkipSuccess}
TypeSurface = { τ | τ : Type }
TypeRuleSurface = TypeSurface ∪ RuleSurface
⟦·⟧^{U,type}_w : TypeRuleSurface ⇀ Denotation_U(w)
∀ T ∈ RuleSurface. ∀ w ∈ World_U. defined(⟦T⟧^{U,type}_w)
```
