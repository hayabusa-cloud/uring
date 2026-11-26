-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import Std

/-! Slot accounting for ordinary receive bundles with uniform buffer size.
The kernel consumes a prefix of the publication queue. The caller derives
which slots to hold from each matched completion. Recycling moves a held slot
to the tail of the queue. A slot may stay held throughout the history. -/
namespace Uring.Bundle

def payloadCount (size : Nat) (result : Int) : Nat :=
  if result ≤ 0 ∨ size = 0 then 0 else 1 + (result.toNat - 1) / size

def slotCount (size : Nat) (result : Int) (selected retained : Bool) : Nat :=
  if size = 0 ∨ retained then 0
  else if result ≤ 0 ∧ selected then 1 else payloadCount size result

theorem empty_slots (hs : size ≠ 0) (hr : result ≤ 0) (selected retained : Bool) :
    slotCount size result selected retained = (if selected && !retained then 1 else 0) := by
  cases selected <;> cases retained <;> simp [slotCount, payloadCount, hs, hr]

theorem bytes_alone_insufficient (hs : size ≠ 0) (hr : result ≤ 0) (decode : Int → Nat) :
    ¬ (decode result = slotCount size result false false ∧
       decode result = slotCount size result true false) := by
  simp [empty_slots hs hr]
  omega

/-- A positive receive fills `full` slots followed by one slot carrying 1 through `size` bytes. -/
theorem positive_slots (full : Nat) {size lastBytes : Nat}
    (hpos : 0 < lastBytes) (hsize : lastBytes ≤ size) :
    slotCount size (.ofNat (full * size + lastBytes)) true false = full + 1 := by
  have hs : 0 < size := Nat.lt_of_lt_of_le hpos hsize
  have decode (bytes : Nat) (hb : 0 < bytes) :
      slotCount size (bytes : Int) true false = 1 + (bytes - 1) / size := by
    simp [slotCount, payloadCount, Nat.ne_of_gt hs, Nat.ne_of_gt hb]
  calc
    _ = 1 + (full * size + lastBytes - 1) / size := decode _ (by omega)
    _ = full + 1 := by
      rw [show full * size + lastBytes - 1 = (lastBytes - 1) + full * size by omega,
          Nat.add_mul_div_right _ _ hs, Nat.div_eq_of_lt (show lastBytes - 1 < size by omega)]
      omega

/-- The decoded count selects exactly the consumed IDs, in publication order. -/
theorem positive_prefix (full : List Nat) (last : Nat) (tail : List Nat) {size lastBytes : Nat}
    (hpos : 0 < lastBytes) (hsize : lastBytes ≤ size) :
    (full ++ last :: tail).take
      (slotCount size (.ofNat (full.length * size + lastBytes)) true false) = full ++ [last] := by
  rw [positive_slots full.length hpos hsize]
  simpa [List.append_assoc] using (List.take_left (l₁ := full ++ [last]) (l₂ := tail))

structure Pool where
  available : List Nat
  held : List Nat

def Pool.inventory (p : Pool) := p.available ++ p.held

inductive Step (size : Nat) : Pool → Pool → Prop where
  | unselected (available held : List Nat) (r : Int) (hr : r ≤ 0) :
      Step size ⟨available, held⟩
        ⟨available, available.take (slotCount size r false false) ++ held⟩
  | selected (id : Nat) (tail held : List Nat) (r : Int) (hr : r ≤ 0) :
      Step size ⟨id :: tail, held⟩
        ⟨tail, (id :: tail).take (slotCount size r true false) ++ held⟩
  | payload (full : List Nat) (last : Nat) (tail held : List Nat) (lastBytes : Nat)
      (hpos : 0 < lastBytes) (hsize : lastBytes ≤ size) :
      Step size ⟨full ++ last :: tail, held⟩
        ⟨tail, (full ++ last :: tail).take
          (slotCount size (.ofNat (full.length * size + lastBytes)) true false) ++ held⟩
  | recycle (id : Nat) (available before after : List Nat) :
      Step size ⟨available, before ++ id :: after⟩ ⟨available ++ [id], before ++ after⟩

theorem step_inventory (hs : size ≠ 0) (step : Step size p q) :
    p.inventory.Perm q.inventory := by
  cases step with
  | unselected available held r hr => simp [Pool.inventory, empty_slots hs hr]
  | selected id tail held r hr =>
    simpa [Pool.inventory, empty_slots hs hr] using
      (List.perm_middle (a := id) (l₁ := tail) (l₂ := held)).symm
  | payload full last tail held lastBytes hpos hsize =>
    simp only [Pool.inventory, positive_prefix full last tail hpos hsize]
    simpa [List.append_assoc] using List.perm_append_comm_assoc (full ++ [last]) tail held
  | recycle id available before after =>
    simpa [Pool.inventory, List.append_assoc] using
      (List.perm_middle (a := id) (l₁ := before) (l₂ := after)).append_left available

inductive History (size : Nat) : Pool → Pool → Prop where
  | start (p : Pool) : History size p p
  | next : History size p q → Step size q s → History size p s

/-- Every ID retains its multiplicity across empty and positive receives and deferred returns. -/
theorem inventory_preserved (hs : size ≠ 0) (trace : History size p q) :
    p.inventory.Perm q.inventory := by
  induction trace with
  | start => exact .refl _
  | next _ step ih => exact ih.trans (step_inventory hs step)

end Uring.Bundle
