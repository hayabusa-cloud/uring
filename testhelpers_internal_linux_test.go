// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"
	"testing"
)

const (
	testInternalLockedBufferMem         = registerBufferSize
	testInternalTwoRegisteredBuffersMem = 2 * registerBufferSize
	testInternalProvidedBufferNum       = 4
)

func testMinimalBufferOptions(opt *Options) {
	opt.MultiIssuers = true // Allow unpinned test goroutines.
	opt.LockedBufferMem = testInternalLockedBufferMem
	opt.ReadBufferNum = testInternalProvidedBufferNum
	opt.Entries = EntriesNano
}

func testTwoRegisteredBuffersOptions(opt *Options) {
	opt.LockedBufferMem = testInternalTwoRegisteredBuffersMem
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
