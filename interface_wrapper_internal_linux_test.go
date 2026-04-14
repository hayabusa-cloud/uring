// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"net"
	"testing"
	"unsafe"
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
	ring := newWrapperTestRing(t)

	flags := uint8(IOSQE_IO_LINK)
	ioprio := uint16(IORING_ACCEPT_POLL_FIRST)
	if err := ring.SubmitAcceptDirectMultishot(ForFD(23), 4, WithFlags(flags), WithIOPrio(ioprio)); err != nil {
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
	if sqe.spliceFdIn != 5 {
		t.Fatalf("spliceFdIn = %d, want 5", sqe.spliceFdIn)
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
		buffers := [][]byte{make([]byte, 8)}
		oob := []byte{5, 6, 7, 8}

		if err := ring.RecvMsg(
			PackDirect(0, 0, 0, 0),
			&mockPollFd{fd: 22},
			buffers,
			oob,
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
		if sqe.addr == 0 {
			t.Fatal("addr = 0, want non-zero message header pointer")
		}
		if sqe.uflags != MSG_WAITALL {
			t.Fatalf("uflags = %#x, want %#x", sqe.uflags, MSG_WAITALL)
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
		if sqe.off != 3 {
			t.Fatalf("off = %d, want 3", sqe.off)
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))))
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

func TestSubmitReceiveMultishotWrapperEncodesExpectedSQEs(t *testing.T) {
	t.Run("fixed buffer", func(t *testing.T) {
		ring := newWrapperTestRing(t)
		buf := make([]byte, 16)
		fd := &mockPollFd{fd: 28}

		if err := ring.SubmitReceiveMultishot(
			PackDirect(0, 0, 0, 0),
			fd,
			buf,
			WithFlags(IOSQE_IO_LINK),
			WithIOPrio(15),
			WithOffset(4),
			WithN(6),
		); err != nil {
			t.Fatalf("SubmitReceiveMultishot(fixed): %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_RECV {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_RECV)
		}
		if sqe.flags != IOSQE_IO_LINK|ring.readLikeOpFlags {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK|ring.readLikeOpFlags)
		}
		if sqe.ioprio != 15|IORING_RECV_MULTISHOT {
			t.Fatalf("ioprio = %#x, want %#x", sqe.ioprio, 15|IORING_RECV_MULTISHOT)
		}
		if sqe.fd != int32(fd.Fd()) {
			t.Fatalf("fd = %d, want %d", sqe.fd, fd.Fd())
		}
		if sqe.off != 4 {
			t.Fatalf("off = %d, want 4", sqe.off)
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))))
		}
		if sqe.len != 6 {
			t.Fatalf("len = %d, want 6", sqe.len)
		}
		if sqe.uflags != MSG_WAITALL {
			t.Fatalf("uflags = %#x, want %#x", sqe.uflags, MSG_WAITALL)
		}
		if sqe.bufIndex != 0 {
			t.Fatalf("bufIndex = %d, want 0", sqe.bufIndex)
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

		fd := &mockPollFd{fd: 29}
		if err := ring.SubmitReceiveMultishot(
			PackDirect(0, 0, 0, 0),
			fd,
			nil,
			WithFlags(IOSQE_ASYNC),
			WithIOPrio(10),
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
		if sqe.ioprio != 10|IORING_RECV_MULTISHOT {
			t.Fatalf("ioprio = %#x, want %#x", sqe.ioprio, 10|IORING_RECV_MULTISHOT)
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
	rings.buffers = [][]byte{backing}
	rings.sizes = []int{bufSize}
	rings.masks = []uintptr{bufCount - 1}

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
	if iter.gidOffset != groupID {
		t.Fatalf("gidOffset = %d, want %d", iter.gidOffset, groupID)
	}
	if iter.group != groupID {
		t.Fatalf("group = %d, want %d", iter.group, groupID)
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
			WithIOPrio(13),
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
		if sqe.ioprio != 13 {
			t.Fatalf("ioprio = %d, want 13", sqe.ioprio)
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
		if err := ring.FutexWaitV(PackDirect(0, 0, 0, 0), waitv, 2, FUTEX2_PRIVATE|FUTEX2_SIZE_U32, WithFlags(IOSQE_ASYNC)); err != nil {
			t.Fatalf("FutexWaitV: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_FUTEX_WAITV {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_FUTEX_WAITV)
		}
		if sqe.flags != IOSQE_ASYNC {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_ASYNC)
		}
		if sqe.fd != int32(FUTEX2_PRIVATE|FUTEX2_SIZE_U32) {
			t.Fatalf("fd = %#x, want %#x", sqe.fd, FUTEX2_PRIVATE|FUTEX2_SIZE_U32)
		}
		if sqe.addr != uint64(uintptr(waitv)) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(waitv)))
		}
		if sqe.len != 2 {
			t.Fatalf("len = %d, want 2", sqe.len)
		}
	})

	t.Run("Waitid", func(t *testing.T) {
		var info [16]byte
		infop := unsafe.Pointer(&info[0])
		if err := ring.Waitid(PackDirect(0, 0, 0, 0), 7, 99, infop, 5, WithFlags(IOSQE_IO_LINK)); err != nil {
			t.Fatalf("Waitid: %v", err)
		}

		sqe := lastSubmittedSQE(t, ring)
		if sqe.opcode != IORING_OP_WAITID {
			t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_WAITID)
		}
		if sqe.flags != IOSQE_IO_LINK {
			t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK)
		}
		if sqe.fd != 7 {
			t.Fatalf("fd = %d, want 7", sqe.fd)
		}
		if sqe.addr != uint64(uintptr(infop)) {
			t.Fatalf("addr = %#x, want %#x", sqe.addr, uint64(uintptr(infop)))
		}
		if sqe.len != 99 {
			t.Fatalf("len = %d, want 99", sqe.len)
		}
		if sqe.uflags != 5 {
			t.Fatalf("uflags = %#x, want %#x", sqe.uflags, 5)
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
	})

	t.Run("FilesUpdateRejectsEmptySlice", func(t *testing.T) {
		if err := ring.FilesUpdate(PackDirect(0, 0, 0, 0), nil, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FilesUpdate(nil): got %v, want %v", err, ErrInvalidParam)
		}
	})

	t.Run("UringCmd", func(t *testing.T) {
		cmdData := []byte{9, 8, 7, 6}
		if err := ring.UringCmd(ForFD(51), 0x1234, cmdData, WithFlags(IOSQE_ASYNC)); err != nil {
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
	})
}

func TestMsgRingFDWrapperEncodesExpectedSQEs(t *testing.T) {
	ring := newWrapperTestRing(t)

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
