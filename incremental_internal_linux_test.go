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
		pool:          pool,
		bufSize:       4,
		bufBacking:    make([]byte, 8),
		bufferOffsets: make([]uint32, 2),
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
	if got := receiver.pool.Extended(); got == nil {
		t.Fatal("invalid single-shot CQE did not return ExtSQE")
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
	if got := receiver.pool.Extended(); got == nil {
		t.Fatal("invalid single-shot CQE did not return ExtSQE")
	}
}

type incrementalFragmentHandler struct {
	fragments  [][]byte
	bufferMore []bool
	completed  atomic.Int32
	errors     atomic.Int32
}

func (h *incrementalFragmentHandler) OnData(buf []byte, bufferMore bool) {
	frag := make([]byte, len(buf))
	copy(frag, buf)
	h.fragments = append(h.fragments, frag)
	h.bufferMore = append(h.bufferMore, bufferMore)
}

func (h *incrementalFragmentHandler) OnComplete() {
	h.completed.Add(1)
}

func (h *incrementalFragmentHandler) OnError(error) {
	h.errors.Add(1)
}

func TestIncrementalHandleResolvedCQEPersistsBufferOffsetAcrossRequests(t *testing.T) {
	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{
		pool:          pool,
		bufSize:       4,
		bufBacking:    []byte("abcdefgh"),
		bufferOffsets: make([]uint32, 2),
	}
	handler := &incrementalFragmentHandler{}
	steps := []struct {
		flags      uint32
		want       string
		bufferMore bool
	}{
		{IORING_CQE_F_BUFFER | IORING_CQE_F_BUF_MORE | (1 << IORING_CQE_BUFFER_SHIFT), "ef", true},
		{IORING_CQE_F_BUFFER | (1 << IORING_CQE_BUFFER_SHIFT), "gh", false},
		{IORING_CQE_F_BUFFER | IORING_CQE_F_BUF_MORE, "ab", true},
		{IORING_CQE_F_BUFFER, "cd", false},
	}

	for i, step := range steps {
		ext := pool.Extended()
		if ext == nil {
			t.Fatalf("step %d: ExtSQE was not returned by the previous request", i)
		}
		ext.SQE.opcode = IORING_OP_RECV
		_ = incInitCtx(ext, receiver, handler)
		cqe := CQEView{Res: 2, Flags: step.flags, ctx: PackExtended(ext)}
		if !receiver.handleResolvedCQE(cqe, ext, handler) {
			t.Fatalf("step %d: CQE was not handled", i)
		}
		if got := string(handler.fragments[i]); got != step.want {
			t.Fatalf("step %d: fragment = %q, want %q", i, got, step.want)
		}
		if got := handler.bufferMore[i]; got != step.bufferMore {
			t.Fatalf("step %d: bufferMore = %t, want %t", i, got, step.bufferMore)
		}
	}

	if handler.completed.Load() != int32(len(steps)) {
		t.Fatalf("OnComplete calls = %d, want %d", handler.completed.Load(), len(steps))
	}
	if handler.errors.Load() != 0 {
		t.Fatalf("OnError calls = %d, want 0", handler.errors.Load())
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("final single-shot CQE did not return ExtSQE")
	}
}

func TestIncrementalHandleResolvedCQENonpositiveBufferReturn(t *testing.T) {
	tests := []struct {
		name       string
		res        int32
		flags      uint32
		wantOffset uint32
		wantData   string
	}{
		{name: "zero_retained", res: 0, wantOffset: 2, wantData: "cd"},
		{name: "error_retained", res: -int32(ECANCELED), wantOffset: 2, wantData: "cd"},
		{name: "zero_returned", res: 0, flags: IORING_CQE_F_BUFFER, wantData: "ab"},
		{name: "error_returned", res: -int32(ECANCELED), flags: IORING_CQE_F_BUFFER, wantData: "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewContextPools(1)
			receiver := &IncrementalReceiver{
				pool:          pool,
				bufSize:       4,
				bufBacking:    []byte("abcd"),
				bufferOffsets: make([]uint32, 1),
			}
			handler := &incrementalFragmentHandler{}

			dispatch := func(res int32, flags uint32) {
				t.Helper()
				ext := pool.Extended()
				if ext == nil {
					t.Fatal("ExtSQE was not returned by the previous request")
				}
				_ = incInitCtx(ext, receiver, handler)
				cqe := CQEView{Res: res, Flags: flags, ctx: PackExtended(ext)}
				if !receiver.handleResolvedCQE(cqe, ext, handler) {
					t.Fatal("CQE was not handled")
				}
			}

			dispatch(2, IORING_CQE_F_BUFFER|IORING_CQE_F_BUF_MORE)
			if receiver.bufferOffsets[0] != 2 {
				t.Fatalf("offset after partial result = %d, want 2", receiver.bufferOffsets[0])
			}
			dispatch(tt.res, tt.flags)
			if receiver.bufferOffsets[0] != tt.wantOffset {
				t.Fatalf("offset after nonpositive result = %d, want %d", receiver.bufferOffsets[0], tt.wantOffset)
			}
			dispatch(2, IORING_CQE_F_BUFFER)

			if got := len(handler.fragments); got != 2 {
				t.Fatalf("fragment count = %d, want 2", got)
			}
			if got := string(handler.fragments[1]); got != tt.wantData {
				t.Fatalf("final fragment = %q, want %q", got, tt.wantData)
			}
			if receiver.bufferOffsets[0] != 0 {
				t.Fatalf("final offset = %d, want 0", receiver.bufferOffsets[0])
			}
		})
	}
}

func TestIncrementalHandleResolvedCQEErrorReturnsExtSQE(t *testing.T) {
	pool := NewContextPools(1)
	receiver := &IncrementalReceiver{pool: pool}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	handler := &incrementalErrorHandler{}
	_ = incInitCtx(ext, receiver, handler)
	cqe := CQEView{Res: -int32(ECANCELED), ctx: PackExtended(ext)}

	if !receiver.handleResolvedCQE(cqe, ext, handler) {
		t.Fatal("error CQE was not handled")
	}
	if handler.errorCalls.Load() != 1 {
		t.Fatalf("OnError calls = %d, want 1", handler.errorCalls.Load())
	}
	lastErr, _ := handler.lastErr.Load().(error)
	if !errors.Is(lastErr, ErrCanceled) {
		t.Fatalf("OnError got %v, want %v", lastErr, ErrCanceled)
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("error CQE did not return ExtSQE")
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

func TestIncrementalRecvEncodesSingleShotReceive(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	ring.readLikeOpFlags = IOSQE_FIXED_FILE

	pool := NewContextPools(2)
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
	if got := sqe.len; got != 256 {
		t.Fatalf("SQE.len = %d, want 256", got)
	}
	if got := sqe.ioprio; got != 0 {
		t.Fatalf("SQE.ioprio = %#x, want 0", got)
	}
	if err := receiver.Recv(11, nil); err != nil {
		t.Fatalf("second Recv: %v", err)
	}
	if got := ring.ioUring.sqCount(); got != 2 {
		t.Fatalf("sqCount = %d, want 2", got)
	}
}

func TestIncrementalRecvAllocations(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	pool := NewContextPools(1)
	receiver := NewIncrementalReceiver(ring, pool, 7, 256, make([]byte, 256), 1)

	allocs := testing.AllocsPerRun(100, func() {
		if err := receiver.Recv(11, nil); err != nil {
			panic(err)
		}
		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		sqe := ring.ioUring.sq.sqeAt((tail - 1) & *ring.ioUring.sq.kRingMask)
		ext := SQEContextFromRaw(sqe.userData).ExtSQE()
		atomic.StoreUint32(ring.ioUring.sq.kHead, tail)
		pool.PutExtended(ext)
	})
	if allocs != 0 {
		t.Fatalf("Recv allocations = %v, want 0", allocs)
	}
}

func TestIncrementalRecvRejectsOutOfRangeKernelLength(t *testing.T) {
	receiver := &IncrementalReceiver{bufSize: oversizedSignedInt32(t)}
	if err := receiver.Recv(0, nil); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("Recv error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestIncrementalRecvRejectsInvalidLayoutBeforePoolMutation(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	pool := NewContextPools(1)
	receiver := NewIncrementalReceiver(ring, pool, 1, 4, make([]byte, 4), 2)

	if err := receiver.Recv(3, nil); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("Recv error = %v, want %v", err, ErrInvalidParam)
	}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("invalid layout consumed an ExtSQE")
	}
	pool.PutExtended(ext)
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("sqCount = %d, want 0", got)
	}
}
