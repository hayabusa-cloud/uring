// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"code.hybscloud.com/uring"
)

func TestRegisterFilesUpdateRejectionPreservesRegistration(t *testing.T) {
	ring := newTestRing(t)
	if n, err := ring.RegisterFilesUpdate(0, []int32{-1}); n != 0 || !errors.Is(err, uring.ErrNotFound) {
		t.Fatalf("update without registration: n=%d error=%v", n, err)
	}
	f, err := os.CreateTemp(t.TempDir(), "registered-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const payload = "registered file remains usable"
	if _, err := f.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	fd := int32(f.Fd())
	if err := ring.RegisterFiles([]int32{fd}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		offset uint32
		fds    []int32
	}{
		{"empty", 0, nil},
		{"at end", 1, []int32{-1}},
		{"past end", 2, []int32{-1}},
		{"overflow", ^uint32(0), []int32{-1}},
		{"excess count", 0, []int32{-1, -1}},
		{"skip sentinel", 0, []int32{-2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n, err := ring.RegisterFilesUpdate(tc.offset, tc.fds); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
				t.Fatalf("update: n=%d error=%v", n, err)
			}
			if got := ring.RegisteredFileCount(); got != 1 {
				t.Fatalf("registered file count=%d, want 1", got)
			}
		})
	}
	buf := make([]byte, len(payload))
	ctx := uring.PackDirect(uring.IORING_OP_READ, uring.IOSQE_FIXED_FILE, 0, 0)
	if err := ring.Read(ctx, buf); err != nil {
		t.Fatal(err)
	}
	cqe, ok := waitForOp(t, ring, uring.IORING_OP_READ, 2*time.Second)
	if !ok || cqe.Res != int32(len(payload)) || string(buf) != payload {
		t.Fatalf("registered read: present=%v result=%d data=%q", ok, cqe.Res, buf)
	}
}

func TestCloneBuffersRejectionPreservesRegistration(t *testing.T) {
	src := newTestRing(t, testTwoRegisteredBuffersOptions)
	empty, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := empty.Stop(); err != nil {
			t.Error(err)
		}
	})
	dst := newTestRing(t, testTwoRegisteredBuffersOptions, func(o *uring.Options) { o.NotifySucceed = true })
	before := [2][]byte{dst.RegisteredBuffer(0), dst.RegisteredBuffer(1)}
	for _, tc := range []struct {
		name                  string
		src                   *uring.Uring
		srcOff, dstOff, count uint32
		want                  error
	}{
		{"nil source", nil, 0, 0, 0, uring.ErrInvalidParam},
		{"uninitialized source", &uring.Uring{}, 0, 0, 0, uring.ErrInvalidParam},
		{"unregistered source", empty, 0, 0, 0, uring.ErrNotFound},
		{"whole table source offset", src, 1, 0, 0, uring.ErrInvalidParam},
		{"whole table destination offset", src, 0, 1, 0, uring.ErrInvalidParam},
		{"source range", src, 1, 0, 2, uring.ErrInvalidParam},
		{"source range overflow", src, ^uint32(0), 0, 2, uring.ErrInvalidParam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := dst.CloneBuffers(tc.src, tc.srcOff, tc.dstOff, tc.count, true); !errors.Is(err, tc.want) {
				t.Fatalf("CloneBuffers: %v, want %v", err, tc.want)
			}
			if got := dst.RegisteredBufferCount(); got != len(before) {
				t.Fatalf("destination buffer count=%d, want %d", got, len(before))
			}
			for i, want := range before {
				if got := dst.RegisteredBuffer(i); len(got) != len(want) || &got[0] != &want[0] {
					t.Fatalf("destination buffer %d changed after rejection", i)
				}
			}
		})
	}
	if err := dst.CloneBuffers(src, 1, 0, 1, true); err != nil {
		t.Fatal(err)
	}
	if got := dst.RegisteredBufferCount(); got != len(before) {
		t.Fatalf("replacement buffer count=%d, want %d", got, len(before))
	}
	views := [2][]byte{src.RegisteredBuffer(1), before[1]}
	for i, want := range views {
		if got := dst.RegisteredBuffer(i); len(got) != len(want) || &got[0] != &want[0] {
			t.Fatalf("destination buffer %d does not match the replacement range", i)
		}
	}
	f, err := os.CreateTemp(t.TempDir(), "cloned-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, int32(f.Fd()))
	for i, payload := range [...]string{"cloned buffer remains usable", "original buffer remains usable"} {
		copy(views[i], payload)
		offset := int64(i * 64)
		if err := dst.WriteFixed(ctx, i, len(payload), uring.WithOffset(offset)); err != nil {
			t.Fatal(err)
		}
		cqe, ok := waitForOp(t, dst, uring.IORING_OP_WRITE_FIXED, 2*time.Second)
		if !ok || cqe.Res != int32(len(payload)) {
			t.Fatalf("buffer %d write: present=%v result=%d", i, ok, cqe.Res)
		}
		got := make([]byte, len(payload))
		if _, err := f.ReadAt(got, offset); err != nil || string(got) != payload {
			t.Fatalf("buffer %d data=%q error=%v", i, got, err)
		}
	}
}
