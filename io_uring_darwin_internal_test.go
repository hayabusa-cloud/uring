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
