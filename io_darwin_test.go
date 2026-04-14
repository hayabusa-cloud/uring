// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/uring"
)

type dummyPollFd int

func (d dummyPollFd) Fd() int { return int(d) }

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
}

func TestDarwinCloseRejectsNegativeFixedFileIndex(t *testing.T) {
	var ring uring.Uring

	if err := ring.Close(uring.ForFD(-1), uring.WithFlags(uring.IOSQE_FIXED_FILE)); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Close(negative fixed file): got %v, want %v", err, uring.ErrInvalidParam)
	}
}
