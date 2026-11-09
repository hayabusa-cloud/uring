// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/zcall"
)

// Inspect the actual mapped ABI on a real ring. Invalid-FD command completion
// permits checking all 80 command bytes without requiring a particular device.
func TestResizeRingsPendingStorage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ring, err := New(testMinimalBufferOptions, func(o *Options) {
		o.MultiIssuers = false
		o.NotifySucceed = true
		o.SQE128 = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Start(); err != nil {
		t.Fatal(err)
	}
	defer ring.Stop()
	ind, ext := ring.IndirectSQE(), ring.ExtSQE()
	defer ring.PutIndirectSQE(ind)
	defer ring.PutExtSQE(ext)
	ext.UserData[0] = 0xa5
	timer := PackIndirect(ind)
	update := PackExtended(ext)
	command := PackDirect(IORING_OP_URING_CMD, 0, 0, -1)
	if err := ring.Timeout(timer, time.Hour, WithCount(0)); err != nil {
		t.Fatal(err)
	}
	if err := ring.TimeoutUpdate(update, timer.Raw(), time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	var payload [80]byte
	for i := range payload {
		payload[i] = byte(i + 1)
	}
	if err := ring.UringCmdInline(command, 17, payload[:]); err != nil {
		t.Fatal(err)
	}
	wantCommand := *ring.sq.sqe128At(2)
	for _, size := range []uint32{64, 32, 128} {
		if err := ring.ResizeRings(size, size*2); err != nil {
			t.Fatal(err)
		}
		if got := *ring.sq.sqe128At(2); got != wantCommand {
			t.Errorf("resize to %d changed the 128-byte command SQE", size)
		}
		timerAddr := uint64(uintptr(unsafe.Pointer(&ring.submit.keepAlive[0].timespec)))
		updateAddr := uint64(uintptr(unsafe.Pointer(&ring.submit.keepAlive[1].timespec)))
		if ring.sq.sqeAt(0).addr != timerAddr || ind.addr != timerAddr {
			t.Errorf("resize to %d: timeout SQE or Indirect snapshot lost staged timespec", size)
		}
		if ring.sq.sqeAt(1).off != updateAddr || ext.SQE.off != updateAddr {
			t.Errorf("resize to %d: update SQE or Extended snapshot lost staged timespec", size)
		}
		if ext.UserData[0] != 0xa5 {
			t.Error("resize changed Extended user data")
		}
		runtime.GC()
	}
	want := map[uint64]int32{
		timer.Raw():   -int32(zcall.ETIME),
		update.Raw():  0,
		command.Raw(): -int32(zcall.EBADF),
	}
	var cqes [3]CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for remaining := 3; remaining > 0 && time.Now().Before(deadline); {
		n, err := ring.Wait(cqes[:])
		if err != nil && err != iox.ErrWouldBlock {
			t.Fatal(err)
		}
		if n == 0 {
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			remaining--
			id := cqe.Context().Raw()
			res, ok := want[id]
			if !ok || cqe.Res != res {
				t.Errorf("completion %#x: result %d, expected %v", id, cqe.Res, want)
			}
			delete(want, id)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing completions: %v", want)
	}
}
