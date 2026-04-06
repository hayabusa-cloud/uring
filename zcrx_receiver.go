// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iobuf"
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/zcall"
)

// ZCRXConfig configures a ZCRX receive instance bound to a NIC RX queue.
type ZCRXConfig struct {
	IfName       string // Network interface name (e.g., "eth0"). Required.
	RxQueue      int    // NIC RX queue index. Required.
	AreaSize     int    // Mapped receive area size in bytes.
	RqEntries    int    // Refill queue entries. Must be power of 2.
	ChunkSize    int    // Refill slot size. Must be a power-of-two multiple of page size.
	UseHugePages bool   // Use huge pages for the area.
}

// defaults fills zero-valued fields with sensible defaults.
func (c *ZCRXConfig) defaults() {
	if c.AreaSize == 0 {
		if c.UseHugePages {
			c.AreaSize = BufferSizeHuge * DefaultBufferNumHuge
		} else {
			c.AreaSize = BufferSizeMedium * DefaultBufferNumMedium
		}
	}
	if c.RqEntries == 0 {
		c.RqEntries = ioUringDefaultEntries
	}
	if c.ChunkSize == 0 {
		c.ChunkSize = int(iobuf.PageSize)
	}
}

// validate checks required fields and constraints.
func (c *ZCRXConfig) validate() error {
	if c.IfName == "" {
		return ErrInvalidParam
	}
	if c.RxQueue < 0 {
		return ErrInvalidParam
	}
	if c.AreaSize < 0 {
		return ErrInvalidParam
	}
	if c.RqEntries <= 0 || c.RqEntries&(c.RqEntries-1) != 0 {
		return ErrInvalidParam
	}
	pageSize := int(iobuf.PageSize)
	if c.ChunkSize <= 0 || c.ChunkSize&(c.ChunkSize-1) != 0 || c.ChunkSize%pageSize != 0 {
		return ErrInvalidParam
	}
	return nil
}

// ZCRXHandler handles ZCRX receive events.
type ZCRXHandler interface {
	// OnData handles received data. Return false for best-effort stop. Zero-length buffers mark TCP EOF and carry no refill identity.
	OnData(buf *ZCRXBuffer) bool

	// OnError handles a CQE error. Return false for best-effort stop.
	OnError(err error) bool

	// OnStopped runs once on the CQE path during terminal retirement before state becomes Stopped.
	OnStopped()
}

// ZCRXBuffer wraps a delivered ZCRX receive view.
// Payload memory is kernel-owned until Release returns nil.
// After a successful release, the buffer must not be reused.
type ZCRXBuffer struct {
	data     []byte
	slotLen  int          // original refill-slot length
	offset   uint64       // kernel offset for refill
	token    uint64       // area token for refill
	rq       *zcrxRefillQ // nil for zero-length EOF buffers
	released atomic.Bool
}

// Bytes returns the received data. Valid until Release.
//
//go:nosplit
func (b *ZCRXBuffer) Bytes() []byte {
	return b.data
}

// Len returns the received data length.
//
//go:nosplit
func (b *ZCRXBuffer) Len() int {
	return len(b.data)
}

// Release returns the buffer to the kernel via the refill queue.
// Call on the CQE path. Returns iox.ErrWouldBlock when the refill ring is full.
func (b *ZCRXBuffer) Release() error {
	if !b.released.CompareAndSwap(false, true) {
		return nil
	}
	if b.rq != nil {
		if err := b.rq.push(b.offset, b.token, b.slotLen); err != nil {
			b.released.Store(false)
			return err
		}
	}
	b.data = nil
	b.slotLen = 0
	b.offset = 0
	b.token = 0
	b.rq = nil
	zcrxBufPool.Put(b)
	return nil
}

// reset prepares the buffer for handler delivery from the pool.
func (b *ZCRXBuffer) reset(data []byte, slotLen int, offset, token uint64, rq *zcrxRefillQ) {
	b.data = data
	b.slotLen = slotLen
	b.offset = offset
	b.token = token
	b.rq = rq
	b.released.Store(false)
}

// zcrxRefillQ is a CQE-path refill ring.
type zcrxRefillQ struct {
	kHead   *uint32 // kernel consumer head (read-only)
	kTail   *uint32 // kernel-visible producer tail
	rqes    []ZCRXRqe
	tail    uint32 // local producer tail
	entries uint32
	mask    uint32
}

// push returns one buffer slot to the kernel.
func (q *zcrxRefillQ) push(offset, token uint64, length int) error {
	head := atomic.LoadUint32(q.kHead)
	if q.tail-head >= q.entries {
		return iox.ErrWouldBlock
	}
	idx := q.tail & q.mask
	q.rqes[idx].Off = (offset & ^uint64(IORING_ZCRX_AREA_MASK)) | token
	q.rqes[idx].Len = uint32(length)
	q.tail++
	atomic.StoreUint32(q.kTail, q.tail)
	return nil
}

// seed pre-fills the refill queue with initial area chunks.
func (q *zcrxRefillQ) seed(areaSize int, chunkSize int, token uint64) {
	if areaSize <= 0 || chunkSize <= 0 {
		return
	}
	for offset := 0; offset+chunkSize <= areaSize; offset += chunkSize {
		if q.push(uint64(offset), token, chunkSize) != nil {
			return
		}
	}
}

// available returns free producer slots.
func (q *zcrxRefillQ) available() uint32 {
	head := atomic.LoadUint32(q.kHead)
	return q.entries - (q.tail - head)
}

// zcrxArea owns the ZCRX mapped memory and refill queue.
type zcrxArea struct {
	ptr       unsafe.Pointer // mmap'd receive area
	size      int
	token     uint64
	chunkSize int
	rq        *zcrxRefillQ
	ringPtr   unsafe.Pointer // mmap'd refill queue ring
	ringSize  int
}

// unmap releases mapped memory.
func (a *zcrxArea) unmap() {
	if a.ringPtr != nil {
		munmapRing(a.ringPtr, a.ringSize)
		a.ringPtr = nil
		a.ringSize = 0
	}
	if a.ptr != nil {
		munmapArea(a.ptr, a.size)
		a.ptr = nil
		a.size = 0
	}
	a.rq = nil
}

// zcrxBufPool is a package-level pool of ZCRXBuffer wrappers.
var zcrxBufPool sync.Pool

// ZCRXReceiver manages a ZCRX receive lifecycle.
// External Stop and Close must not race CQE dispatch or terminal retirement. Call Close only after Stopped reports true.
type ZCRXReceiver struct {
	ring     *Uring
	pool     *ContextPools
	zcrxID   uint32
	area     zcrxArea
	handler  ZCRXHandler
	fd       iofd.FD
	closed   atomic.Bool
	state    atomic.Uint32
	ext      *ExtSQE
	userData atomic.Uint64 // for async cancel
}

const (
	zcrxStateIdle uint32 = iota
	zcrxStateActive
	zcrxStateStopping
	zcrxStateRetiring
	zcrxStateStopped
)

var zcrxAsyncCancelByUserData = func(ring *Uring, userData uint64) error {
	return ring.asyncCancelByUserData(userData)
}

// NewZCRXReceiver creates a ZCRX receiver.
// Returns ErrNotSupported if ZCRX is unavailable.
func NewZCRXReceiver(ring *Uring, pool *ContextPools, cfg ZCRXConfig) (*ZCRXReceiver, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if ring.params.flags&IORING_SETUP_CQE32 == 0 {
		return nil, ErrNotSupported
	}

	query, err := ring.QueryZCRX()
	if err != nil {
		return nil, ErrNotSupported
	}
	if query.NrCtrlOpcodes == 0 {
		return nil, ErrNotSupported
	}

	iface, err := sock.LinkByName(cfg.IfName)
	if err != nil {
		return nil, err
	}
	ifIndex := uint32(iface.Index)

	areaPtr, err := mmapArea(cfg.AreaSize, cfg.UseHugePages)
	if err != nil {
		return nil, err
	}

	pageSize := int(iobuf.PageSize)
	rqRingSize := cfg.RqEntries*int(unsafe.Sizeof(ZCRXRqe{})) + pageSize
	rqRingSize = (rqRingSize + pageSize - 1) &^ (pageSize - 1)

	rqRingPtr, err := mmapRing(rqRingSize)
	if err != nil {
		munmapArea(areaPtr, cfg.AreaSize)
		return nil, err
	}

	areaReg := ZCRXAreaReg{
		Addr: uint64(uintptr(areaPtr)),
		Len:  uint64(cfg.AreaSize),
	}

	regionDesc := RegionDesc{
		UserAddr: uint64(uintptr(rqRingPtr)),
		Size:     uint64(rqRingSize),
		Flags:    IORING_MEM_REGION_TYPE_USER,
	}

	zcrxID, offsets, err := ring.RegisterZCRXIfq(
		ifIndex,
		uint32(cfg.RxQueue),
		uint32(cfg.RqEntries),
		&areaReg,
		&regionDesc,
		uint32(cfg.ChunkSize),
	)
	if err != nil {
		munmapRing(rqRingPtr, rqRingSize)
		munmapArea(areaPtr, cfg.AreaSize)
		return nil, err
	}

	rq := &zcrxRefillQ{
		kHead:   (*uint32)(unsafe.Add(rqRingPtr, uintptr(offsets.Head))),
		kTail:   (*uint32)(unsafe.Add(rqRingPtr, uintptr(offsets.Tail))),
		rqes:    unsafe.Slice((*ZCRXRqe)(unsafe.Add(rqRingPtr, uintptr(offsets.Rqes))), cfg.RqEntries),
		entries: uint32(cfg.RqEntries),
		mask:    uint32(cfg.RqEntries - 1),
	}
	rq.seed(cfg.AreaSize, cfg.ChunkSize, areaReg.RqAreaToken)
	return &ZCRXReceiver{
		ring:   ring,
		pool:   pool,
		zcrxID: zcrxID,
		area: zcrxArea{
			ptr:       areaPtr,
			size:      cfg.AreaSize,
			token:     areaReg.RqAreaToken,
			chunkSize: cfg.ChunkSize,
			rq:        rq,
			ringPtr:   rqRingPtr,
			ringSize:  rqRingSize,
		},
	}, nil
}

// Start submits the multishot ZCRX receive.
// Call at most once. fd must remain valid until terminal retirement.
func (r *ZCRXReceiver) Start(fd iofd.FD, handler ZCRXHandler) error {
	if !r.state.CompareAndSwap(zcrxStateIdle, zcrxStateActive) {
		return ErrExists
	}

	if handler == nil {
		handler = NoopZCRXHandler{}
	}

	r.fd = fd
	r.handler = handler

	ext := r.pool.Extended()
	if ext == nil {
		r.state.Store(zcrxStateIdle)
		return iox.ErrWouldBlock
	}

	ctx := CastUserData[zcrxCtx](ext)
	ctx.Fn = zcrxCQEHandler
	extAnchors(ext).owner = r
	if err := r.ring.ZCRXFlushRQ(r.zcrxID); err != nil {
		r.state.Store(zcrxStateIdle)
		r.ext = nil
		r.userData.Store(0)
		r.pool.PutExtended(ext)
		return err
	}

	ext.SQE.opcode = IORING_OP_RECV_ZC
	ext.SQE.flags = 0
	ext.SQE.ioprio = uint16(IORING_RECV_MULTISHOT)
	ext.SQE.fd = int32(fd)
	ext.SQE.len = 0
	ext.SQE.spliceFdIn = int32(r.zcrxID)

	// Publish cancel userData before submit to cover immediate CQE completion.
	sqeCtx := PackExtended(ext)
	r.ext = ext
	r.userData.Store(uint64(sqeCtx))

	if err := r.ring.SubmitExtended(sqeCtx); err != nil {
		r.ext = nil
		r.userData.Store(0)
		r.pool.PutExtended(ext)
		r.state.Store(zcrxStateIdle)
		return err
	}

	return nil
}

// zcrxCtx is the 64-byte extended user-data payload for ZCRX CQEs.
type zcrxCtx struct {
	Fn Handler
	_  [56]byte // pad to 64 bytes
}

var _ [64 - unsafe.Sizeof(zcrxCtx{})]byte
var _ [unsafe.Sizeof(zcrxCtx{}) - 64]byte

// zcrxCQEHandler is the static CQE dispatcher for ZCRX multishot receives.
func zcrxCQEHandler(_ *Uring, _ *ioUringSqe, cqe *ioUringCqe) {
	view := CQEView{
		Res:   cqe.res,
		Flags: cqe.flags,
		ctx:   SQEContext(cqe.userData),
	}
	if !view.Extended() {
		return
	}
	ext := view.ExtSQE()
	receiver, _ := extAnchors(ext).owner.(*ZCRXReceiver)
	if receiver == nil {
		return
	}
	receiver.handleCQE(view, cqe)
}

// handleCQE dispatches a single ZCRX CQE.
func (r *ZCRXReceiver) handleCQE(view CQEView, rawCqe *ioUringCqe) {
	if view.Res < 0 {
		continueOnError := true
		if r.handler != nil {
			continueOnError = r.handler.OnError(errFromResult(view.Res))
		}
		if view.Flags&IORING_CQE_F_MORE != 0 {
			if r.handler != nil && !continueOnError {
				_ = r.Stop()
			}
			return
		}
		r.finish(view.ctx.ExtSQE())
		return
	}

	r.deliverData(view, rawCqe)
	if view.Flags&IORING_CQE_F_MORE == 0 {
		r.finish(view.ctx.ExtSQE())
	}
}

// finish retires the receiver, runs OnStopped, returns ext, and then publishes Stopped.
func (r *ZCRXReceiver) finish(ext *ExtSQE) {
	for {
		state := r.state.Load()
		if state != zcrxStateActive && state != zcrxStateStopping {
			return
		}
		if r.state.CompareAndSwap(state, zcrxStateRetiring) {
			break
		}
	}
	r.ext = nil
	r.userData.Store(0)
	if r.handler != nil {
		r.handler.OnStopped()
	}
	if ext != nil {
		r.pool.PutExtended(ext)
	}
	r.state.CompareAndSwap(zcrxStateRetiring, zcrxStateStopped)
}

// Stop requests cancellation of the active multishot receive.
// Leaves the receiver active if cancel submission fails.
func (r *ZCRXReceiver) Stop() error {
	if !r.state.CompareAndSwap(zcrxStateActive, zcrxStateStopping) {
		return nil
	}
	userData := r.userData.Load()
	if userData != 0 {
		if err := zcrxAsyncCancelByUserData(r.ring, userData); err != nil {
			r.state.CompareAndSwap(zcrxStateStopping, zcrxStateActive)
			return err
		}
	}
	return nil
}

// Stopped reports whether terminal retirement is complete.
// Close still requires the owning ring to have been stopped.
func (r *ZCRXReceiver) Stopped() bool {
	state := r.state.Load()
	return state == zcrxStateIdle || state == zcrxStateStopped
}

// Close releases ZCRX resources after the receiver has retired and the owning
// ring has been stopped. Returns iox.ErrWouldBlock while retirement is still
// in flight or while the ring still owns the ZCRX IFQ registration.
// Safe to call more than once; subsequent calls return nil.
func (r *ZCRXReceiver) Close() error {
	if !r.Stopped() {
		return iox.ErrWouldBlock
	}
	if r.ring != nil && !r.ring.closed.Load() {
		return iox.ErrWouldBlock
	}
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}

	r.area.unmap()

	return nil
}

// Active reports whether the receiver is receiving.
func (r *ZCRXReceiver) Active() bool {
	return r.state.Load() == zcrxStateActive
}

// deliverData handles a successful ZCRX CQE.
func (r *ZCRXReceiver) deliverData(view CQEView, rawCqe *ioUringCqe) {
	zcrxCqe := (*ZCRXCqe)(unsafe.Add(unsafe.Pointer(rawCqe), 16))
	dataOffset := zcrxCqe.Off & ((1 << IORING_ZCRX_AREA_SHIFT) - 1)

	dataLen := int(view.Res)
	if dataLen == 0 {
		buf := r.acquireBuffer()
		buf.reset(nil, 0, 0, 0, nil)
		if r.handler != nil {
			if !r.handler.OnData(buf) {
				_ = r.Stop()
			}
		}
		return
	}
	data, err := r.dataSlice(dataOffset, dataLen)
	if err != nil {
		r.deliverError(err)
		return
	}
	buf := r.acquireBuffer()
	buf.reset(data, r.area.chunkSize, zcrxCqe.Off, r.area.token, r.area.rq)
	if r.handler != nil {
		if !r.handler.OnData(buf) {
			_ = r.Stop()
		}
	}
}

// acquireBuffer returns a pooled ZCRXBuffer, allocating on pool miss.
func (r *ZCRXReceiver) acquireBuffer() *ZCRXBuffer {
	if buf, _ := zcrxBufPool.Get().(*ZCRXBuffer); buf != nil {
		return buf
	}
	return &ZCRXBuffer{}
}

// dataSlice returns the payload slice. Caller must ensure dataLen >= 0.
func (r *ZCRXReceiver) dataSlice(offset uint64, dataLen int) ([]byte, error) {
	if r.area.ptr == nil || r.area.size <= 0 {
		return nil, fmt.Errorf("uring: invalid zcrx area")
	}
	endOffset := offset + uint64(dataLen)
	if endOffset < offset || endOffset > uint64(r.area.size) {
		return nil, fmt.Errorf("uring: invalid zcrx cqe bounds off=%d len=%d area=%d", offset, dataLen, r.area.size)
	}
	return unsafe.Slice((*byte)(unsafe.Add(r.area.ptr, offset)), dataLen), nil
}

// deliverError routes a data-path error to OnError.
func (r *ZCRXReceiver) deliverError(err error) {
	if r.handler == nil {
		_ = r.Stop()
		return
	}
	if !r.handler.OnError(err) {
		_ = r.Stop()
	}
}

// errFromResult converts a negative CQE result to an error.
func errFromResult(res int32) error {
	if res >= 0 {
		return nil
	}
	return errFromErrno(uintptr(-res))
}

// mmapArea allocates the shared receive area via mmap.
func mmapArea(size int, hugePages bool) (unsafe.Pointer, error) {
	const mapHugeTLB = 0x040000
	flags := uintptr(zcall.MAP_SHARED | zcall.MAP_ANONYMOUS)
	if hugePages {
		flags |= mapHugeTLB
	}
	prot := uintptr(zcall.PROT_READ | zcall.PROT_WRITE)

	ptr, errno := zcall.Mmap(nil, uintptr(size), prot, flags, ^uintptr(0), 0)
	if errno != 0 {
		return nil, errFromErrno(errno)
	}
	return ptr, nil
}

// munmapArea releases the receive area.
func munmapArea(ptr unsafe.Pointer, size int) {
	if ptr != nil {
		zcall.Munmap(ptr, uintptr(size))
	}
}

// mmapRing allocates shared refill-ring memory via mmap.
func mmapRing(size int) (unsafe.Pointer, error) {
	flags := uintptr(zcall.MAP_SHARED | zcall.MAP_ANONYMOUS)
	prot := uintptr(zcall.PROT_READ | zcall.PROT_WRITE)

	ptr, errno := zcall.Mmap(nil, uintptr(size), prot, flags, ^uintptr(0), 0)
	if errno != 0 {
		return nil, errFromErrno(errno)
	}
	return ptr, nil
}

// munmapRing releases the refill ring.
func munmapRing(ptr unsafe.Pointer, size int) {
	if ptr != nil {
		zcall.Munmap(ptr, uintptr(size))
	}
}

// NoopZCRXHandler provides default ZCRXHandler behavior.
// Embed to override selectively.
type NoopZCRXHandler struct{}

// OnData releases the buffer and continues.
func (NoopZCRXHandler) OnData(buf *ZCRXBuffer) bool {
	return buf.Release() == nil
}

// OnError stops on any error.
func (NoopZCRXHandler) OnError(error) bool { return false }

// OnStopped is a no-op.
func (NoopZCRXHandler) OnStopped() {}

var _ ZCRXHandler = NoopZCRXHandler{}
