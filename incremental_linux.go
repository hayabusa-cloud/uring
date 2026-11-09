// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"math"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
)

// IncrementalHandler handles incremental receive completion events.
type IncrementalHandler interface {
	// OnData is called with the newly received fragment.
	// bufferMore reports IORING_CQE_F_BUF_MORE: the selected buffer remains
	// incrementally consumable. It does not report parent-request liveness.
	// The buffer is valid until this callback returns.
	OnData(buf []byte, bufferMore bool)

	// OnComplete is called after a single-shot receive succeeds. The selected
	// buffer may remain incrementally consumable by a later operation.
	OnComplete()

	// OnError is called when an error occurs.
	OnError(err error)
}

// IncrementalReceiver manages single-shot receives over an IOU_PBUF_RING_INC
// group. It tracks a separate consumed offset for each buffer ID.
// IORING_CQE_F_BUF_MORE reports that the kernel retained the selected buffer
// for later incremental consumption.
type IncrementalReceiver struct {
	ring          *Uring
	pool          *ContextPools
	groupID       uint16
	bufSize       int
	bufBacking    []byte   // Buffer backing memory (GC-safe)
	bufferOffsets []uint32 // Completions for different buffer IDs can interleave.
}

// NewIncrementalReceiver creates an incremental receiver over entries
// contiguous buffers in bufBacking. Buffer ID i must identify
// bufBacking[i*bufSize:(i+1)*bufSize]. Create the receiver before the group's
// first receive and use it for every receive that consumes the group.
// Caller-side completion dispatch must serialize its CQEs in CQ order.
//
// Before calling NewIncrementalReceiver, register the same group with
// [Uring.RegisterBufRingIncremental], add each corresponding bufBacking slice
// with [BufferRing.Add] using the matching buffer ID, and publish them with
// [BufferRing.Advance]. The receiver keeps bufBacking reachable but does not own
// the BufferRing registration, which remains owned by Uring until Stop.
func NewIncrementalReceiver(ring *Uring, pool *ContextPools, groupID uint16, bufSize int, bufBacking []byte, entries int) *IncrementalReceiver {
	r := &IncrementalReceiver{
		ring:       ring,
		pool:       pool,
		groupID:    groupID,
		bufSize:    bufSize,
		bufBacking: bufBacking,
	}
	if entries > 0 && entries <= 1<<15 {
		r.bufferOffsets = make([]uint32, entries)
	}
	return r
}

// Recv submits one single-shot receive over the incremental buffer group.
// The handler receives OnData for a non-empty result. Its bufferMore argument
// reports whether the selected buffer remains available to later operations.
// OnComplete runs after the request's successful CQE, including when
// bufferMore is true.
//
// A nil return means the request was queued. Recv returns iox.ErrWouldBlock if
// the context pool or submission queue is full.
func (r *IncrementalReceiver) Recv(fd iofd.FD, handler IncrementalHandler) error {
	if !r.valid() {
		return ErrInvalidParam
	}
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

	ext.SQE = ioUringSqe{}
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

func (r *IncrementalReceiver) valid() bool {
	return r != nil && r.ring != nil && r.pool != nil &&
		r.bufSize > 0 && r.bufSize <= math.MaxInt32 &&
		len(r.bufferOffsets) > 0 &&
		len(r.bufBacking)/r.bufSize >= len(r.bufferOffsets)
}

// HandleCQE dispatches a receive CQE owned by this receiver and returns true.
// It returns false for a CQE owned by another receiver or operation.
// Caller must serialize dispatch in CQ order and dispatch each CQE once.
// Handling returns the request's ExtSQE to its pool after callbacks finish;
// copy any needed context-dependent facts before calling HandleCQE.
func (r *IncrementalReceiver) HandleCQE(cqe CQEView) bool {
	if !cqe.Extended() {
		return false
	}

	if cqe.Op() != IORING_OP_RECV {
		return false
	}

	ext := cqe.ExtSQE()
	if ext == nil {
		return false
	}
	owner, _ := extAnchors(ext).owner.(*IncrementalReceiver)
	if owner != r {
		return false
	}

	handler := incGetHandler(ext)
	if handler == nil {
		handler = NoopIncrementalHandler{}
	}
	return r.handleResolvedCQE(cqe, ext, handler)
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
	if ext == nil {
		return
	}
	receiver, _ := extAnchors(ext).owner.(*IncrementalReceiver)
	if receiver == nil {
		return
	}
	handler := incGetHandler(ext)
	if handler == nil {
		handler = NoopIncrementalHandler{}
	}
	receiver.handleResolvedCQE(view, ext, handler)
}

// handleCQEWithHandler processes a CQE with the supplied handler.
// It resolves the anchored extended state before delegating to the shared CQE path.
func (r *IncrementalReceiver) handleCQEWithHandler(cqe CQEView, handler IncrementalHandler) bool {
	if !cqe.Extended() {
		return false
	}
	ext := cqe.ExtSQE()
	if ext == nil {
		return false
	}
	if handler == nil {
		handler = NoopIncrementalHandler{}
	}
	return r.handleResolvedCQE(cqe, ext, handler)
}

// handleResolvedCQE processes one incremental receive CQE after ownership,
// rooting, and handler selection have already been resolved.
func (r *IncrementalReceiver) handleResolvedCQE(cqe CQEView, ext *ExtSQE, handler IncrementalHandler) bool {
	if cqe.Res <= 0 {
		// A worker can consume the selected buffer before receiving any data.
		if bufID := cqeBufID(cqe.Flags); cqeHasBuffer(cqe.Flags) && !cqeHasBufferMore(cqe.Flags) && int(bufID) < len(r.bufferOffsets) {
			r.bufferOffsets[bufID] = 0
		}
		if cqe.Res < 0 {
			handler.OnError(errFromErrno(uintptr(-cqe.Res)))
		} else {
			handler.OnComplete()
		}
		r.pool.PutExtended(ext)
		return true
	}

	bufferMore := cqeHasBufferMore(cqe.Flags)
	buf, bufID, nextOffset, err := r.bufferFromCQE(cqe)
	if err != nil {
		handler.OnError(err)
		r.pool.PutExtended(ext)
		return true
	}

	if bufferMore {
		r.bufferOffsets[bufID] = uint32(nextOffset)
	} else {
		r.bufferOffsets[bufID] = 0
	}

	handler.OnData(buf, bufferMore)
	handler.OnComplete()
	r.pool.PutExtended(ext)

	return true
}

func (r *IncrementalReceiver) bufferFromCQE(cqe CQEView) ([]byte, uint16, int, error) {
	if !cqe.HasBuffer() {
		return nil, 0, 0, ErrInvalidParam
	}
	bufID := cqe.BufID()
	if int(bufID) >= len(r.bufferOffsets) {
		return nil, bufID, 0, ErrInvalidParam
	}
	bufferOffset := int(r.bufferOffsets[bufID])
	if bufferOffset > r.bufSize || int(cqe.Res) > r.bufSize-bufferOffset {
		return nil, bufID, 0, ErrInvalidParam
	}
	nextOffset := bufferOffset + int(cqe.Res)
	off := int(bufID)*r.bufSize + bufferOffset
	end := int(bufID)*r.bufSize + nextOffset
	slotEnd := int(bufID+1) * r.bufSize
	if off < 0 || end < off || end > slotEnd || end > len(r.bufBacking) {
		return nil, bufID, 0, ErrInvalidParam
	}
	return r.bufBacking[off:end], bufID, nextOffset, nil
}

// incCtx is the raw scalar dispatch context for incremental receive.
type incCtx struct {
	Fn Handler  // 8 bytes - static dispatch function
	_  [56]byte // reserved for future scalar state
}

var _ [64 - unsafe.Sizeof(incCtx{})]struct{}

// incInitCtx initializes an incCtx in the ExtSQE UserData.
func incInitCtx(ext *ExtSQE, receiver *IncrementalReceiver, handler IncrementalHandler) *incCtx {
	ctx := (*incCtx)(unsafe.Pointer(&ext.UserData[0]))
	anchors := extAnchors(ext)
	anchors.owner = receiver
	anchors.handler = handler
	return ctx
}

// incGetHandler returns the anchored IncrementalHandler.
func incGetHandler(ext *ExtSQE) IncrementalHandler {
	handler, _ := extAnchors(ext).handler.(IncrementalHandler)
	return handler
}
