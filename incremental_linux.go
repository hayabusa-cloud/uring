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

// IncrementalHandler handles incremental receive completion events.
type IncrementalHandler interface {
	// OnData is called with the newly received fragment.
	// hasMore indicates whether additional data remains in the same buffer.
	// The buffer is valid until this callback returns.
	OnData(buf []byte, hasMore bool)

	// OnComplete is called when all fragments have been received.
	OnComplete()

	// OnError is called when an error occurs.
	OnError(err error)
}

// incrementalState tracks the incremental receive operation state.
type incrementalState uint32

const (
	incStateActive    incrementalState = iota // Operation is active
	incStateCompleted                         // All data received
	incStateFailed                            // Operation failed
)

// incrementalTrackerData tracks the multi-CQE state machine for incremental receive.
// Layout: 16 bytes, fits in Ctx0V5.Data[16].
type incrementalTrackerData struct {
	state      atomic.Uint32 // 4 bytes - state machine
	bufferID   atomic.Uint32 // 4 bytes - current buffer ID
	totalBytes atomic.Int32  // 4 bytes - total bytes received
	_          [4]byte       // padding to 16 bytes
}

var _ [16 - unsafe.Sizeof(incrementalTrackerData{})]struct{}

// transitionToCompleted atomically transitions from Active to Completed.
//
//go:nosplit
func (d *incrementalTrackerData) transitionToCompleted() bool {
	return d.state.CompareAndSwap(uint32(incStateActive), uint32(incStateCompleted))
}

// transitionToFailed atomically transitions from Active to Failed.
//
//go:nosplit
func (d *incrementalTrackerData) transitionToFailed() bool {
	return d.state.CompareAndSwap(uint32(incStateActive), uint32(incStateFailed))
}

// IncrementalReceiver manages receives that use `IOU_PBUF_RING_INC`.
// `IORING_CQE_F_BUF_MORE` reports that the current buffer still has unread data.
type IncrementalReceiver struct {
	ring       *Uring
	pool       *ContextPools
	groupID    uint16
	bufSize    int
	bufBacking []byte // Buffer backing memory (GC-safe)
	bufMask    uint16 // Entries - 1 for wrap-around
}

// NewIncrementalReceiver creates an incremental receiver.
func NewIncrementalReceiver(ring *Uring, pool *ContextPools, groupID uint16, bufSize int, bufBacking []byte, entries int) *IncrementalReceiver {
	return &IncrementalReceiver{
		ring:       ring,
		pool:       pool,
		groupID:    groupID,
		bufSize:    bufSize,
		bufBacking: bufBacking,
		bufMask:    uint16(entries - 1),
	}
}

// Recv submits an incremental receive operation.
// The handler will receive OnData for each data fragment, with hasMore indicating
// whether additional data remains. OnComplete is called when the message is fully received.
// Returns iox.ErrWouldBlock if the context pool is exhausted.
func (r *IncrementalReceiver) Recv(fd iofd.FD, handler IncrementalHandler) error {
	if handler == nil {
		handler = NoopIncrementalHandler{}
	}

	ext := r.pool.Extended()
	if ext == nil {
		return iox.ErrWouldBlock
	}

	// Initialize context
	ctx := incInitCtx(ext, r, handler)
	ctx.Fn = incrementalCQEHandler

	// Set up SQE for RECV with buffer selection
	ext.SQE.opcode = IORING_OP_RECV
	ext.SQE.fd = int32(fd)
	ext.SQE.addr = 0 // Kernel selects buffer
	ext.SQE.len = uint32(r.bufSize)
	ext.SQE.flags = IOSQE_BUFFER_SELECT | r.ring.readLikeOpFlags
	ext.SQE.bufIndex = r.groupID

	sqeCtx := PackExtended(ext)
	if err := r.ring.SubmitExtended(sqeCtx); err != nil {
		r.pool.PutExtended(ext)
		return err
	}

	return nil
}

// HandleCQE processes a CQE from an incremental receive operation.
// Returns true if the CQE was handled, false if it's not an incremental recv CQE.
func (r *IncrementalReceiver) HandleCQE(cqe CQEView) bool {
	if !cqe.Extended() {
		return false
	}

	if cqe.Op() != IORING_OP_RECV {
		return false
	}

	ext := cqe.ExtSQE()
	owner, _ := extAnchors(ext).owner.(*IncrementalReceiver)
	if owner != r {
		return false
	}

	ctx := asIncCtx(ext)
	if ctx == nil {
		return false
	}
	tracker := incGetTrackerData(ctx)
	handler := incGetHandler(ext)
	if handler == nil {
		r.pool.PutExtended(ext)
		return true
	}
	return r.handleResolvedCQE(cqe, ext, tracker, handler)
}

// incrementalCQEHandler is the static CQE dispatcher for incremental receive.
// The live receiver and handler anchors stay in the pooled ExtSQE sidecar.
var incrementalCQEHandler Handler = func(_ *Uring, _ *ioUringSqe, cqe *ioUringCqe) {
	view := CQEView{
		Res:   cqe.res,
		Flags: cqe.flags,
		ctx:   SQEContextFromRaw(cqe.userData),
	}
	if !view.Extended() {
		return
	}
	ext := view.ExtSQE()
	receiver, _ := extAnchors(ext).owner.(*IncrementalReceiver)
	if receiver == nil {
		return
	}
	ctx := asIncCtx(ext)
	if ctx == nil {
		return
	}
	tracker := incGetTrackerData(ctx)
	handler := incGetHandler(ext)
	if handler == nil {
		receiver.pool.PutExtended(ext)
		return
	}
	receiver.handleResolvedCQE(view, ext, tracker, handler)
}

// handleCQEWithHandler processes a CQE with the supplied handler.
// It resolves the anchored extended state before delegating to the shared CQE path.
func (r *IncrementalReceiver) handleCQEWithHandler(cqe CQEView, handler IncrementalHandler) bool {
	if !cqe.Extended() {
		return false
	}
	ext := cqe.ExtSQE()
	ctx := asIncCtx(ext)
	if ctx == nil {
		return false
	}
	return r.handleResolvedCQE(cqe, ext, incGetTrackerData(ctx), handler)
}

// handleResolvedCQE processes one incremental receive CQE after ownership,
// rooting, and handler selection have already been resolved.
func (r *IncrementalReceiver) handleResolvedCQE(cqe CQEView, ext *ExtSQE, tracker *incrementalTrackerData, handler IncrementalHandler) bool {
	// Handle error cases
	if cqe.Res < 0 {
		if tracker.transitionToFailed() {
			handler.OnError(errFromErrno(uintptr(-cqe.Res)))
		}
		r.pool.PutExtended(ext)
		return true
	}

	// Get buffer data
	hasMore := cqe.HasBufferMore()
	buf, bufID, err := r.bufferFromCQE(cqe, tracker)
	if err != nil {
		if tracker.transitionToFailed() {
			handler.OnError(err)
		}
		r.pool.PutExtended(ext)
		return true
	}

	// Track total bytes
	tracker.totalBytes.Add(cqe.Res)
	tracker.bufferID.Store(uint32(bufID))

	// Deliver data
	handler.OnData(buf, hasMore)

	// If no more data, complete
	if !hasMore {
		if tracker.transitionToCompleted() {
			handler.OnComplete()
		}
		r.pool.PutExtended(ext)
	}

	return true
}

func (r *IncrementalReceiver) bufferFromCQE(cqe CQEView, tracker *incrementalTrackerData) ([]byte, uint16, error) {
	if !cqe.HasBuffer() {
		return nil, 0, ErrInvalidParam
	}
	bufID := cqe.BufID()
	consumed := int(tracker.totalBytes.Load())
	if consumed != 0 && tracker.bufferID.Load() != uint32(bufID) {
		return nil, bufID, ErrInvalidParam
	}
	off := int(bufID)*r.bufSize + consumed
	end := off + int(cqe.Res)
	slotEnd := int(bufID+1) * r.bufSize
	if off < 0 || end < off || end > slotEnd || end > len(r.bufBacking) {
		return nil, bufID, ErrInvalidParam
	}
	return r.bufBacking[off:end], bufID, nil
}

// incCtx is the raw scalar context layout for incremental receive tracking.
type incCtx struct {
	Fn      Handler                // 8 bytes - static dispatch function
	_       [40]byte               // reserved for future scalar state
	Tracker incrementalTrackerData // 16 bytes - state tracking
}

var _ [64 - unsafe.Sizeof(incCtx{})]struct{}

// incInitCtx initializes an incCtx in the ExtSQE UserData.
func incInitCtx(ext *ExtSQE, receiver *IncrementalReceiver, handler IncrementalHandler) *incCtx {
	ctx := (*incCtx)(unsafe.Pointer(&ext.UserData[0]))
	anchors := extAnchors(ext)
	anchors.owner = receiver
	anchors.handler = handler
	ctx.Tracker.state.Store(uint32(incStateActive))
	ctx.Tracker.bufferID.Store(0)
	ctx.Tracker.totalBytes.Store(0)
	return ctx
}

// asIncCtx interprets ExtSQE UserData as incCtx.
func asIncCtx(ext *ExtSQE) *incCtx {
	return (*incCtx)(unsafe.Pointer(&ext.UserData[0]))
}

// incGetTrackerData returns the tracker data from an incCtx.
func incGetTrackerData(ctx *incCtx) *incrementalTrackerData {
	return &ctx.Tracker
}

// incGetHandler returns the anchored IncrementalHandler.
func incGetHandler(ext *ExtSQE) IncrementalHandler {
	handler, _ := extAnchors(ext).handler.(IncrementalHandler)
	return handler
}
