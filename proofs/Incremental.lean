-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import Std

/-! Per-buffer accounting in incremental_linux.go. CQEs are dispatched once,
in CQ order; registration and machine-representable geometry are premises.
Natural-number intervals describe borrowed views, not ownership transfers. -/
namespace Uring.Incremental

abbrev Offsets := Nat → Nat

structure CQE where
  result : Int
  id : Nat
  hasBuffer : Bool
  bufferMore : Bool

structure Fragment where
  id : Nat
  offset : Nat
  length : Nat

def admissible (size entries : Nat) (offsets : Offsets) (e : CQE) : Prop :=
  0 < e.result ∧ e.hasBuffer = true ∧ e.id < entries ∧
  offsets e.id ≤ size ∧ e.result.toNat ≤ size - offsets e.id

instance (size entries : Nat) (offsets : Offsets) (e : CQE) :
    Decidable (admissible size entries offsets e) := inferInstanceAs (Decidable (_ ∧ _))

def fragment (size entries : Nat) (offsets : Offsets) (e : CQE) : Option Fragment :=
  if admissible size entries offsets e then
    some ⟨e.id, offsets e.id, e.result.toNat⟩ else none

def set (offsets : Offsets) (id value : Nat) : Offsets :=
  fun i => if i = id then value else offsets i

def advance (size entries : Nat) (offsets : Offsets) (e : CQE) : Offsets :=
  if e.result ≤ 0 then
    if e.hasBuffer = true ∧ e.bufferMore = false ∧ e.id < entries then set offsets e.id 0
    else offsets
  else if admissible size entries offsets e then
    set offsets e.id (if e.bufferMore then offsets e.id + e.result.toNat else 0)
  else offsets

def runFrom (size entries : Nat) : Offsets → List CQE → Offsets
  | offsets, [] => offsets
  | offsets, e :: es => runFrom size entries (advance size entries offsets e) es

private theorem fragment_spec (size entries : Nat) (offsets : Offsets) (e : CQE)
    (f : Fragment) (h : fragment size entries offsets e = some f) :
    admissible size entries offsets e ∧
    f = ⟨e.id, offsets e.id, e.result.toNat⟩ := by
  unfold fragment at h
  split at h <;> simp_all

private theorem advance_bounded (size entries : Nat) (offsets : Offsets) (e : CQE)
    (h : ∀ i, offsets i ≤ size) : ∀ i, advance size entries offsets e i ≤ size := by
  intro i
  unfold advance
  split
  · split
    · unfold set; split <;> simp_all
    · exact h i
  · split
    · rename_i valid
      have bounds := valid.2.2.2
      unfold set
      split
      · subst i
        split <;> omega
      · exact h i
    · exact h i

theorem offsets_bounded (size entries : Nat) (es : List CQE) :
    ∀ i, runFrom size entries (fun _ => 0) es i ≤ size := by
  have preserve : ∀ es offsets, (∀ i, offsets i ≤ size) →
      ∀ i, runFrom size entries offsets es i ≤ size := by
    intro events
    induction events with
    | nil => intro offsets h; exact h
    | cons e es ih => intro offsets h; exact ih _ (advance_bounded _ _ _ _ h)
  exact preserve es _ (by simp)

theorem other_ids_unchanged (size entries : Nat) (offsets : Offsets) (e : CQE)
    (i : Nat) (h : i ≠ e.id) : advance size entries offsets e i = offsets i := by
  unfold advance
  split <;> split <;> simp [set, h]

/-- The selected fragment fits its slot and the registered backing allocation. -/
theorem fragment_bounds (size entries backing : Nat) (offsets : Offsets) (e : CQE)
    (f : Fragment) (h : fragment size entries offsets e = some f)
    (registered : entries * size ≤ backing) :
    f.id < entries ∧ f.offset + f.length ≤ size ∧
    f.id * size + f.offset + f.length ≤ (f.id + 1) * size ∧
    f.id * size + f.offset + f.length ≤ backing := by
  obtain ⟨valid, rfl⟩ := fragment_spec _ _ _ _ _ h
  obtain ⟨_, _, idBound, offsetBound, lengthBound⟩ := valid
  have within : offsets e.id + e.result.toNat ≤ size := by omega
  have slot : e.id * size + offsets e.id + e.result.toNat ≤ (e.id + 1) * size := by
    simpa [Nat.add_mul, Nat.add_assoc] using Nat.add_le_add_left within (e.id * size)
  have allocation := Nat.mul_le_mul_right size (show e.id + 1 ≤ entries by omega)
  exact ⟨idBound, within, slot, Nat.le_trans slot (Nat.le_trans allocation registered)⟩

/-- Retention advances to the fragment end; return resets that ID for reuse. -/
theorem selected_offset (size entries : Nat) (offsets : Offsets) (e : CQE)
    (f : Fragment) (h : fragment size entries offsets e = some f) :
    advance size entries offsets e e.id =
      if e.bufferMore then f.offset + f.length else 0 := by
  obtain ⟨valid, rfl⟩ := fragment_spec _ _ _ _ _ h
  have positive := valid.1
  simp [advance, set, valid, show ¬e.result ≤ 0 by omega]

theorem retained_adjacency (size entries : Nat) (offsets : Offsets) (e next : CQE)
    (first second : Fragment) (h1 : fragment size entries offsets e = some first)
    (retained : e.bufferMore = true) (sameID : next.id = e.id)
    (h2 : fragment size entries (advance size entries offsets e) next = some second) :
    second.offset = first.offset + first.length := by
  have updated := selected_offset _ _ _ _ _ h1
  obtain ⟨_, rfl⟩ := fragment_spec _ _ _ _ _ h2
  simpa [sameID, retained] using updated

theorem nonpositive_return (size entries : Nat) (offsets : Offsets) (e : CQE)
    (h : e.result ≤ 0) (selected : e.hasBuffer = true) (returned : e.bufferMore = false)
    (validID : e.id < entries) : advance size entries offsets e e.id = 0 := by
  simp [advance, set, h, selected, returned, validID]

theorem nonpositive_retained (size entries : Nat) (offsets : Offsets) (e : CQE)
    (h : e.result ≤ 0) (retained : e.bufferMore = true) :
    advance size entries offsets e = offsets := by
  simp [advance, h, retained]

end Uring.Incremental
