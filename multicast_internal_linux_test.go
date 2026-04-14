// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"testing"
	"unsafe"

	"code.hybscloud.com/iofd"
)

type testSendTargets struct {
	fds []iofd.FD
}

func (t testSendTargets) Count() int { return len(t.fds) }
func (t testSendTargets) FD(i int) iofd.FD {
	return t.fds[i]
}

func assertFixedSendSQE(t *testing.T, sqe *ioUringSqe, bufIndex int, addr uint64, n int) {
	t.Helper()

	if sqe.opcode != IORING_OP_SEND {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND)
	}
	if sqe.ioprio != IORING_RECVSEND_FIXED_BUF {
		t.Fatalf("ioprio = %d, want %d", sqe.ioprio, IORING_RECVSEND_FIXED_BUF)
	}
	if sqe.bufIndex != uint16(bufIndex) {
		t.Fatalf("bufIndex = %d, want %d", sqe.bufIndex, bufIndex)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.addr != addr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, addr)
	}
	if sqe.len != uint32(n) {
		t.Fatalf("len = %d, want %d", sqe.len, n)
	}
}

func assertZeroCopyFixedSendSQE(t *testing.T, sqe *ioUringSqe, bufIndex int, addr uint64, n int) {
	t.Helper()

	if sqe.opcode != IORING_OP_SEND_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND_ZC)
	}
	if sqe.ioprio != IORING_RECVSEND_FIXED_BUF {
		t.Fatalf("ioprio = %d, want %d", sqe.ioprio, IORING_RECVSEND_FIXED_BUF)
	}
	if sqe.bufIndex != uint16(bufIndex) {
		t.Fatalf("bufIndex = %d, want %d", sqe.bufIndex, bufIndex)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.addr != addr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, addr)
	}
	if sqe.len != uint32(n) {
		t.Fatalf("len = %d, want %d", sqe.len, n)
	}
	if sqe.uflags != 0 {
		t.Fatalf("uflags = %#x, want 0", sqe.uflags)
	}
}

func TestMulticastUsesOffsetWindowForUserBuffers(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	payload := []byte("0123456789abcdef")
	const (
		offset = int64(5)
		length = 7
	)

	if err := ring.Multicast(PackDirect(0, 0, 0, 0), testSendTargets{fds: []iofd.FD{11}}, -1, payload, offset, length); err != nil {
		t.Fatalf("Multicast: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload[int(offset):]))))
	if sqe.opcode != IORING_OP_SEND {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.len != uint32(length) {
		t.Fatalf("len = %d, want %d", sqe.len, length)
	}
}

func TestMulticastAllowsZeroLengthTailForUserBuffers(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	payload := []byte("0123456789abcdef")

	if err := ring.Multicast(PackDirect(0, 0, 0, 0), testSendTargets{fds: []iofd.FD{11}}, -1, payload, int64(len(payload)), 0); err != nil {
		t.Fatalf("Multicast: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_SEND {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.len != 0 {
		t.Fatalf("len = %d, want 0", sqe.len)
	}
}

func TestMulticastCopyPathsUseFixedSendEncoding(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	if ring.RegisteredBufferCount() < 1 {
		t.Skip("need a registered buffer")
	}

	const (
		bufIndex = 0
		offset   = int64(32)
		length   = 128
	)
	buf := ring.RegisteredBuffer(bufIndex)
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(offset)
	targets := testSendTargets{fds: []iofd.FD{12}}

	t.Run("Multicast", func(t *testing.T) {
		if err := ring.Multicast(PackDirect(0, 0, 0, 0), targets, bufIndex, nil, offset, length); err != nil {
			t.Fatalf("Multicast: %v", err)
		}
		assertFixedSendSQE(t, lastSubmittedSQE(t, ring), bufIndex, wantAddr, length)
	})

	t.Run("MulticastZeroCopy fallback", func(t *testing.T) {
		if err := ring.MulticastZeroCopy(PackDirect(0, 0, 0, 0), targets, bufIndex, offset, length); err != nil {
			t.Fatalf("MulticastZeroCopy: %v", err)
		}
		assertFixedSendSQE(t, lastSubmittedSQE(t, ring), bufIndex, wantAddr, length)
	})
}

func TestMulticastUsesZeroCopyForLargeRegisteredPayloads(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	if ring.RegisteredBufferCount() < 1 {
		t.Skip("need a registered buffer")
	}

	const (
		bufIndex = 0
		offset   = int64(64)
		length   = 4096
	)
	buf := ring.RegisteredBuffer(bufIndex)
	if len(buf) < int(offset)+length {
		t.Skipf("registered buffer too small: len=%d want>=%d", len(buf), int(offset)+length)
	}
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(offset)
	targets := testSendTargets{fds: []iofd.FD{21, 22, 23, 24, 25, 26, 27, 28}}

	if err := ring.Multicast(PackDirect(0, 0, 0, 0), targets, bufIndex, nil, offset, length, WithFlags(IOSQE_IO_LINK)); err != nil {
		t.Fatalf("Multicast: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.flags != IOSQE_IO_LINK|ring.writeLikeOpFlags {
		t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_LINK|ring.writeLikeOpFlags)
	}
	if sqe.fd != int32(targets.fds[len(targets.fds)-1]) {
		t.Fatalf("fd = %d, want %d", sqe.fd, targets.fds[len(targets.fds)-1])
	}
	assertZeroCopyFixedSendSQE(t, sqe, bufIndex, wantAddr, length)
}

func TestMulticastZeroCopyUsesZeroCopyAboveThreshold(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	if ring.RegisteredBufferCount() < 1 {
		t.Skip("need a registered buffer")
	}

	const (
		bufIndex = 0
		offset   = int64(32)
		length   = 1024
	)
	buf := ring.RegisteredBuffer(bufIndex)
	if len(buf) < int(offset)+length {
		t.Skipf("registered buffer too small: len=%d want>=%d", len(buf), int(offset)+length)
	}
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))) + uint64(offset)
	targets := testSendTargets{fds: []iofd.FD{31, 32, 33, 34}}

	if err := ring.MulticastZeroCopy(PackDirect(0, 0, 0, 0), targets, bufIndex, offset, length, WithFlags(IOSQE_IO_DRAIN)); err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.flags != IOSQE_IO_DRAIN|ring.writeLikeOpFlags {
		t.Fatalf("flags = %d, want %d", sqe.flags, IOSQE_IO_DRAIN|ring.writeLikeOpFlags)
	}
	if sqe.fd != int32(targets.fds[len(targets.fds)-1]) {
		t.Fatalf("fd = %d, want %d", sqe.fd, targets.fds[len(targets.fds)-1])
	}
	assertZeroCopyFixedSendSQE(t, sqe, bufIndex, wantAddr, length)
}
