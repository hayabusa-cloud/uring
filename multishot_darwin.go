// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "sync/atomic"

// MultishotAction tells `uring` whether to keep or stop a live subscription.
type MultishotAction uint8

const (
	MultishotContinue MultishotAction = iota
	MultishotStop
)

// MultishotStep is the canonical observation surface for multishot CQEs.
type MultishotStep struct {
	CQE       CQEView
	Err       error
	Cancelled bool
}

// HasMore reports whether the observed CQE carried `IORING_CQE_F_MORE`.
func (s MultishotStep) HasMore() bool { return s.CQE.HasMore() }

// Final reports whether the observed CQE lacked `IORING_CQE_F_MORE`.
func (s MultishotStep) Final() bool { return !s.HasMore() }

// MultishotHandler handles multishot step observations and the terminal stop.
type MultishotHandler interface {
	OnMultishotStep(step MultishotStep) MultishotAction
	OnMultishotStop(err error, cancelled bool)
}

// SubscriptionState represents the state of a multishot subscription.
type SubscriptionState uint32

const (
	SubscriptionActive SubscriptionState = iota
	SubscriptionCancelling
	SubscriptionStopped
)

// MultishotSubscription tracks one multishot io_uring operation.
// On Darwin, multishot operations are not supported.
type MultishotSubscription struct {
	ring         *Uring
	ext          *ExtSQE
	userData     uint64
	handler      MultishotHandler
	unsubscribed atomic.Bool
}

// Cancel reports that multishot subscriptions are not supported on Darwin.
func (s *MultishotSubscription) Cancel() error {
	return ErrNotSupported
}

// Unsubscribe suppresses callbacks that have not started yet, then calls Cancel.
func (s *MultishotSubscription) Unsubscribe() {
	s.unsubscribed.Store(true)
	_ = s.Cancel()
}

// Active returns true if the subscription is still receiving completions.
func (s *MultishotSubscription) Active() bool {
	return false
}

// State returns the current subscription state.
func (s *MultishotSubscription) State() SubscriptionState {
	return SubscriptionStopped
}

// HandleCQE reports that multishot subscriptions are not supported on Darwin.
func (s *MultishotSubscription) HandleCQE(cqe CQEView) bool {
	return false
}

// AcceptMultishot creates a multishot accept subscription (stub on Darwin).
func (ur *Uring) AcceptMultishot(sqeCtx SQEContext, handler MultishotHandler, options ...OpOptionFunc) (*MultishotSubscription, error) {
	return nil, ErrNotSupported
}

// ReceiveMultishot creates a multishot receive subscription (stub on Darwin).
func (ur *Uring) ReceiveMultishot(sqeCtx SQEContext, handler MultishotHandler, options ...OpOptionFunc) (*MultishotSubscription, error) {
	return nil, ErrNotSupported
}

// asyncCancelByUserData submits an async cancel request using userData.
func (ur *Uring) asyncCancelByUserData(userData uint64) error {
	ctx := PackDirect(IORING_OP_ASYNC_CANCEL, 0, 0, 0)
	return ur.asyncCancel(ctx, userData)
}
