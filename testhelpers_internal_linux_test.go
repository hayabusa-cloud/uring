// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"
	"testing"

	"code.hybscloud.com/zcall"
)

const (
	testInternalLockedBufferMem = 1 << 18
	testInternalBufferNum       = 64
)

func testMinimalBufferOptions(opt *Options) {
	opt.LockedBufferMem = testInternalLockedBufferMem
	opt.ReadBufferNum = testInternalBufferNum
	opt.WriteBufferNum = testInternalBufferNum
}

func mustStartRing(tb testing.TB, ring *Uring) {
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

func newWrapperTestRing(t *testing.T) *Uring {
	t.Helper()

	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.Entries = EntriesNano
		opt.MultiIssuers = true
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := ring.ioUring.stop(); err != nil {
			t.Fatalf("stop: %v", err)
		}
	})
	return ring
}

type mockPollFd struct {
	fd int
}

func (m *mockPollFd) Fd() int { return m.fd }

func lastSubmittedSQE(t *testing.T, ring *Uring) *ioUringSqe {
	t.Helper()

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	if tail == 0 {
		t.Fatal("no SQE submitted")
	}
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	return ring.ioUring.sq.sqeAt(idx)
}

func dispatchSimplifiedMultishotCQE(handler MultishotHandler, cqe CQEView) bool {
	step := MultishotStep{CQE: cqe}
	if cqe.Res < 0 {
		step.Err = zcall.Errno(uintptr(-cqe.Res))
		step.Cancelled = cqe.Res == -int32(ECANCELED)
	}
	keep := handler.OnMultishotStep(step) == MultishotContinue
	if step.Final() {
		handler.OnMultishotStop(step.Err, step.Cancelled)
	}
	return keep
}
