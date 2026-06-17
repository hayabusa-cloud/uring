# Verification

This is the file you finish on: how to show a change is correct before you call it done. It gives the staged loop in two directions — design (start from intent, formalize, prove, then compile to Go) and review (start from existing Go, lift it, then check) — and the single predicate `Verify(J, go_surface)` that bundles every piece of evidence a correct change must produce. Verification here is more than "tests pass": it is well-formedness, normal form, discharged obligations, a faithful round trip between judgment and Go, denotation that respects the boundary, composability with anything the change builds on, and the mechanical checks (gofmt, vet, escape analysis on hot paths, link reachability for the guide) — plus the named residual assumptions you are allowed to lean on.

In plain terms, the rules an agent follows here are:

- Verify in one of two directions. Design runs `analyze → formalize → typecheck → reduce → prove → compile_to_go → lift → verify`; review starts from existing Go and lifts it first.
- A correct change produces all of the evidence, not just passing tests. `Verify(J, go_surface)` bundles well-formedness, normal form, discharged obligations, a faithful round trip between judgment and Go, boundary-respecting denotation, composability, and the static checks (gofmt, vet, escape analysis on hot paths, link reachability for the guide).
- Residual assumptions are named, not hidden. Anything the change leans on that is not proved here is recorded explicitly in the verification-evidence record.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

VerificationDesign =
  analyze
  → formalize
  → typecheck
  → reduce
  → prove
  → compile_to_go
  → lift
  → verify
VerificationReview =
  analyze
  → lift
  → formalize
  → typecheck
  → reduce
  → prove
  → compile_to_go
  → verify

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
⟦·⟧_U   : JudgmentSurface ⇀ GoSurface
⟦·⟧⁻¹_U : GoSurface ⇀ JudgmentSurface
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

RealizationEvidence(J,go_surface) ⇔
  J ∈ JudgmentSurface
  ∧ go_surface ∈ GoSurface
  ∧ BoundaryClean(go_surface)
  ∧ ((defined(⟦J⟧_U) ∧ ⟦J⟧_U ≃_pub go_surface)
     ∨ (defined(⟦go_surface⟧⁻¹_U) ∧ ⟦go_surface⟧⁻¹_U ≃_B J))

RoundtripEvidenceFormal(J,go_surface) ⇔
  J ∈ JudgmentSurface
  ∧ go_surface ∈ GoSurface
  ∧ BoundaryClean(go_surface)
  ∧ defined(⟦J⟧_U)
  ∧ defined(⟦go_surface⟧⁻¹_U)
  ∧ defined(⟦⟦J⟧_U⟧⁻¹_U)
  ∧ defined(⟦⟦go_surface⟧⁻¹_U⟧_U)
  ∧ ⟦⟦J⟧_U⟧⁻¹_U ≃_B J
  ∧ ⟦⟦go_surface⟧⁻¹_U⟧_U ≃_pub go_surface

DenotationEvidence(J,w) ⇔
  J ∈ JudgmentSurface
  ∧ w ∈ World_U
  ∧ defined(⟦J⟧^U_w)
  ∧ denotation_respects_boundary(J,w)

DenotationCorrespondenceAt(J,go_surface,w) ⇔
  J ∈ JudgmentSurface
  ∧ go_surface ∈ GoSurface
  ∧ w ∈ World_U
  ∧ BoundaryClean(go_surface)
  ∧ defined(⟦J⟧^U_w)
  ∧ defined(⟦go_surface⟧⁻¹_U)
  ∧ defined(⟦⟦go_surface⟧⁻¹_U⟧^U_w)
  ∧ π_boundary(⟦⟦go_surface⟧⁻¹_U⟧^U_w) = π_boundary(⟦J⟧^U_w)
  ∧ π_caller(⟦⟦go_surface⟧⁻¹_U⟧^U_w) = π_caller(⟦J⟧^U_w)
  ∧ Obs(⟦go_surface⟧⁻¹_U) = Obs(J)
  ∧ Rel(⟦go_surface⟧⁻¹_U) = Rel(J)

ComposabilityEvidence(J1,J2,w) ⇔
  J1 ∈ JudgmentSurface
  ∧ J2 ∈ JudgmentSurface
  ∧ w ∈ World_U
  ∧ Composable_U(J1,J2)
  ∧ ProvableComposability_U(J1,J2)
  ∧ defined(⟦J1⟧^U_w)
  ∧ defined(⟦J2⟧^U_w)
  ∧ defined(⟦J1 ⊙_U J2⟧^U_w)
  ∧ preserves(π_boundary(⟦J1 ⊙_U J2⟧^U_w),
      {π_boundary(⟦J1⟧^U_w), π_boundary(⟦J2⟧^U_w)})
  ∧ preserves(π_caller(⟦J1 ⊙_U J2⟧^U_w),
      {π_caller(⟦J1⟧^U_w), π_caller(⟦J2⟧^U_w)})

FormalizationGuideEvidence ⇔
  P_formalization_guide_family
  ∧ ∀ f ∈ FormalizationGuideFile_U.
      GuideInternalWell_U(f)
  ∧ ∀ f1,f2 ∈ FormalizationGuideFile_U.
      composable_topic_order(f1,f2) →
        ∃ w ∈ World_U.
          ComposabilityEvidence(GuideJudgment_U(f1),GuideJudgment_U(f2),w)

Verify(J,go_surface) ⇔
  WF(J)
  ∧ normal_form(J)
  ∧ obligations_discharged(J)
  ∧ RealizationEvidence(J,go_surface)
  ∧ RoundtripEvidenceFormal(J,go_surface)
  ∧ ∃ w ∈ World_U. DenotationEvidence(J,w)
  ∧ ∃ w ∈ World_U. DenotationCorrespondenceAt(J,go_surface,w)
  ∧ preserves_release_observation(J,go_surface)
  ∧ static_checks_pass(go_surface)
  ∧ tests_pass(go_surface)
  ∧ P_roundtrip(J,go_surface)
  ∧ FormalizationGuideEvidence
  ∧ (∀ J_prev.
      uses_composed_boundary(J_prev,J) →
        ∃ w ∈ World_U. ComposabilityEvidence(J_prev,J,w))

static_checks(go_surface) =
  { gofmt
  , go_vet_if_available
  , allocation_escape_check_if_hot_path
  , link_reachability_if_guide
  }

static_checks_pass(go_surface) ⇔
  ∀ check ∈ static_checks(go_surface). passed(check,go_surface)

preserves_release_observation(J,go_surface) ⇔
  Rel(J) ∈ ReleasePlane
  ∧ (defined(⟦go_surface⟧⁻¹_U) → Rel(⟦go_surface⟧⁻¹_U) = Rel(J))
  ∧ (defined(⟦J⟧_U) ∧ defined(⟦⟦J⟧_U⟧⁻¹_U)
     → Rel(⟦⟦J⟧_U⟧⁻¹_U) = Rel(J))

tests(go_surface) =
  package_tests(go_surface) ∪ focused_boundary_tests(go_surface)

tests_pass(go_surface) ⇔
  ∀ test ∈ tests(go_surface). passed(test,go_surface)

ResidualAssumption =
  { kernel_capability_truth
  , documented_linux_abi_truth
  , scheduler_fairness_caller_boundary
  , device_completion_eventuality_if_claimed
  }

∀ a ∈ ResidualAssumption.
  used(a,J) → named(a) ∧ evidence(a)

VerificationEvidenceRecord(E) ⇔
  declares(E.artifact)
  ∧ declares(E.specification)
  ∧ declares(E.property_class)
  ∧ declares(E.model_or_semantics)
  ∧ declares(E.method)
  ∧ declares(E.toolchain)
  ∧ declares(E.assumptions)
  ∧ declares(E.fragment_or_bound)
  ∧ route(E) ⊆ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}

admit_verification_evidence_U(E,J) ⇔
  J ∈ JudgmentSurface
  ∧ VerificationEvidenceRecord(E)
  ∧ route(E) ⊆ {Γ,Θ,Δ,χ,Σ,Μ,Φ,Obs,Rel,Π}
  ∧ ∀ p ∈ route(E). projection_defined(J,p)
  ∧ conservative(E,J)
  ∧ ¬creates_new_plane(E,J)
  ∧ ¬hidden({coercion, ownership, scheduler, runner_memory, terminality},J)
  ∧ preserves(Obs(J) ∈ OutcomePlane)
  ∧ preserves(Rel(J) ∈ ReleasePlane)

verify_accept(J,go_surface) ⇔ Verify(J,go_surface) ∧ no_unproved_required_obligation(J)
```
