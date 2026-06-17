# Overview

This file is the entry point to the formal core of the guide — the part that says, precisely, what "use `code.hybscloud.com/uring` correctly" means and gives an agent the machinery to check that a change actually does. Read it first. It fixes the strata you reason across, the three separations you must never collapse, the single judgment every other topic file refines, and the maps that route each concept to where it is owned and what it must prove.

Three separations carry the whole guide; every later file enforces one or more of them:

- kernel fact ⊥ caller policy — what the kernel did is not whether to retry, back off, or route; the boundary states facts, the caller decides policy.
- outcome ⊥ release evidence — a completion's result (`ok` / `wouldBlock` / `errMore` / `fail(e)`) is independent of the proof that a buffer or zero-copy window may be released.
- owned ⊥ borrowed — ownership transfer is explicit and linear; a borrowed CQE view must be copied before it outlives the observation that produced it.

The judgment `J` is the shape you reason in. Every topic file projects onto one or more of its slots — resources onto `Θ`/`Θ'`, the demanded operation onto `Δ`, the completion result onto `Obs`, release evidence onto `Rel`, the caller's frontier onto `Σ`, the caller's policy onto `Π`:

```text
J = Γ | Θ ⊢ action @ ring : result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π
```

The maps in the block are the load-bearing tables. `ConceptRoute_U` sends each of the twenty-one theory concepts to the judgment slots it may touch; `ConceptOwner_U` names the package that owns a concept, so you know which neighbour to read; `ConceptObligation_U` lists what each concept must discharge. `Composable_U` / `ProvableComposability_U` are what let you sequence two correct boundary actions and stay correct.

Read the formal core in this order:

1. `overview.md` — this file: goal, separations, judgment, concept maps, composability.
2. `notation.md` — the notation and typing discipline you reason in.
3. `kernel-boundary.md` — kernel facts vs caller policy; the SQE-to-CQE correspondence.
4. `outcomes.md` — interpreting completions: the outcome, release, and control planes.
5. `resources.md` — owning, borrowing, and releasing resources.
6. `sessions.md` — protocols, multishot frontiers, and sessions.
7. `handler.md` — the shallow handler: calculus, reduction, and suspend/resume.
8. `go-mapping.md` — mapping the model down to Go and back.
9. `guarantees.md` — why the discipline is sound and composable.
10. `verification.md` — how to verify a change is correct.

The block below fixes the module strata, the Go and judgment surfaces, the judgment `J` and its slots, the three concept maps, composability, and the well-formedness predicate `WF(J)` that every judgment in the guide must satisfy.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
K = below(module)

GoSurface = { g | public_go_term_caller(module,g) }
boundary_go : GoSurface ⇀ GoSurface
caller_go    : GoSurface ⇀ GoSurface
support(go_surface) =
  support(boundary_go(go_surface)) ∪ support(caller_go(go_surface))
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
BoundaryClean(go_surface) ⇔
  go_surface ∈ GoSurface
  ∧ support(boundary_go(go_surface)) ∩ (CallerFrontier ∪ CallerPolicy) = ∅
  ∧ support(caller_go(go_surface)) ⊆ CallerFrontier ∪ CallerPolicy
  ∧ ¬hidden(CallerFrontier ∪ CallerPolicy, support(go_surface))

⟦·⟧⁻¹_U : GoSurface ⇀ JudgmentSurface
⟦·⟧_U   : JudgmentSurface ⇀ GoSurface
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)
lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J) = ⟦J⟧_U
denote_U(J,w) = ⟦J⟧^U_w

J =
  Γ | Θ ⊢ action @ ring : result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π

Γ = reusable_value_type_assumptions
Δ = visible_operation_demand
Θ,Θ' = affine_resource_frontiers
χ = capability_environment_context_requirement_safety_spin_frame_boundary_evidence
Σ = caller_frontier
Μ = observation_witness
Φ = temporal_lifecycle_obligations
Obs(J) ∈ OutcomePlane
Rel(J) ∈ ReleasePlane
Π = caller_policy

ConceptRoute_U =
  { value_structure ↦ {Γ}
  , type_structure ↦ {Γ}
  , abstraction ↦ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
  , compilation ↦ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
  , denotation ↦ {Γ,χ,Μ,Φ,Obs,Rel,Π}
  , logical_relation ↦ {Γ,χ,Obs,Rel}
  , Hoare_projection ↦ {Θ,χ,Σ,Φ,Obs,Rel,Π}
  , verification_evidence ↦ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
  , algebraic_effect ↦ {Δ}
  , handler ↦ {Δ,Μ}
  , shallow_handler ↦ {Δ,Μ}
  , resumption ↦ {Δ,Μ,Φ}
  , coeffect_requirement ↦ {χ}
  , modal_world ↦ {χ}
  , modal_later ↦ {χ,Φ}
  , coalgebraic_observation ↦ {Μ,Φ,Obs,Rel}
  , session_frontier ↦ {Σ}
  , linear_resource ↦ {Θ}
  , temporal_obligation ↦ {Φ}
  , outcome_quotient ↦ {Obs,Π}
  , composability ↦ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
  }

ConceptOwner_U =
  { algebraic_effect ↦ pkg("code.hybscloud.com/kont")
  , handler ↦ pkg("code.hybscloud.com/kont")
  , shallow_handler ↦ pkg("code.hybscloud.com/kont")
  , resumption ↦ pkg("code.hybscloud.com/kont")
  , coeffect_requirement ↦ pkg("code.hybscloud.com/cove")
  , modal_world ↦ pkg("code.hybscloud.com/cove")
  , modal_later ↦ pkg("code.hybscloud.com/cove")
  , coalgebraic_observation ↦ pkg("code.hybscloud.com/takt")
  , session_frontier ↦ pkg("code.hybscloud.com/sess")
  , linear_resource ↦ pkg("code.hybscloud.com/uring")
  , outcome_quotient ↦ pkg("code.hybscloud.com/iox")
  , composability ↦ pkg("code.hybscloud.com/uring")
  }

ConceptObligation_U =
  { value_structure ↦ {name_public_carrier, route_to_plane}
  , type_structure ↦ {name_public_carrier, route_to_plane}
  , abstraction ↦ {preserve_roundtrip, preserve_ownership_epoch}
  , compilation ↦ {preserve_roundtrip, keep_out_of_boundary}
  , denotation ↦ {preserve_denotation, preserve_outcome_partition}
  , logical_relation ↦ {preserve_denotation, preserve_release_partition}
  , Hoare_projection ↦ {preserve_ownership_epoch, enforce_temporal_frontier}
  , verification_evidence ↦ {verify_evidence_record, preserve_roundtrip}
  , algebraic_effect ↦ {name_public_operation, keep_in_caller_layer}
  , handler ↦ {name_public_operation, enforce_handler_shallow}
  , shallow_handler ↦ {enforce_handler_shallow, keep_out_of_boundary}
  , resumption ↦ {enforce_affine_resource, keep_in_caller_layer}
  , coeffect_requirement ↦ {enforce_context_requirement, keep_in_caller_layer}
  , modal_world ↦ {enforce_context_requirement, preserve_denotation}
  , modal_later ↦ {enforce_context_requirement, enforce_temporal_frontier}
  , coalgebraic_observation ↦ {enforce_stream_route, preserve_outcome_partition}
  , session_frontier ↦ {enforce_session_frontier, keep_in_caller_layer}
  , linear_resource ↦ {enforce_affine_resource, preserve_ownership_epoch}
  , temporal_obligation ↦ {enforce_temporal_frontier, preserve_release_partition}
  , outcome_quotient ↦ {preserve_outcome_partition, preserve_roundtrip}
  , composability ↦ {prove_composability, preserve_composability, preserve_roundtrip}
  }
ConceptObligation_U ∈ ConceptObligation
OwnerObligation_U(c) =
  {read_owner_package} if c ∈ dom(ConceptOwner_U)
  ∅                    if c ∉ dom(ConceptOwner_U)
RequiredConceptObligation_U(c) =
  ConceptObligation_U(c) ∪ OwnerObligation_U(c)

TemporalObligationOwner_U(φ) =
  pkg("code.hybscloud.com/takt")  if φ ∈ runner_temporal_obligation
  pkg("code.hybscloud.com/uring") if φ ∈ boundary_lifecycle_obligation
  C                               if φ ∈ caller_temporal_policy
owner_prepared_U(o) ⇔ (o = C) ∨ (o ∈ Pkg ∧ read_complete(o))
TemporalObligationAtoms_U(J) = facts(Φ(J))
concept_utilized_U(J,c) ⇔
  c ∈ dom(ConceptObligation_U)
  ∧ RequiredConceptObligation_U(c) ≠ ∅
  ∧ ∀ o ∈ RequiredConceptObligation_U(c).
      obligation_named(J,o)
      ∧ obligation_routes_to_guide_check(o)

admissible_concept_U(J,c) ⇔
  c ∈ dom(ConceptRoute_U)
  ∧ ConceptRoute_U(c) ⊆ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
  ∧ (c ∈ dom(ConceptOwner_U) → read_complete(ConceptOwner_U(c)))
  ∧ concept_utilized_U(J,c)
  ∧ (c = temporal_obligation →
      ∀ φ ∈ TemporalObligationAtoms_U(J).
        defined(TemporalObligationOwner_U(φ))
        ∧ owner_prepared_U(TemporalObligationOwner_U(φ)))
  ∧ ¬introduces_plane(c,J)
  ∧ ¬hidden(CallerFrontier ∪ CallerPolicy, support(B))
  ∧ preserves(Obs(J) ∈ OutcomePlane ∧ Rel(J) ∈ ReleasePlane)

π_boundary(J) = {Δ(J), Θ(J), Θ'(J), χ(J), Μ(J), Φ(J), Obs(J), Rel(J)}
π_caller(J) = {Σ(J), Π(J)}
NameAtoms(X) = finite set of names occurring in X
NameAtoms(none) = ∅
facts(X) = {f | atomic_fact(f) ∧ NameAtoms(f) ⊆ NameAtoms(X)}
facts(none) = ∅
facts({X1,...,Xn}) = ⋃ {facts(Xi) | 1 ≤ i ≤ n}
boundary_facts(J) = facts(π_boundary(J))

compose_U(J1,J2) = J1 ⊙_U J2
defined(J1 ⊙_U J2) ⇔
  J1 ∈ JudgmentSurface
  ∧ J2 ∈ JudgmentSurface
  ∧ compatible(Θ'(J1),Θ(J2))
  ∧ compatible(Σ(J1),Σ(J2))
  ∧ compatible(Π(J1),Π(J2))

OutcomeCompose_U(Obs1,Obs2,Obs12) ⇔
  Obs1 ∈ OutcomePlane
  ∧ Obs2 ∈ OutcomePlane
  ∧ Obs12 ∈ OutcomePlane
  ∧ preserves_outcome_partition({Obs1,Obs2},Obs12)

ReleaseCompose_U(Rel1,Rel2,Rel12) ⇔
  Rel1 ∈ ReleasePlane
  ∧ Rel2 ∈ ReleasePlane
  ∧ Rel12 ∈ ReleasePlane
  ∧ preserves_release_partition({Rel1,Rel2},Rel12)
  ∧ ¬defined(ctrl(Rel12))

Composable_U(J1,J2) ⇔
  defined(J1 ⊙_U J2)
  ∧ WF(J1)
  ∧ WF(J2)
  ∧ support(Σ(J1) ∪ Σ(J2)) ⊆ CallerFrontier
  ∧ support(Π(J1) ∪ Π(J2)) ⊆ CallerPolicy
  ∧ (support(Σ(J1) ∪ Σ(J2) ∪ Π(J1) ∪ Π(J2)) ∩ support(B) = ∅)
  ∧ pairwise_disjoint(support(B),support(C),support(K))
  ∧ affine(Θ(J1),Θ'(J1))
  ∧ affine(Θ(J2),Θ'(J2))
  ∧ affine(Θ(J1),Θ'(J2))
  ∧ OutcomeCompose_U(Obs(J1),Obs(J2),Obs(J1 ⊙_U J2))
  ∧ ReleaseCompose_U(Rel(J1),Rel(J2),Rel(J1 ⊙_U J2))
  ∧ ∀ f ∈ boundary_facts(J1) ∪ boundary_facts(J2).
      single_stratum_owner(f)

composition_fact_closed_U(J1,J2) ⇔
  boundary_facts(J1) ∪ boundary_facts(J2) ⊆ boundary_facts(J1 ⊙_U J2)
  ∧ ∀ f ∈ boundary_facts(J1 ⊙_U J2).
      single_stratum_owner(f)
      ∧ (f ∈ boundary_facts(J1) ∪ boundary_facts(J2)
         ∨ derived_by_composition_rule(f,J1,J2))

ProvableComposability_U(J1,J2) ⇔
  Composable_U(J1,J2)
  ∧ WF(J1 ⊙_U J2)
  ∧ support(Σ(J1 ⊙_U J2)) ⊆ CallerFrontier
  ∧ support(Π(J1 ⊙_U J2)) ⊆ CallerPolicy
  ∧ composition_fact_closed_U(J1,J2)
  ∧ ∀ w ∈ World_U.
      defined(⟦J1⟧^U_w) ∧ defined(⟦J2⟧^U_w)
        → defined(⟦J1 ⊙_U J2⟧^U_w)

FormalizationGuideFile_U =
  { path("uring/agents/formalization/overview.md")
  , path("uring/agents/formalization/notation.md")
  , path("uring/agents/formalization/kernel-boundary.md")
  , path("uring/agents/formalization/outcomes.md")
  , path("uring/agents/formalization/resources.md")
  , path("uring/agents/formalization/sessions.md")
  , path("uring/agents/formalization/handler.md")
  , path("uring/agents/formalization/go-mapping.md")
  , path("uring/agents/formalization/guarantees.md")
  , path("uring/agents/formalization/verification.md")
  }

GuideJudgment_U : FormalizationGuideFile_U ⇀ JudgmentSurface
GuideJudgment_U(f) = guide_judgment(f)
composable_topic_order ⊆ FormalizationGuideFile_U × FormalizationGuideFile_U
FormalizationGuideComposable_U ⇔
  ∀ f1,f2 ∈ FormalizationGuideFile_U.
    composable_topic_order(f1,f2) →
      ProvableComposability_U(GuideJudgment_U(f1),GuideJudgment_U(f2))

WF(J) ⇔
  pairwise_disjoint(support(B),support(C),support(K))
  ∧ support(C) = CallerFrontier ∪ CallerPolicy
  ∧ support(Σ(J)) ⊆ CallerFrontier
  ∧ support(Π(J)) ⊆ CallerPolicy
  ∧ frontier_owner(Σ(J)) = C
  ∧ policy_owner(Π(J)) = C
  ∧ affine(Θ(J),Θ'(J))
  ∧ Obs(J) ∈ OutcomePlane
  ∧ Rel(J) ∈ ReleasePlane
  ∧ ∀ f ∈ boundary_facts(J). single_stratum_owner(f)

JudgmentSurface =
  { J | WF(J)
        ∧ Obs(J) ∈ OutcomePlane
        ∧ Rel(J) ∈ ReleasePlane
        ∧ support(Σ(J)) ⊆ CallerFrontier
        ∧ support(Π(J)) ⊆ CallerPolicy }

denotation_entry_sound(J,w) ⇔
  J ∈ JudgmentSurface
  ∧ w ∈ World_U
  ∧ defined(⟦J⟧^U_w)
  ∧ π_boundary(⟦J⟧^U_w) = π_boundary(J)
  ∧ π_caller(⟦J⟧^U_w) = π_caller(J)
  ∧ support(π_caller(⟦J⟧^U_w)) ⊆ CallerFrontier ∪ CallerPolicy

admissible(J) ⇔ WF(J) ∧ ¬hidden(CallerFrontier ∪ CallerPolicy, support(B))
```
