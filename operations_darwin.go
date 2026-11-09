// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"math"
	"unsafe"

	"code.hybscloud.com/iofd"
)

// io_uring operation codes for API compatibility.
const (
	IORING_OP_NOP uint8 = iota
	IORING_OP_READV
	IORING_OP_WRITEV
	IORING_OP_FSYNC
	IORING_OP_READ_FIXED
	IORING_OP_WRITE_FIXED
	IORING_OP_POLL_ADD
	IORING_OP_POLL_REMOVE
	IORING_OP_SYNC_FILE_RANGE
	IORING_OP_SENDMSG
	IORING_OP_RECVMSG
	IORING_OP_TIMEOUT
	IORING_OP_TIMEOUT_REMOVE
	IORING_OP_ACCEPT
	IORING_OP_ASYNC_CANCEL
	IORING_OP_LINK_TIMEOUT
	IORING_OP_CONNECT
	IORING_OP_FALLOCATE
	IORING_OP_OPENAT
	IORING_OP_CLOSE
	IORING_OP_FILES_UPDATE
	IORING_OP_STATX
	IORING_OP_READ
	IORING_OP_WRITE
	IORING_OP_FADVISE
	IORING_OP_MADVISE
	IORING_OP_SEND
	IORING_OP_RECV
	IORING_OP_OPENAT2
	IORING_OP_EPOLL_CTL
	IORING_OP_SPLICE
	IORING_OP_PROVIDE_BUFFERS
	IORING_OP_REMOVE_BUFFERS
	IORING_OP_TEE
	IORING_OP_SHUTDOWN
	IORING_OP_RENAMEAT
	IORING_OP_UNLINKAT
	IORING_OP_MKDIRAT
	IORING_OP_SYMLINKAT
	IORING_OP_LINKAT
	IORING_OP_MSG_RING
	IORING_OP_FSETXATTR
	IORING_OP_SETXATTR
	IORING_OP_FGETXATTR
	IORING_OP_GETXATTR
	IORING_OP_SOCKET
	IORING_OP_URING_CMD
	IORING_OP_SEND_ZC
	IORING_OP_SENDMSG_ZC
	IORING_OP_READ_MULTISHOT
	IORING_OP_WAITID
	IORING_OP_FUTEX_WAIT
	IORING_OP_FUTEX_WAKE
	IORING_OP_FUTEX_WAITV
	IORING_OP_FIXED_FD_INSTALL
	IORING_OP_FTRUNCATE
	IORING_OP_BIND
	IORING_OP_LISTEN
	IORING_OP_RECV_ZC      // Zero-copy receive
	IORING_OP_EPOLL_WAIT   // Epoll wait
	IORING_OP_READV_FIXED  // Vectored read with one fixed buffer
	IORING_OP_WRITEV_FIXED // Vectored write with one fixed buffer
	IORING_OP_PIPE         // Create pipe
	IORING_OP_NOP128       // 128-byte NOP
	IORING_OP_URING_CMD128 // 128-byte uring command
	ioUringOpLast
)

// URING_CMD operation flags mirror Linux for API compatibility.
const (
	IORING_URING_CMD_FIXED = 1 << iota
	IORING_URING_CMD_MULTISHOT
	IORING_URING_CMD_MASK = IORING_URING_CMD_FIXED | IORING_URING_CMD_MULTISHOT
)

// Timeout clock and behavior flags mirror Linux IORING_TIMEOUT_* constants.
// Defined here so darwin build stubs share the same named constants.
const (
	IORING_TIMEOUT_ABS           = 1 << iota // 0x01, absolute timestamp
	IORING_TIMEOUT_UPDATE                    // 0x02, update existing timeout
	IORING_TIMEOUT_BOOTTIME                  // 0x04, use CLOCK_BOOTTIME
	IORING_TIMEOUT_REALTIME                  // 0x08, use CLOCK_REALTIME
	IORING_LINK_TIMEOUT_UPDATE               // 0x10, update linked timeout
	IORING_TIMEOUT_ETIME_SUCCESS             // 0x20, convert -ETIME to success
	IORING_TIMEOUT_MULTISHOT                 // 0x40, repeat on expiry

	IORING_TIMEOUT_CLOCK_MASK  = IORING_TIMEOUT_BOOTTIME | IORING_TIMEOUT_REALTIME
	IORING_TIMEOUT_UPDATE_MASK = IORING_TIMEOUT_UPDATE | IORING_LINK_TIMEOUT_UPDATE
)

// Accept operation flags.
const (
	IORING_ACCEPT_MULTISHOT  = 1 << 0
	IORING_ACCEPT_DONTWAIT   = 1 << 1
	IORING_ACCEPT_POLL_FIRST = 1 << 2
)

// Send/receive operation flags.
const (
	IORING_RECVSEND_POLL_FIRST  = 1 << 0
	IORING_RECV_MULTISHOT       = 1 << 1
	IORING_RECVSEND_FIXED_BUF   = 1 << 2
	IORING_SEND_ZC_REPORT_USAGE = 1 << 3
	IORING_RECVSEND_BUNDLE      = 1 << 4 // Bundle send/recv
	IORING_SEND_VECTORIZED      = 1 << 5 // Vectorized send
)

// Notification CQE usage flags for zero-copy operations.
const IORING_NOTIF_USAGE_ZC_COPIED = 1 << 31

// Internal flags
const (
	uringOpFlagsNone  uint8  = 0
	uringOpIOPrioNone uint16 = 0
	maxIovecs                = 1024 // Linux UIO_MAXIOV compatibility limit.
)

// nop submits a no-op operation.
func (ur *ioUring) nop(sqeCtx SQEContext) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_NOP), func(e *ioUringSqe) {
		e.opcode = IORING_OP_NOP
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()
	})
}

// readv performs vectored read.
func (ur *ioUring) readv(sqeCtx SQEContext, iovs [][]byte) error {
	if iovs == nil || len(iovs) < 1 || len(iovs) > maxIovecs {
		return ErrInvalidParam
	}
	addr, n := ioVecFromBytesSlice(iovs)
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_READV), 0, uint64(uintptr(addr)), n)
}

// writev performs vectored write.
func (ur *ioUring) writev(sqeCtx SQEContext, iovs [][]byte) error {
	if iovs == nil || len(iovs) < 1 || len(iovs) > maxIovecs {
		return ErrInvalidParam
	}
	addr, n := ioVecFromBytesSlice(iovs)
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_WRITEV), 0, uint64(uintptr(addr)), n)
}

// fsync performs file sync.
func (ur *ioUring) fsync(sqeCtx SQEContext, uflags uint32) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FSYNC), 0, 0, 0, 0, uflags)
}

// readFixed reads into a registered buffer.
func (ur *ioUring) readFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int) ([]byte, error) {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return nil, ErrInvalidParam
	}
	buf := ur.bufs[bufIndex]
	if n < 0 || uint(n) > math.MaxUint32 || n > len(buf) {
		return nil, ErrInvalidParam
	}
	payload := buf[:n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	ctx := sqeCtx.WithOp(IORING_OP_READ_FIXED).WithBufGroup(uint16(bufIndex))
	err := ur.submitPacked9WithPayload(ctx, payload, 0, offset, addr, n, 0, 0, 0)
	return buf, err
}

// writeFixed writes from a registered buffer.
func (ur *ioUring) writeFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int) error {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return ErrInvalidParam
	}
	buf := ur.bufs[bufIndex]
	if n < 0 || uint(n) > math.MaxUint32 || n > len(buf) {
		return ErrInvalidParam
	}
	payload := buf[:n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	ctx := sqeCtx.WithOp(IORING_OP_WRITE_FIXED).WithBufGroup(uint16(bufIndex))
	return ur.submitPacked9WithPayload(ctx, payload, 0, offset, addr, n, 0, 0, 0)
}

// pollAdd adds a file descriptor to poll.
func (ur *ioUring) pollAdd(sqeCtx SQEContext, ioprio uint16, events int) error {
	if events < 0 || uint64(events) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_POLL_ADD), ioprio, 0, 0, 0, uint32(events))
}

// pollRemove removes a poll request matched by original user_data.
func (ur *ioUring) pollRemove(sqeCtx SQEContext, targetUserData uint64) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_POLL_REMOVE), func(e *ioUringSqe) {
		e.opcode = IORING_OP_POLL_REMOVE
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()
		e.addr = targetUserData
	})
}

// pollUpdate modifies an existing poll request in-place.
func (ur *ioUring) pollUpdate(sqeCtx SQEContext, oldUserData, newUserData uint64, newEvents, updateFlags int) error {
	const updateMask = IORING_POLL_UPDATE_EVENTS | IORING_POLL_UPDATE_USER_DATA
	updateEvents := updateFlags&IORING_POLL_UPDATE_EVENTS != 0
	updateUserData := updateFlags&IORING_POLL_UPDATE_USER_DATA != 0

	if newEvents < 0 || uint64(newEvents) > math.MaxUint32 ||
		updateFlags < 0 || uint64(updateFlags) > math.MaxUint32 ||
		updateFlags & ^updateMask != 0 ||
		(!updateUserData && newUserData != 0) || (!updateEvents && newEvents != 0) {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_POLL_REMOVE), 0, newUserData, oldUserData, updateFlags, uint32(newEvents))
}

// syncFileRange syncs a file range.
func (ur *ioUring) syncFileRange(sqeCtx SQEContext, offset uint64, n int, uflags uint32) error {
	if n < 0 || uint(n) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SYNC_FILE_RANGE), 0, offset, 0, n, uflags)
}

// sendmsg sends a message with control data.
func (ur *ioUring) sendmsg(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte, to Sockaddr) error {
	if len(buffers) > maxIovecs {
		return ErrInvalidParam
	}
	var saPtr unsafe.Pointer
	saN := 0
	var err error
	if to != nil {
		saPtr, saN, err = sockaddr(to)
		if err != nil {
			return err
		}
	}
	addr, n := ioVecFromBytesSlice(buffers)
	msg := Msghdr{
		Name:       (*byte)(saPtr),
		Namelen:    uint32(saN),
		Iov:        (*IoVec)(unsafe.Pointer(addr)),
		Iovlen:     uint64(n),
		Control:    nil,
		Controllen: 0,
	}
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.Controllen = uint64(len(oob))
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(&msg)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SENDMSG), ioprio, 0, msgAddr, 1, 0)
}

// recvmsg receives a message through caller-owned completion-lived metadata.
func (ur *ioUring) recvmsg(sqeCtx SQEContext, ioprio uint16, msg *Msghdr) error {
	if msg == nil || msg.Iovlen > maxIovecs ||
		(msg.Iovlen != 0 && msg.Iov == nil) ||
		(msg.Controllen != 0 && msg.Control == nil) ||
		(msg.Namelen != 0 && msg.Name == nil) {
		return ErrInvalidParam
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(msg)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_RECVMSG), ioprio, 0, msgAddr, 1, MSG_WAITALL)
}

// timeout submits a timeout operation.
func (ur *ioUring) timeout(sqeCtx SQEContext, cnt int, ts *Timespec, uflags int) error {
	if cnt < 0 || uint64(cnt) > math.MaxUint32 {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(ts)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_TIMEOUT), 0, uint64(cnt), addr, 1, uint32(uflags))
}

// timeoutRemove removes a timeout.
func (ur *ioUring) timeoutRemove(sqeCtx SQEContext, userData uint64, uflags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_TIMEOUT_REMOVE), 0, 0, userData, 0, uint32(uflags))
}

// accept accepts a connection.
func (ur *ioUring) accept(sqeCtx SQEContext, ioprio uint16) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ACCEPT), ioprio, 0, 0, 0, 0)
}

// acceptDirect accepts a connection into a registered file table slot (Darwin stub).
func (ur *ioUring) acceptDirect(sqeCtx SQEContext, ioprio uint16, fileIndex uint32) error {
	return ErrNotSupported
}

// asyncCancel cancels an async operation.
func (ur *ioUring) asyncCancel(sqeCtx SQEContext, targetUserData uint64) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ASYNC_CANCEL), 0, 0, targetUserData, 0, 0)
}

// asyncCancelExt cancels operations with extended matching modes.
func (ur *ioUring) asyncCancelExt(sqeCtx SQEContext, targetUserData uint64, targetOpcode uint8, cancelFlags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ASYNC_CANCEL), 0, 0, targetUserData, int(targetOpcode), uint32(cancelFlags))
}

// linkTimeout creates a linked timeout.
func (ur *ioUring) linkTimeout(sqeCtx SQEContext, ts *Timespec, uflags int) error {
	addr := uint64(uintptr(unsafe.Pointer(ts)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_LINK_TIMEOUT), 0, 0, addr, 1, uint32(uflags))
}

// connect initiates a connection.
func (ur *ioUring) connect(sqeCtx SQEContext, sa Sockaddr) error {
	ptr, n, err := sockaddr(sa)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(ptr))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_CONNECT), 0, 0, addr, n, 0)
}

// fAllocate allocates file space.
func (ur *ioUring) fAllocate(sqeCtx SQEContext, mode uint32, off uint64, length int64) error {
	if length <= 0 {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FALLOCATE), 0, off, uint64(length), int(mode), 0)
}

// openAt opens a file.
func (ur *ioUring) openAt(sqeCtx SQEContext, pathname string, openFlags uint32, mode uint32) error {
	pathPtr, err := bytePtrFromString(pathname)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(unsafe.Pointer(pathPtr)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_OPENAT), 0, 0, addr, int(mode), openFlags)
}

// close closes a file descriptor.
func (ur *ioUring) close(sqeCtx SQEContext) error {
	ctx := sqeCtx.WithOp(IORING_OP_CLOSE)
	if ctx.Flags()&IOSQE_FIXED_FILE != 0 && ctx.FD() < 0 {
		return ErrInvalidParam
	}
	return ur.submitPacked(ctx, func(e *ioUringSqe) {
		e.opcode = IORING_OP_CLOSE
		e.flags = ctx.Flags()
		e.fd = ctx.FD()
		if e.flags&IOSQE_FIXED_FILE != 0 {
			fileIndex := ctx.FD()
			e.flags &^= IOSQE_FIXED_FILE
			e.fd = 0
			e.spliceFdIn = int32(uint32(fileIndex) + 1)
		}
	})
}

// read performs a read operation.
func (ur *ioUring) read(sqeCtx SQEContext, ioprio uint16, b []byte, off uint64, n int) error {
	if len(b) < 1 || n < 0 || uint(n) > math.MaxUint32 || n > len(b) {
		return ErrInvalidParam
	}
	payload := b[:n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	return ur.submitPacked6WithPayload(sqeCtx.WithOp(IORING_OP_READ), payload, ioprio, off, addr, n, 0)
}

// write performs a write operation.
func (ur *ioUring) write(sqeCtx SQEContext, ioprio uint16, b []byte, off uint64, n int) error {
	if len(b) < 1 || n < 0 || uint(n) > math.MaxUint32 || n > len(b) {
		return ErrInvalidParam
	}
	payload := b[:n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	return ur.submitPacked6WithPayload(sqeCtx.WithOp(IORING_OP_WRITE), payload, ioprio, off, addr, n, 0)
}

// fadvise provides file advice.
func (ur *ioUring) fadvise(sqeCtx SQEContext, off uint64, n int, advice uint32) error {
	if n < 0 || uint(n) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FADVISE), 0, off, 0, n, advice)
}

// send sends data on a socket.
func (ur *ioUring) send(sqeCtx SQEContext, ioprio uint16, b []byte, off uint64, n int) error {
	if len(b) < 1 || n < 0 || n > math.MaxInt32 || off > uint64(len(b)) || uint64(n) > uint64(len(b))-off {
		return ErrInvalidParam
	}
	start := int(off)
	payload := b[start : start+n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	return ur.submitPacked6WithPayload(sqeCtx.WithOp(IORING_OP_SEND), payload, ioprio, 0, addr, n, 0)
}

// receive receives data from a socket.
func (ur *ioUring) receive(sqeCtx SQEContext, ioprio uint16, b []byte, off uint64, n int) error {
	if len(b) < 1 || n < 0 || n > math.MaxInt32 || off > uint64(len(b)) || uint64(n) > uint64(len(b))-off {
		return ErrInvalidParam
	}
	start := int(off)
	payload := b[start : start+n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	return ur.submitPacked6WithPayload(sqeCtx.WithOp(IORING_OP_RECV), payload, ioprio, 0, addr, n, MSG_WAITALL)
}

// receiveWithBufferSelect receives with kernel buffer selection.
func (ur *ioUring) receiveWithBufferSelect(sqeCtx SQEContext, ioprio uint16, bufSize int, bufGroup uint16) error {
	if bufSize <= 0 || bufSize > math.MaxInt32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// splice transfers data between file descriptors.
func (ur *ioUring) splice(sqeCtx SQEContext, fdIn iofd.FD, offIn, offOut *int64, n int, uflags uint32) error {
	if n < 0 || uint(n) > math.MaxUint32 {
		return ErrInvalidParam
	}
	var offInVal, offOutVal uint64
	if offIn != nil {
		offInVal = uint64(uintptr(unsafe.Pointer(offIn)))
	}
	if offOut != nil {
		offOutVal = uint64(uintptr(unsafe.Pointer(offOut)))
	}
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_SPLICE), 0, offOutVal, offInVal, n, uflags, 0, int32(fdIn))
}

// provideBuffers provides buffers to the kernel.
func (ur *ioUring) provideBuffers(sqeCtx SQEContext, nbufs int, bid int, ptr unsafe.Pointer, bufLen int, bgid uint16) error {
	return ErrNotSupported
}

// tee duplicates data between pipes.
func (ur *ioUring) tee(sqeCtx SQEContext, fdIn iofd.FD, length int, uflags uint32) error {
	if length < 0 || uint(length) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_TEE), 0, 0, 0, length, uflags, 0, int32(fdIn))
}

// shutdown shuts down a socket.
func (ur *ioUring) shutdown(sqeCtx SQEContext, how int) error {
	if how != SHUT_RD && how != SHUT_WR && how != SHUT_RDWR {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SHUTDOWN), 0, 0, 0, how, 0)
}

// socket creates a socket.
func (ur *ioUring) socket(sqeCtx SQEContext, domain, typ, proto int) error {
	if domain < 0 || domain > math.MaxInt32 || typ < 0 || typ > math.MaxInt32 || proto < 0 || proto > math.MaxInt32 {
		return ErrInvalidParam
	}
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_SOCKET), 0, uint64(typ)|uint64(proto)<<32, 0, domain, 0, 0, 0)
}

// socketDirect creates a socket directly into a registered file table slot (Darwin stub).
func (ur *ioUring) socketDirect(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32) error {
	if domain < 0 || domain > math.MaxInt32 || typ < 0 || typ > math.MaxInt32 || proto < 0 || proto > math.MaxInt32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// sendZeroCopy sends with zero-copy.
func (ur *ioUring) sendZeroCopy(sqeCtx SQEContext, b []byte, off uint64, n int, uflags int) error {
	if len(b) < 1 || n < 0 || n > math.MaxInt32 || off > uint64(len(b)) || uint64(n) > uint64(len(b))-off {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(b)))) + off
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND_ZC), 0, 0, addr, n, uint32(uflags))
}

// sendFixed submits ordinary SEND from registered buffer storage.
func (ur *ioUring) sendFixed(sqeCtx SQEContext, bufIndex int, off uint64, n int, uflags int) error {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return ErrInvalidParam
	}
	buf := ur.bufs[bufIndex]
	if n < 0 || n > math.MaxInt32 || off > uint64(len(buf)) || uint64(n) > uint64(len(buf))-off {
		return ErrInvalidParam
	}
	start := int(off)
	payload := buf[start : start+n]
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + off
	ctx := sqeCtx.WithOp(IORING_OP_SEND).WithBufGroup(uint16(bufIndex))
	return ur.submitPacked6WithPayload(ctx, payload, 0, 0, addr, n, uint32(uflags))
}

// sendZeroCopyFixed sends with zero-copy using registered buffer.
func (ur *ioUring) sendZeroCopyFixed(sqeCtx SQEContext, bufIndex int, off uint64, n int, uflags int) error {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return ErrInvalidParam
	}
	buf := ur.bufs[bufIndex]
	if n < 0 || n > math.MaxInt32 || off > uint64(len(buf)) || uint64(n) > uint64(len(buf))-off {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + off
	ctx := sqeCtx.WithOp(IORING_OP_SEND_ZC).WithBufGroup(uint16(bufIndex))
	return ur.submitPacked9(ctx, IORING_RECVSEND_FIXED_BUF, 0, addr, n, uint32(uflags), 0, 0)
}

// bind binds a socket to an address.
func (ur *ioUring) bind(sqeCtx SQEContext, sa Sockaddr) error {
	ptr, n, err := sockaddr(sa)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(ptr))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_BIND), 0, 0, addr, n, 0)
}

// listen starts listening on a socket.
func (ur *ioUring) listen(sqeCtx SQEContext, backlog int) error {
	if backlog < 0 || backlog > math.MaxInt32 {
		return ErrInvalidParam
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_LISTEN), 0, 0, 0, backlog, 0)
}

// readWithBufferSelect performs read with kernel buffer selection.
func (ur *ioUring) readWithBufferSelect(sqeCtx SQEContext, n int, bgid uint16) error {
	if n <= 0 || uint(n) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// pipe creates a pipe using io_uring (Darwin stub).
func (ur *ioUring) pipe(sqeCtx SQEContext, fds *[2]iofd.FD, flags uint32) error {
	return ErrNotSupported
}

// fsetxattr sets an extended attribute on a file descriptor (Darwin stub).
func (ur *ioUring) fsetxattr(sqeCtx SQEContext, name string, value []byte, flags uint32) error {
	if uint(len(value)) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// setxattr sets an extended attribute on a path (Darwin stub).
func (ur *ioUring) setxattr(sqeCtx SQEContext, path, name string, value []byte, flags uint32) error {
	if uint(len(value)) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// fgetxattr gets an extended attribute from a file descriptor (Darwin stub).
func (ur *ioUring) fgetxattr(sqeCtx SQEContext, name string, value []byte) error {
	if uint(len(value)) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// getxattr gets an extended attribute from a path (Darwin stub).
func (ur *ioUring) getxattr(sqeCtx SQEContext, path, name string, value []byte) error {
	if uint(len(value)) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// futexWait submits a futex wait operation (Darwin stub).
func (ur *ioUring) futexWait(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32) error {
	return ErrNotSupported
}

// futexWake submits a futex wake operation (Darwin stub).
func (ur *ioUring) futexWake(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32) error {
	return ErrNotSupported
}

// futexWaitV submits a vectored futex wait operation (Darwin stub).
func (ur *ioUring) futexWaitV(sqeCtx SQEContext, waitv unsafe.Pointer, count uint32, flags uint32) error {
	return ErrNotSupported
}

// waitid waits for a process state change (Darwin stub).
func (ur *ioUring) waitid(sqeCtx SQEContext, idtype int, id int, infop unsafe.Pointer, options int) error {
	return ErrNotSupported
}

// fixedFdInstall installs a fixed descriptor (Darwin stub).
func (ur *ioUring) fixedFdInstall(sqeCtx SQEContext, fixedIndex int, flags uint32) error {
	return ErrNotSupported
}

// filesUpdate updates registered files (Darwin stub).
func (ur *ioUring) filesUpdate(sqeCtx SQEContext, fds []int32, offset int) error {
	if len(fds) == 0 || uint(len(fds)) > math.MaxUint32 || offset < 0 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// uringCmd submits a generic uring command (Darwin stub).
func (ur *ioUring) uringCmd(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, cmdFlags uint32) error {
	if uint(len(cmdData)) > math.MaxUint32 {
		return ErrInvalidParam
	}
	return ErrNotSupported
}

// uringCmdInline submits an ordinary uring command with inline data (Darwin stub).
func (ur *ioUring) uringCmdInline(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, cmdFlags uint32) error {
	return ErrNotSupported
}

// nop128 submits a 128-byte NOP (Darwin stub).
func (ur *ioUring) nop128(sqeCtx SQEContext) error {
	return ErrNotSupported
}

// uringCmd128 submits a 128-byte uring command (Darwin stub).
func (ur *ioUring) uringCmd128(sqeCtx SQEContext, cmdOp uint32, cmdData []byte, cmdFlags uint32) error {
	return ErrNotSupported
}

// msgRing sends a message to another ring (Darwin stub).
func (ur *ioUring) msgRing(sqeCtx SQEContext, userData int64, res int32) error {
	return ErrNotSupported
}

// msgRingFD sends a fixed file descriptor to another ring (Darwin stub).
func (ur *ioUring) msgRingFD(sqeCtx SQEContext, srcFD uint32, dstSlot uint32, userData int64, flags uint32) error {
	return ErrNotSupported
}
