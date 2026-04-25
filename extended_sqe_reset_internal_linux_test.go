// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"testing"

	"code.hybscloud.com/iofd"
)

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
	ring := newWrapperTestRing(t)
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
	ring := newWrapperTestRing(t)
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
