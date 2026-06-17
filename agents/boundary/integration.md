# Integration Boundary

Using `uring` correctly means using it *with* its neighbours, each in its own lane, and never letting one package's job leak into another's. This file is the map of who owns what. `OwnerMap` assigns every role to exactly one package: outcome and stream vocabulary to `iox`, descriptors to `iofd`, addresses to `sock`, buffers to `iobuf`, suspension and one-shot resumption to `kont`, protocol frontiers to `sess`, route and subscription frontiers to `takt`, and so on. The rules then pin which facts may be projected across the package boundary and which — caller frontier, caller policy, `cove` requirement evidence, resumptions, mailbox policy, and completion-memory policy — must stay strictly in caller code, and require that every action go through the public surface rather than a bypass.

In plain terms, the rules an agent follows here are:

- Every role has exactly one owner, and you read that owner before using its facts. Outcome, stream, datagram, and byte-progress vocabulary belong to `iox`; syscall result and errno to `zcall`; descriptors to `iofd`; socket addresses to `sock`; buffers to `iobuf`; spin, yield, and lock to `spin`; the caller mailbox queue to `lfq`; frame boundaries to `framer`; context, requirement, and safety evidence to `cove`; suspension and one-shot resumption to `kont`; protocol frontiers to `sess`; and route, subscription, runner-movement, and completion-memory vocabulary to `takt`.
- Only owned boundary inputs may cross into the boundary. The boundary-projectable carriers are the outcome contract, descriptor authority, socket address, buffer fact, spin/yield fact, and frame boundary; nothing else is projected across.
- Caller frontier and caller policy never cross into the boundary. Context, requirement, and safety evidence from `cove` may guard caller decisions and may justify copied boundary inputs, but the `cove` carriers themselves stay in caller code. Resumptions, mailbox policy, and completion-memory facts and policy also stay strictly in caller code, and projecting any of them into the boundary is rejected.
- Every action goes through the public surface. An action that bypasses the public surface is rejected.

The block below states these rules formally.

```text
module = pkg("code.hybscloud.com/uring")
B = boundary(module)
A = above(module)
D = beyond(module)
C = caller(module) = A ∪ D
K = below(module)
World_U = { w | world_for(module,w) }
Denotation_U(w) = { d | denotation_at(module,w,d) }
⟦·⟧^U_w : JudgmentSurface ⇀ Denotation_U(w)

J_int =
  Γ_int | Θ_int ⊢ integration_action @ ring : result | Δ_int ▷ Θ_int'
    ; χ_int ; Σ_int ; Μ_int ; Φ_int ; Obs_int ; Rel_int ; Π_int
Obs_int ∈ OutcomePlane
Rel_int ∈ ReleasePlane
denote_integration_boundary(w) ⇔
  w ∈ World_U
  ∧ J_int ∈ JudgmentSurface
  ∧ defined(⟦J_int⟧^U_w)

OwnerMap =
  { outcome_contract ↦ pkg("code.hybscloud.com/iox")
  , stream_contract ↦ pkg("code.hybscloud.com/iox")
  , stream_frontier_vocabulary ↦ pkg("code.hybscloud.com/iox")
  , datagram_contract ↦ pkg("code.hybscloud.com/iox")
  , outcome_control_vocabulary ↦ pkg("code.hybscloud.com/iox")
  , byte_progress_contract ↦ pkg("code.hybscloud.com/iox")
  , syscall_result_errno ↦ pkg("code.hybscloud.com/zcall")
  , descriptor_authority ↦ pkg("code.hybscloud.com/iofd")
  , socket_address_authority ↦ pkg("code.hybscloud.com/sock")
  , buffer_authority ↦ pkg("code.hybscloud.com/iobuf")
  , spin_wait_yield_lock ↦ pkg("code.hybscloud.com/spin")
  , caller_mailbox_queue ↦ pkg("code.hybscloud.com/lfq")
  , frame_boundary ↦ pkg("code.hybscloud.com/framer")
  , context_requirement_safety_evidence ↦ pkg("code.hybscloud.com/cove")
  , suspension_frontier_vocabulary ↦ pkg("code.hybscloud.com/kont")
  , one_shot_resumption ↦ pkg("code.hybscloud.com/kont")
  , affine_resumption ↦ pkg("code.hybscloud.com/kont")
  , protocol_frontier_vocabulary ↦ pkg("code.hybscloud.com/sess")
  , route_frontier_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , subscription_frontier_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , runner_movement_vocabulary ↦ pkg("code.hybscloud.com/takt")
  , completion_memory_vocabulary ↦ pkg("code.hybscloud.com/takt")
  }

OwnerRole = dom(OwnerMap)
OwnerPackages_int = {pkg | ∃ role ∈ OwnerRole. OwnerMap(role) = pkg}
role_owner : OwnerRole → OwnerPackages_int
∀ role ∈ OwnerRole. role_owner(role) = OwnerMap(role)
frontier_vocabulary(suspension_frontier) = suspension_frontier_vocabulary
frontier_vocabulary(stream_frontier) = stream_frontier_vocabulary
frontier_owner(stream_frontier) = C
frontier_vocabulary(protocol_frontier) = protocol_frontier_vocabulary
frontier_vocabulary(route_frontier) = route_frontier_vocabulary
frontier_vocabulary(subscription_frontier) = subscription_frontier_vocabulary
frontier_vocabulary(runner_movement) = runner_movement_vocabulary

∀ role ∈ OwnerRole.
  used(role,J_int) →
    read_complete(role_owner(role))

zcall_result(pkg("code.hybscloud.com/zcall"),errno_or_value) ∈ support(K)
project(pkg("code.hybscloud.com/zcall"),B) =
  {typed_error(errno), raw_syscall_result(value)}

fd_authority(pkg("code.hybscloud.com/iofd"),fd) ∈ Carrier
socket_address_fact(pkg("code.hybscloud.com/sock"),addr) ∈ Carrier
buffer_fact(pkg("code.hybscloud.com/iobuf"),buf) ∈ Carrier
context_evidence(pkg("code.hybscloud.com/cove"),e) ∈ CallerFrontier
requirement_evidence(pkg("code.hybscloud.com/cove"),e) ∈ CallerFrontier
safety_evidence(pkg("code.hybscloud.com/cove"),e) ∈ CallerFrontier
context_evidence(pkg("code.hybscloud.com/cove"),e) ∈ support(C)
requirement_evidence(pkg("code.hybscloud.com/cove"),e) ∈ support(C)
safety_evidence(pkg("code.hybscloud.com/cove"),e) ∈ support(C)
context_evidence(pkg("code.hybscloud.com/cove"),e) ∉ support(B)
requirement_evidence(pkg("code.hybscloud.com/cove"),e) ∉ support(B)
safety_evidence(pkg("code.hybscloud.com/cove"),e) ∉ support(B)
spin_yield_fact(pkg("code.hybscloud.com/spin"),y) ∈ Carrier
q : type_symbol(pkg("code.hybscloud.com/lfq"),"Queue")
mailbox_policy(q) ∈ CallerPolicy
mailbox_policy(q) ∈ support(C)
mailbox_policy(q) ∉ support(B)
frame_boundary_fact(pkg("code.hybscloud.com/framer"),frame) ∈ Carrier
resumption_fact(pkg("code.hybscloud.com/kont"),k) ∈ CallerFrontier
resumption_fact(pkg("code.hybscloud.com/kont"),k) ∈ support(C)
resumption_fact(pkg("code.hybscloud.com/kont"),k) ∉ support(B)
m : type_symbol(pkg("code.hybscloud.com/takt"),"CompletionMemory")
completion_memory_fact(m) ∈ CallerFrontier
completion_memory_policy(m) ∈ CallerPolicy
completion_memory_fact(m) ∈ support(C)
completion_memory_policy(m) ∈ support(C)
completion_memory_fact(m) ∉ support(B)
completion_memory_policy(m) ∉ support(B)

BoundaryProjectableCarrier_int =
  {outcome_contract, fd_authority, socket_address_fact, buffer_fact,
   spin_yield_fact, frame_boundary_fact}

project(BoundaryProjectableCarrier_int,B) = BoundaryProjectableCarrier_int

project(pkg("code.hybscloud.com/iox"),B) =
  {outcome_contract, stream_contract, datagram_contract,
   outcome_control_vocabulary, byte_progress_contract}

caller_only = CallerFrontier ∪ CallerPolicy

support(Σ_int) ⊆ CallerFrontier
support(Π_int) ⊆ CallerPolicy
frontier_owner(Σ_int) = C
policy_owner(Π_int) = C
caller_only ⊆ support(C)
caller_only ∩ support(B) = ∅
reject(project(caller_only,B))
∀ e. reject(project(context_evidence(pkg("code.hybscloud.com/cove"),e),B))
∀ e. reject(project(requirement_evidence(pkg("code.hybscloud.com/cove"),e),B))
∀ e. reject(project(safety_evidence(pkg("code.hybscloud.com/cove"),e),B))
∀ k. reject(project(resumption_fact(pkg("code.hybscloud.com/kont"),k),B))

boundary_public_surface_only(integration_action)
bypass_public_surface(integration_action) → reject(integration_action)
```
