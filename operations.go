// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"math"
	"unsafe"
)

// Refactored from code.hybscloud.com/sox.

// IORING_OP_* values encode io_uring operation types in SQEs and SQEContext.
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
	IORING_OP_READV_FIXED  // Vectored read with fixed buffers
	IORING_OP_WRITEV_FIXED // Vectored write with fixed buffers
	IORING_OP_PIPE         // Create pipe
	IORING_OP_NOP128       // 128-byte NOP opcode
	IORING_OP_URING_CMD128 // 128-byte uring command opcode
)

// Timeout operation flags.
const (
	IORING_TIMEOUT_ABS = 1 << iota
	IORING_TIMEOUT_UPDATE
	IORING_TIMEOUT_BOOTTIME
	IORING_TIMEOUT_REALTIME
	IORING_LINK_TIMEOUT_UPDATE
	IORING_TIMEOUT_ETIME_SUCCESS
	IORING_TIMEOUT_MULTISHOT
	IORING_TIMEOUT_CLOCK_MASK  = IORING_TIMEOUT_BOOTTIME | IORING_TIMEOUT_REALTIME
	IORING_TIMEOUT_UPDATE_MASK = IORING_TIMEOUT_UPDATE | IORING_LINK_TIMEOUT_UPDATE
)

// Accept operation flags.
const (
	IORING_ACCEPT_MULTISHOT  = 1 << 0 // Multi-shot accept: one SQE, multiple completions
	IORING_ACCEPT_DONTWAIT   = 1 << 1 // Non-blocking accept
	IORING_ACCEPT_POLL_FIRST = 1 << 2 // Poll for connection before accepting
)

// Send/receive operation flags.
const (
	IORING_RECVSEND_POLL_FIRST  = 1 << iota // Poll before send/recv
	IORING_RECV_MULTISHOT                   // Multi-shot receive
	IORING_RECVSEND_FIXED_BUF               // Use registered buffer
	IORING_SEND_ZC_REPORT_USAGE             // Report zero-copy usage
	IORING_RECVSEND_BUNDLE                  // Bundle mode
	IORING_SEND_VECTORIZED                  // Vectorized send
)

// Notification CQE usage flags for zero-copy operations.
const (
	IORING_NOTIF_USAGE_ZC_COPIED = 1 << 31 // Data was copied instead of zero-copy
)

// nop submits a no-op operation.
func (ur *ioUring) nop(sqeCtx SQEContext) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_NOP), 0, 0, 0)
}

// readv submits a vectored read operation.
func (ur *ioUring) readv(sqeCtx SQEContext, iov [][]byte) error {
	if iov == nil || len(iov) < 1 {
		return ErrInvalidParam
	}
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	vec := slot.keep.iovsFromBytes(iov)
	ptr, n := ioVecAddrLen(vec)
	ctx := sqeCtx.WithOp(IORING_OP_READV)
	slot.fill6(ctx, 0, 0, uint64(uintptr(ptr)), n, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// writev submits a vectored write operation.
func (ur *ioUring) writev(sqeCtx SQEContext, iov [][]byte) error {
	if iov == nil || len(iov) < 1 {
		return ErrInvalidParam
	}
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	vec := slot.keep.iovsFromBytes(iov)
	ptr, n := ioVecAddrLen(vec)
	ctx := sqeCtx.WithOp(IORING_OP_WRITEV)
	slot.fill6(ctx, 0, 0, uint64(uintptr(ptr)), n, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// fsync submits a file sync operation.
func (ur *ioUring) fsync(sqeCtx SQEContext, uflags uint32) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FSYNC), 0, 0, 0, 0, uflags)
}

// readFixed submits a read with registered buffer.
func (ur *ioUring) readFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int) ([]byte, error) {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return nil, ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(ur.bufs[bufIndex]))))
	ctx := sqeCtx.WithOp(IORING_OP_READ_FIXED).WithBufGroup(uint16(bufIndex))
	return ur.bufs[bufIndex], ur.submitPacked9(ctx, 0, offset, addr, n, 0, 0, 0)
}

// writeFixed submits a write with registered buffer.
func (ur *ioUring) writeFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int) error {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) || n < 0 || n > len(ur.bufs[bufIndex]) {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(ur.bufs[bufIndex]))))
	ctx := sqeCtx.WithOp(IORING_OP_WRITE_FIXED).WithBufGroup(uint16(bufIndex))
	return ur.submitPacked9(ctx, 0, offset, addr, n, 0, 0, 0)
}

// pollAdd adds a file descriptor to the poll set.
func (ur *ioUring) pollAdd(sqeCtx SQEContext, how int, events int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_POLL_ADD), 0, 0, 0, how, uint32(events))
}

// pollRemove removes a file descriptor from the poll set.
func (ur *ioUring) pollRemove(sqeCtx SQEContext) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_POLL_REMOVE), 0, 0, 0)
}

// pollUpdate modifies an existing poll request in place.
// In update mode, POLL_REMOVE uses addr=oldUserData, off=newUserData,
// len=updateFlags, and uflags=newEvents.
func (ur *ioUring) pollUpdate(sqeCtx SQEContext, oldUserData, newUserData uint64, newEvents, updateFlags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_POLL_REMOVE), 0, newUserData, oldUserData, updateFlags, uint32(newEvents))
}

// syncFileRange syncs a range of a file to storage.
func (ur *ioUring) syncFileRange(sqeCtx SQEContext, offset uint64, n int, uflags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SYNC_FILE_RANGE), 0, offset, 0, n, uint32(uflags))
}

// sendmsg submits SENDMSG. Slot-owned staging keeps the msghdr, iovecs,
// destination address, and control buffer live until kernel consumption.
func (ur *ioUring) sendmsg(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte, to Sockaddr) error {
	saPtr, saN, err := sockaddr(to)
	if err != nil {
		return err
	}
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(oob)
	slot.keep.retainSockaddr(to)
	vec := slot.keep.iovsFromBytes(buffers)
	msg := &slot.keep.ensureExt(ur).msg
	*msg = Msghdr{
		Name:       (*byte)(saPtr),
		Namelen:    uint32(saN),
		Iov:        (*IoVec)(unsafe.Pointer(unsafe.SliceData(vec))),
		Iovlen:     uint64(len(vec)),
		Control:    nil,
		Controllen: 0,
	}
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.Controllen = uint64(len(oob))
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(msg)))
	ctx := sqeCtx.WithOp(IORING_OP_SENDMSG)
	slot.fill6(ctx, ioprio, 0, msgAddr, 1, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// recvmsg receives a message with control data.
func (ur *ioUring) recvmsg(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(oob)
	vec := slot.keep.iovsFromBytes(buffers)
	ext := slot.keep.ensureExt(ur)
	from := &ext.recvSockaddr
	msg := &ext.msg
	*msg = Msghdr{
		Name:       (*byte)(unsafe.Pointer(from)),
		Namelen:    uint32(SizeofSockaddrAny),
		Iov:        (*IoVec)(unsafe.Pointer(unsafe.SliceData(vec))),
		Iovlen:     uint64(len(vec)),
		Control:    nil,
		Controllen: 0,
	}
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.Controllen = uint64(len(oob))
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(msg)))
	ctx := sqeCtx.WithOp(IORING_OP_RECVMSG)
	slot.fill6(ctx, ioprio, 0, msgAddr, 1, MSG_WAITALL)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// timeout submits a timeout operation.
func (ur *ioUring) timeout(sqeCtx SQEContext, cnt int, ts *Timespec, uflags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr := uint64(0)
	if ts != nil {
		slot.keep.timespec = *ts
		addr = uint64(uintptr(unsafe.Pointer(&slot.keep.timespec)))
	}
	ctx := sqeCtx.WithOp(IORING_OP_TIMEOUT)
	slot.fill6(ctx, 0, uint64(cnt), addr, 1, uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// timeoutUpdate updates an existing timeout.
func (ur *ioUring) timeoutUpdate(sqeCtx SQEContext, userData uint64, ts *Timespec, uflags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr2 := uint64(0)
	if ts != nil {
		slot.keep.timespec = *ts
		addr2 = uint64(uintptr(unsafe.Pointer(&slot.keep.timespec)))
	}
	ctx := sqeCtx.WithOp(IORING_OP_TIMEOUT_REMOVE)
	slot.fill6(ctx, 0, addr2, userData, 0, uint32(uflags|IORING_TIMEOUT_UPDATE))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// timeoutRemove removes a timeout.
func (ur *ioUring) timeoutRemove(sqeCtx SQEContext, userData uint64, uflags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_TIMEOUT_REMOVE), 0, 0, userData, 0, uint32(uflags))
}

// accept submits an accept operation.
func (ur *ioUring) accept(sqeCtx SQEContext, ioprio uint16) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ACCEPT), ioprio, 0, 0, 0, SOCK_NONBLOCK|SOCK_CLOEXEC)
}

// acceptDirect submits ACCEPT into a registered file slot. fileIndex is a
// zero-based slot number or IORING_FILE_INDEX_ALLOC. SOCK_CLOEXEC is not valid.
func (ur *ioUring) acceptDirect(sqeCtx SQEContext, ioprio uint16, fileIndex uint32) error {
	// The kernel uses 0 for "not direct" and N+1 for slot N.
	kernelIndex := fileIndex
	if fileIndex != IORING_FILE_INDEX_ALLOC {
		kernelIndex = fileIndex + 1
	}
	// Direct accept allows SOCK_NONBLOCK but rejects SOCK_CLOEXEC.
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_ACCEPT), ioprio, 0, 0, 0, SOCK_NONBLOCK, 0, int32(kernelIndex))
}

// asyncCancel cancels a pending async operation.
func (ur *ioUring) asyncCancel(sqeCtx SQEContext, targetUserData uint64) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_ASYNC_CANCEL), 0, targetUserData, 0)
}

// asyncCancelExt submits ASYNC_CANCEL with extended match flags.
// The SQE uses addr=userData, uflags=cancelFlags, fd=sqeCtx.FD(), and
// len=targetOpcode when opcode matching is requested.
func (ur *ioUring) asyncCancelExt(sqeCtx SQEContext, targetUserData uint64, targetOpcode uint8, cancelFlags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ASYNC_CANCEL), 0, 0, targetUserData, int(targetOpcode), uint32(cancelFlags))
}

// linkTimeout creates a linked timeout operation.
func (ur *ioUring) linkTimeout(sqeCtx SQEContext, ts *Timespec, uflags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr := uint64(0)
	if ts != nil {
		slot.keep.timespec = *ts
		addr = uint64(uintptr(unsafe.Pointer(&slot.keep.timespec)))
	}
	ctx := sqeCtx.WithOp(IORING_OP_LINK_TIMEOUT)
	slot.fill6(ctx, 0, 0, addr, 1, uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// connect initiates a connection.
func (ur *ioUring) connect(sqeCtx SQEContext, sa Sockaddr) error {
	saPtr, saN, err := sockaddr(sa)
	if err != nil {
		return err
	}
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainSockaddr(sa)
	ctx := sqeCtx.WithOp(IORING_OP_CONNECT)
	slot.fill6(ctx, 0, uint64(saN), uint64(uintptr(saPtr)), 0, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// fAllocate allocates space for a file.
func (ur *ioUring) fAllocate(sqeCtx SQEContext, mode uint32, offset uint64, length int64) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FALLOCATE), 0, offset, uint64(length), int(mode), 0)
}

// openAt opens a file.
func (ur *ioUring) openAt(sqeCtx SQEContext, pathname string, uflags int, mode uint32) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr, err := slot.keep.stagePath1(ur, pathname)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	ctx := sqeCtx.WithOp(IORING_OP_OPENAT)
	slot.fill6(ctx, 0, 0, addr, int(mode), uint32(uflags|O_LARGEFILE))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// close closes a file descriptor.
func (ur *ioUring) close(sqeCtx SQEContext) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_CLOSE), 0, 0, 0)
}

// statx gets file status.
func (ur *ioUring) statx(sqeCtx SQEContext, path string, uflags int, mask int, stat *Statx) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr, err := slot.keep.stagePath1(ur, path)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	slot.keep.ensureExt(ur).statx = stat
	off := uint64(uintptr(unsafe.Pointer(stat)))
	ctx := sqeCtx.WithOp(IORING_OP_STATX)
	slot.fill6(ctx, 0, off, addr, mask, uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// read submits a read operation.
func (ur *ioUring) read(sqeCtx SQEContext, ioprio uint16, p []byte, offset uint64, n int) error {
	if p == nil || len(p) < 1 {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_READ), ioprio, offset, addr, n, 0)
}

// readWithBufferSelect submits a read with kernel buffer selection.
func (ur *ioUring) readWithBufferSelect(sqeCtx SQEContext, n int, group uint16) error {
	ctx := sqeCtx.WithOp(IORING_OP_READ).
		WithFlags(sqeCtx.Flags() | IOSQE_BUFFER_SELECT).
		WithBufGroup(group)
	return ur.submitPacked9(ctx, 0, 0, 0, n, 0, 0, 0)
}

// write submits a write operation.
func (ur *ioUring) write(sqeCtx SQEContext, ioprio uint16, p []byte, offset uint64, n int) error {
	if p == nil || len(p) < 1 {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_WRITE), ioprio, offset, addr, n, 0)
}

// fadvise provides file access advice.
func (ur *ioUring) fadvise(sqeCtx SQEContext, offset uint64, n int, advice int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FADVISE), 0, offset, 0, n, uint32(advice))
}

// madvise provides memory advice.
func (ur *ioUring) madvise(sqeCtx SQEContext, b []byte, advice int) error {
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(b))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_MADVISE), 0, 0, addr, len(b), uint32(advice))
}

// send submits a send operation.
func (ur *ioUring) send(sqeCtx SQEContext, ioprio uint16, p []byte, offset uint64, n int) error {
	if p == nil || len(p) < 1 {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND), ioprio, offset, addr, n, 0)
}

// receive submits a receive operation with MSG_WAITALL semantics.
// For file reads, use read/readv/readFixed instead; those leave rw_flags clear.
func (ur *ioUring) receive(sqeCtx SQEContext, ioprio uint16, p []byte, offset uint64, n int) error {
	if p == nil || len(p) < 1 {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_RECV), ioprio, offset, addr, n, MSG_WAITALL)
}

// receiveWithBufferSelect submits a receive with kernel buffer selection.
func (ur *ioUring) receiveWithBufferSelect(sqeCtx SQEContext, ioprio uint16, n int, group uint16) error {
	ctx := sqeCtx.WithOp(IORING_OP_RECV).
		WithFlags(sqeCtx.Flags() | IOSQE_BUFFER_SELECT).
		WithBufGroup(group)
	return ur.submitPacked9(ctx, ioprio, 0, 0, n, 0, 0, 0)
}

// receiveBundle submits a bundle receive with IORING_RECVSEND_BUNDLE flag.
// Grabs multiple contiguous buffers from buffer group in a single operation.
// The CQE result contains the number of buffers consumed.
func (ur *ioUring) receiveBundle(sqeCtx SQEContext, ioprio uint16, n int, group uint16) error {
	ctx := sqeCtx.WithOp(IORING_OP_RECV).
		WithFlags(sqeCtx.Flags() | IOSQE_BUFFER_SELECT).
		WithBufGroup(group)
	return ur.submitPacked9(ctx, ioprio|IORING_RECVSEND_BUNDLE, 0, 0, n, 0, 0, 0)
}

// openAt2 opens a file with extended options.
func (ur *ioUring) openAt2(sqeCtx SQEContext, pathname string, how *OpenHow) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr, err := slot.keep.stagePath1(ur, pathname)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	slot.keep.ensureExt(ur).openHow = how
	off := uint64(uintptr(unsafe.Pointer(how)))
	ctx := sqeCtx.WithOp(IORING_OP_OPENAT2)
	slot.fill6(ctx, 0, off, addr, SizeofOpenHow, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// epollCtl controls an epoll file descriptor.
func (ur *ioUring) epollCtl(sqeCtx SQEContext, op int, targetFd int, events uint32) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	ext := slot.keep.ensureExt(ur)
	ext.epollEvent = EpollEvent{Events: events, Fd: int32(targetFd)}
	addr := uint64(uintptr(unsafe.Pointer(&ext.epollEvent)))
	ctx := sqeCtx.WithOp(IORING_OP_EPOLL_CTL)
	slot.fill6(ctx, 0, uint64(targetFd), addr, op, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// splice transfers data between file descriptors.
func (ur *ioUring) splice(sqeCtx SQEContext, rfd int, rOff *int64, wOff *int64, n int, uflags int) error {
	rOffVal, wOffVal := uint64(math.MaxUint64), uint64(math.MaxUint64)
	if rOff != nil {
		rOffVal = uint64(*rOff)
	}
	if wOff != nil {
		wOffVal = uint64(*wOff)
	}
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_SPLICE), 0, wOffVal, rOffVal, n, uint32(uflags), 0, int32(rfd))
}

// provideBuffers provides buffers to the kernel.
func (ur *ioUring) provideBuffers(sqeCtx SQEContext, num int, starting int, addr unsafe.Pointer, size int, group uint16) error {
	ctx := sqeCtx.WithOp(IORING_OP_PROVIDE_BUFFERS).WithBufGroup(group).withFD(int32(num))
	return ur.submitPacked9(ctx, 0, uint64(starting), uint64(uintptr(addr)), size, 0, 0, 0)
}

// removeBuffers removes buffers from the kernel.
func (ur *ioUring) removeBuffers(sqeCtx SQEContext, num int, group uint16) error {
	ctx := sqeCtx.WithOp(IORING_OP_REMOVE_BUFFERS).WithBufGroup(group).withFD(int32(num))
	return ur.submitPacked9(ctx, 0, 0, 0, 0, 0, 0, 0)
}

// tee duplicates data between pipes.
func (ur *ioUring) tee(sqeCtx SQEContext, rfd int, n int, uflags int) error {
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_TEE), 0, 0, 0, n, uint32(uflags), 0, int32(rfd))
}

// shutdown shuts down a socket.
func (ur *ioUring) shutdown(sqeCtx SQEContext, how int) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_SHUTDOWN), 0, 0, how)
}

// renameAt renames a file.
func (ur *ioUring) renameAt(sqeCtx SQEContext, oldPath, newPath string, uflags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	oldAddr, err := slot.keep.stagePath1(ur, oldPath)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	newAddr, err := slot.keep.stagePath2(ur, newPath)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	ctx := sqeCtx.WithOp(IORING_OP_RENAMEAT).withFD(AT_FDCWD)
	slot.fill6(ctx, 0, newAddr, oldAddr, AT_FDCWD, uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// unlinkAt removes a file.
func (ur *ioUring) unlinkAt(sqeCtx SQEContext, pathname string, uflags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr, err := slot.keep.stagePath1(ur, pathname)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	ctx := sqeCtx.WithOp(IORING_OP_UNLINKAT)
	slot.fill6(ctx, 0, 0, addr, 0, uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// mkdirAt creates a directory.
func (ur *ioUring) mkdirAt(sqeCtx SQEContext, pathname string, uflags int, mode uint32) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	addr, err := slot.keep.stagePath1(ur, pathname)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	ctx := sqeCtx.WithOp(IORING_OP_MKDIRAT)
	slot.fill6(ctx, 0, 0, addr, int(mode), uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// symlinkAt creates a symbolic link.
func (ur *ioUring) symlinkAt(sqeCtx SQEContext, oldPath string, newPath string) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	oldAddr, err := slot.keep.stagePath1(ur, oldPath)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	newAddr, err := slot.keep.stagePath2(ur, newPath)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	ctx := sqeCtx.WithOp(IORING_OP_SYMLINKAT)
	slot.fill6(ctx, 0, newAddr, oldAddr, 0, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// linkAt creates a hard link.
func (ur *ioUring) linkAt(sqeCtx SQEContext, oldDirfd int, oldPath string, newPath string, uflags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	oldAddr, err := slot.keep.stagePath1(ur, oldPath)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	newAddr, err := slot.keep.stagePath2(ur, newPath)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	newDirfd := sqeCtx.FD()
	ctx := sqeCtx.WithOp(IORING_OP_LINKAT).withFD(int32(oldDirfd))
	slot.fill6(ctx, 0, newAddr, oldAddr, int(newDirfd), uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// msgRing sends a message to another ring.
func (ur *ioUring) msgRing(sqeCtx SQEContext, userData int64, res int32) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_MSG_RING), 0, uint64(userData), 0, int(res), 0)
}

// msgRingFD sends a fixed file from this ring to another ring.
func (ur *ioUring) msgRingFD(sqeCtx SQEContext, srcFD uint32, dstSlot uint32, userData int64, flags uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_MSG_RING)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode:     IORING_OP_MSG_RING,
		flags:      sqeCtx.Flags(),
		fd:         sqeCtx.FD(),
		off:        uint64(userData),
		addr:       IORING_MSG_SEND_FD,
		uflags:     flags,
		spliceFdIn: int32(dstSlot + 1),
		pad:        [2]uint64{uint64(srcFD)},
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// socket submits SOCKET. fileIndex==0 returns a normal fd in CQE res.
func (ur *ioUring) socket(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_SOCKET).withFD(int32(domain))
	return ur.submitPacked9(ctx, 0, uint64(typ), 0, proto, 0, 0, int32(fileIndex))
}

// socketDirect submits SOCKET into a registered file slot. fileIndex is a
// zero-based slot number or IORING_FILE_INDEX_ALLOC.
func (ur *ioUring) socketDirect(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32) error {
	// The kernel uses 0 for "not direct" and N+1 for slot N.
	kernelIndex := fileIndex
	if fileIndex != IORING_FILE_INDEX_ALLOC {
		kernelIndex = fileIndex + 1
	}
	ctx := sqeCtx.WithOp(IORING_OP_SOCKET).withFD(int32(domain))
	return ur.submitPacked9(ctx, 0, uint64(typ), 0, proto, 0, 0, int32(kernelIndex))
}

// sendZeroCopy submits SEND_ZC.
func (ur *ioUring) sendZeroCopy(sqeCtx SQEContext, p []byte, offset uint64, n int, msgFlags uint32) error {
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND_ZC), 0, offset, addr, n, msgFlags)
}

// sendZeroCopyFixed submits SEND_ZC with a registered buffer.
func (ur *ioUring) sendZeroCopyFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int, msgFlags uint32) error {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return ErrInvalidParam
	}
	buf := ur.bufs[bufIndex]
	if n < 0 || offset > uint64(len(buf)) || uint64(n) > uint64(len(buf))-offset {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + offset
	ctx := sqeCtx.WithOp(IORING_OP_SEND_ZC).WithBufGroup(uint16(bufIndex))
	// Fixed-buffer SEND_ZC carries IORING_RECVSEND_FIXED_BUF in ioprio.
	return ur.submitPacked9(ctx, IORING_RECVSEND_FIXED_BUF, 0, addr, n, msgFlags, 0, 0)
}

// sendtoZeroCopy submits a zero-copy sendto operation.
func (ur *ioUring) sendtoZeroCopy(sqeCtx SQEContext, p []byte, offset uint64, n int, addr Addr, msgFlags uint32) error {
	dataAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))

	if addr != nil {
		sa := AddrToSockaddr(addr)
		saPtr, saN, err := sockaddr(sa)
		if err != nil {
			return err
		}
		slot, err := ur.reserveSubmitSlot()
		if err != nil {
			return err
		}
		slot.keep.retainSockaddr(sa)
		ctx := sqeCtx.WithOp(IORING_OP_SEND_ZC)
		slot.fill9(ctx, 0, uint64(uintptr(saPtr)), dataAddr, n, msgFlags, 0, int32(saN))
		ur.publishSubmitSlot(slot, ctx)
		return nil
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND_ZC), 0, offset, dataAddr, n, msgFlags)
}

// sendmsgZeroCopy submits SENDMSG_ZC. Slot-owned staging keeps the msghdr,
// iovecs, destination address, and control buffer live until kernel consumption.
func (ur *ioUring) sendmsgZeroCopy(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte, to Sockaddr, uflags int) error {
	saPtr, saN, err := sockaddr(to)
	if err != nil {
		return err
	}
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(oob)
	slot.keep.retainSockaddr(to)
	vec := slot.keep.iovsFromBytes(buffers)
	msg := &slot.keep.ensureExt(ur).msg
	*msg = Msghdr{
		Name:       (*byte)(saPtr),
		Namelen:    uint32(saN),
		Iov:        (*IoVec)(unsafe.Pointer(unsafe.SliceData(vec))),
		Iovlen:     uint64(len(vec)),
		Control:    nil,
		Controllen: 0,
	}
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.Controllen = uint64(len(oob))
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(msg)))
	ctx := sqeCtx.WithOp(IORING_OP_SENDMSG_ZC)
	slot.fill6(ctx, ioprio, 0, msgAddr, 1, uint32(uflags))
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// readMultiShot submits a multi-shot read operation.
func (ur *ioUring) readMultiShot(sqeCtx SQEContext, offset uint64, n int, group uint16) error {
	ctx := sqeCtx.WithOp(IORING_OP_READ_MULTISHOT).
		WithFlags(sqeCtx.Flags() | IOSQE_BUFFER_SELECT).
		WithBufGroup(group)
	return ur.submitPacked9(ctx, 0, offset, 0, n, 0, 0, 0)
}

// fTruncate truncates a file.
func (ur *ioUring) fTruncate(sqeCtx SQEContext, length int64) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FTRUNCATE), 0, uint64(length), 0, 0, 0)
}

// bind binds a socket to an address.
func (ur *ioUring) bind(sqeCtx SQEContext, sa Sockaddr) error {
	saPtr, saN, err := sockaddr(sa)
	if err != nil {
		return err
	}
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainSockaddr(sa)
	ctx := sqeCtx.WithOp(IORING_OP_BIND)
	slot.fill6(ctx, 0, uint64(saN), uint64(uintptr(saPtr)), 0, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// listen starts listening on a socket.
func (ur *ioUring) listen(sqeCtx SQEContext, backlog int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_LISTEN), 0, 0, 0, backlog, 0)
}

// receiveZeroCopy submits RECV_ZC for a registered ZCRX interface queue.
func (ur *ioUring) receiveZeroCopy(sqeCtx SQEContext, ioprio uint16, n int, zcrxIfqIdx uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_RECV_ZC)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode:     IORING_OP_RECV_ZC,
		flags:      sqeCtx.Flags(),
		ioprio:     ioprio,
		fd:         sqeCtx.FD(),
		len:        uint32(n),
		spliceFdIn: int32(zcrxIfqIdx),
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// epollWait submits EPOLL_WAIT through io_uring.
func (ur *ioUring) epollWait(sqeCtx SQEContext, events *EpollEvent, maxEvents int, timeout int32) error {
	if timeout != 0 {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(events)))
	ctx := sqeCtx.WithOp(IORING_OP_EPOLL_WAIT)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_EPOLL_WAIT,
		flags:  sqeCtx.Flags(),
		fd:     sqeCtx.FD(),
		addr:   addr,
		len:    uint32(maxEvents),
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// readvFixed submits a vectored read operation using registered buffers.
// All iovecs must point to registered buffer memory.
func (ur *ioUring) readvFixed(sqeCtx SQEContext, offset uint64, bufIndices []int) error {
	n := len(bufIndices)
	if n < 1 {
		return ErrInvalidParam
	}

	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	iov := slot.keep.allocIovs(n)

	for i, idx := range bufIndices {
		if idx < 0 || idx >= len(ur.bufs) {
			ur.abortSubmitSlot(slot)
			return ErrInvalidParam
		}
		iov[i] = IoVec{
			Base: unsafe.SliceData(ur.bufs[idx]),
			Len:  uint64(len(ur.bufs[idx])),
		}
	}
	addr := uint64(uintptr(unsafe.Pointer(&iov[0])))
	ctx := sqeCtx.WithOp(IORING_OP_READV_FIXED)
	slot.fill6(ctx, 0, offset, addr, n, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// writevFixed submits a vectored write operation using registered buffers.
// All iovecs must point to registered buffer memory.
func (ur *ioUring) writevFixed(sqeCtx SQEContext, offset uint64, bufIndices []int, lengths []int) error {
	n := len(bufIndices)
	if n < 1 || n != len(lengths) {
		return ErrInvalidParam
	}

	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	iov := slot.keep.allocIovs(n)

	for i, idx := range bufIndices {
		if idx < 0 || idx >= len(ur.bufs) || lengths[i] > len(ur.bufs[idx]) {
			ur.abortSubmitSlot(slot)
			return ErrInvalidParam
		}
		iov[i] = IoVec{
			Base: unsafe.SliceData(ur.bufs[idx]),
			Len:  uint64(lengths[i]),
		}
	}
	addr := uint64(uintptr(unsafe.Pointer(&iov[0])))
	ctx := sqeCtx.WithOp(IORING_OP_WRITEV_FIXED)
	slot.fill6(ctx, 0, offset, addr, n, 0)
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// pipe creates a pipe using io_uring.
// The fds parameter must point to an int32[2] array where the kernel
// will write the read end (fds[0]) and write end (fds[1]) file descriptors.
// The fd field in sqeCtx is not used.
func (ur *ioUring) pipe(sqeCtx SQEContext, fds *[2]int32, flags uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_PIPE)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_PIPE,
		flags:  sqeCtx.Flags(),
		addr:   uint64(uintptr(unsafe.Pointer(fds))),
		uflags: flags,
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// fsetxattr sets an extended attribute on a file descriptor.
func (ur *ioUring) fsetxattr(sqeCtx SQEContext, name string, value []byte, flags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(value)
	nameAddr, err := slot.keep.stagePath1(ur, name)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	valueAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(value))))
	ctx := sqeCtx.WithOp(IORING_OP_FSETXATTR)
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FSETXATTR,
		flags:  sqeCtx.Flags(),
		fd:     sqeCtx.FD(),
		off:    valueAddr,
		addr:   nameAddr,
		len:    uint32(len(value)),
		uflags: uint32(flags),
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// setxattr sets an extended attribute on a path.
func (ur *ioUring) setxattr(sqeCtx SQEContext, path, name string, value []byte, flags int) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(value)
	pathAddr, err := slot.keep.stagePath1(ur, path)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	nameAddr, err := slot.keep.stagePath2(ur, name)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	valueAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(value))))
	ctx := sqeCtx.WithOp(IORING_OP_SETXATTR)
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_SETXATTR,
		flags:  sqeCtx.Flags(),
		fd:     sqeCtx.FD(),
		off:    valueAddr,
		addr:   nameAddr,
		len:    uint32(len(value)),
		uflags: uint32(flags),
		pad:    [2]uint64{pathAddr},
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// fgetxattr gets an extended attribute from a file descriptor.
func (ur *ioUring) fgetxattr(sqeCtx SQEContext, name string, value []byte) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(value)
	nameAddr, err := slot.keep.stagePath1(ur, name)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	valueAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(value))))
	ctx := sqeCtx.WithOp(IORING_OP_FGETXATTR)
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FGETXATTR,
		flags:  sqeCtx.Flags(),
		fd:     sqeCtx.FD(),
		off:    valueAddr,
		addr:   nameAddr,
		len:    uint32(len(value)),
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// getxattr gets an extended attribute from a path.
func (ur *ioUring) getxattr(sqeCtx SQEContext, path, name string, value []byte) error {
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	slot.keep.retainBytes(value)
	pathAddr, err := slot.keep.stagePath1(ur, path)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	nameAddr, err := slot.keep.stagePath2(ur, name)
	if err != nil {
		ur.abortSubmitSlot(slot)
		return err
	}
	valueAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(value))))
	ctx := sqeCtx.WithOp(IORING_OP_GETXATTR)
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_GETXATTR,
		flags:  sqeCtx.Flags(),
		fd:     sqeCtx.FD(),
		off:    valueAddr,
		addr:   nameAddr,
		len:    uint32(len(value)),
		pad:    [2]uint64{pathAddr},
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// futexWait submits a futex wait operation.
func (ur *ioUring) futexWait(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_FUTEX_WAIT)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FUTEX_WAIT,
		flags:  sqeCtx.Flags(),
		fd:     int32(flags),
		off:    val,
		addr:   uint64(uintptr(unsafe.Pointer(addr))),
		pad:    [2]uint64{mask},
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// futexWake submits a futex wake operation.
func (ur *ioUring) futexWake(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_FUTEX_WAKE)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FUTEX_WAKE,
		flags:  sqeCtx.Flags(),
		fd:     int32(flags),
		off:    val,
		addr:   uint64(uintptr(unsafe.Pointer(addr))),
		pad:    [2]uint64{mask},
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// futexWaitV submits a vectored futex wait operation.
func (ur *ioUring) futexWaitV(sqeCtx SQEContext, waitv unsafe.Pointer, count uint32, flags uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_FUTEX_WAITV)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FUTEX_WAITV,
		flags:  sqeCtx.Flags(),
		fd:     int32(flags),
		addr:   uint64(uintptr(waitv)),
		len:    count,
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// waitid submits a waitid operation.
func (ur *ioUring) waitid(sqeCtx SQEContext, idtype int, id int, infop unsafe.Pointer, options int) error {
	ctx := sqeCtx.WithOp(IORING_OP_WAITID)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_WAITID,
		flags:  sqeCtx.Flags(),
		fd:     int32(idtype),
		addr:   uint64(uintptr(infop)),
		len:    uint32(id),
		uflags: uint32(options),
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// fixedFdInstall installs a fixed descriptor into the normal file table.
func (ur *ioUring) fixedFdInstall(sqeCtx SQEContext, fixedIndex int, flags uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_FIXED_FD_INSTALL)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FIXED_FD_INSTALL,
		flags:  sqeCtx.Flags(),
		fd:     int32(fixedIndex),
		uflags: flags,
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// filesUpdate updates registered files at specified offsets.
func (ur *ioUring) filesUpdate(sqeCtx SQEContext, fds []int32, offset int) error {
	if len(fds) == 0 {
		return ErrInvalidParam
	}
	ctx := sqeCtx.WithOp(IORING_OP_FILES_UPDATE)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_FILES_UPDATE,
		flags:  sqeCtx.Flags(),
		fd:     -1,
		off:    uint64(offset),
		addr:   uint64(uintptr(unsafe.Pointer(&fds[0]))),
		len:    uint32(len(fds)),
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// uringCmd submits a generic uring command (passthrough).
func (ur *ioUring) uringCmd(sqeCtx SQEContext, cmdOp uint32, cmdData []byte) error {
	ctx := sqeCtx.WithOp(IORING_OP_URING_CMD)
	slot, err := ur.reserveSubmitSlot()
	if err != nil {
		return err
	}
	e := slot.sqe
	*e = ioUringSqe{
		opcode: IORING_OP_URING_CMD,
		flags:  sqeCtx.Flags(),
		fd:     sqeCtx.FD(),
		off:    uint64(cmdOp),
	}
	if len(cmdData) > 0 {
		e.addr = uint64(uintptr(unsafe.Pointer(&cmdData[0])))
		e.len = uint32(len(cmdData))
	}
	ur.publishSubmitSlot(slot, ctx)
	return nil
}

// nop128 returns ErrNotSupported until the ring can map 128-byte SQE slots.
func (ur *ioUring) nop128(sqeCtx SQEContext) error {
	return ErrNotSupported
}

// uringCmd128 returns ErrNotSupported until the ring can map 128-byte SQE slots.
func (ur *ioUring) uringCmd128(sqeCtx SQEContext, cmdOp uint32, cmdData []byte) error {
	return ErrNotSupported
}
