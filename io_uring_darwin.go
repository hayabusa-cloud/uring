// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"math"
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
	IORING_SETUP_CQE_MIXED          = 1 << 18
	IORING_SETUP_SQE_MIXED          = 1 << 19
)

const (
	IORING_ENTER_GETEVENTS       = 1 << 0
	IORING_ENTER_SQ_WAKEUP       = 1 << 1
	IORING_ENTER_SQ_WAIT         = 1 << 2
	IORING_ENTER_EXT_ARG         = 1 << 3
	IORING_ENTER_REGISTERED_RING = 1 << 4
	IORING_ENTER_ABS_TIMER       = 1 << 5 // Absolute timer
	IORING_ENTER_EXT_ARG_REG     = 1 << 6 // Extended arg registered
	IORING_ENTER_NO_IOWAIT       = 1 << 7 // Skip I/O-wait accounting; may still block
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
	IORING_CQE_F_BUF_MORE // Buffer has more data
	IORING_CQE_F_SKIP     // Skip CQE gap filler
)

const (
	IORING_CQE_BUFFER_SHIFT = 16
)

// Darwin simulator does not use kernel registration.
const (
	IORING_REGISTER_BUFFERS = iota
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
const IORING_REGISTER_USE_REGISTERED_RING = 1 << 31

// Resource registration flags.
const (
	IORING_RSRC_REGISTER_SPARSE = 1 << 0 // Sparse registration
)

// IORING_FILE_INDEX_ALLOC is passed as file_index to have io_uring allocate
// a free direct descriptor slot. The allocated index is returned in cqe->res.
// Returns -ENFILE if no free slots available.
const IORING_FILE_INDEX_ALLOC uint32 = 0xFFFFFFFF

// IORING_FIXED_FD_NO_CLOEXEC omits O_CLOEXEC when installing a fixed fd.
// By default, FixedFdInstall sets O_CLOEXEC on the new regular fd.
const IORING_FIXED_FD_NO_CLOEXEC uint32 = 1 << 0

// Futex2 flags for FutexWait/FutexWake operations.
const (
	FUTEX2_SIZE_U8  uint32 = 0x00
	FUTEX2_SIZE_U16 uint32 = 0x01
	FUTEX2_SIZE_U32 uint32 = 0x02
	FUTEX2_SIZE_U64 uint32 = 0x03
	FUTEX2_NUMA     uint32 = 0x04
	FUTEX2_PRIVATE  uint32 = 128
)

// FUTEX_BITSET_MATCH_ANY matches any waker when used as mask in FutexWait.
const FUTEX_BITSET_MATCH_ANY uint64 = 0xFFFFFFFF

// MSG_RING command types for the addr field.
const (
	IORING_MSG_DATA    uint64 = 0
	IORING_MSG_SEND_FD uint64 = 1
)

// MSG_RING flags for MsgRing operations.
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

	// lifecycleMu serializes stopChan handoff and close so concurrent Stop calls
	// cannot both observe the same channel and close it twice.
	lifecycleMu sync.Mutex
	params      *ioUringParams

	// Simulated SQ using a buffered channel
	sqChan chan *darwinSubmission
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
	started  atomic.Bool
	closed   atomic.Bool

	ops []ioUringProbeOp
}

type darwinSubmission struct {
	sqe     ioUringSqe
	payload []byte
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
	ioUringSQE128Options = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_SQE128
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

func sqeBytesFromFlags(flags uint32) int {
	if flags&IORING_SETUP_SQE128 != 0 {
		return 128
	}
	return 64
}

// newIoUring creates a new Darwin io_uring simulator.
func newIoUring(entries int, opts ...func(params *ioUringParams)) (*ioUring, error) {
	if entries < 1 || entries > EntriesHuge {
		return nil, ErrInvalidParam
	}

	entries = roundToPowerOf2(entries)

	params := new(ioUringParams)
	*params = *ioUringDefaultParams
	params.sqEntries = uint32(entries)
	params.cqEntries = uint32(entries * 2)

	for _, opt := range opts {
		opt(params)
	}

	ur := &ioUring{
		params:   params,
		sqChan:   make(chan *darwinSubmission, entries),
		cqChan:   make(chan *ioUringCqe, entries*2),
		cqMask:   uint32(entries*2 - 1),
		bufs:     [][]byte{},
		stopChan: make(chan struct{}),
	}

	return ur, nil
}

func (ur *ioUring) registerProbe(probe *ioUringProbe) error {
	// Darwin simulator supports all ops
	ur.ops = make([]ioUringProbeOp, 0, ioUringOpLast)
	for i := uint8(0); i < ioUringOpLast; i++ {
		ur.ops = append(ur.ops, ioUringProbeOp{
			op:    i,
			flags: IO_URING_OP_SUPPORTED,
		})
	}
	return nil
}

func (ur *ioUring) feature(feat uint32) bool {
	return feat == ur.params.features&feat
}

// registerBuffers registers buffers for fixed I/O.
func (ur *ioUring) registerBuffers(addr unsafe.Pointer, n, size int) error {
	if ur.bufs != nil && len(ur.bufs) > 0 {
		panic("uring: io-uring buffers already registered")
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
		panic("uring: no io-uring buffers registered")
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

func (ur *ioUring) stop() error {
	if ur.closed.Load() {
		return nil
	}

	ur.lifecycleMu.Lock()
	if ur.closed.Load() {
		ur.lifecycleMu.Unlock()
		return nil
	}
	ur.closed.Store(true)

	stopChan := ur.stopChan
	ur.stopChan = nil
	waitWorkers := ur.enabled.Swap(false)
	ur.lifecycleMu.Unlock()

	if stopChan != nil {
		close(stopChan)
	}
	if waitWorkers {
		ur.workerWg.Wait()
	}

	ur.lifecycleMu.Lock()
	ur.bufs = nil
	ur.files = nil
	ur.ops = nil
	ur.lifecycleMu.Unlock()
	return nil
}

// feature checks if a feature is supported.
// enable enables the ring and starts worker goroutines.
func (ur *ioUring) enable() error {
	if ur.enabled.Load() {
		return nil
	}
	ur.enabled.Store(true)
	stopChan := ur.stopChan

	// Start worker goroutines to process SQEs
	numWorkers := 4
	for range numWorkers {
		ur.workerWg.Add(1)
		go ur.worker(stopChan)
	}
	return nil
}

// worker processes SQEs and produces CQEs.
func (ur *ioUring) worker(stopChan <-chan struct{}) {
	defer ur.workerWg.Done()
	for {
		select {
		case <-stopChan:
			return
		case submission := <-ur.sqChan:
			if submission == nil {
				continue
			}
			cqe := ur.executeSQE(submission)
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
func (ur *ioUring) executeSQE(submission *darwinSubmission) *ioUringCqe {
	sqe := &submission.sqe
	cqe := &ioUringCqe{
		userData: sqe.userData,
		flags:    0,
	}

	switch sqe.opcode {
	case IORING_OP_NOP:
		cqe.res = 0
	case IORING_OP_READ, IORING_OP_READ_FIXED:
		cqe.res = ur.doRead(sqe, submission.payload)
	case IORING_OP_WRITE, IORING_OP_WRITE_FIXED:
		cqe.res = ur.doWrite(sqe, submission.payload)
	case IORING_OP_RECV:
		cqe.res = ur.doRecv(sqe, submission.payload)
	case IORING_OP_SEND:
		cqe.res = ur.doSend(sqe, submission.payload)
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
	return ur.submitPackedWithPayload(sqeCtx, nil, fn)
}

func (ur *ioUring) submitPackedWithPayload(sqeCtx SQEContext, payload []byte, fn func(e *ioUringSqe)) error {
	submission := &darwinSubmission{payload: payload}
	fn(&submission.sqe)
	if submission.sqe.flags&IOSQE_FIXED_FILE != 0 {
		return ErrNotSupported
	}
	submission.sqe.userData = sqeCtx.Raw()
	// Retain the submitted fields before the consumer can execute the SQE.
	switch sqeCtx.Mode() {
	case CtxModeIndirect:
		sqeCtx.IndirectSQE().ioUringSqe = submission.sqe
	case CtxModeExtended:
		sqeCtx.ExtSQE().SQE = submission.sqe
	}

	select {
	case ur.sqChan <- submission:
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
	return ur.submitPacked6WithPayload(sqeCtx, nil, ioprio, off, addr, n, uflags)
}

func (ur *ioUring) submitPacked6WithPayload(sqeCtx SQEContext, payload []byte, ioprio uint16, off uint64, addr uint64, n int, uflags uint32) error {
	return ur.submitPackedWithPayload(sqeCtx, payload, func(e *ioUringSqe) {
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
	return ur.submitPacked9WithPayload(sqeCtx, nil, ioprio, off, addr, n, uflags, personality, spliceFdIn)
}

func (ur *ioUring) submitPacked9WithPayload(sqeCtx SQEContext, payload []byte, ioprio uint16, off uint64, addr uint64, n int, uflags uint32, personality uint16, spliceFdIn int32) error {
	return ur.submitPackedWithPayload(sqeCtx, payload, func(e *ioUringSqe) {
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
	switch ext.SQE.opcode {
	case IORING_OP_READ, IORING_OP_WRITE, IORING_OP_RECV, IORING_OP_SEND:
		return ErrNotSupported
	}
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
func (ur *ioUring) doRead(sqe *ioUringSqe, buf []byte) int32 {
	var n int
	var err error
	switch {
	case sqe.off == math.MaxUint64:
		n, err = syscall.Read(int(sqe.fd), buf)
	case sqe.off <= math.MaxInt64:
		n, err = syscall.Pread(int(sqe.fd), buf, int64(sqe.off))
		if err == syscall.ESPIPE && sqe.off == 0 {
			n, err = syscall.Read(int(sqe.fd), buf)
		}
	default:
		return -int32(syscall.EINVAL)
	}
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
}

func (ur *ioUring) doWrite(sqe *ioUringSqe, buf []byte) int32 {
	var n int
	var err error
	switch {
	case sqe.off == math.MaxUint64:
		n, err = syscall.Write(int(sqe.fd), buf)
	case sqe.off <= math.MaxInt64:
		n, err = syscall.Pwrite(int(sqe.fd), buf, int64(sqe.off))
		if err == syscall.ESPIPE && sqe.off == 0 {
			n, err = syscall.Write(int(sqe.fd), buf)
		}
	default:
		return -int32(syscall.EINVAL)
	}
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
}

func (ur *ioUring) doRecv(sqe *ioUringSqe, buf []byte) int32 {
	n, _, err := syscall.Recvfrom(int(sqe.fd), buf, int(sqe.uflags))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
}

func (ur *ioUring) doSend(sqe *ioUringSqe, buf []byte) int32 {
	n, err := syscall.SendmsgN(int(sqe.fd), buf, nil, nil, int(sqe.uflags))
	if err != nil {
		return -int32(err.(syscall.Errno))
	}
	return int32(n)
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

func decodeCloseFileIndex(encoded int32) uint32 {
	return uint32(encoded) - 1
}

func (ur *ioUring) doClose(sqe *ioUringSqe) int32 {
	fd := sqe.fd
	if sqe.spliceFdIn != 0 {
		fileIndex := decodeCloseFileIndex(sqe.spliceFdIn)
		if uint64(fileIndex) >= uint64(len(ur.files)) {
			return -int32(syscall.EBADF)
		}
		fd = ur.files[fileIndex]
		if fd < 0 {
			return -int32(syscall.EBADF)
		}
		err := syscall.Close(int(fd))
		if err != nil {
			return -int32(err.(syscall.Errno))
		}
		ur.files[fileIndex] = -1
		return 0
	}
	err := syscall.Close(int(fd))
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

const (
	IORING_OFF_SQ_RING    int64 = 0
	IORING_OFF_CQ_RING    int64 = 0x8000000
	IORING_OFF_SQES       int64 = 0x10000000
	IORING_OFF_PBUF_RING        = 0x80000000
	IORING_OFF_PBUF_SHIFT       = 16
	IORING_OFF_MMAP_MASK        = 0xf8000000
)

const IORING_CQE_F_32 = 1 << 15

// Provided-buffer ring registration flags.
const (
	IOU_PBUF_RING_MMAP = 1
	IOU_PBUF_RING_INC  = 2
)

// ZCRX area encoding constants.
const (
	IORING_ZCRX_AREA_SHIFT  = 48
	IORING_ZCRX_AREA_MASK   = ^((uint64(1) << IORING_ZCRX_AREA_SHIFT) - 1)
	IORING_ZCRX_AREA_DMABUF = 1
)

// ZCRX control operations.
const (
	ZCRX_CTRL_FLUSH_RQ = 0
	ZCRX_CTRL_EXPORT   = 1
)

// ZCRX registration flags.
const ZCRX_REG_IMPORT = 1

// Query operation types for IORING_REGISTER_QUERY.
const (
	IO_URING_QUERY_OPCODES = 0
	IO_URING_QUERY_ZCRX    = 1
	IO_URING_QUERY_SCQ     = 2
)

// Memory region types.
const IORING_MEM_REGION_TYPE_USER = 1

// Memory region registration flags.
const IORING_MEM_REGION_REG_WAIT_ARG = 1

// NAPI operation types.
const (
	IO_URING_NAPI_REGISTER_OP   = 0
	IO_URING_NAPI_STATIC_ADD_ID = 1
	IO_URING_NAPI_STATIC_DEL_ID = 2
)

// NAPI tracking strategies.
const (
	IO_URING_NAPI_TRACKING_DYNAMIC  = 0
	IO_URING_NAPI_TRACKING_STATIC   = 1
	IO_URING_NAPI_TRACKING_INACTIVE = 255
)

// Socket uring command operations.
const (
	SOCKET_URING_OP_SIOCINQ      = 0
	SOCKET_URING_OP_SIOCOUTQ     = 1
	SOCKET_URING_OP_GETSOCKOPT   = 2
	SOCKET_URING_OP_SETSOCKOPT   = 3
	SOCKET_URING_OP_TX_TIMESTAMP = 4
	SOCKET_URING_OP_GETSOCKNAME  = 5
)

// Socket uring timestamp constants.
const (
	IORING_TIMESTAMP_HW_SHIFT   = 16
	IORING_TIMESTAMP_TYPE_SHIFT = IORING_TIMESTAMP_HW_SHIFT + 1
	IORING_CQE_F_TSTAMP_HW      = 1 << IORING_TIMESTAMP_HW_SHIFT
)

// NOP operation flags.
const (
	IORING_NOP_INJECT_RESULT = 1 << 0
	IORING_NOP_FILE          = 1 << 1
	IORING_NOP_FIXED_FILE    = 1 << 2
	IORING_NOP_FIXED_BUFFER  = 1 << 3
	IORING_NOP_TW            = 1 << 4
	IORING_NOP_CQE32         = 1 << 5
)

// Clone buffer registration flags.
const (
	IORING_REGISTER_SRC_REGISTERED = 1 << 0
	IORING_REGISTER_DST_REPLACE    = 1 << 1
)

// Registered wait flags.
const IORING_REG_WAIT_TS = 1 << 0

// Read/write attribute flags.
const IORING_RW_ATTR_FLAG_PI = 1 << 0

// QueryHdr is the header for query operations.
// Matches struct io_uring_query_hdr in Linux.
type QueryHdr struct {
	NextEntry uint64 // Pointer to next query entry
	QueryData uint64 // Query-specific data pointer
	QueryOp   uint32 // Query operation type (IO_URING_QUERY_*)
	Size      uint32 // Size of the query response
	Result    int32  // Result code
	_         [3]uint32
}

// QueryOpcode returns information about supported io_uring operations.
// Matches struct io_uring_query_opcode in Linux.
type QueryOpcode struct {
	NrRequestOpcodes  uint32 // Number of supported IORING_OP_* opcodes
	NrRegisterOpcodes uint32 // Number of supported IORING_REGISTER_* opcodes
	FeatureFlags      uint64 // Raw kernel feature bitmask returned by IORING_REGISTER_QUERY
	RingSetupFlags    uint64 // Bitmask of IORING_SETUP_* flags
	EnterFlags        uint64 // Bitmask of IORING_ENTER_* flags
	SqeFlags          uint64 // Bitmask of IOSQE_* flags
	NrQueryOpcodes    uint32 // Number of available query opcodes
	_                 uint32
}

// QueryZCRX returns information about ZCRX capabilities.
// Matches struct io_uring_query_zcrx in Linux.
type QueryZCRX struct {
	RegisterFlags  uint64 // Bitmask of supported ZCRX_REG_* flags
	AreaFlags      uint64 // Bitmask of IORING_ZCRX_AREA_* flags
	NrCtrlOpcodes  uint32 // Number of supported ZCRX_CTRL_* opcodes
	_              uint32
	RqHdrSize      uint32 // Refill ring header size
	RqHdrAlignment uint32 // Header alignment requirement
	_              uint64
}

// QuerySCQ returns information about SQ/CQ rings.
// Matches struct io_uring_query_scq in Linux.
type QuerySCQ struct {
	HdrSize      uint64 // SQ/CQ rings header size
	HdrAlignment uint64 // Header alignment requirement
}

// RegionDesc describes a memory region for io_uring.
// Matches struct io_uring_region_desc in Linux.
type RegionDesc struct {
	UserAddr   uint64 // User address of the region
	Size       uint64 // Size of the region
	Flags      uint32 // Region flags
	ID         uint32 // Region identifier
	MmapOffset uint64 // Offset for mmap
	_          [4]uint64
}

// MemRegionReg is the registration structure for memory regions.
// Matches struct io_uring_mem_region_reg in Linux.
type MemRegionReg struct {
	RegionUptr uint64 // Pointer to RegionDesc
	Flags      uint64 // Registration flags (IORING_MEM_REGION_REG_*)
	_          [2]uint64
}

// NapiReg is the registration structure for NAPI busy polling.
// Matches struct io_uring_napi in Linux.
type NapiReg struct {
	BusyPollTo     uint32 // Busy poll timeout in microseconds
	PreferBusyPoll uint8  // Prefer busy poll over sleeping
	Opcode         uint8  // IO_URING_NAPI_* operation
	_              [2]uint8
	OpParam        uint32 // Operation parameter (strategy or NAPI ID)
	_              uint32
}

// IoTimespec is a 128-bit timespec for high-precision timestamps.
// Matches struct io_timespec in Linux.
type IoTimespec struct {
	TvSec  uint64 // Seconds
	TvNsec uint64 // Nanoseconds
}

// CloneBuffers describes a buffer clone operation.
// Matches struct io_uring_clone_buffers in Linux.
type CloneBuffers struct {
	SrcFD  uint32 // Source ring file descriptor
	Flags  uint32 // IORING_REGISTER_SRC_* flags
	SrcOff uint32 // Source buffer offset
	DstOff uint32 // Destination buffer offset
	Nr     uint32 // Number of buffers to clone
	_      [3]uint32
}

// RegWait is a registered wait region entry.
// Matches struct io_uring_reg_wait in Linux.
type RegWait struct {
	Ts          Timespec // Timeout specification
	MinWaitUsec uint32   // Minimum wait time in microseconds
	Flags       uint32   // IORING_REG_WAIT_* flags
	Sigmask     uint64   // Signal mask
	SigmaskSz   uint32   // Signal mask size
	_           [3]uint32
	_           [2]uint64
}

// AttrPI is the PI attribute information for read/write operations.
// Matches struct io_uring_attr_pi in Linux.
type AttrPI struct {
	Flags  uint16 // PI flags
	AppTag uint16 // Application tag
	Len    uint32 // Length
	Addr   uint64 // Address
	Seed   uint64 // Seed value
	_      uint64
}

// ZCRXRqe is a zero-copy receive refill queue entry.
// Matches struct io_uring_zcrx_rqe in Linux.
type ZCRXRqe struct {
	Off uint64 // Offset into the ZCRX area
	Len uint32 // Length of the buffer
	_   uint32 // Padding
}

// ZCRXCqe is a zero-copy receive completion queue entry extension.
// Matches struct io_uring_zcrx_cqe in Linux.
type ZCRXCqe struct {
	Off uint64 // Offset into the ZCRX area
	_   uint64 // Padding
}

// ZCRXOffsets describes the layout of a ZCRX refill queue ring.
// Matches struct io_uring_zcrx_offsets in Linux.
type ZCRXOffsets struct {
	Head uint32    // Head offset
	Tail uint32    // Tail offset
	Rqes uint32    // RQE array offset
	_    uint32    // Reserved
	Resv [2]uint64 // Reserved
}

// ZCRXAreaReg is the area registration for ZCRX.
// Matches struct io_uring_zcrx_area_reg in Linux.
type ZCRXAreaReg struct {
	Addr        uint64    // Base address of the area
	Len         uint64    // Length of the area
	RqAreaToken uint64    // Token for RQ area
	Flags       uint32    // IORING_ZCRX_AREA_* flags
	DmabufFD    uint32    // DMA buffer file descriptor
	Resv        [2]uint64 // Reserved
}

// ZCRXIfqReg is the interface queue registration for ZCRX.
// Matches struct io_uring_zcrx_ifq_reg in Linux.
type ZCRXIfqReg struct {
	IfIdx     uint32      // Network interface index
	IfRxq     uint32      // RX queue index
	RqEntries uint32      // Number of refill queue entries
	Flags     uint32      // ZCRX_REG_* flags
	AreaPtr   uint64      // Pointer to ZCRXAreaReg
	RegionPtr uint64      // Pointer to memory region descriptor
	Offsets   ZCRXOffsets // Offsets within the ring
	ZcrxID    uint32      // ZCRX instance ID (output)
	RxBufLen  uint32      // Chunk size hint; 0 defaults to page size
	Resv      [3]uint64   // Reserved
}

// ZCRXCtrl is the control structure for ZCRX operations.
// Matches struct zcrx_ctrl in Linux.
type ZCRXCtrl struct {
	ZcrxID uint32    // ZCRX instance ID
	Op     uint32    // ZCRX_CTRL_* operation
	Resv   [2]uint64 // Reserved
	// Union: either ZcExport or ZcFlush based on Op
	Data [48]byte // Large enough for both structures
}
