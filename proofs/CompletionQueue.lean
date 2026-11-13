-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import Std

/-! Logical CQ positions for claimCQ, wait/waitBatch and publishCQ.
The kernel respects capacity; a successful claim grants exclusive copying.
Failed CAS and backoff stutter. Atomic ordering is a refinement premise. -/
namespace Uring.CompletionQueue

inductive Phase where
  | idle | claimed | copied
  deriving DecidableEq

structure State where
  head : Nat := 0
  tail : Nat := 0
  count : Nat := 0
  phase : Phase := .idle
  copies : List Nat := []

inductive Event where
  | produce (count : Nat)
  | claim (limit : Nat)
  | copy
  | publish

def step (capacity : Nat) (s : State) (e : Event) : State :=
  match e with
  | .produce n =>
      if s.tail + n ≤ s.head + capacity then { s with tail := s.tail + n } else s
  | .claim limit =>
      if s.phase = .idle ∧ 0 < min limit (s.tail - s.head) then
        { s with count := min limit (s.tail - s.head), phase := .claimed } else s
  | .copy =>
      if s.phase = .claimed then
        { s with phase := .copied, copies := s.copies ++ List.range' s.head s.count } else s
  | .publish =>
      if s.phase = .copied then
        { s with head := s.head + s.count, count := 0, phase := .idle } else s

def runFrom (capacity : Nat) : State → List Event → State
  | s, [] => s
  | s, e :: es => runFrom capacity (step capacity s e) es

def run (capacity : Nat) := runFrom capacity {}

def copiedUntil (s : State) := s.head + if s.phase = .copied then s.count else 0

private def valid (capacity : Nat) (s : State) : Prop :=
  s.head ≤ s.tail ∧ s.tail ≤ s.head + capacity ∧ s.count ≤ s.tail - s.head ∧
  (if s.phase = .idle then s.count = 0 else 0 < s.count) ∧
  s.copies = List.range' 0 (copiedUntil s)

private theorem step_valid (capacity : Nat) (s : State) (e : Event)
    (h : valid capacity s) : valid capacity (step capacity s e) := by
  rcases s with ⟨head, tail, count, phase, copies⟩
  cases phase <;> cases e <;> simp_all [step, valid, copiedUntil]
  all_goals try (split <;> simp_all)
  all_goals try omega
  all_goals simpa only [Nat.zero_add] using (List.range'_append_1 (s := 0) (m := head) (n := count))

private theorem run_valid_from (capacity : Nat) (es : List Event) (s : State)
    (h : valid capacity s) : valid capacity (runFrom capacity s es) := by
  induction es generalizing s with
  | nil => exact h
  | cons e es ih => exact ih _ (step_valid _ _ _ h)

private theorem run_valid (capacity : Nat) (es : List Event) : valid capacity (run capacity es) :=
  run_valid_from capacity es {} (by simp [valid, copiedUntil])

/-- Every copied logical position occurs once, in producer order, without gaps. -/
theorem copied_prefix (capacity : Nat) (es : List Event) :
    (run capacity es).copies = List.range' 0 (copiedUntil (run capacity es)) :=
  (run_valid capacity es).2.2.2.2

theorem no_duplicate_copies (capacity : Nat) (es : List Event) :
    (run capacity es).copies.Nodup := by
  rw [copied_prefix]
  exact List.nodup_range'

theorem publication_follows_copy (capacity : Nat) (es : List Event) :
    (run capacity es).head ≤ (run capacity es).copies.length := by
  rw [copied_prefix, List.length_range']
  unfold copiedUntil
  omega

theorem copies_are_published_by_kernel (capacity : Nat) (es : List Event) :
    copiedUntil (run capacity es) ≤ (run capacity es).tail := by
  have h := run_valid capacity es
  unfold valid at h
  unfold copiedUntil
  split <;> omega

/-- uint32 subtraction recovers the logical distance even across counter wrap. -/
theorem distance_across_wrap (head tail : Nat) (ordered : head ≤ tail)
    (bounded : tail - head < 4294967296) :
    (tail % 4294967296 + 4294967296 - head % 4294967296) % 4294967296 = tail - head := by
  omega

/-- The 63-bit claim sequence excludes stale equality only within this horizon. -/
theorem claim_sequence_unique (before after : Nat) (ordered : before ≤ after)
    (horizon : after - before < 9223372036854775808)
    (same : before % 9223372036854775808 = after % 9223372036854775808) :
    before = after := by
  omega

end Uring.CompletionQueue
