// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"math"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	"code.hybscloud.com/iofd"
)

func assertZeroedSQETail(t *testing.T, sqe *ioUringSqe) {
	t.Helper()

	if sqe.addr != 0 {
		t.Fatalf("addr = %#x, want 0", sqe.addr)
	}
	if sqe.uflags != 0 {
		t.Fatalf("uflags = %#x, want 0", sqe.uflags)
	}
	if sqe.bufIndex != 0 {
		t.Fatalf("bufIndex = %d, want 0", sqe.bufIndex)
	}
	if sqe.personality != 0 {
		t.Fatalf("personality = %d, want 0", sqe.personality)
	}
}

func assertSocketSQE(t *testing.T, sqe *ioUringSqe, wantDomain, wantType, wantProto int, wantFileIndex int32) {
	t.Helper()

	if sqe.opcode != IORING_OP_SOCKET {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SOCKET)
	}
	if sqe.flags != 0 {
		t.Fatalf("flags = %d, want 0", sqe.flags)
	}
	if sqe.ioprio != 0 {
		t.Fatalf("ioprio = %d, want 0", sqe.ioprio)
	}
	if sqe.fd != int32(wantDomain) {
		t.Fatalf("fd = %d, want %d", sqe.fd, wantDomain)
	}
	if sqe.off != uint64(wantType) {
		t.Fatalf("off = %#x, want %#x", sqe.off, uint64(wantType))
	}
	if sqe.len != uint32(wantProto) {
		t.Fatalf("len = %d, want %d", sqe.len, wantProto)
	}
	if sqe.spliceFdIn != wantFileIndex {
		t.Fatalf("spliceFdIn = %d, want %d", sqe.spliceFdIn, wantFileIndex)
	}
	assertZeroedSQETail(t, sqe)
}

func TestSocketWrappersEncodeExpectedSQEs(t *testing.T) {
	ring := newWrapperTestRing(t)

	tests := []struct {
		name          string
		call          func(*Uring) error
		wantDomain    int
		wantType      int
		wantProto     int
		wantFileIndex int32
	}{
		{
			name:          "UDPLITE4Socket",
			call:          func(ur *Uring) error { return ur.UDPLITE4Socket(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET,
			wantType:      SOCK_DGRAM | SOCK_CLOEXEC,
			wantProto:     IPPROTO_UDPLITE,
			wantFileIndex: 0,
		},
		{
			name:          "UDPLITE6Socket",
			call:          func(ur *Uring) error { return ur.UDPLITE6Socket(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET6,
			wantType:      SOCK_DGRAM | SOCK_CLOEXEC,
			wantProto:     IPPROTO_UDPLITE,
			wantFileIndex: 0,
		},
		{
			name:          "SCTP4Socket",
			call:          func(ur *Uring) error { return ur.SCTP4Socket(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET,
			wantType:      SOCK_SEQPACKET | SOCK_CLOEXEC,
			wantProto:     IPPROTO_SCTP,
			wantFileIndex: 0,
		},
		{
			name:          "SCTP6Socket",
			call:          func(ur *Uring) error { return ur.SCTP6Socket(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET6,
			wantType:      SOCK_SEQPACKET | SOCK_CLOEXEC,
			wantProto:     IPPROTO_SCTP,
			wantFileIndex: 0,
		},
		{
			name:          "TCP6SocketDirect",
			call:          func(ur *Uring) error { return ur.TCP6SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET6,
			wantType:      SOCK_STREAM | SOCK_NONBLOCK,
			wantProto:     IPPROTO_TCP,
			wantFileIndex: -1,
		},
		{
			name:          "UDP4SocketDirect",
			call:          func(ur *Uring) error { return ur.UDP4SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET,
			wantType:      SOCK_DGRAM | SOCK_NONBLOCK,
			wantProto:     IPPROTO_UDP,
			wantFileIndex: -1,
		},
		{
			name:          "UDP6SocketDirect",
			call:          func(ur *Uring) error { return ur.UDP6SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET6,
			wantType:      SOCK_DGRAM | SOCK_NONBLOCK,
			wantProto:     IPPROTO_UDP,
			wantFileIndex: -1,
		},
		{
			name:          "UDPLITE4SocketDirect",
			call:          func(ur *Uring) error { return ur.UDPLITE4SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET,
			wantType:      SOCK_DGRAM | SOCK_NONBLOCK,
			wantProto:     IPPROTO_UDPLITE,
			wantFileIndex: -1,
		},
		{
			name:          "UDPLITE6SocketDirect",
			call:          func(ur *Uring) error { return ur.UDPLITE6SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET6,
			wantType:      SOCK_DGRAM | SOCK_NONBLOCK,
			wantProto:     IPPROTO_UDPLITE,
			wantFileIndex: -1,
		},
		{
			name:          "SCTP4SocketDirect",
			call:          func(ur *Uring) error { return ur.SCTP4SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET,
			wantType:      SOCK_SEQPACKET | SOCK_NONBLOCK,
			wantProto:     IPPROTO_SCTP,
			wantFileIndex: -1,
		},
		{
			name:          "SCTP6SocketDirect",
			call:          func(ur *Uring) error { return ur.SCTP6SocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_INET6,
			wantType:      SOCK_SEQPACKET | SOCK_NONBLOCK,
			wantProto:     IPPROTO_SCTP,
			wantFileIndex: -1,
		},
		{
			name:          "UnixSocketDirect",
			call:          func(ur *Uring) error { return ur.UnixSocketDirect(PackDirect(0, 0, 0, 0)) },
			wantDomain:    AF_LOCAL,
			wantType:      SOCK_SEQPACKET | SOCK_NONBLOCK,
			wantProto:     0,
			wantFileIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(ring); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			assertSocketSQE(t, lastSubmittedSQE(t, ring), tt.wantDomain, tt.wantType, tt.wantProto, tt.wantFileIndex)
		})
	}
}

func TestSubmitAcceptDirectMultishotEncodesExpectedSQE(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)

	flags := uint8(IOSQE_IO_LINK)
	ioprio := uint16(IORING_ACCEPT_POLL_FIRST)
	if err := ring.SubmitAcceptDirectMultishot(ForFD(23), IORING_FILE_INDEX_ALLOC, WithFlags(flags), WithIOPrio(ioprio)); err != nil {
		t.Fatalf("SubmitAcceptDirectMultishot: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_ACCEPT {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_ACCEPT)
	}
	if sqe.flags != flags {
		t.Fatalf("flags = %d, want %d", sqe.flags, flags)
	}
	if sqe.ioprio != ioprio|IORING_ACCEPT_MULTISHOT {
		t.Fatalf("ioprio = %#x, want %#x", sqe.ioprio, ioprio|IORING_ACCEPT_MULTISHOT)
	}
	if sqe.fd != 23 {
		t.Fatalf("fd = %d, want 23", sqe.fd)
	}
	if sqe.uflags != SOCK_NONBLOCK {
		t.Fatalf("uflags = %#x, want %#x", sqe.uflags, SOCK_NONBLOCK)
	}
	if sqe.spliceFdIn != -1 {
		t.Fatalf("spliceFdIn = %d, want -1", sqe.spliceFdIn)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.addr != 0 {
		t.Fatalf("addr = %#x, want 0", sqe.addr)
	}
	if sqe.len != 0 {
		t.Fatalf("len = %d, want 0", sqe.len)
	}
	if sqe.bufIndex != 0 {
		t.Fatalf("bufIndex = %d, want 0", sqe.bufIndex)
	}
	if sqe.personality != 0 {
		t.Fatalf("personality = %d, want 0", sqe.personality)
	}
}

func TestCloseEncodesExpectedSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	t.Run("regular", func(t *testing.T) {
		ctx := PackDirect(IORING_OP_CLOSE, IOSQE_IO_LINK, 0, 23)
		if err := ring.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_CLOSE {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_CLOSE)
		}
		if sqe.flags != IOSQE_IO_LINK {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
		}
		if sqe.fd != 23 {
			t.Fatalf("fd = %d, want 23", sqe.fd)
		}
		if sqe.off != 0 {
			t.Fatalf("off = %d, want 0", sqe.off)
		}
		if sqe.spliceFdIn != 0 {
			t.Fatalf("spliceFdIn = %d, want 0", sqe.spliceFdIn)
		}
		assertZeroedSQETail(t, sqe)
	})

	t.Run("wrapper builds close context", func(t *testing.T) {
		ctx := ForFD(23)
		if err := ring.Close(ctx, WithFlags(IOSQE_IO_LINK)); err != nil {
			t.Fatalf("Close: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_CLOSE {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_CLOSE)
		}
		if sqe.flags != IOSQE_IO_LINK {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
		}
		if sqe.fd != 23 {
			t.Fatalf("fd = %d, want 23", sqe.fd)
		}
		gotCtx := SQEContextFromRaw(sqe.userData)
		wantCtx := ctx.WithFlags(IOSQE_IO_LINK).WithOp(IORING_OP_CLOSE)
		if gotCtx != wantCtx {
			t.Fatalf("userData ctx = %#x, want %#x", gotCtx.Raw(), wantCtx.Raw())
		}
		if gotCtx.Op() != IORING_OP_CLOSE {
			t.Fatalf("userData op = %d, want %d", gotCtx.Op(), IORING_OP_CLOSE)
		}
		assertZeroedSQETail(t, sqe)
	})

	t.Run("direct", func(t *testing.T) {
		ctx := PackDirect(IORING_OP_CLOSE, IOSQE_FIXED_FILE|IOSQE_IO_LINK, 0, 4)
		if err := ring.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_CLOSE {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_CLOSE)
		}
		if sqe.flags != IOSQE_IO_LINK {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
		}
		if sqe.fd != 0 {
			t.Fatalf("fd = %d, want 0", sqe.fd)
		}
		if sqe.off != 0 {
			t.Fatalf("off = %d, want 0", sqe.off)
		}
		if sqe.spliceFdIn != 5 {
			t.Fatalf("spliceFdIn = %d, want 5", sqe.spliceFdIn)
		}
		assertZeroedSQETail(t, sqe)
	})
}

func TestWrappersPreserveCallerFlags(t *testing.T) {
	ring := newWrapperTestRing(t)

	t.Run("Nop", func(t *testing.T) {
		ctx := PackDirect(0, IOSQE_IO_LINK, 0, -1)
		if err := ring.Nop(ctx, WithFlags(IOSQE_ASYNC)); err != nil {
			t.Fatalf("Nop: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_NOP {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_NOP)
		}
		if sqe.flags != IOSQE_IO_LINK|IOSQE_ASYNC {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK|IOSQE_ASYNC)
		}
		if sqe.fd != -1 {
			t.Fatalf("fd = %d, want -1", sqe.fd)
		}
		gotCtx := SQEContextFromRaw(sqe.userData)
		wantCtx := ctx.OrFlags(IOSQE_ASYNC).WithOp(IORING_OP_NOP)
		if gotCtx != wantCtx {
			t.Fatalf("userData ctx = %#x, want %#x", gotCtx.Raw(), wantCtx.Raw())
		}
		assertZeroedSQETail(t, sqe)
	})

	t.Run("Connect", func(t *testing.T) {
		ctx := PackDirect(0, IOSQE_FIXED_FILE|IOSQE_IO_LINK, 0, 7)
		remoteAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9000}
		if err := ring.Connect(ctx, remoteAddr, WithFlags(IOSQE_ASYNC)); err != nil {
			t.Fatalf("Connect: %v", err)
		}

		saPtr, saN, err := sockaddr(AddrToSockaddr(remoteAddr))
		if err != nil {
			t.Fatalf("sockaddr: %v", err)
		}
		if saPtr == nil {
			t.Fatal("sockaddr pointer = nil, want non-nil")
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_CONNECT {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_CONNECT)
		}
		if sqe.flags != IOSQE_FIXED_FILE|IOSQE_IO_LINK|IOSQE_ASYNC {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_FIXED_FILE|IOSQE_IO_LINK|IOSQE_ASYNC)
		}
		if sqe.fd != 7 {
			t.Fatalf("fd = %d, want 7", sqe.fd)
		}
		if sqe.off != uint64(saN) {
			t.Fatalf("off = %d, want %d", sqe.off, saN)
		}
		if sqe.addr == 0 {
			t.Fatal("addr = 0, want non-zero sockaddr pointer")
		}
		if sqe.len != 0 {
			t.Fatalf("len = %d, want 0", sqe.len)
		}
		gotCtx := SQEContextFromRaw(sqe.userData)
		wantCtx := ctx.OrFlags(IOSQE_ASYNC).WithOp(IORING_OP_CONNECT)
		if gotCtx != wantCtx {
			t.Fatalf("userData ctx = %#x, want %#x", gotCtx.Raw(), wantCtx.Raw())
		}
		if gotCtx.Flags()&IOSQE_FIXED_FILE == 0 {
			t.Fatalf("userData flags = %#x, want fixed-file bit set", gotCtx.Flags())
		}
	})
}

func TestMessageWrappersEncodeExpectedSQEs(t *testing.T) {
	ring := newWrapperTestRing(t)

	t.Run("SendMsg", func(t *testing.T) {
		buffers := [][]byte{[]byte("ping"), []byte("pong")}
		oob := []byte{1, 2, 3, 4}
		to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9000}

		if err := ring.SendMsg(
			PackDirect(0, 0, 0, 0),
			&mockPollFd{fd: 21},
			buffers,
			oob,
			to,
			WithFlags(IOSQE_IO_DRAIN),
			WithIOPrio(9),
		); err != nil {
			t.Fatalf("SendMsg: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_SENDMSG {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SENDMSG)
		}
		if sqe.flags != IOSQE_IO_DRAIN|ring.writeLikeOpFlags {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_DRAIN|ring.writeLikeOpFlags)
		}
		if sqe.ioprio != 9 {
			t.Fatalf("ioprio = %d, want 9", sqe.ioprio)
		}
		if sqe.fd != 21 {
			t.Fatalf("fd = %d, want 21", sqe.fd)
		}
		if sqe.len != 1 {
			t.Fatalf("len = %d, want 1", sqe.len)
		}
		if sqe.addr == 0 {
			t.Fatal("addr = 0, want non-zero message header pointer")
		}
		if sqe.off != 0 {
			t.Fatalf("off = %d, want 0", sqe.off)
		}
		if sqe.uflags != 0 {
			t.Fatalf("uflags = %#x, want 0", sqe.uflags)
		}
	})

	t.Run("RecvMsg", func(t *testing.T) {
		buffer := make([]byte, 8)
		iovs := []IoVec{{Base: unsafe.SliceData(buffer), Len: uint64(len(buffer))}}
		oob := []byte{5, 6, 7, 8}
		from := RawSockaddrAny{}
		msg := Msghdr{
			Name:       (*byte)(unsafe.Pointer(&from)),
			Namelen:    uint32(SizeofSockaddrAny),
			Iov:        unsafe.SliceData(iovs),
			Iovlen:     uint64(len(iovs)),
			Control:    unsafe.SliceData(oob),
			Controllen: uint64(len(oob)),
		}

		if err := ring.RecvMsg(
			PackDirect(0, 0, 0, 0),
			&mockPollFd{fd: 22},
			&msg,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(7),
		); err != nil {
			t.Fatalf("RecvMsg: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_RECVMSG {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_RECVMSG)
		}
		if sqe.flags != IOSQE_ASYNC|ring.readLikeOpFlags {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC|ring.readLikeOpFlags)
		}
		if sqe.ioprio != 7 {
			t.Fatalf("ioprio = %d, want 7", sqe.ioprio)
		}
		if sqe.fd != 22 {
			t.Fatalf("fd = %d, want 22", sqe.fd)
		}
		if sqe.len != 1 {
			t.Fatalf("len = %d, want 1", sqe.len)
		}
		if got, want := sqe.addr, uint64(uintptr(unsafe.Pointer(&msg))); got != want {
			t.Fatalf("addr = %#x, want caller message header %#x", got, want)
		}
		if sqe.uflags != MSG_WAITALL {
			t.Fatalf("uflags = %#x, want %#x", sqe.uflags, MSG_WAITALL)
		}
		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		keep := &ring.ioUring.submit.keepAlive[(tail-1)&*ring.ioUring.sq.kRingMask]
		if keep.ext == nil || keep.ext.recvMsg != &msg {
			t.Fatal("submit keepAlive did not retain the caller message header")
		}
	})
}

func TestReceiveWrapperEncodesExpectedSQEs(t *testing.T) {
	t.Run("fixed buffer", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		buf := make([]byte, 16)
		fd := &mockPollFd{fd: 27}

		if err := ring.Receive(
			PackDirect(0, 0, 0, 0),
			fd,
			buf,
			WithFlags(IOSQE_IO_LINK),
			WithIOPrio(14),
			WithOffset(3),
			WithN(7),
		); err != nil {
			t.Fatalf("Receive(fixed): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_RECV {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_RECV)
		}
		if sqe.flags != IOSQE_IO_LINK|ring.readLikeOpFlags {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK|ring.readLikeOpFlags)
		}
		if sqe.ioprio != 14 {
			t.Fatalf("ioprio = %d, want 14", sqe.ioprio)
		}
		if sqe.fd != int32(fd.Fd()) {
			t.Fatalf("fd = %d, want %d", sqe.fd, fd.Fd())
		}
		if sqe.off != 0 {
			t.Fatalf("off = %d, want 0", sqe.off)
		}
		wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + 3
		if sqe.addr != wantAddr {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
		}
		if sqe.len != 7 {
			t.Fatalf("len = %d, want 7", sqe.len)
		}
		if sqe.uflags != MSG_WAITALL {
			t.Fatalf("uflags = %#x, want %#x", sqe.uflags, MSG_WAITALL)
		}
		if sqe.bufIndex != 0 {
			t.Fatalf("bufIndex = %d, want 0", sqe.bufIndex)
		}
	})

	t.Run("offset-only subrange", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		buf := make([]byte, 16)

		if err := ring.Receive(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 28}, buf, WithOffset(5)); err != nil {
			t.Fatalf("Receive(offset-only): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + 5
		if sqe.addr != wantAddr || sqe.off != 0 || sqe.len != 11 {
			t.Fatalf("offset-only SQE = {addr:%#x off:%d len:%d}, want {addr:%#x off:0 len:11}", sqe.addr, sqe.off, sqe.len, wantAddr)
		}
	})

	t.Run("buffer select", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
			PicoNum:   16,
			MediumNum: 16,
		})
		groups.setGIDOffset(7)
		ring.bufferGroups = groups
		ring.buffers = nil
		ring.ReadBufferGidOffset = 7

		fd := &mockPollFd{fd: 3}
		if err := ring.Receive(
			PackDirect(0, 0, 0, 0),
			fd,
			nil,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(12),
			WithReadBufferSize(BufferSizePico),
		); err != nil {
			t.Fatalf("Receive(buffer select): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantGroup := groups.bufGroupBySize(fd, BufferSizePico)
		if sqe.opcode != IORING_OP_RECV {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_RECV)
		}
		if sqe.flags != IOSQE_ASYNC|ring.readLikeOpFlags|IOSQE_BUFFER_SELECT {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC|ring.readLikeOpFlags|IOSQE_BUFFER_SELECT)
		}
		if sqe.ioprio != 12 {
			t.Fatalf("ioprio = %d, want 12", sqe.ioprio)
		}
		if sqe.fd != int32(fd.Fd()) {
			t.Fatalf("fd = %d, want %d", sqe.fd, fd.Fd())
		}
		if sqe.len != BufferSizePico {
			t.Fatalf("len = %d, want %d", sqe.len, BufferSizePico)
		}
		if sqe.bufIndex != wantGroup {
			t.Fatalf("bufIndex = %d, want %d", sqe.bufIndex, wantGroup)
		}
		if sqe.off != 0 {
			t.Fatalf("off = %d, want 0", sqe.off)
		}
		if sqe.addr != 0 {
			t.Fatalf("addr = %#x, want 0", sqe.addr)
		}
		if sqe.uflags != 0 {
			t.Fatalf("uflags = %#x, want 0", sqe.uflags)
		}
	})
}

func TestSubmitReceiveBundleMultishotWrapperEncodesLengthCap(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
		PicoNum:   16,
		MediumNum: 16,
	})
	groups.setGIDOffset(7)
	ring.Options = &Options{ReadBufferGidOffset: 7}
	ring.bufferGroups = groups
	ring.buffers = nil

	fd := &mockPollFd{fd: 30}
	if err := ring.SubmitReceiveBundleMultishot(
		PackDirect(0, 0, 0, 0),
		fd,
		WithReadBufferSize(BufferSizePico),
	); err != nil {
		t.Fatalf("SubmitReceiveBundleMultishot: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.len != BufferSizePico {
		t.Fatalf("len = %d, want %d", sqe.len, BufferSizePico)
	}
	if sqe.ioprio != IORING_RECV_MULTISHOT|IORING_RECVSEND_BUNDLE {
		t.Fatalf("ioprio = %#x, want %#x", sqe.ioprio, IORING_RECV_MULTISHOT|IORING_RECVSEND_BUNDLE)
	}

	if err := ring.SubmitReceiveBundleMultishot(
		PackDirect(0, 0, 0, 0),
		fd,
		WithReadBufferSize(0),
	); err != nil {
		t.Fatalf("SubmitReceiveBundleMultishot(uncapped): %v", err)
	}

	sqe = lastSubmittedSQE(t, ring)
	wantGroup := groups.bufGroupBySize(fd, bufferSizeDefault)
	if wantGroup == 0 {
		t.Fatal("uncapped bundle witness selected group zero")
	}
	if sqe.len != 0 || sqe.bufIndex != wantGroup {
		t.Fatalf("SubmitReceiveBundleMultishot(uncapped) SQE = %#v, want len 0 and bufIndex %d", *sqe, wantGroup)
	}
}

func TestReceiveMultishotLowLevelEncodesLengthDomain(t *testing.T) {
	tests := []struct {
		name       string
		n          int
		submit     func(*ioUring, SQEContext, int) error
		wantIoprio uint16
	}{
		{
			name: "receive positive cap",
			n:    1,
			submit: func(ring *ioUring, ctx SQEContext, n int) error {
				return ring.receiveWithBufferSelect(ctx, IORING_RECV_MULTISHOT, n, 2)
			},
			wantIoprio: IORING_RECV_MULTISHOT,
		},
		{
			name: "receive uncapped",
			n:    0,
			submit: func(ring *ioUring, ctx SQEContext, n int) error {
				return ring.receiveWithBufferSelect(ctx, IORING_RECV_MULTISHOT, n, 2)
			},
			wantIoprio: IORING_RECV_MULTISHOT,
		},
		{
			name: "bundle positive cap",
			n:    1,
			submit: func(ring *ioUring, ctx SQEContext, n int) error {
				return ring.receiveBundle(ctx, IORING_RECV_MULTISHOT, n, 2)
			},
			wantIoprio: IORING_RECV_MULTISHOT | IORING_RECVSEND_BUNDLE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			ctx := PackDirect(IORING_OP_RECV, 0, 0, 3)
			if err := tt.submit(ring.ioUring, ctx, tt.n); err != nil {
				t.Fatalf("submit: %v", err)
			}
			sqe := lastSubmittedSQE(t, ring)
			if sqe.len != uint32(tt.n) {
				t.Fatalf("len = %d, want %d", sqe.len, tt.n)
			}
			if sqe.ioprio != tt.wantIoprio {
				t.Fatalf("ioprio = %#x, want %#x", sqe.ioprio, tt.wantIoprio)
			}
		})
	}

	t.Run("rejects invalid domains", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		ctx := PackDirect(IORING_OP_RECV, 0, 0, 3)
		if err := ring.ioUring.receiveWithBufferSelect(ctx, IORING_RECV_MULTISHOT, -1, 2); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("multishot negative length = %v, want %v", err, ErrInvalidParam)
		}
		if err := ring.ioUring.receiveBundle(ctx, 0, 0, 2); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("single-shot zero length = %v, want %v", err, ErrInvalidParam)
		}
		if got := ring.ioUring.sqCount(); got != 0 {
			t.Fatalf("sqCount = %d, want 0", got)
		}
	})
}

func TestRWCurrentPositionSentinelEncodesMaxUint64(t *testing.T) {
	assertOffset := func(t *testing.T, ring *Uring) {
		t.Helper()
		if got := lastSubmittedSQE(t, ring).off; got != math.MaxUint64 {
			t.Fatalf("SQE off = %#x, want %#x", got, uint64(math.MaxUint64))
		}
	}

	t.Run("read", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		if err := ring.Read(PackDirect(0, 0, 0, 1), []byte{1}, WithOffset(-1)); err != nil {
			t.Fatalf("Read: %v", err)
		}
		assertOffset(t, ring)
	})

	t.Run("write", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		if err := ring.Write(PackDirect(0, 0, 0, 1), []byte{1}, WithOffset(-1)); err != nil {
			t.Fatalf("Write: %v", err)
		}
		assertOffset(t, ring)
	})

	t.Run("read fixed", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		ring.ioUring.bufs = [][]byte{{1}}
		if _, err := ring.ReadFixed(PackDirect(0, 0, 0, 1), 0, WithOffset(-1)); err != nil {
			t.Fatalf("ReadFixed: %v", err)
		}
		assertOffset(t, ring)
	})

	t.Run("write fixed", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		ring.ioUring.bufs = [][]byte{{1}}
		if err := ring.WriteFixed(PackDirect(0, 0, 0, 1), 0, 1, WithOffset(-1)); err != nil {
			t.Fatalf("WriteFixed: %v", err)
		}
		assertOffset(t, ring)
	})

	t.Run("readv fixed", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		ring.ioUring.bufs = [][]byte{{1}}
		if err := ring.ReadvFixed(PackDirect(0, 0, 0, 1), -1, 0, [][]byte{ring.ioUring.bufs[0]}); err != nil {
			t.Fatalf("ReadvFixed: %v", err)
		}
		assertOffset(t, ring)
	})

	t.Run("writev fixed", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		ring.ioUring.bufs = [][]byte{{1}}
		if err := ring.WritevFixed(PackDirect(0, 0, 0, 1), -1, 0, [][]byte{ring.ioUring.bufs[0]}); err != nil {
			t.Fatalf("WritevFixed: %v", err)
		}
		assertOffset(t, ring)
	})
}

func TestSubmitReceiveMultishotWrapperEncodesExpectedSQEs(t *testing.T) {
	t.Run("rejects caller buffer before option reduction", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		buf := make([]byte, 16)
		fd := &mockPollFd{fd: 28}
		optionApplied := false

		err := ring.SubmitReceiveMultishot(
			PackDirect(0, 0, 0, 0),
			fd,
			buf,
			func(*OpOption) { optionApplied = true },
		)
		if !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("SubmitReceiveMultishot(caller buffer) = %v, want %v", err, ErrInvalidParam)
		}
		if optionApplied {
			t.Fatal("SubmitReceiveMultishot applied options before validating b")
		}
		if got := ring.ioUring.sqCount(); got != 0 {
			t.Fatalf("sqCount() = %d, want 0", got)
		}
	})

	t.Run("buffer select", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
			PicoNum:   16,
			MediumNum: 16,
		})
		groups.setGIDOffset(7)
		ring.Options = &Options{ReadBufferGidOffset: 7}
		ring.bufferGroups = groups
		ring.buffers = nil

		fd := &mockPollFd{fd: 29}
		if err := ring.SubmitReceiveMultishot(
			PackDirect(0, 0, 0, 0),
			fd,
			nil,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(IORING_RECVSEND_POLL_FIRST),
			WithReadBufferSize(BufferSizePico),
		); err != nil {
			t.Fatalf("SubmitReceiveMultishot(buffer select): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantGroup := groups.bufGroupBySize(fd, BufferSizePico)
		if sqe.opcode != IORING_OP_RECV {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_RECV)
		}
		if sqe.flags != IOSQE_ASYNC|ring.readLikeOpFlags|IOSQE_BUFFER_SELECT {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC|ring.readLikeOpFlags|IOSQE_BUFFER_SELECT)
		}
		if sqe.ioprio != IORING_RECVSEND_POLL_FIRST|IORING_RECV_MULTISHOT {
			t.Fatalf("ioprio = %#x, want %#x", sqe.ioprio, IORING_RECVSEND_POLL_FIRST|IORING_RECV_MULTISHOT)
		}
		if sqe.fd != int32(fd.Fd()) {
			t.Fatalf("fd = %d, want %d", sqe.fd, fd.Fd())
		}
		if sqe.len != BufferSizePico {
			t.Fatalf("len = %d, want %d", sqe.len, BufferSizePico)
		}
		if sqe.bufIndex != wantGroup {
			t.Fatalf("bufIndex = %d, want %d", sqe.bufIndex, wantGroup)
		}
		if sqe.off != 0 {
			t.Fatalf("off = %d, want 0", sqe.off)
		}
		if sqe.addr != 0 {
			t.Fatalf("addr = %#x, want 0", sqe.addr)
		}
		if sqe.uflags != 0 {
			t.Fatalf("uflags = %#x, want 0", sqe.uflags)
		}
	})

	t.Run("uncapped buffer select", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
			PicoNum:   16,
			MediumNum: 16,
		})
		groups.setGIDOffset(7)
		ring.bufferGroups = groups
		ring.buffers = nil

		fd := &mockPollFd{fd: 29}
		if err := ring.SubmitReceiveMultishot(
			PackDirect(0, 0, 0, 0),
			fd,
			nil,
			WithReadBufferSize(0),
		); err != nil {
			t.Fatalf("SubmitReceiveMultishot(uncapped): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantGroup := groups.bufGroupBySize(fd, bufferSizeDefault)
		if wantGroup == 0 {
			t.Fatal("uncapped receive witness selected group zero")
		}
		if sqe.len != 0 || sqe.bufIndex != wantGroup {
			t.Fatalf("SubmitReceiveMultishot(uncapped) SQE = %#v, want len 0 and bufIndex %d", *sqe, wantGroup)
		}
	})
}

func TestFDStreamWrappersEncodeExpectedSQEs(t *testing.T) {
	t.Run("receive", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		buf := make([]byte, 16)
		if err := ring.ReceiveFD(
			PackDirect(0, 0, 0, 0),
			iofd.FD(29),
			buf,
			WithFlags(IOSQE_IO_LINK),
			WithIOPrio(14),
			WithOffset(3),
			WithN(7),
		); err != nil {
			t.Fatalf("ReceiveFD: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + 3
		if sqe.opcode != IORING_OP_RECV ||
			sqe.flags != IOSQE_IO_LINK|ring.readLikeOpFlags ||
			sqe.ioprio != 14 ||
			sqe.fd != 29 ||
			sqe.addr != wantAddr ||
			sqe.len != 7 ||
			sqe.uflags != MSG_WAITALL {
			t.Fatalf("ReceiveFD SQE = %#v", *sqe)
		}
	})

	t.Run("receive multishot", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
			PicoNum:   16,
			MediumNum: 16,
		})
		groups.setGIDOffset(7)
		ring.bufferGroups = groups

		const fd = iofd.FD(29)
		if err := ring.SubmitReceiveMultishotFD(
			PackDirect(0, 0, 0, 0),
			fd,
			nil,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(IORING_RECVSEND_POLL_FIRST),
			WithReadBufferSize(BufferSizePico),
		); err != nil {
			t.Fatalf("SubmitReceiveMultishotFD: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantGroup := groups.bufGroupBySizeFD(fd, BufferSizePico)
		if sqe.opcode != IORING_OP_RECV ||
			sqe.flags != IOSQE_ASYNC|ring.readLikeOpFlags|IOSQE_BUFFER_SELECT ||
			sqe.ioprio != IORING_RECVSEND_POLL_FIRST|IORING_RECV_MULTISHOT ||
			sqe.fd != int32(fd) ||
			sqe.len != BufferSizePico ||
			sqe.bufIndex != wantGroup ||
			sqe.addr != 0 ||
			sqe.uflags != 0 {
			t.Fatalf("SubmitReceiveMultishotFD SQE = %#v", *sqe)
		}
	})

	t.Run("receive multishot uncapped", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
			PicoNum:   16,
			MediumNum: 16,
		})
		groups.setGIDOffset(7)
		ring.bufferGroups = groups

		const fd = iofd.FD(29)
		if err := ring.SubmitReceiveMultishotFD(
			PackDirect(0, 0, 0, 0),
			fd,
			nil,
			WithReadBufferSize(0),
		); err != nil {
			t.Fatalf("SubmitReceiveMultishotFD(uncapped): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantGroup := groups.bufGroupBySizeFD(fd, bufferSizeDefault)
		if wantGroup == 0 {
			t.Fatal("uncapped direct receive witness selected group zero")
		}
		if sqe.len != 0 || sqe.bufIndex != wantGroup {
			t.Fatalf("SubmitReceiveMultishotFD(uncapped) SQE = %#v, want len 0 and bufIndex %d", *sqe, wantGroup)
		}
	})

	t.Run("send", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		buf := []byte("payload")
		if err := ring.SendFD(
			PackDirect(0, 0, 0, 0),
			iofd.FD(31),
			buf,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(9),
			WithOffset(1),
			WithN(4),
		); err != nil {
			t.Fatalf("SendFD: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + 1
		if sqe.opcode != IORING_OP_SEND ||
			sqe.flags != IOSQE_ASYNC|ring.writeLikeOpFlags ||
			sqe.ioprio != 9 ||
			sqe.fd != 31 ||
			sqe.addr != wantAddr ||
			sqe.len != 4 {
			t.Fatalf("SendFD SQE = %#v", *sqe)
		}
	})
}

func TestFDStreamWrappersDoNotAllocate(t *testing.T) {
	t.Run("receive", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 128)
		buf := make([]byte, 16)
		allocs := testing.AllocsPerRun(100, func() {
			if err := ring.ReceiveFD(PackDirect(0, 0, 0, 0), iofd.FD(29), buf); err != nil {
				panic(err)
			}
		})
		if allocs != 0 {
			t.Fatalf("ReceiveFD allocations = %v, want 0", allocs)
		}
	})

	t.Run("receive multishot", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 128)
		ring.buffers = &uringProvideBuffers{gMask: 3}
		allocs := testing.AllocsPerRun(100, func() {
			if err := ring.SubmitReceiveMultishotFD(PackDirect(0, 0, 0, 0), iofd.FD(29), nil); err != nil {
				panic(err)
			}
		})
		if allocs != 0 {
			t.Fatalf("SubmitReceiveMultishotFD allocations = %v, want 0", allocs)
		}
	})

	t.Run("send", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 128)
		buf := []byte("payload")
		allocs := testing.AllocsPerRun(100, func() {
			if err := ring.SendFD(PackDirect(0, 0, 0, 0), iofd.FD(31), buf); err != nil {
				panic(err)
			}
		})
		if allocs != 0 {
			t.Fatalf("SendFD allocations = %v, want 0", allocs)
		}
	})
}

func TestFallocateRejectsZeroLengthBeforeSubmission(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	optionApplied := false

	err := ring.Fallocate(
		PackDirect(0, 0, 0, 0),
		0,
		0,
		0,
		func(*OpOption) { optionApplied = true },
	)
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("Fallocate(zero length) = %v, want %v", err, ErrInvalidParam)
	}
	if optionApplied {
		t.Fatal("Fallocate applied options before validating length")
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("sqCount() = %d, want 0", got)
	}

	if err := ring.ioUring.fAllocate(PackDirect(0, 0, 0, 0), 0, 0, 0); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("fAllocate(zero length) = %v, want %v", err, ErrInvalidParam)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("sqCount() after fAllocate = %d, want 0", got)
	}
}

func TestBundleIteratorWrapperUsesRegisteredGroup(t *testing.T) {
	const (
		bufCount = 4
		bufSize  = 64
		groupID  = 7
	)

	backing := make([]byte, bufCount*bufSize)
	for i := 0; i < bufCount; i++ {
		backing[i*bufSize] = byte(i + 1)
	}

	rings := newUringBufferRings()
	rings.rings = []*ioUringBufRing{new(ioUringBufRing)}
	rings.gids = []uint16{groupID}
	rings.buffers = [][]byte{backing}
	rings.sizes = []int{bufSize}
	rings.masks = []uintptr{bufCount - 1}
	rings.nextIDs = [][]uint16{{1, 2, 3, 0}}

	ring := &Uring{
		Options:     &Options{ReadBufferGidOffset: groupID},
		bufferRings: rings,
	}

	cqe := CQEView{
		Res:   96,
		Flags: 1 << IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := ring.BundleIterator(cqe, groupID)
	if !ok {
		t.Fatal("BundleIterator returned false")
	}
	if iter.recycleOwner != rings || iter.ringIndex != 0 || iter.group != groupID {
		t.Fatalf("recycle identity = (%p, %d, %d), want (%p, 0, %d)",
			iter.recycleOwner, iter.ringIndex, iter.group, rings, groupID)
	}
	if iter.Count() != 2 {
		t.Fatalf("Count = %d, want 2", iter.Count())
	}
	if iter.Buffer(0)[0] != 2 {
		t.Fatalf("Buffer(0)[0] = %d, want 2", iter.Buffer(0)[0])
	}
	if iter.Buffer(1)[0] != 3 {
		t.Fatalf("Buffer(1)[0] = %d, want 3", iter.Buffer(1)[0])
	}

	if _, ok := ring.BundleIterator(cqe, groupID-1); ok {
		t.Fatal("BundleIterator(invalid group) = true, want false")
	}
}

func TestControlWrappersEncodeExpectedSQEs(t *testing.T) {
	ring := newWrapperTestRing(t)

	t.Run("AsyncCancelAll", func(t *testing.T) {
		if err := ring.AsyncCancelAll(ForFD(24), WithFlags(IOSQE_IO_LINK)); err != nil {
			t.Fatalf("AsyncCancelAll: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_ASYNC_CANCEL {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_ASYNC_CANCEL)
		}
		if sqe.flags != IOSQE_IO_LINK {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
		}
		if sqe.fd != 24 {
			t.Fatalf("fd = %d, want 24", sqe.fd)
		}
		if sqe.uflags != IORING_ASYNC_CANCEL_ALL|IORING_ASYNC_CANCEL_ANY {
			t.Fatalf("uflags = %#x, want %#x", sqe.uflags, IORING_ASYNC_CANCEL_ALL|IORING_ASYNC_CANCEL_ANY)
		}
		if sqe.addr != 0 {
			t.Fatalf("addr = %#x, want 0", sqe.addr)
		}
		if sqe.len != 0 {
			t.Fatalf("len = %d, want 0", sqe.len)
		}
	})

	t.Run("AsyncCancelFD", func(t *testing.T) {
		tests := []struct {
			name       string
			cancelAll  bool
			wantUFlags uint32
		}{
			{
				name:       "first match",
				cancelAll:  false,
				wantUFlags: IORING_ASYNC_CANCEL_FD,
			},
			{
				name:       "all matches",
				cancelAll:  true,
				wantUFlags: IORING_ASYNC_CANCEL_FD | IORING_ASYNC_CANCEL_ALL,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := ring.AsyncCancelFD(ForFD(35), tt.cancelAll, WithFlags(IOSQE_ASYNC)); err != nil {
					t.Fatalf("AsyncCancelFD: %v", err)
				}

				sqe := lastSubmittedSQE(t, ring)
				if sqe.opcode != IORING_OP_ASYNC_CANCEL {
					t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_ASYNC_CANCEL)
				}
				if sqe.flags != IOSQE_ASYNC {
					t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC)
				}
				if sqe.fd != 35 {
					t.Fatalf("fd = %d, want 35", sqe.fd)
				}
				if sqe.addr != 0 {
					t.Fatalf("addr = %#x, want 0", sqe.addr)
				}
				if sqe.len != 0 {
					t.Fatalf("len = %d, want 0", sqe.len)
				}
				if sqe.uflags != tt.wantUFlags {
					t.Fatalf("uflags = %#x, want %#x", sqe.uflags, tt.wantUFlags)
				}
			})
		}
	})

	t.Run("AsyncCancelOpcode", func(t *testing.T) {
		tests := []struct {
			name       string
			cancelAll  bool
			wantUFlags uint32
		}{
			{
				name:       "first match",
				cancelAll:  false,
				wantUFlags: IORING_ASYNC_CANCEL_OP,
			},
			{
				name:       "all matches",
				cancelAll:  true,
				wantUFlags: IORING_ASYNC_CANCEL_OP | IORING_ASYNC_CANCEL_ALL,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := ring.AsyncCancelOpcode(ForFD(36), IORING_OP_RECV, tt.cancelAll, WithFlags(IOSQE_IO_LINK)); err != nil {
					t.Fatalf("AsyncCancelOpcode: %v", err)
				}

				sqe := lastSubmittedSQE(t, ring)
				if sqe.opcode != IORING_OP_ASYNC_CANCEL {
					t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_ASYNC_CANCEL)
				}
				if sqe.flags != IOSQE_IO_LINK {
					t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
				}
				if sqe.fd != 36 {
					t.Fatalf("fd = %d, want 36", sqe.fd)
				}
				if sqe.addr != 0 {
					t.Fatalf("addr = %#x, want 0", sqe.addr)
				}
				if sqe.len != uint32(IORING_OP_RECV) {
					t.Fatalf("len = %d, want %d", sqe.len, uint32(IORING_OP_RECV))
				}
				if sqe.uflags != tt.wantUFlags {
					t.Fatalf("uflags = %#x, want %#x", sqe.uflags, tt.wantUFlags)
				}
			})
		}
	})

	t.Run("ReceiveZeroCopy", func(t *testing.T) {
		if err := ring.ReceiveZeroCopy(
			PackDirect(0, 0, 0, 0),
			&mockPollFd{fd: 31},
			64,
			17,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(IORING_RECVSEND_POLL_FIRST),
		); err != nil {
			t.Fatalf("ReceiveZeroCopy: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_RECV_ZC {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_RECV_ZC)
		}
		if sqe.flags != IOSQE_ASYNC|ring.readLikeOpFlags {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC|ring.readLikeOpFlags)
		}
		if sqe.ioprio != IORING_RECV_MULTISHOT|IORING_RECVSEND_POLL_FIRST {
			t.Fatalf("ioprio = %d, want %d", sqe.ioprio, IORING_RECV_MULTISHOT|IORING_RECVSEND_POLL_FIRST)
		}
		if sqe.fd != 31 {
			t.Fatalf("fd = %d, want 31", sqe.fd)
		}
		if sqe.len != 64 {
			t.Fatalf("len = %d, want 64", sqe.len)
		}
		if sqe.spliceFdIn != 17 {
			t.Fatalf("spliceFdIn = %d, want 17", sqe.spliceFdIn)
		}
		if sqe.off != 0 || sqe.pad != [2]uint64{} {
			t.Fatalf("off/pad = {%#x, %#v}, want zero", sqe.off, sqe.pad)
		}
		assertZeroedSQETail(t, sqe)
	})

	t.Run("ReceiveZeroCopyAddsMultishot", func(t *testing.T) {
		if err := ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 31}, 64, 17); err != nil {
			t.Fatalf("ReceiveZeroCopy: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.ioprio != IORING_RECV_MULTISHOT {
			t.Fatalf("ioprio = %d, want %d", sqe.ioprio, IORING_RECV_MULTISHOT)
		}
	})

	t.Run("ReceiveZeroCopyAcceptsUint32Length", func(t *testing.T) {
		if uint64(^uint(0)) <= math.MaxUint32 {
			t.Skip("uint32 maximum requires a wider int")
		}
		n := int(uint64(math.MaxUint32))
		if err := ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 31}, n, 17); err != nil {
			t.Fatalf("ReceiveZeroCopy: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.len != math.MaxUint32 {
			t.Fatalf("len = %d, want %d", sqe.len, uint32(math.MaxUint32))
		}
	})

	t.Run("ReceiveZeroCopyRejectsInvalidDomain", func(t *testing.T) {
		tests := []struct {
			name string
			call func(*testing.T) error
		}{
			{
				name: "missing multishot internally",
				call: func(*testing.T) error {
					return ring.ioUring.receiveZeroCopy(PackDirect(0, 0, 0, 31), 0, 64, 17)
				},
			},
			{
				name: "unsupported flag",
				call: func(*testing.T) error {
					return ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 31}, 64, 17, WithIOPrio(IORING_RECVSEND_FIXED_BUF))
				},
			},
			{
				name: "negative length",
				call: func(*testing.T) error {
					return ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 31}, -1, 17)
				},
			},
			{
				name: "length above uint32",
				call: func(t *testing.T) error {
					return ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 31}, oversizedLen32(t), 17)
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				before := ring.ioUring.sqCount()
				if err := tt.call(t); !errors.Is(err, ErrInvalidParam) {
					t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
				}
				if got := ring.ioUring.sqCount(); got != before {
					t.Fatalf("SQ count = %d, want %d", got, before)
				}
			})
		}
	})

	t.Run("EpollWait", func(t *testing.T) {
		events := make([]EpollEvent, 4)
		if err := ring.EpollWait(ForFD(41), events, 0, WithFlags(IOSQE_IO_DRAIN)); err != nil {
			t.Fatalf("EpollWait: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_EPOLL_WAIT {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_EPOLL_WAIT)
		}
		if sqe.flags != IOSQE_IO_DRAIN {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_DRAIN)
		}
		if sqe.fd != 41 {
			t.Fatalf("fd = %d, want 41", sqe.fd)
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(&events[0]))) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(unsafe.Pointer(&events[0]))))
		}
		if sqe.len != uint32(len(events)) {
			t.Fatalf("len = %d, want %d", sqe.len, len(events))
		}
		if sqe.uflags != 0 {
			t.Fatalf("uflags = %#x, want 0", sqe.uflags)
		}
	})

	t.Run("EpollWaitRejectsEmptyEvents", func(t *testing.T) {
		if err := ring.EpollWait(ForFD(41), nil, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("EpollWait(nil): got %v, want %v", err, ErrInvalidParam)
		}
	})

	t.Run("EpollWaitRejectsNonZeroTimeout", func(t *testing.T) {
		events := make([]EpollEvent, 1)
		if err := ring.EpollWait(ForFD(41), events, 1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("EpollWait(timeout): got %v, want %v", err, ErrInvalidParam)
		}
	})

	t.Run("FutexWaitV", func(t *testing.T) {
		waitWords := [2]uint64{11, 22}
		waitv := unsafe.Pointer(&waitWords[0])
		if err := ring.FutexWaitV(PackDirect(0, 0, 0, 0), waitv, 2, 0, WithFlags(IOSQE_ASYNC)); err != nil {
			t.Fatalf("FutexWaitV: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_FUTEX_WAITV {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_FUTEX_WAITV)
		}
		if sqe.flags != IOSQE_ASYNC {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC)
		}
		if sqe.fd != 0 {
			t.Fatalf("fd = %#x, want 0", sqe.fd)
		}
		if sqe.addr != uint64(uintptr(waitv)) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(waitv)))
		}
		if sqe.len != 2 {
			t.Fatalf("len = %d, want 2", sqe.len)
		}
		if sqe.off != 0 || sqe.uflags != 0 || sqe.bufIndex != 0 || sqe.personality != 0 || sqe.spliceFdIn != 0 || sqe.pad != [2]uint64{} {
			t.Fatalf("unused fields are nonzero: off=%#x uflags=%#x bufIndex=%d personality=%d spliceFdIn=%d pad=%#v", sqe.off, sqe.uflags, sqe.bufIndex, sqe.personality, sqe.spliceFdIn, sqe.pad)
		}
	})

	t.Run("FutexWaitVRejectsInvalidDomain", func(t *testing.T) {
		waitWords := [2]uint64{11, 22}
		waitv := unsafe.Pointer(&waitWords[0])
		tests := []struct {
			name  string
			count uint32
			flags uint32
		}{
			{name: "flags", count: 2, flags: FUTEX2_PRIVATE | FUTEX2_SIZE_U32},
			{name: "zero count", count: 0},
			{name: "count above maximum", count: 129},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				before := ring.ioUring.sqCount()
				if err := ring.FutexWaitV(PackDirect(0, 0, 0, 0), waitv, tt.count, tt.flags); !errors.Is(err, ErrInvalidParam) {
					t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
				}
				if got := ring.ioUring.sqCount(); got != before {
					t.Fatalf("SQ count = %d, want %d", got, before)
				}
			})
		}
	})

	t.Run("Waitid", func(t *testing.T) {
		const (
			pPID     = 1
			wNoHang  = 1
			wExited  = 4
			waitOpts = wNoHang | wExited
		)
		var info [16]byte
		var pinner runtime.Pinner
		pinner.Pin(&info[0])
		defer pinner.Unpin()
		infop := unsafe.Pointer(&info[0])
		if err := ring.Waitid(PackDirect(0, 0, 0, 0), pPID, 99, infop, waitOpts, WithFlags(IOSQE_IO_LINK)); err != nil {
			t.Fatalf("Waitid: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_WAITID {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_WAITID)
		}
		if sqe.flags != IOSQE_IO_LINK {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
		}
		if sqe.fd != 99 {
			t.Fatalf("fd = %d, want 99", sqe.fd)
		}
		if sqe.off != uint64(uintptr(infop)) {
			t.Fatalf("off = %#x, want %#x", sqe.off, uint64(uintptr(infop)))
		}
		if sqe.len != pPID {
			t.Fatalf("len = %d, want %d", sqe.len, pPID)
		}
		if sqe.spliceFdIn != waitOpts {
			t.Fatalf("spliceFdIn = %#x, want %#x", sqe.spliceFdIn, waitOpts)
		}
		if sqe.pad != [2]uint64{} {
			t.Fatalf("pad = %#v, want zero", sqe.pad)
		}
		assertZeroedSQETail(t, sqe)
	})

	t.Run("WaitidAcceptsSelectorBoundaries", func(t *testing.T) {
		const wExited = 4
		tests := []struct {
			name   string
			idtype int
			id     int
		}{
			{name: "all", idtype: 0, id: -1},
			{name: "process group zero", idtype: 2, id: 0},
			{name: "pidfd zero", idtype: 3, id: 0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := ring.Waitid(PackDirect(0, 0, 0, 0), tt.idtype, tt.id, nil, wExited); err != nil {
					t.Fatalf("Waitid: %v", err)
				}
				sqe := lastSubmittedSQE(t, ring)
				if sqe.len != uint32(tt.idtype) || sqe.fd != int32(tt.id) {
					t.Fatalf("len/fd = {%d, %d}, want {%d, %d}", sqe.len, sqe.fd, tt.idtype, tt.id)
				}
			})
		}
	})

	t.Run("WaitidRejectsInvalidDomain", func(t *testing.T) {
		const wExited = 4
		type testCase struct {
			name    string
			idtype  int
			id      int
			options int
		}
		tests := []testCase{
			{name: "idtype", idtype: 4, id: 1, options: wExited},
			{name: "pid zero", idtype: 1, id: 0, options: wExited},
			{name: "pgid negative", idtype: 2, id: -1, options: wExited},
			{name: "pidfd negative", idtype: 3, id: -1, options: wExited},
			{name: "missing state option", idtype: 1, id: 1, options: 1},
			{name: "unsupported option", idtype: 1, id: 1, options: wExited | 0x10},
			{name: "negative options", idtype: 1, id: 1, options: -1},
		}
		if uint64(^uint(0)) > math.MaxUint32 {
			tests = append(tests,
				testCase{name: "id above int32", idtype: 1, id: int(int64(math.MaxInt32) + 1), options: wExited},
				testCase{name: "options above uint32", idtype: 1, id: 1, options: int(uint64(math.MaxUint32) + 1)},
			)
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				before := ring.ioUring.sqCount()
				if err := ring.Waitid(PackDirect(0, 0, 0, 0), tt.idtype, tt.id, nil, tt.options); !errors.Is(err, ErrInvalidParam) {
					t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
				}
				if got := ring.ioUring.sqCount(); got != before {
					t.Fatalf("SQ count = %d, want %d", got, before)
				}
			})
		}
	})

	t.Run("FilesUpdate", func(t *testing.T) {
		fds := []int32{3, -1, 9}
		if err := ring.FilesUpdate(PackDirect(0, 0, 0, 0), fds, 4, WithFlags(IOSQE_IO_DRAIN)); err != nil {
			t.Fatalf("FilesUpdate: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_FILES_UPDATE {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_FILES_UPDATE)
		}
		if sqe.flags != IOSQE_IO_DRAIN {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_DRAIN)
		}
		if sqe.fd != -1 {
			t.Fatalf("fd = %d, want -1", sqe.fd)
		}
		if sqe.off != 4 {
			t.Fatalf("off = %d, want 4", sqe.off)
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(&fds[0]))) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(unsafe.Pointer(&fds[0]))))
		}
		if sqe.len != uint32(len(fds)) {
			t.Fatalf("len = %d, want %d", sqe.len, len(fds))
		}
		if sqe.uflags != 0 || sqe.spliceFdIn != 0 || sqe.pad != [2]uint64{} {
			t.Fatalf("unused fields are nonzero: uflags=%#x spliceFdIn=%d pad=%#v", sqe.uflags, sqe.spliceFdIn, sqe.pad)
		}
	})

	t.Run("FilesUpdateRejectsEmptySlice", func(t *testing.T) {
		if err := ring.FilesUpdate(PackDirect(0, 0, 0, 0), nil, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FilesUpdate(nil): got %v, want %v", err, ErrInvalidParam)
		}
	})

	t.Run("FilesUpdateRejectsNegativeOffset", func(t *testing.T) {
		if err := ring.FilesUpdate(PackDirect(0, 0, 0, 0), []int32{3}, -1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FilesUpdate(offset=-1): got %v, want %v", err, ErrInvalidParam)
		}
	})

	t.Run("FilesUpdateAcceptsUint32MaximumOffset", func(t *testing.T) {
		if uint64(^uint(0)) <= math.MaxUint32 {
			t.Skip("uint32 maximum requires a wider int")
		}
		offset := int(uint64(math.MaxUint32))
		if err := ring.FilesUpdate(PackDirect(0, 0, 0, 0), []int32{3}, offset); err != nil {
			t.Fatalf("FilesUpdate: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.off != math.MaxUint32 {
			t.Fatalf("off = %d, want %d", sqe.off, uint64(math.MaxUint32))
		}
	})

	t.Run("FilesUpdateRejectsOffsetAboveUint32", func(t *testing.T) {
		before := ring.ioUring.sqCount()
		if err := ring.FilesUpdate(PackDirect(0, 0, 0, 0), []int32{3}, oversizedLen32(t)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FilesUpdate: got %v, want %v", err, ErrInvalidParam)
		}
		if got := ring.ioUring.sqCount(); got != before {
			t.Fatalf("SQ count = %d, want %d", got, before)
		}
	})

}

func TestUringCmdWrapperEncodesCommandFlags(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	cmdData := []byte{9, 8, 7, 6}
	ctx := ForFD(51).WithBufGroup(27)
	if err := ring.UringCmd(ctx, 0x1234, cmdData,
		WithFlags(IOSQE_ASYNC),
		WithUringCmdFlags(IORING_URING_CMD_FIXED)); err != nil {
		t.Fatalf("UringCmd: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_URING_CMD {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_URING_CMD)
	}
	if sqe.flags != IOSQE_ASYNC {
		t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC)
	}
	if sqe.fd != 51 {
		t.Fatalf("fd = %d, want 51", sqe.fd)
	}
	if sqe.off != 0x1234 {
		t.Fatalf("off = %#x, want %#x", sqe.off, uint64(0x1234))
	}
	if sqe.addr != uint64(uintptr(unsafe.Pointer(&cmdData[0]))) {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(unsafe.Pointer(&cmdData[0]))))
	}
	if sqe.len != uint32(len(cmdData)) {
		t.Fatalf("len = %d, want %d", sqe.len, len(cmdData))
	}
	if sqe.uflags != IORING_URING_CMD_FIXED {
		t.Fatalf("uflags = %#x, want %#x", sqe.uflags, IORING_URING_CMD_FIXED)
	}
	if sqe.bufIndex != 27 {
		t.Fatalf("bufIndex = %d, want 27", sqe.bufIndex)
	}

	t.Run("clears unowned buffer union", func(t *testing.T) {
		if err := ring.UringCmd(ForFD(51).WithBufGroup(28), 0x1234, cmdData); err != nil {
			t.Fatalf("UringCmd: %v", err)
		}
		sqe := lastSubmittedSQE(t, ring)
		if sqe.uflags != 0 || sqe.bufIndex != 0 {
			t.Fatalf("unflagged command union = {uflags:%#x bufIndex:%d}, want zero", sqe.uflags, sqe.bufIndex)
		}
	})
}

func TestMsgRingFDWrapperEncodesExpectedSQEs(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)

	tests := []struct {
		name       string
		skipCQE    bool
		wantUFlags uint32
	}{
		{
			name:       "with target CQE",
			skipCQE:    false,
			wantUFlags: 0,
		},
		{
			name:       "skip target CQE",
			skipCQE:    true,
			wantUFlags: IORING_MSG_RING_CQE_SKIP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const userData int64 = 0x112233445566778
			if err := ring.MsgRingFD(ForFD(61), 7, 3, userData, tt.skipCQE, WithFlags(IOSQE_IO_LINK)); err != nil {
				t.Fatalf("MsgRingFD: %v", err)
			}

			sqe := lastSubmittedSQE(t, ring)
			if sqe.opcode != IORING_OP_MSG_RING {
				t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_MSG_RING)
			}
			if sqe.flags != IOSQE_IO_LINK {
				t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
			}
			if sqe.fd != 61 {
				t.Fatalf("fd = %d, want 61", sqe.fd)
			}
			if sqe.off != uint64(userData) {
				t.Fatalf("off = %#x, want %#x", sqe.off, uint64(userData))
			}
			if sqe.addr != IORING_MSG_SEND_FD {
				t.Fatalf("addr = %#x, want %#x", sqe.addr, IORING_MSG_SEND_FD)
			}
			if sqe.uflags != tt.wantUFlags {
				t.Fatalf("uflags = %#x, want %#x", sqe.uflags, tt.wantUFlags)
			}
			if sqe.spliceFdIn != 4 {
				t.Fatalf("spliceFdIn = %d, want 4", sqe.spliceFdIn)
			}
			if sqe.pad[0] != 7 {
				t.Fatalf("pad[0] = %d, want 7", sqe.pad[0])
			}
			if sqe.pad[1] != 0 {
				t.Fatalf("pad[1] = %d, want 0", sqe.pad[1])
			}
			if sqe.len != 0 {
				t.Fatalf("len = %d, want 0", sqe.len)
			}
			if sqe.bufIndex != 0 {
				t.Fatalf("bufIndex = %d, want 0", sqe.bufIndex)
			}
			if sqe.personality != 0 {
				t.Fatalf("personality = %d, want 0", sqe.personality)
			}
		})
	}
}

func TestUringCmdInlineWrapperEncodesExpectedSQE(t *testing.T) {
	ring := newWrapperSyntheticSQE128SubmitRing(t, 8)
	var cmdData [uringCmdInlineDataMax]byte
	for i := range cmdData {
		cmdData[i] = byte(i + 1)
	}
	wantData := cmdData

	ctx := ForFD(51).WithBufGroup(29)
	if err := ring.UringCmdInline(ctx, 0x1234, cmdData[:],
		WithFlags(IOSQE_ASYNC|IOSQE_BUFFER_SELECT),
		WithUringCmdFlags(IORING_URING_CMD_MULTISHOT)); err != nil {
		t.Fatalf("UringCmdInline: %v", err)
	}
	clear(cmdData[:])

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	sqe := ring.ioUring.sq.sqeAt(idx)
	sqe128 := ring.ioUring.sq.sqe128At(idx)
	if sqe128 == nil {
		t.Fatal("SQE128 ring did not expose a 128-byte SQE")
	}
	if sqe.opcode != IORING_OP_URING_CMD {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_URING_CMD)
	}
	if sqe.flags != IOSQE_ASYNC|IOSQE_BUFFER_SELECT {
		t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC|IOSQE_BUFFER_SELECT)
	}
	if sqe.fd != 51 {
		t.Fatalf("fd = %d, want 51", sqe.fd)
	}
	if sqe.off != 0x1234 {
		t.Fatalf("off = %#x, want %#x", sqe.off, uint64(0x1234))
	}
	if sqe.uflags != IORING_URING_CMD_MULTISHOT {
		t.Fatalf("uflags = %#x, want %#x", sqe.uflags, IORING_URING_CMD_MULTISHOT)
	}
	if sqe.bufIndex != 29 {
		t.Fatalf("bufIndex = %d, want 29", sqe.bufIndex)
	}
	if sqe.ioprio != 0 || sqe.addr != 0 || sqe.len != 0 ||
		sqe.personality != 0 || sqe.spliceFdIn != 0 {
		t.Fatalf("unused fields are nonzero: ioprio=%d addr=%#x len=%d uflags=%#x bufIndex=%d personality=%d spliceFdIn=%d",
			sqe.ioprio, sqe.addr, sqe.len, sqe.uflags, sqe.bufIndex, sqe.personality, sqe.spliceFdIn)
	}
	gotData := *(*[uringCmdInlineDataMax]byte)(unsafe.Pointer(&sqe128.pad[0]))
	if gotData != wantData {
		t.Fatalf("inline command data = %v, want %v", gotData, wantData)
	}
	if ring.ioUring.submit.keepAlive[idx].active {
		t.Fatal("UringCmdInline retained caller command data")
	}

	t.Run("clears unowned buffer union", func(t *testing.T) {
		wideRing := newWrapperSyntheticSQE128SubmitRing(t, 8)
		if err := wideRing.UringCmdInline(ForFD(51).WithBufGroup(30), 0x1234, cmdData[:]); err != nil {
			t.Fatalf("UringCmdInline: %v", err)
		}
		sqe := lastSubmittedSQE(t, wideRing)
		if sqe.uflags != 0 || sqe.bufIndex != 0 {
			t.Fatalf("unflagged command union = {uflags:%#x bufIndex:%d}, want zero", sqe.uflags, sqe.bufIndex)
		}
	})

	t.Run("requires SQE128", func(t *testing.T) {
		narrowRing := newWrapperSyntheticSubmitRing(t, 8)
		before := narrowRing.ioUring.sqCount()
		err := narrowRing.UringCmdInline(ForFD(1), 0, nil)
		if !errors.Is(err, ErrNotSupported) {
			t.Fatalf("UringCmdInline: got %v, want %v", err, ErrNotSupported)
		}
		if got := narrowRing.ioUring.sqCount(); got != before {
			t.Fatalf("SQ count = %d, want %d", got, before)
		}
	})

	t.Run("rejects command data above 80 bytes", func(t *testing.T) {
		wideRing := newWrapperSyntheticSQE128SubmitRing(t, 8)
		before := wideRing.ioUring.sqCount()
		err := wideRing.UringCmdInline(ForFD(1), 0, make([]byte, uringCmdInlineDataMax+1))
		if !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("UringCmdInline: got %v, want %v", err, ErrInvalidParam)
		}
		if got := wideRing.ioUring.sqCount(); got != before {
			t.Fatalf("SQ count = %d, want %d", got, before)
		}
	})

	t.Run("does not allocate", func(t *testing.T) {
		wideRing := newWrapperSyntheticSQE128SubmitRing(t, 128)
		cmdData := []byte{1, 2, 3, 4}
		var callErr error
		allocs := testing.AllocsPerRun(100, func() {
			callErr = wideRing.UringCmdInline(ForFD(1), 0, cmdData)
		})
		if callErr != nil {
			t.Fatalf("UringCmdInline: %v", callErr)
		}
		if allocs != 0 {
			t.Fatalf("allocations = %v, want 0", allocs)
		}
	})
}

func TestUringCmd128WrapperEncodesCommandFlags(t *testing.T) {
	ring := newWrapperSyntheticSQE128SubmitRing(t, 8)
	ring.ioUring.ops = []ioUringProbeOp{{op: IORING_OP_URING_CMD128, flags: IO_URING_OP_SUPPORTED}}
	cmdData := []byte{1, 2, 3, 4}
	ctx := ForFD(51).WithBufGroup(31)

	if err := ring.UringCmd128(ctx, 0x5678, cmdData,
		WithUringCmdFlags(IORING_URING_CMD_FIXED)); err != nil {
		t.Fatalf("UringCmd128: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_URING_CMD128 {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_URING_CMD128)
	}
	if sqe.uflags != IORING_URING_CMD_FIXED {
		t.Fatalf("uflags = %#x, want %#x", sqe.uflags, IORING_URING_CMD_FIXED)
	}
	if sqe.bufIndex != 31 {
		t.Fatalf("bufIndex = %d, want 31", sqe.bufIndex)
	}

	t.Run("clears unowned buffer union", func(t *testing.T) {
		wideRing := newWrapperSyntheticSQE128SubmitRing(t, 8)
		wideRing.ioUring.ops = []ioUringProbeOp{{op: IORING_OP_URING_CMD128, flags: IO_URING_OP_SUPPORTED}}
		if err := wideRing.UringCmd128(ForFD(51).WithBufGroup(32), 0x5678, cmdData); err != nil {
			t.Fatalf("UringCmd128: %v", err)
		}
		sqe := lastSubmittedSQE(t, wideRing)
		if sqe.uflags != 0 || sqe.bufIndex != 0 {
			t.Fatalf("unflagged command union = {uflags:%#x bufIndex:%d}, want zero", sqe.uflags, sqe.bufIndex)
		}
	})
}

func TestUringCmdWrappersRejectInvalidFlagShapes(t *testing.T) {
	builders := []struct {
		name    string
		newRing func(*testing.T) *Uring
		submit  func(*Uring, SQEContext, uint32) error
	}{
		{
			name: "external payload",
			newRing: func(t *testing.T) *Uring {
				return newWrapperSyntheticSubmitRing(t, 8)
			},
			submit: func(ring *Uring, ctx SQEContext, flags uint32) error {
				return ring.UringCmd(ctx, 0, nil, WithUringCmdFlags(flags))
			},
		},
		{
			name: "inline payload",
			newRing: func(t *testing.T) *Uring {
				return newWrapperSyntheticSQE128SubmitRing(t, 8)
			},
			submit: func(ring *Uring, ctx SQEContext, flags uint32) error {
				return ring.UringCmdInline(ctx, 0, nil, WithUringCmdFlags(flags))
			},
		},
		{
			name: "wide opcode",
			newRing: func(t *testing.T) *Uring {
				ring := newWrapperSyntheticSQE128SubmitRing(t, 8)
				ring.ioUring.ops = []ioUringProbeOp{{op: IORING_OP_URING_CMD128, flags: IO_URING_OP_SUPPORTED}}
				return ring
			},
			submit: func(ring *Uring, ctx SQEContext, flags uint32) error {
				return ring.UringCmd128(ctx, 0, nil, WithUringCmdFlags(flags))
			},
		},
	}

	invalid := []struct {
		name  string
		ctx   SQEContext
		flags uint32
	}{
		{name: "unknown bit", ctx: ForFD(51), flags: 1 << 2},
		{
			name:  "fixed and multishot",
			ctx:   ForFD(51).WithBufGroup(1).OrFlags(IOSQE_BUFFER_SELECT),
			flags: IORING_URING_CMD_MASK,
		},
		{
			name:  "multishot without buffer select",
			ctx:   ForFD(51).WithBufGroup(1),
			flags: IORING_URING_CMD_MULTISHOT,
		},
		{
			name: "buffer select without multishot",
			ctx:  ForFD(51).WithBufGroup(1).OrFlags(IOSQE_BUFFER_SELECT),
		},
	}

	for _, builder := range builders {
		for _, tt := range invalid {
			t.Run(builder.name+"/"+tt.name, func(t *testing.T) {
				ring := builder.newRing(t)
				before := ring.ioUring.sqCount()
				if err := builder.submit(ring, tt.ctx, tt.flags); !errors.Is(err, ErrInvalidParam) {
					t.Fatalf("submit = %v, want %v", err, ErrInvalidParam)
				}
				if got := ring.ioUring.sqCount(); got != before {
					t.Fatalf("sqCount = %d, want %d", got, before)
				}
			})
		}
	}
}

func newRegistrationFailureTestRing() *Uring {
	head, tail, mask := uint32(0), uint32(0), uint32(EntriesNano-1)
	return &Uring{
		ioUring: &ioUring{
			ringFd: -1,
			params: &ioUringParams{
				sqEntries: EntriesNano,
				cqEntries: EntriesNano * 2,
			},
			sq: ioUringSq{
				kHead:     &head,
				kTail:     &tail,
				kRingMask: &mask,
			},
		},
		Features: &Features{
			SQEntries: EntriesNano,
			CQEntries: EntriesNano * 2,
		},
	}
}

func TestZCRXExportFailureReturnsInvalidFD(t *testing.T) {
	fd, err := newRegistrationFailureTestRing().ZCRXExport(1)
	if err == nil {
		t.Fatal("ZCRXExport error = nil, want invalid-ring failure")
	}
	if fd != iofd.InvalidFD {
		t.Fatalf("ZCRXExport fd = %d, want %d", fd, iofd.InvalidFD)
	}
}

func TestAdvancedRegistrationWrappersReturnKernelErrors(t *testing.T) {
	ring := newRegistrationFailureTestRing()
	cloneSource := newRegistrationFailureTestRing()
	cloneSource.bufs = [][]byte{[]byte("source")}
	area := ZCRXAreaReg{}
	region := RegionDesc{}

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "RegisterZCRXIfq",
			call: func() error {
				_, err := ring.RegisterZCRXIfq(1, 2, 4, &area, &region, 0)
				return err
			},
		},
		{
			name: "ZCRXFlushRQ",
			call: func() error { return ring.ZCRXFlushRQ(1) },
		},
		{
			name: "RegisterMemRegion",
			call: func() error { return ring.RegisterMemRegion(&region, 0) },
		},
		{
			name: "RegisterNAPI",
			call: func() error { return ring.RegisterNAPI(0, true, IO_URING_NAPI_TRACKING_STATIC) },
		},
		{
			name: "NAPIAddStaticID",
			call: func() error { return ring.NAPIAddStaticID(7) },
		},
		{
			name: "NAPIDelStaticID",
			call: func() error { return ring.NAPIDelStaticID(7) },
		},
		{
			name: "UnregisterNAPI",
			call: func() error { return ring.UnregisterNAPI() },
		},
		{
			name: "CloneBuffers",
			call: func() error { return ring.CloneBuffers(cloneSource, 0, 0, 1, true) },
		},
		{
			name: "CloneBuffersFromRegistered",
			call: func() error {
				return ring.CloneBuffersFromRegistered(cloneSource, math.MaxUint32, 0, 0, 1, true)
			},
		},
		{
			name: "ZCRXExport",
			call: func() error {
				_, err := ring.ZCRXExport(1)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("error = nil, want kernel error from invalid ring fd")
			}
		})
	}
}

func TestRegisterZCRXIfqRequiresAdvertisedRxBufferLength(t *testing.T) {
	ring := newRegistrationFailureTestRing()
	area := ZCRXAreaReg{}
	region := RegionDesc{}

	if _, err := ring.RegisterZCRXIfq(1, 2, 4, &area, &region, uint32(os.Getpagesize()+1)); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("RegisterZCRXIfq non-power-of-two error = %v, want %v", err, ErrInvalidParam)
	}

	pageSize := uint32(os.Getpagesize())
	if _, err := ring.registerZCRXIfqWithFeatures(1, 2, 4, &area, &region, pageSize, 0); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("registerZCRXIfqWithFeatures without capability error = %v, want %v", err, ErrNotSupported)
	}
	if _, err := ring.registerZCRXIfqWithFeatures(1, 2, 4, &area, &region, pageSize, ZCRX_FEATURE_RX_PAGE_SIZE); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("registerZCRXIfqWithFeatures with capability error = %v, want a kernel registration error", err)
	}
}

func TestZCRXIfqRegistrationResultPreservesKernelMutations(t *testing.T) {
	reg := ZCRXIfqReg{
		RqEntries: 3,
		Offsets: ZCRXOffsets{
			Head: 64,
			Tail: 128,
			Rqes: 256,
		},
		ZcrxID:   17,
		RxBufLen: 8192,
	}

	// Linux replaces the requested cardinality with the effective value.
	reg.RqEntries = 4
	got := zcrxIfqRegistrationResult(&reg)
	want := ZCRXIfqRegistration{
		ID:        17,
		RqEntries: 4,
		Offsets:   reg.Offsets,
		RxBufLen:  8192,
	}
	if got != want {
		t.Fatalf("registration result = %+v, want %+v", got, want)
	}
}

func TestResizeRingsPropagatesRegisterFailure(t *testing.T) {
	ring := newRegistrationFailureTestRing()
	if err := ring.ResizeRings(EntriesNano, EntriesNano*2); err == nil {
		t.Fatal("ResizeRings error = nil, want kernel error from invalid ring fd")
	}
	if ring.Features.SQEntries != EntriesNano {
		t.Fatalf("SQEntries = %d, want %d", ring.Features.SQEntries, EntriesNano)
	}
	if ring.Features.CQEntries != EntriesNano*2 {
		t.Fatalf("CQEntries = %d, want %d", ring.Features.CQEntries, EntriesNano*2)
	}
}

func poisonNextExtendedSQE(pool *ContextPools) {
	pool.extended[0].ext.SQE = ioUringSqe{
		opcode:      IORING_OP_SOCKET,
		flags:       0xff,
		ioprio:      0xffff,
		fd:          -99,
		off:         99,
		addr:        99,
		len:         99,
		uflags:      0xdeadbeef,
		userData:    99,
		bufIndex:    7,
		personality: 3,
		spliceFdIn:  11,
		pad:         [2]uint64{13, 17},
	}
}

func assertNoStaleExtendedSQEFields(t *testing.T, sqe *ioUringSqe, wantBufIndex uint16) {
	t.Helper()

	if got := sqe.off; got != 0 {
		t.Fatalf("SQE.off = %d, want 0", got)
	}
	if got := sqe.uflags; got != 0 {
		t.Fatalf("SQE.uflags = %#x, want 0", got)
	}
	if got := sqe.bufIndex; got != wantBufIndex {
		t.Fatalf("SQE.bufIndex = %d, want %d", got, wantBufIndex)
	}
	if got := sqe.personality; got != 0 {
		t.Fatalf("SQE.personality = %d, want 0", got)
	}
	if got := sqe.spliceFdIn; got != 0 {
		t.Fatalf("SQE.spliceFdIn = %d, want 0", got)
	}
	if got := sqe.pad; got != [2]uint64{} {
		t.Fatalf("SQE.pad = %#v, want zero", got)
	}
}

func TestIncrementalRecvClearsBorrowedExtendedSQE(t *testing.T) {
	ring := newWrapperTestRing(t)
	pool := NewContextPools(1)
	poisonNextExtendedSQE(pool)

	receiver := NewIncrementalReceiver(ring, pool, 7, 256, make([]byte, 256), 1)
	if err := receiver.Recv(11, nil); err != nil {
		t.Fatalf("Recv: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if got, want := sqe.opcode, uint8(IORING_OP_RECV); got != want {
		t.Fatalf("SQE.opcode = %d, want %d", got, want)
	}
	assertNoStaleExtendedSQEFields(t, sqe, 7)
}

func TestZCTrackerSendZCClearsBorrowedExtendedSQE(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	pool := NewContextPools(1)
	poisonNextExtendedSQE(pool)

	tracker := NewZCTracker(ring, pool)
	if err := tracker.SendZC(iofd.FD(11), []byte("payload"), nil); err != nil {
		t.Fatalf("SendZC: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if got, want := sqe.opcode, uint8(IORING_OP_SEND_ZC); got != want {
		t.Fatalf("SQE.opcode = %d, want %d", got, want)
	}
	assertNoStaleExtendedSQEFields(t, sqe, 0)
}

func TestZCTrackerSendZCFixedClearsBorrowedExtendedSQE(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	ring.bufs = [][]byte{[]byte("registered-payload")}
	pool := NewContextPools(1)
	poisonNextExtendedSQE(pool)

	tracker := NewZCTracker(ring, pool)
	if err := tracker.SendZCFixed(iofd.FD(11), 0, 0, 10, nil); err != nil {
		t.Fatalf("SendZCFixed: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if got, want := sqe.opcode, uint8(IORING_OP_SEND_ZC); got != want {
		t.Fatalf("SQE.opcode = %d, want %d", got, want)
	}
	assertNoStaleExtendedSQEFields(t, sqe, 0)
}

func newWrapperSyntheticSubmitRing(t *testing.T, entries uint32) *Uring {
	t.Helper()

	head := new(uint32)
	tail := new(uint32)
	mask := new(uint32)
	ringEntries := new(uint32)
	flags := new(uint32)
	*mask = entries - 1
	*ringEntries = entries
	sqes := make([]ioUringSqe, entries)
	t.Cleanup(func() {
		runtime.KeepAlive(sqes)
	})

	return &Uring{
		ioUring: &ioUring{
			params: &ioUringParams{},
			sq: ioUringSq{
				kHead:        head,
				kTail:        tail,
				kRingMask:    mask,
				kRingEntries: ringEntries,
				kFlags:       flags,
				array:        make([]uint32, entries),
				sqesPtr:      unsafe.Pointer(unsafe.SliceData(sqes)),
				sqeStride:    unsafe.Sizeof(ioUringSqe{}),
			},
			submit: submitState{
				keepAlive: make([]submitKeepAlive, entries),
				shared:    true,
			},
			ringFd:   -1,
			pollerFd: -1,
		},
	}
}

func newWrapperSyntheticSQE128SubmitRing(t *testing.T, entries uint32) *Uring {
	t.Helper()

	head := new(uint32)
	tail := new(uint32)
	mask := new(uint32)
	ringEntries := new(uint32)
	flags := new(uint32)
	*mask = entries - 1
	*ringEntries = entries
	sqes := make([]ioUringSqe128, entries)
	t.Cleanup(func() {
		runtime.KeepAlive(sqes)
	})

	return &Uring{
		ioUring: &ioUring{
			params: &ioUringParams{flags: IORING_SETUP_SQE128},
			sq: ioUringSq{
				kHead:        head,
				kTail:        tail,
				kRingMask:    mask,
				kRingEntries: ringEntries,
				kFlags:       flags,
				array:        make([]uint32, entries),
				sqesPtr:      unsafe.Pointer(unsafe.SliceData(sqes)),
				sqeStride:    unsafe.Sizeof(ioUringSqe128{}),
			},
			submit: submitState{
				keepAlive: make([]submitKeepAlive, entries),
				shared:    true,
			},
			ringFd:   -1,
			pollerFd: -1,
		},
	}
}

func TestFixedWidthCarrierBoundariesEncodeWithoutNarrowing(t *testing.T) {
	const maxU32 = uint32(math.MaxUint32)

	tests := []struct {
		name       string
		wantOpcode uint8
		call       func(*Uring) error
		check      func(*testing.T, *ioUringSqe)
	}{
		{
			name:       "splice",
			wantOpcode: IORING_OP_SPLICE,
			call: func(ring *Uring) error {
				return ring.Splice(ForFD(9), iofd.FD(math.MaxInt32), 1, WithSpliceFlags(maxU32))
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.spliceFdIn != math.MaxInt32 || sqe.uflags != maxU32 {
					t.Fatalf("spliceFdIn/uflags = {%d, %#x}, want {%d, %#x}", sqe.spliceFdIn, sqe.uflags, int32(math.MaxInt32), maxU32)
				}
			},
		},
		{
			name:       "tee",
			wantOpcode: IORING_OP_TEE,
			call: func(ring *Uring) error {
				return ring.Tee(ForFD(9), iofd.FD(math.MaxInt32), 1, WithSpliceFlags(maxU32))
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.spliceFdIn != math.MaxInt32 || sqe.uflags != maxU32 {
					t.Fatalf("spliceFdIn/uflags = {%d, %#x}, want {%d, %#x}", sqe.spliceFdIn, sqe.uflags, int32(math.MaxInt32), maxU32)
				}
			},
		},
		{
			name:       "openat",
			wantOpcode: IORING_OP_OPENAT,
			call: func(ring *Uring) error {
				return ring.OpenAt(ForFD(AT_FDCWD), "carrier-open", maxU32, 0)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("open flags = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
		{
			name:       "fadvise",
			wantOpcode: IORING_OP_FADVISE,
			call: func(ring *Uring) error {
				return ring.FileAdvise(ForFD(9), 0, 1, maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("advice = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
		{
			name:       "sync file range",
			wantOpcode: IORING_OP_SYNC_FILE_RANGE,
			call: func(ring *Uring) error {
				return ring.SyncFileRange(ForFD(9), 0, 1, maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("sync flags = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
		{
			name:       "statx",
			wantOpcode: IORING_OP_STATX,
			call: func(ring *Uring) error {
				return ring.Statx(ForFD(AT_FDCWD), "carrier-statx", maxU32, maxU32, new(Statx))
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 || sqe.len != maxU32 {
					t.Fatalf("statx flags/mask = {%#x, %#x}, want {%#x, %#x}", sqe.uflags, sqe.len, maxU32, maxU32)
				}
			},
		},
		{
			name:       "renameat",
			wantOpcode: IORING_OP_RENAMEAT,
			call: func(ring *Uring) error {
				return ring.RenameAt(ForFD(AT_FDCWD), "carrier-old", "carrier-new", maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("rename flags = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
		{
			name:       "unlinkat",
			wantOpcode: IORING_OP_UNLINKAT,
			call: func(ring *Uring) error {
				return ring.UnlinkAt(ForFD(AT_FDCWD), "carrier-unlink", maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("unlink flags = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
		{
			name:       "linkat",
			wantOpcode: IORING_OP_LINKAT,
			call: func(ring *Uring) error {
				return ring.LinkAt(ForFD(AT_FDCWD), iofd.FD(math.MaxInt32), "carrier-old", "carrier-new", maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.fd != math.MaxInt32 || sqe.uflags != maxU32 {
					t.Fatalf("oldDirfd/flags = {%d, %#x}, want {%d, %#x}", sqe.fd, sqe.uflags, int32(math.MaxInt32), maxU32)
				}
			},
		},
		{
			name:       "fsetxattr",
			wantOpcode: IORING_OP_FSETXATTR,
			call: func(ring *Uring) error {
				return ring.FSetXattr(ForFD(9), "user.carrier", []byte{1}, maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("fsetxattr flags = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
		{
			name:       "setxattr",
			wantOpcode: IORING_OP_SETXATTR,
			call: func(ring *Uring) error {
				return ring.SetXattr(ForFD(0), "carrier-path", "user.carrier", []byte{1}, maxU32)
			},
			check: func(t *testing.T, sqe *ioUringSqe) {
				if sqe.uflags != maxU32 {
					t.Fatalf("setxattr flags = %#x, want %#x", sqe.uflags, maxU32)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			if err := tt.call(ring); err != nil {
				t.Fatalf("submit: %v", err)
			}
			sqe := lastSubmittedSQE(t, ring)
			if sqe.opcode != tt.wantOpcode {
				t.Fatalf("opcode = %d, want %d", sqe.opcode, tt.wantOpcode)
			}
			tt.check(t, sqe)
		})
	}
}

func TestDirectTargetInputDomains(t *testing.T) {
	const collisionIndex = IORING_FILE_INDEX_ALLOC - 1

	tests := []struct {
		name string
		call func(*testing.T, *Uring) error
	}{
		{
			name: "accept direct sentinel collision",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.AcceptDirect(ForFD(11), collisionIndex)
			},
		},
		{
			name: "accept direct multishot explicit slot",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.SubmitAcceptDirectMultishot(ForFD(11), 0)
			},
		},
		{
			name: "socket direct sentinel collision",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.SocketDirect(ForFD(0), AF_INET, SOCK_STREAM|SOCK_NONBLOCK, IPPROTO_TCP, collisionIndex)
			},
		},
		{
			name: "socket raw direct sentinel collision",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.SocketRaw(ForFD(0), AF_INET, SOCK_STREAM|SOCK_NONBLOCK, IPPROTO_TCP, WithFileIndex(collisionIndex))
			},
		},
		{
			name: "socket direct close-on-exec",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.SocketDirect(ForFD(0), AF_INET, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, 0)
			},
		},
		{
			name: "message ring sentinel collision",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.MsgRingFD(ForFD(12), 0, collisionIndex, 0, false)
			},
		},
		{
			name: "fixed install negative index",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.FixedFdInstall(ForFD(0), -1, 0)
			},
		},
		{
			name: "fixed install oversized index",
			call: func(t *testing.T, ring *Uring) error {
				return ring.FixedFdInstall(ForFD(0), oversizedSignedInt32(t), 0)
			},
		},
		{
			name: "fixed install unknown flag",
			call: func(_ *testing.T, ring *Uring) error {
				return ring.FixedFdInstall(ForFD(0), 0, IORING_FIXED_FD_NO_CLOEXEC<<1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			if err := tt.call(t, ring); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
			}
			if got := ring.ioUring.sqCount(); got != 0 {
				t.Fatalf("submitted SQEs = %d, want 0", got)
			}
		})
	}
}

func TestSocketRawFileIndexOptionEncodesDirectTarget(t *testing.T) {
	tests := []struct {
		name    string
		options []OpOptionFunc
		want    int32
	}{
		{name: "unspecified", want: 0},
		{name: "slot zero", options: []OpOptionFunc{WithFileIndex(0)}, want: 1},
		{name: "explicit slot", options: []OpOptionFunc{WithFileIndex(21)}, want: 22},
		{name: "allocation", options: []OpOptionFunc{WithFileIndex(IORING_FILE_INDEX_ALLOC)}, want: -1},
		{
			name: "explicit zero pointer",
			options: []OpOptionFunc{func(opt *OpOption) {
				opt.FileIndex = new(uint32)
			}},
			want: 1,
		},
		{
			name: "clear explicit target",
			options: []OpOptionFunc{WithFileIndex(21), func(opt *OpOption) {
				opt.FileIndex = nil
			}},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			if err := ring.SocketRaw(ForFD(0), AF_INET, SOCK_STREAM|SOCK_NONBLOCK, IPPROTO_TCP, tt.options...); err != nil {
				t.Fatalf("SocketRaw: %v", err)
			}
			if got := lastSubmittedSQE(t, ring).spliceFdIn; got != tt.want {
				t.Fatalf("spliceFdIn = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMsgRingFDAllocationTargetPreservesSentinel(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	if err := ring.MsgRingFD(ForFD(12), 3, IORING_FILE_INDEX_ALLOC, 9, false); err != nil {
		t.Fatalf("MsgRingFD: %v", err)
	}
	if got := lastSubmittedSQE(t, ring).spliceFdIn; got != -1 {
		t.Fatalf("spliceFdIn = %d, want -1", got)
	}
}

func TestFixedFdInstallValidDomainEncodesSQE(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	if err := ring.FixedFdInstall(ForFD(0), math.MaxInt32, IORING_FIXED_FD_NO_CLOEXEC); err != nil {
		t.Fatalf("FixedFdInstall: %v", err)
	}
	sqe := lastSubmittedSQE(t, ring)
	if sqe.fd != math.MaxInt32 {
		t.Fatalf("fd = %d, want %d", sqe.fd, int32(math.MaxInt32))
	}
	if sqe.flags != IOSQE_FIXED_FILE {
		t.Fatalf("flags = %#x, want %#x", sqe.flags, IOSQE_FIXED_FILE)
	}
	if sqe.uflags != IORING_FIXED_FD_NO_CLOEXEC {
		t.Fatalf("uflags = %#x, want %#x", sqe.uflags, IORING_FIXED_FD_NO_CLOEXEC)
	}
}

func TestZCTrackerSendZCFixedRejectsOverflowingRange(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	ring.bufs = [][]byte{[]byte("registered-payload")}
	pool := NewContextPools(1)

	tracker := NewZCTracker(ring, pool)
	maxInt := int(^uint(0) >> 1)
	if err := tracker.SendZCFixed(iofd.FD(11), 0, 1, maxInt, nil); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZCFixed overflow range error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after invalid range = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after invalid range = %d, want 0", got)
	}
}

func TestZCTrackerSendZCRejectsInvalidOptionRange(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	pool := NewContextPools(1)

	tracker := NewZCTracker(ring, pool)
	err := tracker.SendZC(iofd.FD(11), nil, nil)
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZC nil buffer error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after nil buffer = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after nil buffer = %d, want 0", got)
	}

	err = tracker.SendZC(iofd.FD(11), []byte("payload"), nil, WithOffset(-1))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZC negative option offset error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after invalid option range = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after invalid option range = %d, want 0", got)
	}

	err = tracker.SendZC(iofd.FD(11), []byte("payload"), nil, WithOffset(4), WithN(4))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZC overflowing option range error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after overflowing option range = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after overflowing option range = %d, want 0", got)
	}

	err = tracker.SendZC(iofd.FD(11), []byte("payload"), nil, WithN(0))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZC zero option length error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after zero option length = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after zero option length = %d, want 0", got)
	}

	err = tracker.SendZC(iofd.FD(11), []byte("payload"), nil, WithOffset(7))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZC empty option subrange error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after empty option subrange = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after empty option subrange = %d, want 0", got)
	}
}

func TestZCTrackerSendZCRejectsOversizedSignedInt32(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	pool := NewContextPools(1)

	tracker := NewZCTracker(ring, pool)
	payload := mappedTestSlice[byte](t, oversizedSignedInt32(t))
	err := tracker.SendZC(iofd.FD(11), payload, nil)
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZC oversized len error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after oversized len = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after oversized len = %d, want 0", got)
	}
}

func TestZCTrackerSendZCEncodesOptionRange(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	pool := NewContextPools(1)
	payload := []byte("payload")

	tracker := NewZCTracker(ring, pool)
	if err := tracker.SendZC(iofd.FD(11), payload, nil, WithOffset(2)); err != nil {
		t.Fatalf("SendZC offset-only range: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	base := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload))))
	if sqe.addr != base+2 {
		t.Fatalf("SendZC offset-only addr = %#x, want %#x", sqe.addr, base+2)
	}
	if sqe.len != uint32(len(payload)-2) {
		t.Fatalf("SendZC offset-only len = %d, want %d", sqe.len, len(payload)-2)
	}
}

func TestZCTrackerSendZCFixedRejectsInvalidOptionRange(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	ring.bufs = [][]byte{[]byte("registered-payload")}
	pool := NewContextPools(1)

	tracker := NewZCTracker(ring, pool)
	err := tracker.SendZCFixed(iofd.FD(11), 0, 2, 5, nil, WithOffset(-1))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZCFixed negative option offset error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after invalid option range = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after invalid option range = %d, want 0", got)
	}

	err = tracker.SendZCFixed(iofd.FD(11), 0, 2, 5, nil, WithOffset(3), WithN(3))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZCFixed overflowing option range error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after overflowing option range = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after overflowing option range = %d, want 0", got)
	}

	err = tracker.SendZCFixed(iofd.FD(11), 0, 2, 5, nil, WithN(0))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZCFixed zero option length error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after zero option length = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after zero option length = %d, want 0", got)
	}

	err = tracker.SendZCFixed(iofd.FD(11), 0, 2, 5, nil, WithOffset(5))
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("SendZCFixed empty option subrange error = %v, want %v", err, ErrInvalidParam)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after empty option subrange = %d, want 1", got)
	}
	if got := ring.ioUring.sqCount(); got != 0 {
		t.Fatalf("submitted SQEs after empty option subrange = %d, want 0", got)
	}
}

func TestZCTrackerSendZCFixedEncodesOptionRange(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)
	ring.bufs = [][]byte{[]byte("registered-payload")}
	pool := NewContextPools(1)

	tracker := NewZCTracker(ring, pool)
	if err := tracker.SendZCFixed(iofd.FD(11), 0, 2, 10, nil, WithOffset(3)); err != nil {
		t.Fatalf("SendZCFixed offset-only range: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	buf := ring.bufs[0]
	base := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf))))
	wantOffset := uint64(2 + 3)
	if sqe.addr != base+wantOffset {
		t.Fatalf("SendZCFixed offset-only addr = %#x, want %#x", sqe.addr, base+wantOffset)
	}
	if sqe.len != uint32(10-3) {
		t.Fatalf("SendZCFixed offset-only len = %d, want %d", sqe.len, 10-3)
	}
}
