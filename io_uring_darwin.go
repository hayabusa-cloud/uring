// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"code.hybscloud.com/iox"
)

// Darwin io_uring simulator constants.
// These mirror Linux io_uring constants for API compatibility.
const (
	IORING_SETUP_IOPOLL             = 1 << 0
	IORING_SETUP_SQPOLL             = 1 << 1
	IORING_SETUP_SQ_AFF             = 1 << 2
	IORING_SETUP_CQSIZE             = 1 << 3
	IORING_SETUP_CLAMP              = 1 << 4
	IORING_SETUP_ATTACH_WQ          = 1 << 5
	IORING_SETUP_R_DISABLED         = 1 << 6
	IORING_SETUP_SUBMIT_ALL         = 1 << 7
	IORING_SETUP_COOP_TASKRUN       = 1 << 8
	IORING_SETUP_TASKRUN_FLAG       = 1 << 9
	IORING_SETUP_SQE128             = 1 << 10
	IORING_SETUP_CQE32              = 1 << 11
	IORING_SETUP_SINGLE_ISSUER      = 1 << 12
	IORING_SETUP_DEFER_TASKRUN      = 1 << 13
	IORING_SETUP_NO_MMAP            = 1 << 14
	IORING_SETUP_REGISTERED_FD_ONLY = 1 << 15
	IORING_SETUP_NO_SQARRAY         = 1 << 16
	IORING_SETUP_HYBRID_IOPOLL      = 1 << 17
)

const (
	IORING_FEAT_SINGLE_MMAP = 1 << iota
	IORING_FEAT_NODROP
	IORING_FEAT_SUBMIT_STABLE
	IORING_FEAT_RW_CUR_POS
	IORING_FEAT_CUR_PERSONALITY
	IORING_FEAT_FAST_POLL
	IORING_FEAT_POLL_32BITS
	IORING_FEAT_SQPOLL_NONFIXED
	IORING_FEAT_EXT_ARG
	IORING_FEAT_NATIVE_WORKERS
	IORING_FEAT_RSRC_TAGS
	IORING_FEAT_CQE_SKIP
	IORING_FEAT_LINKED_FILE
	IORING_FEAT_REG_REG_RING
	IORING_FEAT_RECVSEND_BUNDLE // Bundle send/recv (6.13+)
	IORING_FEAT_MIN_TIMEOUT     // Minimum timeout (6.13+)
	IORING_FEAT_RW_ATTR         // Read/write with attributes (6.14+)
	IORING_FEAT_NO_IOWAIT       // No I/O wait mode (6.14+)
)

const (
	IORING_ENTER_GETEVENTS       = 1 << 0
	IORING_ENTER_SQ_WAKEUP       = 1 << 1
	IORING_ENTER_SQ_WAIT         = 1 << 2
	IORING_ENTER_EXT_ARG         = 1 << 3
	IORING_ENTER_REGISTERED_RING = 1 << 4
	IORING_ENTER_ABS_TIMER       = 1 << 5 // Absolute timer (6.13+)
	IORING_ENTER_EXT_ARG_REG     = 1 << 6 // Extended arg registered (6.13+)
	IORING_ENTER_NO_IOWAIT       = 1 << 7 // No I/O wait (6.14+)
)

const (
	IORING_SQ_NEED_WAKEUP = 1 << iota
	IORING_SQ_CQ_OVERFLOW
	IORING_SQ_TASKRUN
)

const (
	IOSQE_FIXED_FILE = 1 << iota
	IOSQE_IO_DRAIN
	IOSQE_IO_LINK
	IOSQE_IO_HARDLINK
	IOSQE_ASYNC
	IOSQE_BUFFER_SELECT
	IOSQE_CQE_SKIP_SUCCESS
)

const (
	IORING_POLL_ADD_MULTI = 1 << iota
	IORING_POLL_UPDATE_EVENTS
	IORING_POLL_UPDATE_USER_DATA
	IORING_POLL_ADD_LEVEL
)

const (
	IORING_ASYNC_CANCEL_ALL = 1 << iota
	IORING_ASYNC_CANCEL_FD
	IORING_ASYNC_CANCEL_ANY
	IORING_ASYNC_CANCEL_FD_FIXED
	IORING_ASYNC_CANCEL_USERDATA
	IORING_ASYNC_CANCEL_OP
)

const (
	IORING_CQE_F_BUFFER = 1 << iota
	IORING_CQE_F_MORE
	IORING_CQE_F_SOCK_NONEMPTY
	IORING_CQE_F_NOTIF
	IORING_CQE_F_BUF_MORE // Buffer has more data (6.0+)
)

const (
	IORING_CQE_BUFFER_SHIFT = 16
)

// Darwin simulator does not use kernel registration.
const (
	IORING_REGISTER_BUFFERS uintptr = iota
	IORING_UNREGISTER_BUFFERS
	IORING_REGISTER_FILES
	IORING_UNREGISTER_FILES
	IORING_REGISTER_EVENTFD
	IORING_UNREGISTER_EVENTFD
	IORING_REGISTER_FILES_UPDATE
	IORING_REGISTER_EVENTFD_ASYNC
	IORING_REGISTER_PROBE
	IORING_REGISTER_PERSONALITY
	IORING_UNREGISTER_PERSONALITY
	IORING_REGISTER_RESTRICTIONS
	IORING_REGISTER_ENABLE_RINGS
	IORING_REGISTER_FILES2
	IORING_REGISTER_FILES_UPDATE2
	IORING_REGISTER_BUFFERS2
	IORING_REGISTER_BUFFERS_UPDATE
	IORING_REGISTER_IOWQ_AFF
	IORING_UNREGISTER_IOWQ_AFF
	IORING_REGISTER_IOWQ_MAX_WORKERS
	IORING_REGISTER_RING_FDS
	IORING_UNREGISTER_RING_FDS
	IORING_REGISTER_PBUF_RING
	IORING_UNREGISTER_PBUF_RING
	IORING_REGISTER_SYNC_CANCEL
	IORING_REGISTER_FILE_ALLOC_RANGE
	IORING_REGISTER_PBUF_STATUS
	IORING_REGISTER_NAPI
	IORING_UNREGISTER_NAPI
	IORING_REGISTER_CLOCK
	IORING_REGISTER_CLONE_BUFFERS
	IORING_REGISTER_SEND_MSG_RING
	IORING_REGISTER_ZCRX_IFQ
	IORING_REGISTER_RESIZE_RINGS
	IORING_REGISTER_MEM_REGION
	IORING_REGISTER_QUERY
	IORING_REGISTER_ZCRX_CTRL
)

// IORING_REGISTER_USE_REGISTERED_RING is a flag that can be OR'd with register
// opcodes to use a registered ring fd instead of a regular fd.
const IORING_REGISTER_USE_REGISTERED_RING uintptr = 1 << 31

// Resource registration flags.
const (
	IORING_RSRC_REGISTER_SPARSE = 1 << 0 // Sparse registration (5.19+)
)

// IORING_FILE_INDEX_ALLOC is passed as file_index to have io_uring allocate
// a free direct descriptor slot. The allocated index is returned in cqe->res.
// Returns -ENFILE if no free slots available.
// Available since kernel 5.15.
const IORING_FILE_INDEX_ALLOC uint32 = 0xFFFFFFFF

// IORING_FIXED_FD_NO_CLOEXEC omits O_CLOEXEC when installing a fixed fd.
// By default, FixedFdInstall sets O_CLOEXEC on the new regular fd.
// Available since kernel 6.8.
const IORING_FIXED_FD_NO_CLOEXEC uint32 = 1 << 0

// Futex2 flags for FutexWait/FutexWake operations (kernel 6.7+).
const (
	FUTEX2_SIZE_U8  uint32 = 0x00
	FUTEX2_SIZE_U16 uint32 = 0x01
	FUTEX2_SIZE_U32 uint32 = 0x02
	FUTEX2_SIZE_U64 uint32 = 0x03
	FUTEX2_NUMA     uint32 = 0x04
	FUTEX2_PRIVATE  uint32 = 128
)

const FUTEX_BITSET_MATCH_ANY uint64 = 0xFFFFFFFF

// MSG_RING command types for the addr field (kernel 5.18+).
const (
	IORING_MSG_DATA    uint64 = 0
	IORING_MSG_SEND_FD uint64 = 1
)

// MSG_RING flags for MsgRing operations (kernel 5.18+).
const (
	IORING_MSG_RING_CQE_SKIP   uint32 = 1 << 0
	IORING_MSG_RING_FLAGS_PASS uint32 = 1 << 1
)

const (
	IO_URING_OP_SUPPORTED = 1 << 0
)

// ioUringFd represents a file descriptor in io_uring context.
type ioUringFd int32

// ioUringSqe represents a simulated submission queue entry.
type ioUringSqe struct {
	opcode   uint8
	flags    uint8
	ioprio   uint16
	fd       int32
	off      uint64
	addr     uint64
	len      uint32
	uflags   uint32
	userData uint64

	bufIndex    uint16
	personality uint16
	spliceFdIn  int32
	pad         [2]uint64
}

// ioUringCqe represents a simulated completion queue entry.
type ioUringCqe struct {
	userData uint64
	res      int32
	flags    uint32
}

// Compile-time size assertions to match Linux kernel structures.
var _ [64 - unsafe.Sizeof(ioUringSqe{})]struct{} // SQE must be 64 bytes
var _ [16 - unsafe.Sizeof(ioUringCqe{})]struct{} // CQE must be 16 bytes

// Context returns the packed SQEContext from the CQE's user_data field.
func (cqe *ioUringCqe) Context() SQEContext {
	return SQEContextFromRaw(cqe.userData)
}

// ioUringParams represents simulated io_uring parameters.
type ioUringParams struct {
	sqEntries    uint32
	cqEntries    uint32
	flags        uint32
	sqThreadCPU  uint32
	sqThreadIdle uint32
	features     uint32
	wqFd         uint32
	resv         [3]uint32
	sqOff        ioSqRingOffsets
	cqOff        ioCqRingOffsets
}

type ioSqRingOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	flags       uint32
	dropped     uint32
	array       uint32
	resv        [3]uint32
}

type ioCqRingOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	overflow    uint32
	cqes        uint32
	flags       uint32
	resv        [3]uint32
}

// ioUring is the Darwin io_uring simulator.
// It emulates io_uring behavior using goroutines and channels.
type ioUring struct {
	_ noCopy

	mu       sync.Mutex
	params   *ioUringParams
	features uint32

	// Simulated SQ using a buffered channel
	sqChan chan *ioUringSqe
	sqHead uint32
	sqTail uint32

	// Simulated CQ using a buffered channel
	cqChan chan *ioUringCqe
	cqHead uint32
	cqTail uint32
	cqMask uint32

	// Registered buffers
	bufs [][]byte

	// Registered files
	files []int32

	// Worker management
	workerWg sync.WaitGroup
	stopChan chan struct{}
	enabled  atomic.Bool

	ops []ioUringProbeOp
}

type ioUringProbe struct {
	lastOp uint8
	opsLen uint8
	resv   uint16
	resv2  [3]uint32
	ops    [256]ioUringProbeOp
}

type ioUringProbeOp struct {
	op    uint8
	resv  uint8
	flags uint16
	resv2 uint32
}

// Buffer ring types for compatibility
type ioUringBufRing struct {
	tail uint16
}

type ioUringBuf struct {
	addr uint64
	len  uint32
	bid  uint16
	tail uint16
}

var ioUringBufSize = unsafe.Sizeof(ioUringBuf{})

type ioUringRSrcRegister struct {
	nr    uint32
	resv  uint32
	resv2 uint64
	data  uint64
	tags  uint64
}

type ioUringBufReg struct {
	ringAddr    uint64
	ringEntries uint32
	bgid        uint16
	pad         uint16
	_           [3]uint64
}

type ioUringBufStatus struct {
	bufGroup uint32
	head     uint32
	_        [8]uint32
}

var (
	ioUringDefaultParams   = &ioUringParams{}
	ioUringDisabledOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_R_DISABLED
	}
	ioUringNoSQArrayOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_NO_SQARRAY
	}
	ioUringIoPollOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_IOPOLL
	}
	ioUringHybridIoPollOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_IOPOLL | IORING_SETUP_HYBRID_IOPOLL
	}
	ioUringSqPollOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_SQPOLL | IORING_SETUP_SQ_AFF
	}
)

// newIoUring creates a new Darwin io_uring simulator.
func newIoUring(entries int, opts ...func(params *ioUringParams)) (*ioUring, error) {
	if entries < 1 {
		return nil, ErrInvalidParam
	}

	// Round up to power of 2
	entries--
	entries |= entries >> 1
	entries |= entries >> 2
	entries |= entries >> 4
	entries |= entries >> 8
	entries++

	params := new(ioUringParams)
	*params = *ioUringDefaultParams
	params.sqEntries = uint32(entries)
	params.cqEntries = uint32(entries * 2)
	// Simulate all features being available
	params.features = IORING_FEAT_SINGLE_MMAP | IORING_FEAT_NODROP |
		IORING_FEAT_SUBMIT_STABLE | IORING_FEAT_RW_CUR_POS |
		IORING_FEAT_CUR_PERSONALITY | IORING_FEAT_FAST_POLL |
		IORING_FEAT_POLL_32BITS | IORING_FEAT_SQPOLL_NONFIXED |
		IORING_FEAT_EXT_ARG | IORING_FEAT_NATIVE_WORKERS |
		IORING_FEAT_RSRC_TAGS | IORING_FEAT_CQE_SKIP |
		IORING_FEAT_LINKED_FILE | IORING_FEAT_REG_REG_RING

	for _, opt := range opts {
		opt(params)
	}

	ur := &ioUring{
		params:   params,
		features: params.features,
		sqChan:   make(chan *ioUringSqe, entries),
		cqChan:   make(chan *ioUringCqe, entries*2),
		cqMask:   uint32(entries*2 - 1),
		bufs:     [][]byte{},
		stopChan: make(chan struct{}),
	}

	return ur, nil
}

// registerProbe simulates probe registration (all ops supported on Darwin).
func (ur *ioUring) registerProbe(probe *ioUringProbe) error {
	// Darwin simulator supports all ops
	ur.ops = make([]ioUringProbeOp, 0, IORING_OP_LAST)
	for i := uint8(0); i < IORING_OP_LAST; i++ {
		ur.ops = append(ur.ops, ioUringProbeOp{
			op:    i,
			flags: IO_URING_OP_SUPPORTED,
		})
	}
	return nil
}

// registerBuffers registers buffers for fixed I/O.
func (ur *ioUring) registerBuffers(addr unsafe.Pointer, n, size int) error {
	if ur.bufs != nil && len(ur.bufs) > 0 {
		panic("io-uring buffers already registered")
	}
	if n < 1 || size < 1 {
		return ErrInvalidParam
	}
	ur.bufs = make([][]byte, 0, n)
	for i := range n {
		base := unsafe.Add(addr, i*size)
		ur.bufs = append(ur.bufs, unsafe.Slice((*byte)(base), size))
	}
	return nil
}

// unregisterBuffers unregisters fixed buffers.
func (ur *ioUring) unregisterBuffers() error {
	if ur.bufs == nil || len(ur.bufs) < 1 {
		panic("no io-uring buffers registered")
	}
	ur.bufs = [][]byte{}
	return nil
}

// registerFiles simulates file registration (darwin stub).
func (ur *ioUring) registerFiles(fds []int32) error {
	if ur.files != nil && len(ur.files) > 0 {
		return ErrExists
	}
	if len(fds) < 1 {
		return ErrInvalidParam
	}
	ur.files = make([]int32, len(fds))
	copy(ur.files, fds)
	return nil
}

// registerFilesSparse simulates sparse file registration (darwin stub).
func (ur *ioUring) registerFilesSparse(count uint32) error {
	if ur.files != nil && len(ur.files) > 0 {
		return ErrExists
	}
	if count < 1 {
		return ErrInvalidParam
	}
	ur.files = make([]int32, count)
	for i := range ur.files {
		ur.files[i] = -1
	}
	return nil
}

// registerFilesUpdate simulates file update (darwin stub).
func (ur *ioUring) registerFilesUpdate(offset uint32, fds []int32) error {
	if ur.files == nil || len(ur.files) < 1 {
		return ErrNotFound
	}
	if int(offset)+len(fds) > len(ur.files) {
		return ErrInvalidParam
	}
	copy(ur.files[offset:], fds)
	return nil
}

// unregisterFiles simulates file unregistration (darwin stub).
func (ur *ioUring) unregisterFiles() error {
	if ur.files == nil || len(ur.files) < 1 {
		return ErrNotFound
	}
	ur.files = nil
	return nil
}

// registerBufRing simulates buffer ring registration.
func (ur *ioUring) registerBufRing(entries int, groupID uint16) (*ioUringBufRing, []byte, error) {
	// Darwin simulator doesn't use kernel buffer rings
	return &ioUringBufRing{}, nil, nil
}

// registerBufRingWithFlags simulates buffer ring registration with flags.
func (ur *ioUring) registerBufRingWithFlags(entries int, groupID uint16, flags uint16) (*ioUringBufRing, []byte, error) {
	// Darwin simulator doesn't use kernel buffer rings
	return &ioUringBufRing{}, nil, nil
}

// unregisterBufRing simulates buffer ring unregistration.
func (ur *ioUring) unregisterBufRing(groupID uint16) error {
	return nil
}

// registerPoller is a no-op on Darwin (no kernel event fd).
func (ur *ioUring) registerPoller(p poller) (int, error) {
	return 0, ErrNotSupported
}

// feature checks if a feature is supported.
func (ur *ioUring) feature(feat uint32) bool {
	return feat == ur.features&feat
}

// enable enables the ring and starts worker goroutines.
func (ur *ioUring) enable() error {
	if ur.enabled.Load() {
		return nil
	}
	ur.enabled.Store(true)

	// Start worker goroutines to process SQEs
	numWorkers := 4
	for range numWorkers {
		ur.workerWg.Add(1)
		go ur.worker()
	}
	return nil
}

// worker processes SQEs and produces CQEs.
func (ur *ioUring) worker() {
	defer ur.workerWg.Done()
	for {
		select {
		case <-ur.stopChan:
			return
		case sqe := <-ur.sqChan:
			if sqe == nil {
				continue
			}
			cqe := ur.executeSQE(sqe)
			if cqe != nil {
				select {
				case ur.cqChan <- cqe:
				default:
					// CQ overflow - drop oldest
				}
			}
		}
	}
}

// executeSQE executes a submission queue entry and returns a completion.
func (ur *ioUring) executeSQE(sqe *ioUringSqe) *ioUringCqe {
	cqe := &ioUringCqe{
		userData: sqe.userData,
		flags:    0,
	}

	switch sqe.opcode {
	case IORING_OP_NOP:
		cqe.res = 0
	case IORING_OP_READ:
		cqe.res = ur.doRead(sqe)
	case IORING_OP_WRITE:
		cqe.res = ur.doWrite(sqe)
	case IORING_OP_RECV:
		cqe.res = ur.doRecv(sqe)
	case IORING_OP_SEND:
		cqe.res = ur.doSend(sqe)
	case IORING_OP_SOCKET:
		cqe.res = ur.doSocket(sqe)
	case IORING_OP_ACCEPT:
		cqe.res = ur.doAccept(sqe)
	case IORING_OP_CONNECT:
		cqe.res = ur.doConnect(sqe)
	case IORING_OP_CLOSE:
		cqe.res = ur.doClose(sqe)
	case IORING_OP_SHUTDOWN:
		cqe.res = ur.doShutdown(sqe)
	case IORING_OP_BIND:
		cqe.res = ur.doBind(sqe)
	case IORING_OP_LISTEN:
		cqe.res = ur.doListen(sqe)
	default:
		cqe.res = -int32(ENOSYS)
	}

	// Check for CQE skip flag
	if sqe.flags&IOSQE_CQE_SKIP_SUCCESS != 0 && cqe.res >= 0 {
		return nil
	}

	return cqe
}

// submitPacked submits an SQE with packed context.
func (ur *ioUring) submitPacked(sqeCtx SQEContext, fn func(e *ioUringSqe)) error {
	sqe := &ioUringSqe{}
	fn(sqe)
	sqe.userData = sqeCtx.Raw()

	select {
	case ur.sqChan <- sqe:
		return nil
	default:
		return iox.ErrWouldBlock
	}
}

// submitPacked3 submits a 3-argument operation.
func (ur *ioUring) submitPacked3(sqeCtx SQEContext, ioprio uint16, addr uint64, n int) error {
	return ur.submitPacked(sqeCtx, func(e *ioUringSqe) {
		e.opcode = sqeCtx.Op()
		e.flags = sqeCtx.Flags()
		e.ioprio = ioprio
		e.fd = sqeCtx.FD()
		e.addr = addr
		e.len = uint32(n)
	})
}

// submitPacked6 submits a 6-argument operation.
func (ur *ioUring) submitPacked6(sqeCtx SQEContext, ioprio uint16, off uint64, addr uint64, n int, uflags uint32) error {
	return ur.submitPacked(sqeCtx, func(e *ioUringSqe) {
		e.opcode = sqeCtx.Op()
		e.flags = sqeCtx.Flags()
		e.ioprio = ioprio
		e.fd = sqeCtx.FD()
		e.off = off
		e.addr = addr
		e.len = uint32(n)
		e.uflags = uflags
	})
}

// submitPacked9 submits a 9-argument operation.
func (ur *ioUring) submitPacked9(sqeCtx SQEContext, ioprio uint16, off uint64, addr uint64, n int, uflags uint32, personality uint16, spliceFdIn int32) error {
	return ur.submitPacked(sqeCtx, func(e *ioUringSqe) {
		e.opcode = sqeCtx.Op()
		e.flags = sqeCtx.Flags()
		e.ioprio = ioprio
		e.fd = sqeCtx.FD()
		e.off = off
		e.addr = addr
		e.len = uint32(n)
		e.uflags = uflags
		e.bufIndex = sqeCtx.BufGroup()
		e.personality = personality
		e.spliceFdIn = spliceFdIn
	})
}

// submitExtended submits an SQE using Extended mode context.
func (ur *ioUring) submitExtended(sqeCtx SQEContext) error {
	ext := sqeCtx.ExtSQE()
	return ur.submitPacked(sqeCtx, func(e *ioUringSqe) {
		*e = ext.SQE
	})
}

// enter processes pending SQEs (no-op on Darwin, workers handle it).
func (ur *ioUring) enter() error {
	return nil
}

// poll is a no-op on Darwin simulator.
func (ur *ioUring) poll(n int) error {
	return nil
}

// wait waits for a completion queue entry.
func (ur *ioUring) wait() (*ioUringCqe, error) {
	select {
	case cqe := <-ur.cqChan:
		if cqe != nil {
			return cqe, nil
		}
	default:
	}
	return nil, iox.ErrWouldBlock
}

// waitBatch retrieves multiple CQEs from the channel.
func (ur *ioUring) waitBatch(cqes []CQEView) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	n := 0
	for n < len(cqes) {
		select {
		case cqe := <-ur.cqChan:
			if cqe != nil {
				cqes[n] = CQEView{
					Res:   cqe.res,
					Flags: cqe.flags,
					ctx:   SQEContextFromRaw(cqe.userData),
				}
				n++
			}
		default:
			if n > 0 {
				return n, nil
			}
			return 0, iox.ErrWouldBlock
		}
	}
	return n, nil
}

// cqAdvance is a no-op on Darwin (channel-based CQ).
func (ur *ioUring) cqAdvance(nr uint32) {}

// sqCount returns the number of pending SQEs.
func (ur *ioUring) sqCount() int {
	return len(ur.sqChan)
}

// Buffer ring methods (minimal implementation for compatibility)
func (ur *ioUring) bufRingInit(br *ioUringBufRing) {
	br.tail = 0
}

func (ur *ioUring) bufRingAdd(br *ioUringBufRing, addr uintptr, n int, bid uint16, mask, offset uintptr) {
}

func (ur *ioUring) bufRingAdvance(br *ioUringBufRing, count int) {
	br.tail += uint16(count)
}

func (ur *ioUring) bufRingAvailable(br *ioUringBufRing, bgid uint16) int {
	return 0
}

func (ur *ioUring) bufRingHead(groupID uint16, head *uint16) int {
	return 0
}

func (ur *ioUring) bufRingCQAdvance(br *ioUringBufRing, count int) {
	ur.bufRingAdvance(br, count)
}

// I/O operation implementations
func (ur *ioUring) doRead(sqe *ioUringSqe) int32 {
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(sqe.addr))), sqe.len)
	n, err := syscall.Read(int(sqe.fd), buf)
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
}

func (ur *ioUring) doWrite(sqe *ioUringSqe) int32 {
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(sqe.addr))), sqe.len)
	n, err := syscall.Write(int(sqe.fd), buf)
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
}

func (ur *ioUring) doRecv(sqe *ioUringSqe) int32 {
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(sqe.addr))), sqe.len)
	n, _, err := syscall.Recvfrom(int(sqe.fd), buf, int(sqe.uflags))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
}

func (ur *ioUring) doSend(sqe *ioUringSqe) int32 {
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(sqe.addr))), sqe.len)
	err := syscall.Sendto(int(sqe.fd), buf, int(sqe.uflags), nil)
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(len(buf))
}

func (ur *ioUring) doSocket(sqe *ioUringSqe) int32 {
	domain := int(sqe.fd)
	typ := int(sqe.off & 0xFFFFFFFF)
	proto := int(sqe.off >> 32)
	fd, err := syscall.Socket(domain, typ, proto)
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(fd)
}

func (ur *ioUring) doAccept(sqe *ioUringSqe) int32 {
	fd, _, err := syscall.Accept(int(sqe.fd))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(fd)
}

func (ur *ioUring) doConnect(sqe *ioUringSqe) int32 {
	// Darwin doesn't support raw pointer connect, return stub error
	return -int32(syscall.ENOSYS)
}

func (ur *ioUring) doClose(sqe *ioUringSqe) int32 {
	err := syscall.Close(int(sqe.fd))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return 0
}

func (ur *ioUring) doShutdown(sqe *ioUringSqe) int32 {
	err := syscall.Shutdown(int(sqe.fd), int(sqe.len))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return 0
}

func (ur *ioUring) doBind(sqe *ioUringSqe) int32 {
	// Darwin doesn't support raw pointer bind, return stub error
	return -int32(syscall.ENOSYS)
}

func (ur *ioUring) doListen(sqe *ioUringSqe) int32 {
	err := syscall.Listen(int(sqe.fd), int(sqe.len))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return 0
}
