// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"

	"code.hybscloud.com/iox"
)

// multishotCtx is the `ExtSQE.UserData` layout used by multishot dispatch.
type multishotCtx = Ctx0

// MultishotAction tells `uring` whether to keep or stop a live subscription.
type MultishotAction uint8

const (
	// MultishotContinue keeps the subscription live.
	// It applies only while the observed step still carries `IORING_CQE_F_MORE`.
	MultishotContinue MultishotAction = iota

	// MultishotStop requests cancellation after the current callback returns.
	// A final step ignores it because the kernel is already closing the stream.
	MultishotStop
)

// MultishotStep describes one observed multishot CQE.
//
// `CQE` is borrowed and valid only during `OnMultishotStep`.
// Negative `res` values are decoded into `Err`. `Cancelled` reports that the
// kernel step was `-ECANCELED`.
type MultishotStep struct {
	CQE       CQEView
	Err       error
	Cancelled bool
}

// HasMore reports whether the observed CQE carried `IORING_CQE_F_MORE`.
func (s MultishotStep) HasMore() bool {
	return s.CQE.HasMore()
}

// Final reports whether the observed CQE lacked `IORING_CQE_F_MORE`.
func (s MultishotStep) Final() bool {
	return !s.HasMore()
}

// MultishotHandler handles multishot step observations and the terminal stop.
// Retry or resubmission policy stays above `uring`.
type MultishotHandler interface {
	// OnMultishotStep handles one observed CQE.
	// Return `MultishotStop` to request async cancellation after a non-final step.
	OnMultishotStep(step MultishotStep) MultishotAction

	// OnMultishotStop handles the terminal stop of the subscription.
	// It runs at most once if callbacks stay enabled. It carries the terminal
	// error or cancellation cause, but not a borrowed CQE view.
	OnMultishotStop(err error, cancelled bool)
}

// SubscriptionState reports where a multishot subscription is in its lifecycle.
//
// Typestate model:
//
// The valid operations depend on the current state. In a language with
// built-in typestate support, each state would be a distinct type.
//
// State machine:
//
// ┌──────────────────────────────────────────────────────────────────┐
// │                                                                  │
// │   ┌──────────┐   Cancel()    ┌──────────────┐                   │
// │   │  Active  │ ────────────▶ │  Cancelling  │                   │
// │   └────┬─────┘               └──────┬───────┘                   │
// │        │                            │                           │
// │        │ Final CQE (!MORE)          │ Terminal CQE             │
// │        │                            │                           │
// │        ▼                            ▼                           │
// │   ┌─────────────────────────────────────────────────────────┐   │
// │   │                      Stopped                            │   │
// │   │           (Terminal/Absorbing State)                    │   │
// │   └─────────────────────────────────────────────────────────┘   │
// │                                                                  │
// └──────────────────────────────────────────────────────────────────┘
//
// Available operations:
//
// State        | Allowed Operations
// -------------|----------------------------------------------------
// Active       | Cancel(), OnMultishotStep(), Active(), State()
// Cancelling   | OnMultishotStep(), Active(), State()
// Stopped      | Active(), State() [returns false/Stopped]
//
// Invariants:
//   - Active → Cancelling: Only via Cancel() with CAS
//   - Active → Stopped: Only via a terminal CQE (success or error)
//   - Cancelling → Stopped: Only via the terminal CQE after cancel wins the race
//   - Stopped is absorbing: No outgoing transitions
type SubscriptionState uint32

const (
	// SubscriptionActive receives completions and can still cancel.
	SubscriptionActive SubscriptionState = iota

	// SubscriptionCancelling has enqueued cancel and awaits terminal CQE.
	SubscriptionCancelling

	// SubscriptionStopped is terminal; no more callbacks will start.
	SubscriptionStopped
)

// MultishotSubscription tracks one multishot `io_uring` operation.
// It delivers zero or more step callbacks until cancelled or exhausted.
//
// Lifecycle:
//  1. Create it with `Uring.AcceptMultishot` or `Uring.RecvMultishot`
//  2. Observe `OnMultishotStep` for each CQE
//  3. End it with `Cancel`, `Unsubscribe`, or a terminal kernel completion
//  4. If callbacks stay enabled, one `OnMultishotStop` runs at most once
//
// Thread Safety:
// `Cancel` and `Unsubscribe` are safe from any goroutine.
// Observer callbacks run on the goroutine that dispatches the CQE, usually `Wait`.
type MultishotSubscription struct {
	ring         *Uring
	ext          atomic.Pointer[ExtSQE] // ExtSQE retained until terminal cleanup
	userData     uint64                 // Kernel-visible user_data for cancellation
	handler      MultishotHandler       // Convenience callback adapter; routing policy stays above `uring`
	state        atomic.Uint32          // Long-lived subscription state
	canceling    atomic.Bool            // Serializes cancel submission without claiming kernel progress early
	unsubscribed atomic.Bool            // Suppresses callbacks that have not started yet
}

// Cancel asks the kernel to stop this multishot operation.
// The subscription remains live until a terminal CQE arrives.
// It is safe to call more than once.
//
// If callbacks remain enabled, later CQEs may still deliver `OnMultishotStep`
// before the terminal CQE delivers `OnMultishotStop`.
func (s *MultishotSubscription) Cancel() error {
	if s.State() != SubscriptionActive {
		return nil
	}
	return s.submitCancel()
}

// Unsubscribe suppresses future callbacks and best-effort cancels the subscription.
// It also suppresses callbacks that have not started yet, including `OnMultishotStop`.
// Use it when you do not need a terminal callback.
//
// In-flight callbacks may still finish after `Unsubscribe` returns. If the cancel
// submission fails, the kernel request can remain live until it terminates
// naturally, but further callbacks stay suppressed.
func (s *MultishotSubscription) Unsubscribe() {
	s.unsubscribed.Store(true)
	_ = s.Cancel()
}

// Active reports whether the subscription has not yet reached its terminal CQE.
// A cancelling subscription remains active until the terminal CQE arrives.
//
//go:nosplit
func (s *MultishotSubscription) Active() bool {
	return SubscriptionState(s.state.Load()) != SubscriptionStopped
}

// State returns the current subscription state.
//
//go:nosplit
func (s *MultishotSubscription) State() SubscriptionState {
	return SubscriptionState(s.state.Load())
}

//go:nosplit
func (s *MultishotSubscription) tryCancel() bool {
	return s.state.CompareAndSwap(uint32(SubscriptionActive), uint32(SubscriptionCancelling))
}

//go:nosplit
func (s *MultishotSubscription) markStopped() {
	s.state.Store(uint32(SubscriptionStopped))
}

// retireExt releases the pooled ExtSQE exactly once.
func (s *MultishotSubscription) retireExt() {
	if ext := s.ext.Swap(nil); ext != nil {
		s.ring.ctxPools.PutExtended(ext)
	}
}

// ========================================
// Factory Methods on Uring
// ========================================

func (ur *Uring) newMultishotSubscription(
	handler MultishotHandler,
	init func(ext *ExtSQE),
) (*MultishotSubscription, error) {
	ext := ur.ctxPools.Extended()
	if ext == nil {
		return nil, iox.ErrWouldBlock
	}

	sub := &MultishotSubscription{
		ring:    ur,
		handler: handler,
	}
	ctx := CastUserData[multishotCtx](ext)
	ctx.Fn = sub.makeHandler()
	sub.ext.Store(ext)
	sub.state.Store(uint32(SubscriptionActive))

	// ExtSQE instances are pooled, so reset the kernel SQE portion before
	// rebuilding the submission-specific fields.
	ext.SQE = ioUringSqe{}
	init(ext)

	extCtx := PackExtended(ext)
	sub.userData = extCtx.Raw()
	if err := ur.SubmitExtended(extCtx); err != nil {
		sub.ext.Store(nil)
		sub.markStopped()
		ur.ctxPools.PutExtended(ext)
		return nil, err
	}

	return sub, nil
}

// AcceptMultishot starts a multishot accept subscription.
// The handler receives one `MultishotStep` per CQE and one terminal stop.
// `step.CQE.Res` contains the accepted fd on success.
func (ur *Uring) AcceptMultishot(sqeCtx SQEContext, handler MultishotHandler) (*MultishotSubscription, error) {
	fd := sqeCtx.FD()
	return ur.newMultishotSubscription(handler, func(ext *ExtSQE) {
		ext.SQE.opcode = IORING_OP_ACCEPT
		ext.SQE.fd = fd
		ext.SQE.addr = 0
		ext.SQE.len = 0
		ext.SQE.flags = sqeCtx.Flags()
		ext.SQE.ioprio = uint16(IORING_ACCEPT_MULTISHOT)
		ext.SQE.uflags = SOCK_NONBLOCK | SOCK_CLOEXEC
	})
}

// RecvMultishot starts a multishot receive subscription with buffer selection.
// The handler receives one `MultishotStep` per CQE and one terminal stop.
// Use `step.CQE.BufID()` for the buffer ID and `step.CQE.Res` for the byte count.
func (ur *Uring) RecvMultishot(sqeCtx SQEContext, handler MultishotHandler) (*MultishotSubscription, error) {
	fd := sqeCtx.FD()
	bufGroup := sqeCtx.BufGroup()
	return ur.newMultishotSubscription(handler, func(ext *ExtSQE) {
		ext.SQE.opcode = IORING_OP_RECV
		ext.SQE.fd = fd
		ext.SQE.addr = 0
		ext.SQE.len = 0
		ext.SQE.flags = sqeCtx.Flags() | IOSQE_BUFFER_SELECT
		ext.SQE.bufIndex = bufGroup
		ext.SQE.ioprio = uint16(IORING_RECV_MULTISHOT)
	})
}

// ========================================
// CQE Handling
// ========================================

func (s *MultishotSubscription) makeHandler() Handler {
	return func(_ *Uring, _ *ioUringSqe, cqe *ioUringCqe) {
		s.handleCQE(cqe)
	}
}

func (s *MultishotSubscription) decodeStep(cqe *ioUringCqe) MultishotStep {
	view := CQEView{
		Res:   cqe.res,
		Flags: cqe.flags,
		ctx:   PackExtended(s.ext.Load()),
	}
	step := MultishotStep{CQE: view}
	if view.Res < 0 {
		step.Err = errFromErrno(uintptr(-view.Res))
		step.Cancelled = view.Res == -int32(ECANCELED)
	}
	return step
}

func (s *MultishotSubscription) handleCQE(cqe *ioUringCqe) {
	if s.unsubscribed.Load() {
		s.retireIfTerminal(cqe)
		return
	}

	step := s.decodeStep(cqe)
	action := s.handler.OnMultishotStep(step)
	if step.Final() {
		s.finish(step.Err, step.Cancelled)
		return
	}
	if action == MultishotStop && s.Active() {
		s.trySubmitCancel()
	}
}

func (s *MultishotSubscription) trySubmitCancel() {
	_ = s.submitCancel()
}

func (s *MultishotSubscription) submitCancel() error {
	return s.submitCancelUsing(s.ring.asyncCancelByUserData)
}

func (s *MultishotSubscription) submitCancelUsing(submit func(uint64) error) error {
	if s.State() != SubscriptionActive {
		return nil
	}
	if !s.canceling.CompareAndSwap(false, true) {
		return nil
	}
	err := submit(s.userData)
	if err != nil {
		s.canceling.Store(false)
		return err
	}
	if s.State() == SubscriptionActive {
		_ = s.tryCancel()
	}
	return nil
}

func (s *MultishotSubscription) finish(err error, cancelled bool) {
	s.markStopped()
	if !s.unsubscribed.Load() {
		s.handler.OnMultishotStop(err, cancelled)
	}
	s.retireExt()
}

// retireIfTerminal retires the unsubscribed subscription once the terminal CQE
// arrives after callbacks have been suppressed.
func (s *MultishotSubscription) retireIfTerminal(cqe *ioUringCqe) {
	if cqe.flags&IORING_CQE_F_MORE == 0 {
		s.markStopped()
		s.retireExt()
	}
}

// asyncCancelByUserData submits an async cancel request using userData.
func (ur *Uring) asyncCancelByUserData(userData uint64) error {
	ctx := PackDirect(IORING_OP_ASYNC_CANCEL, 0, 0, 0)
	return ur.asyncCancel(ctx, userData)
}
