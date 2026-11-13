-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import Std

/-! Publication and retirement model for multishot.go and cancel_linux.go.
Events separate the publisher's recheck, preparation, admission and return.
A CQE may arrive after admission but before the submitting call returns.
Atomic-step correspondence and valid CQE causality are external premises. -/
namespace Uring.Cancellation

inductive Original where
  | active | cancelling | stopped
  deriving DecidableEq

inductive Publisher where
  | idle | gate | checked | prepared | admitted | finishing
  deriving DecidableEq

inductive Cancel where
  | none | prepared | outstanding | observed
  deriving DecidableEq

structure State where
  original : Original := .active
  publisher : Publisher := .idle
  cancel : Cancel := .none
  owned : Bool := true
  returns : Nat := 0

inductive Event where
  | acquire | recheck | prepare | accept | reject | returned | leave
  | originalCQE | cancelCQE | retire

def pending (c : Cancel) : Bool := c == .prepared || c == .outstanding

def step (s : State) (e : Event) : State :=
  match e with
  | .acquire =>
      if s.publisher == .idle then { s with publisher := .gate } else s
  | .recheck =>
      if s.publisher == .gate then
        { s with publisher := if s.original == .active then .checked else .finishing }
      else s
  | .prepare =>
      if s.publisher == .checked then
        { s with publisher := .prepared, cancel := .prepared } else s
  | .accept =>
      if s.publisher == .prepared then
        { s with publisher := .admitted, cancel := .outstanding } else s
  | .reject =>
      if s.publisher == .prepared then
        { s with publisher := .finishing, cancel := .none } else s
  | .returned =>
      if s.publisher == .admitted then
        { s with publisher := .finishing, original :=
            if s.original == .active then .cancelling else s.original }
      else s
  | .leave =>
      if s.publisher == .finishing then { s with publisher := .idle } else s
  | .originalCQE => { s with original := .stopped }
  | .cancelCQE =>
      if s.cancel == .outstanding then { s with cancel := .observed } else s
  | .retire =>
      if s.original == .stopped && s.publisher == .idle && !pending s.cancel && s.owned
      then { s with owned := false, returns := s.returns + 1 } else s

def runFrom : State → List Event → State
  | s, [] => s
  | s, e :: es => runFrom (step s e) es

def run := runFrom {}

private def coherent (o : Original) (p : Publisher) (c : Cancel) : Prop :=
  match p with
  | .idle | .gate =>
      match o with
      | .active => c = .none
      | .cancelling => c = .outstanding ∨ c = .observed
      | .stopped => c ≠ .prepared
  | .checked => c = .none ∧ o ≠ .cancelling
  | .prepared => c = .prepared ∧ o ≠ .cancelling
  | .admitted => (c = .outstanding ∨ c = .observed) ∧ o ≠ .cancelling
  | .finishing =>
      (c = .none ∧ o ≠ .cancelling) ∨
      ((c = .outstanding ∨ c = .observed) ∧ o ≠ .active)

private def safe (s : State) : Prop :=
  coherent s.original s.publisher s.cancel ∧
  s.returns = (if s.owned then 0 else 1) ∧
  (s.owned = false → s.original = .stopped ∧
    (s.cancel = .none ∨ s.cancel = .observed) ∧
    (s.publisher = .idle ∨ s.publisher = .gate ∨ s.publisher = .finishing))

set_option maxHeartbeats 2000000 in
private theorem step_safe (s : State) (e : Event) (h : safe s) : safe (step s e) := by
  rcases s with ⟨o, p, c, owned, n⟩
  cases o <;> cases p <;> cases c <;> cases owned <;>
    simp_all [safe, coherent]
  all_goals cases e <;> simp_all [step, pending]

private theorem run_safe_from (es : List Event) (s : State) (h : safe s) :
    safe (runFrom s es) := by
  induction es generalizing s with
  | nil => exact h
  | cons e es ih => exact ih _ (step_safe s e h)

private theorem run_safe (es : List Event) : safe (run es) :=
  run_safe_from es {} (by simp [safe, coherent])

theorem at_most_once (es : List Event) : (run es).returns ≤ 1 := by
  have h := (run_safe es).2.1
  split at h <;> omega

/-- Retirement requires original terminality and discharge of any admitted cancel. -/
theorem retirement_requires_observation (es : List Event) (h : (run es).owned = false) :
    (run es).original = .stopped ∧
    ((run es).cancel = .none ∨ (run es).cancel = .observed) := by
  exact ⟨((run_safe es).2.2 h).1, ((run_safe es).2.2 h).2.1⟩

/-- A publisher that passed the active recheck retains the original context. -/
theorem publisher_keeps_context (es : List Event)
    (h : (run es).publisher = .checked ∨ (run es).publisher = .prepared ∨
      (run es).publisher = .admitted) : (run es).owned = true := by
  have hs := (run_safe es).2.2
  cases ho : (run es).owned
  · have allowed := (hs ho).2.2
    rcases h with h | h | h <;> simp_all
  · rfl

theorem failed_admission_has_no_credit (s : State) (h : s.publisher = .prepared) :
    (step s .reject).cancel = .none ∧ (step s .reject).owned = s.owned := by
  simp [step, h]

/-- Both CQE orders permit one pool return after the required drain. -/
theorem completion_orders :
    (run [.acquire, .recheck, .prepare, .accept, .originalCQE, .retire,
      .cancelCQE, .returned, .leave, .retire]).returns = 1 ∧
    (run [.acquire, .recheck, .prepare, .accept, .cancelCQE, .returned,
      .leave, .retire, .originalCQE, .retire]).returns = 1 := by
  decide

end Uring.Cancellation
