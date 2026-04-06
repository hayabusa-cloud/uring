// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "code.hybscloud.com/iofd"

// Zero-copy send states for the two-CQE model (darwin stub).
const (
	zcStateSubmitted uint32 = iota
	zcStateCompleted
	zcStateNotified
)

// ZCHandler handles zero-copy send completion events (darwin stub).
type ZCHandler interface {
	OnCompleted(result int32)
	OnNotification(result int32)
}

// ZCTracker manages zero-copy send operations (darwin stub).
type ZCTracker struct {
	ring *Uring
	pool *ContextPools
}

// NewZCTracker creates a tracker for managing zero-copy sends (darwin stub).
func NewZCTracker(ring *Uring, pool *ContextPools) *ZCTracker {
	return &ZCTracker{
		ring: ring,
		pool: pool,
	}
}

// SendZC submits a zero-copy send operation (darwin stub).
func (t *ZCTracker) SendZC(fd iofd.FD, buf []byte, handler ZCHandler, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SendZCFixed submits a zero-copy send using a registered buffer (darwin stub).
func (t *ZCTracker) SendZCFixed(fd iofd.FD, bufIndex int, offset int, length int, handler ZCHandler, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// HandleCQE processes a CQE that may be a zero-copy completion or notification (darwin stub).
func (t *ZCTracker) HandleCQE(cqe CQEView) bool {
	return false
}
