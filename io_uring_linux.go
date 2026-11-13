// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"code.hybscloud.com/iobuf"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/spin"
	"code.hybscloud.com/zcall"
)

// These core `io_uring` definitions were refactored from
// `code.hybscloud.com/sox` into this package.

const (
	IORING_SETUP_IOPOLL             = zcall.IORING_SETUP_IOPOLL
	IORING_SETUP_SQPOLL             = zcall.IORING_SETUP_SQPOLL
	IORING_SETUP_SQ_AFF             = zcall.IORING_SETUP_SQ_AFF
	IORING_SETUP_CQSIZE             = zcall.IORING_SETUP_CQSIZE
	IORING_SETUP_CLAMP              = zcall.IORING_SETUP_CLAMP
	IORING_SETUP_ATTACH_WQ          = zcall.IORING_SETUP_ATTACH_WQ
	IORING_SETUP_R_DISABLED         = zcall.IORING_SETUP_R_DISABLED
	IORING_SETUP_SUBMIT_ALL         = zcall.IORING_SETUP_SUBMIT_ALL
	IORING_SETUP_COOP_TASKRUN       = zcall.IORING_SETUP_COOP_TASKRUN
	IORING_SETUP_TASKRUN_FLAG       = zcall.IORING_SETUP_TASKRUN_FLAG
	IORING_SETUP_SQE128             = zcall.IORING_SETUP_SQE128
	IORING_SETUP_CQE32              = zcall.IORING_SETUP_CQE32
	IORING_SETUP_SINGLE_ISSUER      = zcall.IORING_SETUP_SINGLE_ISSUER
	IORING_SETUP_DEFER_TASKRUN      = zcall.IORING_SETUP_DEFER_TASKRUN
	IORING_SETUP_NO_MMAP            = zcall.IORING_SETUP_NO_MMAP
	IORING_SETUP_REGISTERED_FD_ONLY = zcall.IORING_SETUP_REGISTERED_FD_ONLY
	IORING_SETUP_NO_SQARRAY         = zcall.IORING_SETUP_NO_SQARRAY
	IORING_SETUP_HYBRID_IOPOLL      = zcall.IORING_SETUP_HYBRID_IOPOLL
	IORING_SETUP_CQE_MIXED          = zcall.IORING_SETUP_CQE_MIXED // Allow both 16b and 32b CQEs
	IORING_SETUP_SQE_MIXED          = zcall.IORING_SETUP_SQE_MIXED // Allow both 64b and 128b SQEs
)

const (
	IORING_ENTER_GETEVENTS       = zcall.IORING_ENTER_GETEVENTS
	IORING_ENTER_SQ_WAKEUP       = zcall.IORING_ENTER_SQ_WAKEUP
	IORING_ENTER_SQ_WAIT         = zcall.IORING_ENTER_SQ_WAIT
	IORING_ENTER_EXT_ARG         = zcall.IORING_ENTER_EXT_ARG
	IORING_ENTER_REGISTERED_RING = zcall.IORING_ENTER_REGISTERED_RING
	IORING_ENTER_ABS_TIMER       = zcall.IORING_ENTER_ABS_TIMER   // Absolute timeout
	IORING_ENTER_EXT_ARG_REG     = zcall.IORING_ENTER_EXT_ARG_REG // Use registered wait region
	IORING_ENTER_NO_IOWAIT       = zcall.IORING_ENTER_NO_IOWAIT   // Skip I/O-wait accounting; may still block
)

const (
	IORING_OFF_SQ_RING    int64 = 0
	IORING_OFF_CQ_RING    int64 = 0x8000000
	IORING_OFF_SQES       int64 = 0x10000000
	IORING_OFF_PBUF_RING        = 0x80000000
	IORING_OFF_PBUF_SHIFT       = 16
	IORING_OFF_MMAP_MASK        = 0xf8000000
)

const (
	IORING_SQ_NEED_WAKEUP = 1 << iota
	IORING_SQ_CQ_OVERFLOW
	IORING_SQ_TASKRUN
)

const (
	IOSQE_FIXED_FILE       = zcall.IOSQE_FIXED_FILE
	IOSQE_IO_DRAIN         = zcall.IOSQE_IO_DRAIN
	IOSQE_IO_LINK          = zcall.IOSQE_IO_LINK
	IOSQE_IO_HARDLINK      = zcall.IOSQE_IO_HARDLINK
	IOSQE_ASYNC            = zcall.IOSQE_ASYNC
	IOSQE_BUFFER_SELECT    = zcall.IOSQE_BUFFER_SELECT
	IOSQE_CQE_SKIP_SUCCESS = zcall.IOSQE_CQE_SKIP_SUCCESS
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
	IORING_CQE_F_BUFFER        = 1 << 0
	IORING_CQE_F_MORE          = 1 << 1
	IORING_CQE_F_SOCK_NONEMPTY = 1 << 2
	IORING_CQE_F_NOTIF         = 1 << 3
	IORING_CQE_F_BUF_MORE      = 1 << 4  // Buffer partially consumed (incremental mode)
	IORING_CQE_F_SKIP          = 1 << 5  // Skip CQE (gap filler for ring wrap)
	IORING_CQE_F_32            = 1 << 15 // 32-byte CQE in mixed mode
)

const (
	IORING_CQE_BUFFER_SHIFT = 16
)

const (
	IORING_REGISTER_BUFFERS          = zcall.IORING_REGISTER_BUFFERS
	IORING_UNREGISTER_BUFFERS        = zcall.IORING_UNREGISTER_BUFFERS
	IORING_REGISTER_FILES            = zcall.IORING_REGISTER_FILES
	IORING_UNREGISTER_FILES          = zcall.IORING_UNREGISTER_FILES
	IORING_REGISTER_EVENTFD          = zcall.IORING_REGISTER_EVENTFD
	IORING_UNREGISTER_EVENTFD        = zcall.IORING_UNREGISTER_EVENTFD
	IORING_REGISTER_FILES_UPDATE     = zcall.IORING_REGISTER_FILES_UPDATE
	IORING_REGISTER_EVENTFD_ASYNC    = zcall.IORING_REGISTER_EVENTFD_ASYNC
	IORING_REGISTER_PROBE            = zcall.IORING_REGISTER_PROBE
	IORING_REGISTER_PERSONALITY      = zcall.IORING_REGISTER_PERSONALITY
	IORING_UNREGISTER_PERSONALITY    = zcall.IORING_UNREGISTER_PERSONALITY
	IORING_REGISTER_RESTRICTIONS     = zcall.IORING_REGISTER_RESTRICTIONS
	IORING_REGISTER_ENABLE_RINGS     = zcall.IORING_REGISTER_ENABLE_RINGS
	IORING_REGISTER_FILES2           = zcall.IORING_REGISTER_FILES2
	IORING_REGISTER_FILES_UPDATE2    = zcall.IORING_REGISTER_FILES_UPDATE2
	IORING_REGISTER_BUFFERS2         = zcall.IORING_REGISTER_BUFFERS2
	IORING_REGISTER_BUFFERS_UPDATE   = zcall.IORING_REGISTER_BUFFERS_UPDATE
	IORING_REGISTER_IOWQ_AFF         = zcall.IORING_REGISTER_IOWQ_AFF
	IORING_UNREGISTER_IOWQ_AFF       = zcall.IORING_UNREGISTER_IOWQ_AFF
	IORING_REGISTER_IOWQ_MAX_WORKERS = zcall.IORING_REGISTER_IOWQ_MAX_WORKERS
	IORING_REGISTER_RING_FDS         = zcall.IORING_REGISTER_RING_FDS
	IORING_UNREGISTER_RING_FDS       = zcall.IORING_UNREGISTER_RING_FDS
	IORING_REGISTER_PBUF_RING        = zcall.IORING_REGISTER_PBUF_RING
	IORING_UNREGISTER_PBUF_RING      = zcall.IORING_UNREGISTER_PBUF_RING
	IORING_REGISTER_SYNC_CANCEL      = zcall.IORING_REGISTER_SYNC_CANCEL
	IORING_REGISTER_FILE_ALLOC_RANGE = zcall.IORING_REGISTER_FILE_ALLOC_RANGE
	IORING_REGISTER_PBUF_STATUS      = zcall.IORING_REGISTER_PBUF_STATUS
	IORING_REGISTER_NAPI             = zcall.IORING_REGISTER_NAPI
	IORING_UNREGISTER_NAPI           = zcall.IORING_UNREGISTER_NAPI
	IORING_REGISTER_CLOCK            = zcall.IORING_REGISTER_CLOCK         // Register clock source
	IORING_REGISTER_CLONE_BUFFERS    = zcall.IORING_REGISTER_CLONE_BUFFERS // Clone buffers from another ring
	IORING_REGISTER_SEND_MSG_RING    = zcall.IORING_REGISTER_SEND_MSG_RING // Send MSG_RING without ring
	IORING_REGISTER_ZCRX_IFQ         = zcall.IORING_REGISTER_ZCRX_IFQ      // Register ZCRX interface queue
	IORING_REGISTER_RESIZE_RINGS     = zcall.IORING_REGISTER_RESIZE_RINGS  // Resize SQ and CQ rings
	IORING_REGISTER_MEM_REGION       = zcall.IORING_REGISTER_MEM_REGION    // Memory region setup (6.13+)
	IORING_REGISTER_QUERY            = zcall.IORING_REGISTER_QUERY         // Query ring state (6.18+)
	IORING_REGISTER_ZCRX_CTRL        = zcall.IORING_REGISTER_ZCRX_CTRL     // ZCRX control operations (6.19+)
)

// IORING_REGISTER_USE_REGISTERED_RING is a flag that can be OR'd with register
// opcodes to use a registered ring fd instead of a regular fd.
const IORING_REGISTER_USE_REGISTERED_RING = zcall.IORING_REGISTER_USE_REGISTERED_RING

const (
	IO_URING_OP_SUPPORTED = 1 << 0
)

const (
	ioUringDefaultEntries = EntriesMedium

	ioUringDefaultSqThreadCPU  = 0 // CPU 0 is always valid
	ioUringDefaultSqThreadIdle = 5 * time.Second
)

type ioUring struct {
	_ noCopy

	submit        submitState
	lifecycleLock spin.Lock
	started       atomic.Bool
	closed        atomic.Bool
	params        *ioUringParams

	sq     ioUringSq
	cq     ioUringCq
	ringFd int
	bufs   [][]byte
	files  []int32 // registered file descriptors
	ops    []ioUringProbeOp

	pollerFd int
}

type submitState struct {
	lock spin.Lock

	keepAlive        []submitKeepAlive
	keepAliveHead    uint32
	keepAliveExtFree *submitKeepAliveExt // protected by the shared-submit lock or the single-issuer caller contract

	shared bool // shared reports whether submit-state operations may arrive from multiple goroutines and need internal locking
}

type submitSlot struct {
	index  uint32
	tail   uint32
	sqe    *ioUringSqe
	sqe128 *ioUringSqe128
	keep   *submitKeepAlive
}

// ioUringFd represents a file descriptor in io_uring context.
type ioUringFd int32

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

func newIoUring(entries int, opts ...func(params *ioUringParams)) (*ioUring, error) {
	if entries < 1 || entries > EntriesHuge {
		return nil, ErrInvalidParam
	}

	params := new(ioUringParams)
	*params = *ioUringDefaultParams
	for _, opt := range opts {
		opt(params)
	}

	fd, err := ioUringSetup(uint32(entries), params)
	if err != nil {
		return nil, err
	}

	uring := &ioUring{
		submit: submitState{
			lock:      spin.Lock{},
			keepAlive: make([]submitKeepAlive, int(params.sqEntries)),
			shared:    params.flags&IORING_SETUP_SINGLE_ISSUER == 0,
		},
		params: params,

		sq: ioUringSq{
			ringSz: params.sqOff.array + uint32(unsafe.Sizeof(uint32(0)))*params.sqEntries,
		},
		cq: ioUringCq{
			ringSz:    params.cqOff.cqes + uint32(cqeBytesFromFlags(params.flags))*params.cqEntries,
			cqeStride: cqeStrideFromFlags(params.flags),
		},
		ringFd:   fd,
		bufs:     [][]byte{},
		pollerFd: -1,
	}

	if err := uring.remapRings(params); err != nil {
		zcall.Close(uintptr(fd))
		return nil, err
	}

	return uring, nil
}

func (slot submitSlot) reset() {
	if slot.sqe128 != nil {
		*slot.sqe128 = ioUringSqe128{}
		return
	}
	*slot.sqe = ioUringSqe{}
}

func (slot submitSlot) inlineCmdData128() []byte {
	if slot.sqe128 == nil {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&slot.sqe128.pad[0])), uringCmdInlineDataMax)
}

func sqeBytesFromFlags(flags uint32) int {
	if flags&IORING_SETUP_SQE128 != 0 {
		return int(unsafe.Sizeof(ioUringSqe128{}))
	}
	return int(unsafe.Sizeof(ioUringSqe{}))
}

func sqeStrideFromFlags(flags uint32) uintptr {
	return uintptr(sqeBytesFromFlags(flags))
}

func cqeBytesFromFlags(flags uint32) int {
	if flags&IORING_SETUP_CQE32 != 0 {
		return 2 * int(unsafe.Sizeof(ioUringCqe{}))
	}
	return int(unsafe.Sizeof(ioUringCqe{}))
}

func cqeStrideFromFlags(flags uint32) int {
	return cqeBytesFromFlags(flags) / int(unsafe.Sizeof(ioUringCqe{}))
}

// ringMapSz returns the shared SQ/CQ mmap size for supported Linux kernels.
// Linux 6.18+ advertises IORING_FEAT_SINGLE_MMAP, but the mapping must still
// cover whichever ring layout is larger.
func (ur *ioUring) ringMapSz() uintptr {
	return uintptr(max(ur.sq.ringSz, ur.cq.ringSz))
}

func (ur *ioUring) supportsOpcode(op uint8) bool {
	for _, supported := range ur.ops {
		if supported.op == op {
			return true
		}
	}
	return false
}

func (ur *ioUring) remapRings(params *ioUringParams) error {
	ur.sq.ringSz = params.sqOff.array + uint32(unsafe.Sizeof(uint32(0)))*params.sqEntries
	ur.cq.ringSz = params.cqOff.cqes + uint32(cqeBytesFromFlags(params.flags))*params.cqEntries
	ur.cq.cqeStride = cqeStrideFromFlags(params.flags)
	sqeStride := sqeStrideFromFlags(params.flags)
	sqeMapSz := uintptr(params.sqEntries) * sqeStride
	ringMapSz := ur.ringMapSz()

	ptr, errno := zcall.Mmap(nil, ringMapSz, PROT_READ|PROT_WRITE, MAP_SHARED|MAP_POPULATE, uintptr(ur.ringFd), uintptr(IORING_OFF_SQ_RING))
	if errno != 0 {
		return errFromErrno(errno)
	}

	sqesPtr, errno := zcall.Mmap(nil, sqeMapSz, PROT_READ|PROT_WRITE, MAP_SHARED|MAP_POPULATE, uintptr(ur.ringFd), uintptr(IORING_OFF_SQES))
	if errno != 0 {
		zcall.Munmap(ptr, ringMapSz)
		return errFromErrno(errno)
	}

	ur.sq.ringPtr = ptr
	ur.sq.sqesPtr = sqesPtr
	ur.sq.sqeStride = sqeStride
	ur.sq.sqeMapSz = sqeMapSz
	ur.sq.kHead = (*uint32)(unsafe.Add(ptr, params.sqOff.head))
	ur.sq.kTail = (*uint32)(unsafe.Add(ptr, params.sqOff.tail))
	ur.sq.kRingMask = (*uint32)(unsafe.Add(ptr, params.sqOff.ringMask))
	ur.sq.kRingEntries = (*uint32)(unsafe.Add(ptr, params.sqOff.ringEntries))
	ur.sq.kFlags = (*uint32)(unsafe.Add(ptr, params.sqOff.flags))
	ur.sq.kDropped = (*uint32)(unsafe.Add(ptr, params.sqOff.dropped))
	ur.sq.array = unsafe.Slice((*uint32)(unsafe.Add(ptr, params.sqOff.array)), int(params.sqEntries))

	ur.cq.kHead = (*uint32)(unsafe.Add(ptr, params.cqOff.head))
	ur.cq.kTail = (*uint32)(unsafe.Add(ptr, params.cqOff.tail))
	ur.cq.kRingMask = (*uint32)(unsafe.Add(ptr, params.cqOff.ringMask))
	ur.cq.kRingEntries = (*uint32)(unsafe.Add(ptr, params.cqOff.ringEntries))
	ur.cq.kOverflow = (*uint32)(unsafe.Add(ptr, params.cqOff.overflow))
	ur.cq.cqes = unsafe.Slice((*ioUringCqe)(unsafe.Add(ptr, params.cqOff.cqes)), int(params.cqEntries)*ur.cq.cqeStride)
	ur.cq.claimState = uint64(atomic.LoadUint32(ur.cq.kHead))

	return nil
}

func (ur *ioUring) registerProbe(probe *ioUringProbe) error {
	n := uintptr(len(probe.ops))
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_PROBE, unsafe.Pointer(probe), n)
	if errno != 0 {
		return errFromErrno(errno)
	}
	ur.ops = make([]ioUringProbeOp, 0, probe.opsLen)
	for i := range probe.opsLen {
		if probe.ops[i].flags&IO_URING_OP_SUPPORTED < IO_URING_OP_SUPPORTED {
			continue
		}
		ur.ops = append(ur.ops, probe.ops[i])
	}

	return nil
}

func (ur *ioUring) feature(feat uint32) bool {
	return feat == ur.params.features&feat
}

func (ur *ioUring) registerBuffers(addr unsafe.Pointer, n, size int) error {
	if len(ur.bufs) > 0 {
		return ErrExists
	}
	if n < 1 || size < 1 || 0 != size&(size-1) {
		return ErrInvalidParam
	}
	vectors := make([]IoVec, 0, n)
	buffers := make([][]byte, 0, n)
	for i := range n {
		base := unsafe.Add(addr, i*size)
		vectors = append(vectors, IoVec{Base: (*byte)(base), Len: uint64(size)})
		buffers = append(buffers, unsafe.Slice((*byte)(base), size))
	}
	data := uintptr(unsafe.Pointer(unsafe.SliceData(vectors)))
	reg := ioUringRSrcRegister{nr: uint32(n), data: uint64(data)}
	regSize := uintptr(unsafe.Sizeof(reg))
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_BUFFERS2, unsafe.Pointer(&reg), regSize)
	runtime.KeepAlive(vectors)
	if errno != 0 {
		return errFromErrno(errno)
	}
	ur.bufs = buffers

	return nil
}

func (ur *ioUring) unregisterBuffers() error {
	if len(ur.bufs) < 1 {
		return ErrNotFound
	}
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_UNREGISTER_BUFFERS, nil, 0)
	if errno != 0 {
		return errFromErrno(errno)
	}
	ur.bufs = [][]byte{}

	return nil
}

// registerFiles registers file descriptors for use with IOSQE_FIXED_FILE.
// Using registered files reduces per-operation overhead by avoiding
// file reference management on each I/O operation.
func (ur *ioUring) registerFiles(fds []int32) error {
	if len(ur.files) > 0 {
		return ErrExists
	}
	if len(fds) < 1 || uint(len(fds)) > math.MaxUint32 {
		return ErrInvalidParam
	}

	// Use IORING_REGISTER_FILES2 with rsrc_register struct for consistency
	reg := ioUringRSrcRegister{
		nr:   uint32(len(fds)),
		data: uint64(uintptr(unsafe.Pointer(unsafe.SliceData(fds)))),
	}
	regSize := uintptr(unsafe.Sizeof(reg))
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_FILES2, unsafe.Pointer(&reg), regSize)
	if errno != 0 {
		return errFromErrno(errno)
	}

	// Store a copy of the file descriptors
	ur.files = make([]int32, len(fds))
	copy(ur.files, fds)

	return nil
}

// registerFilesSparse allocates a sparse file table of the given size.
// Entries are initially empty (-1) and can be populated with registerFilesUpdate.
// Sparse registration allows dynamic file management without pre-populating all entries.
func (ur *ioUring) registerFilesSparse(count uint32) error {
	if len(ur.files) > 0 {
		return ErrExists
	}
	if count < 1 {
		return ErrInvalidParam
	}

	reg := ioUringRSrcRegister{
		nr:    count,
		flags: IORING_RSRC_REGISTER_SPARSE,
	}
	regSize := uintptr(unsafe.Sizeof(reg))
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_FILES2, unsafe.Pointer(&reg), regSize)
	if errno != 0 {
		return errFromErrno(errno)
	}

	// Initialize sparse table with -1 (empty slots)
	ur.files = make([]int32, count)
	for i := range ur.files {
		ur.files[i] = -1
	}

	return nil
}

// registerFilesUpdate updates registered files at the specified offset.
// Use -1 to clear a slot, or a valid fd to set it.
func (ur *ioUring) registerFilesUpdate(offset uint32, fds []int32) (int, error) {
	if len(ur.files) < 1 {
		return 0, ErrNotFound
	}
	if len(fds) < 1 || uint(len(fds)) > math.MaxUint32 {
		return 0, ErrInvalidParam
	}
	if uint64(offset) > uint64(len(ur.files)) {
		return 0, ErrInvalidParam
	}
	start := int(offset)
	if len(fds) > len(ur.files)-start {
		return 0, ErrInvalidParam
	}
	for _, fd := range fds {
		if fd < -1 {
			return 0, ErrInvalidParam
		}
	}

	// io_uring_files_update struct layout
	type ioUringFilesUpdate struct {
		offset uint32
		resv   uint32
		fds    uint64
	}
	up := ioUringFilesUpdate{
		offset: offset,
		fds:    uint64(uintptr(unsafe.Pointer(unsafe.SliceData(fds)))),
	}
	result, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_FILES_UPDATE, unsafe.Pointer(&up), uintptr(len(fds)))
	if errno != 0 {
		// Linux clears the destination before acquiring the replacement file.
		if errno == uintptr(zcall.EBADF) || errno == ENOMEM {
			ur.files[start] = -1
		}
		return 0, errFromErrno(errno)
	}

	updated := int(result)
	copy(ur.files[start:start+updated], fds[:updated])
	if updated < len(fds) {
		ur.files[start+updated] = -1
	}

	return updated, nil
}

// unregisterFiles removes all registered file descriptors.
func (ur *ioUring) unregisterFiles() error {
	if len(ur.files) < 1 {
		return ErrNotFound
	}
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_UNREGISTER_FILES, nil, 0)
	if errno != 0 {
		return errFromErrno(errno)
	}
	ur.files = nil

	return nil
}

func (ur *ioUring) registerBufRing(entries int, groupID uint16) (*ioUringBufRing, []byte, error) {
	r, backing, _, err := ur.registerBufRingWithFlags(entries, groupID, 0)
	return r, backing, err
}

// registerBufRingWithFlags registers a buffer ring with specified flags.
// Supported flags:
//   - IOU_PBUF_RING_MMAP: kernel allocates memory, use mmap to access
//   - IOU_PBUF_RING_INC: incremental buffer consumption mode
//
// Returns the buffer ring pointer and the backing slice (nil for mmap mode).
// Caller must keep the backing slice reachable while the registration is live.
func (ur *ioUring) registerBufRingWithFlags(entries int, groupID uint16, flags uint16) (*ioUringBufRing, []byte, uintptr, error) {
	if entries < 1 || entries > (1<<15) {
		return nil, nil, 0, ErrInvalidParam
	}
	entries = roundToPowerOf2(entries)

	var r *ioUringBufRing
	var backing []byte

	if flags&IOU_PBUF_RING_MMAP != 0 {
		// Kernel allocates memory - register first, then mmap
		reg := ioUringBufReg{
			ringAddr:    0, // Kernel will allocate
			ringEntries: uint32(entries),
			bgid:        groupID,
			flags:       flags,
		}
		_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_PBUF_RING, unsafe.Pointer(&reg), 1)
		if errno != 0 {
			return nil, nil, 0, errFromErrno(errno)
		}

		// Calculate mmap offset for this buffer ring
		offset := uintptr(IORING_OFF_PBUF_RING) | (uintptr(groupID) << IORING_OFF_PBUF_SHIFT)
		size := uintptr(entries) * unsafe.Sizeof(ioUringBuf{})

		// Map the kernel-allocated buffer ring
		ptr, errno := zcall.Mmap(nil, size, PROT_READ|PROT_WRITE, MAP_SHARED|MAP_POPULATE, uintptr(ur.ringFd), offset)
		if errno != 0 {
			// Unregister on mmap failure
			_ = ur.unregisterBufRing(groupID)
			return nil, nil, 0, errFromErrno(errno)
		}
		r = (*ioUringBufRing)(ptr)
		// backing is nil for mmap mode - kernel manages the memory
		return r, nil, size, nil
	} else {
		// User allocates memory - keep the slice alive to prevent GC
		backing = iobuf.AlignedMem(entries*int(unsafe.Sizeof(ioUringBuf{})), iobuf.PageSize)
		// Direct conversion without going through uintptr to satisfy checkptr
		r = (*ioUringBufRing)(unsafe.Pointer(unsafe.SliceData(backing)))
		ringAddr := uint64(uintptr(unsafe.Pointer(r)))

		reg := ioUringBufReg{
			ringAddr:    ringAddr,
			ringEntries: uint32(entries),
			bgid:        groupID,
			flags:       flags,
		}
		_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_PBUF_RING, unsafe.Pointer(&reg), 1)
		if errno != 0 {
			return nil, nil, 0, errFromErrno(errno)
		}
	}
	return r, backing, 0, nil
}

func (ur *ioUring) unregisterBufRing(groupID uint16) error {
	reg := ioUringBufReg{bgid: groupID}
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_UNREGISTER_PBUF_RING, unsafe.Pointer(&reg), 1)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

func (ur *ioUring) registerPoller(p poller) (int, error) {
	if ur.closed.Load() {
		return 0, ErrClosed
	}
	if ur.pollerFd >= 0 {
		return 0, ErrExists
	}
	efd, errno := zcall.Eventfd2(0, zcall.EFD_NONBLOCK|zcall.EFD_CLOEXEC)
	if errno != 0 {
		return 0, errFromErrno(errno)
	}

	_, errno = zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_EVENTFD_ASYNC, unsafe.Pointer(&efd), 1)
	if errno != 0 {
		zcall.Close(uintptr(efd))
		return 0, errFromErrno(errno)
	}

	err := p.add(int(efd), EPOLLIN|EPOLLET)
	if err != nil {
		_, _ = zcall.IoUringRegister(uintptr(ur.ringFd), IORING_UNREGISTER_EVENTFD, nil, 0)
		zcall.Close(uintptr(efd))
		return 0, err
	}

	ur.pollerFd = int(efd)
	return int(efd), nil
}

func (ur *ioUring) stop() error {
	if ur.closed.Load() {
		return nil
	}

	ur.lifecycleLock.Lock()
	defer ur.lifecycleLock.Unlock()
	if ur.closed.Load() {
		return nil
	}
	ur.closed.Store(true)

	ur.lockSubmitState()
	defer ur.unlockSubmitState()
	ur.clearAllKeepAlive()
	return ur.cleanupCoreRingResources()
}

func (ur *ioUring) closeAfterFatalResize(remapErr error) error {
	ur.closed.Store(true)
	ur.clearAllKeepAlive()
	err := ur.cleanupCoreRingResources()
	return errors.Join(remapErr, err)
}

func (ur *ioUring) cleanupCoreRingResources() error {
	var err error
	if ur.pollerFd >= 0 {
		errno := zcall.Close(uintptr(ur.pollerFd))
		if errno != 0 {
			err = errors.Join(err, fmt.Errorf("close eventfd %d: %w", ur.pollerFd, errFromErrno(errno)))
		}
		ur.pollerFd = -1
	}

	ur.bufs = nil
	ur.files = nil
	ur.ops = nil

	ur.unmapRings()
	if ur.ringFd >= 0 {
		errno := zcall.Close(uintptr(ur.ringFd))
		if errno != 0 {
			err = errors.Join(err, fmt.Errorf("close ring fd %d: %w", ur.ringFd, errFromErrno(errno)))
		}
		ur.ringFd = -1
	}

	return err
}

func (ur *ioUring) clearRingViews() {
	ur.sq = ioUringSq{}
	ur.cq = ioUringCq{}
}

func (ur *ioUring) unmapRings() {
	if ur.sq.sqesPtr != nil {
		zcall.Munmap(ur.sq.sqesPtr, ur.sq.sqeMapSz)
	}
	if ur.sq.ringPtr != nil {
		zcall.Munmap(ur.sq.ringPtr, ur.ringMapSz())
	}
	ur.clearRingViews()
}

func (ur *ioUring) enable() error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_ENABLE_RINGS, nil, 0)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// submitPacked3 submits a 3-argument operation with packed context.
func (ur *ioUring) submitPacked3(sqeCtx SQEContext, ioprio uint16, addr uint64, n int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.fill3(sqeCtx, ioprio, addr, n)
	ur.publishSubmitSlot(slot, sqeCtx)
	return nil
}

// submitPacked6 submits a 6-argument operation with packed context.
func (ur *ioUring) submitPacked6(sqeCtx SQEContext, ioprio uint16, off uint64, addr uint64, n int, uflags uint32) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.fill6(sqeCtx, ioprio, off, addr, n, uflags)
	ur.publishSubmitSlot(slot, sqeCtx)
	return nil
}

// submitPacked9 submits a 9-argument operation with packed context.
func (ur *ioUring) submitPacked9(sqeCtx SQEContext, ioprio uint16, off uint64, addr uint64, n int, uflags uint32, personality uint16, spliceFdIn int32) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.fill9(sqeCtx, ioprio, off, addr, n, uflags, personality, spliceFdIn)
	ur.publishSubmitSlot(slot, sqeCtx)
	return nil
}

func (slot submitSlot) fill3(sqeCtx SQEContext, ioprio uint16, addr uint64, n int) {
	e := slot.sqe
	e.opcode = sqeCtx.Op()
	e.flags = sqeCtx.Flags()
	e.ioprio = ioprio
	e.fd = sqeCtx.FD()
	e.off = 0
	e.addr = addr
	e.len = uint32(n)
	e.uflags = 0
	e.bufIndex = 0
	e.personality = 0
	e.spliceFdIn = 0
	e.pad = [2]uint64{}
}

func (slot submitSlot) fill6(sqeCtx SQEContext, ioprio uint16, off uint64, addr uint64, n int, uflags uint32) {
	e := slot.sqe
	e.opcode = sqeCtx.Op()
	e.flags = sqeCtx.Flags()
	e.ioprio = ioprio
	e.fd = sqeCtx.FD()
	e.off = off
	e.addr = addr
	e.len = uint32(n)
	e.uflags = uflags
	e.bufIndex = 0
	e.personality = 0
	e.spliceFdIn = 0
	e.pad = [2]uint64{}
}

func (slot submitSlot) fill9(sqeCtx SQEContext, ioprio uint16, off uint64, addr uint64, n int, uflags uint32, personality uint16, spliceFdIn int32) {
	e := slot.sqe
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
	e.pad = [2]uint64{}
}

// submitExtended copies ExtSQE.SQE into the reserved kernel SQE.
func (ur *ioUring) submitExtended(sqeCtx SQEContext) error {
	ext := sqeCtx.ExtSQE()
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	*slot.sqe = ext.SQE
	listenerTrackSubmit(ext)
	ur.publishSubmitSlot(slot, sqeCtx)
	return nil
}

func (ur *ioUring) sqCount() int {
	h := atomic.LoadUint32(ur.sq.kHead)
	t := atomic.LoadUint32(ur.sq.kTail)
	return int(t - h)
}

// enter flushes published SQEs and drives deferred task work when needed.
// Single-issuer rings rely on caller serialization here so the fast path can
// avoid shared-submit locking.
func (ur *ioUring) enter() error {
	ur.lockSubmitState()
	if ur.closed.Load() || ur.sq.kFlags == nil || ur.sq.kHead == nil || ur.sq.kTail == nil {
		ur.unlockSubmitState()
		return ErrClosed
	}

	if err := ur.wakeSQPollLocked(); err != nil {
		ur.unlockSubmitState()
		return err
	}
	if err := ur.flushSubmitLocked(); err != nil {
		ur.unlockSubmitState()
		return err
	}
	ur.unlockSubmitState()
	return nil
}

func (ur *ioUring) wakeSQPollLocked() error {
	flags := atomic.LoadUint32(ur.sq.kFlags)
	if flags&IORING_SQ_NEED_WAKEUP == 0 {
		return nil
	}
	_, err := ioUringEnter(ur.ringFd, uintptr(ur.params.sqEntries), 0, IORING_ENTER_SQ_WAKEUP)
	// ErrInProgress means another enter is already in progress. ErrExists is a
	// wrong-task verdict for SINGLE_ISSUER + DEFER_TASKRUN and must stay visible.
	if err != nil && err != ErrInProgress {
		return err
	}
	return nil
}

func (ur *ioUring) flushSubmitLocked() error {
	// Atomic loads pair with publishSubmitSlot's stores.
	sqHead := atomic.LoadUint32(ur.sq.kHead)
	sqTail := atomic.LoadUint32(ur.sq.kTail)
	if ur.params.flags&IORING_SETUP_SQPOLL == 0 {
		n := sqTail - sqHead
		// DEFER_TASKRUN and COOP_TASKRUN may still need a zero-submit enter after
		// the kernel has already consumed the SQEs.
		needTaskrunKick := n == 0 && ur.params.flags&(IORING_SETUP_DEFER_TASKRUN|IORING_SETUP_COOP_TASKRUN) != 0
		if n != 0 || needTaskrunKick {
			_, err := ioUringEnter(ur.ringFd, uintptr(n), 0, IORING_ENTER_GETEVENTS)
			// ErrExists identifies a wrong submitting task on a deferred
			// single-issuer ring and must not be suppressed.
			if err != nil && err != ErrInProgress {
				ur.releaseConsumedKeepAlive()
				return err
			}
		}
	}
	ur.releaseConsumedKeepAlive()
	return nil
}

func (ur *ioUring) poll(n int) error {
	if ur.params.flags&IORING_SETUP_IOPOLL == 0 {
		return nil
	}
	h := atomic.LoadUint32(ur.sq.kHead)
	t := atomic.LoadUint32(ur.sq.kTail)
	submit := t - h
	for {
		_, err := ioUringEnter(ur.ringFd, uintptr(submit), uintptr(n), IORING_ENTER_GETEVENTS)
		if err == ErrInterrupted {
			submit = 0 // Already submitted, just retry the wait
			continue
		}
		return err
	}
}

// claimCQ reserves at most limit visible CQEs. Shared consumers acquire the
// claim word before copying; its sequence binds the observed window to that CAS.
// Single-issuer callers already serialize consumption. The range must be copied
// before publishCQ makes its slots reusable and admits the next consumer.
func (ur *ioUring) claimCQ(limit int) (claim uint64, available uint32, err error) {
	if ur.closed.Load() || ur.cq.kHead == nil || ur.cq.kTail == nil || ur.cq.kRingMask == nil || len(ur.cq.cqes) == 0 {
		return 0, 0, ErrClosed
	}

	var backoff spin.Wait
	polled := false
	for {
		if ur.submit.shared {
			claim = atomic.LoadUint64(&ur.cq.claimState)
			if claim&cqClaimBusy != 0 {
				backoff.Once()
				continue
			}
		} else {
			claim = uint64(atomic.LoadUint32(ur.cq.kHead))
		}
		head := uint32(claim)
		// Acquire the kernel's publication before reading the claimed CQEs.
		tail := atomic.LoadUint32(ur.cq.kTail)
		if head == tail {
			// Enter and overflow accounting still share submit-state ownership.
			// A visible CQ window never needs this lock.
			ur.lockSubmitState()
			if !polled && ur.params.flags&IORING_SETUP_IOPOLL != 0 {
				polled = true
				if err := ur.poll(0); err != nil {
					ur.unlockSubmitState()
					return 0, 0, err
				}
			}
			tail = atomic.LoadUint32(ur.cq.kTail)
			if head == tail {
				overflowCount := uint32(0)
				if ur.cq.kOverflow != nil {
					overflowCount = atomic.LoadUint32(ur.cq.kOverflow)
				}
				if ur.sq.kFlags != nil && atomic.LoadUint32(ur.sq.kFlags)&IORING_SQ_CQ_OVERFLOW != 0 {
					ur.cq.overflowSeen = overflowCount
					ur.unlockSubmitState()
					return 0, 0, ErrCQOverflow
				}
				if ur.cq.kOverflow != nil && ur.cq.overflowSeen != overflowCount {
					ur.cq.overflowSeen = overflowCount
					ur.unlockSubmitState()
					return 0, 0, ErrCQOverflow
				}
				ur.unlockSubmitState()
				return 0, 0, iox.ErrWouldBlock
			}
			ur.unlockSubmitState()
		}

		available = tail - head
		if uint64(limit) < uint64(available) {
			available = uint32(limit)
		}
		if !ur.submit.shared || atomic.CompareAndSwapUint64(&ur.cq.claimState, claim, claim|cqClaimBusy) {
			return claim, available, nil
		}
		backoff.Once()
	}
}

// publishCQ releases copied slots before admitting the next CQ consumer.
func (ur *ioUring) publishCQ(claim uint64, count uint32) {
	next := claim + uint64(count)
	atomic.StoreUint32(ur.cq.kHead, uint32(next))
	if ur.submit.shared {
		atomic.StoreUint64(&ur.cq.claimState, next&^cqClaimBusy)
	}
}

// wait returns one copied CQE from the current completion window.
func (ur *ioUring) wait() (ioUringCqe, error) {
	claim, _, err := ur.claimCQ(1)
	if err != nil {
		return ioUringCqe{}, err
	}

	// The kernel may reuse the slot as soon as head is published.
	e := *ur.cq.cqeAt(uint32(claim) & *ur.cq.kRingMask)
	ur.publishCQ(claim, 1)
	return e, nil
}

// waitBatch copies a completion batch before publishing its head advance.
func (ur *ioUring) waitBatch(cqes []CQEView) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	claim, available, err := ur.claimCQ(len(cqes))
	if err != nil {
		return 0, err
	}

	h, mask := uint32(claim), *ur.cq.kRingMask
	n := int(available)
	for i := range n {
		e := ur.cq.cqeAt((h + uint32(i)) & mask)
		cqes[i] = CQEView{
			Res:   e.res,
			Flags: e.flags,
			ctx:   SQEContextFromRaw(e.userData),
		}
	}
	ur.publishCQ(claim, available)
	return n, nil
}

func (ur *ioUring) bufRingInit(br *ioUringBufRing) {
	br.tail = 0
}

func (ur *ioUring) bufRingAdd(br *ioUringBufRing, addr uintptr, n int, bid uint16, mask, offset uintptr) {
	add := ioUringBufSize * ((uintptr(br.tail) + offset) & mask)
	buf := (*ioUringBuf)(unsafe.Add(unsafe.Pointer(br), add))
	buf.addr = uint64(addr)
	buf.len = uint32(n)
	buf.bid = bid
}

func (ur *ioUring) bufRingAdvance(br *ioUringBufRing, count int) {
	br.tail += uint16(count)
}

func (ur *ioUring) bufRingAvailable(br *ioUringBufRing, bgid uint16) int {
	head, ret := uint16(0), 0
	ret = ur.bufRingHead(bgid, &head)
	if ret < 0 {
		return ret
	}
	return int(br.tail - head)
}

// bufRingHead queries the kernel for the current buffer-ring head.
// Returns 0 on success or a negative errno on failure.
func (ur *ioUring) bufRingHead(groupID uint16, head *uint16) int {
	status := ioUringBufStatus{bufGroup: uint32(groupID)}

	ret, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_PBUF_STATUS, unsafe.Pointer(&status), 1)
	if ret != 0 {
		return -int(errno)
	}
	*head = uint16(status.head)
	return 0
}

// ioUringRSrcRegister mirrors struct io_uring_rsrc_register
// from include/uapi/linux/io_uring.h (Linux 6.18).
type ioUringRSrcRegister struct {
	nr    uint32
	flags uint32 // IORING_RSRC_REGISTER_* flags
	resv2 uint64
	data  uint64
	tags  uint64
}

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
// These follow the futex2(2) interface, not the legacy futex(2) v1 flags.
const (
	FUTEX2_SIZE_U8  uint32 = 0x00 // 8-bit futex
	FUTEX2_SIZE_U16 uint32 = 0x01 // 16-bit futex
	FUTEX2_SIZE_U32 uint32 = 0x02 // 32-bit futex (most common)
	FUTEX2_SIZE_U64 uint32 = 0x03 // 64-bit futex
	FUTEX2_NUMA     uint32 = 0x04 // NUMA-aware futex
	FUTEX2_PRIVATE  uint32 = 128  // Private futex (process-local, faster)
)

// FUTEX_BITSET_MATCH_ANY matches any waker when used as mask in FutexWait.
const FUTEX_BITSET_MATCH_ANY uint64 = 0xFFFFFFFF

// MSG_RING command types for the addr field.
const (
	// IORING_MSG_DATA sends data (result + userData) to target ring's CQ.
	IORING_MSG_DATA uint64 = 0

	// IORING_MSG_SEND_FD transfers a fixed file from source to target ring.
	IORING_MSG_SEND_FD uint64 = 1
)

// MSG_RING flags for MsgRing operations.
const (
	// IORING_MSG_RING_CQE_SKIP skips posting CQE to target ring.
	// The source ring still gets a completion.
	IORING_MSG_RING_CQE_SKIP uint32 = 1 << 0

	// IORING_MSG_RING_FLAGS_PASS passes the specified flags to target CQE.
	IORING_MSG_RING_FLAGS_PASS uint32 = 1 << 1
)

// Buffer ring registration flags.
const (
	IOU_PBUF_RING_MMAP = 1 // Kernel allocates memory, app uses mmap
	IOU_PBUF_RING_INC  = 2 // Incremental buffer consumption mode
)

// ioUringBufReg mirrors struct io_uring_buf_reg
// from include/uapi/linux/io_uring.h (Linux 6.18).
type ioUringBufReg struct {
	ringAddr    uint64
	ringEntries uint32
	bgid        uint16
	flags       uint16 // IOU_PBUF_RING_* flags
	_           [3]uint64
}

// ioUringBufStatus mirrors struct io_uring_buf_status
// from include/uapi/linux/io_uring.h (Linux 6.18).
type ioUringBufStatus struct {
	bufGroup uint32
	head     uint32
	_        [8]uint32
}

// ioUringBuf mirrors struct io_uring_buf
// from include/uapi/linux/io_uring.h (Linux 6.18).
type ioUringBuf struct {
	addr uint64
	len  uint32
	bid  uint16
	tail uint16
}

var ioUringBufSize = unsafe.Sizeof(ioUringBuf{})

type ioUringBufRing ioUringBuf

type ioUringSq struct {
	kHead        *uint32
	kTail        *uint32
	kRingMask    *uint32
	kRingEntries *uint32
	kDropped     *uint32
	kFlags       *uint32
	array        []uint32

	ringPtr   unsafe.Pointer // for Munmap
	sqesPtr   unsafe.Pointer // for Munmap
	ringSz    uint32
	sqeStride uintptr
	sqeMapSz  uintptr
}

func (sq *ioUringSq) sqeAt(index uint32) *ioUringSqe {
	return (*ioUringSqe)(unsafe.Add(sq.sqesPtr, uintptr(index)*sq.sqeStride))
}

func (sq *ioUringSq) sqe128At(index uint32) *ioUringSqe128 {
	if sq.sqeStride != unsafe.Sizeof(ioUringSqe128{}) {
		return nil
	}
	return (*ioUringSqe128)(unsafe.Add(sq.sqesPtr, uintptr(index)*sq.sqeStride))
}

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

type ioUringSqe128 struct {
	ioUringSqe
	extra [64]byte
}

var _ [128 - unsafe.Sizeof(ioUringSqe128{})]struct{}

const cqClaimBusy = uint64(1) << 63

type ioUringCq struct {
	// The high bit owns the CQ copy; the low 63 bits identify its head sequence.
	// The wider sequence avoids claim ABA at the kernel's 32-bit counter wrap.
	claimState   uint64
	kHead        *uint32
	kTail        *uint32
	kRingMask    *uint32
	kRingEntries *uint32
	kOverflow    *uint32
	cqes         []ioUringCqe
	cqeStride    int

	ringSz       uint32
	overflowSeen uint32 // protected by submit-state serialization
}

func (cq *ioUringCq) cqeAt(index uint32) *ioUringCqe {
	stride := cq.cqeStride
	if stride == 0 {
		stride = 1
	}
	return &cq.cqes[int(index)*stride]
}

type ioUringCqe struct {
	userData uint64
	res      int32
	flags    uint32
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
		params.flags &= ^uint32(IORING_SETUP_COOP_TASKRUN)
		params.flags &= ^uint32(IORING_SETUP_TASKRUN_FLAG)
		params.flags &= ^uint32(IORING_SETUP_DEFER_TASKRUN)
	}
	ioUringHybridIoPollOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_IOPOLL | IORING_SETUP_HYBRID_IOPOLL
		params.flags &= ^uint32(IORING_SETUP_COOP_TASKRUN)
		params.flags &= ^uint32(IORING_SETUP_TASKRUN_FLAG)
		params.flags &= ^uint32(IORING_SETUP_DEFER_TASKRUN)
	}
	// sq poll mode is not recommended
	ioUringSqPollOptions = func(params *ioUringParams) {
		params.flags |= IORING_SETUP_SQPOLL | IORING_SETUP_SQ_AFF
		params.sqThreadCPU = ioUringDefaultSqThreadCPU
		params.sqThreadIdle = uint32(ioUringDefaultSqThreadIdle.Milliseconds())
	}
)

func ioUringSetup(entries uint32, params *ioUringParams) (fd int, err error) {
	r1, errno := zcall.IoUringSetup(uintptr(entries), unsafe.Pointer(params))
	if errno != 0 {
		return 0, errFromErrno(errno)
	}
	return int(r1), nil
}

func ioUringEnter(fd int, toSubmit uintptr, minComplete uintptr, flags uintptr) (n int, err error) {
	result, errno := zcall.IoUringEnter(uintptr(fd), toSubmit, minComplete, flags, nil, 0)
	if errno != 0 {
		return int(result), errFromErrno(errno)
	}
	return int(result), nil
}

// ========================================
// Zero-Copy Receive (ZCRX) Structures
// Requires the Linux 6.18+ baseline and network device hardware RX queue support
// ========================================

// ZCRX area shift and mask for encoding area ID into offsets.
const (
	IORING_ZCRX_AREA_SHIFT = 48
	IORING_ZCRX_AREA_MASK  = ^((uint64(1) << IORING_ZCRX_AREA_SHIFT) - 1)
)

// ZCRX area registration flags.
const (
	IORING_ZCRX_AREA_DMABUF = 1 // Use DMA buffer
)

// ZCRX control operations.
const (
	ZCRX_CTRL_FLUSH_RQ = 0 // Flush refill queue
	ZCRX_CTRL_EXPORT   = 1 // Export ZCRX state
)

// ZCRX registration flags.
const (
	ZCRX_REG_IMPORT = 1 // Import mode
)

// ========================================
// Query Interface (Linux 6.18+)
// ========================================

// Query operation types for IORING_REGISTER_QUERY.
const (
	IO_URING_QUERY_OPCODES = 0 // Query supported opcodes
	IO_URING_QUERY_ZCRX    = 1 // Query ZCRX capabilities
	IO_URING_QUERY_SCQ     = 2 // Query SQ/CQ ring info
)

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
// Fields before ResultSize match struct io_uring_query_opcode in Linux.
type QueryOpcode struct {
	NrRequestOpcodes  uint32 // Number of supported IORING_OP_* opcodes
	NrRegisterOpcodes uint32 // Number of supported IORING_REGISTER_* opcodes
	FeatureFlags      uint64 // Raw kernel feature bitmask returned by IORING_REGISTER_QUERY
	RingSetupFlags    uint64 // Bitmask of IORING_SETUP_* flags
	EnterFlags        uint64 // Bitmask of IORING_ENTER_* flags
	SqeFlags          uint64 // Bitmask of IOSQE_* flags
	NrQueryOpcodes    uint32 // Number of available query opcodes
	_                 uint32
	ResultSize        uint32 // Number of query-data bytes returned by the kernel
}

// ZCRX query feature flags.
const ZCRX_FEATURE_RX_PAGE_SIZE = 1 << 0

// QueryZCRX returns information about ZCRX capabilities.
// Fields before ResultSize match struct io_uring_query_zcrx in Linux.
type QueryZCRX struct {
	RegisterFlags  uint64 // Bitmask of supported ZCRX_REG_* flags
	AreaFlags      uint64 // Bitmask of IORING_ZCRX_AREA_* flags
	NrCtrlOpcodes  uint32 // Number of supported ZCRX_CTRL_* opcodes
	Features       uint32 // ZCRX_FEATURE_* bitmask (Linux 7.0+; reserved before then)
	RqHdrSize      uint32 // Refill ring header size
	RqHdrAlignment uint32 // Header alignment requirement
	_              uint64
	ResultSize     uint32 // Number of query-data bytes returned by the kernel
}

// QuerySCQ returns information about SQ/CQ rings.
// Fields before ResultSize match struct io_uring_query_scq in Linux.
type QuerySCQ struct {
	HdrSize      uint64 // SQ/CQ rings header size
	HdrAlignment uint64 // Header alignment requirement
	ResultSize   uint32 // Number of query-data bytes returned by the kernel
}

// ========================================
// Memory Region Support (Linux 6.13+)
// ========================================

// Memory region types.
const (
	IORING_MEM_REGION_TYPE_USER = 1 // User-provided memory
)

// Memory region registration flags.
const (
	IORING_MEM_REGION_REG_WAIT_ARG = 1 // Expose region as registered wait arguments
)

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

// ========================================
// NAPI Support (Linux 6.9+; static tracking requires Linux 6.13+)
// ========================================

// NAPI operation types.
const (
	IO_URING_NAPI_REGISTER_OP   = 0 // Register/unregister (backward compatible)
	IO_URING_NAPI_STATIC_ADD_ID = 1 // Add NAPI ID with static tracking
	IO_URING_NAPI_STATIC_DEL_ID = 2 // Delete NAPI ID with static tracking
)

// NAPI tracking strategies.
const (
	IO_URING_NAPI_TRACKING_DYNAMIC  = 0   // Dynamic tracking (default)
	IO_URING_NAPI_TRACKING_STATIC   = 1   // Static tracking
	IO_URING_NAPI_TRACKING_INACTIVE = 255 // Inactive/disabled
)

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

// ========================================
// Socket Uring Commands (Linux 6.19+)
// ========================================

// Socket uring command operations.
const (
	SOCKET_URING_OP_SIOCINQ      = 0 // Get input queue size
	SOCKET_URING_OP_SIOCOUTQ     = 1 // Get output queue size
	SOCKET_URING_OP_GETSOCKOPT   = 2 // Get socket option
	SOCKET_URING_OP_SETSOCKOPT   = 3 // Set socket option
	SOCKET_URING_OP_TX_TIMESTAMP = 4 // TX timestamp support
	SOCKET_URING_OP_GETSOCKNAME  = 5 // Get socket name
)

// Timestamp constants for SOCKET_URING_OP_TX_TIMESTAMP.
const (
	IORING_TIMESTAMP_HW_SHIFT   = 16                             // CQE flags bit shift for HW timestamp
	IORING_TIMESTAMP_TYPE_SHIFT = IORING_TIMESTAMP_HW_SHIFT + 1  // CQE flags bit shift for timestamp type
	IORING_CQE_F_TSTAMP_HW      = 1 << IORING_TIMESTAMP_HW_SHIFT // Hardware timestamp flag
)

// IoTimespec is a 128-bit timespec for high-precision timestamps.
// Matches struct io_timespec in Linux.
type IoTimespec struct {
	TvSec  uint64 // Seconds
	TvNsec uint64 // Nanoseconds
}

// ========================================
// NOP Flags (Linux 6.19+)
// ========================================

// NOP operation flags for IORING_OP_NOP.
const (
	IORING_NOP_INJECT_RESULT = 1 << 0 // Inject result from sqe->result
	IORING_NOP_FILE          = 1 << 1 // NOP with file reference
	IORING_NOP_FIXED_FILE    = 1 << 2 // NOP with fixed file
	IORING_NOP_FIXED_BUFFER  = 1 << 3 // NOP with fixed buffer
	IORING_NOP_TW            = 1 << 4 // NOP via task work
	IORING_NOP_CQE32         = 1 << 5 // NOP produces 32-byte CQE
)

// ========================================
// Clone Buffers (Linux 6.12+; offsets and replacement require Linux 6.13+)
// ========================================

// Clone buffers registration flags.
const (
	IORING_REGISTER_SRC_REGISTERED = 1 << 0 // Source ring is registered
	IORING_REGISTER_DST_REPLACE    = 1 << 1 // Replace destination buffers
)

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

// ========================================
// Registered Wait Region (Linux 6.19+)
// ========================================

// Registered wait flags.
const (
	IORING_REG_WAIT_TS = 1 << 0 // Timestamp in wait region
)

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

// ========================================
// Read/Write Attributes (Linux 6.19+)
// ========================================

// RW attribute flags for sqe->attr_type_mask.
const (
	IORING_RW_ATTR_FLAG_PI = 1 << 0 // PI (Protection Information) attribute
)

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
	RxBufLen  uint32      // Chunk size in/out (Linux 7.0+); must be zero before then
	Resv      [3]uint64   // Reserved
}

// ZCRXIfqRegistration is the observable result of ZCRX interface-queue registration.
type ZCRXIfqRegistration struct {
	ID        uint32
	RqEntries uint32 // Effective refill queue entry count.
	Offsets   ZCRXOffsets
	RxBufLen  uint32
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

// registerZCRXIfq registers a zero-copy receive interface queue.
// This sets up ZCRX for a specific network interface RX queue.
func (ur *ioUring) registerZCRXIfq(reg *ZCRXIfqReg) error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_ZCRX_IFQ, unsafe.Pointer(reg), 1)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// zcrxCtrl performs a ZCRX control operation.
func (ur *ioUring) zcrxCtrl(ctrl *ZCRXCtrl) error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_ZCRX_CTRL, unsafe.Pointer(ctrl), 0)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// ========================================
// Query Interface Methods (Linux 6.18+)
// ========================================

// query performs a query operation on the io_uring ring.
// This can be used to query supported opcodes, ZCRX capabilities, or ring info.
func (ur *ioUring) query(hdr *QueryHdr) error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_QUERY, unsafe.Pointer(hdr), 0)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return CompletionError(hdr.Result)
}

// queryOpcodes queries the kernel for supported io_uring operations.
// Returns a QueryOpcode structure with information about supported operations.
func (ur *ioUring) queryOpcodes() (*QueryOpcode, error) {
	result := &QueryOpcode{}
	hdr := QueryHdr{
		QueryOp:   IO_URING_QUERY_OPCODES,
		QueryData: uint64(uintptr(unsafe.Pointer(result))),
		Size:      uint32(unsafe.Offsetof(result.ResultSize)),
	}
	err := ur.query(&hdr)
	if err != nil {
		return nil, err
	}
	result.ResultSize = hdr.Size
	return result, nil
}

// queryZCRX queries the kernel for ZCRX capabilities.
func (ur *ioUring) queryZCRX() (*QueryZCRX, error) {
	result := &QueryZCRX{}
	hdr := QueryHdr{
		QueryOp:   IO_URING_QUERY_ZCRX,
		QueryData: uint64(uintptr(unsafe.Pointer(result))),
		Size:      uint32(unsafe.Offsetof(result.ResultSize)),
	}
	err := ur.query(&hdr)
	if err != nil {
		return nil, err
	}
	result.ResultSize = hdr.Size
	return result, nil
}

// querySCQ queries the kernel for SQ/CQ ring information.
func (ur *ioUring) querySCQ() (*QuerySCQ, error) {
	result := &QuerySCQ{}
	hdr := QueryHdr{
		QueryOp:   IO_URING_QUERY_SCQ,
		QueryData: uint64(uintptr(unsafe.Pointer(result))),
		Size:      uint32(unsafe.Offsetof(result.ResultSize)),
	}
	err := ur.query(&hdr)
	if err != nil {
		return nil, err
	}
	result.ResultSize = hdr.Size
	return result, nil
}

// ========================================
// Memory Region Methods (Linux 6.13+)
// ========================================

// registerMemRegion registers a memory region with the io_uring ring.
// This enables shared memory regions between user space and kernel for data transfer.
func (ur *ioUring) registerMemRegion(reg *MemRegionReg) error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_MEM_REGION, unsafe.Pointer(reg), 1)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// ========================================
// NAPI Methods (Linux 6.9+; static tracking requires Linux 6.13+)
// ========================================

// registerNAPI registers NAPI busy polling with the io_uring ring.
// NAPI (New API) enables kernel-side batched network packet processing.
func (ur *ioUring) registerNAPI(napi *NapiReg) error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_NAPI, unsafe.Pointer(napi), 1)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// unregisterNAPI unregisters NAPI busy polling from the io_uring ring.
func (ur *ioUring) unregisterNAPI() error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_UNREGISTER_NAPI, nil, 1)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// ========================================
// Clone Buffers Methods (Linux 6.12+; offsets and replacement require Linux 6.13+)
// ========================================

// cloneBuffers clones registered buffers from another io_uring ring.
// This is useful for sharing buffers between multiple rings.
func (ur *ioUring) cloneBuffers(clone *CloneBuffers) error {
	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_CLONE_BUFFERS, unsafe.Pointer(clone), 1)
	if errno != 0 {
		return errFromErrno(errno)
	}
	return nil
}

// ========================================
// Resize Rings Methods
// ========================================

// resizeRings replaces the SQ/CQ mappings while preserving pending entries
// and their submission storage.
//
// The kernel requires DEFER_TASKRUN and no pending CQ overflow. Requested
// sizes are rounded up to powers of two and clamped to kernel limits. A zero
// SQ size keeps the current size; a zero CQ size selects twice the resulting SQ.
//
// A post-register remap failure closes the ring: the kernel has committed the
// resize, so the old userspace mappings can no longer drive it.
func (ur *ioUring) resizeRings(newSQSize, newCQSize uint32) error {
	ur.lockSubmitState()
	defer ur.unlockSubmitState()
	ur.releaseConsumedKeepAlive()

	if newSQSize == 0 {
		newSQSize = ur.params.sqEntries
	}
	// Linux accepts only resize-control flags here. It inherits and returns
	// immutable layout flags such as SQE128 and CQE32.
	params := ioUringParams{
		sqEntries: newSQSize,
		cqEntries: newCQSize,
		flags:     IORING_SETUP_CLAMP,
	}
	// If explicit CQ size provided, set the flag
	if newCQSize > 0 {
		params.flags |= IORING_SETUP_CQSIZE
	}

	oldTail := atomic.LoadUint32(ur.sq.kTail)
	oldMask := uint32(0)
	if ur.sq.kRingMask != nil {
		oldMask = *ur.sq.kRingMask
	}
	oldKeepAliveHead := ur.submit.keepAliveHead
	oldKeepAlive := ur.submit.keepAlive
	oldSQ := ur.sq
	oldCQ := ur.cq
	oldRingPtr := ur.sq.ringPtr
	oldRingMapSz := ur.ringMapSz()
	oldSqesPtr := ur.sq.sqesPtr
	oldSqeMapSz := ur.sq.sqeMapSz

	_, errno := zcall.IoUringRegister(uintptr(ur.ringFd), IORING_REGISTER_RESIZE_RINGS, unsafe.Pointer(&params), 1)
	if errno != 0 {
		return errFromErrno(errno)
	}

	// Pending entries remain readable through the old mappings until relocation.
	defer zcall.Munmap(oldSqesPtr, oldSqeMapSz)
	defer zcall.Munmap(oldRingPtr, oldRingMapSz)
	ur.clearRingViews()
	if err := ur.remapRings(&params); err != nil {
		return ur.closeAfterFatalResize(fmt.Errorf("remap resized rings: %w", err))
	}
	// Registration freezes the old CQ. Preserve complete entries, including
	// CQE32 payloads that older kernels copy in 16-byte units during resize.
	cqHead := atomic.LoadUint32(oldCQ.kHead)
	cqTail := atomic.LoadUint32(oldCQ.kTail)
	for idx := cqHead; idx != cqTail; idx++ {
		src := int(idx&*oldCQ.kRingMask) * oldCQ.cqeStride
		dst := int(idx&*ur.cq.kRingMask) * ur.cq.cqeStride
		copy(ur.cq.cqes[dst:dst+ur.cq.cqeStride], oldCQ.cqes[src:src+oldCQ.cqeStride])
	}

	resizedSQ := len(oldKeepAlive) != int(params.sqEntries)
	if resizedSQ {
		ur.submit.keepAlive = make([]submitKeepAlive, int(params.sqEntries))
	}
	newMask := params.sqEntries - 1
	for idx := oldKeepAliveHead; idx != oldTail; idx++ {
		oldIndex, newIndex := idx&oldMask, idx&newMask
		sqe := ur.sq.sqeAt(newIndex)
		// Reconstruct complete entries: older kernels copy only 64-byte units
		// during resize, including on SQE128 rings.
		if params.flags&IORING_SETUP_SQE128 != 0 {
			*ur.sq.sqe128At(newIndex) = *oldSQ.sqe128At(oldIndex)
		} else {
			*sqe = *oldSQ.sqeAt(oldIndex)
		}
		if params.flags&IORING_SETUP_NO_SQARRAY == 0 {
			ur.sq.array[newIndex] = newIndex
		}
		if !resizedSQ {
			continue
		}
		keep := &ur.submit.keepAlive[newIndex]
		*keep = oldKeepAlive[oldIndex]
		// Iovec slices and ext pointers retain their original backing. Inline
		// timespecs have only integer SQE addresses, so rebase those addresses.
		oldAddr := uint64(uintptr(unsafe.Pointer(&oldKeepAlive[oldIndex].timespec)))
		newAddr := uint64(uintptr(unsafe.Pointer(&keep.timespec)))
		switch sqe.opcode {
		case IORING_OP_TIMEOUT, IORING_OP_LINK_TIMEOUT:
			if sqe.addr != oldAddr {
				continue
			}
			sqe.addr = newAddr
		case IORING_OP_TIMEOUT_REMOVE:
			if sqe.uflags&IORING_TIMEOUT_UPDATE_MASK == 0 || sqe.off != oldAddr {
				continue
			}
			sqe.off = newAddr
		default:
			continue
		}
		ctx := SQEContextFromRaw(sqe.userData)
		switch ctx.Mode() {
		case CtxModeIndirect:
			ctx.IndirectSQE().ioUringSqe = *sqe
		case CtxModeExtended:
			ctx.ExtSQE().SQE = *sqe
		}
	}

	ur.params.sqEntries = params.sqEntries
	ur.params.cqEntries = params.cqEntries
	ur.params.sqOff = params.sqOff
	ur.params.cqOff = params.cqOff
	return nil
}
