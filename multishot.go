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
// Negative `CQE.Res` values are decoded into `Err`. `Cancelled` reports that the
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
// Retry or resubmission policy stays in caller-side runtime code above `uring`.
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
//  1. Create it with `Uring.AcceptMultishot` or `Uring.ReceiveMultishot`
//  2. Observe `OnMultishotStep` for each CQE
//  3. End it with `Cancel`, `Unsubscribe`, or a terminal kernel completion
//  4. If callbacks stay enabled, one `OnMultishotStop` runs at most once
//
// Thread Safety:
// `Cancel` and `Unsubscribe` are safe for the subscription state itself, but
// cancel submission follows the ring's submit-state serialization contract. On
// default single-issuer rings, call them from the ring owner or otherwise
// serialize them with submit, Wait, WaitDirect, WaitExtended, Stop, and
// ResizeRings. On MultiIssuers rings, the shared-submit lock serializes the
// cancel SQE.
// Observer callbacks run on the goroutine that dispatches the CQE, usually `Wait`.
type MultishotSubscription struct {
	ring         *Uring
	ext          atomic.Pointer[ExtSQE] // ExtSQE retained until terminal cleanup
	userData     uint64                 // Kernel-visible user_data for cancellation
	handler      MultishotHandler       // Convenience callback adapter; routing policy stays in the caller-side runtime
	state        atomic.Uint32          // Long-lived subscription state
	cancelSubmit atomic.Bool            // Protects userData from ExtSQE reuse while cancel SQE is being submitted
	unsubscribed atomic.Bool            // Suppresses callbacks that have not started yet
}

// Cancel asks the kernel to stop this multishot operation. The subscription
// remains live until a terminal CQE arrives. Cancel follows the ring's
// submit-state serialization contract: on default single-issuer rings, call it
// from the ring owner or otherwise serialize it with submit, Wait, WaitDirect,
// WaitExtended, Stop, and ResizeRings; on MultiIssuers rings, the
// shared-submit lock serializes the cancel SQE.
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
// Unsubscribe follows the same cancel submission serialization contract as
// [MultishotSubscription.Cancel].
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

// HandleCQE processes a copied CQE observation for this subscription.
// It returns true when the CQE belongs to this route and was handled. It returns
// false for non-extended CQEs, foreign routes, or observations whose current
// pooled ExtSQE owner is no longer this subscription.
//
// HandleCQE is for immediate dispatch in the caller's serialized completion
// loop. Caller code must call it before the observed ExtSQE can be retired and
// reused. If caller code keeps copied CQEs beyond that loop, caller code must
// keep its own route state and reject observations for retired subscriptions.
//
// HandleCQE does not wait, retry, rearm, or resubmit. Caller-side runtime code
// owns polling cadence and any policy after the subscription reaches its
// terminal `!HasMore()` observation.
func (s *MultishotSubscription) HandleCQE(cqe CQEView) bool {
	if !cqe.Extended() {
		return false
	}
	if cqe.Context().Raw() != s.userData {
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
// The handler receives one `MultishotStep` per CQE and one terminal stop.
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
// The handler receives one `MultishotStep` per CQE and one terminal stop.
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
	return s.submitCancelUsing(s.ring.asyncCancelByUserData)
}

func (s *MultishotSubscription) submitCancelUsing(submit func(uint64) error) error {
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
	err := submit(s.userData)
	if err != nil {
		return err
	}
	if s.State() == SubscriptionActive {
		_ = s.tryCancel()
	}
	return nil
}

func (s *MultishotSubscription) finishCancelSubmit() {
	s.cancelSubmit.Store(false)
	if s.State() == SubscriptionStopped {
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
	if !s.cancelSubmit.Load() {
		s.retireExt()
	}
}

// retireIfTerminal retires the unsubscribed subscription once the terminal CQE
// arrives after callbacks have been suppressed.
func (s *MultishotSubscription) retireIfTerminal(cqe CQEView) {
	if !cqe.HasMore() {
		if SubscriptionState(s.state.Swap(uint32(SubscriptionStopped))) == SubscriptionStopped {
			return
		}
		if !s.cancelSubmit.Load() {
			s.retireExt()
		}
	}
}

// asyncCancelByUserData submits an async cancel request using userData.
func (ur *Uring) asyncCancelByUserData(userData uint64) error {
	ctx := PackDirect(IORING_OP_ASYNC_CANCEL, 0, 0, 0)
	return ur.asyncCancel(ctx, userData)
}
