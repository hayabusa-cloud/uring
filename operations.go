// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"math"
	"unsafe"
)

// This opcode surface was split out when the `uring` package was refactored
// from `code.hybscloud.com/sox` into a dedicated `io_uring` package.

// io_uring operation opcodes.
//
// Each opcode identifies an I/O operation type. When preparing an SQE,
// the opcode is packed into the SQEContext and used by the kernel to
// determine which operation to perform.
//
// Operations are categorized as:
//   - Socket: SOCKET, BIND, LISTEN, ACCEPT, CONNECT, SEND, RECV, etc.
//   - File: READ, WRITE, OPENAT, CLOSE, FSYNC, SPLICE, TEE, etc.
//   - Control: TIMEOUT, ASYNC_CANCEL, POLL_ADD, NOP, etc.
//   - Advanced: SEND_ZC, RECV_ZC, PROVIDE_BUFFERS, etc.
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
	ptr, n := ioVecFromBytesSlice(iov)
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_READV), 0, 0, uint64(uintptr(ptr)), n, MSG_WAITALL)
}

// writev submits a vectored write operation.
func (ur *ioUring) writev(sqeCtx SQEContext, iov [][]byte) error {
	if iov == nil || len(iov) < 1 {
		return ErrInvalidParam
	}
	ptr, n := ioVecFromBytesSlice(iov)
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_WRITEV), 0, uint64(uintptr(ptr)), n)
}

// fsync submits a file sync operation.
func (ur *ioUring) fsync(sqeCtx SQEContext) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_FSYNC), 0, 0, 0)
}

// readFixed submits a read with registered buffer.
func (ur *ioUring) readFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int) ([]byte, error) {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return nil, ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(ur.bufs[bufIndex]))))
	ctx := sqeCtx.WithOp(IORING_OP_READ_FIXED).WithBufGroup(uint16(bufIndex))
	return ur.bufs[bufIndex], ur.submitPacked9(ctx, 0, offset, addr, n, MSG_WAITALL, 0, 0)
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

// pollUpdate modifies an existing poll request in-place.
// SQE field mapping for POLL_REMOVE (update mode):
//   - addr: oldUserData (to locate existing poll)
//   - off: newUserData (if UPDATE_USER_DATA set)
//   - len: updateFlags (UPDATE_EVENTS | UPDATE_USER_DATA | ADD_MULTI)
//   - uflags: newEvents (if UPDATE_EVENTS set)
func (ur *ioUring) pollUpdate(sqeCtx SQEContext, oldUserData, newUserData uint64, newEvents, updateFlags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_POLL_REMOVE), 0, newUserData, oldUserData, updateFlags, uint32(newEvents))
}

// syncFileRange syncs a range of a file to storage.
func (ur *ioUring) syncFileRange(sqeCtx SQEContext, offset uint64, n int, uflags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SYNC_FILE_RANGE), 0, offset, 0, n, uint32(uflags))
}

// sendmsg sends a message with control data.
func (ur *ioUring) sendmsg(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte, to Sockaddr) error {
	var saPtr unsafe.Pointer
	var saN int
	var err error
	if to != nil {
		saPtr, saN, err = sockaddr(to)
		if err != nil {
			return err
		}
	}
	iovPtr, iovN := ioVecFromBytesSlice(buffers)
	msg := Msghdr{
		Name:       (*byte)(saPtr),
		Namelen:    uint32(saN),
		Iov:        (*IoVec)(iovPtr),
		Iovlen:     uint64(iovN),
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

// recvmsg receives a message with control data.
func (ur *ioUring) recvmsg(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte) error {
	from := RawSockaddrAny{}
	iovPtr, n := ioVecFromBytesSlice(buffers)
	msg := Msghdr{
		Name:       (*byte)(unsafe.Pointer(&from)),
		Namelen:    uint32(SizeofSockaddrAny),
		Iov:        (*IoVec)(iovPtr),
		Iovlen:     uint64(n),
		Control:    nil,
		Controllen: 0,
	}
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.Controllen = uint64(len(oob))
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(&msg)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_RECVMSG), ioprio, 0, msgAddr, 1, MSG_WAITALL)
}

// timeout submits a timeout operation.
func (ur *ioUring) timeout(sqeCtx SQEContext, cnt int, ts *Timespec, uflags int) error {
	addr := uint64(uintptr(unsafe.Pointer(ts)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_TIMEOUT), 0, uint64(cnt), addr, 1, uint32(uflags))
}

// timeoutUpdate updates an existing timeout.
func (ur *ioUring) timeoutUpdate(sqeCtx SQEContext, userData uint64, ts *Timespec, uflags int) error {
	addr2 := uint64(uintptr(unsafe.Pointer(ts)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_TIMEOUT_REMOVE), 0, addr2, userData, 0, uint32(uflags|IORING_TIMEOUT_UPDATE))
}

// timeoutRemove removes a timeout.
func (ur *ioUring) timeoutRemove(sqeCtx SQEContext, userData uint64, uflags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_TIMEOUT_REMOVE), 0, 0, userData, 0, uint32(uflags))
}

// accept submits an accept operation.
func (ur *ioUring) accept(sqeCtx SQEContext, ioprio uint16) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ACCEPT), ioprio, 0, 0, 0, SOCK_NONBLOCK|SOCK_CLOEXEC)
}

// acceptDirect submits an accept operation that stores the result directly
// into a registered file table slot. The fileIndex parameter specifies which
// slot to use (0-based), or IORING_FILE_INDEX_ALLOC for auto-allocation.
// Note: SOCK_CLOEXEC is not supported with direct accept.
func (ur *ioUring) acceptDirect(sqeCtx SQEContext, ioprio uint16, fileIndex uint32) error {
	// Convert fileIndex: kernel expects 0 = no direct, N+1 = slot N
	// For IORING_FILE_INDEX_ALLOC (0xFFFFFFFF), it's handled specially by kernel
	kernelIndex := fileIndex
	if fileIndex != IORING_FILE_INDEX_ALLOC {
		kernelIndex = fileIndex + 1
	}
	// SOCK_NONBLOCK only - SOCK_CLOEXEC causes -EINVAL with direct accept
	return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_ACCEPT), ioprio, 0, 0, 0, SOCK_NONBLOCK, 0, int32(kernelIndex))
}

// asyncCancel cancels a pending async operation.
func (ur *ioUring) asyncCancel(sqeCtx SQEContext, targetUserData uint64) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_ASYNC_CANCEL), 0, targetUserData, 0)
}

// asyncCancelExt cancels operations with extended matching modes.
// SQE field mapping for IORING_OP_ASYNC_CANCEL:
//   - addr: userData to match (if CANCEL_USERDATA set or default)
//   - uflags: cancel_flags (ALL, FD, ANY, FD_FIXED, USERDATA, OP)
//   - fd: FD to match (if CANCEL_FD set) - from sqeCtx
//   - len: opcode to match (if CANCEL_OP set)
func (ur *ioUring) asyncCancelExt(sqeCtx SQEContext, targetUserData uint64, targetOpcode uint8, cancelFlags int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_ASYNC_CANCEL), 0, 0, targetUserData, int(targetOpcode), uint32(cancelFlags))
}

// linkTimeout creates a linked timeout operation.
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
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_CONNECT), 0, uint64(n), uint64(uintptr(ptr)), 0, 0)
}

// fAllocate allocates space for a file.
func (ur *ioUring) fAllocate(sqeCtx SQEContext, mode uint32, offset uint64, length int64) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_FALLOCATE), 0, offset, uint64(length), int(mode), 0)
}

// openAt opens a file.
func (ur *ioUring) openAt(sqeCtx SQEContext, pathname string, uflags int, mode uint32) error {
	ptr, err := bytePtrFromString(pathname)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(unsafe.Pointer(ptr)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_OPENAT), 0, 0, addr, int(mode), uint32(uflags|O_LARGEFILE))
}

// close closes a file descriptor.
func (ur *ioUring) close(sqeCtx SQEContext) error {
	return ur.submitPacked3(sqeCtx.WithOp(IORING_OP_CLOSE), 0, 0, 0)
}

// statx gets file status.
func (ur *ioUring) statx(sqeCtx SQEContext, path string, uflags int, mask int, stat *Statx_t) error {
	ptr, err := bytePtrFromString(path)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(unsafe.Pointer(ptr)))
	off := uint64(uintptr(unsafe.Pointer(stat)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_STATX), 0, off, addr, mask, uint32(uflags))
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
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND), 0, offset, addr, n, 0)
}

// receive submits a receive operation.
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
	ptr, err := bytePtrFromString(pathname)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(unsafe.Pointer(ptr)))
	off := uint64(uintptr(unsafe.Pointer(how)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_OPENAT2), 0, off, addr, SizeofOpenHow, 0)
}

// epollCtl controls an epoll file descriptor.
func (ur *ioUring) epollCtl(sqeCtx SQEContext, op int, targetFd int, events uint32) error {
	e := EpollEvent{Events: events, Fd: int32(targetFd)}
	addr := uint64(uintptr(unsafe.Pointer(&e)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_EPOLL_CTL), 0, uint64(targetFd), addr, op, 0)
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
	oldPtr, err := bytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	oldAddr := uint64(uintptr(unsafe.Pointer(oldPtr)))
	newPtr, err := bytePtrFromString(newPath)
	if err != nil {
		return err
	}
	newAddr := uint64(uintptr(unsafe.Pointer(newPtr)))
	ctx := sqeCtx.WithOp(IORING_OP_RENAMEAT).withFD(AT_FDCWD)
	return ur.submitPacked6(ctx, 0, newAddr, oldAddr, AT_FDCWD, uint32(uflags))
}

// unlinkAt removes a file.
func (ur *ioUring) unlinkAt(sqeCtx SQEContext, pathname string, uflags int) error {
	ptr, err := bytePtrFromString(pathname)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(unsafe.Pointer(ptr)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_UNLINKAT), 0, 0, addr, 0, uint32(uflags))
}

// mkdirAt creates a directory.
func (ur *ioUring) mkdirAt(sqeCtx SQEContext, pathname string, uflags int, mode uint32) error {
	ptr, err := bytePtrFromString(pathname)
	if err != nil {
		return err
	}
	addr := uint64(uintptr(unsafe.Pointer(ptr)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_MKDIRAT), 0, 0, addr, int(mode), uint32(uflags))
}

// symlinkAt creates a symbolic link.
func (ur *ioUring) symlinkAt(sqeCtx SQEContext, oldPath string, newPath string) error {
	ptr, err := bytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	oldAddr := uint64(uintptr(unsafe.Pointer(ptr)))

	ptr, err = bytePtrFromString(newPath)
	if err != nil {
		return err
	}
	newAddr := uint64(uintptr(unsafe.Pointer(ptr)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SYMLINKAT), 0, newAddr, oldAddr, 0, 0)
}

// linkAt creates a hard link.
func (ur *ioUring) linkAt(sqeCtx SQEContext, oldDirfd int, oldPath string, newPath string, uflags int) error {
	ptr, err := bytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	oldAddr := uint64(uintptr(unsafe.Pointer(ptr)))

	ptr, err = bytePtrFromString(newPath)
	if err != nil {
		return err
	}
	newAddr := uint64(uintptr(unsafe.Pointer(ptr)))
	newDirfd := sqeCtx.FD()
	ctx := sqeCtx.WithOp(IORING_OP_LINKAT).withFD(int32(oldDirfd))
	return ur.submitPacked6(ctx, 0, newAddr, oldAddr, int(newDirfd), uint32(uflags))
}

// msgRing sends a message to another ring.
func (ur *ioUring) msgRing(sqeCtx SQEContext, userData int64, res int32) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_MSG_RING), 0, uint64(userData), 0, int(res), 0)
}

// msgRingFD sends a fixed file descriptor to another ring.
// srcFD is the fixed file index in the source ring.
// dstSlot is the fixed file slot in the target ring.
// userData is passed to the target ring's CQE.
// flags can include IORING_MSG_RING_CQE_SKIP.
func (ur *ioUring) msgRingFD(sqeCtx SQEContext, srcFD uint32, dstSlot uint32, userData int64, flags uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_MSG_RING), func(e *ioUringSqe) {
		e.opcode = IORING_OP_MSG_RING
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()                // Target ring FD
		e.off = uint64(userData)          // userData for target CQE
		e.addr = IORING_MSG_SEND_FD       // Command type
		e.uflags = flags                  // msg_ring_flags
		e.spliceFdIn = int32(dstSlot + 1) // dst fixed file slot (kernel expects N+1)
		e.pad[0] = uint64(srcFD)          // src fixed file index (addr3)
	})
}

// socket creates a socket.
// fileIndex of 0 means regular socket (FD returned in CQE res).
// Non-zero fileIndex is used for backward compatibility but doesn't encode properly.
func (ur *ioUring) socket(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32) error {
	ctx := sqeCtx.WithOp(IORING_OP_SOCKET).withFD(int32(domain))
	return ur.submitPacked9(ctx, 0, uint64(typ), 0, proto, 0, 0, int32(fileIndex))
}

// socketDirect creates a socket directly into a registered file table slot.
// The fileIndex parameter specifies which slot to use (0-based), or
// IORING_FILE_INDEX_ALLOC for auto-allocation (returns slot in CQE res).
// Requires registered files via RegisterFiles or RegisterFilesSparse.
func (ur *ioUring) socketDirect(sqeCtx SQEContext, domain, typ, proto int, fileIndex uint32) error {
	// Encode fileIndex: kernel expects 0 = no direct, N+1 = slot N
	// IORING_FILE_INDEX_ALLOC (0xFFFFFFFF) is passed through unchanged
	kernelIndex := fileIndex
	if fileIndex != IORING_FILE_INDEX_ALLOC {
		kernelIndex = fileIndex + 1
	}
	ctx := sqeCtx.WithOp(IORING_OP_SOCKET).withFD(int32(domain))
	return ur.submitPacked9(ctx, 0, uint64(typ), 0, proto, 0, 0, int32(kernelIndex))
}

// sendZeroCopy submits a zero-copy send operation.
// This uses IORING_OP_SEND_ZC to avoid copying the payload to kernel buffers.
func (ur *ioUring) sendZeroCopy(sqeCtx SQEContext, p []byte, offset uint64, n int, msgFlags uint32) error {
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND_ZC), 0, offset, addr, n, msgFlags)
}

// sendZeroCopyFixed submits a zero-copy send using a registered buffer.
// This combines zero-copy semantics with pre-registered buffers for maximum efficiency.
// Use this for broadcast scenarios where the same buffer is sent to many connections.
//
// Parameters:
//   - sqeCtx: packed context with FD set to the target socket
//   - bufIndex: index of the registered buffer (from IORING_REGISTER_BUFFERS2)
//   - offset: offset within the buffer
//   - n: number of bytes to send
//   - msgFlags: message flags (e.g., MSG_ZEROCOPY)
func (ur *ioUring) sendZeroCopyFixed(sqeCtx SQEContext, bufIndex int, offset uint64, n int, msgFlags uint32) error {
	if bufIndex < 0 || bufIndex >= len(ur.bufs) {
		return ErrInvalidParam
	}
	addr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(ur.bufs[bufIndex]))))
	ctx := sqeCtx.WithOp(IORING_OP_SEND_ZC).WithBufGroup(uint16(bufIndex))
	// Set IORING_RECVSEND_FIXED_BUF in ioprio field
	return ur.submitPacked6(ctx, IORING_RECVSEND_FIXED_BUF, offset, addr, n, msgFlags)
}

// sendtoZeroCopy submits a zero-copy sendto operation.
func (ur *ioUring) sendtoZeroCopy(sqeCtx SQEContext, p []byte, offset uint64, n int, addr Addr, msgFlags uint32) error {
	dataAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(p))))

	if addr != nil {
		saData, _ := sockaddrData(AddrToSockaddr(addr))
		addr2 := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(saData))))
		return ur.submitPacked9(sqeCtx.WithOp(IORING_OP_SEND_ZC), 0, addr2, dataAddr, n, msgFlags, 0, 16)
	}
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SEND_ZC), 0, offset, dataAddr, n, msgFlags)
}

// sendmsgZeroCopy sends a message with zero-copy.
func (ur *ioUring) sendmsgZeroCopy(sqeCtx SQEContext, ioprio uint16, buffers [][]byte, oob []byte, to Sockaddr, uflags int) error {
	var saPtr unsafe.Pointer
	var saN int
	var err error
	if to != nil {
		saPtr, saN, err = sockaddr(to)
		if err != nil {
			return err
		}
	}
	iovPtr, iovN := ioVecFromBytesSlice(buffers)
	msg := Msghdr{
		Name:       (*byte)(saPtr),
		Namelen:    uint32(saN),
		Iov:        (*IoVec)(iovPtr),
		Iovlen:     uint64(iovN),
		Control:    nil,
		Controllen: 0,
	}
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.Controllen = uint64(len(oob))
	}
	msgAddr := uint64(uintptr(unsafe.Pointer(&msg)))
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_SENDMSG_ZC), ioprio, 0, msgAddr, 1, uint32(uflags))
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
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_BIND), 0, uint64(saN), uint64(uintptr(saPtr)), 0, 0)
}

// listen starts listening on a socket.
func (ur *ioUring) listen(sqeCtx SQEContext, backlog int) error {
	return ur.submitPacked6(sqeCtx.WithOp(IORING_OP_LISTEN), 0, 0, 0, backlog, 0)
}

// receiveZeroCopy submits a zero-copy receive operation.
// This uses IORING_OP_RECV_ZC to avoid copying received data to userspace buffers.
// Requires a registered ZCRX interface queue backed by compatible hardware.
func (ur *ioUring) receiveZeroCopy(sqeCtx SQEContext, ioprio uint16, n int, zcrxIfqIdx uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_RECV_ZC), func(e *ioUringSqe) {
		e.opcode = IORING_OP_RECV_ZC
		e.flags = sqeCtx.Flags()
		e.ioprio = ioprio
		e.fd = sqeCtx.FD()
		e.len = uint32(n)
		e.spliceFdIn = int32(zcrxIfqIdx)
	})
}

// epollWait performs an epoll_wait operation via io_uring.
// This allows epoll monitoring to be integrated into the io_uring event loop.
func (ur *ioUring) epollWait(sqeCtx SQEContext, events *EpollEvent, maxEvents int, timeout int32) error {
	addr := uint64(uintptr(unsafe.Pointer(events)))
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_EPOLL_WAIT), func(e *ioUringSqe) {
		e.opcode = IORING_OP_EPOLL_WAIT
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()
		e.addr = addr
		e.len = uint32(maxEvents)
		e.off = uint64(uint32(timeout))
	})
}

// readvFixed submits a vectored read operation using registered buffers.
// All iovecs must point to registered buffer memory.
func (ur *ioUring) readvFixed(sqeCtx SQEContext, offset uint64, bufIndices []int) error {
	n := len(bufIndices)
	if n < 1 {
		return ErrInvalidParam
	}

	// Stack buffer for common case (≤8 buffers), heap for larger
	var stackIov [8]IoVec
	var iov []IoVec
	if n <= len(stackIov) {
		iov = stackIov[:n]
	} else {
		iov = make([]IoVec, n)
	}

	for i, idx := range bufIndices {
		if idx < 0 || idx >= len(ur.bufs) {
			return ErrInvalidParam
		}
		iov[i] = IoVec{
			Base: unsafe.SliceData(ur.bufs[idx]),
			Len:  uint64(len(ur.bufs[idx])),
		}
	}
	addr := uint64(uintptr(unsafe.Pointer(&iov[0])))
	ctx := sqeCtx.WithOp(IORING_OP_READV_FIXED)
	return ur.submitPacked6(ctx, 0, offset, addr, n, 0)
}

// writevFixed submits a vectored write operation using registered buffers.
// All iovecs must point to registered buffer memory.
func (ur *ioUring) writevFixed(sqeCtx SQEContext, offset uint64, bufIndices []int, lengths []int) error {
	n := len(bufIndices)
	if n < 1 || n != len(lengths) {
		return ErrInvalidParam
	}

	// Stack buffer for common case (≤8 buffers), heap for larger
	var stackIov [8]IoVec
	var iov []IoVec
	if n <= len(stackIov) {
		iov = stackIov[:n]
	} else {
		iov = make([]IoVec, n)
	}

	for i, idx := range bufIndices {
		if idx < 0 || idx >= len(ur.bufs) || lengths[i] > len(ur.bufs[idx]) {
			return ErrInvalidParam
		}
		iov[i] = IoVec{
			Base: unsafe.SliceData(ur.bufs[idx]),
			Len:  uint64(lengths[i]),
		}
	}
	addr := uint64(uintptr(unsafe.Pointer(&iov[0])))
	ctx := sqeCtx.WithOp(IORING_OP_WRITEV_FIXED)
	return ur.submitPacked6(ctx, 0, offset, addr, n, 0)
}

// pipe creates a pipe using io_uring.
// The fds parameter must point to an int32[2] array where the kernel
// will write the read end (fds[0]) and write end (fds[1]) file descriptors.
// The fd field in sqeCtx is not used.
func (ur *ioUring) pipe(sqeCtx SQEContext, fds *[2]int32, flags uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_PIPE), func(e *ioUringSqe) {
		e.opcode = IORING_OP_PIPE
		e.flags = sqeCtx.Flags()
		e.fd = 0 // Not used for pipe (liburing uses 0)
		e.addr = uint64(uintptr(unsafe.Pointer(fds)))
		e.uflags = flags
	})
}

// fsetxattr sets an extended attribute on a file descriptor.
func (ur *ioUring) fsetxattr(sqeCtx SQEContext, name string, value []byte, flags int) error {
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0) // null-terminate
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FSETXATTR), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FSETXATTR
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()
		e.addr = uint64(uintptr(unsafe.Pointer(&nameBytes[0])))
		e.len = uint32(len(value))
		e.off = uint64(uintptr(unsafe.Pointer(&value[0])))
		e.uflags = uint32(flags)
	})
}

// setxattr sets an extended attribute on a path.
func (ur *ioUring) setxattr(sqeCtx SQEContext, path, name string, value []byte, flags int) error {
	pathBytes := []byte(path)
	pathBytes = append(pathBytes, 0) // null-terminate
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0) // null-terminate
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_SETXATTR), func(e *ioUringSqe) {
		e.opcode = IORING_OP_SETXATTR
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD() // dirfd for relative paths
		e.addr = uint64(uintptr(unsafe.Pointer(&nameBytes[0])))
		e.len = uint32(len(value))
		e.off = uint64(uintptr(unsafe.Pointer(&value[0])))
		e.uflags = uint32(flags)
		e.pad[0] = uint64(uintptr(unsafe.Pointer(&pathBytes[0])))
	})
}

// fgetxattr gets an extended attribute from a file descriptor.
func (ur *ioUring) fgetxattr(sqeCtx SQEContext, name string, value []byte) error {
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0) // null-terminate
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FGETXATTR), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FGETXATTR
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()
		e.addr = uint64(uintptr(unsafe.Pointer(&nameBytes[0])))
		e.len = uint32(len(value))
		e.off = uint64(uintptr(unsafe.Pointer(&value[0])))
	})
}

// getxattr gets an extended attribute from a path.
func (ur *ioUring) getxattr(sqeCtx SQEContext, path, name string, value []byte) error {
	pathBytes := []byte(path)
	pathBytes = append(pathBytes, 0) // null-terminate
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0) // null-terminate
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_GETXATTR), func(e *ioUringSqe) {
		e.opcode = IORING_OP_GETXATTR
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD() // dirfd for relative paths
		e.addr = uint64(uintptr(unsafe.Pointer(&nameBytes[0])))
		e.len = uint32(len(value))
		e.off = uint64(uintptr(unsafe.Pointer(&value[0])))
		e.pad[0] = uint64(uintptr(unsafe.Pointer(&pathBytes[0])))
	})
}

// futexWait submits a futex wait operation.
func (ur *ioUring) futexWait(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FUTEX_WAIT), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FUTEX_WAIT
		e.flags = sqeCtx.Flags()
		e.fd = int32(flags) // futex_flags
		e.addr = uint64(uintptr(unsafe.Pointer(addr)))
		e.off = val     // futex expected value
		e.pad[0] = mask // futex mask (addr3 in kernel)
	})
}

// futexWake submits a futex wake operation.
func (ur *ioUring) futexWake(sqeCtx SQEContext, addr *uint32, val uint64, mask uint64, flags uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FUTEX_WAKE), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FUTEX_WAKE
		e.flags = sqeCtx.Flags()
		e.fd = int32(flags) // futex_flags
		e.addr = uint64(uintptr(unsafe.Pointer(addr)))
		e.off = val     // number to wake
		e.pad[0] = mask // futex mask (addr3 in kernel)
	})
}

// futexWaitV submits a vectored futex wait operation.
func (ur *ioUring) futexWaitV(sqeCtx SQEContext, waitv unsafe.Pointer, count uint32, flags uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FUTEX_WAITV), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FUTEX_WAITV
		e.flags = sqeCtx.Flags()
		e.fd = int32(flags)
		e.addr = uint64(uintptr(waitv))
		e.len = count
	})
}

// waitid submits a waitid operation.
func (ur *ioUring) waitid(sqeCtx SQEContext, idtype int, id int, infop unsafe.Pointer, options int) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_WAITID), func(e *ioUringSqe) {
		e.opcode = IORING_OP_WAITID
		e.flags = sqeCtx.Flags()
		e.fd = int32(idtype) // P_PID, P_PGID, P_ALL, etc.
		e.len = uint32(id)
		e.addr = uint64(uintptr(infop)) // siginfo_t pointer
		e.uflags = uint32(options)
	})
}

// fixedFdInstall installs a fixed descriptor into the normal file table.
func (ur *ioUring) fixedFdInstall(sqeCtx SQEContext, fixedIndex int, flags uint32) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FIXED_FD_INSTALL), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FIXED_FD_INSTALL
		e.flags = sqeCtx.Flags()
		e.fd = int32(fixedIndex)
		e.uflags = flags
	})
}

// filesUpdate updates registered files at specified offsets.
func (ur *ioUring) filesUpdate(sqeCtx SQEContext, fds []int32, offset int) error {
	if len(fds) == 0 {
		return ErrInvalidParam
	}
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_FILES_UPDATE), func(e *ioUringSqe) {
		e.opcode = IORING_OP_FILES_UPDATE
		e.flags = sqeCtx.Flags()
		e.fd = -1 // not used
		e.addr = uint64(uintptr(unsafe.Pointer(&fds[0])))
		e.len = uint32(len(fds))
		e.off = uint64(offset)
	})
}

// uringCmd submits a generic uring command (passthrough).
func (ur *ioUring) uringCmd(sqeCtx SQEContext, cmdOp uint32, cmdData []byte) error {
	return ur.submitPacked(sqeCtx.WithOp(IORING_OP_URING_CMD), func(e *ioUringSqe) {
		e.opcode = IORING_OP_URING_CMD
		e.flags = sqeCtx.Flags()
		e.fd = sqeCtx.FD()
		e.off = uint64(cmdOp) // cmd_op in union
		if len(cmdData) > 0 {
			e.addr = uint64(uintptr(unsafe.Pointer(&cmdData[0])))
			e.len = uint32(len(cmdData))
		}
	})
}

// nop128 returns ErrNotSupported until the ring can map 128-byte SQE slots.
func (ur *ioUring) nop128(sqeCtx SQEContext) error {
	return ErrNotSupported
}

// uringCmd128 returns ErrNotSupported until the ring can map 128-byte SQE slots.
func (ur *ioUring) uringCmd128(sqeCtx SQEContext, cmdOp uint32, cmdData []byte) error {
	return ErrNotSupported
}
