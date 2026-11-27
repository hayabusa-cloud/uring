// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// Two receives share a socket. Shorten the second deadline, then send one byte
// to the first receive. The pair ID and opcode identify each completion.
func TestOverlappingReceiveDeadlines(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	fd, peer := tcpPair(t)
	buf := ring.RegisteredBuffer(0)[:2]
	var receives, timers [2]uring.SQEContext
	for i := range receives {
		id := uint16(i + 1)
		receives[i] = uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_IO_LINK, id, int32(fd))
		timers[i] = uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, id, 0)
		if n, err := queueReceiveWithTimeout(ring, id, fd, buf[i:i+1], time.Minute); err != nil || n != 2 {
			t.Fatalf("queue pair %d: admitted %d, err %v", id, n, err)
		}
	}
	// Submit both pairs so the update can address the second live timer.
	if _, err := ring.Wait(nil); err != nil {
		t.Fatal(err)
	}
	update := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 3, 0)
	if err := ring.TimeoutUpdate(update, timers[1].Raw(), time.Millisecond, false, uring.WithTimeoutFlags(uring.IORING_LINK_TIMEOUT_UPDATE)); err != nil {
		t.Fatal(err)
	}
	checkDeadlineCompletions(t, ring, map[uring.SQEContext]int32{
		update:      0,
		timers[1]:   -int32(zcall.ETIME),
		receives[1]: -int32(zcall.ECANCELED),
	})

	// The first receive is still pending. Completing it disarms its timer.
	if n, err := peer.Write([]byte("A")); err != nil || n != 1 {
		t.Fatalf("peer write: wrote %d, err %v", n, err)
	}
	checkDeadlineCompletions(t, ring, map[uring.SQEContext]int32{
		receives[0]: 1,
		timers[0]:   -int32(zcall.ECANCELED),
	})
	if buf[0] != 'A' {
		t.Fatalf("first receive got %#x, want 'A'", buf[0])
	}
}

func queueReceiveWithTimeout(ring *uring.Uring, pairID uint16, fd iofd.FD, buf []byte, d time.Duration) (int, error) {
	if ring.SQAvailable() < 2 {
		return 0, iox.ErrWouldBlock
	}
	recvCtx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_IO_LINK, pairID, int32(fd))
	if err := ring.ReceiveFD(recvCtx, fd, buf); err != nil {
		return 0, err
	}
	timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, pairID, 0)
	if err := ring.LinkTimeout(timeoutCtx, d); err != nil {
		return 1, err
	}
	return 2, nil
}

func checkDeadlineCompletions(t *testing.T, ring *uring.Uring, want map[uring.SQEContext]int32) {
	t.Helper()
	var cqes [3]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(exampleTimeout)
	for len(want) > 0 {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatal(err)
		}
		if n == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for completions: %v", want)
			}
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			ctx := cqe.Context()
			res, ok := want[ctx]
			if !ok {
				t.Fatalf("unexpected completion %#x: result %d", ctx.Raw(), cqe.Res)
			}
			if cqe.Res != res || cqe.HasMore() {
				t.Fatalf("completion %#x: result %d, flags %#x, want terminal result %d", ctx.Raw(), cqe.Res, cqe.Flags, res)
			}
			delete(want, ctx)
		}
	}
}
