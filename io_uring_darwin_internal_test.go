// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"os"
	"syscall"
	"testing"
)

func TestDarwinDoCloseUsesRegisteredFileSlot(t *testing.T) {
	slotFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(slotFile): %v", err)
	}
	sqeFile, err := os.Open(os.DevNull)
	if err != nil {
		_ = slotFile.Close()
		t.Fatalf("Open(sqeFile): %v", err)
	}

	ur := &ioUring{
		files: []int32{int32(slotFile.Fd())},
	}
	sqe := &ioUringSqe{
		fd:         int32(sqeFile.Fd()),
		spliceFdIn: 1,
	}

	if got := ur.doClose(sqe); got != 0 {
		_ = slotFile.Close()
		_ = sqeFile.Close()
		t.Fatalf("doClose() = %d, want 0", got)
	}
	if got := ur.files[0]; got != -1 {
		_ = slotFile.Close()
		_ = sqeFile.Close()
		t.Fatalf("files[0] = %d, want -1", got)
	}
	if err := sqeFile.Close(); err != nil {
		_ = slotFile.Close()
		t.Fatalf("sqeFile should stay open: %v", err)
	}
	if err := slotFile.Close(); err == nil {
		t.Fatal("slotFile.Close() succeeded after direct close, want closed descriptor")
	}
}

func TestDarwinDoCloseRejectsEmptyRegisteredFileSlot(t *testing.T) {
	sqeFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(sqeFile): %v", err)
	}

	ur := &ioUring{
		files: []int32{-1},
	}
	sqe := &ioUringSqe{
		fd:         int32(sqeFile.Fd()),
		spliceFdIn: 1,
	}

	if got := ur.doClose(sqe); got != -int32(syscall.EBADF) {
		_ = sqeFile.Close()
		t.Fatalf("doClose() = %d, want %d", got, -int32(syscall.EBADF))
	}
	if err := sqeFile.Close(); err != nil {
		t.Fatalf("sqeFile should stay open on EBADF: %v", err)
	}
}

func TestDarwinSQEView(t *testing.T) {
	t.Run("ViewSQE from IndirectSQE", func(t *testing.T) {
		indirect := &IndirectSQE{}
		indirect.opcode = IORING_OP_RECV
		indirect.flags = IOSQE_BUFFER_SELECT | IOSQE_ASYNC
		indirect.ioprio = 42
		indirect.fd = 100
		indirect.off = 0x1234567890
		indirect.addr = 0xDEADBEEF
		indirect.len = 4096
		indirect.uflags = 0x8000
		indirect.bufIndex = 7
		indirect.personality = 3
		indirect.spliceFdIn = 50
		indirect.userData = PackDirect(IORING_OP_RECV, indirect.flags, indirect.bufIndex, indirect.fd).Raw()

		view := ViewSQE(indirect)

		if !view.Valid() {
			t.Error("expected Valid() to be true")
		}
		if view.Opcode() != IORING_OP_RECV {
			t.Errorf("Opcode() = %d, want %d", view.Opcode(), IORING_OP_RECV)
		}
		if view.Flags() != IOSQE_BUFFER_SELECT|IOSQE_ASYNC {
			t.Errorf("Flags() = %d, want %d", view.Flags(), IOSQE_BUFFER_SELECT|IOSQE_ASYNC)
		}
		if view.IoPrio() != 42 {
			t.Errorf("IoPrio() = %d, want 42", view.IoPrio())
		}
		if view.RawFD() != 100 {
			t.Errorf("RawFD() = %d, want 100", view.RawFD())
		}
		if view.Off() != 0x1234567890 {
			t.Errorf("Off() = %x, want 0x1234567890", view.Off())
		}
		if view.Addr() != 0xDEADBEEF {
			t.Errorf("Addr() = %x, want 0xDEADBEEF", view.Addr())
		}
		if view.Len() != 4096 {
			t.Errorf("Len() = %d, want 4096", view.Len())
		}
		if view.UFlags() != 0x8000 {
			t.Errorf("UFlags() = %x, want 0x8000", view.UFlags())
		}
		if view.UserData() != indirect.userData {
			t.Errorf("UserData() = %x, want %x", view.UserData(), indirect.userData)
		}
		if view.BufIndex() != 7 {
			t.Errorf("BufIndex() = %d, want 7", view.BufIndex())
		}
		if view.BufGroup() != 7 {
			t.Errorf("BufGroup() = %d, want 7", view.BufGroup())
		}
		if view.Personality() != 3 {
			t.Errorf("Personality() = %d, want 3", view.Personality())
		}
		if view.SpliceFDIn() != 50 {
			t.Errorf("SpliceFDIn() = %d, want 50", view.SpliceFDIn())
		}
		if view.FileIndex() != 50 {
			t.Errorf("FileIndex() = %d, want 50", view.FileIndex())
		}
		if !view.HasBufferSelect() {
			t.Error("expected HasBufferSelect() true")
		}
		if !view.HasAsync() {
			t.Error("expected HasAsync() true")
		}
	})

	t.Run("ViewExtSQE from ExtSQE", func(t *testing.T) {
		ext := &ExtSQE{}
		ext.SQE.opcode = IORING_OP_SEND
		ext.SQE.flags = IOSQE_CQE_SKIP_SUCCESS
		ext.SQE.fd = 200

		view := ViewExtSQE(ext)

		if !view.Valid() {
			t.Error("expected Valid() to be true")
		}
		if view.Opcode() != IORING_OP_SEND {
			t.Errorf("Opcode() = %d, want %d", view.Opcode(), IORING_OP_SEND)
		}
		if view.RawFD() != 200 {
			t.Errorf("RawFD() = %d, want 200", view.RawFD())
		}
		if !view.HasCQESkipSuccess() {
			t.Error("expected HasCQESkipSuccess() to be true")
		}
	})
}

func TestDarwinNewIoUringRejectsEntriesOutsideSetupDomain(t *testing.T) {
	for _, entries := range []int{-1, 0, EntriesHuge + 1} {
		ur, err := newIoUring(entries)
		if ur != nil {
			t.Fatalf("newIoUring(%d) = non-nil ring, want nil", entries)
		}
		if err != ErrInvalidParam {
			t.Fatalf("newIoUring(%d) error = %v, want %v", entries, err, ErrInvalidParam)
		}
	}
}

func TestDarwinNewUringProvideBuffersRoundsAndPartitionsCount(t *testing.T) {
	buffers := newUringProvideBuffers(3, 3)
	if buffers.size != 4 {
		t.Fatalf("size = %d, want 4", buffers.size)
	}
	if buffers.n != 4 {
		t.Fatalf("n = %d, want 4", buffers.n)
	}
	if buffers.gn != 1 || buffers.gMask != 0 {
		t.Fatalf("group geometry = (%d, %d), want (1, 0)", buffers.gn, buffers.gMask)
	}

	buffers = newUringProvideBuffers(3, providedBufferGroupEntries+1)
	if buffers.n != providedBufferGroupEntries {
		t.Fatalf("split n = %d, want %d", buffers.n, providedBufferGroupEntries)
	}
	if buffers.gn != 2 || buffers.gMask != 1 {
		t.Fatalf("split group geometry = (%d, %d), want (2, 1)", buffers.gn, buffers.gMask)
	}
	if err := buffers.register(nil, 0); err != nil {
		t.Fatalf("register split buffers: %v", err)
	}
	if got, want := len(buffers.mem), buffers.size*buffers.n*buffers.gn; got != want {
		t.Fatalf("split backing length = %d, want %d", got, want)
	}
	if got := buffers.buf(bufferGroupIndex(1), 0); len(got) != buffers.size {
		t.Fatalf("second group buffer length = %d, want %d", len(got), buffers.size)
	}
}
