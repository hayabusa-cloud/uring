-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import Std

/-! Arithmetic model of the disjoint fields in ctx.go. Powers-of-two division,
remainder and addition represent the reviewed masks, shifts and disjoint ORs.
Pointer validity and lifetime are outside this representation model. -/
namespace Uring.Context

structure Fields where
  op : Fin 256
  flags : Fin 256
  group : Fin 65536
  fd : Fin 1073741824

def pack (f : Fields) : Nat :=
  f.op.val + 256 * f.flags.val + 65536 * f.group.val + 4294967296 * f.fd.val

def op (word : Nat) := word % 256
def flags (word : Nat) := word / 256 % 256
def group (word : Nat) := word / 65536 % 65536
def fd (word : Nat) := word / 4294967296 % 1073741824
def mode (word : Nat) := word / 4611686018427387904 % 4

theorem direct_roundtrip (f : Fields) :
    op (pack f) = f.op.val ∧ flags (pack f) = f.flags.val ∧
    group (pack f) = f.group.val ∧ fd (pack f) = f.fd.val ∧
    mode (pack f) = 0 ∧ pack f < 4611686018427387904 := by
  have := f.op.isLt
  have := f.flags.isLt
  have := f.group.isLt
  have := f.fd.isLt
  dsimp [pack, op, flags, group, fd, mode]
  omega

/-- Equal direct encodings cannot conflate different field tuples. -/
theorem direct_injective (a b : Fields) (same : pack a = pack b) : a = b := by
  have ha := direct_roundtrip a
  have hb := direct_roundtrip b
  rw [same] at ha
  have ho : a.op = b.op := Fin.ext (by omega)
  have hf : a.flags = b.flags := Fin.ext (by omega)
  have hg : a.group = b.group := Fin.ext (by omega)
  have hd : a.fd = b.fd := Fin.ext (by omega)
  cases a; cases b; simp_all

def withFlags (word : Nat) (value : Fin 256) : Nat :=
  word - flags word * 256 + value.val * 256

/-- Clearing and replacing flags preserves every other direct field. -/
theorem flags_replacement (f : Fields) (value : Fin 256) :
    withFlags (pack f) value = pack { f with flags := value } := by
  have recovered := (direct_roundtrip f).2.1
  unfold withFlags
  rw [recovered]
  dsimp [pack]
  omega

def encodeFD (value : Int) : Nat := (value % 1073741824).toNat
def decodeFD (bits : Nat) : Int :=
  if bits < 536870912 then Int.ofNat bits else Int.ofNat bits - 1073741824

/-- Direct FD recovery has the signed 30-bit domain, not all of int32. -/
theorem signed_fd_roundtrip (value : Int)
    (lower : -536870912 ≤ value) (upper : value < 536870912) :
    decodeFD (encodeFD value) = value := by
  have nonneg := Int.emod_nonneg value (by decide : (1073741824 : Int) ≠ 0)
  have bounded := Int.emod_lt_of_pos value (by decide : (0 : Int) < 1073741824)
  have cast : ((value % 1073741824).toNat : Int) = value % 1073741824 :=
    Int.toNat_of_nonneg nonneg
  unfold decodeFD encodeFD
  change (if (value % 1073741824).toNat < 536870912 then
    ((value % 1073741824).toNat : Int) else
    ((value % 1073741824).toNat : Int) - 1073741824) = value
  split <;> omega

abbrev Address := Fin 4611686018427387904
def indirect (address : Address) : Nat := address.val + 4611686018427387904
def extended (address : Address) : Nat := address.val + 9223372036854775808
def pointerBits (word : Nat) : Nat := word % 4611686018427387904

theorem pointer_roundtrip (address : Address) :
    pointerBits (indirect address) = address.val ∧ mode (indirect address) = 1 ∧
    pointerBits (extended address) = address.val ∧ mode (extended address) = 2 := by
  have := address.isLt
  dsimp [pointerBits, indirect, extended, mode]
  omega

theorem modes_disjoint (f : Fields) (a b : Address) :
    pack f ≠ indirect a ∧ pack f ≠ extended b ∧ indirect a ≠ extended b := by
  have := (direct_roundtrip f).2.2.2.2.2
  have := a.isLt
  have := b.isLt
  dsimp [indirect, extended]
  omega

end Uring.Context
