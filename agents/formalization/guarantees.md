# Guarantees

Why should an agent trust this guide? Because the discipline it imposes is *sound*: the separations it asks you to keep — outcome apart from release, kernel fact apart from caller policy, owned apart from borrowed — are preserved by every step, survive composition, and correspond to real Go across a round trip. This file collects that argument. Read it not to re-prove anything, but to know exactly which guarantees your change may rely on, and which properties your change must itself satisfy before it counts as correct.

The first section states the metatheory: the world an agent reasons in, the denotation that gives a judgment meaning, and the theorems that together make up soundness. The second turns those theorems into a checklist of named properties, ending in the single acceptance predicate that every correct change must clear.

## Soundness And The Theorems It Rests On

`Soundness(M)` is the conjunction of every theorem in this block. Preservation and progress keep a well-formed judgment well-formed and never stuck; the three separation theorems keep outcomes, release evidence, and caller policy apart; monotonicity and adequacy tie meaning to an advancing world; round-trip and composability make the model faithful to Go and stable under sequencing. When a guarantee is relied on elsewhere in the guide, this is where it is discharged.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

Theory =
  { Syntax
  , Typing
  , Reduction
  , OutcomeProduct
  , OutcomePlane
  , ReleaseObservation
  , ReleasePlane
  , BoundaryObservation
  , ResourceAffine
  , BoundaryProjection
  , BidirectionalDenotation
  , CompositionalClosure
  }

World_U =
  { w | w = (Γ_w, Δ_w, Θ_w, Θ'_w, χ_w, Σ_w, Μ_w, Φ_w, Obs_w, Rel_w, Π_w)
        ∧ affine(Θ_w,Θ'_w)
        ∧ support(Σ_w) ⊆ CallerFrontier
        ∧ support(Π_w) ⊆ CallerPolicy
        ∧ Obs_w ∈ OutcomePlane
        ∧ Rel_w ∈ ReleasePlane }

GoSurface = { g | public_go_term_caller(module,g) }
JudgmentSurface =
  { J | WF(J)
        ∧ Obs(J) ∈ OutcomePlane
        ∧ Rel(J) ∈ ReleasePlane
        ∧ support(Σ(J)) ⊆ CallerFrontier
        ∧ support(Π(J)) ⊆ CallerPolicy }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧⁻¹_U : GoSurface ⇀ JudgmentSurface
⟦·⟧_U   : JudgmentSurface ⇀ GoSurface
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

preorder_U(w,w') ⇔
  extends(Γ_w,Γ_w')
  ∧ preserves(Δ_w,Δ_w')
  ∧ transports(χ_w,χ_w')
  ∧ preserves_visible(Θ_w,Θ_w')
  ∧ preserves_visible(Θ'_w,Θ'_w')
  ∧ preserves(Σ_w,Σ_w')
  ∧ preserves(Μ_w,Μ_w')
  ∧ preserves(Φ_w,Φ_w')
  ∧ preserves(Obs_w,Obs_w')
  ∧ preserves(Rel_w,Rel_w')
  ∧ preserves(Π_w,Π_w')

⟦Resource(κ)⟧^U_w =
  { r | resource_kind(r)=κ ∧ r ∈ Θ_w }

⟦OutcomeProduct⟧^U_w =
  { Obs | Obs ∈ OutcomeProduct
          ∧ ctrl(Obs) ∈ Ctrl[E]
          ∧ distinct(Ctrl[E]) }

⟦OutcomePlane⟧^U_w =
  { Obs | Obs = none
          ∨ Obs ∈ ⟦OutcomeProduct⟧^U_w }

⟦ReleasePlane⟧^U_w =
  { Rel | Rel = none
          ∨ (Rel ∈ ReleaseObservation ∧ ¬defined(ctrl(Rel))) }

defined(⟦J⟧^U_w) ⇔
  w ∈ World_U
  ∧ WF(J)
  ∧ Γ(J) ⊆ Γ_w
  ∧ Δ(J) ⊆ Δ_w
  ∧ Θ(J) ⊆ Θ_w
  ∧ Θ'(J) ⊆ Θ'_w
  ∧ transportable(χ(J),χ_w)
  ∧ Σ(J) ⊆ Σ_w
  ∧ Μ(J) ⊆ Μ_w
  ∧ Φ(J) ⊆ Φ_w
  ∧ Obs(J) ∈ ⟦OutcomePlane⟧^U_w
  ∧ Rel(J) ∈ ⟦ReleasePlane⟧^U_w
  ∧ Π(J) ⊆ Π_w

RelativeModel(M) ⇔
  interprets(M,Theory)
  ∧ interprets(M,World_U)
  ∧ interprets(M,⟦·⟧^U)
  ∧ interprets(M,⟦·⟧_U)
  ∧ interprets(M,⟦·⟧⁻¹_U)
  ∧ respects(M, documented_linux_abi)
  ∧ respects(M, public_api(module))
  ∧ abstracts_over(M, caller_policy_order)

Reduces1(J,J') ⇔ J → J'
Step(J,J') ⇔ Reduces1(J,J')
frontier_fact(X,F) ⇔ F ∈ frontier_set(X)
terminal_completion(ctx,r,J) ⇔
  frontier_fact(Obs(J), terminal_frontier(ctx,r))
  ∨ frontier_fact(Obs(J), terminal_frontier_after_failure(ctx,r))
terminal_observation(J) ⇔
  ∃ ctx,r. terminal_completion(ctx,r,J)
wouldBlock_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct ∧ ctrl(Obs(J)) = wouldBlock
errMore_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct
  ∧ ctrl(Obs(J)) = errMore
  ∧ ∃ ctx,r.
      frontier_fact(Obs(J),nonterminal_frontier(ctx,r))
nonterminal_success_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct
  ∧ ctrl(Obs(J)) = ok
  ∧ ∃ ctx,r.
      frontier_fact(Obs(J),nonterminal_frontier(ctx,r))
nonterminal_failure_visible(J) ⇔
  Obs(J) ∈ OutcomeProduct
  ∧ ∃ e ∈ E. ctrl(Obs(J)) = fail(e)
  ∧ ∃ ctx,r.
      frontier_fact(Obs(J),nonterminal_frontier_after_failure(ctx,r))
live_frontier_visible(J) ⇔
  nonterminal_success_visible(J)
  ∨ errMore_visible(J)
  ∨ nonterminal_failure_visible(J)
no_operation_outcome_visible(J) ⇔
  Obs(J) = none

Theorem_Preservation(J,J') ⇔
  (WF(J) ∧ Step(J,J')) → (WF(J') ∧ affine(Θ(J'),Θ'(J')))

Theorem_Progress(J) ⇔
  (WF(J) ∧ closed(J)) →
    terminal_observation(J)
    ∨ wouldBlock_visible(J)
    ∨ live_frontier_visible(J)
    ∨ no_operation_outcome_visible(J)
    ∨ ∃ J'. Step(J,J')

Theorem_OutcomeSeparation(Obs) ⇔
  Obs ∈ OutcomeProduct →
    ctrl(Obs) ∈ Ctrl[E] ∧ distinct(Ctrl[E])

Theorem_ReleaseSeparation(Rel) ⇔
  Rel ∈ ReleaseObservation → ¬defined(ctrl(Rel))

Theorem_PolicySeparation(J) ⇔
  WF(J) → ((support(Π(J)) ∩ support(B) = ∅)
           ∧ (support(Σ(J)) ∩ support(B) = ∅))

Theorem_Monotonicity(J,w,w') ⇔
  (defined(⟦J⟧^U_w) ∧ preorder_U(w,w')) → defined(⟦J⟧^U_{w'})

Theorem_Adequacy(J,w) ⇔
  defined(⟦J⟧^U_w) →
    contextual_observation(J,w) = π_boundary(⟦J⟧^U_w)

Theorem_Roundtrip(J,go_surface) ⇔
  (DesignDirection(J) →
     defined(⟦⟦J⟧_U⟧⁻¹_U)
     ∧ ⟦⟦J⟧_U⟧⁻¹_U ≃_B J)
  ∧ (ReviewDirection(go_surface) →
     defined(⟦⟦go_surface⟧⁻¹_U⟧_U)
     ∧ ⟦⟦go_surface⟧⁻¹_U⟧_U ≃_pub go_surface)

Theorem_Composability(J1,J2,w) ⇔
  (Composable_U(J1,J2)
   ∧ defined(⟦J1⟧^U_w)
   ∧ defined(⟦J2⟧^U_w))
    → (ProvableComposability_U(J1,J2)
       ∧ defined(⟦J1 ⊙_U J2⟧^U_w)
       ∧ preserves(π_boundary(⟦J1 ⊙_U J2⟧^U_w),
           {π_boundary(⟦J1⟧^U_w), π_boundary(⟦J2⟧^U_w)})
       ∧ preserves(π_caller(⟦J1 ⊙_U J2⟧^U_w),
           {π_caller(⟦J1⟧^U_w), π_caller(⟦J2⟧^U_w)}))

Theorem_FormalizationGuideComposability ⇔
  P_formalization_guide_family
  ∧ FormalizationGuideComposable_U
  ∧ ∀ f ∈ FormalizationGuideFile_U. GuideInternalWell_U(f)

Soundness(M) ⇔
  RelativeModel(M)
  ∧ ∀ J,J'. Theorem_Preservation(J,J')
  ∧ ∀ J. Theorem_Progress(J)
  ∧ ∀ Obs. Theorem_OutcomeSeparation(Obs)
  ∧ ∀ Rel. Theorem_ReleaseSeparation(Rel)
  ∧ ∀ J. Theorem_PolicySeparation(J)
  ∧ ∀ J,w,w'. Theorem_Monotonicity(J,w,w')
  ∧ ∀ J,w. Theorem_Adequacy(J,w)
  ∧ ∀ J,go_surface. Theorem_Roundtrip(J,go_surface)
  ∧ ∀ J1,J2,w. Theorem_Composability(J1,J2,w)
  ∧ Theorem_FormalizationGuideComposability

Completeness_relative(surface) ⇔
  ∀ boundary_task ∈ surface.
    ∃ J. formalizes(boundary_task,J) ∧ compile_allowed(J)
```
## The Properties A Change Must Satisfy

The theorems above are about the system; the properties below are about *your* judgment and *your* Go. Each is a predicate you can check directly: owner unique, resources affine, outcomes separated, policy kept out of the boundary, denotation defined, round trip closed. `Accept(J, go_surface)` bundles them into the bar a change must clear — it is the formal restatement of "this change uses `code.hybscloud.com/uring` correctly".

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

GoSurface = { g | public_go_term_caller(module,g) }
boundary_go : GoSurface ⇀ GoSurface
caller_go    : GoSurface ⇀ GoSurface
support(go_surface) =
  support(boundary_go(go_surface)) ∪ support(caller_go(go_surface))
JudgmentSurface =
  { J | WF(J)
        ∧ Obs(J) ∈ OutcomePlane
        ∧ Rel(J) ∈ ReleasePlane
        ∧ support(Σ(J)) ⊆ CallerFrontier
        ∧ support(Π(J)) ⊆ CallerPolicy }
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧⁻¹_U : GoSurface ⇀ JudgmentSurface
⟦·⟧_U   : JudgmentSurface ⇀ GoSurface
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)
π_boundary : JudgmentSurface ∪ ⋃_{w∈World_U} Denotation_U(w) ⇀ BoundaryProjection
π_caller    : JudgmentSurface ∪ ⋃_{w∈World_U} Denotation_U(w) ⇀ CallerProjection
π_boundary(J) = {Δ(J), Θ(J), Θ'(J), χ(J), Μ(J), Φ(J), Obs(J), Rel(J)}
π_caller(J) = {Σ(J), Π(J)}
BoundaryClean(go_surface) ⇔
  go_surface ∈ GoSurface
  ∧ support(boundary_go(go_surface)) ∩ (CallerFrontier ∪ CallerPolicy) = ∅
  ∧ support(caller_go(go_surface)) ⊆ CallerFrontier ∪ CallerPolicy
  ∧ ¬hidden(CallerFrontier ∪ CallerPolicy, support(go_surface))

UnaryPropertySet =
  { P_WF
  , P_owner_unique
  , P_affine_resources
  , P_outcome_separation
  , P_identity_stability
  , P_capability_honesty
  , P_policy_separation
  , P_temporal_lifecycle
  , P_denotation
  , P_concept_conservativity
  }

GuideInternalPropertySet_U =
  { P_guide_judgment_wf
  , P_guide_plane_closed
  , P_guide_owner_closed
  , P_guide_denotation_defined
  , P_guide_obligation_routed
  , P_guide_composable
  }

BinaryPropertySet =
  { P_roundtrip
  , P_denotation_correspondence
  , P_composability
  }

PropertySet =
  UnaryPropertySet ∪ BinaryPropertySet ∪ GuideInternalPropertySet_U
  ∪ {P_formalization_guide_family}

P_WF(J) ⇔ WF(J)
P_owner_unique(J) ⇔ ∀ f ∈ boundary_facts(J). single_stratum_owner(f)
P_affine_resources(J) ⇔ affine(Θ(J),Θ'(J))
P_outcome_separation(J) ⇔
  Obs(J) ∈ OutcomePlane
  ∧ Rel(J) ∈ ReleasePlane
  ∧ (Obs(J) = none
     ∨ (Obs(J) ∈ OutcomeProduct ∧ ctrl(Obs(J)) ∈ Ctrl[E]))
  ∧ distinct(Ctrl[E])
  ∧ (Rel(J) = none
     ∨ (Rel(J) ∈ ReleaseObservation ∧ ¬defined(ctrl(Rel(J)))))
P_identity_stability(J) ⇔
  ∀ cqe,ctx. emitted(cqe,ctx) → cqe.user_data = raw(ctx)
P_capability_honesty(J) ⇔ capability_claims(χ(J)) ⊆ evidence(χ(J))
P_policy_separation(J) ⇔
  support(Σ(J)) ⊆ CallerFrontier
  ∧ support(Π(J)) ⊆ CallerPolicy
  ∧ (support(Σ(J)) ∪ support(Π(J))) ∩ support(boundary(module)) = ∅
frontier_fact(X,F) ⇔ F ∈ frontier_set(X)
terminal_completion(ctx,r,J) ⇔
  frontier_fact(Obs(J), terminal_frontier(ctx,r))
  ∨ frontier_fact(Obs(J), terminal_frontier_after_failure(ctx,r))
release_observation_ready(ctx,r,J) ⇔
  Rel(J) ∈ ReleaseObservation
  ∧ frontier_fact(Rel(J), release_frontier(ctx,r))
active_after_cqe(ctx,r,J) ⇔ ∃ cqe. after_cqe(ctx,r,cqe) ∈ Θ(J)
P_temporal_lifecycle(J) ⇔
  ∀ ctx,r. names(ctx,r) →
    (□(pending(ctx,r) → ¬recycle(r))
     ∧ □(active_after_cqe(ctx,r,J) → ¬recycle(r) ∧ ¬close_owner(r))
     ∧ □(terminal_completion(ctx,r,J) → ◇(may_release(r) ∨ caller_retains(r)))
     ∧ □(release_observation_ready(ctx,r,J) → ◇may_release(r)))
P_denotation(J) ⇔
  J ∈ JudgmentSurface
  ∧ ∃ w0 ∈ World_U. defined(⟦J⟧^U_w0)
  ∧ ∀ w ∈ World_U.
    defined(⟦J⟧^U_w) → denotation_respects_boundary(J,w)
P_roundtrip(J,go_surface) ⇔
  compilation_sound(J)
  ∧ abstraction_sound(go_surface)
  ∧ BoundaryClean(go_surface)
  ∧ BoundaryPair(J,go_surface)
  ∧ defined(⟦J⟧_U)
  ∧ defined(⟦go_surface⟧⁻¹_U)
  ∧ defined(⟦⟦J⟧_U⟧⁻¹_U)
  ∧ defined(⟦⟦go_surface⟧⁻¹_U⟧_U)
  ∧ ⟦⟦J⟧_U⟧⁻¹_U ≃_B J
  ∧ ⟦⟦go_surface⟧⁻¹_U⟧_U ≃_pub go_surface

P_denotation_correspondence(J,go_surface) ⇔
  J ∈ JudgmentSurface
  ∧ go_surface ∈ GoSurface
  ∧ BoundaryClean(go_surface)
  ∧ ∃ w ∈ World_U.
    defined(⟦J⟧^U_w)
    ∧ defined(⟦go_surface⟧⁻¹_U)
    ∧ defined(⟦⟦go_surface⟧⁻¹_U⟧^U_w)
    ∧ π_boundary(⟦⟦go_surface⟧⁻¹_U⟧^U_w) = π_boundary(⟦J⟧^U_w)
    ∧ π_caller(⟦⟦go_surface⟧⁻¹_U⟧^U_w) = π_caller(⟦J⟧^U_w)
    ∧ Obs(⟦go_surface⟧⁻¹_U) = Obs(J)
    ∧ Rel(⟦go_surface⟧⁻¹_U) = Rel(J)

P_composability(J1,J2) ⇔
  Composable_U(J1,J2)
  ∧ ProvableComposability_U(J1,J2)
  ∧ J1 ⊙_U J2 ∈ JudgmentSurface
  ∧ P_WF(J1 ⊙_U J2)
  ∧ P_policy_separation(J1 ⊙_U J2)
  ∧ P_outcome_separation(J1 ⊙_U J2)
  ∧ P_denotation(J1 ⊙_U J2)

P_guide_judgment_wf(f) ⇔
  f ∈ FormalizationGuideFile_U
  ∧ GuideJudgment_U(f) ∈ JudgmentSurface
  ∧ WF(GuideJudgment_U(f))

P_guide_plane_closed(f) ⇔
  f ∈ FormalizationGuideFile_U
  ∧ Obs(GuideJudgment_U(f)) ∈ OutcomePlane
  ∧ Rel(GuideJudgment_U(f)) ∈ ReleasePlane
  ∧ support(Σ(GuideJudgment_U(f))) ⊆ CallerFrontier
  ∧ support(Π(GuideJudgment_U(f))) ⊆ CallerPolicy

P_guide_owner_closed(f) ⇔
  f ∈ FormalizationGuideFile_U
  ∧ ∀ c ∈ TheoryConceptUsed(GuideJudgment_U(f)).
      (c ∈ dom(ConceptOwner_U) → read_complete(ConceptOwner_U(c)))

P_guide_denotation_defined(f) ⇔
  f ∈ FormalizationGuideFile_U
  ∧ ∃ w ∈ World_U. defined(⟦GuideJudgment_U(f)⟧^U_w)

P_guide_obligation_routed(f) ⇔
  f ∈ FormalizationGuideFile_U
  ∧ ∀ c ∈ TheoryConceptUsed(GuideJudgment_U(f)).
      c ∈ dom(ConceptRoute_U)
      ∧ admissible_concept_U(GuideJudgment_U(f),c)
      ∧ concept_utilized_U(GuideJudgment_U(f),c)
      ∧ ConceptRoute_U(c) ⊆ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}

P_guide_composable(f) ⇔
  f ∈ FormalizationGuideFile_U
  ∧ (∀ g ∈ FormalizationGuideFile_U.
      composable_topic_order(f,g) →
        P_composability(GuideJudgment_U(f),GuideJudgment_U(g)))
  ∧ (∀ g ∈ FormalizationGuideFile_U.
      composable_topic_order(g,f) →
        P_composability(GuideJudgment_U(g),GuideJudgment_U(f)))

GuideInternalWell_U(f) ⇔
  P_guide_judgment_wf(f)
  ∧ P_guide_plane_closed(f)
  ∧ P_guide_owner_closed(f)
  ∧ P_guide_denotation_defined(f)
  ∧ P_guide_obligation_routed(f)
  ∧ P_guide_composable(f)

P_formalization_guide_family ⇔
  (∀ f ∈ FormalizationGuideFile_U. GuideInternalWell_U(f))
  ∧ FormalizationGuideComposable_U
  ∧ (∀ f1,f2 ∈ FormalizationGuideFile_U.
      composable_topic_order(f1,f2) →
        P_composability(GuideJudgment_U(f1),GuideJudgment_U(f2)))

P_concept_conservativity(J) ⇔
  ∀ c ∈ TheoryConceptUsed(J).
    c ∈ dom(ConceptRoute_U)
    ∧ admissible_concept_U(J,c)
    ∧ concept_utilized_U(J,c)
    ∧ ConceptRoute_U(c) ⊆ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
    ∧ ¬introduces_plane(c,J)
    ∧ ¬changes_owner(c,J)

Accept(J,go_surface) ⇔
  (∀ P ∈ UnaryPropertySet. P(J))
  ∧ P_roundtrip(J,go_surface)
  ∧ P_denotation_correspondence(J,go_surface)
  ∧ (∀ J_prev.
      uses_composed_boundary(J_prev,J) → P_composability(J_prev,J))
```
