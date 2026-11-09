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
	// Submission is best effort. If it fails, the subscription stays active;
	// caller code can retry with MultishotSubscription.Cancel.
	MultishotStop
)

// MultishotStep describes one observed multishot CQE.
//
// CQE is a copied descriptor. Its context target is borrowed only during
// OnMultishotStep; copy needed target fields before returning.
// Negative CQE.Res values are decoded into Err. Cancelled reports -ECANCELED.
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
// Retry and resubmission policy stay in caller-side runtime code outside
// `uring`.
type MultishotHandler interface {
	// OnMultishotStep handles one observed CQE.
	// Return `MultishotStop` to request async cancellation after a non-final step.
	OnMultishotStep(step MultishotStep) MultishotAction

	// OnMultishotStop handles the terminal stop of the subscription.
	// It runs at most once if callbacks stay enabled. It carries the terminal
	// error or cancellation cause, but not a borrowed CQE view.
	// A queued cancellation CQE may still need dispatch after this callback.
	OnMultishotStop(err error, cancelled bool)
}

// SubscriptionState describes the original multishot request, independently
// of any pending cancellation CQE.
//
//	Active --cancel submitted--> Cancelling
//	   |                              |
//	   +---------------+--------------+
//	                   | original terminal CQE handled (any result)
//	                   v
//	                Stopped
//
//	State        Active()  Cancel()
//	Active       true      May submit cancellation
//	Cancelling   true      No-op
//	Stopped      false     No-op
//
// Stopped is absorbing. It does not imply that cancellation dispatch or the
// terminal callback has finished. Continue dispatching queued cancellation
// CQEs through [MultishotSubscription.HandleCQE] or their extended handlers.
type SubscriptionState uint32

const (
	// SubscriptionActive receives completions and can still cancel.
	SubscriptionActive SubscriptionState = iota

	// SubscriptionCancelling has enqueued cancel and awaits the original terminal CQE.
	SubscriptionCancelling

	// SubscriptionStopped has observed the original terminal CQE.
	// A queued cancellation CQE may still need dispatch.
	SubscriptionStopped
)

// MultishotSubscription tracks one multishot `io_uring` operation.
// It delivers zero or more step callbacks until cancelled or exhausted.
//
// Lifecycle:
//  1. Create it with `Uring.AcceptMultishot` or `Uring.ReceiveMultishot`
//  2. Observe `OnMultishotStep` for each original CQE
//  3. Request cancellation with `Cancel` or `Unsubscribe`, or let it end naturally
//  4. If callbacks stay enabled, one `OnMultishotStop` runs at most once
//  5. Dispatch every queued cancellation CQE before final ring teardown
//
// Thread Safety:
// `Cancel` and `Unsubscribe` are safe for the subscription state itself, but
// cancel submission follows the ring's submit-state serialization contract. On
// explicit single-issuer rings (MultiIssuers is false), call them from the
// OS-thread-pinned ring owner and serialize them with submit, Wait, WaitDirect,
// WaitExtended, Stop, and ResizeRings. On default MultiIssuers rings, the
// shared-submit lock serializes the cancel SQE.
// Callbacks run synchronously on the goroutine that dispatches the CQE.
// Wait returns CQEs; caller code invokes HandleCQE or an extended handler.
type MultishotSubscription struct {
	ring         *Uring
	ext          atomic.Pointer[ExtSQE] // Original identity retained through original and cancellation cleanup
	userData     uint64                 // Kernel-visible user_data for cancellation
	handler      MultishotHandler       // Convenience callback adapter; routing policy stays in the caller-side runtime
	state        atomic.Uint32          // Long-lived subscription state
	cancelSubmit atomic.Bool            // Protects userData from ExtSQE reuse while cancel SQE is being submitted
	cancel       cancelRequest          // Retains identity until the cancellation CQE is handled
	unsubscribed atomic.Bool            // Suppresses callbacks that have not started yet
}

// Cancel asks the kernel to stop this multishot operation. The subscription
// remains live until its original terminal CQE is handled. Cancel follows the
// ring's submit-state serialization contract: on explicit single-issuer rings, call
// it from the OS-thread-pinned ring owner and serialize it with submit, Wait,
// WaitDirect, WaitExtended, Stop, and ResizeRings; on default MultiIssuers
// rings, the shared-submit lock serializes the cancel SQE.
//
// It is safe to call more than once:
//
//	Return   Meaning
//	nil      Cancellation submitted or no-op
//	non-nil  Not submitted; no cancellation CQE for this attempt
//
// The original and cancellation CQEs report the kernel outcomes.
//
// Dispatch both original and cancellation CQEs through HandleCQE or their
// extended handlers. The pooled original context is retained until both
// lifetimes end; the cancellation context belongs to the subscription.
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
// Continue dispatching original and cancellation CQEs to release their contexts.
// Unsubscribe follows the same cancel submission serialization contract as
// [MultishotSubscription.Cancel].
func (s *MultishotSubscription) Unsubscribe() {
	s.unsubscribed.Store(true)
	_ = s.Cancel()
}

// Active reports whether the original terminal CQE has not yet been handled.
// A false result does not discharge queued cancellation CQEs.
//
//go:nosplit
func (s *MultishotSubscription) Active() bool {
	return SubscriptionState(s.state.Load()) != SubscriptionStopped
}

// State returns the original request's [SubscriptionState].
//
//go:nosplit
func (s *MultishotSubscription) State() SubscriptionState {
	return SubscriptionState(s.state.Load())
}

// HandleCQE processes an original or cancellation CQE for this subscription.
// It returns true when the CQE belongs to this route and was handled. It returns
// false for non-extended CQEs, foreign routes, or observations whose current
// pooled ExtSQE owner is no longer this subscription.
//
// HandleCQE is for immediate dispatch in the caller's serialized completion
// loop. Caller code must call it before the observed ExtSQE can be retired and
// reused. If copied CQEs outlive that loop, caller code must pair them with its
// own route state and reject observations for retired subscriptions.
//
// HandleCQE may attempt cancellation when a non-final step returns MultishotStop.
// It does not wait or rearm the original operation. Failed cancellation admission
// leaves the subscription active and is not returned by this method; caller code
// owns retry policy, polling cadence, and handling after original terminality.
func (s *MultishotSubscription) HandleCQE(cqe CQEView) bool {
	if !cqe.Extended() {
		return false
	}
	if cqe.Context().Raw() != s.userData {
		if cqe.Context() == PackExtended(&s.cancel.slot.ext) && s.cancel.pending.Load() {
			s.finishCancellation()
			return true
		}
		return false
	}
	ext := cqe.ExtSQE()
	if ext == nil {
		return false
	}
	owner, _ := extAnchors(ext).owner.(*MultishotSubscription)
	if owner != s {
		return false
	}
	s.handleCQEView(cqe)
	return true
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
	if handler == nil {
		handler = NoopMultishotHandler{}
	}

	ext := ur.ctxPools.Extended()
	if ext == nil {
		return nil, iox.ErrWouldBlock
	}

	sub := &MultishotSubscription{
		ring:    ur,
		handler: handler,
	}
	ctx := CastUserData[multishotCtx](ext)
	ctx.Fn = multishotCQEHandler
	extAnchors(ext).owner = sub
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
// While callbacks stay enabled, the handler receives one MultishotStep per
// original CQE and at most one terminal stop.
// `step.CQE.Res` contains the accepted fd on success.
// Options may add SQE control flags and accept-specific ioprio flags such as
// `IORING_ACCEPT_POLL_FIRST`.
func (ur *Uring) AcceptMultishot(sqeCtx SQEContext, handler MultishotHandler, options ...OpOptionFunc) (*MultishotSubscription, error) {
	fd := sqeCtx.FD()
	flags, ioprio := ur.flagsIOPrioOptions(options)
	sqeFlags := sqeCtx.Flags() | flags
	return ur.newMultishotSubscription(handler, func(ext *ExtSQE) {
		ext.SQE.opcode = IORING_OP_ACCEPT
		ext.SQE.fd = fd
		ext.SQE.addr = 0
		ext.SQE.len = 0
		ext.SQE.flags = sqeFlags
		ext.SQE.ioprio = uint16(ioprio | IORING_ACCEPT_MULTISHOT)
		ext.SQE.uflags = SOCK_NONBLOCK | SOCK_CLOEXEC
	})
}

// ReceiveMultishot starts a multishot receive subscription with buffer selection.
// While callbacks stay enabled, the handler receives one MultishotStep per
// original CQE and at most one terminal stop.
// Use `step.CQE.BufID()` for the buffer ID and `step.CQE.Res` for the byte count.
// Options may add SQE control flags and recv-specific ioprio flags such as
// `IORING_RECVSEND_POLL_FIRST`.
func (ur *Uring) ReceiveMultishot(sqeCtx SQEContext, handler MultishotHandler, options ...OpOptionFunc) (*MultishotSubscription, error) {
	fd := sqeCtx.FD()
	bufGroup := sqeCtx.BufGroup()
	flags, ioprio := ur.flagsIOPrioOptions(options)
	sqeFlags := sqeCtx.Flags() | flags
	return ur.newMultishotSubscription(handler, func(ext *ExtSQE) {
		ext.SQE.opcode = IORING_OP_RECV
		ext.SQE.fd = fd
		ext.SQE.addr = 0
		ext.SQE.len = 0
		ext.SQE.flags = sqeFlags | IOSQE_BUFFER_SELECT
		ext.SQE.bufIndex = bufGroup
		ext.SQE.ioprio = uint16(ioprio | IORING_RECV_MULTISHOT)
	})
}

// ========================================
// CQE Handling
// ========================================

func multishotCQEHandler(ring *Uring, _ *ioUringSqe, cqe *ioUringCqe) {
	ctx := SQEContextFromRaw(cqe.userData)
	if !ctx.IsExtended() {
		return
	}
	ext := ctx.ExtSQE()
	sub, _ := extAnchors(ext).owner.(*MultishotSubscription)
	if sub == nil {
		return
	}
	sub.handleCQE(cqe)
}

func (s *MultishotSubscription) handleCQE(cqe *ioUringCqe) {
	ctx := SQEContextFromRaw(cqe.userData)
	s.handleCQEView(CQEView{
		Res:   cqe.res,
		Flags: cqe.flags,
		ctx:   ctx,
	})
}

func (s *MultishotSubscription) handleCQEView(cqe CQEView) {
	if s.unsubscribed.Load() {
		s.retireIfTerminal(cqe)
		return
	}

	step := MultishotStep{CQE: cqe}
	if cqe.Res < 0 {
		step.Err = errFromErrno(uintptr(-cqe.Res))
		step.Cancelled = cqe.Res == -int32(ECANCELED)
	}
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
	return s.submitCancelUsing(s.ring.asyncCancel)
}

func (s *MultishotSubscription) submitCancelUsing(submit func(SQEContext, uint64) error) error {
	if s.State() != SubscriptionActive {
		return nil
	}
	if !s.cancelSubmit.CompareAndSwap(false, true) {
		return nil
	}
	defer s.finishCancelSubmit()
	if s.State() != SubscriptionActive {
		return nil
	}
	err := submit(s.cancel.prepare(s), s.userData)
	if err != nil {
		s.cancel.complete()
		return err
	}
	if s.State() == SubscriptionActive {
		_ = s.tryCancel()
	}
	return nil
}

func (s *MultishotSubscription) finishCancelSubmit() {
	s.cancelSubmit.Store(false)
	if s.State() == SubscriptionStopped && !s.cancel.pending.Load() {
		s.retireExt()
	}
}

func (s *MultishotSubscription) finishCancellation() {
	if s.cancel.complete() && s.State() == SubscriptionStopped && !s.cancelSubmit.Load() {
		s.retireExt()
	}
}

func (s *MultishotSubscription) finish(err error, cancelled bool) {
	if SubscriptionState(s.state.Swap(uint32(SubscriptionStopped))) == SubscriptionStopped {
		return
	}
	if !s.unsubscribed.Load() {
		s.handler.OnMultishotStop(err, cancelled)
	}
	if !s.cancelSubmit.Load() && !s.cancel.pending.Load() {
		s.retireExt()
	}
}

// retireIfTerminal records original terminality with callbacks suppressed.
// The pooled identity stays retained while cancellation is pending or publishing.
func (s *MultishotSubscription) retireIfTerminal(cqe CQEView) {
	if !cqe.HasMore() {
		if SubscriptionState(s.state.Swap(uint32(SubscriptionStopped))) == SubscriptionStopped {
			return
		}
		if !s.cancelSubmit.Load() && !s.cancel.pending.Load() {
			s.retireExt()
		}
	}
}
