// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func TestAsyncCancelAnyMultipleRequests(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	var fds [2]iofd.FD
	for i := range fds {
		fd, errno := zcall.Eventfd2(0, zcall.EFD_NONBLOCK|zcall.EFD_CLOEXEC)
		if errno != 0 {
			t.Fatal(zcall.Errno(errno))
		}
		defer zcall.Close(fd)
		fds[i] = iofd.FD(fd)
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(fd))
		if err := ring.PollAdd(ctx, uring.EPOLLIN); err != nil {
			t.Fatal(err)
		}
	}
	var cqes [8]uring.CQEView
	if n, err := ring.Wait(cqes[:]); n != 0 || !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("unready polls: n=%d err=%v", n, err)
	}
	if err := ring.AsyncCancelAny(uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 0, 0)); err != nil {
		t.Fatal(err)
	}

	var canceled [2]bool
	cancelSeen := false
	backoff := iox.Backoff{}
	deadline := time.Now().Add(2 * time.Second)
	for !(cancelSeen && canceled[0] && canceled[1]) && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatal(err)
		}
		if n == 0 {
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			switch cqe.Op() {
			case uring.IORING_OP_ASYNC_CANCEL:
				if cancelSeen || cqe.Res != 2 {
					t.Fatalf("cancellation: duplicate=%v result=%d, want 2", cancelSeen, cqe.Res)
				}
				cancelSeen = true
			case uring.IORING_OP_POLL_ADD:
				index := -1
				for i, fd := range fds {
					if cqe.FD() == fd {
						index = i
					}
				}
				if index < 0 || canceled[index] || cqe.Res != -int32(zcall.ECANCELED) {
					t.Fatalf("unexpected poll completion: %+v", cqe)
				}
				canceled[index] = true
			default:
				t.Fatalf("unexpected completion: %+v", cqe)
			}
		}
	}
	if !cancelSeen || !canceled[0] || !canceled[1] {
		t.Fatalf("missing completions: cancellation=%v polls=%v", cancelSeen, canceled)
	}
}

func TestFutexWaitValueMismatch(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	value := uint32(1)
	var pin runtime.Pinner
	pin.Pin(&value)
	defer pin.Unpin()
	ctx := uring.PackDirect(uring.IORING_OP_FUTEX_WAIT, 0, 0, 0)
	if err := ring.FutexWait(ctx, &value, 0, uring.FUTEX_BITSET_MATCH_ANY, uring.FUTEX2_SIZE_U32); err != nil {
		t.Fatal(err)
	}
	cqe, ok := waitForOp(t, ring, uring.IORING_OP_FUTEX_WAIT, 2*time.Second)
	if !ok || cqe.Res != -int32(zcall.EAGAIN) {
		t.Fatalf("mismatched value: present=%v result=%d, want -EAGAIN", ok, cqe.Res)
	}
}

func TestUringCmdInlineRetriesAfterSQDrain(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) {
		o.Entries = uring.EntriesPico
		o.SQE128 = true
	})
	capacity := ring.SQAvailable()
	for range capacity {
		if err := ring.Nop(uring.ForFD(0)); err != nil {
			t.Fatal(err)
		}
	}
	var command [80]byte
	ctx := uring.PackDirect(uring.IORING_OP_URING_CMD, 0, 0, -1)
	if err := ring.UringCmdInline(ctx, 0, command[:]); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("full SQ: %v, want ErrWouldBlock", err)
	}
	var cqes [uring.EntriesPico]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(2 * time.Second)
	completed := 0
	for completed < capacity && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatal(err)
		}
		if n == 0 {
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
				t.Fatalf("unexpected completion before retry: %+v", cqe)
			}
			completed++
		}
	}
	if completed != capacity || ring.SQAvailable() != capacity {
		t.Fatalf("drain: completed=%d available=%d, want %d", completed, ring.SQAvailable(), capacity)
	}
	if err := ring.UringCmdInline(ctx, 0, command[:]); err != nil {
		t.Fatalf("retry after drain: %v", err)
	}
	cqe, ok := waitForOp(t, ring, uring.IORING_OP_URING_CMD, 2*time.Second)
	if !ok || cqe.FD() != -1 || cqe.Res != -int32(zcall.EBADF) {
		t.Fatalf("retried command: present=%v cqe=%+v", ok, cqe)
	}
}
