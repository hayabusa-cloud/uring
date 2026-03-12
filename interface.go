// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
	"encoding/binary"
	"errors"
	"sync/atomic"
	"time"
	"unsafe"
)

// Uring entry count constants define the number of SQE slots in the submission queue.
// 7 tiers with power-of-4 progression: 8, 32, 128, 512, 2048, 8192, 32768.
const (
	UringEntriesPico   = 1 << 3  // 8 entries
	UringEntriesNano   = 1 << 5  // 32 entries
	UringEntriesMicro  = 1 << 7  // 128 entries
	UringEntriesSmall  = 1 << 9  // 512 entries
	UringEntriesMedium = 1 << 11 // 2048 entries
	UringEntriesLarge  = 1 << 13 // 8192 entries
	UringEntriesHuge   = 1 << 15 // 32768 entries
)

// UringOptions configures the io_uring instance behavior.
// All fields have sensible defaults if not specified.
type UringOptions struct {
	// Entries specifies the number of SQE slots (use UringEntries* constants).
	Entries int
	// LockedBufferMem is the total memory for registered buffers (bytes).
	LockedBufferMem int
	// ReadBufferSize is the size of each read buffer (bytes).
	ReadBufferSize int
	// ReadBufferNum is the number of read buffers to allocate.
	ReadBufferNum int
	// ReadBufferGidOffset is the base group ID for read buffers.
	ReadBufferGidOffset int
	// WriteBufferSize is the size of each write buffer (bytes).
	WriteBufferSize int
	// WriteBufferNum is the number of write buffers to allocate.
	WriteBufferNum int
	// MultiSizeBuffer enables multiple buffer size groups when > 0.
	MultiSizeBuffer int
	// MultiIssuers enables COOP_TASKRUN mode for concurrent submission.
	MultiIssuers bool
	// NotifySucceed ensures CQEs are generated for all successful operations.
	NotifySucceed bool
	// IndirectSubmissionQueue enables the SQ array (for legacy compatibility).
	IndirectSubmissionQueue bool
	// HybridPolling enables hybrid I/O polling mode (IORING_SETUP_HYBRID_IOPOLL).
	// This delays polling to reduce CPU usage while maintaining low latency.
	// Requires: O_DIRECT files on polling-capable storage devices (e.g., NVMe).
	// Available since kernel 6.13.
	HybridPolling bool
}

// NewUring creates a new io_uring instance with the specified options.
// Returns an unstarted ring; call Start() to initialize buffers and enable.
func NewUring(options ...func(options *UringOptions)) (*Uring, error) {
	opt := defaultUringOptions
	for _, option := range options {
		option(&opt)
	}
	setupOpts := []func(params *ioUringParams){ioUringDisabledOptions}
	if opt.MultiIssuers {
		setupOpts = append(setupOpts, func(params *ioUringParams) {
			params.flags |= IORING_SETUP_COOP_TASKRUN
		})
	} else {
		setupOpts = append(setupOpts, func(params *ioUringParams) {
			params.flags |= IORING_SETUP_SINGLE_ISSUER
			params.flags |= IORING_SETUP_DEFER_TASKRUN
		})
	}
	if !opt.IndirectSubmissionQueue {
		setupOpts = append(setupOpts, ioUringNoSQArrayOptions)
	}
	if opt.HybridPolling {
		setupOpts = append(setupOpts, ioUringHybridIoPollOptions)
	}
	r, err := newIoUring(opt.Entries, setupOpts...)
	if err != nil {
		return nil, err
	}
	rFlags, wFlags := uint8(0), uint8(0)
	if r.feature(IORING_FEAT_CQE_SKIP) && !opt.NotifySucceed {
		wFlags |= IOSQE_CQE_SKIP_SUCCESS
	}
	ret := Uring{
		ioUring:          r,
		UringOptions:     &opt,
		bufferRings:      newUringBufferRings(),
		ctxPools:         NewContextPools(opt.Entries),
		readLikeOpFlags:  rFlags,
		writeLikeOpFlags: wFlags,
	}

	if opt.MultiSizeBuffer > 0 {
		ret.bufferGroups = newUringBufferGroups(opt.MultiSizeBuffer)
		ret.bufferGroups.setGIDOffset(opt.ReadBufferGidOffset)
	} else {
		ret.buffers = newUringProvideBuffers(opt.ReadBufferSize, opt.ReadBufferNum)
		ret.buffers.setGIDOffset(opt.ReadBufferGidOffset)
	}
	feat := UringFeatures{
		SQEntries:         int(r.params.sqEntries),
		CQEntries:         int(r.params.cqEntries),
		UserdataByteOrder: binary.LittleEndian,
	}
	if isBigEndian {
		feat.UserdataByteOrder = binary.BigEndian
	}
	ret.Features = &feat

	return &ret, nil
}

// UringFeatures reports the capabilities of the io_uring instance.
// Fields are populated during NewUring() and Start().
type UringFeatures struct {
	// SQEntries is the actual number of SQ entries allocated by the kernel.
	SQEntries int
	// CQEntries is the actual number of CQ entries allocated by the kernel.
	CQEntries int
	// UserdataByteOrder is the byte order for userdata field interpretation.
	UserdataByteOrder binary.ByteOrder

	// Kernel feature flags (populated during Start())
	HasRecvSendBundle bool // IORING_FEAT_RECVSEND_BUNDLE (6.13+)
	HasMinTimeout     bool // IORING_FEAT_MIN_TIMEOUT (6.13+)
	HasRWAttr         bool // IORING_FEAT_RW_ATTR (6.14+)
	HasNoIOWait       bool // IORING_FEAT_NO_IOWAIT (6.14+)

	// Operation support flags (6.13+)
	HasRecvZC       bool // IORING_OP_RECV_ZC
	HasEpollWait    bool // IORING_OP_EPOLL_WAIT
	HasReadvFixed   bool // IORING_OP_READV_FIXED
	HasWritevFixed  bool // IORING_OP_WRITEV_FIXED
	HasPipe         bool // IORING_OP_PIPE (6.14+)
	HasMixedSQEMode bool // IORING_SETUP_SQE_MIXED (6.13+)
	HasMixedCQEMode bool // IORING_SETUP_CQE_MIXED (6.13+)
}

// Uring is the main io_uring interface for submitting and completing I/O operations.
// It wraps the kernel io_uring instance with buffer management and typed operations.
type Uring struct {
	*ioUring
	*UringOptions
	// Features reports kernel capabilities (populated during Start).
	Features *UringFeatures

	buffers      *uringProvideBuffers
	bufferGroups *uringProvideBufferGroups
	bufferRings  *uringBufferRings

	buffersPool *RegisterBufferPool

	// ctxPools provides lock-free pools for IndirectSQE and ExtSQE contexts.
	// Capacity matches SQ entries for natural backpressure.
	ctxPools *ContextPools

	// bufRingBackings stores backing memory for buffer rings to prevent GC.
	bufRingBackings [][]byte

	readLikeOpFlags  uint8
	writeLikeOpFlags uint8
}

// Start initializes the io_uring instance with probes, buffers, and enables the ring.
func (ur *Uring) Start() error {
	// Initialize context pools
	ur.ctxPools.Init()

	// register probes
	probe := ioUringProbe{}
	err := ur.registerProbe(&probe)
	if err != nil {
		return err
	}

	// Detect kernel features from params.features
	ur.Features.HasRecvSendBundle = ur.feature(IORING_FEAT_RECVSEND_BUNDLE)
	ur.Features.HasMinTimeout = ur.feature(IORING_FEAT_MIN_TIMEOUT)
	ur.Features.HasRWAttr = ur.feature(IORING_FEAT_RW_ATTR)
	ur.Features.HasNoIOWait = ur.feature(IORING_FEAT_NO_IOWAIT)

	// Check operation support from probes (6.13+ ops)
	for _, op := range ur.ops {
		switch op.op {
		case IORING_OP_RECV_ZC:
			ur.Features.HasRecvZC = true
		case IORING_OP_EPOLL_WAIT:
			ur.Features.HasEpollWait = true
		case IORING_OP_READV_FIXED:
			ur.Features.HasReadvFixed = true
		case IORING_OP_WRITEV_FIXED:
			ur.Features.HasWritevFixed = true
		case IORING_OP_PIPE:
			ur.Features.HasPipe = true
		}
	}

	// Check mixed SQE/CQE mode support (from setup flags echo)
	ur.Features.HasMixedSQEMode = ur.params.flags&IORING_SETUP_SQE_MIXED != 0 || probe.lastOp >= IORING_OP_NOP128
	ur.Features.HasMixedCQEMode = ur.params.flags&IORING_SETUP_CQE_MIXED != 0 || probe.lastOp >= IORING_OP_NOP128

	// register buffers
	if ur.LockedBufferMem > (1 << 16) {
		maximizeMemoryLock()
	}
	regBufNum := min(registerBufferNum, ur.LockedBufferMem/registerBufferSize)
	ur.buffersPool = NewRegisterBufferPool(regBufNum)
	ur.buffersPool.Fill(func() RegisterBuffer { return RegisterBuffer{} })
	regBufAddr := unsafe.Pointer(unsafe.SliceData(ur.buffersPool.items))
	err = ur.registerBuffers(regBufAddr, regBufNum, registerBufferSize)
	if err != nil {
		return err
	}

	// provide buffers
	if ur.buffers != nil {
		err = ur.bufferRings.registerBuffers(ur.ioUring, ur.buffers)
		if err != nil {
			return err
		}
	} else if ur.bufferGroups != nil {
		err = ur.bufferRings.registerGroups(ur.ioUring, ur.bufferGroups)
		if err != nil {
			return err
		}
	}
	ur.bufferRings.advance(ur.ioUring)

	// enable ring
	err = ur.enable()
	if err != nil {
		return err
	}

	return nil
}

// Wait flushes pending submissions and collects completion events into CQEView slice.
// Returns the number of events received, or ErrWouldBlock if the CQ is empty.
//
// CQEView provides direct field access to Res and Flags, and methods to access
// the submission context based on mode (Direct, Indirect, Extended).
//
// Example:
//
//	cqes := make([]CQEView, 64)
//	n, err := ring.Wait(cqes)
//	for i := 0; i < n; i++ {
//	    cqe := &cqes[i]
//	    if cqe.Extended() {
//	        ext := cqe.ExtSQE()
//	        ctx := ViewCtx1[*Conn](ext).Vals1()
//	        ctx.Fn(ring, cqe)
//	    }
//	}
func (ur *Uring) Wait(cqes []CQEView) (n int, err error) {
	err = ur.ioUring.enter()
	if err != nil {
		return 0, err
	}

	// Use batch retrieval: single CAS claims multiple CQEs
	return ur.ioUring.waitBatch(cqes)
}

// ========================================
// Context Pool Accessors
// ========================================

// GetExtSQE acquires an ExtSQE from the pool for Extended mode submissions.
// Returns nil if the pool is exhausted (ring is full - natural backpressure).
// The returned ExtSQE is borrowed until PutExtSQE after the corresponding CQE
// is processed. Callers must not retain pointers into SQE or UserData after
// release.
//
//go:nosplit
func (ur *Uring) GetExtSQE() *ExtSQE {
	return ur.ctxPools.GetExtended()
}

// PutExtSQE returns an ExtSQE to the pool after completion processing.
// Must be called exactly once per GetExtSQE to maintain pool balance.
// After this call the ExtSQE, typed context views, and raw CastUserData
// overlays derived from it are invalid.
//
//go:nosplit
func (ur *Uring) PutExtSQE(sqe *ExtSQE) {
	ur.ctxPools.PutExtended(sqe)
}

// GetIndirectSQE acquires an IndirectSQE from the pool for Indirect mode submissions.
// Returns nil if the pool is exhausted.
// The returned IndirectSQE is borrowed until PutIndirectSQE.
//
//go:nosplit
func (ur *Uring) GetIndirectSQE() *IndirectSQE {
	return ur.ctxPools.GetIndirect()
}

// PutIndirectSQE returns an IndirectSQE to the pool.
// After this call the IndirectSQE is invalid and must not be reused.
//
//go:nosplit
func (ur *Uring) PutIndirectSQE(sqe *IndirectSQE) {
	ur.ctxPools.PutIndirect(sqe)
}

// submitExtended submits an SQE using Extended mode context.
// The ExtSQE.SQE fields must be populated before calling this method.
// The io_uring.user_data field is set to the SQEContext (pointer + mode bits).
func (ur *Uring) submitExtended(sqeCtx SQEContext) error {
	return ur.ioUring.submitExtended(sqeCtx)
}

// ========================================
// Registered Buffer Access
// ========================================

// RegisteredBuffer returns the registered buffer at the given index.
// Returns nil if the index is out of range.
// The returned slice shares memory with the kernel; writes are visible
// to zero-copy operations using the same buffer index.
//
//go:nosplit
func (ur *Uring) RegisteredBuffer(index int) []byte {
	if index < 0 || index >= len(ur.bufs) {
		return nil
	}
	return ur.bufs[index]
}

// RegisteredBufferCount returns the number of registered buffers.
//
//go:nosplit
func (ur *Uring) RegisteredBufferCount() int {
	return len(ur.bufs)
}

// ========================================
// Ring Introspection (for backpressure)
// ========================================

// SQAvailable returns the number of SQEs available for submission.
// Higher layers can use this for admission control and backpressure.
//
//go:nosplit
func (ur *Uring) SQAvailable() int {
	entries := int(*ur.sq.kRingEntries)
	pending := ur.sqCount()
	return entries - pending
}

// CQPending returns the number of CQEs waiting to be reaped.
// Higher layers can use this to decide when to drain completions.
//
//go:nosplit
func (ur *Uring) CQPending() int {
	h := atomic.LoadUint32(ur.cq.kHead)
	t := atomic.LoadUint32(ur.cq.kTail)
	return int(t - h)
}

// RingFD returns the io_uring file descriptor.
// Required for cross-ring operations via IORING_OP_MSG_RING.
//
//go:nosplit
func (ur *Uring) RingFD() int {
	return ur.ringFd
}

// ========================================
// Socket Operations
// ========================================

// SocketRaw creates a socket using io_uring.
// The fd field in sqeCtx is ignored (will be set to domain by the kernel).
func (ur *Uring) SocketRaw(sqeCtx SQEContext, domain, typ, proto int, options ...OpOptionFunc) error {
	flags, _, fileIndex := ur.socketOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.socket(ctx, domain, typ, proto, fileIndex)
}

// TCP4Socket creates a TCP IPv4 socket.
func (ur *Uring) TCP4Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, options...)
}

// TCP6Socket creates a TCP IPv6 socket.
func (ur *Uring) TCP6Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET6, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, options...)
}

// UDP4Socket creates a UDP IPv4 socket.
func (ur *Uring) UDP4Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET, SOCK_DGRAM|SOCK_CLOEXEC, IPPROTO_UDP, options...)
}

// UDP6Socket creates a UDP IPv6 socket.
func (ur *Uring) UDP6Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET6, SOCK_DGRAM|SOCK_CLOEXEC, IPPROTO_UDP, options...)
}

// UDPLITE4Socket creates a UDP-Lite IPv4 socket.
func (ur *Uring) UDPLITE4Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET, SOCK_DGRAM|SOCK_CLOEXEC, IPPROTO_UDPLITE, options...)
}

// UDPLITE6Socket creates a UDP-Lite IPv6 socket.
func (ur *Uring) UDPLITE6Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET6, SOCK_DGRAM|SOCK_CLOEXEC, IPPROTO_UDPLITE, options...)
}

// SCTP4Socket creates an SCTP IPv4 socket.
func (ur *Uring) SCTP4Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET, SOCK_SEQPACKET|SOCK_CLOEXEC, IPPROTO_SCTP, options...)
}

// SCTP6Socket creates an SCTP IPv6 socket.
func (ur *Uring) SCTP6Socket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_INET6, SOCK_SEQPACKET|SOCK_CLOEXEC, IPPROTO_SCTP, options...)
}

// UnixSocket creates a Unix domain socket.
func (ur *Uring) UnixSocket(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketRaw(sqeCtx, AF_LOCAL, SOCK_SEQPACKET|SOCK_CLOEXEC, 0, options...)
}

// SocketDirect creates a socket directly into a registered file table slot.
// The fileIndex specifies which slot to use (0-based), or use IORING_FILE_INDEX_ALLOC
// for auto-allocation (the allocated index is returned in CQE res).
// Requires registered files via RegisterFiles or RegisterFilesSparse.
func (ur *Uring) SocketDirect(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32, options ...OpOptionFunc) error {
	flags, _, _ := ur.socketOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.socketDirect(ctx, domain, typ, proto, fileIndex)
}

// TCP4SocketDirect creates a TCP IPv4 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) TCP4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET, SOCK_STREAM|SOCK_NONBLOCK, IPPROTO_TCP, IORING_FILE_INDEX_ALLOC, options...)
}

// TCP6SocketDirect creates a TCP IPv6 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) TCP6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET6, SOCK_STREAM|SOCK_NONBLOCK, IPPROTO_TCP, IORING_FILE_INDEX_ALLOC, options...)
}

// UDP4SocketDirect creates a UDP IPv4 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) UDP4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET, SOCK_DGRAM|SOCK_NONBLOCK, IPPROTO_UDP, IORING_FILE_INDEX_ALLOC, options...)
}

// UDP6SocketDirect creates a UDP IPv6 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) UDP6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET6, SOCK_DGRAM|SOCK_NONBLOCK, IPPROTO_UDP, IORING_FILE_INDEX_ALLOC, options...)
}

// UDPLITE4SocketDirect creates a UDP-Lite IPv4 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) UDPLITE4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET, SOCK_DGRAM|SOCK_NONBLOCK, IPPROTO_UDPLITE, IORING_FILE_INDEX_ALLOC, options...)
}

// UDPLITE6SocketDirect creates a UDP-Lite IPv6 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) UDPLITE6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET6, SOCK_DGRAM|SOCK_NONBLOCK, IPPROTO_UDPLITE, IORING_FILE_INDEX_ALLOC, options...)
}

// SCTP4SocketDirect creates an SCTP IPv4 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) SCTP4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET, SOCK_SEQPACKET|SOCK_NONBLOCK, IPPROTO_SCTP, IORING_FILE_INDEX_ALLOC, options...)
}

// SCTP6SocketDirect creates an SCTP IPv6 socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) SCTP6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_INET6, SOCK_SEQPACKET|SOCK_NONBLOCK, IPPROTO_SCTP, IORING_FILE_INDEX_ALLOC, options...)
}

// UnixSocketDirect creates a Unix domain socket directly into a registered file table slot.
// Uses IORING_FILE_INDEX_ALLOC for auto-allocation; returns slot index in CQE res.
func (ur *Uring) UnixSocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ur.SocketDirect(sqeCtx, AF_LOCAL, SOCK_SEQPACKET|SOCK_NONBLOCK, 0, IORING_FILE_INDEX_ALLOC, options...)
}

// Bind binds a socket to an address.
func (ur *Uring) Bind(sqeCtx SQEContext, addr Addr, options ...OpOptionFunc) error {
	flags, _ := ur.bindOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.bind(ctx, AddrToSockaddr(addr))
}

// Listen starts listening on a socket.
func (ur *Uring) Listen(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, backlog := ur.listenOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.listen(ctx, backlog)
}

// Accept accepts a new connection from a listener socket.
// The fd in sqeCtx should be set to the listener socket.
func (ur *Uring) Accept(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.accept(ctx, ioprio)
}

// AcceptMultiShot performs multi-shot accept operation.
func (ur *Uring) AcceptMultiShot(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.accept(ctx, ioprio|IORING_ACCEPT_MULTISHOT)
}

// AcceptDirect accepts a connection directly into a registered file table slot.
// The fileIndex specifies which slot to use (0-based), or use IORING_FILE_INDEX_ALLOC
// for auto-allocation (the allocated index is returned in CQE res).
// Note: SOCK_CLOEXEC is not supported with direct accept.
// Requires registered files via RegisterFiles or RegisterFilesSparse.
func (ur *Uring) AcceptDirect(sqeCtx SQEContext, fileIndex uint32, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.acceptDirect(ctx, ioprio, fileIndex)
}

// AcceptDirectMultiShot performs multi-shot accept into registered file table slots.
// Each accepted connection uses the next available slot from auto-allocation.
// Requires IORING_FILE_INDEX_ALLOC as fileIndex for auto-allocation.
func (ur *Uring) AcceptDirectMultiShot(sqeCtx SQEContext, fileIndex uint32, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.acceptDirect(ctx, ioprio|IORING_ACCEPT_MULTISHOT, fileIndex)
}

// Connect initiates a socket connection to a remote address.
func (ur *Uring) Connect(sqeCtx SQEContext, remote Addr, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.connect(ctx, AddrToSockaddr(remote))
}

// Receive performs a socket receive operation.
// If b is nil, uses buffer selection from the kernel-provided buffer ring.
func (ur *Uring) Receive(sqeCtx SQEContext, so PollFd, b []byte, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(int32(so.Fd()))
	if b == nil {
		flags, ioprio, bufSize, bufGroup := ur.receiveWithBufferSelectOptions(so, options)
		ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
		return ur.receiveWithBufferSelect(ctx, ioprio, bufSize, bufGroup)
	}
	flags, ioprio, offset, n := ur.receiveOptions(b, options)
	ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.receive(ctx, ioprio, b, uint64(offset), n)
}

// ReceiveMultiShot performs multi-shot receive operation.
func (ur *Uring) ReceiveMultiShot(sqeCtx SQEContext, so PollFd, b []byte, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(int32(so.Fd()))
	if b == nil {
		flags, ioprio, bufSize, bufGroup := ur.receiveWithBufferSelectOptions(so, options)
		ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
		return ur.receiveWithBufferSelect(ctx, ioprio|IORING_RECV_MULTISHOT, bufSize, bufGroup)
	}
	flags, ioprio, offset, n := ur.receiveOptions(b, options)
	ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.receive(ctx, ioprio|IORING_RECV_MULTISHOT, b, uint64(offset), n)
}

// ReceiveBundle performs a bundle receive operation.
// Grabs multiple contiguous buffers from the buffer group in a single operation.
// The CQE result contains bytes received; use BundleBuffers() to get buffer range.
// Requires kernel 6.10+ with IORING_FEAT_RECVSEND_BUNDLE support.
// Always uses buffer selection from the kernel-provided buffer ring.
func (ur *Uring) ReceiveBundle(sqeCtx SQEContext, so PollFd, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(int32(so.Fd()))
	flags, ioprio, bufSize, bufGroup := ur.receiveWithBufferSelectOptions(so, options)
	ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.receiveBundle(ctx, ioprio, bufSize, bufGroup)
}

// ReceiveBundleMultiShot combines multishot with bundle for maximum throughput.
// Continuous reception with automatic buffer replenishment, grabbing multiple
// buffers per completion.
// Requires kernel 6.10+ with IORING_FEAT_RECVSEND_BUNDLE support.
func (ur *Uring) ReceiveBundleMultiShot(sqeCtx SQEContext, so PollFd, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(int32(so.Fd()))
	flags, ioprio, bufSize, bufGroup := ur.receiveWithBufferSelectOptions(so, options)
	ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.receiveBundle(ctx, ioprio|IORING_RECV_MULTISHOT, bufSize, bufGroup)
}

// Send writes data to a socket.
func (ur *Uring) Send(sqeCtx SQEContext, so PollFd, p []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.sendOptions(p, options)
	ctx := sqeCtx.WithFD(int32(so.Fd())).WithFlags(flags | ur.writeLikeOpFlags)
	return ur.send(ctx, ioprio, p, uint64(offset), n)
}

// SendTargets represents a set of target sockets for multicast/broadcast.
type SendTargets interface {
	// Count returns the number of targets.
	Count() int
	// FD returns the file descriptor at index i.
	FD(i int) int
}

// Multicast sends data to multiple sockets, selecting copy vs zero-copy per message size.
//
// Strategy selection (conservative thresholds, based on Linux 6.18 measurements):
// io_uring cycle ~523ns, ZC needs 2 cycles (~1046ns overhead).
// Uses zero-copy only when memcpy savings clearly exceed overhead:
//   - N < 8:    >= 8 KiB uses zero-copy (high bar, overhead not amortized)
//   - N < 64:   >= 4 KiB uses zero-copy
//   - N < 512:  >= 3 KiB uses zero-copy
//   - N < 4096: >= 2 KiB uses zero-copy
//   - N >= 4096: >= 1.5 KiB uses zero-copy (fully amortized)
//
// For aggressive zero-copy usage, use MulticastZeroCopy instead.
//
// Zero-copy notes:
//   - Produces two CQEs per send: completion (IORING_CQE_F_MORE) + notification
//   - Buffer must not be modified until notification CQE is received
//   - Requires TCP sockets; returns EOPNOTSUPP on Unix sockets or loopback
//
// Parameters:
//   - sqeCtx: base context (FD will be overwritten per target)
//   - targets: collection of target sockets
//   - bufIndex: registered buffer index (use -1 for non-registered buffer)
//   - p: payload data (used when bufIndex < 0)
//   - offset: offset within buffer
//   - n: number of bytes to send
func (ur *Uring) Multicast(sqeCtx SQEContext, targets SendTargets, bufIndex int, p []byte, offset int64, n int, options ...OpOptionFunc) error {
	count := targets.Count()
	if count == 0 {
		return nil
	}

	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)

	// Strategy: use zero-copy with registered buffers based on payload size and
	// destination count. More destinations amortize ZC overhead (pinning, two-CQE).
	//
	// Conservative thresholds (based on Linux 6.18 measurements):
	// io_uring cycle ~523ns, ZC needs 2 cycles (~1046ns overhead).
	// Use ZC only when memcpy savings clearly exceed overhead.
	//
	//   N < 8:    8 KiB  (high bar - ZC overhead not amortized)
	//   N < 64:   4 KiB
	//   N < 512:  3 KiB
	//   N < 4096: 2 KiB
	//   N >= 4096: 1.5 KiB (fully amortized)
	var threshold int
	switch {
	case count >= 4096:
		threshold = 1536
	case count >= 512:
		threshold = 2048
	case count >= 64:
		threshold = 3072
	case count >= 8:
		threshold = 4096
	default:
		threshold = 8192
	}

	useZeroCopy := bufIndex >= 0 && n >= threshold

	var err error
	for i := range count {
		fd := targets.FD(i)
		targetCtx := ctx.WithFD(int32(fd))

		var sendErr error
		if useZeroCopy {
			sendErr = ur.sendZeroCopyFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		} else if bufIndex >= 0 {
			// Use registered buffer without zero-copy
			sendErr = ur.writeFixed(targetCtx, bufIndex, uint64(offset), n)
		} else {
			// Use regular send with user buffer
			sendErr = ur.send(targetCtx, 0, p, uint64(offset), n)
		}
		err = errors.Join(err, sendErr)
	}
	return err
}

// MulticastZeroCopy sends data to multiple sockets using zero-copy with registered buffers.
// This method uses very aggressive thresholds - user explicitly requested zero-copy.
//
// Very aggressive thresholds (use ZC whenever there's any reasonable chance of benefit):
//   - N < 4:    >= 1.5 KiB uses zero-copy (minimal bar)
//   - N < 16:   >= 1 KiB uses zero-copy
//   - N < 64:   >= 512 B uses zero-copy
//   - N < 256:  >= 128 B uses zero-copy
//   - N >= 256: any size uses zero-copy (fully amortized)
//
// For conservative zero-copy usage, use Multicast instead.
//
// Prerequisites:
//   - Buffer must be registered via IORING_REGISTER_BUFFERS2
//   - bufIndex must be a valid registered buffer index
//
// Use this for:
//   - Live streaming (same video/audio frame to thousands of viewers)
//   - Real-time gaming (same game state to many players)
//   - Any scenario with O(1) payload and O(N) targets
//
// Zero-copy notes:
//   - Produces two CQEs per send: completion (IORING_CQE_F_MORE) + notification
//   - Buffer must not be modified until notification CQE is received
//   - May return EOPNOTSUPP on Unix sockets or loopback
func (ur *Uring) MulticastZeroCopy(sqeCtx SQEContext, targets SendTargets, bufIndex int, offset int64, n int, options ...OpOptionFunc) error {
	count := targets.Count()
	if count == 0 {
		return nil
	}
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return ErrInvalidParam
	}

	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)

	// Very aggressive thresholds - user explicitly requested zero-copy.
	// Use ZC whenever there's any reasonable chance of benefit.
	//
	//   N < 4:    1.5 KiB (minimal bar)
	//   N < 16:   1 KiB
	//   N < 64:   512 B
	//   N < 256:  128 B
	//   N >= 256: any size (fully amortized)
	var threshold int
	switch {
	case count >= 256:
		threshold = 0 // any size
	case count >= 64:
		threshold = 128
	case count >= 16:
		threshold = 512
	case count >= 4:
		threshold = 1024
	default:
		threshold = 1536
	}

	// Fall back to writeFixed if below threshold
	useZeroCopy := n >= threshold

	var err error
	for i := range count {
		fd := targets.FD(i)
		targetCtx := ctx.WithFD(int32(fd))
		var sendErr error
		if useZeroCopy {
			sendErr = ur.sendZeroCopyFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		} else {
			sendErr = ur.writeFixed(targetCtx, bufIndex, uint64(offset), n)
		}
		err = errors.Join(err, sendErr)
	}
	return err
}

// Timeout submits a timeout request with the specified duration.
func (ur *Uring) Timeout(sqeCtx SQEContext, d time.Duration, options ...OpOptionFunc) error {
	flags, cnt := ur.timeoutOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	nano := d.Nanoseconds()
	return ur.timeout(ctx, cnt, &Timespec{Sec: nano / int64(time.Second), Nsec: nano % int64(time.Second)}, 0)
}

// Shutdown gracefully closes a socket.
func (ur *Uring) Shutdown(sqeCtx SQEContext, how int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.shutdown(ctx, how)
}

// Nop submits a no-op request.
func (ur *Uring) Nop(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	return ur.nop(sqeCtx.WithFlags(flags))
}

// Close closes a file descriptor.
func (ur *Uring) Close(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	return ur.close(sqeCtx.WithFlags(flags))
}

// Read performs a read operation.
func (ur *Uring) Read(sqeCtx SQEContext, b []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.readOptions(b, options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.read(ctx, ioprio, b, uint64(offset), n)
}

// Write performs a write operation.
func (ur *Uring) Write(sqeCtx SQEContext, b []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.writeOptions(b, options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.write(ctx, ioprio, b, uint64(offset), n)
}

// Splice transfers data between file descriptors.
func (ur *Uring) Splice(sqeCtx SQEContext, fdIn int, n int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.splice(ctx, fdIn, nil, nil, n, 0)
}

// Tee duplicates data between pipes.
func (ur *Uring) Tee(sqeCtx SQEContext, fdIn int, length int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.tee(ctx, fdIn, length, 0)
}

// Sync performs a file sync operation.
func (ur *Uring) Sync(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	return ur.fsync(sqeCtx.WithFlags(flags))
}

// ReadV performs a vectored read operation.
func (ur *Uring) ReadV(sqeCtx SQEContext, iovs [][]byte, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.readv(ctx, iovs)
}

// WriteV performs a vectored write operation.
func (ur *Uring) WriteV(sqeCtx SQEContext, iovs [][]byte, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.writev(ctx, iovs)
}

// ReadFixed performs a read with a registered (fixed) buffer.
func (ur *Uring) ReadFixed(sqeCtx SQEContext, bufIndex int, options ...OpOptionFunc) ([]byte, error) {
	flags, offset, n := ur.readFixedOptions(bufIndex, options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.readFixed(ctx, bufIndex, uint64(offset), n)
}

// WriteFixed performs a write with a registered (fixed) buffer.
func (ur *Uring) WriteFixed(sqeCtx SQEContext, bufIndex int, n int, options ...OpOptionFunc) error {
	flags, offset := ur.writeFixedOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.writeFixed(ctx, bufIndex, uint64(offset), n)
}

// OpenAt opens a file at the given path relative to a directory fd.
func (ur *Uring) OpenAt(sqeCtx SQEContext, pathname string, openFlags int, mode uint32, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.openAt(ctx, pathname, openFlags, mode)
}

// FileAdvise provides advice about file access patterns.
func (ur *Uring) FileAdvise(sqeCtx SQEContext, offset int64, length int, advice int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.fadvise(ctx, uint64(offset), length, advice)
}

// SendMsg sends a message with control data.
func (ur *Uring) SendMsg(sqeCtx SQEContext, so PollFd, buffers [][]byte, oob []byte, to Addr, options ...OpOptionFunc) error {
	flags, ioprio := ur.sendmsgOptions(buffers, options)
	ctx := sqeCtx.WithFD(int32(so.Fd())).WithFlags(flags | ur.writeLikeOpFlags)
	var sa Sockaddr
	if to != nil {
		sa = AddrToSockaddr(to)
	}
	return ur.sendmsg(ctx, ioprio, buffers, oob, sa)
}

// RecvMsg receives a message with control data.
func (ur *Uring) RecvMsg(sqeCtx SQEContext, so PollFd, buffers [][]byte, oob []byte, options ...OpOptionFunc) error {
	flags, ioprio := ur.recvmsgOptions(buffers, options)
	ctx := sqeCtx.WithFD(int32(so.Fd())).WithFlags(flags | ur.readLikeOpFlags)
	return ur.recvmsg(ctx, ioprio, buffers, oob)
}

// PollAdd adds a file descriptor to the poll set.
func (ur *Uring) PollAdd(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.pollAdd(ctx, 0, events)
}

// PollRemove removes a file descriptor from the poll set.
func (ur *Uring) PollRemove(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	return ur.pollRemove(sqeCtx.WithFlags(flags))
}

// PollAddMultishot adds a persistent poll request that generates multiple CQEs.
// Unlike PollAdd which requires re-submission after each event, multishot poll
// automatically re-arms and continues generating CQEs until cancelled.
// Each CQE has IORING_CQE_F_MORE set while poll continues; the final CQE
// has !IORING_CQE_F_MORE when poll terminates or is cancelled.
func (ur *Uring) PollAddMultishot(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.pollAdd(ctx, IORING_POLL_ADD_MULTI, events)
}

// PollAddLevel adds a level-triggered poll request.
// Unlike edge-triggered poll which fires once when state changes,
// level-triggered poll fires continuously while the condition is true.
func (ur *Uring) PollAddLevel(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.pollAdd(ctx, IORING_POLL_ADD_LEVEL, events)
}

// PollAddMultishotLevel combines multishot and level-triggered modes.
// This creates a persistent, level-triggered poll subscription.
func (ur *Uring) PollAddMultishotLevel(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.pollAdd(ctx, IORING_POLL_ADD_MULTI|IORING_POLL_ADD_LEVEL, events)
}

// PollUpdate modifies an existing poll request in-place without cancellation.
// This atomically updates the poll events and/or userData of an active poll.
//
// Parameters:
//   - sqeCtx: Context for this update operation
//   - oldUserData: userData of the target poll request to update
//   - newUserData: New userData (used if updateFlags includes IORING_POLL_UPDATE_USER_DATA)
//   - newEvents: New poll events (used if updateFlags includes IORING_POLL_UPDATE_EVENTS)
//   - updateFlags: Combination of:
//     IORING_POLL_UPDATE_EVENTS - update the poll event mask
//     IORING_POLL_UPDATE_USER_DATA - update the userData
//     IORING_POLL_ADD_MULTI - make the updated poll multishot
//
// The poll is located by matching oldUserData. If no matching poll is found,
// the operation returns -ENOENT. If updateFlags is 0 (or only ADD_MULTI without
// UPDATE_EVENTS or UPDATE_USER_DATA), the operation behaves as PollRemove.
func (ur *Uring) PollUpdate(sqeCtx SQEContext, oldUserData, newUserData uint64, newEvents, updateFlags int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.pollUpdate(ctx, oldUserData, newUserData, newEvents, updateFlags)
}

// TimeoutRemove removes a timeout request.
func (ur *Uring) TimeoutRemove(sqeCtx SQEContext, userData uint64, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.timeoutRemove(ctx, userData, 0)
}

// TimeoutUpdate modifies an existing timeout request in-place.
// This atomically updates the timeout's expiration without removing and re-adding.
//
// Parameters:
//   - sqeCtx: Context for this update operation
//   - userData: userData of the target timeout to update
//   - d: New timeout duration
//   - absolute: If true, d is treated as absolute time; if false, relative from now
func (ur *Uring) TimeoutUpdate(sqeCtx SQEContext, userData uint64, d time.Duration, absolute bool, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	nano := d.Nanoseconds()
	ts := &Timespec{Sec: nano / int64(time.Second), Nsec: nano % int64(time.Second)}
	uflags := 0
	if absolute {
		uflags = IORING_TIMEOUT_ABS
	}
	return ur.timeoutUpdate(ctx, userData, ts, uflags)
}

// AsyncCancel cancels a pending async operation.
func (ur *Uring) AsyncCancel(sqeCtx SQEContext, targetUserData uint64, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.asyncCancel(ctx, targetUserData)
}

// AsyncCancelFD cancels operations on a specific file descriptor.
// If cancelAll is true, cancels all matching operations and returns count.
// Otherwise cancels the first matching operation.
// The FD to cancel is taken from sqeCtx.FD().
func (ur *Uring) AsyncCancelFD(sqeCtx SQEContext, cancelAll bool, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	cancelFlags := IORING_ASYNC_CANCEL_FD
	if cancelAll {
		cancelFlags |= IORING_ASYNC_CANCEL_ALL
	}
	return ur.asyncCancelExt(ctx, 0, 0, cancelFlags)
}

// AsyncCancelOpcode cancels operations of a specific opcode type.
// If cancelAll is true, cancels all matching operations and returns count.
// Otherwise cancels the first matching operation.
func (ur *Uring) AsyncCancelOpcode(sqeCtx SQEContext, opcode uint8, cancelAll bool, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	cancelFlags := IORING_ASYNC_CANCEL_OP
	if cancelAll {
		cancelFlags |= IORING_ASYNC_CANCEL_ALL
	}
	return ur.asyncCancelExt(ctx, 0, opcode, cancelFlags)
}

// AsyncCancelAny cancels any one pending operation.
// Returns 0 on success, -ENOENT if no operations pending.
func (ur *Uring) AsyncCancelAny(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.asyncCancelExt(ctx, 0, 0, IORING_ASYNC_CANCEL_ANY)
}

// AsyncCancelAll cancels all pending operations.
// Returns the count of cancelled operations.
func (ur *Uring) AsyncCancelAll(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.asyncCancelExt(ctx, 0, 0, IORING_ASYNC_CANCEL_ALL|IORING_ASYNC_CANCEL_ANY)
}

// Fallocate allocates space for a file.
func (ur *Uring) Fallocate(sqeCtx SQEContext, mode uint32, offset int64, length int64, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.fAllocate(ctx, mode, uint64(offset), length)
}

// SyncFileRange syncs a file range to storage.
func (ur *Uring) SyncFileRange(sqeCtx SQEContext, offset int64, length int, syncFlags int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.syncFileRange(ctx, uint64(offset), length, syncFlags)
}

// LinkTimeout creates a linked timeout operation.
func (ur *Uring) LinkTimeout(sqeCtx SQEContext, d time.Duration, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	nano := d.Nanoseconds()
	return ur.linkTimeout(ctx, &Timespec{Sec: nano / int64(time.Second), Nsec: nano % int64(time.Second)}, 0)
}

// ========================================
// Kernel 6.13+ Operations
// ========================================

// ReceiveZeroCopy performs a zero-copy receive operation.
// Requires kernel 6.13+ with ZCRX support and a registered ZCRX interface queue.
// zcrxIfqIdx is the ZCRX interface queue index from RegisterZCRXIfq.
func (ur *Uring) ReceiveZeroCopy(sqeCtx SQEContext, so PollFd, n int, zcrxIfqIdx uint32, options ...OpOptionFunc) error {
	if !ur.Features.HasRecvZC {
		return ErrNotSupported
	}
	flags, ioprio, _, _ := ur.receiveOptions(nil, options)
	ctx := sqeCtx.WithFD(int32(so.Fd())).WithFlags(flags | ur.readLikeOpFlags)
	return ur.receiveZeroCopy(ctx, ioprio, n, zcrxIfqIdx)
}

// EpollWait performs an epoll_wait operation via io_uring.
// This integrates epoll monitoring into the io_uring event loop.
// Requires kernel 6.13+.
func (ur *Uring) EpollWait(sqeCtx SQEContext, events []EpollEvent, timeout int32, options ...OpOptionFunc) error {
	if !ur.Features.HasEpollWait {
		return ErrNotSupported
	}
	if len(events) == 0 {
		return ErrInvalidParam
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.epollWait(ctx, &events[0], len(events), timeout)
}

// ReadvFixed performs a vectored read using registered buffers.
// All buffer indices must refer to previously registered buffers.
// Requires kernel 6.13+.
func (ur *Uring) ReadvFixed(sqeCtx SQEContext, offset int64, bufIndices []int, options ...OpOptionFunc) error {
	if !ur.Features.HasReadvFixed {
		return ErrNotSupported
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.readvFixed(ctx, uint64(offset), bufIndices)
}

// WritevFixed performs a vectored write using registered buffers.
// All buffer indices must refer to previously registered buffers.
// Requires kernel 6.13+.
func (ur *Uring) WritevFixed(sqeCtx SQEContext, offset int64, bufIndices []int, lengths []int, options ...OpOptionFunc) error {
	if !ur.Features.HasWritevFixed {
		return ErrNotSupported
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.writevFixed(ctx, uint64(offset), bufIndices, lengths)
}

// Pipe creates a pipe using io_uring.
// The fds parameter must point to an int32[2] array where the kernel
// will write the read end (fds[0]) and write end (fds[1]) file descriptors.
// On successful completion, fds[0] will be the read end and fds[1] the write end.
// Requires kernel 6.14+.
func (ur *Uring) Pipe(sqeCtx SQEContext, fds *[2]int32, pipeFlags uint32, options ...OpOptionFunc) error {
	if !ur.Features.HasPipe {
		return ErrNotSupported
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.pipe(ctx, fds, pipeFlags)
}

// ========================================
// ZCRX (Zero-Copy Receive) Support
// ========================================

// RegisterZCRXIfq registers a zero-copy receive interface queue.
// This sets up ZCRX for a specific network interface RX queue.
// Returns the ZCRX instance ID on success.
// Requires kernel 6.18+ with ZCRX-capable network hardware.
func (ur *Uring) RegisterZCRXIfq(ifIdx, ifRxq uint32, rqEntries uint32, area *ZCRXAreaReg) (uint32, error) {
	reg := ZCRXIfqReg{
		IfIdx:     ifIdx,
		IfRxq:     ifRxq,
		RqEntries: rqEntries,
		AreaPtr:   uint64(uintptr(unsafe.Pointer(area))),
	}
	err := ur.registerZCRXIfq(&reg)
	if err != nil {
		return 0, err
	}
	return reg.ZcrxID, nil
}

// ZCRXFlushRQ flushes the ZCRX refill queue.
// This ensures all pending refill queue entries are processed.
func (ur *Uring) ZCRXFlushRQ(zcrxID uint32) error {
	ctrl := ZCRXCtrl{
		ZcrxID: zcrxID,
		Op:     ZCRX_CTRL_FLUSH_RQ,
	}
	return ur.zcrxCtrl(&ctrl)
}

// ========================================
// Buffer Ring with Flags
// ========================================

// RegisterBufRingMMAP registers a buffer ring with kernel-allocated memory.
// The kernel allocates the ring memory and the application uses mmap to access it.
// Returns the buffer ring for adding buffers.
// Requires kernel 6.13+.
func (ur *Uring) RegisterBufRingMMAP(entries int, groupID uint16) (*ioUringBufRing, error) {
	r, backing, err := ur.registerBufRingWithFlags(entries, groupID, IOU_PBUF_RING_MMAP)
	if backing != nil {
		ur.bufRingBackings = append(ur.bufRingBackings, backing)
	}
	return r, err
}

// RegisterBufRingIncremental registers a buffer ring in incremental consumption mode.
// In this mode, buffers can be partially consumed across multiple completions.
// The CQE will have IORING_CQE_F_BUF_MORE set if more data remains.
// Requires kernel 6.13+.
func (ur *Uring) RegisterBufRingIncremental(entries int, groupID uint16) (*ioUringBufRing, error) {
	r, backing, err := ur.registerBufRingWithFlags(entries, groupID, IOU_PBUF_RING_INC)
	if backing != nil {
		ur.bufRingBackings = append(ur.bufRingBackings, backing)
	}
	return r, err
}

// RegisterBufRingWithFlags registers a buffer ring with specified flags.
// Flags can be combined: IOU_PBUF_RING_MMAP | IOU_PBUF_RING_INC
// Requires kernel 6.13+.
func (ur *Uring) RegisterBufRingWithFlags(entries int, groupID uint16, flags uint16) (*ioUringBufRing, error) {
	r, backing, err := ur.registerBufRingWithFlags(entries, groupID, flags)
	if backing != nil {
		ur.bufRingBackings = append(ur.bufRingBackings, backing)
	}
	return r, err
}

// ========================================
// Fixed Files (Linux 5.1+)
// ========================================

// RegisterFiles registers file descriptors for use with IOSQE_FIXED_FILE flag.
// Registered files bypass per-operation fget/fput kernel calls.
//
// Once registered, use the file index (0-based) instead of the fd in SQEs,
// and set the IOSQE_FIXED_FILE flag.
//
// Returns ErrExists if files are already registered.
// Use UnregisterFiles before re-registering.
func (ur *Uring) RegisterFiles(fds []int32) error {
	return ur.registerFiles(fds)
}

// RegisterFilesSparse allocates a sparse file table of the given size.
// All entries are initially empty (-1) and can be populated dynamically
// using RegisterFilesUpdate.
//
// Sparse registration is useful for applications that manage a dynamic
// set of file descriptors (e.g., connection pools, file caches).
//
// Requires kernel 5.19+.
func (ur *Uring) RegisterFilesSparse(count uint32) error {
	return ur.registerFilesSparse(count)
}

// RegisterFilesUpdate updates registered files at the specified offset.
// Use -1 to clear a slot, or a valid fd to set it.
//
// This allows dynamic management of registered files without
// unregistering and re-registering the entire table.
func (ur *Uring) RegisterFilesUpdate(offset uint32, fds []int32) error {
	return ur.registerFilesUpdate(offset, fds)
}

// UnregisterFiles removes all registered file descriptors.
// After unregistering, IOSQE_FIXED_FILE flag must not be used
// until new files are registered.
func (ur *Uring) UnregisterFiles() error {
	return ur.unregisterFiles()
}

// RegisteredFileCount returns the number of registered files, or 0 if none.
func (ur *Uring) RegisteredFileCount() int {
	return len(ur.files)
}

// ========================================
// Query Interface (Linux 6.19+)
// ========================================

// QueryOpcodes queries the kernel for supported io_uring operations.
// Returns detailed information about supported opcodes, features, and flags.
// Requires kernel 6.19+.
func (ur *Uring) QueryOpcodes() (*QueryOpcode, error) {
	return ur.queryOpcodes()
}

// QueryZCRX queries the kernel for ZCRX (zero-copy receive) capabilities.
// Returns information about supported ZCRX features and configuration.
// Requires kernel 6.19+.
func (ur *Uring) QueryZCRX() (*QueryZCRX, error) {
	return ur.queryZCRX()
}

// QuerySCQ queries the kernel for SQ/CQ ring information.
// Returns header size and alignment requirements for shared rings.
// Requires kernel 6.19+.
func (ur *Uring) QuerySCQ() (*QuerySCQ, error) {
	return ur.querySCQ()
}

// ========================================
// Memory Region Support (Linux 6.19+)
// ========================================

// RegisterMemRegion registers a memory region with the io_uring ring.
// Memory regions allow efficient sharing between user space and kernel.
// Requires kernel 6.19+.
func (ur *Uring) RegisterMemRegion(region *RegionDesc, flags uint64) error {
	reg := MemRegionReg{
		RegionUptr: uint64(uintptr(unsafe.Pointer(region))),
		Flags:      flags,
	}
	return ur.registerMemRegion(&reg)
}

// ========================================
// NAPI Support (Linux 6.19+)
// ========================================

// RegisterNAPI enables NAPI busy polling for network operations.
// NAPI (New API) provides more efficient network packet processing
// by allowing the kernel to batch packet handling.
//
// Parameters:
//   - busyPollTimeout: timeout in microseconds for busy polling
//   - preferBusyPoll: if true, prefer busy poll over sleeping
//   - strategy: IO_URING_NAPI_TRACKING_* strategy
//
// Requires kernel 6.19+.
func (ur *Uring) RegisterNAPI(busyPollTimeout uint32, preferBusyPoll bool, strategy uint32) error {
	prefer := uint8(0)
	if preferBusyPoll {
		prefer = 1
	}
	napi := NapiReg{
		BusyPollTo:     busyPollTimeout,
		PreferBusyPoll: prefer,
		Opcode:         IO_URING_NAPI_REGISTER_OP,
		OpParam:        strategy,
	}
	return ur.registerNAPI(&napi)
}

// NAPIAddStaticID adds a NAPI ID for static tracking mode.
// Use this when IO_URING_NAPI_TRACKING_STATIC is enabled.
// Requires kernel 6.19+.
func (ur *Uring) NAPIAddStaticID(napiID uint32) error {
	napi := NapiReg{
		Opcode:  IO_URING_NAPI_STATIC_ADD_ID,
		OpParam: napiID,
	}
	return ur.registerNAPI(&napi)
}

// NAPIDelStaticID removes a NAPI ID from static tracking.
// Use this when IO_URING_NAPI_TRACKING_STATIC is enabled.
// Requires kernel 6.19+.
func (ur *Uring) NAPIDelStaticID(napiID uint32) error {
	napi := NapiReg{
		Opcode:  IO_URING_NAPI_STATIC_DEL_ID,
		OpParam: napiID,
	}
	return ur.registerNAPI(&napi)
}

// UnregisterNAPI disables NAPI busy polling for this ring.
func (ur *Uring) UnregisterNAPI() error {
	return ur.unregisterNAPI()
}

// ========================================
// Clone Buffers (Linux 6.19+)
// ========================================

// CloneBuffers clones registered buffers from another io_uring ring.
// This allows efficient buffer sharing between multiple rings.
//
// Parameters:
//   - srcFD: source ring file descriptor
//   - srcOff: source buffer offset
//   - dstOff: destination buffer offset
//   - count: number of buffers to clone
//   - replace: if true, replace existing buffers at destination offset
//
// Requires kernel 6.19+.
func (ur *Uring) CloneBuffers(srcFD int, srcOff, dstOff, count uint32, replace bool) error {
	flags := uint32(0)
	if replace {
		flags |= IORING_REGISTER_DST_REPLACE
	}
	clone := CloneBuffers{
		SrcFD:  uint32(srcFD),
		Flags:  flags,
		SrcOff: srcOff,
		DstOff: dstOff,
		Nr:     count,
	}
	return ur.cloneBuffers(&clone)
}

// CloneBuffersFromRegistered clones buffers from a registered ring.
// The source ring must be registered with IORING_REGISTER_RING_FDS.
// Requires kernel 6.19+.
func (ur *Uring) CloneBuffersFromRegistered(srcRegisteredIdx int, srcOff, dstOff, count uint32, replace bool) error {
	flags := uint32(IORING_REGISTER_SRC_REGISTERED)
	if replace {
		flags |= IORING_REGISTER_DST_REPLACE
	}
	clone := CloneBuffers{
		SrcFD:  uint32(srcRegisteredIdx),
		Flags:  flags,
		SrcOff: srcOff,
		DstOff: dstOff,
		Nr:     count,
	}
	return ur.cloneBuffers(&clone)
}

// ========================================
// Ring Resize (Linux 6.19+)
// ========================================

// ResizeRings resizes the SQ and CQ rings of this io_uring instance.
// This allows dynamic adjustment of ring sizes without recreating the ring.
//
// Requirements:
//   - The ring must be created with IORING_SETUP_DEFER_TASKRUN flag
//   - The ring must not be in CQ overflow condition
//   - Sizes must be power-of-two values
//
// Parameters:
//   - newSQSize: New SQ ring size (0 to keep current)
//   - newCQSize: New CQ ring size (0 defaults to 2×newSQSize)
//
// Requires kernel 6.13+.
func (ur *Uring) ResizeRings(newSQSize, newCQSize uint32) error {
	return ur.resizeRings(newSQSize, newCQSize)
}

// ========================================
// ZCRX Export (Linux 6.19+)
// ========================================

// ZCRXExport exports a ZCRX instance for cross-ring sharing.
// Returns a file descriptor that can be passed to another process.
// Requires kernel 6.19+.
func (ur *Uring) ZCRXExport(zcrxID uint32) (int, error) {
	ctrl := ZCRXCtrl{
		ZcrxID: zcrxID,
		Op:     ZCRX_CTRL_EXPORT,
	}
	// The zcrx_fd is at offset 0 of the export data
	err := ur.zcrxCtrl(&ctrl)
	if err != nil {
		return -1, err
	}
	// Extract fd from Data field (first 4 bytes)
	fd := *(*uint32)(unsafe.Pointer(&ctrl.Data[0]))
	return int(fd), nil
}

// ========================================
// Extended Attributes (xattr) Operations
// ========================================

// FSetXattr sets an extended attribute on a file descriptor.
func (ur *Uring) FSetXattr(sqeCtx SQEContext, name string, value []byte, flags int, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.fsetxattr(sqeCtx, name, value, flags)
}

// SetXattr sets an extended attribute on a path.
func (ur *Uring) SetXattr(sqeCtx SQEContext, path, name string, value []byte, flags int, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.setxattr(sqeCtx, path, name, value, flags)
}

// FGetXattr gets an extended attribute from a file descriptor.
// The result length is returned in the CQE.
func (ur *Uring) FGetXattr(sqeCtx SQEContext, name string, value []byte, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.fgetxattr(sqeCtx, name, value)
}

// GetXattr gets an extended attribute from a path.
// The result length is returned in the CQE.
func (ur *Uring) GetXattr(sqeCtx SQEContext, path, name string, value []byte, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.getxattr(sqeCtx, path, name, value)
}

// ========================================
// Filesystem Operations
// ========================================

// Statx gets file status with extended information.
func (ur *Uring) Statx(sqeCtx SQEContext, path string, flags, mask int, stat *Statx_t, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.statx(sqeCtx, path, flags, mask, stat)
}

// RenameAt renames a file at a path.
func (ur *Uring) RenameAt(sqeCtx SQEContext, oldPath, newPath string, flags int, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.renameAt(sqeCtx, oldPath, newPath, flags)
}

// UnlinkAt removes a file or directory.
func (ur *Uring) UnlinkAt(sqeCtx SQEContext, path string, flags int, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.unlinkAt(sqeCtx, path, flags)
}

// MkdirAt creates a directory.
func (ur *Uring) MkdirAt(sqeCtx SQEContext, path string, mode uint32, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.mkdirAt(sqeCtx, path, 0, mode)
}

// SymlinkAt creates a symbolic link.
func (ur *Uring) SymlinkAt(sqeCtx SQEContext, target, linkpath string, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.symlinkAt(sqeCtx, target, linkpath)
}

// LinkAt creates a hard link.
func (ur *Uring) LinkAt(sqeCtx SQEContext, oldDirfd int, oldPath, newPath string, flags int, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.linkAt(sqeCtx, oldDirfd, oldPath, newPath, flags)
}

// FTruncate truncates a file to the specified length.
func (ur *Uring) FTruncate(sqeCtx SQEContext, length int64, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.fTruncate(sqeCtx, length)
}

// ========================================
// Futex Operations (Linux 6.7+)
// ========================================

// FutexWait submits an async futex wait operation.
// Waits until the value at addr matches val, using the specified mask and flags.
// Requires kernel 6.7+.
func (ur *Uring) FutexWait(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.futexWait(sqeCtx, addr, val, mask, flags)
}

// FutexWake submits an async futex wake operation.
// Wakes up to val waiters on the futex at addr, using the specified mask and flags.
// Requires kernel 6.7+.
func (ur *Uring) FutexWake(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.futexWake(sqeCtx, addr, val, mask, flags)
}

// FutexWaitV submits a vectored futex wait operation.
// Waits on multiple futexes simultaneously. The waitv pointer should point to
// a struct futex_waitv array with count elements.
// Requires kernel 6.7+.
func (ur *Uring) FutexWaitV(sqeCtx SQEContext, waitv unsafe.Pointer, count uint32, flags uint32, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.futexWaitV(sqeCtx, waitv, count, flags)
}

// ========================================
// Process Operations (Linux 6.7+)
// ========================================

// Waitid waits for a process to change state asynchronously.
// idtype specifies which id to wait for (P_PID, P_PGID, P_ALL).
// The siginfo_t result is written to infop.
// Requires kernel 6.7+.
func (ur *Uring) Waitid(sqeCtx SQEContext, idtype, id int, infop unsafe.Pointer, options int, opts ...OpOptionFunc) error {
	_ = ur.operationOptions(opts)
	return ur.ioUring.waitid(sqeCtx, idtype, id, infop, options)
}

// ========================================
// Fixed Descriptor Operations (Linux 6.8+)
// ========================================

// FixedFdInstall installs a fixed (registered) file descriptor into the
// normal file descriptor table. Returns the new fd in the CQE result.
// The fixedIndex is the index in the registered files table.
// Requires kernel 6.8+.
func (ur *Uring) FixedFdInstall(sqeCtx SQEContext, fixedIndex int, flags uint32, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.fixedFdInstall(sqeCtx, fixedIndex, flags)
}

// FilesUpdate updates registered files at the specified offset.
// The fds slice contains the new file descriptors to register.
// Use -1 to unregister a slot.
func (ur *Uring) FilesUpdate(sqeCtx SQEContext, fds []int32, offset int, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.filesUpdate(sqeCtx, fds, offset)
}

// ========================================
// Inter-Ring Messaging
// ========================================

// MsgRing sends a message to another io_uring instance.
// The sqeCtx.FD() should be the target ring's file descriptor.
// userData and result are passed to the target ring's CQE.
func (ur *Uring) MsgRing(sqeCtx SQEContext, userData int64, result int32, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.msgRing(sqeCtx, userData, result)
}

// MsgRingFD transfers a fixed file descriptor to another io_uring instance.
// This is useful for multi-ring architectures where one ring accepts connections
// and passes them to worker rings.
//
// Parameters:
//   - sqeCtx: Context with target ring's FD (from RingFD())
//   - srcFD: Fixed file index in the source ring (this ring)
//   - dstSlot: Fixed file slot in the target ring to install the FD
//   - userData: Value passed to the target ring's CQE
//   - skipCQE: If true, no CQE is posted to the target ring
//
// Requires kernel 5.18+. Both rings must have registered file tables.
func (ur *Uring) MsgRingFD(sqeCtx SQEContext, srcFD uint32, dstSlot uint32, userData int64, skipCQE bool, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	var flags uint32
	if skipCQE {
		flags = IORING_MSG_RING_CQE_SKIP
	}
	return ur.ioUring.msgRingFD(sqeCtx, srcFD, dstSlot, userData, flags)
}

// ========================================
// Passthrough Commands (Linux 5.19+/6.13+)
// ========================================

// UringCmd submits a generic passthrough command.
// The cmdOp specifies the command operation, and cmdData provides optional data.
// Requires kernel 5.19+.
func (ur *Uring) UringCmd(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.uringCmd(sqeCtx, cmdOp, cmdData)
}

// Nop128 submits a 128-byte NOP operation for testing mixed SQE mode.
// Requires kernel 6.13+ with IORING_SETUP_SQE_MIXED.
func (ur *Uring) Nop128(sqeCtx SQEContext, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.nop128(sqeCtx)
}

// UringCmd128 submits a 128-byte passthrough command.
// Provides 80 bytes of command data space (vs 48 bytes for standard UringCmd).
// Requires kernel 6.13+ with IORING_SETUP_SQE_MIXED.
func (ur *Uring) UringCmd128(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, options ...OpOptionFunc) error {
	_ = ur.operationOptions(options)
	return ur.ioUring.uringCmd128(sqeCtx, cmdOp, cmdData)
}
