// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

type dummyPollFd int

func (d dummyPollFd) Fd() int { return int(d) }

func TestDarwinBundleCountAvoidsIntegerOverflow(t *testing.T) {
	cqe := uring.CQEView{Res: 2}
	if got := cqe.BundleCount(int(^uint(0) >> 1)); got != 1 {
		t.Fatalf("BundleCount = %d, want 1", got)
	}
}

func TestDarwinReadWriteRejectEmptyBuffer(t *testing.T) {
	var ring uring.Uring
	var ctx uring.SQEContext

	if err := ring.Read(ctx, []byte{}); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Read(empty): got %v, want %v", err, uring.ErrInvalidParam)
	}
	if err := ring.Write(ctx, []byte{}); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Write(empty): got %v, want %v", err, uring.ErrInvalidParam)
	}
}

func TestDarwinReadWriteOffsetDomain(t *testing.T) {
	var zeroRing uring.Uring
	var ctx uring.SQEContext

	if err := zeroRing.Read(ctx, []byte{1}, uring.WithOffset(-2)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Read(offset -2): got %v, want %v", err, uring.ErrInvalidParam)
	}
	if err := zeroRing.Write(ctx, []byte{1}, uring.WithOffset(-2)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Write(offset -2): got %v, want %v", err, uring.ErrInvalidParam)
	}

	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = 1
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Keep the simulator unstarted so queue saturation is deterministic.
	for range ring.SQAvailable() {
		if err := ring.Nop(ctx); err != nil {
			t.Fatalf("Nop: %v", err)
		}
	}
	if got := ring.SQAvailable(); got != 0 {
		t.Fatalf("SQAvailable = %d, want 0", got)
	}
	if err := ring.Read(ctx, []byte{1}, uring.WithOffset(-1)); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("Read(offset -1): got %v, want %v", err, iox.ErrWouldBlock)
	}
	if err := ring.Write(ctx, []byte{1}, uring.WithOffset(-1)); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("Write(offset -1): got %v, want %v", err, iox.ErrWouldBlock)
	}
}

func TestDarwinSocketRejectNegativeOffset(t *testing.T) {
	var ring uring.Uring
	var ctx uring.SQEContext
	fd := dummyPollFd(0)

	if err := ring.Send(ctx, fd, []byte{1}, uring.WithOffset(-1)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Send(negative offset): got %v, want %v", err, uring.ErrInvalidParam)
	}
	if err := ring.Receive(ctx, fd, []byte{1}, uring.WithOffset(-1)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Receive(negative offset): got %v, want %v", err, uring.ErrInvalidParam)
	}
}

func TestDarwinFixedReadWriteOffsetDomain(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.LockedBufferMem = uring.BufferSizeLarge
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()
	if ring.RegisteredBufferCount() == 0 {
		t.Fatal("RegisteredBufferCount = 0, want fixed-buffer coverage")
	}

	var ctx uring.SQEContext
	if _, err := ring.ReadFixed(ctx, 0, uring.WithOffset(-1)); err != nil {
		t.Fatalf("ReadFixed(offset -1): %v", err)
	}
	if err := ring.WriteFixed(ctx, 0, 1, uring.WithOffset(-1)); err != nil {
		t.Fatalf("WriteFixed(offset -1): %v", err)
	}
	if _, err := ring.ReadFixed(ctx, 0, uring.WithOffset(-2)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("ReadFixed(offset -2): got %v, want %v", err, uring.ErrInvalidParam)
	}
	if err := ring.WriteFixed(ctx, 0, 1, uring.WithOffset(-2)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("WriteFixed(offset -2): got %v, want %v", err, uring.ErrInvalidParam)
	}
}

func TestDarwinReceiveRejectsNilAndEmptyBuffer(t *testing.T) {
	var ring uring.Uring
	var ctx uring.SQEContext
	fd := dummyPollFd(0)

	if err := ring.Receive(ctx, fd, nil); !errors.Is(err, uring.ErrNotSupported) {
		t.Fatalf("Receive(nil): got %v, want %v", err, uring.ErrNotSupported)
	}
	if err := ring.Receive(ctx, fd, []byte{}); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Receive(empty): got %v, want %v", err, uring.ErrInvalidParam)
	}
	if err := ring.SubmitReceiveMultishot(ctx, fd, nil); !errors.Is(err, uring.ErrNotSupported) {
		t.Fatalf("SubmitReceiveMultishot(nil): got %v, want %v", err, uring.ErrNotSupported)
	}
	if err := ring.SubmitReceiveMultishot(ctx, fd, []byte{}); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("SubmitReceiveMultishot(empty): got %v, want %v", err, uring.ErrInvalidParam)
	}
	if err := ring.SubmitReceiveMultishot(ctx, fd, make([]byte, 1)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("SubmitReceiveMultishot(caller buffer): got %v, want %v", err, uring.ErrInvalidParam)
	}
}

func TestDarwinFallocateRejectsZeroLength(t *testing.T) {
	var ring uring.Uring
	var ctx uring.SQEContext

	if err := ring.Fallocate(ctx, 0, 0, 0); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Fallocate(zero length): got %v, want %v", err, uring.ErrInvalidParam)
	}
}

func TestDarwinCloseRejectsNegativeFixedFileIndex(t *testing.T) {
	var ring uring.Uring

	if err := ring.Close(uring.ForFD(-1), uring.WithFlags(uring.IOSQE_FIXED_FILE)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Close(negative fixed file): got %v, want %v", err, uring.ErrInvalidParam)
	}
}
