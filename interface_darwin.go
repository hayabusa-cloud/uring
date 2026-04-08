// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"encoding/binary"
	"errors"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
)

const (
	EntriesPico   = 1 << 3  // 8 entries
	EntriesNano   = 1 << 5  // 32 entries
	EntriesMicro  = 1 << 7  // 128 entries
	EntriesSmall  = 1 << 9  // 512 entries
	EntriesMedium = 1 << 11 // 2048 entries
	EntriesLarge  = 1 << 13 // 8192 entries
	EntriesHuge   = 1 << 15 // 32768 entries
)

// Options configures the io_uring instance behavior.
// All fields have sensible defaults if not specified.
type Options struct {
	Entries                 int
	LockedBufferMem         int
	ReadBufferSize          int
	ReadBufferNum           int
	ReadBufferGidOffset     int
	WriteBufferSize         int
	WriteBufferNum          int
	MultiSizeBuffer         int
	MultiIssuers            bool
	NotifySucceed           bool
	IndirectSubmissionQueue bool
	HybridPolling           bool
}

// New creates a new io_uring instance with the specified options.
// Returns an unstarted ring; call Start() to initialize buffers and enable.
func New(options ...OptionFunc) (*Uring, error) {
	opt := defaultOptions
	opt.Apply(options...)
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
	if !opt.NotifySucceed {
		wFlags |= IOSQE_CQE_SKIP_SUCCESS
	}
	ret := Uring{
		ioUring:          r,
		Options:          &opt,
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
	feat := Features{
		SQEntries:         int(r.params.sqEntries),
		CQEntries:         int(r.params.cqEntries),
		UserDataByteOrder: binary.LittleEndian,
	}
	if isBigEndian {
		feat.UserDataByteOrder = binary.BigEndian
	}
	ret.Features = &feat

	return &ret, nil
}

// Features reports per-ring sizing and metadata returned at creation time.
type Features struct {
	SQEntries         int
	CQEntries         int
	UserDataByteOrder binary.ByteOrder
}

// Uring is the main io_uring interface for submitting and completing I/O operations.
// It wraps the kernel io_uring instance with buffer management and typed operations.
// Default rings use the single-issuer fast path, so submit-state operations
// are not safe for concurrent use by multiple goroutines; caller must
// serialize submit, Wait/enter, Stop, and ResizeRings unless MultiIssuers is
// enabled.
type Uring struct {
	*ioUring
	*Options
	Features *Features

	buffers      *uringProvideBuffers
	bufferGroups *uringProvideBufferGroups
	bufferRings  *uringBufferRings

	buffersPool *RegisterBufferPool

	// ctxPools provides lock-free pools for IndirectSQE and ExtSQE contexts.
	ctxPools *ContextPools

	readLikeOpFlags  uint8
	writeLikeOpFlags uint8
}

// Start initializes the io_uring instance, populates simulated capabilities, registers buffers, and enables the ring.
func (ur *Uring) Start() (err error) {
	if ur.closed.Load() {
		return ErrClosed
	}
	if !ur.started.CompareAndSwap(false, true) {
		return ErrExists
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, ur.Stop())
		}
	}()

	// populate simulated capabilities
	probe := ioUringProbe{}
	err = ur.registerProbe(&probe)
	if err != nil {
		return err
	}

	// register buffers (Darwin uses in-memory buffers)
	regBufNum := min(registerBufferNum, ur.LockedBufferMem/registerBufferSize)
	if regBufNum > 0 {
		ur.buffersPool = NewRegisterBufferPool(regBufNum)
		ur.buffersPool.Fill(func() RegisterBuffer { return RegisterBuffer{} })
		regBufAddr := unsafe.Pointer(unsafe.SliceData(ur.buffersPool.items))
		err = ur.registerBuffers(regBufAddr, regBufNum, registerBufferSize)
		if err != nil {
			return err
		}
	}

	// provide buffers (simulated on Darwin)
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

	// enable ring (starts worker goroutines on Darwin)
	err = ur.enable()
	if err != nil {
		return err
	}

	return nil
}

// Stop tears down Darwin simulator resources and makes the ring permanently unusable.
// It is idempotent. On single-issuer rings it is not safe for concurrent use
// with submit or Wait/enter; caller must serialize those operations.
func (ur *Uring) Stop() error {
	var err error
	if ur.bufferRings != nil {
		if stopErr := ur.bufferRings.release(ur.ioUring); stopErr != nil {
			err = errors.Join(err, stopErr)
		}
	}
	if stopErr := ur.ioUring.stop(); stopErr != nil {
		err = errors.Join(err, stopErr)
	}
	ur.buffersPool = nil
	ur.buffers = nil
	ur.bufferGroups = nil
	return err
}

// Wait flushes pending submissions and collects completion events into cqes.
// On single-issuer rings it is not safe for concurrent use with submit or
// Stop; caller must serialize those operations. It returns the number of
// events received, or `iox.ErrWouldBlock` if the CQ is empty.
func (ur *Uring) Wait(cqes []CQEView) (n int, err error) {
	err = ur.ioUring.enter()
	if err != nil {
		return 0, err
	}

	// Use batch retrieval for consistency with Linux implementation
	return ur.ioUring.waitBatch(cqes)
}

// ExtSQE acquires an ExtSQE from the pool for Extended mode submissions.
// The returned ExtSQE is borrowed until PutExtSQE after the corresponding CQE
// is processed. Callers must not retain pointers into SQE or UserData after
// release.
//
//go:nosplit
func (ur *Uring) ExtSQE() *ExtSQE {
	return ur.ctxPools.Extended()
}

// PutExtSQE returns an ExtSQE to the pool after completion processing.
// After this call the ExtSQE, typed context views, and raw CastUserData
// overlays derived from it are invalid.
//
//go:nosplit
func (ur *Uring) PutExtSQE(sqe *ExtSQE) {
	ur.ctxPools.PutExtended(sqe)
}

// IndirectSQE acquires an IndirectSQE from the pool.
// The returned IndirectSQE is borrowed until PutIndirectSQE.
//
//go:nosplit
func (ur *Uring) IndirectSQE() *IndirectSQE {
	return ur.ctxPools.Indirect()
}

// PutIndirectSQE returns an IndirectSQE to the pool.
// After this call the IndirectSQE is invalid and must not be reused.
//
//go:nosplit
func (ur *Uring) PutIndirectSQE(sqe *IndirectSQE) {
	ur.ctxPools.PutIndirect(sqe)
}

// SubmitExtended submits an SQE using Extended mode context.
func (ur *Uring) SubmitExtended(sqeCtx SQEContext) error {
	return ur.ioUring.submitExtended(sqeCtx)
}

// ========================================
// Registered Buffer Access
// ========================================

// RegisteredBuffer returns the registered buffer at the given index.
// Returns nil if the index is out of range.
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
// Fixed Files (Darwin stub)
// ========================================

// RegisterFiles registers file descriptors for use with IOSQE_FIXED_FILE flag.
func (ur *Uring) RegisterFiles(fds []int32) error {
	return ur.registerFiles(fds)
}

// RegisterFilesSparse allocates a sparse file table of the given size.
func (ur *Uring) RegisterFilesSparse(count uint32) error {
	return ur.registerFilesSparse(count)
}

// RegisterFilesUpdate updates registered files at the specified offset.
func (ur *Uring) RegisterFilesUpdate(offset uint32, fds []int32) error {
	return ur.registerFilesUpdate(offset, fds)
}

// UnregisterFiles removes all registered file descriptors.
func (ur *Uring) UnregisterFiles() error {
	return ur.unregisterFiles()
}

// RegisteredFileCount returns the number of registered files, or 0 if none.
func (ur *Uring) RegisteredFileCount() int {
	return len(ur.files)
}

// ========================================
// Ring Introspection (for backpressure)
// ========================================

// SQAvailable returns the number of SQEs available for submission.
// Higher layers can use this for admission control and backpressure.
//
//go:nosplit
func (ur *Uring) SQAvailable() int {
	return cap(ur.sqChan) - len(ur.sqChan)
}

// CQPending returns the number of CQEs waiting to be reaped.
// Higher layers can use this to decide when to drain completions.
//
//go:nosplit
func (ur *Uring) CQPending() int {
	return len(ur.cqChan)
}

// RingFD returns the io_uring file descriptor.
// Darwin stub returns -1 (no real io_uring).
//
//go:nosplit
func (ur *Uring) RingFD() int {
	return -1
}

// MsgRing sends a message to another io_uring instance.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) MsgRing(sqeCtx SQEContext, userData int64, result int32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// MsgRingFD transfers a fixed file descriptor to another io_uring instance.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) MsgRingFD(sqeCtx SQEContext, srcFD uint32, dstSlot uint32, userData int64, skipCQE bool, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SocketRaw creates a socket using io_uring.
func (ur *Uring) SocketRaw(sqeCtx SQEContext, domain, typ, proto int, options ...OpOptionFunc) error {
	flags, fileIndex := ur.socketOptions(options)
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
// SOCK_CLOEXEC is not supported with direct socket; use SOCK_NONBLOCK only.
// Requires registered files via RegisterFiles or RegisterFilesSparse.
func (ur *Uring) SocketDirect(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// TCP4SocketDirect creates a TCP IPv4 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) TCP4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// TCP6SocketDirect creates a TCP IPv6 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) TCP6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UDP4SocketDirect creates a UDP IPv4 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) UDP4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UDP6SocketDirect creates a UDP IPv6 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) UDP6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UDPLITE4SocketDirect creates a UDP-Lite IPv4 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) UDPLITE4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UDPLITE6SocketDirect creates a UDP-Lite IPv6 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) UDPLITE6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SCTP4SocketDirect creates an SCTP IPv4 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) SCTP4SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SCTP6SocketDirect creates an SCTP IPv6 socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) SCTP6SocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UnixSocketDirect creates a Unix domain socket directly into a registered file slot.
// Uses auto-allocation (IORING_FILE_INDEX_ALLOC) for the file index.
func (ur *Uring) UnixSocketDirect(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Bind binds a socket to an address.
func (ur *Uring) Bind(sqeCtx SQEContext, addr Addr, options ...OpOptionFunc) error {
	flags := ur.bindOptions(options)
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
func (ur *Uring) Accept(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.accept(ctx, ioprio)
}

// SubmitAcceptMultishot submits the raw kernel multishot accept opcode.
// For the managed subscription helper, use [Uring.AcceptMultishot].
func (ur *Uring) SubmitAcceptMultishot(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.accept(ctx, ioprio|IORING_ACCEPT_MULTISHOT)
}

// AcceptDirect accepts a connection directly into a registered file table slot.
// The fileIndex specifies which slot to use (0-based), or use IORING_FILE_INDEX_ALLOC
// for auto-allocation (the allocated index is returned in CQE res).
// SOCK_CLOEXEC is not supported with direct accept.
// Requires registered files via RegisterFiles or RegisterFilesSparse.
func (ur *Uring) AcceptDirect(sqeCtx SQEContext, fileIndex uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SubmitAcceptDirectMultishot submits the raw kernel multishot accept-direct opcode.
// Each accepted connection uses the next available slot from auto-allocation.
// Requires IORING_FILE_INDEX_ALLOC as fileIndex for auto-allocation.
func (ur *Uring) SubmitAcceptDirectMultishot(sqeCtx SQEContext, fileIndex uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Connect initiates a socket connection to a remote address.
func (ur *Uring) Connect(sqeCtx SQEContext, remote Addr, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.connect(ctx, AddrToSockaddr(remote))
}

// Receive performs a socket receive operation.
// If b is non-nil, caller must keep b valid until the operation completes.
func (ur *Uring) Receive(sqeCtx SQEContext, pollFD PollFd, b []byte, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd()))
	if b == nil {
		flags, ioprio, bufSize, bufGroup := ur.receiveWithBufferSelectOptions(pollFD, options)
		ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
		return ur.receiveWithBufferSelect(ctx, ioprio, bufSize, bufGroup)
	}
	flags, ioprio, offset, n := ur.receiveOptions(b, options)
	ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.receive(ctx, ioprio, b, uint64(offset), n)
}

// SubmitReceiveMultishot submits the raw kernel multishot receive opcode.
// For the managed subscription helper, use [Uring.ReceiveMultishot].
func (ur *Uring) SubmitReceiveMultishot(sqeCtx SQEContext, pollFD PollFd, b []byte, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd()))
	if b == nil {
		flags, ioprio, bufSize, bufGroup := ur.receiveWithBufferSelectOptions(pollFD, options)
		ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
		return ur.receiveWithBufferSelect(ctx, ioprio|IORING_RECV_MULTISHOT, bufSize, bufGroup)
	}
	flags, ioprio, offset, n := ur.receiveOptions(b, options)
	ctx = ctx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.receive(ctx, ioprio|IORING_RECV_MULTISHOT, b, uint64(offset), n)
}

// Send writes data to a socket.
// Caller must keep p valid until the operation completes.
func (ur *Uring) Send(sqeCtx SQEContext, pollFD PollFd, p []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.sendOptions(p, options)
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd())).WithFlags(flags | ur.writeLikeOpFlags)
	return ur.send(ctx, ioprio, p, uint64(offset), n)
}

// SendTargets represents a set of target sockets for multicast/broadcast.
type SendTargets interface {
	Count() int
	FD(i int) iofd.FD
}

// Multicast sends data to multiple sockets.
func (ur *Uring) Multicast(sqeCtx SQEContext, targets SendTargets, bufIndex int, p []byte, offset int64, n int, options ...OpOptionFunc) error {
	count := targets.Count()
	if count == 0 {
		return nil
	}

	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)

	const zeroCopyThreshold = 8
	const payloadThreshold = 4 * 1024

	useZeroCopy := bufIndex >= 0 && count > zeroCopyThreshold && n >= payloadThreshold

	var err error
	for i := range count {
		fd := targets.FD(i)
		targetCtx := ctx.WithFD(fd)

		var sendErr error
		if useZeroCopy {
			sendErr = ur.sendZeroCopyFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		} else if bufIndex >= 0 {
			sendErr = ur.writeFixed(targetCtx, bufIndex, uint64(offset), n)
		} else {
			sendErr = ur.send(targetCtx, 0, p, uint64(offset), n)
		}
		err = errors.Join(err, sendErr)
	}
	return err
}

// MulticastZeroCopy sends data to multiple sockets using zero-copy.
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

	var err error
	for i := range count {
		fd := targets.FD(i)
		targetCtx := ctx.WithFD(fd)
		sendErr := ur.sendZeroCopyFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		err = errors.Join(err, sendErr)
	}
	return err
}

// Timeout submits a timeout request.
func (ur *Uring) Timeout(sqeCtx SQEContext, d time.Duration, options ...OpOptionFunc) error {
	flags, cnt, timeoutFlags := ur.timeoutOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	nano := d.Nanoseconds()
	return ur.timeout(ctx, cnt, &Timespec{Sec: nano / int64(time.Second), Nsec: nano % int64(time.Second)}, int(timeoutFlags))
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

// Close submits `IORING_OP_CLOSE` for the file descriptor carried in sqeCtx.
// It closes a target fd; it does not tear down the Uring instance.
func (ur *Uring) Close(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	return ur.close(sqeCtx.WithFlags(flags))
}

// Read performs a read operation.
// Caller must keep b valid until the operation completes.
func (ur *Uring) Read(sqeCtx SQEContext, b []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.readOptions(b, options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.read(ctx, ioprio, b, uint64(offset), n)
}

// Write performs a write operation.
// Caller must keep b valid until the operation completes.
func (ur *Uring) Write(sqeCtx SQEContext, b []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.writeOptions(b, options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.write(ctx, ioprio, b, uint64(offset), n)
}

// Splice transfers data between file descriptors.
func (ur *Uring) Splice(sqeCtx SQEContext, fdIn int, n int, options ...OpOptionFunc) error {
	flags, _, spliceFlags := ur.spliceOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.splice(ctx, fdIn, nil, nil, n, int(spliceFlags))
}

// Tee duplicates data between pipes.
func (ur *Uring) Tee(sqeCtx SQEContext, fdIn int, length int, options ...OpOptionFunc) error {
	flags, _, spliceFlags := ur.teeOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.tee(ctx, fdIn, length, int(spliceFlags))
}

// Sync performs a file sync operation.
func (ur *Uring) Sync(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, fsyncFlags := ur.syncOptions(options)
	return ur.fsync(sqeCtx.WithFlags(flags), fsyncFlags)
}

// ReadV performs a vectored read operation.
// Caller must keep iovs and their backing buffers valid until the operation completes.
func (ur *Uring) ReadV(sqeCtx SQEContext, iovs [][]byte, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.readv(ctx, iovs)
}

// WriteV performs a vectored write operation.
// Caller must keep iovs and their backing buffers valid until the operation completes.
func (ur *Uring) WriteV(sqeCtx SQEContext, iovs [][]byte, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.writev(ctx, iovs)
}

// ReadFixed performs a read with a registered buffer.
func (ur *Uring) ReadFixed(sqeCtx SQEContext, bufIndex int, options ...OpOptionFunc) ([]byte, error) {
	flags, offset, n := ur.readFixedOptions(bufIndex, options)
	ctx := sqeCtx.WithFlags(flags | ur.readLikeOpFlags)
	return ur.readFixed(ctx, bufIndex, uint64(offset), n)
}

// WriteFixed performs a write with a registered buffer.
func (ur *Uring) WriteFixed(sqeCtx SQEContext, bufIndex int, n int, options ...OpOptionFunc) error {
	flags, offset := ur.writeFixedOptions(options)
	ctx := sqeCtx.WithFlags(flags | ur.writeLikeOpFlags)
	return ur.writeFixed(ctx, bufIndex, uint64(offset), n)
}

// OpenAt opens a file.
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
// Caller must keep buffers and oob valid until the operation completes.
func (ur *Uring) SendMsg(sqeCtx SQEContext, pollFD PollFd, buffers [][]byte, oob []byte, to Addr, options ...OpOptionFunc) error {
	flags, ioprio := ur.sendmsgOptions(options)
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd())).WithFlags(flags | ur.writeLikeOpFlags)
	var sa Sockaddr
	if to != nil {
		sa = AddrToSockaddr(to)
	}
	return ur.sendmsg(ctx, ioprio, buffers, oob, sa)
}

// RecvMsg receives a message with control data.
// Caller must keep buffers and oob valid until the operation completes.
func (ur *Uring) RecvMsg(sqeCtx SQEContext, pollFD PollFd, buffers [][]byte, oob []byte, options ...OpOptionFunc) error {
	flags, ioprio := ur.recvmsgOptions(options)
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd())).WithFlags(flags | ur.readLikeOpFlags)
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
// Darwin stub returns ErrNotSupported.
func (ur *Uring) PollAddMultishot(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// PollAddLevel adds a level-triggered poll request.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) PollAddLevel(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// PollAddMultishotLevel combines multishot and level-triggered modes.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) PollAddMultishotLevel(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// PollUpdate modifies an existing poll request in-place.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) PollUpdate(sqeCtx SQEContext, oldUserData, newUserData uint64, newEvents, updateFlags int, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// TimeoutRemove removes a timeout request.
func (ur *Uring) TimeoutRemove(sqeCtx SQEContext, userData uint64, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.timeoutRemove(ctx, userData, 0)
}

// TimeoutUpdate modifies an existing timeout request in-place.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) TimeoutUpdate(sqeCtx SQEContext, userData uint64, d time.Duration, absolute bool, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// AsyncCancel cancels a pending async operation.
func (ur *Uring) AsyncCancel(sqeCtx SQEContext, targetUserData uint64, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.WithFlags(flags)
	return ur.asyncCancel(ctx, targetUserData)
}

// AsyncCancelFD cancels operations on a specific file descriptor.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) AsyncCancelFD(sqeCtx SQEContext, cancelAll bool, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// AsyncCancelOpcode cancels operations of a specific opcode type.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) AsyncCancelOpcode(sqeCtx SQEContext, opcode uint8, cancelAll bool, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// AsyncCancelAny cancels any one pending operation.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) AsyncCancelAny(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// AsyncCancelAll cancels all pending operations.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) AsyncCancelAll(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
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
