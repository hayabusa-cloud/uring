// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

const (
	testLockedBufferMem = 1 << 18
	testBufferNum       = 64
)

func testMinimalBufferOptions(opt *uring.Options) {
	opt.LockedBufferMem = testLockedBufferMem
	opt.ReadBufferNum = testBufferNum
	opt.WriteBufferNum = testBufferNum
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

func newTestRing(t *testing.T, opts ...func(*uring.Options)) *uring.Uring {
	t.Helper()

	options := append([]func(*uring.Options){testMinimalBufferOptions}, opts...)
	ring, err := uring.New(options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	return ring
}

func waitForOp(t *testing.T, ring *uring.Uring, op uint8, timeout time.Duration) (uring.CQEView, bool) {
	t.Helper()

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(timeout)
	b := iox.Backoff{}
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil &&
			!errors.Is(err, iox.ErrWouldBlock) &&
			!errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			b.Reset()
		}
		for j := 0; j < n; j++ {
			if cqes[j].Op() == op {
				return cqes[j], true
			}
		}
		b.Wait()
	}
	return uring.CQEView{}, false
}

func isWSL2() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func mustCloseAndWait(t *testing.T, ring *uring.Uring, ctx uring.SQEContext, timeout time.Duration, label string) {
	t.Helper()

	if err := ring.Close(ctx); err != nil {
		t.Fatalf("Close %s: %v", label, err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_CLOSE, timeout)
	if !ok {
		t.Fatalf("%s close did not complete", label)
	}
	if ev.Res < 0 {
		t.Fatalf("%s close failed: %d", label, ev.Res)
	}
}

func newUnixSocketPairForTest() ([2]iofd.FD, error) {
	var fds [2]int32
	errno := zcall.Socketpair(zcall.AF_UNIX, zcall.SOCK_STREAM|zcall.SOCK_CLOEXEC, 0, &fds)
	if errno != 0 {
		return [2]iofd.FD{}, zcall.Errno(errno)
	}
	return [2]iofd.FD{iofd.FD(fds[0]), iofd.FD(fds[1])}, nil
}

func closeTestFD(fd iofd.FD) {
	if fd >= 0 {
		zcall.Close(uintptr(fd))
	}
}

func closeTestFds(fds [2]iofd.FD) {
	closeTestFD(fds[0])
	closeTestFD(fds[1])
}

func readTestFD(fd iofd.FD, buf []byte) (int, uintptr) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, errno := zcall.Read(uintptr(fd), buf)
	return int(n), errno
}

func writeTestFD(fd iofd.FD, buf []byte) (int, uintptr) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, errno := zcall.Write(uintptr(fd), buf)
	return int(n), errno
}

// dispatchSimplifiedMultishotCQE is a test helper for direct handler exercises.
// It intentionally skips the internal MultishotSubscription state machine and therefore
// must not be read as the authoritative runtime lifecycle.
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
