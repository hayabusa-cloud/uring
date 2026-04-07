// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"code.hybscloud.com/iobuf"
	"code.hybscloud.com/iox"
	"errors"
	"testing"
	"unsafe"
)

type zcrxCQE32 struct {
	base ioUringCqe
	ext  ZCRXCqe
}

type zcrxRecordingHandler struct {
	errors          []error
	data            []*ZCRXBuffer
	stopped         int
	continueOnError bool
	events          []string
}

type zcrxBlockingStoppedHandler struct {
	onStopped func()
}

func (h *zcrxRecordingHandler) OnData(buf *ZCRXBuffer) bool {
	h.data = append(h.data, buf)
	h.events = append(h.events, "data")
	return true
}

func (h *zcrxRecordingHandler) OnError(err error) bool {
	h.errors = append(h.errors, err)
	h.events = append(h.events, "error")
	return h.continueOnError
}

func (h *zcrxRecordingHandler) OnStopped() {
	h.stopped++
	h.events = append(h.events, "stopped")
}

func (h *zcrxBlockingStoppedHandler) OnData(*ZCRXBuffer) bool { return true }
func (h *zcrxBlockingStoppedHandler) OnError(error) bool      { return true }
func (h *zcrxBlockingStoppedHandler) OnStopped() {
	if h.onStopped != nil {
		h.onStopped()
	}
}

func TestZCRXReceiverHandleCQETerminalErrorPreservesErrorAndClearsIdentity(t *testing.T) {
	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	h := &zcrxRecordingHandler{}
	r := &ZCRXReceiver{pool: pool, handler: h, ext: ext}
	r.state.Store(zcrxStateActive)
	view := CQEView{Res: -int32(EINVAL), ctx: PackExtended(ext)}

	r.handleCQE(view, &ioUringCqe{})

	if len(h.errors) != 1 {
		t.Fatalf("OnError calls: got %d, want 1", len(h.errors))
	}
	if !errors.Is(h.errors[0], ErrInvalidParam) {
		t.Fatalf("OnError err: got %v, want %v", h.errors[0], ErrInvalidParam)
	}
	if h.stopped != 1 {
		t.Fatalf("OnStopped calls: got %d, want 1", h.stopped)
	}
	if got := r.userData.Load(); got != 0 {
		t.Fatalf("userData: got %d, want 0", got)
	}
	if r.ext != nil {
		t.Fatal("ext: got non-nil, want nil")
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected terminal CQE to return ExtSQE to pool")
	}
}

func TestZCRXReceiverHandleCQETerminalErrorDoesNotCancelDeadRequest(t *testing.T) {
	prev := zcrxAsyncCancelByUserData
	defer func() { zcrxAsyncCancelByUserData = prev }()

	calls := 0
	zcrxAsyncCancelByUserData = func(ring *Uring, userData uint64) error {
		calls++
		return nil
	}

	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	h := &zcrxRecordingHandler{}
	r := &ZCRXReceiver{pool: pool, handler: h, ext: ext}
	r.userData.Store(654)
	r.state.Store(zcrxStateActive)

	view := CQEView{Res: -int32(EINVAL), ctx: PackExtended(ext)}
	r.handleCQE(view, &ioUringCqe{})

	if calls != 0 {
		t.Fatalf("terminal error issued %d cancel submissions, want 0", calls)
	}
	if got := r.state.Load(); got != zcrxStateStopped {
		t.Fatalf("state: got %d, want %d", got, zcrxStateStopped)
	}
	if len(h.errors) != 1 {
		t.Fatalf("OnError calls: got %d, want 1", len(h.errors))
	}
	if h.stopped != 1 {
		t.Fatalf("OnStopped calls: got %d, want 1", h.stopped)
	}
}

func TestZCRXConfigValidateRqEntriesPowerOfTwo(t *testing.T) {
	pageSize := int(iobuf.PageSize)
	tests := []struct {
		name      string
		rqEntries int
		wantErr   bool
	}{
		{name: "zero", rqEntries: 0, wantErr: true},
		{name: "negative", rqEntries: -2, wantErr: true},
		{name: "non power of two", rqEntries: 3, wantErr: true},
		{name: "power of two", rqEntries: 1024, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   0,
				RqEntries: tt.rqEntries,
				ChunkSize: pageSize, // valid ChunkSize so only RqEntries is under test
			}

			err := cfg.validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidParam) {
					t.Fatalf("validate() error = %v, want %v", err, ErrInvalidParam)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() error = %v, want nil", err)
			}
		})
	}
}

func TestZCRXConfigValidateChunkSize(t *testing.T) {
	pageSize := int(iobuf.PageSize)
	tests := []struct {
		name      string
		chunkSize int
		wantErr   bool
	}{
		{name: "zero", chunkSize: 0, wantErr: true},
		{name: "negative", chunkSize: -4096, wantErr: true},
		{name: "not power of two", chunkSize: pageSize * 3, wantErr: true},
		{name: "not page multiple", chunkSize: pageSize / 2, wantErr: true},
		{name: "one page", chunkSize: pageSize, wantErr: false},
		{name: "two pages", chunkSize: pageSize * 2, wantErr: false},
		{name: "four pages", chunkSize: pageSize * 4, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   0,
				RqEntries: 1024,
				ChunkSize: tt.chunkSize,
			}

			err := cfg.validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidParam) {
					t.Fatalf("validate() error = %v, want %v", err, ErrInvalidParam)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() error = %v, want nil", err)
			}
		})
	}
}

func TestZCRXRefillQSeedUsesChunkBoundaries(t *testing.T) {
	head := uint32(0)
	tail := uint32(0)
	rqes := make([]ZCRXRqe, 8)
	q := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}

	q.seed(4*1024, 1024, 0xABCD000000000000)

	if got := q.tail; got != 4 {
		t.Fatalf("tail: got %d, want 4", got)
	}
	for i, wantOffset := range []uint64{0, 1024, 2048, 3072} {
		want := wantOffset | 0xABCD000000000000
		if got := q.rqes[i].Off; got != want {
			t.Fatalf("rqes[%d].Off: got %#x, want %#x", i, got, want)
		}
		if got := q.rqes[i].Len; got != 1024 {
			t.Fatalf("rqes[%d].Len: got %d, want 1024", i, got)
		}
	}
}

func TestZCRXBufferReleasePublishesOriginalSlotLength(t *testing.T) {
	head := uint32(0)
	tail := uint32(0)
	rqes := make([]ZCRXRqe, 2)
	q := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	var wantOffset uint64 = 0xABCD000000001000
	var wantToken uint64 = 0xABCD000000000000
	var wantSlotLen uint32 = 4096
	buf := &ZCRXBuffer{
		data:    make([]byte, 512),
		slotLen: int(wantSlotLen),
		offset:  wantOffset,
		token:   wantToken,
		rq:      q,
	}

	if err := buf.Release(); err != nil {
		t.Fatalf("Release error: %v", err)
	}

	if got := q.tail; got != 1 {
		t.Fatalf("tail: got %d, want 1", got)
	}
	if got := q.rqes[0].Off; got != wantOffset {
		t.Fatalf("rqes[0].Off: got %#x, want %#x", got, wantOffset)
	}
	if got := q.rqes[0].Len; got != wantSlotLen {
		t.Fatalf("rqes[0].Len: got %d, want %d", got, wantSlotLen)
	}
}

func TestZCRXBufferReleaseReturnsErrWouldBlock(t *testing.T) {
	head := uint32(0)
	tail := uint32(1)
	rqes := make([]ZCRXRqe, 1)
	q := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	q.tail = 1 // ring full: tail == entries, head == 0

	buf := &ZCRXBuffer{
		data:    make([]byte, 256),
		slotLen: 4096,
		offset:  0xABCD000000001000,
		token:   0xABCD000000000000,
		rq:      q,
	}

	err := buf.Release()
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("Release error: got %v, want %v", err, iox.ErrWouldBlock)
	}
	if got := buf.released.Load(); got {
		t.Fatal("released: got true after failed release, want false so caller can retry")
	}
}

func TestZCRXBufferReleaseCanRetryAfterErrWouldBlock(t *testing.T) {
	head := uint32(0)
	tail := uint32(1)
	rqes := make([]ZCRXRqe, 1)
	q := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	q.tail = 1 // ring full: tail == entries, head == 0
	var wantOffset uint64 = 0xABCD000000001000
	var wantSlotLen uint32 = 4096
	buf := &ZCRXBuffer{
		data:    make([]byte, 256),
		slotLen: int(wantSlotLen),
		offset:  wantOffset,
		token:   0xABCD000000000000,
		rq:      q,
	}

	if err := buf.Release(); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("first Release error: got %v, want %v", err, iox.ErrWouldBlock)
	}

	head = 1 // kernel consumed one slot
	if err := buf.Release(); err != nil {
		t.Fatalf("second Release error: %v", err)
	}
	if got := q.tail; got != 2 {
		t.Fatalf("tail: got %d, want 2", got)
	}
	if got := q.rqes[0].Off; got != wantOffset {
		t.Fatalf("rqes[0].Off: got %#x, want %#x", got, wantOffset)
	}
	if got := q.rqes[0].Len; got != wantSlotLen {
		t.Fatalf("rqes[0].Len: got %d, want %d", got, wantSlotLen)
	}
}

func TestZCRXRefillQPushReturnsErrWouldBlockWhenFull(t *testing.T) {
	head := uint32(0)
	tail := uint32(1)
	rqes := make([]ZCRXRqe, 1)
	q := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	q.tail = 1 // ring full

	err := q.push(0, 0xABCD000000000000, 1024)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("push err: got %v, want %v", err, iox.ErrWouldBlock)
	}
	if got := q.tail; got != 1 {
		t.Fatalf("tail: got %d, want 1 (unchanged after full)", got)
	}
}

func TestZCRXReceiverDeliverDataRejectsOutOfBoundsCQE(t *testing.T) {
	area := make([]byte, 16)
	h := &zcrxRecordingHandler{}
	r := &ZCRXReceiver{
		area: zcrxArea{
			ptr:  unsafe.Pointer(unsafe.SliceData(area)),
			size: len(area),
		},
		handler: h,
	}
	view := CQEView{Res: 8, Flags: IORING_CQE_F_MORE}
	raw := &zcrxCQE32{}
	raw.ext.Off = 12

	r.deliverData(view, &raw.base)

	if len(h.data) != 0 {
		t.Fatalf("OnData calls: got %d, want 0", len(h.data))
	}
	if len(h.errors) != 1 {
		t.Fatalf("OnError calls: got %d, want 1", len(h.errors))
	}
	if h.errors[0] == nil || h.errors[0].Error() == "" {
		t.Fatal("expected detailed bounds error")
	}
	if h.stopped != 0 {
		t.Fatalf("OnStopped calls: got %d, want 0", h.stopped)
	}
}

func TestZCRXReceiverDeliverDataUsesDecodedAreaOffset(t *testing.T) {
	area := make([]byte, 64)
	copy(area[8:12], []byte{1, 2, 3, 4})
	h := &zcrxRecordingHandler{}
	const areaToken = 0xABCD000000000000
	r := &ZCRXReceiver{
		area: zcrxArea{
			ptr:   unsafe.Pointer(unsafe.SliceData(area)),
			size:  len(area),
			token: areaToken,
		},
		handler: h,
	}
	view := CQEView{Res: 4, Flags: IORING_CQE_F_MORE}
	raw := &zcrxCQE32{}
	raw.ext.Off = areaToken | 8

	r.deliverData(view, &raw.base)

	if len(h.errors) != 0 {
		t.Fatalf("OnError calls: got %d, want 0", len(h.errors))
	}
	if len(h.data) != 1 {
		t.Fatalf("OnData calls: got %d, want 1", len(h.data))
	}
	if got := h.data[0].Bytes(); len(got) != 4 || got[0] != 1 || got[3] != 4 {
		t.Fatalf("decoded data: got %v, want [1 2 3 4]", got)
	}
}

func TestZCRXReceiverDeliverDataUsesNoopHandlerWhenNil(t *testing.T) {
	area := make([]byte, 32)
	copy(area[4:8], []byte{9, 8, 7, 6})
	head := uint32(0)
	tail := uint32(0)
	rqes := make([]ZCRXRqe, 2)
	const areaToken = 0xABCD000000000000
	rq := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	r := &ZCRXReceiver{
		area: zcrxArea{
			ptr:       unsafe.Pointer(unsafe.SliceData(area)),
			size:      len(area),
			token:     areaToken,
			chunkSize: 4096,
			rq:        rq,
		},
		handler: NoopZCRXHandler{},
	}
	view := CQEView{Res: 4, Flags: IORING_CQE_F_MORE}
	raw := &zcrxCQE32{}
	raw.ext.Off = areaToken | 4

	r.deliverData(view, &raw.base)

	if got := rq.tail; got != 1 {
		t.Fatalf("tail: got %d, want 1", got)
	}
	if got := rq.rqes[0].Off; got != raw.ext.Off {
		t.Fatalf("rqes[0].Off: got %#x, want %#x", got, raw.ext.Off)
	}
	if got := rq.rqes[0].Len; got != 4096 {
		t.Fatalf("rqes[0].Len: got %d, want 4096", got)
	}
}

func TestZCRXReceiverDeliverDataZeroLengthPayload(t *testing.T) {
	h := &zcrxRecordingHandler{}
	head := uint32(0)
	tail := uint32(0)
	rqes := make([]ZCRXRqe, 2)
	const areaToken = 0xABCD000000000000
	rq := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	r := &ZCRXReceiver{
		handler: h,
		area: zcrxArea{
			token:     areaToken,
			chunkSize: 4096,
			rq:        rq,
		},
	}
	view := CQEView{Res: 0, Flags: IORING_CQE_F_MORE}
	raw := &zcrxCQE32{}
	raw.ext.Off = areaToken | 16

	r.deliverData(view, &raw.base)

	if len(h.errors) != 0 {
		t.Fatalf("OnError calls: got %d, want 0", len(h.errors))
	}
	if len(h.data) != 1 {
		t.Fatalf("OnData calls: got %d, want 1", len(h.data))
	}
	if got := h.data[0].Len(); got != 0 {
		t.Fatalf("buffer length: got %d, want 0", got)
	}
	if got := h.data[0].Bytes(); len(got) != 0 {
		t.Fatalf("buffer bytes length: got %d, want 0", len(got))
	}
	// Zero-length completions carry no refill identity; Release is a no-op.
	if err := h.data[0].Release(); err != nil {
		t.Fatalf("Release error: %v", err)
	}
	if got := rq.tail; got != 0 {
		t.Fatalf("tail: got %d, want 0 (no refill for zero-length)", got)
	}
}

func TestZCRXReceiverStopLeavesReceiverActiveWhenCancelFails(t *testing.T) {
	prev := zcrxAsyncCancelByUserData
	zcrxAsyncCancelByUserData = func(ring *Uring, userData uint64) error {
		return ErrInvalidParam
	}
	t.Cleanup(func() {
		zcrxAsyncCancelByUserData = prev
	})

	r := &ZCRXReceiver{}
	r.userData.Store(123)
	r.state.Store(zcrxStateActive)

	err := r.Stop()
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("Stop error: got %v, want %v", err, ErrInvalidParam)
	}
	if got := r.state.Load(); got != zcrxStateActive {
		t.Fatalf("state: got %d, want %d", got, zcrxStateActive)
	}
}

func TestNoopZCRXHandlerStopsWhenReleaseWouldBlock(t *testing.T) {
	head := uint32(0)
	tail := uint32(1)
	rqes := make([]ZCRXRqe, 1)
	q := &zcrxRefillQ{
		kHead:   &head,
		kTail:   &tail,
		rqes:    rqes,
		entries: uint32(len(rqes)),
		mask:    uint32(len(rqes) - 1),
	}
	q.tail = 1 // ring full

	buf := &ZCRXBuffer{
		data:    make([]byte, 64),
		slotLen: 4096,
		offset:  0xABCD000000002000,
		token:   0xABCD000000000000,
		rq:      q,
	}

	if got := (NoopZCRXHandler{}).OnData(buf); got {
		t.Fatal("OnData: got true, want false when release would block")
	}
	if got := buf.released.Load(); got {
		t.Fatal("released: got true after noop release failure, want false")
	}
}

func TestZCRXReceiverStartNormalizesNilHandler(t *testing.T) {
	pool := NewContextPools(4)

	r := &ZCRXReceiver{
		ring: &Uring{ioUring: &ioUring{
			ringFd: -1,
		}},
		pool: pool,
	}

	err := r.Start(42, nil)
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("Start(nil): got %v, want %v", err, ErrInvalidParam)
	}

	if _, ok := r.handler.(NoopZCRXHandler); !ok {
		t.Fatalf("handler type: got %T, want NoopZCRXHandler", r.handler)
	}
	if r.ext != nil {
		t.Fatal("ext: got non-nil, want nil after flush failure cleanup")
	}
	if got := r.userData.Load(); got != 0 {
		t.Fatalf("userData: got %d, want 0", got)
	}
	if got := r.state.Load(); got != zcrxStateIdle {
		t.Fatalf("state: got %d, want %d", got, zcrxStateIdle)
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected flush failure to return ExtSQE to pool")
	}
}

func TestZCRXReceiverStopUsesAtomicUserData(t *testing.T) {
	prev := zcrxAsyncCancelByUserData
	defer func() { zcrxAsyncCancelByUserData = prev }()

	calls := 0
	var seen uint64
	zcrxAsyncCancelByUserData = func(ring *Uring, userData uint64) error {
		calls++
		seen = userData
		return nil
	}

	r := &ZCRXReceiver{}
	r.userData.Store(321)
	r.state.Store(zcrxStateActive)

	if err := r.Stop(); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("cancel calls: got %d, want 1", calls)
	}
	if seen != 321 {
		t.Fatalf("cancel userData: got %d, want 321", seen)
	}
	if got := r.state.Load(); got != zcrxStateStopping {
		t.Fatalf("state: got %d, want %d", got, zcrxStateStopping)
	}
}

func TestZCRXReceiverHandleCQETerminalSuccessProcessesFinalPayload(t *testing.T) {
	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	area := make([]byte, 32)
	copy(area[4:8], []byte{9, 8, 7, 6})
	h := &zcrxRecordingHandler{}
	r := &ZCRXReceiver{
		pool: pool,
		area: zcrxArea{
			ptr:   unsafe.Pointer(unsafe.SliceData(area)),
			size:  len(area),
			token: 0xABCD000000000000,
		},
		handler: h,
		ext:     ext,
	}
	r.state.Store(zcrxStateActive)
	r.userData.Store(456)
	view := CQEView{Res: 4, ctx: PackExtended(ext)}
	raw := &zcrxCQE32{}
	raw.ext.Off = 0xABCD000000000000 | 4

	r.handleCQE(view, &raw.base)

	if len(h.errors) != 0 {
		t.Fatalf("OnError calls: got %d, want 0", len(h.errors))
	}
	if len(h.data) != 1 {
		t.Fatalf("OnData calls: got %d, want 1", len(h.data))
	}
	if got := h.data[0].Bytes(); len(got) != 4 || got[0] != 9 || got[3] != 6 {
		t.Fatalf("decoded data: got %v, want [9 8 7 6]", got)
	}
	if h.stopped != 1 {
		t.Fatalf("OnStopped calls: got %d, want 1", h.stopped)
	}
	if len(h.events) != 2 || h.events[0] != "data" || h.events[1] != "stopped" {
		t.Fatalf("event order: got %v, want [data stopped]", h.events)
	}
	if got := r.userData.Load(); got != 0 {
		t.Fatalf("userData: got %d, want 0", got)
	}
	if r.ext != nil {
		t.Fatal("ext: got non-nil, want nil")
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected terminal CQE to return ExtSQE to pool")
	}
}

func TestZCRXReceiverStoppedReportsCloseSafety(t *testing.T) {
	r := &ZCRXReceiver{}
	if !r.Stopped() {
		t.Fatal("Stopped: idle receiver should report true")
	}

	r.state.Store(zcrxStateActive)
	if r.Stopped() {
		t.Fatal("Stopped: active receiver should report false")
	}

	r.state.Store(zcrxStateStopping)
	if r.Stopped() {
		t.Fatal("Stopped: stopping receiver should report false")
	}

	r.state.Store(zcrxStateRetiring)
	if r.Stopped() {
		t.Fatal("Stopped: retiring receiver should report false")
	}

	r.state.Store(zcrxStateStopped)
	if !r.Stopped() {
		t.Fatal("Stopped: completed stop should report true")
	}
}

func TestZCRXReceiverFinishPublishesStoppedAfterOnStopped(t *testing.T) {
	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	release := make(chan struct{})
	observed := make(chan struct{}, 1)
	r := &ZCRXReceiver{pool: pool, ext: ext}
	h := &zcrxBlockingStoppedHandler{
		onStopped: func() {
			if r.Stopped() {
				t.Error("Stopped returned true before OnStopped completed")
			}
			observed <- struct{}{}
			<-release
		},
	}
	r.handler = h
	r.state.Store(zcrxStateStopping)

	done := make(chan struct{})
	go func() {
		r.finish(ext)
		close(done)
	}()

	<-observed
	if got := r.state.Load(); got != zcrxStateRetiring {
		t.Fatalf("state during OnStopped: got %d, want %d", got, zcrxStateRetiring)
	}
	if r.Stopped() {
		t.Fatal("Stopped returned true while OnStopped was still running")
	}

	close(release)
	<-done

	if got := r.state.Load(); got != zcrxStateStopped {
		t.Fatalf("final state: got %d, want %d", got, zcrxStateStopped)
	}
	if !r.Stopped() {
		t.Fatal("Stopped returned false after finish completed")
	}
	if r.ext != nil {
		t.Fatal("ext: got non-nil, want nil")
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected finish to return ExtSQE to pool")
	}
}

func TestZCRXReceiverCloseRequiresStoppedAndClosedRing(t *testing.T) {
	ring := &Uring{ioUring: &ioUring{ringFd: -1}}
	areaBuf := make([]byte, 64)
	ringAreaBuf := make([]byte, 64)
	r := &ZCRXReceiver{
		ring: ring,
		area: zcrxArea{
			ptr:      unsafe.Pointer(unsafe.SliceData(areaBuf)),
			size:     len(areaBuf),
			ringPtr:  unsafe.Pointer(unsafe.SliceData(ringAreaBuf)),
			ringSize: len(ringAreaBuf),
		},
	}

	r.state.Store(zcrxStateStopping)
	if err := r.Close(); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("Close error: got %v, want %v", err, iox.ErrWouldBlock)
	}

	r.state.Store(zcrxStateStopped)
	if err := r.Close(); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("Close error before ring stop: got %v, want %v", err, iox.ErrWouldBlock)
	}
	if r.area.ringPtr == nil || r.area.ptr == nil {
		t.Fatal("Close unmapped ZCRX area before ring stop")
	}

	ring.closed.Store(true)
	if err := r.Close(); err != nil {
		t.Fatalf("Close error after ring stop: got %v, want nil", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close error: got %v, want nil", err)
	}
	if r.area.ringPtr != nil {
		t.Fatal("area.ringPtr: got non-nil, want nil")
	}
	if r.area.ptr != nil {
		t.Fatal("area.ptr: got non-nil, want nil")
	}
	if r.area.rq != nil {
		t.Fatal("area.rq: got non-nil, want nil")
	}
	if r.area.ringSize != 0 {
		t.Fatalf("area.ringSize: got %d, want 0", r.area.ringSize)
	}
	if r.area.size != 0 {
		t.Fatalf("area.size: got %d, want 0", r.area.size)
	}
}
