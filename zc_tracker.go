// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
)

// Zero-copy send states for the two-CQE model.
const (
	zcStateSubmitted uint32 = iota // SQE submitted, waiting for first CQE
	zcStateCompleted               // First CQE received (IORING_CQE_F_MORE set)
	zcStateNotified                // The second CQE received (IORING_CQE_F_NOTIF set)
)

// zcTrackerData tracks zero-copy send state in ExtSQE UserData.
// Layout: 16 bytes, fits in Ctx0V5.Data[16].
type zcTrackerData struct {
	used     atomic.Bool   // 4 bytes - one-shot handler invocation
	state    atomic.Uint32 // 4 bytes - state machine
	opResult atomic.Int32  // 4 bytes - operation result from first CQE
	_        [4]byte       // padding to 16 bytes
}

var _ [16 - unsafe.Sizeof(zcTrackerData{})]struct{}

// tryInvokeOnce ensures the handler is invoked exactly once.
// Returns true on the first call, false on subsequent calls.
//
//go:nosplit
func (d *zcTrackerData) tryInvokeOnce() bool {
	return d.used.CompareAndSwap(false, true)
}

// markCompleted atomically transitions from Submitted to Completed.
// Stores the operation result before transitioning.
//
//go:nosplit
func (d *zcTrackerData) markCompleted(result int32) bool {
	d.opResult.Store(result)
	return d.state.CompareAndSwap(zcStateSubmitted, zcStateCompleted)
}

// markNotified atomically transitions from Completed to Notified.
//
//go:nosplit
func (d *zcTrackerData) markNotified() bool {
	return d.state.CompareAndSwap(zcStateCompleted, zcStateNotified)
}

// result returns the operation result stored during completion.
//
//go:nosplit
func (d *zcTrackerData) result() int32 {
	return d.opResult.Load()
}

// ZCHandler handles zero-copy send completion events.
type ZCHandler interface {
	// OnCompleted is called when the send CQE arrives.
	// The result is the number of bytes sent or a negative errno value.
	// The buffer must not be modified until OnNotification runs.
	OnCompleted(result int32)

	// OnNotification is called when the buffer can be safely reused.
	// This is the second CQE in the zero-copy two-CQE model and it carries
	// the original send result observed at completion time.
	OnNotification(result int32)
}

// ZCTracker manages zero-copy send operations through the two-CQE model.
// Zero-copy sends produce two CQEs:
//  1. Operation CQE (IORING_CQE_F_MORE) - send completed, buffer still in use
//  2. Notification CQE (IORING_CQE_F_NOTIF) - buffer can be reused
//
// The tracker ensures:
//   - Handlers are invoked in the correct order
//   - Each handler is invoked exactly once
//   - ExtSQE is returned to pool only after notification
type ZCTracker struct {
	ring *Uring
	pool *ContextPools
}

// NewZCTracker creates a tracker for managing zero-copy sends.
func NewZCTracker(ring *Uring, pool *ContextPools) *ZCTracker {
	return &ZCTracker{
		ring: ring,
		pool: pool,
	}
}

// SendZC submits a zero-copy send operation.
// The handler receives OnCompleted for the send CQE and OnNotification when the
// buffer can be safely reused.
// Returns iox.ErrWouldBlock if the context pool is exhausted.
func (t *ZCTracker) SendZC(fd iofd.FD, buf []byte, handler ZCHandler, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := t.ring.sendOptions(buf, options)

	ext := t.pool.Extended()
	if ext == nil {
		return iox.ErrWouldBlock
	}

	// Initialize tracker data in UserData
	ctx := zcInitCtx(ext, t, handler)
	ctx.Fn = zcTrackerCQEHandler

	// Set up ExtSQE.SQE fields directly for Extended mode
	ext.SQE.opcode = IORING_OP_SEND_ZC
	ext.SQE.flags = flags | t.ring.writeLikeOpFlags
	ext.SQE.ioprio = ioprio
	ext.SQE.fd = int32(fd)
	ext.SQE.addr = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(offset)
	ext.SQE.len = uint32(n)
	ext.SQE.off = 0
	ext.SQE.uflags = 0

	// Submit with Extended mode context (preserves pointer + mode bits)
	sqeCtx := PackExtended(ext)
	if err := t.ring.SubmitExtended(sqeCtx); err != nil {
		t.pool.PutExtended(ext)
		return err
	}

	return nil
}

// SendZCFixed submits a zero-copy send using a registered buffer.
// Returns iox.ErrWouldBlock if the context pool is exhausted.
func (t *ZCTracker) SendZCFixed(fd iofd.FD, bufIndex int, offset int, length int, handler ZCHandler, options ...OpOptionFunc) error {
	buf := t.ring.RegisteredBuffer(bufIndex)
	if buf == nil {
		return ErrInvalidParam
	}
	if offset < 0 || length < 0 || offset+length > len(buf) {
		return ErrInvalidParam
	}

	flags, ioprio, off, n := t.ring.sendOptions(buf[offset:offset+length], options)

	ext := t.pool.Extended()
	if ext == nil {
		return iox.ErrWouldBlock
	}

	// Initialize tracker data in UserData
	ctx := zcInitCtx(ext, t, handler)
	ctx.Fn = zcTrackerCQEHandler

	// Set up ExtSQE.SQE fields for registered buffer zero-copy send.
	// IORING_RECVSEND_FIXED_BUF goes in ioprio (not IOSQE_FIXED_FILE in flags).
	ext.SQE.opcode = IORING_OP_SEND_ZC
	ext.SQE.flags = flags | t.ring.writeLikeOpFlags
	ext.SQE.ioprio = ioprio | IORING_RECVSEND_FIXED_BUF
	ext.SQE.fd = int32(fd)
	ext.SQE.addr = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(offset+int(off))
	ext.SQE.len = uint32(n)
	ext.SQE.off = 0
	ext.SQE.uflags = 0
	ext.SQE.bufIndex = uint16(bufIndex)

	// Submit with Extended mode context
	sqeCtx := PackExtended(ext)
	if err := t.ring.SubmitExtended(sqeCtx); err != nil {
		t.pool.PutExtended(ext)
		return err
	}

	return nil
}

// HandleCQE processes a CQE that may be a zero-copy completion or notification.
// Returns true if the CQE was handled, false if it's not a ZC tracker CQE.
func (t *ZCTracker) HandleCQE(cqe CQEView) bool {
	if !cqe.Extended() {
		return false
	}

	ext := cqe.ExtSQE()
	ctx := asZCCtx(ext)
	if ctx == nil {
		return false
	}
	owner, _ := extAnchors(ext).owner.(*ZCTracker)
	if owner != t {
		return false
	}

	handler := zcGetHandler(ext)
	if handler == nil {
		return false
	}

	return t.dispatchCQE(cqe, ext, ctx, handler)
}

// dispatchCQE handles the two-CQE zero-copy state machine.
func (t *ZCTracker) dispatchCQE(cqe CQEView, ext *ExtSQE, ctx *zcCtx, handler ZCHandler) bool {
	tracker := zcGetTrackerData(ctx)

	if cqe.IsNotification() {
		// Second CQE: notification - buffer can be reused
		if tracker.markNotified() {
			handler.OnNotification(tracker.result())
		}
		// Notification CQEs are terminal for this operation, even if the state
		// machine has already advanced or the caller replays the CQE.
		t.pool.PutExtended(ext)
		return true
	}

	if cqe.HasMore() {
		// First CQE: operation completed, more CQEs coming
		if tracker.markCompleted(cqe.Res) {
			handler.OnCompleted(cqe.Res)
		}
		return true
	}

	// Single CQE without MORE flag - possibly EOPNOTSUPP
	// Complete immediately and return to pool
	if tracker.tryInvokeOnce() {
		handler.OnCompleted(cqe.Res)
		// No notification expected, complete now
		handler.OnNotification(cqe.Res)
		t.pool.PutExtended(ext)
	}
	return true
}

// zcTrackerCQEHandler is the static CQE dispatcher for zero-copy tracking.
func zcTrackerCQEHandler(_ *Uring, _ *ioUringSqe, cqe *ioUringCqe) {
	view := CQEView{
		Res:   cqe.res,
		Flags: cqe.flags,
		ctx:   SQEContext(cqe.userData),
	}
	if !view.Extended() {
		return
	}
	ext := view.ExtSQE()
	tracker, _ := extAnchors(ext).owner.(*ZCTracker)
	if tracker == nil {
		return
	}
	tracker.HandleCQE(view)
}

// zcCtx is the raw scalar context layout for zero-copy tracking.
type zcCtx struct {
	Fn      Handler       // 8 bytes - static dispatch function
	_       [40]byte      // reserved for future scalar state
	Tracker zcTrackerData // 16 bytes - state tracking
}

var _ [64 - unsafe.Sizeof(zcCtx{})]struct{}

// zcInitCtx initializes a zcCtx in the ExtSQE UserData.
func zcInitCtx(ext *ExtSQE, tracker *ZCTracker, handler ZCHandler) *zcCtx {
	ctx := (*zcCtx)(unsafe.Pointer(&ext.UserData[0]))
	anchors := extAnchors(ext)
	anchors.owner = tracker
	anchors.handler = zcHandlerOrDefault(handler)
	// Initialize tracker state
	ctx.Tracker.used.Store(false)
	ctx.Tracker.state.Store(zcStateSubmitted)
	ctx.Tracker.opResult.Store(0)
	return ctx
}

// asZCCtx interprets ExtSQE UserData as zcCtx.
func asZCCtx(ext *ExtSQE) *zcCtx {
	return (*zcCtx)(unsafe.Pointer(&ext.UserData[0]))
}

// zcGetTrackerData returns the tracker data from a zcCtx.
func zcGetTrackerData(ctx *zcCtx) *zcTrackerData {
	return &ctx.Tracker
}

// zcGetHandler returns the anchored ZCHandler.
func zcGetHandler(ext *ExtSQE) ZCHandler {
	handler, _ := extAnchors(ext).handler.(ZCHandler)
	return handler
}

func zcHandlerOrDefault(handler ZCHandler) ZCHandler {
	if handler == nil {
		return NoopZCHandler{}
	}
	return handler
}
