// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"runtime"
	"testing"

	"code.hybscloud.com/iofd"
)

func TestCQEViewBufGroupForIndirectAndExtendedModes(t *testing.T) {
	var targets runtime.Pinner
	t.Cleanup(targets.Unpin)

	t.Run("indirect with buffer select", func(t *testing.T) {
		sqe := &IndirectSQE{}
		targets.Pin(sqe)
		sqe.flags = IOSQE_BUFFER_SELECT
		sqe.bufIndex = 23

		view := CQEView{ctx: PackIndirect(sqe)}
		if got := view.BufGroup(); got != 23 {
			t.Fatalf("BufGroup() = %d, want 23", got)
		}
	})

	t.Run("extended with buffer select", func(t *testing.T) {
		ext := &ExtSQE{}
		targets.Pin(ext)
		ext.SQE.flags = IOSQE_BUFFER_SELECT
		ext.SQE.bufIndex = 41

		view := CQEView{ctx: PackExtended(ext)}
		if got := view.BufGroup(); got != 41 {
			t.Fatalf("BufGroup() = %d, want 41", got)
		}
	})

	t.Run("non direct without buffer select", func(t *testing.T) {
		sqe := &IndirectSQE{}
		targets.Pin(sqe)
		view := CQEView{ctx: PackIndirect(sqe)}
		if got := view.BufGroup(); got != 0 {
			t.Fatalf("BufGroup() = %d, want 0", got)
		}
	})
}

func TestCQEViewAccessorsAcrossModes(t *testing.T) {
	var targets runtime.Pinner
	t.Cleanup(targets.Unpin)

	t.Run("direct", func(t *testing.T) {
		const fd = -1 << 29 // Lowest signed 30-bit descriptor.
		view := CQEView{ctx: PackDirect(IORING_OP_SEND, IOSQE_ASYNC, 0, fd)}

		if got := view.Op(); got != IORING_OP_SEND {
			t.Fatalf("Op() = %d, want %d", got, IORING_OP_SEND)
		}
		if got := view.FD(); got != iofd.FD(fd) {
			t.Fatalf("FD() = %d, want %d", got, fd)
		}
		if view.FullSQE() {
			t.Fatal("FullSQE() = true, want false")
		}
		if view.Extended() {
			t.Fatal("Extended() = true, want false")
		}
		if got := view.SQE(); got.Valid() {
			t.Fatal("SQE().Valid() = true, want false")
		}
	})

	t.Run("indirect", func(t *testing.T) {
		const fd = 1<<31 - 1 // Pointer-backed descriptors retain all 32 bits.
		sqe := &IndirectSQE{}
		targets.Pin(sqe)
		sqe.opcode = IORING_OP_RECV
		sqe.fd = fd

		view := CQEView{ctx: PackIndirect(sqe)}
		if got := view.Op(); got != IORING_OP_RECV {
			t.Fatalf("Op() = %d, want %d", got, IORING_OP_RECV)
		}
		if got := view.FD(); got != iofd.FD(fd) {
			t.Fatalf("FD() = %d, want %d", got, fd)
		}
		if !view.FullSQE() {
			t.Fatal("FullSQE() = false, want true")
		}
		if view.Extended() {
			t.Fatal("Extended() = true, want false")
		}
		sqeView := view.SQE()
		if !sqeView.Valid() {
			t.Fatal("SQE().Valid() = false, want true")
		}
		if got := sqeView.Opcode(); got != IORING_OP_RECV {
			t.Fatalf("SQE().Opcode() = %d, want %d", got, IORING_OP_RECV)
		}
		if got := sqeView.FD(); got != iofd.FD(fd) {
			t.Fatalf("SQE().FD() = %d, want %d", got, fd)
		}
	})

	t.Run("extended", func(t *testing.T) {
		const fd = -1 << 31
		ext := &ExtSQE{}
		targets.Pin(ext)
		ext.SQE.opcode = IORING_OP_ACCEPT
		ext.SQE.fd = fd

		view := CQEView{ctx: PackExtended(ext)}
		if got := view.Op(); got != IORING_OP_ACCEPT {
			t.Fatalf("Op() = %d, want %d", got, IORING_OP_ACCEPT)
		}
		if got := view.FD(); got != iofd.FD(fd) {
			t.Fatalf("FD() = %d, want %d", got, fd)
		}
		if !view.FullSQE() {
			t.Fatal("FullSQE() = false, want true")
		}
		if !view.Extended() {
			t.Fatal("Extended() = false, want true")
		}
		sqeView := view.SQE()
		if !sqeView.Valid() {
			t.Fatal("SQE().Valid() = false, want true")
		}
		if got := sqeView.Opcode(); got != IORING_OP_ACCEPT {
			t.Fatalf("SQE().Opcode() = %d, want %d", got, IORING_OP_ACCEPT)
		}
		if got := sqeView.FD(); got != iofd.FD(fd) {
			t.Fatalf("SQE().FD() = %d, want %d", got, fd)
		}
	})
}
