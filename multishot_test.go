// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// testMultishotHandler implements MultishotHandler for testing.
type testMultishotHandler struct {
	resultCount  atomic.Int32
	errorCount   atomic.Int32
	stoppedCount atomic.Int32
	lastRes      atomic.Int32
	shouldStop   atomic.Bool
}

func (h *testMultishotHandler) OnMultishotStep(step uring.MultishotStep) uring.MultishotAction {
	if step.Err == nil {
		h.resultCount.Add(1)
		h.lastRes.Store(step.CQE.Res)
		if h.shouldStop.Load() {
			return uring.MultishotStop
		}
		return uring.MultishotContinue
	}
	if !step.Cancelled {
		h.errorCount.Add(1)
	}
	return uring.MultishotStop
}

func (h *testMultishotHandler) OnMultishotStop(error, bool) {
	h.stoppedCount.Add(1)
}

// TestMultishotSubscriptionCreate tests subscription creation via Uring factory methods.
func TestMultishotSubscriptionCreate(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener for test
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	handler := &testMultishotHandler{}
	sqeCtx := uring.ForFD(fd)

	// Factory is on Uring now
	sub, err := ring.AcceptMultishot(sqeCtx, handler)
	if err != nil {
		t.Logf("AcceptMultishot: %v (may be unsupported)", err)
		return
	}
	if sub == nil {
		t.Fatal("AcceptMultishot returned nil subscription")
	}
}

// TestSubmitAcceptMultishotCancelTerminates tests raw multishot accept cancellation CQEs.
func TestSubmitAcceptMultishotCancelTerminates(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, fd)
	if err := ring.SubmitAcceptMultishot(acceptCtx); err != nil {
		// Multishot accept might not be supported on all kernels
		t.Logf("SubmitAcceptMultishot returned error (may be unsupported): %v", err)
		return
	}

	clientConn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("DialTimeout: %v", err)
	}
	defer clientConn.Close()

	acceptDeadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	accepted := 0
	hasMoreSeen := false
	cqes := make([]uring.CQEView, 16)

	for accepted == 0 && time.Now().Before(acceptDeadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() != uring.IORING_OP_ACCEPT {
				continue
			}
			if cqe.Res < 0 {
				t.Fatalf("accept CQE failed: %d", cqe.Res)
			}
			accepted++
			if cqe.HasMore() {
				hasMoreSeen = true
			}
			closeTestFD(iofd.FD(cqe.Res))
		}
		if accepted == 0 {
			b.Wait()
		}
	}

	if accepted > 0 && !hasMoreSeen {
		t.Fatal("accept multishot CQE did not advertise continuation")
	}
	if accepted == 0 {
		t.Skip("SubmitAcceptMultishot completion not received - timing-sensitive")
	}

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, fd)
	if err := ring.AsyncCancel(cancelCtx, acceptCtx.Raw()); err != nil {
		t.Fatalf("AsyncCancel: %v", err)
	}

	terminalAccept := false
	stopDeadline := time.Now().Add(3 * time.Second)
	b.Reset()
	for time.Now().Before(stopDeadline) && !terminalAccept {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			switch cqe.Op() {
			case uring.IORING_OP_ACCEPT:
				if cqe.Res >= 0 {
					accepted++
					if cqe.HasMore() {
						hasMoreSeen = true
					}
					closeTestFD(iofd.FD(cqe.Res))
				} else if cqe.Res != -int32(uring.ECANCELED) {
					t.Fatalf("terminal accept CQE failed: %d", cqe.Res)
				}
				if !cqe.HasMore() {
					terminalAccept = true
				}
			case uring.IORING_OP_ASYNC_CANCEL:
				// AsyncCancel completion is expected alongside the terminal accept CQE.
			}
		}
		if !terminalAccept {
			b.Wait()
		}
	}

	if !terminalAccept {
		t.Fatal("SubmitAcceptMultishot cancel termination not received after successful accept")
	}
}

// TestSubmitReceiveMultishot tests raw multishot receive CQEs.
func TestSubmitReceiveMultishot(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("no registered buffers available")
	}

	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)

	pollFD := fds[0]
	recvCtx := uring.ForFD(int32(fds[0]))
	if err := ring.SubmitReceiveMultishot(
		recvCtx,
		&pollFD,
		nil,
		uring.WithIOPrio(uring.IORING_RECVSEND_POLL_FIRST),
	); err != nil {
		// May not be supported without buffer ring
		t.Logf("SubmitReceiveMultishot returned error (may require buffer ring): %v", err)
		return
	}

	cqes := make([]uring.CQEView, 16)
	// Prime Wait: no data has been written yet, so any recv CQE here
	// should carry Res==0 (multishot armed, no payload).
	n, err := ring.Wait(cqes)
	if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
		t.Fatalf("prime Wait: %v", err)
	}
	for i := 0; i < n; i++ {
		cqe := cqes[i]
		if cqe.Op() != uring.IORING_OP_RECV {
			continue
		}
		if cqe.Res < 0 {
			t.Fatalf("prime Wait recv CQE failed: %d", cqe.Res)
		}
		if cqe.Res > 0 {
			t.Fatalf("prime Wait returned unexpected recv CQE: bufID=%d bytes=%d", cqe.BufID(), cqe.Res)
		}
	}

	payload := []byte("test multishot recv")
	if n, errno := writeTestFD(fds[1], payload); errno != 0 {
		t.Fatalf("Write payload: %v", zcall.Errno(errno))
	} else if n != len(payload) {
		t.Fatalf("Write payload bytes: got %d, want %d", n, len(payload))
	}

	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	recvCount := 0
	totalBytes := 0
	hasMoreSeen := false

	for time.Now().Before(deadline) && totalBytes < len(payload) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() != uring.IORING_OP_RECV {
				continue
			}
			if cqe.Res < 0 {
				t.Fatalf("recv CQE failed: %d", cqe.Res)
			}
			if cqe.Res == 0 {
				continue
			}
			if !cqe.HasBuffer() {
				t.Fatal("recv CQE missing buffer-selection metadata")
			}
			recvCount++
			totalBytes += int(cqe.Res)
			if cqe.HasMore() {
				hasMoreSeen = true
			}
		}
		if totalBytes < len(payload) {
			b.Wait()
		}
	}

	if recvCount == 0 {
		t.Fatal("received 0 recv CQEs, want at least 1")
	}
	if totalBytes != len(payload) {
		t.Fatalf("received %d bytes, want %d", totalBytes, len(payload))
	}
	if !hasMoreSeen {
		t.Fatal("recv multishot CQE did not advertise continuation")
	}

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, int32(fds[0]))
	if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
		t.Fatalf("AsyncCancelFD: %v", err)
	}

	terminalRecv := false
	stopDeadline := time.Now().Add(3 * time.Second)
	b.Reset()
	for time.Now().Before(stopDeadline) && !terminalRecv {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			switch cqe.Op() {
			case uring.IORING_OP_RECV:
				if cqe.Res < 0 && cqe.Res != -int32(uring.ECANCELED) {
					t.Fatalf("terminal recv CQE failed: %d", cqe.Res)
				}
				if !cqe.HasMore() {
					terminalRecv = true
				}
			case uring.IORING_OP_ASYNC_CANCEL:
				// AsyncCancelFD completion is expected alongside the terminal recv CQE.
			}
		}
		if !terminalRecv {
			b.Wait()
		}
	}

	if !terminalRecv {
		t.Fatal("multishot recv did not terminate after cancel")
	}
}

// TestMultishotHandlerStopRequest tests handler-initiated stop.
func TestMultishotHandlerStopRequest(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	handler := &testMultishotHandler{}
	handler.shouldStop.Store(true) // Handler will request stop on first result
	sqeCtx := uring.ForFD(fd)

	sub, err := ring.AcceptMultishot(sqeCtx, handler)
	if err != nil {
		t.Logf("AcceptMultishot returned error: %v", err)
		return
	}

	// Connect a client
	go func() {
		time.Sleep(50 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", addr.String(), 2*time.Second)
		if err == nil {
			conn.Close()
		}
	}()

	// Wait for completion
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		for i := 0; i < n; i++ {
			dispatchSimplifiedMultishotCQE(handler, cqes[i])
		}

		if handler.resultCount.Load() > 0 {
			break
		}
		b.Wait()
	}

	// After handler returns false, subscription should no longer be active
	if handler.resultCount.Load() > 0 && sub.Active() {
		// May still be active while cancellation is in progress
		t.Logf("Subscription still active after handler stop request (cancellation in progress)")
	}

	t.Logf("results: %d, errors: %d, stopped: %d",
		handler.resultCount.Load(),
		handler.errorCount.Load(),
		handler.stoppedCount.Load())
}

// TestMultishotPoolExhaustion tests pool exhaustion returns ErrWouldBlock.
func TestMultishotPoolExhaustion(t *testing.T) {
	// Create ring with minimal pool capacity (2 entries)
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = 2 // Minimal entries for pool exhaustion test
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	handler := &testMultishotHandler{}
	sqeCtx := uring.ForFD(fd)

	// Exhaust the pool
	subs := make([]*uring.MultishotSubscription, 0, 2)
	for i := 0; i < 2; i++ {
		sub, err := ring.AcceptMultishot(sqeCtx, handler)
		if err != nil {
			t.Logf("AcceptMultishot[%d]: %v", i, err)
			continue
		}
		subs = append(subs, sub)
	}

	// Next operation should return ErrWouldBlock
	_, err = ring.AcceptMultishot(sqeCtx, handler)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Errorf("expected ErrWouldBlock when pool exhausted, got: %v", err)
	}

	// Cancel all subscriptions before ring teardown
	for _, sub := range subs {
		sub.Cancel()
	}
}

// TestMultishotCancelIdempotent tests that Cancel is idempotent.
func TestMultishotCancelIdempotent(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	handler := &testMultishotHandler{}
	sqeCtx := uring.ForFD(fd)
	sub, err := ring.AcceptMultishot(sqeCtx, handler)
	if err != nil {
		t.Logf("AcceptMultishot returned error: %v", err)
		return
	}

	// Cancel multiple times should be safe (idempotent)
	for i := 0; i < 3; i++ {
		if err := sub.Cancel(); err != nil {
			t.Logf("Cancel[%d] returned error: %v", i, err)
		}
	}

	// Drain terminal CQE to verify lifecycle convergence
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	for time.Now().Before(deadline) && sub.State() != uring.SubscriptionStopped {
		_, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		b.Wait()
	}
	t.Logf("final state: %v, stopped: %d", sub.State(), handler.stoppedCount.Load())
}

// TestSubscriptionState tests the SubscriptionState constants.
func TestSubscriptionState(t *testing.T) {
	if uring.SubscriptionActive >= uring.SubscriptionCancelling {
		t.Error("SubscriptionActive should be less than SubscriptionCancelling")
	}
	if uring.SubscriptionCancelling >= uring.SubscriptionStopped {
		t.Error("SubscriptionCancelling should be less than SubscriptionStopped")
	}
}

// TestMultishotStressAccept stress tests multishot accept with many connections
// using raw SubmitAcceptMultishot and manual CQE processing.
func TestMultishotStressAccept(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesLarge
			opts.NotifySucceed = true
			opts.MultiIssuers = true
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, fd)
	if err := ring.SubmitAcceptMultishot(acceptCtx); err != nil {
		t.Logf("SubmitAcceptMultishot: %v (may be unsupported)", err)
		return
	}

	const numConnections = 50
	go func() {
		for i := 0; i < numConnections; i++ {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				continue
			}
			conn.Close()
		}
	}()

	cqes := make([]uring.CQEView, 64)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	accepted := 0

	for time.Now().Before(deadline) && accepted < numConnections {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Logf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() != uring.IORING_OP_ACCEPT {
				continue
			}
			if cqe.Res >= 0 {
				accepted++
				closeTestFD(iofd.FD(cqe.Res))
			}
		}
		if accepted < numConnections {
			b.Wait()
		}
	}

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, fd)
	if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
		t.Fatalf("AsyncCancelFD: %v", err)
	}

	b.Reset()
	terminalSeen := false
	stopDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(stopDeadline) && !terminalSeen {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() == uring.IORING_OP_ACCEPT {
				if cqe.Res >= 0 {
					accepted++
					closeTestFD(iofd.FD(cqe.Res))
				}
				if !cqe.HasMore() {
					terminalSeen = true
				}
			}
		}
		if !terminalSeen {
			b.Wait()
		}
	}

	t.Logf("Accepted %d connections (target: %d)", accepted, numConnections)
	if accepted < numConnections/2 {
		t.Fatalf("accepted %d connections, want at least %d", accepted, numConnections/2)
	}
}
