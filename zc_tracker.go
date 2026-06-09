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

// Zero-copy send states for notification and no-notification release paths.
const (
	zcStateSubmitted     uint32 = iota // SQE submitted, waiting for data CQE
	zcStateEarlyNotified               // Notification CQE arrived before data CQE
	zcStateCompleted                   // Data CQE received (IORING_CQE_F_MORE set)
	zcStateNotified                    // Terminal state; buffer and ExtSQE may be released
)

// zcTrackerData tracks zero-copy send state in ExtSQE UserData.
// Layout: 16 bytes, fits in Ctx0V5.Data[16].
type zcTrackerData struct {
	meta atomic.Uint64 // state in high 32 bits, result in low 32 bits
	_    [8]byte       // padding to 16 bytes
}

var _ [16 - unsafe.Sizeof(zcTrackerData{})]struct{}

//go:nosplit
func zcPackMeta(state uint32, result int32) uint64 {
	return uint64(state)<<32 | uint64(uint32(result))
}

//go:nosplit
func zcMetaState(meta uint64) uint32 {
	return uint32(meta >> 32)
}

//go:nosplit
func zcMetaResult(meta uint64) int32 {
	return int32(uint32(meta))
}

// markCompleted atomically transitions from Submitted to Completed.
// If a notification arrived early, it closes the lifecycle after the data CQE.
//
//go:nosplit
func (d *zcTrackerData) markCompleted(result int32) (complete bool, release bool) {
	for {
		meta := d.meta.Load()
		switch zcMetaState(meta) {
		case zcStateSubmitted:
			if d.meta.CompareAndSwap(meta, zcPackMeta(zcStateCompleted, result)) {
				return true, false
			}
		case zcStateEarlyNotified:
			if d.meta.CompareAndSwap(meta, zcPackMeta(zcStateNotified, result)) {
				return true, true
			}
		default:
			return false, false
		}
	}
}

// markNotification atomically processes a notification CQE.
// It returns true when the notification closes the lifecycle—OnNotification
// should run and the ExtSQE should be released.
//
//go:nosplit
func (d *zcTrackerData) markNotification() (ready bool) {
	for {
		meta := d.meta.Load()
		result := zcMetaResult(meta)
		switch zcMetaState(meta) {
		case zcStateCompleted:
			if d.meta.CompareAndSwap(meta, zcPackMeta(zcStateNotified, result)) {
				return true
			}
		case zcStateSubmitted:
			if d.meta.CompareAndSwap(meta, zcPackMeta(zcStateEarlyNotified, result)) {
				return false
			}
		default:
			return false
		}
	}
}

// markTerminalData atomically completes a terminal data CQE without MORE.
// If no notification arrived, this is the no-notification fallback. If a
// notification arrived early, this pairs with it and closes the lifecycle.
// Returns true when the lifecycle closes—OnCompleted, OnNotification, and
// pool release should all run.
//
//go:nosplit
func (d *zcTrackerData) markTerminalData(result int32) (ready bool) {
	for {
		meta := d.meta.Load()
		switch zcMetaState(meta) {
		case zcStateSubmitted, zcStateEarlyNotified:
			if d.meta.CompareAndSwap(meta, zcPackMeta(zcStateNotified, result)) {
				return true
			}
		default:
			return false
		}
	}
}

// result returns the operation result stored during completion.
//
//go:nosplit
func (d *zcTrackerData) result() int32 {
	return zcMetaResult(d.meta.Load())
}

// ZCHandler handles zero-copy send completion events.
type ZCHandler interface {
	// OnCompleted is called when the send CQE arrives.
	// The result is the number of bytes sent or a negative errno value.
	// The buffer must not be modified until OnNotification runs.
	OnCompleted(result int32)

	// OnNotification is called when the buffer can be safely reused.
	// In the notification path this callback runs after the data CQE is known,
	// even if the notification CQE arrived first. If the data CQE is terminal
	// without MORE, the tracker calls this from the terminal no-notification
	// fallback. The result is the original send result.
	OnNotification(result int32)
}

// ZCTracker manages zero-copy send operations through the notification path and
// the terminal no-notification fallback. The notification path pairs an
// operation CQE with MORE and a notification CQE; the tracker tolerates either
// CQE arrival order and releases only after the pair is complete. The fallback
// produces a terminal operation CQE without MORE.
//
// A terminal operation CQE without IORING_CQE_F_MORE is release-ready for this
// tracker. If no notification was observed, it is the no-notification fallback;
// if a notification arrived early, it closes the already paired lifecycle.
//
// The tracker ensures:
//   - Handlers are invoked in the correct order
//   - Each handler is invoked exactly once
//   - ExtSQE is returned to pool only after data plus notification, or terminal fallback
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
// Caller must keep buf valid and unmodified until OnNotification runs.
// Returns iox.ErrWouldBlock if the context pool is exhausted.
func (t *ZCTracker) SendZC(fd iofd.FD, buf []byte, handler ZCHandler, options ...OpOptionFunc) error {
	if len(buf) < 1 {
		return ErrInvalidParam
	}

	var flags uint8 = uringOpFlagsNone
	var ioprio uint16 = uringOpIOPrioNone
	dataOffset := 0
	n := len(buf)
	if len(options) > 0 {
		opt := defaultUringOpOption
		opt.Apply(options...)
		if opt.Offset < 0 || opt.Offset > int64(len(buf)) {
			return ErrInvalidParam
		}
		dataOffset = int(opt.Offset)
		remaining := len(buf) - dataOffset
		if opt.N != nil {
			if *opt.N < 1 || *opt.N > remaining {
				return ErrInvalidParam
			}
			n = *opt.N
		} else {
			n = remaining
			if n < 1 {
				return ErrInvalidParam
			}
		}
		flags = opt.Flags
		ioprio = opt.IOPrio
	}

	ext := t.pool.Extended()
	if ext == nil {
		return iox.ErrWouldBlock
	}

	// Initialize tracker data in UserData
	ctx := zcInitCtx(ext, t, handler)
	ctx.Fn = zcTrackerCQEHandler

	ext.SQE = ioUringSqe{}
	ext.SQE.opcode = IORING_OP_SEND_ZC
	ext.SQE.flags = flags | t.ring.writeLikeOpFlags
	ext.SQE.ioprio = ioprio
	ext.SQE.fd = int32(fd)
	ext.SQE.addr = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(dataOffset)
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
// Caller must keep the registered buffer range valid and unmodified until
// OnNotification runs.
// Returns iox.ErrWouldBlock if the context pool is exhausted.
func (t *ZCTracker) SendZCFixed(fd iofd.FD, bufIndex int, offset int, length int, handler ZCHandler, options ...OpOptionFunc) error {
	buf := t.ring.RegisteredBuffer(bufIndex)
	if buf == nil {
		return ErrInvalidParam
	}
	if offset < 0 || length < 1 || offset > len(buf) || length > len(buf)-offset {
		return ErrInvalidParam
	}

	var flags uint8 = uringOpFlagsNone
	var ioprio uint16 = uringOpIOPrioNone
	dataOffset := 0
	n := length
	if len(options) > 0 {
		opt := defaultUringOpOption
		opt.Apply(options...)
		if opt.Offset < 0 || opt.Offset > int64(length) {
			return ErrInvalidParam
		}
		dataOffset = int(opt.Offset)
		remaining := length - dataOffset
		if opt.N != nil {
			if *opt.N < 1 || *opt.N > remaining {
				return ErrInvalidParam
			}
			n = *opt.N
		} else {
			n = remaining
			if n < 1 {
				return ErrInvalidParam
			}
		}
		flags = opt.Flags
		ioprio = opt.IOPrio
	}
	effectiveOffset := offset + dataOffset

	ext := t.pool.Extended()
	if ext == nil {
		return iox.ErrWouldBlock
	}

	// Initialize tracker data in UserData
	ctx := zcInitCtx(ext, t, handler)
	ctx.Fn = zcTrackerCQEHandler

	ext.SQE = ioUringSqe{}
	// IORING_RECVSEND_FIXED_BUF goes in ioprio, not IOSQE_FIXED_FILE in flags.
	ext.SQE.opcode = IORING_OP_SEND_ZC
	ext.SQE.flags = flags | t.ring.writeLikeOpFlags
	ext.SQE.ioprio = ioprio | IORING_RECVSEND_FIXED_BUF
	ext.SQE.fd = int32(fd)
	ext.SQE.addr = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(effectiveOffset)
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
//
// HandleCQE is for immediate dispatch in the caller's serialized completion
// loop. Caller-side completion code must keep completion referents reachable
// until CQE reap and serialize retirement.
func (t *ZCTracker) HandleCQE(cqe CQEView) bool {
	if !cqe.Extended() {
		return false
	}

	ext := cqe.ExtSQE()
	owner, _ := extAnchors(ext).owner.(*ZCTracker)
	if owner != t {
		return false
	}

	ctx := asZCCtx(ext)
	handler := zcGetHandler(ext)

	return t.dispatchCQE(cqe, ext, ctx, handler)
}

// dispatchCQE handles the zero-copy notification and release state machine.
func (t *ZCTracker) dispatchCQE(cqe CQEView, ext *ExtSQE, ctx *zcCtx, handler ZCHandler) bool {
	tracker := zcGetTrackerData(ctx)

	if cqe.IsNotification() {
		if tracker.markNotification() {
			handler.OnNotification(tracker.result())
			t.pool.PutExtended(ext)
		}
		return true
	}

	if cqe.HasMore() {
		// Data CQE with MORE: operation completed, notification may follow.
		complete, release := tracker.markCompleted(cqe.Res)
		if complete {
			handler.OnCompleted(cqe.Res)
		}
		if release {
			handler.OnNotification(cqe.Res)
			t.pool.PutExtended(ext)
		}
		return true
	}

	// Terminal data CQE without MORE: either terminal no-notification
	// fallback, or the data half of an early-notification pair.
	if tracker.markTerminalData(cqe.Res) {
		handler.OnCompleted(cqe.Res)
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
		ctx:   SQEContextFromRaw(cqe.userData),
	}
	if !view.Extended() {
		return
	}
	ext := view.ExtSQE()
	tracker, _ := extAnchors(ext).owner.(*ZCTracker)
	if tracker == nil {
		return
	}
	ctx := asZCCtx(ext)
	handler := zcGetHandler(ext)
	tracker.dispatchCQE(view, ext, ctx, handler)
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
	ctx.Tracker.meta.Store(zcPackMeta(zcStateSubmitted, 0))
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
