# Go Mapping

The model in the other topic files only matters because it maps faithfully to real Go. This file is that bridge, in both directions. *Abstraction* lifts a Go surface up into a judgment so you can reason about it; *compilation* sends a judgment back down to Go and demands that the round trip preserve denotation and never smuggle caller frontier or policy into the boundary. When you change `code.hybscloud.com/uring` code, this is the file that says your change still *means* what the model says it means: same outcomes, same release evidence, ownership and epochs intact, nothing hidden in the boundary.

## Abstraction: Go To Guide

The block lifts a `GoSurface` into a `JudgmentSurface` (`lift = ⟦·⟧⁻¹_U`) and states what that lift must preserve. Read it when you start from existing Go and need the judgment it denotes.

```text
module = pkg("code.hybscloud.com/uring")
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

GoSurface = { g | public_go_term_caller(module, g) }
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
  {J | WF(J)
       ∧ Obs(J) ∈ OutcomePlane
       ∧ Rel(J) ∈ ReleasePlane
       ∧ support(Σ(J)) ⊆ CallerFrontier
       ∧ support(Π(J)) ⊆ CallerPolicy}
World_U = { w | world_for(module, w) }
Denotation_U(w) = { d | denotation_at(module, w, d) }

⟦·⟧⁻¹_U : GoSurface ⇀ JudgmentSurface
⟦·⟧_U   : JudgmentSurface ⇀ GoSurface
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

lift(go_surface) = ⟦go_surface⟧⁻¹_U
compile_to_go(J) = ⟦J⟧_U

J =
  Γ | Θ ⊢ action @ ring : result | Δ ▷ Θ' ; χ ; Σ ; Μ ; Φ ; Obs ; Rel ; Π

π_boundary(J) = {Δ(J), Θ(J), Θ'(J), χ(J), Μ(J), Φ(J), Obs(J), Rel(J)}
π_caller(J) = {Σ(J), Π(J)}

DesignDirection(J) ⇔
  J ∈ JudgmentSurface
  ∧ normal_form(J)
  ∧ defined(⟦J⟧_U)

ReviewDirection(go_surface) ⇔
  go_surface ∈ GoSurface
  ∧ BoundaryClean(go_surface)
  ∧ defined(⟦go_surface⟧⁻¹_U)
  ∧ WF(⟦go_surface⟧⁻¹_U)
  ∧ normal_form(⟦go_surface⟧⁻¹_U)

Roundtrip1(J) ⇔
  DesignDirection(J)
  ∧ defined(⟦⟦J⟧_U⟧⁻¹_U)
  ∧ ⟦⟦J⟧_U⟧⁻¹_U ≃_B J

Roundtrip2(go_surface) ⇔
  ReviewDirection(go_surface)
  ∧ defined(⟦⟦go_surface⟧⁻¹_U⟧_U)
  ∧ ⟦⟦go_surface⟧⁻¹_U⟧_U ≃_pub go_surface

BoundaryEquiv(J1,J2) ⇔
  same(action,J1,J2)
  ∧ same(Θ,Θ',J1,J2)
  ∧ same(χ,J1,J2)
  ∧ same(Μ,J1,J2)
  ∧ same(Obs,J1,J2)
  ∧ same(Rel,J1,J2)
  ∧ same(frontier_owner,J1,J2)
  ∧ same(policy_owner,J1,J2)
  ∧ same(package_owner,boundary_facts(J1),boundary_facts(J2))

J1 ≃_B J2 ⇔ BoundaryEquiv(J1,J2)
go1 ≃_pub go2 ⇔
  preserves(go1,public_behavior(go2))
  ∧ preserves(go2,public_behavior(go1))

BoundaryPair(J,go_surface) ⇔
  J ∈ JudgmentSurface
  ∧ go_surface ∈ GoSurface
  ∧ defined(⟦J⟧_U)
  ∧ defined(⟦go_surface⟧⁻¹_U)
  ∧ BoundaryClean(go_surface)
  ∧ J ≃_B ⟦go_surface⟧⁻¹_U
  ∧ ⟦J⟧_U ≃_pub go_surface

abstraction_sound(go_surface) ⇔ ReviewDirection(go_surface) ∧ Roundtrip2(go_surface)
compilation_roundtrip(J) ⇔ DesignDirection(J) ∧ Roundtrip1(J)

⟦τ⟧^U_w ⊆ Val_U(τ) × Val_U(τ)
⟦Eff[τ]⟧^{U,c}_w ⊆ Comp_U(τ) × Comp_U(τ)

denotation_respects_boundary(J,w) ⇔
  J ∈ JudgmentSurface
  ∧ defined(⟦J⟧^U_w)
  ∧ π_boundary(⟦J⟧^U_w) = π_boundary(J)
  ∧ π_caller(⟦J⟧^U_w) = π_caller(J)
```

## Compilation: Guide To Go

The block compiles a judgment back to Go (`compile_to_go = ⟦·⟧_U`) and pins the round-trip and out-of-boundary obligations. Read it when you start from the model and need to know what a correct Go realization may and may not do — in particular, that no caller frontier or policy may land inside the boundary.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D

GoSurface = { g | public_go_term_caller(module,g) }
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
compile_to_go(J) = ⟦J⟧_U
boundary_go : GoSurface ⇀ GoSurface
caller_go    : GoSurface ⇀ GoSurface
support(go_surface) =
  support(boundary_go(go_surface)) ∪ support(caller_go(go_surface))
BoundaryClean(go_surface) ⇔
  go_surface ∈ GoSurface
  ∧ support(boundary_go(go_surface)) ∩ (CallerFrontier ∪ CallerPolicy) = ∅
  ∧ support(caller_go(go_surface)) ⊆ CallerFrontier ∪ CallerPolicy
  ∧ ¬hidden(CallerFrontier ∪ CallerPolicy, support(go_surface))

compile_allowed(J) ⇔
  J ∈ JudgmentSurface
  ∧ normal_form(J)
  ∧ complete(prove,J)
  ∧ Obs(J) ∈ OutcomePlane
  ∧ Rel(J) ∈ ReleasePlane
  ∧ support(Π(J)) ⊆ CallerPolicy
  ∧ support(Σ(J)) ⊆ CallerFrontier
  ∧ one_boundary_action(action(J))

compile_image(J,h) ⇔
  compile_allowed(J)
  ∧ h = helper(action(J))
  ∧ helper_action(h) = action(J)
  ∧ one_boundary_action(action(J))
  ∧ encodes(h, action(J))

defined(⟦J⟧_U) → compile_image(J,⟦J⟧_U)
compile_allowed(J) → defined(compile_to_go(J))

compile_denotation_preserved(J,w) ⇔
  compile_allowed(J)
  ∧ w ∈ World_U
  ∧ defined(⟦J⟧^U_w)
  ∧ defined(compile_to_go(J))
  ∧ defined(lift(compile_to_go(J)))
  ∧ defined(⟦lift(compile_to_go(J))⟧^U_w)
  ∧ π_boundary(lift(compile_to_go(J))) = π_boundary(J)
  ∧ π_caller(lift(compile_to_go(J))) = π_caller(J)
  ∧ π_boundary(⟦lift(compile_to_go(J))⟧^U_w) = π_boundary(⟦J⟧^U_w)
  ∧ π_caller(⟦lift(compile_to_go(J))⟧^U_w) = π_caller(⟦J⟧^U_w)

CompileSubmit(J) ⇔
  defined(⟦J⟧_U)
  ∧ action(J)=submit
  ∧ ⟦J⟧_U = helper(submit)
  ∧ emits(Encode(req,ctx))
  ∧ no_loop(helper)
  ∧ no_retry(helper)

CompileObserve(J) ⇔
  defined(⟦J⟧_U)
  ∧ action(J)∈{wait,observe,decode}
  ∧ ⟦J⟧_U = helper(action(J))
  ∧ reads(cqe)
  ∧ (operation_cqe(cqe) → projects(CQEToObs(cqe,ctx,r,χ(J))))
  ∧ (release_ready_carrier(cqe,ctx,r) → projects(ReleaseProjection(cqe,ctx,r,χ(J))))
  ∧ preserves(cqe.Res,cqe.Flags,cqe.user_data)

CompileResource(J) ⇔
  preserves(Θ(J),Θ'(J))
  ∧ no_hidden_pool_policy(helper)
  ∧ no_hidden_gc_root(helper)

CompilePolicy(J) ⇔
  defined(⟦J⟧_U)
  ∧ ⟦J⟧_U ∈ GoSurface
  ∧ ∀ p ∈ support(Π(J)). implemented_in_caller_layer(module,p)
  ∧ ∀ f ∈ support(Σ(J)). implemented_in_caller_layer(module,f)
  ∧ BoundaryClean(⟦J⟧_U)

compile_total_on_boundary(J) ⇔
  compile_allowed(J)
  → defined(⟦J⟧_U)

compilation_sound(J) ⇔
  compile_allowed(J)
  ∧ DesignDirection(J)
  ∧ Roundtrip1(J)
  ∧ ∃ w ∈ World_U. compile_denotation_preserved(J,w)
  ∧ CompilePolicy(J)

reject(defined(⟦J⟧_U) ∧ hidden(CallerFrontier ∪ CallerPolicy, support(⟦J⟧_U)))
reject(defined(⟦J⟧_U) ∧ (support(boundary_go(⟦J⟧_U)) ∩ (CallerFrontier ∪ CallerPolicy) ≠ ∅))
```
