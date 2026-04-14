// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"sync/atomic"
	"testing"
)

type incrementalErrorHandler struct {
	dataCalls     atomic.Int32
	completeCalls atomic.Int32
	errorCalls    atomic.Int32
	lastErr       atomic.Value
}

func (h *incrementalErrorHandler) OnData([]byte, bool) {
	h.dataCalls.Add(1)
}

func (h *incrementalErrorHandler) OnComplete() {
	h.completeCalls.Add(1)
}

func (h *incrementalErrorHandler) OnError(err error) {
	h.errorCalls.Add(1)
	h.lastErr.Store(err)
}

func newIncrementalBoundsTestState(t *testing.T) (*IncrementalReceiver, *ExtSQE, *incrementalErrorHandler) {
	t.Helper()

	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{
		pool:       pool,
		bufSize:    4,
		bufBacking: make([]byte, 8),
	}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("Extended returned nil")
	}
	ext.SQE.opcode = IORING_OP_RECV

	handler := &incrementalErrorHandler{}
	_ = incInitCtx(ext, receiver, handler)
	return receiver, ext, handler
}

func TestIncrementalHandleCQERejectsOutOfRangeBufferID(t *testing.T) {
	receiver, ext, handler := newIncrementalBoundsTestState(t)
	cqe := CQEView{
		Res:   4,
		Flags: IORING_CQE_F_BUFFER | (3 << IORING_CQE_BUFFER_SHIFT),
		ctx:   PackExtended(ext),
	}

	if !receiver.HandleCQE(cqe) {
		t.Fatal("HandleCQE did not claim incremental CQE")
	}
	if handler.dataCalls.Load() != 0 || handler.completeCalls.Load() != 0 {
		t.Fatal("invalid buffer CQE should not deliver data or completion")
	}
	if handler.errorCalls.Load() != 1 {
		t.Fatalf("errorCalls = %d, want 1", handler.errorCalls.Load())
	}
	lastErr, _ := handler.lastErr.Load().(error)
	if !errors.Is(lastErr, ErrInvalidParam) {
		t.Fatalf("OnError got %v, want %v", lastErr, ErrInvalidParam)
	}
}

func TestIncrementalHandleCQEWithHandlerRejectsOutOfRangeBufferID(t *testing.T) {
	receiver, ext, handler := newIncrementalBoundsTestState(t)
	cqe := CQEView{
		Res:   4,
		Flags: IORING_CQE_F_BUFFER | (3 << IORING_CQE_BUFFER_SHIFT),
		ctx:   PackExtended(ext),
	}

	receiver.handleCQEWithHandler(cqe, handler)
	if handler.dataCalls.Load() != 0 || handler.completeCalls.Load() != 0 {
		t.Fatal("invalid buffer CQE should not deliver data or completion")
	}
	if handler.errorCalls.Load() != 1 {
		t.Fatalf("errorCalls = %d, want 1", handler.errorCalls.Load())
	}
	lastErr, _ := handler.lastErr.Load().(error)
	if !errors.Is(lastErr, ErrInvalidParam) {
		t.Fatalf("OnError got %v, want %v", lastErr, ErrInvalidParam)
	}
}

type incrementalFragmentHandler struct {
	fragments [][]byte
	hasMore   []bool
	completed atomic.Int32
	errors    atomic.Int32
}

func (h *incrementalFragmentHandler) OnData(buf []byte, hasMore bool) {
	frag := make([]byte, len(buf))
	copy(frag, buf)
	h.fragments = append(h.fragments, frag)
	h.hasMore = append(h.hasMore, hasMore)
}

func (h *incrementalFragmentHandler) OnComplete() {
	h.completed.Add(1)
}

func (h *incrementalFragmentHandler) OnError(error) {
	h.errors.Add(1)
}

func TestIncrementalHandleResolvedCQEUsesFragmentOffsets(t *testing.T) {
	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{
		pool:       pool,
		bufSize:    4,
		bufBacking: []byte("abcdefgh"),
	}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	ext.SQE.opcode = IORING_OP_RECV

	handler := &incrementalFragmentHandler{}
	ctx := incInitCtx(ext, receiver, handler)
	tracker := incGetTrackerData(ctx)

	first := CQEView{
		Res:   2,
		Flags: IORING_CQE_F_BUFFER | IORING_CQE_F_BUF_MORE | (1 << IORING_CQE_BUFFER_SHIFT),
		ctx:   PackExtended(ext),
	}
	if !receiver.handleResolvedCQE(first, ext, tracker, handler) {
		t.Fatal("first fragment CQE was not handled")
	}
	if got := string(handler.fragments[0]); got != "ef" {
		t.Fatalf("first fragment: got %q, want %q", got, "ef")
	}
	if !handler.hasMore[0] {
		t.Fatal("first fragment should report hasMore")
	}
	if got := pool.Extended(); got != nil {
		t.Fatalf("ExtSQE returned too early: got %p, want nil", got)
	}

	second := CQEView{
		Res:   2,
		Flags: IORING_CQE_F_BUFFER | (1 << IORING_CQE_BUFFER_SHIFT),
		ctx:   PackExtended(ext),
	}
	if !receiver.handleResolvedCQE(second, ext, tracker, handler) {
		t.Fatal("second fragment CQE was not handled")
	}
	if got := string(handler.fragments[1]); got != "gh" {
		t.Fatalf("second fragment: got %q, want %q", got, "gh")
	}
	if handler.hasMore[1] {
		t.Fatal("final fragment should not report hasMore")
	}
	if handler.completed.Load() != 1 {
		t.Fatalf("OnComplete calls = %d, want 1", handler.completed.Load())
	}
	if handler.errors.Load() != 0 {
		t.Fatalf("OnError calls = %d, want 0", handler.errors.Load())
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected final fragment CQE to return ExtSQE to pool")
	}
}

func TestIncrementalHandleResolvedCQERejectsBufferIDChangeMidStream(t *testing.T) {
	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{
		pool:       pool,
		bufSize:    4,
		bufBacking: []byte("abcdefgh"),
	}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	ext.SQE.opcode = IORING_OP_RECV

	handler := &incrementalErrorHandler{}
	ctx := incInitCtx(ext, receiver, handler)
	tracker := incGetTrackerData(ctx)

	first := CQEView{
		Res:   2,
		Flags: IORING_CQE_F_BUFFER | IORING_CQE_F_BUF_MORE | (1 << IORING_CQE_BUFFER_SHIFT),
		ctx:   PackExtended(ext),
	}
	if !receiver.handleResolvedCQE(first, ext, tracker, handler) {
		t.Fatal("first fragment CQE was not handled")
	}

	second := CQEView{
		Res:   1,
		Flags: IORING_CQE_F_BUFFER | (0 << IORING_CQE_BUFFER_SHIFT),
		ctx:   PackExtended(ext),
	}
	if !receiver.handleResolvedCQE(second, ext, tracker, handler) {
		t.Fatal("second fragment CQE was not handled")
	}
	if handler.errorCalls.Load() != 1 {
		t.Fatalf("errorCalls = %d, want 1", handler.errorCalls.Load())
	}
	lastErr, _ := handler.lastErr.Load().(error)
	if !errors.Is(lastErr, ErrInvalidParam) {
		t.Fatalf("OnError got %v, want %v", lastErr, ErrInvalidParam)
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected invalid fragment CQE to return ExtSQE to pool")
	}
}

func TestIncrementalHandleCQEReturnsExtSQEOnNilHandler(t *testing.T) {
	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{pool: pool}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	ext.SQE.opcode = IORING_OP_RECV
	_ = incInitCtx(ext, receiver, nil)

	cqe := CQEView{
		Res:   0,
		Flags: IORING_CQE_F_BUFFER,
		ctx:   PackExtended(ext),
	}
	if !receiver.HandleCQE(cqe) {
		t.Fatal("HandleCQE did not claim incremental CQE")
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected nil-handler path to return ExtSQE to pool")
	}
}

func TestIncrementalStaticHandlerReturnsExtSQEOnNilHandler(t *testing.T) {
	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{pool: pool}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	ext.SQE.opcode = IORING_OP_RECV
	_ = incInitCtx(ext, receiver, nil)

	incrementalCQEHandler(nil, nil, &ioUringCqe{
		res:      0,
		flags:    IORING_CQE_F_BUFFER,
		userData: uint64(PackExtended(ext)),
	})
	if got := pool.Extended(); got == nil {
		t.Fatal("expected static nil-handler path to return ExtSQE to pool")
	}
}

func TestIncrementalRecvAppliesReadLikeFlags(t *testing.T) {
	ring := newWrapperTestRing(t)
	ring.readLikeOpFlags = IOSQE_FIXED_FILE

	pool := NewContextPools(1)
	receiver := NewIncrementalReceiver(ring, pool, 7, 256, make([]byte, 256), 1)

	if err := receiver.Recv(11, nil); err != nil {
		t.Fatalf("Recv: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if got, want := sqe.flags, uint8(IOSQE_BUFFER_SELECT|IOSQE_FIXED_FILE); got != want {
		t.Fatalf("SQE.flags = %#x, want %#x", got, want)
	}
	if got := sqe.bufIndex; got != 7 {
		t.Fatalf("SQE.bufIndex = %d, want 7", got)
	}
}
