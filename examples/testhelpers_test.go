// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

const testLockedBufferMem = 1 << 18

func testMinimalBufferOptions(opt *uring.Options) {
	opt.LockedBufferMem = testLockedBufferMem
}

func mustStartRing(tb testing.TB, ring *uring.Uring) {
	tb.Helper()

	if err := ring.Start(); err != nil {
		tb.Fatalf("Start: %v", err)
	}
	tb.Cleanup(func() {
		if err := ring.Stop(); err != nil {
			tb.Fatalf("Stop: %v", err)
		}
	})
}

func waitForOp(t *testing.T, ring *uring.Uring, op uint8, timeout time.Duration) (uring.CQEView, bool) {
	t.Helper()

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(timeout)
	b := iox.Backoff{}
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Logf("Wait error: %v", err)
			return uring.CQEView{}, false
		}
		for j := 0; j < n; j++ {
			if cqes[j].Op() == op {
				return cqes[j], true
			}
		}
		if n == 0 {
			b.Wait()
		}
	}
	return uring.CQEView{}, false
}

func drainCompletions(ring *uring.Uring, expected int) int {
	cqes := make([]uring.CQEView, 64)
	drained := 0
	deadline := time.Now().Add(time.Second)
	b := iox.Backoff{}
	for drained < expected && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			break
		}
		drained += n
		if n == 0 {
			b.Wait()
		}
	}
	return drained
}

// createPipe creates a pipe and returns read/write file descriptors.
func createPipe() (r, w uintptr, err error) {
	var fds [2]int32
	errno := zcall.Pipe2(&fds, zcall.O_NONBLOCK|zcall.O_CLOEXEC)
	if errno != 0 {
		return 0, 0, zcall.Errno(errno)
	}
	return uintptr(fds[0]), uintptr(fds[1]), nil
}

// closeFd closes a file descriptor.
func closeFd(fd uintptr) {
	zcall.Close(fd)
}

// dispatchSimplifiedMultishotCQE mirrors only the observable callback flow used
// by these examples. It does not model the full MultishotSubscription lifecycle such as
// cancellation serialization, unsubscribe suppression, or ExtSQE retirement.
func dispatchSimplifiedMultishotCQE(handler uring.MultishotHandler, cqe uring.CQEView) bool {
	step := uring.MultishotStep{CQE: cqe}
	if cqe.Res < 0 {
		step.Err = zcall.Errno(uintptr(-cqe.Res))
		step.Cancelled = cqe.Res == -int32(uring.ECANCELED)
	}

	keep := handler.OnMultishotStep(step) == uring.MultishotContinue
	if step.Final() {
		handler.OnMultishotStop(step.Err, step.Cancelled)
	}
	return keep
}
