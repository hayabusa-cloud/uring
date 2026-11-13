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
	// Entries must be between 1 and EntriesHuge, inclusive.
	Entries int
	// LockedBufferMem is the total memory for registered buffers. It must be a
	// multiple of BufferSizeLarge from BufferSizeLarge through the package
	// maximum selected by LargeLockedBufferMemOptions.
	LockedBufferMem int
	// ReadBufferSize is the size of each single-size provided buffer. New rounds
	// values in [2, 1<<28] upward to a power of two. Multi-size mode ignores it.
	ReadBufferSize int
	// ReadBufferNum is the number of single-size provided buffers. New rounds
	// values in [2, 1<<28] upward to a power of two. Multi-size mode ignores it.
	ReadBufferNum int
	// ReadBufferGidOffset is the non-negative base group ID. The selected buffer
	// layout must fit in the remaining uint16 group-ID namespace.
	ReadBufferGidOffset int
	// MultiSizeBuffer selects single-size mode at 0. Values in [1, 4096] enable
	// multi-size groups and are rounded upward to a power of two.
	MultiSizeBuffer int
	// MultiIssuers mirrors the Linux task-migratable setup contract. It defaults
	// to true; set it to false only for a caller pinned to one OS thread.
	MultiIssuers bool
	// NotifySucceed disables default CQE suppression on write-like operations.
	// Explicit IOSQE_CQE_SKIP_SUCCESS flags still apply.
	NotifySucceed           bool
	IndirectSubmissionQueue bool
	SQE128                  bool
	HybridPolling           bool
}

// New creates a new io_uring instance with the specified options.
// It returns ErrInvalidParam for an invalid option domain before creating the
// ring. A successful call returns an unstarted ring; call Start to initialize
// buffers and enable it.
func New(options ...OptionFunc) (*Uring, error) {
	opt := defaultOptions
	opt.Apply(options...)
	if err := opt.validate(); err != nil {
		return nil, err
	}
	if opt.SQE128 {
		return nil, ErrNotSupported
	}
	setupOpts := newSetupOptions(opt, false)
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
		SQEBytes:          sqeBytesFromFlags(r.params.flags),
		UserDataByteOrder: binary.LittleEndian,
	}
	if isBigEndian {
		feat.UserDataByteOrder = binary.BigEndian
	}
	ret.Features = &feat

	return &ret, nil
}

// Features reports the stub's queue geometry and user_data byte order.
type Features struct {
	SQEntries         int
	CQEntries         int
	SQEBytes          int
	UserDataByteOrder binary.ByteOrder
}

func newSetupOptions(opt Options, sqe128 bool) []func(params *ioUringParams) {
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
	if sqe128 {
		setupOpts = append(setupOpts, ioUringSQE128Options)
	}
	if opt.HybridPolling {
		setupOpts = append(setupOpts, ioUringHybridIoPollOptions)
	}
	return setupOpts
}

// Uring is the main io_uring interface for submitting and completing I/O operations.
// It wraps the kernel io_uring instance with buffer management and typed operations.
// Default rings use the task-migratable shared-submit path. Set MultiIssuers
// to false only for a caller pinned to one OS thread from New through Stop.
type Uring struct {
	_ [0]func()

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

	// Provide buffers are tracked purely in userspace on Darwin, so startup must
	// not enqueue synthetic PROVIDE_BUFFERS SQEs that would leak setup CQEs.
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
// It is idempotent. Caller must drain all in-flight operations, reap
// outstanding CQEs, and quiesce live subscriptions before calling Stop. Stop
// is not safe for concurrent use.
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
// On single-issuer rings it is not safe for concurrent use with submit, Wait,
// WaitDirect, WaitExtended, or Stop; caller must serialize those operations.
// It returns the number of events received, or `iox.ErrWouldBlock` if the CQ
// is empty.
func (ur *Uring) Wait(cqes []CQEView) (n int, err error) {
	err = ur.ioUring.enter()
	if err != nil {
		return 0, err
	}

	// Use batch retrieval for consistency with Linux implementation
	return ur.ioUring.waitBatch(cqes)
}

// ExtSQE borrows a cleared ExtSQE for Extended mode submissions.
// It returns nil if the context pool is exhausted, independently of SQ capacity.
// Keep the context until all request completions and borrowed access finish,
// then return it to this ring with PutExtSQE.
//
//go:nosplit
func (ur *Uring) ExtSQE() *ExtSQE {
	return ur.ctxPools.Extended()
}

// PutExtSQE clears and returns an ExtSQE borrowed from this ring.
// Call exactly once after all request completions and borrowed access finish.
// This includes terminal multishot and zero-copy notification CQEs when present.
// After return, the ExtSQE and all views into it are invalid.
//
//go:nosplit
func (ur *Uring) PutExtSQE(sqe *ExtSQE) {
	ur.ctxPools.PutExtended(sqe)
}

// IndirectSQE borrows an IndirectSQE for Indirect mode submissions.
// It returns nil if the context pool is exhausted, independently of SQ capacity.
// Initialize its retained contents before use. Keep it until all request
// completions and borrowed access finish, then return it with PutIndirectSQE.
//
//go:nosplit
func (ur *Uring) IndirectSQE() *IndirectSQE {
	return ur.ctxPools.Indirect()
}

// PutIndirectSQE returns an IndirectSQE borrowed from this ring without clearing it.
// Call exactly once after all request completions and borrowed access finish.
// This includes terminal multishot and zero-copy notification CQEs when present.
// After return, the IndirectSQE and all views into it are invalid.
//
//go:nosplit
func (ur *Uring) PutIndirectSQE(sqe *IndirectSQE) {
	ur.ctxPools.PutIndirect(sqe)
}

// SubmitExtended submits an SQE using Extended mode context.
// Raw READ, WRITE, RECV, and SEND SQEs are unsupported on the Darwin simulator;
// use the typed operation methods so their payload remains GC-visible.
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
// fds must have between 1 and 2^32-1 entries.
// On Darwin, only Close consumes registered-file slots; other fixed-file
// submissions return ErrNotSupported.
func (ur *Uring) RegisterFiles(fds []int32) error {
	return ur.registerFiles(fds)
}

// RegisterFilesSparse allocates a sparse file table of the given size.
func (ur *Uring) RegisterFilesSparse(count uint32) error {
	return ur.registerFilesSparse(count)
}

// RegisterFilesUpdate updates registered files at the specified offset and
// returns the number of entries processed.
// fds must have between 1 and 2^32-1 entries. The update range must fit
// within the registered file table.
func (ur *Uring) RegisterFilesUpdate(offset uint32, fds []int32) (int, error) {
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
func (ur *Uring) RingFD() iofd.FD {
	return iofd.InvalidFD
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

// SocketRaw creates a socket using io_uring. WithFileIndex requests a direct
// descriptor, which the Darwin backend does not support.
func (ur *Uring) SocketRaw(sqeCtx SQEContext, domain, typ, proto int, options ...OpOptionFunc) error {
	flags, fileIndex := ur.socketOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	if fileIndex != nil {
		return ur.socketDirect(ctx, domain, typ, proto, *fileIndex)
	}
	return ur.socket(ctx, domain, typ, proto)
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
	ctx := sqeCtx.OrFlags(flags)
	return ur.bind(ctx, AddrToSockaddr(addr))
}

// Listen starts listening on a socket.
func (ur *Uring) Listen(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, backlog := ur.listenOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.listen(ctx, backlog)
}

// Accept accepts a new connection from a listener socket.
func (ur *Uring) Accept(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.accept(ctx, ioprio)
}

// SubmitAcceptMultishot submits the raw kernel multishot accept opcode.
// For the managed subscription helper, use [Uring.AcceptMultishot].
func (ur *Uring) SubmitAcceptMultishot(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, ioprio := ur.acceptOptions(options)
	ctx := sqeCtx.OrFlags(flags)
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
	ctx := sqeCtx.OrFlags(flags)
	return ur.connect(ctx, AddrToSockaddr(remote))
}

// Receive performs a socket receive operation.
// On Darwin, b must be non-nil because kernel buffer selection is unsupported.
// Caller must keep b valid until the operation completes.
// WithOffset without WithN receives into the remaining buffer range.
func (ur *Uring) Receive(sqeCtx SQEContext, pollFD PollFd, b []byte, options ...OpOptionFunc) error {
	return ur.ReceiveFD(sqeCtx, iofd.FD(pollFD.Fd()), b, options...)
}

// ReceiveFD performs a socket receive operation using an iofd.FD value instead
// of a PollFd interface. On Darwin, b must be non-nil because kernel buffer
// selection is unsupported, and IOSQE_FIXED_FILE is unsupported for receive.
// Caller must keep b valid until the operation completes.
func (ur *Uring) ReceiveFD(sqeCtx SQEContext, fd iofd.FD, b []byte, options ...OpOptionFunc) error {
	ctx := sqeCtx.WithFD(fd)
	if b == nil {
		return ErrNotSupported
	}
	if len(b) < 1 {
		return ErrInvalidParam
	}
	flags, ioprio, offset, n := ur.receiveOptions(b, options)
	if offset < 0 {
		return ErrInvalidParam
	}
	ctx = ctx.OrFlags(flags | ur.readLikeOpFlags)
	return ur.receive(ctx, ioprio, b, uint64(offset), n)
}

// SubmitReceiveMultishot submits the raw kernel multishot receive opcode.
// For the managed subscription helper, use [Uring.ReceiveMultishot].
// b must be nil because multishot receive requires kernel buffer selection.
// Darwin does not support that operation.
func (ur *Uring) SubmitReceiveMultishot(sqeCtx SQEContext, pollFD PollFd, b []byte, options ...OpOptionFunc) error {
	return ur.SubmitReceiveMultishotFD(sqeCtx, iofd.FD(pollFD.Fd()), b, options...)
}

// SubmitReceiveMultishotFD reports the Darwin multishot receive boundary for an
// iofd.FD value. b must be nil; Darwin does not support the kernel
// buffer-selection operation required by multishot receive.
func (ur *Uring) SubmitReceiveMultishotFD(sqeCtx SQEContext, fd iofd.FD, b []byte, options ...OpOptionFunc) error {
	if b != nil {
		return ErrInvalidParam
	}
	_ = sqeCtx
	_ = fd
	_ = options
	return ErrNotSupported
}

// Send writes data to a socket.
// Caller must keep p valid until the operation completes.
// WithOffset without WithN sends the remaining payload range.
func (ur *Uring) Send(sqeCtx SQEContext, pollFD PollFd, p []byte, options ...OpOptionFunc) error {
	return ur.SendFD(sqeCtx, iofd.FD(pollFD.Fd()), p, options...)
}

// SendFD writes data using an iofd.FD value instead of a PollFd interface. The
// fd value is encoded verbatim. On Darwin, IOSQE_FIXED_FILE is unsupported for
// send. Caller must keep p valid until the operation completes. WithOffset
// without WithN sends the remaining payload range.
func (ur *Uring) SendFD(sqeCtx SQEContext, fd iofd.FD, p []byte, options ...OpOptionFunc) error {
	flags, ioprio, offset, n := ur.sendOptions(p, options)
	if offset < 0 {
		return ErrInvalidParam
	}
	ctx := sqeCtx.WithFD(fd).OrFlags(flags | ur.writeLikeOpFlags)
	return ur.send(ctx, ioprio, p, uint64(offset), n)
}

// Timeout submits a timeout request.
func (ur *Uring) Timeout(sqeCtx SQEContext, d time.Duration, options ...OpOptionFunc) error {
	flags, cnt, timeoutFlags := ur.timeoutOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	nano := d.Nanoseconds()
	return ur.timeout(ctx, cnt, &Timespec{Sec: nano / int64(time.Second), Nsec: nano % int64(time.Second)}, int(timeoutFlags))
}

// Shutdown submits a socket shutdown for the directions selected by how.
// Use [SHUT_RD] for reads, [SHUT_WR] for writes, or [SHUT_RDWR] for both.
// The socket descriptor remains open.
func (ur *Uring) Shutdown(sqeCtx SQEContext, how int, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.shutdown(ctx, how)
}

// Nop submits a no-op request.
func (ur *Uring) Nop(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	return ur.nop(sqeCtx.OrFlags(flags))
}

// Close submits `IORING_OP_CLOSE` for the descriptor carried in sqeCtx.
// If sqeCtx carries IOSQE_FIXED_FILE, Close treats its FD field as a
// registered-file slot and emits close-direct semantics for that slot.
// It closes a target descriptor; it does not tear down the Uring instance.
func (ur *Uring) Close(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags := ur.closeOptions(options)
	return ur.close(sqeCtx.OrFlags(flags))
}

// Read performs a read operation.
// Caller must keep b valid until the operation completes.
// WithOffset(-1) uses and advances the file's current position.
func (ur *Uring) Read(sqeCtx SQEContext, b []byte, options ...OpOptionFunc) error {
	if len(b) < 1 {
		return ErrInvalidParam
	}
	flags, ioprio, offset, n := ur.readOptions(b, options)
	if offset < -1 {
		return ErrInvalidParam
	}
	ctx := sqeCtx.OrFlags(flags | ur.readLikeOpFlags)
	return ur.read(ctx, ioprio, b, uint64(offset), n)
}

// Write performs a write operation.
// Caller must keep b valid until the operation completes.
// WithOffset(-1) uses and advances the file's current position.
func (ur *Uring) Write(sqeCtx SQEContext, b []byte, options ...OpOptionFunc) error {
	if len(b) < 1 {
		return ErrInvalidParam
	}
	flags, ioprio, offset, n := ur.writeOptions(b, options)
	if offset < -1 {
		return ErrInvalidParam
	}
	ctx := sqeCtx.OrFlags(flags | ur.writeLikeOpFlags)
	return ur.write(ctx, ioprio, b, uint64(offset), n)
}

// Splice submits a transfer of up to n bytes between file descriptors.
// The Darwin backend completes this request with -ENOSYS.
func (ur *Uring) Splice(sqeCtx SQEContext, fdIn iofd.FD, n int, options ...OpOptionFunc) error {
	flags, _, spliceFlags := ur.spliceOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.splice(ctx, fdIn, nil, nil, n, spliceFlags)
}

// Tee submits a duplication of up to length bytes between pipes.
// The Darwin backend completes this request with -ENOSYS.
func (ur *Uring) Tee(sqeCtx SQEContext, fdIn iofd.FD, length int, options ...OpOptionFunc) error {
	flags, _, spliceFlags := ur.teeOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.tee(ctx, fdIn, length, spliceFlags)
}

// Sync performs a file sync operation.
func (ur *Uring) Sync(sqeCtx SQEContext, options ...OpOptionFunc) error {
	flags, fsyncFlags := ur.syncOptions(options)
	return ur.fsync(sqeCtx.OrFlags(flags), fsyncFlags)
}

// ReadV submits a vectored read with file offset zero.
// Only [OpOption.Flags] is used.
// The Darwin backend completes this request with -ENOSYS.
// Caller must keep iovs and their backing buffers valid until the operation completes.
func (ur *Uring) ReadV(sqeCtx SQEContext, iovs [][]byte, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags | ur.readLikeOpFlags)
	return ur.readv(ctx, iovs)
}

// WriteV submits a vectored write with file offset zero.
// Only [OpOption.Flags] is used.
// The Darwin backend completes this request with -ENOSYS.
// Caller must keep iovs and their backing buffers valid until the operation completes.
func (ur *Uring) WriteV(sqeCtx SQEContext, iovs [][]byte, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags | ur.writeLikeOpFlags)
	return ur.writev(ctx, iovs)
}

// ReadFixed performs a read with a registered buffer.
// WithOffset(-1) uses and advances the file's current position.
func (ur *Uring) ReadFixed(sqeCtx SQEContext, bufIndex int, options ...OpOptionFunc) ([]byte, error) {
	if bufIndex < 0 || bufIndex >= len(ur.ioUring.bufs) {
		return nil, ErrInvalidParam
	}
	flags, offset, n := ur.readFixedOptions(bufIndex, options)
	if offset < -1 {
		return nil, ErrInvalidParam
	}
	ctx := sqeCtx.OrFlags(flags | ur.readLikeOpFlags)
	return ur.readFixed(ctx, bufIndex, uint64(offset), n)
}

// WriteFixed submits a write of up to n bytes from a registered buffer.
// WithOffset(-1) uses and advances the file's current position.
func (ur *Uring) WriteFixed(sqeCtx SQEContext, bufIndex int, n int, options ...OpOptionFunc) error {
	flags, offset := ur.writeFixedOptions(options)
	if offset < -1 {
		return ErrInvalidParam
	}
	ctx := sqeCtx.OrFlags(flags | ur.writeLikeOpFlags)
	return ur.writeFixed(ctx, bufIndex, uint64(offset), n)
}

// OpenAt opens a file.
func (ur *Uring) OpenAt(sqeCtx SQEContext, pathname string, openFlags uint32, mode uint32, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.openAt(ctx, pathname, openFlags, mode)
}

// FileAdvise provides advice about file access patterns.
func (ur *Uring) FileAdvise(sqeCtx SQEContext, offset int64, length int, advice uint32, options ...OpOptionFunc) error {
	if offset < 0 {
		return ErrInvalidParam
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.fadvise(ctx, uint64(offset), length, advice)
}

// SendMsg sends a message with control data.
// Caller must keep buffers and oob valid until the operation completes.
func (ur *Uring) SendMsg(sqeCtx SQEContext, pollFD PollFd, buffers [][]byte, oob []byte, to Addr, options ...OpOptionFunc) error {
	flags, ioprio := ur.sendmsgOptions(options)
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd())).OrFlags(flags | ur.writeLikeOpFlags)
	var sa Sockaddr
	if to != nil {
		sa = AddrToSockaddr(to)
	}
	return ur.sendmsg(ctx, ioprio, buffers, oob, sa)
}

// RecvMsg receives a message through the caller-owned message header.
// Caller must keep msg and all memory it references valid until the operation
// completes.
func (ur *Uring) RecvMsg(sqeCtx SQEContext, pollFD PollFd, msg *Msghdr, options ...OpOptionFunc) error {
	flags, ioprio := ur.recvmsgOptions(options)
	ctx := sqeCtx.WithFD(iofd.FD(pollFD.Fd())).OrFlags(flags | ur.readLikeOpFlags)
	return ur.recvmsg(ctx, ioprio, msg)
}

// PollAdd adds a file descriptor to the poll set.
func (ur *Uring) PollAdd(sqeCtx SQEContext, events int, options ...OpOptionFunc) error {
	flags := ur.pollAddOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.pollAdd(ctx, 0, events)
}

// PollRemove removes a poll request matched by the original SQE user_data.
func (ur *Uring) PollRemove(sqeCtx SQEContext, targetUserData uint64, options ...OpOptionFunc) error {
	flags := ur.pollRemoveOptions(options)
	return ur.pollRemove(sqeCtx.OrFlags(flags), targetUserData)
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
	ctx := sqeCtx.OrFlags(flags)
	return ur.timeoutRemove(ctx, userData, 0)
}

// TimeoutUpdate modifies an existing timeout request in-place.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) TimeoutUpdate(sqeCtx SQEContext, userData uint64, d time.Duration, absolute bool, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// AsyncCancel cancels a pending async operation matched by the user_data value
// of the original SQE.
func (ur *Uring) AsyncCancel(sqeCtx SQEContext, targetUserData uint64, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
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

// AsyncCancelAny submits cancellation without filtering by request identity.
// It may cancel multiple pending operations. The cancellation CQE reports the
// matched count or a negative errno; each original operation has its own CQE.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) AsyncCancelAny(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// AsyncCancelAll cancels all pending operations.
// Darwin stub returns ErrNotSupported.
func (ur *Uring) AsyncCancelAll(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Fallocate allocates a positive-length file range.
func (ur *Uring) Fallocate(sqeCtx SQEContext, mode uint32, offset int64, length int64, options ...OpOptionFunc) error {
	if offset < 0 || length <= 0 {
		return ErrInvalidParam
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.fAllocate(ctx, mode, uint64(offset), length)
}

// SyncFileRange syncs a file range to storage.
func (ur *Uring) SyncFileRange(sqeCtx SQEContext, offset int64, length int, syncFlags uint32, options ...OpOptionFunc) error {
	if offset < 0 {
		return ErrInvalidParam
	}
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	return ur.syncFileRange(ctx, uint64(offset), length, syncFlags)
}

// LinkTimeout creates a linked timeout operation.
func (ur *Uring) LinkTimeout(sqeCtx SQEContext, d time.Duration, options ...OpOptionFunc) error {
	flags := ur.operationOptions(options)
	ctx := sqeCtx.OrFlags(flags)
	nano := d.Nanoseconds()
	return ur.linkTimeout(ctx, &Timespec{Sec: nano / int64(time.Second), Nsec: nano % int64(time.Second)}, 0)
}

// ReceiveBundle reports that bundled receive is unavailable on Darwin.
func (ur *Uring) ReceiveBundle(sqeCtx SQEContext, pollFD PollFd, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SubmitReceiveBundleMultishot reports that multishot bundled receive is unavailable on Darwin.
func (ur *Uring) SubmitReceiveBundleMultishot(sqeCtx SQEContext, pollFD PollFd, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// BundleIterator reports that bundled receive buffers are unavailable on Darwin.
func (ur *Uring) BundleIterator(cqe CQEView, group uint16) (BundleIterator, bool) {
	return BundleIterator{}, false
}

// ReceiveZeroCopy reports that zero-copy receive is unavailable on Darwin.
func (ur *Uring) ReceiveZeroCopy(sqeCtx SQEContext, pollFD PollFd, n int, zcrxIfqIdx uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// EpollWait reports that io_uring epoll wait is unavailable on Darwin.
func (ur *Uring) EpollWait(sqeCtx SQEContext, events []EpollEvent, timeout int32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// ReadvFixed reports that fixed-buffer vectored reads are unavailable on Darwin.
func (ur *Uring) ReadvFixed(sqeCtx SQEContext, offset int64, bufIndex int, iovs [][]byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// WritevFixed reports that fixed-buffer vectored writes are unavailable on Darwin.
func (ur *Uring) WritevFixed(sqeCtx SQEContext, offset int64, bufIndex int, iovs [][]byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Pipe reports that io_uring pipe creation is unavailable on Darwin.
func (ur *Uring) Pipe(sqeCtx SQEContext, fds *[2]iofd.FD, pipeFlags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// RegisterZCRXIfq reports that ZCRX interface queues are unavailable on Darwin.
func (ur *Uring) RegisterZCRXIfq(ifIdx, ifRxq uint32, rqEntries uint32, area *ZCRXAreaReg, region *RegionDesc, rxBufLen uint32) (ZCRXIfqRegistration, error) {
	return ZCRXIfqRegistration{}, ErrNotSupported
}

// ZCRXFlushRQ reports that ZCRX refill queues are unavailable on Darwin.
func (ur *Uring) ZCRXFlushRQ(zcrxID uint32) error {
	return ErrNotSupported
}

// RegisterBufRingMMAP reports that kernel-mapped provided-buffer rings are unavailable on Darwin.
func (ur *Uring) RegisterBufRingMMAP(entries int, groupID uint16) (*BufferRing, error) {
	return nil, ErrNotSupported
}

// RegisterBufRingIncremental reports that incremental provided-buffer rings are unavailable on Darwin.
func (ur *Uring) RegisterBufRingIncremental(entries int, groupID uint16) (*BufferRing, error) {
	return nil, ErrNotSupported
}

// RegisterBufRingWithFlags reports that explicit provided-buffer rings are unavailable on Darwin.
func (ur *Uring) RegisterBufRingWithFlags(entries int, groupID uint16, flags uint16) (*BufferRing, error) {
	return nil, ErrNotSupported
}

// QueryOpcodes reports that io_uring opcode queries are unavailable on Darwin.
func (ur *Uring) QueryOpcodes() (*QueryOpcode, error) {
	return nil, ErrNotSupported
}

// QueryZCRX reports that ZCRX capability queries are unavailable on Darwin.
func (ur *Uring) QueryZCRX() (*QueryZCRX, error) {
	return nil, ErrNotSupported
}

// QuerySCQ reports that io_uring shared-ring queries are unavailable on Darwin.
func (ur *Uring) QuerySCQ() (*QuerySCQ, error) {
	return nil, ErrNotSupported
}

// RegisterMemRegion reports that io_uring memory regions are unavailable on Darwin.
func (ur *Uring) RegisterMemRegion(region *RegionDesc, flags uint64) error {
	return ErrNotSupported
}

// RegisterNAPI reports that io_uring NAPI polling is unavailable on Darwin.
func (ur *Uring) RegisterNAPI(busyPollTimeout uint32, preferBusyPoll bool, strategy uint32) error {
	return ErrNotSupported
}

// NAPIAddStaticID reports that io_uring NAPI polling is unavailable on Darwin.
func (ur *Uring) NAPIAddStaticID(napiID uint32) error {
	return ErrNotSupported
}

// NAPIDelStaticID reports that io_uring NAPI polling is unavailable on Darwin.
func (ur *Uring) NAPIDelStaticID(napiID uint32) error {
	return ErrNotSupported
}

// UnregisterNAPI reports that io_uring NAPI polling is unavailable on Darwin.
func (ur *Uring) UnregisterNAPI() error {
	return ErrNotSupported
}

// CloneBuffers reports that cross-ring registered-buffer cloning is unavailable on Darwin.
func (ur *Uring) CloneBuffers(src *Uring, srcOff, dstOff, count uint32, replace bool) error {
	return ErrNotSupported
}

// CloneBuffersFromRegistered reports that registered-ring buffer cloning is unavailable on Darwin.
func (ur *Uring) CloneBuffersFromRegistered(src *Uring, srcRegisteredRingIdx uint32, srcOff, dstOff, count uint32, replace bool) error {
	return ErrNotSupported
}

// ResizeRings reports that io_uring ring resizing is unavailable on Darwin.
func (ur *Uring) ResizeRings(newSQSize, newCQSize uint32) error {
	return ErrNotSupported
}

// ZCRXExport reports that ZCRX export is unavailable on Darwin.
func (ur *Uring) ZCRXExport(zcrxID uint32) (iofd.FD, error) {
	return iofd.InvalidFD, ErrNotSupported
}

// FSetXattr reports that io_uring extended-attribute operations are unavailable on Darwin.
func (ur *Uring) FSetXattr(sqeCtx SQEContext, name string, value []byte, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SetXattr reports that io_uring extended-attribute operations are unavailable on Darwin.
func (ur *Uring) SetXattr(sqeCtx SQEContext, path, name string, value []byte, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// FGetXattr reports that io_uring extended-attribute operations are unavailable on Darwin.
func (ur *Uring) FGetXattr(sqeCtx SQEContext, name string, value []byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// GetXattr reports that io_uring extended-attribute operations are unavailable on Darwin.
func (ur *Uring) GetXattr(sqeCtx SQEContext, path, name string, value []byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Statx reports that io_uring statx is unavailable on Darwin.
func (ur *Uring) Statx(sqeCtx SQEContext, path string, flags, mask uint32, stat *Statx, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// RenameAt reports that io_uring path operations are unavailable on Darwin.
func (ur *Uring) RenameAt(sqeCtx SQEContext, oldPath, newPath string, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UnlinkAt reports that io_uring path operations are unavailable on Darwin.
func (ur *Uring) UnlinkAt(sqeCtx SQEContext, path string, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// MkdirAt reports that io_uring path operations are unavailable on Darwin.
func (ur *Uring) MkdirAt(sqeCtx SQEContext, path string, mode uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// SymlinkAt reports that io_uring path operations are unavailable on Darwin.
func (ur *Uring) SymlinkAt(sqeCtx SQEContext, target, linkpath string, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// LinkAt reports that io_uring path operations are unavailable on Darwin.
func (ur *Uring) LinkAt(sqeCtx SQEContext, oldDirfd iofd.FD, oldPath, newPath string, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// FTruncate reports that io_uring file truncation is unavailable on Darwin.
func (ur *Uring) FTruncate(sqeCtx SQEContext, length int64, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// FutexWait reports that io_uring futex operations are unavailable on Darwin.
func (ur *Uring) FutexWait(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// FutexWake reports that io_uring futex operations are unavailable on Darwin.
func (ur *Uring) FutexWake(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// FutexWaitV reports that io_uring futex operations are unavailable on Darwin.
func (ur *Uring) FutexWaitV(sqeCtx SQEContext, waitv unsafe.Pointer, count uint32, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Waitid reports that io_uring process waits are unavailable on Darwin.
func (ur *Uring) Waitid(sqeCtx SQEContext, idtype, id int, infop unsafe.Pointer, options int, opts ...OpOptionFunc) error {
	return ErrNotSupported
}

// FixedFdInstall reports that io_uring fixed-descriptor installation is unavailable on Darwin.
func (ur *Uring) FixedFdInstall(sqeCtx SQEContext, fixedIndex int, flags uint32, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// FilesUpdate reports that io_uring fixed-file updates are unavailable on Darwin.
func (ur *Uring) FilesUpdate(sqeCtx SQEContext, fds []int32, offset int, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UringCmd reports that io_uring passthrough commands are unavailable on Darwin.
func (ur *Uring) UringCmd(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UringCmdInline reports that io_uring inline passthrough commands are unavailable on Darwin.
func (ur *Uring) UringCmdInline(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// Nop128 reports that 128-byte SQEs are unavailable on Darwin.
func (ur *Uring) Nop128(sqeCtx SQEContext, options ...OpOptionFunc) error {
	return ErrNotSupported
}

// UringCmd128 reports that 128-byte SQEs are unavailable on Darwin.
func (ur *Uring) UringCmd128(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, options ...OpOptionFunc) error {
	return ErrNotSupported
}
