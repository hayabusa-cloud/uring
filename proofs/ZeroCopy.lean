-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import Std

/-!
The successful atomic transitions in zc_tracker.go. Results are their 32-bit
representations; signed interpretation and packing into the atomic word are
outside this model. Each trace concerns one request lifetime.
-/
namespace Uring.ZeroCopy

inductive Phase where
  | submitted | earlyNotified | completed | notified
  deriving DecidableEq, Repr

structure Tracker where
  phase : Phase := .submitted
  result : UInt32 := 0
  deriving DecidableEq, Repr

inductive Event where
  | dataMore (result : UInt32)
  | dataFinal (result : UInt32)
  | notification
  deriving Repr

structure Output where
  tracker : Tracker
  complete : Bool := false
  release : Bool := false
  deriving Repr

/-- One successful state change, or the observed no-op branch. CAS retries
    stutter before this step; their progress and memory ordering are not modeled. -/
def step (s : Tracker) (event : Event) : Output :=
  match event, s.phase with
  | .dataMore result, .submitted => ⟨⟨.completed, result⟩, true, false⟩
  | .dataMore result, .earlyNotified => ⟨⟨.notified, result⟩, true, true⟩
  | .dataFinal result, .submitted
  | .dataFinal result, .earlyNotified => ⟨⟨.notified, result⟩, true, true⟩
  | .notification, .submitted => ⟨{ s with phase := .earlyNotified }, false, false⟩
  | .notification, .completed => ⟨{ s with phase := .notified }, false, true⟩
  | _, _ => ⟨s, false, false⟩

/-- Proof-only history: signal counts and observed input evidence. -/
structure History where
  tracker : Tracker := {}
  completions : Nat := 0
  releases : Nat := 0
  sawData : Bool := false
  sawNotification : Bool := false
  sawFinal : Bool := false

def advance (h : History) (event : Event) : History :=
  let out := step h.tracker event
  { tracker := out.tracker
    completions := h.completions + if out.complete then 1 else 0
    releases := h.releases + if out.release then 1 else 0
    sawData := h.sawData || match event with | .notification => false | _ => true
    sawNotification := h.sawNotification || match event with | .notification => true | _ => false
    sawFinal := h.sawFinal || match event with | .dataFinal _ => true | _ => false }

def runFrom : List Event → History → History
  | [], h => h
  | event :: rest, h => runFrom rest (advance h event)

def run (events : List Event) : History := runFrom events {}

private def completionCount : Phase → Nat
  | .completed | .notified => 1
  | _ => 0

private def releaseCount : Phase → Nat
  | .notified => 1
  | _ => 0

private def Invariant (h : History) : Prop :=
  h.completions = completionCount h.tracker.phase ∧
  h.releases = releaseCount h.tracker.phase ∧
  (h.tracker.phase = .earlyNotified → h.sawNotification = true) ∧
  (h.tracker.phase = .completed → h.sawData = true) ∧
  (h.tracker.phase = .notified →
    h.sawData = true ∧ (h.sawNotification = true ∨ h.sawFinal = true))

private theorem advance_invariant (h : History) (event : Event)
    (valid : Invariant h) : Invariant (advance h event) := by
  cases h with
  | mk tracker completions releases sawData sawNotification sawFinal =>
    cases tracker with
    | mk phase result =>
      cases phase <;> cases event <;>
        simp_all [Invariant, advance, step, completionCount, releaseCount]

private theorem runFrom_invariant (events : List Event) (h : History)
    (valid : Invariant h) : Invariant (runFrom events h) := by
  induction events generalizing h with
  | nil => exact valid
  | cons event rest ih => exact ih _ (advance_invariant h event valid)

private theorem run_invariant (events : List Event) : Invariant (run events) :=
  runFrom_invariant events {} (by simp [Invariant, completionCount, releaseCount])

/-- Arbitrarily long finite histories, including repeated modeled events,
    emit at most one completion and one release signal. -/
theorem at_most_once (events : List Event) :
    (run events).completions ≤ 1 ∧ (run events).releases ≤ 1 := by
  have valid := run_invariant events
  cases phase : (run events).tracker.phase <;>
    simp_all [Invariant, completionCount, releaseCount]

theorem release_requires_completion (events : List Event) :
    (run events).releases ≤ (run events).completions := by
  have valid := run_invariant events
  cases phase : (run events).tracker.phase <;>
    simp_all [Invariant, completionCount, releaseCount]

/-- Release needs a data event and either notification or terminal-data
    evidence. A notification alone is insufficient. -/
theorem release_requires_evidence (events : List Event)
    (released : (run events).releases = 1) :
    (run events).sawData = true ∧
      ((run events).sawNotification = true ∨ (run events).sawFinal = true) := by
  have valid := run_invariant events
  cases phase : (run events).tracker.phase <;>
    simp_all [Invariant, completionCount, releaseCount]

theorem terminal_absorbing (result : UInt32) (event : Event) :
    step ⟨.notified, result⟩ event = ⟨⟨.notified, result⟩, false, false⟩ := by
  cases event <;> rfl

theorem accepted_result_stable (s : Tracker) (event : Event)
    (accepted : s.phase = .completed ∨ s.phase = .notified) :
    (step s event).tracker.result = s.result ∧
      ((step s event).tracker.phase = .completed ∨
        (step s event).tracker.phase = .notified) := by
  cases s with
  | mk phase result => cases phase <;> cases event <;> simp_all [step]

theorem result_stable_through_trace (events : List Event) (h : History)
    (accepted : h.tracker.phase = .completed ∨ h.tracker.phase = .notified) :
    (runFrom events h).tracker.result = h.tracker.result := by
  induction events generalizing h with
  | nil => rfl
  | cons event rest ih =>
    have stable := accepted_result_stable h.tracker event accepted
    exact (ih (advance h event) stable.2).trans stable.1

/-- Both notification orders preserve the result and finish once. -/
theorem notification_orders (result : UInt32) :
    (run [.dataMore result, .notification]).tracker = ⟨.notified, result⟩ ∧
    (run [.notification, .dataMore result]).tracker = ⟨.notified, result⟩ ∧
    (run [.dataMore result, .notification]).releases = 1 ∧
    (run [.notification, .dataMore result]).releases = 1 := by
  simp [run, runFrom, advance, step]

theorem terminal_fallback (result : UInt32) :
    (run [.dataFinal result]).tracker = ⟨.notified, result⟩ ∧
    (run [.dataFinal result]).completions = 1 ∧
    (run [.dataFinal result]).releases = 1 := by
  simp [run, runFrom, advance, step]

end Uring.ZeroCopy
