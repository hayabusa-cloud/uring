// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"testing"
	"unsafe"
)

// The real mapped SQ exposes reserved fields and submission roots that are not
// observable through a completed message's public result.
func TestSendMsgHeaderSubmitStorage(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	ext := ring.ExtSQE()
	defer ring.PutExtSQE(ext)
	ctx := PackExtended(ext)
	msg := &Msghdr{}
	sqe := ring.sq.sqeAt(0)
	*sqe = ioUringSqe{off: 9, uflags: 7, bufIndex: 5, personality: 3, spliceFdIn: 1}
	err := ring.SendMsgHeader(ctx, -1, msg, func(o *OpOption) {
		// SQAvailable also takes the submit lock: option evaluation must precede reservation.
		if ring.SQAvailable() == 0 {
			t.Fatal("fresh ring has no SQ capacity")
		}
		o.Flags = IOSQE_ASYNC
		o.IOPrio = IORING_RECVSEND_POLL_FIRST
		o.Offset = -1
		o.Personality = 65535
	})
	if err != nil {
		t.Fatal(err)
	}
	want := ioUringSqe{opcode: IORING_OP_SENDMSG, flags: IOSQE_ASYNC,
		ioprio: IORING_RECVSEND_POLL_FIRST, fd: -1, len: 1,
		addr: uint64(uintptr(unsafe.Pointer(msg))), userData: ctx.Raw()}
	if *sqe != want || ext.SQE != want {
		t.Fatalf("SENDMSG encoding or context snapshot differs: %+v / %+v", *sqe, ext.SQE)
	}
	keep := &ring.submit.keepAlive[0]
	if !keep.active || keep.message != msg || keep.ext != nil {
		t.Fatal("caller message did not use the fixed submit root")
	}
	// A drain proves the send has finished before inspecting root cleanup.
	if err := ring.Nop(PackDirect(IORING_OP_NOP, IOSQE_IO_DRAIN, 0, 0)); err != nil {
		t.Fatal(err)
	}
	waitForNopCQE(t, ring)
	slot, err := ring.reserveSubmitSlot()
	if err != nil {
		t.Fatal(err)
	}
	if keep.active || keep.message != nil {
		ring.abortSubmitSlot(slot)
		t.Fatal("consumed slot retained the caller message")
	}
	slot.keep.active, slot.keep.message = true, msg
	ring.abortSubmitSlot(slot)
	if slot.keep.active || slot.keep.message != nil {
		t.Fatal("aborted reservation retained the caller message")
	}
}
