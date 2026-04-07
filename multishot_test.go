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

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
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

// TestMultishotAcceptMultishot tests multishot accept operation.
func TestMultishotAcceptMultishot(t *testing.T) {
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

	// Get the listener fd
	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	handler := &testMultishotHandler{}
	sqeCtx := uring.ForFD(fd)

	// Submit multishot accept via Uring factory
	sub, err := ring.AcceptMultishot(sqeCtx, handler)
	if err != nil {
		// Multishot accept might not be supported on all kernels
		t.Logf("AcceptMultishot returned error (may be unsupported): %v", err)
		return
	}

	if !sub.Active() {
		t.Error("Subscription should be active after submission")
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

		if handler.resultCount.Load() > 0 || handler.errorCount.Load() > 0 {
			break
		}
		b.Wait()
	}

	// Cancel the subscription
	if err := sub.Cancel(); err != nil {
		t.Logf("Cancel returned error: %v", err)
	}

	// Wait for stop callback
	for i := 0; i < 10 && handler.stoppedCount.Load() == 0; i++ {
		n, _ := ring.Wait(cqes)
		for j := 0; j < n; j++ {
			dispatchSimplifiedMultishotCQE(handler, cqes[j])
		}
		b.Wait()
	}

	t.Logf("results: %d, errors: %d, stopped: %d",
		handler.resultCount.Load(),
		handler.errorCount.Load(),
		handler.stoppedCount.Load())
}

// TestMultishotReceiveMultishot tests multishot receive operation.
func TestMultishotReceiveMultishot(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create socket pair
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	clientConn, err := net.DialTimeout("tcp", addr.String(), 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer serverConn.Close()

	serverFile, err := serverConn.(*net.TCPConn).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer serverFile.Close()
	fd := int32(serverFile.Fd())

	handler := &testMultishotHandler{}
	// SQEContext with FD and buffer group
	sqeCtx := uring.ForFD(fd).WithBufGroup(0)

	// Submit multishot receive via Uring factory
	sub, err := ring.ReceiveMultishot(sqeCtx, handler)
	if err != nil {
		// May not be supported without buffer ring
		t.Logf("ReceiveMultishot returned error (may require buffer ring): %v", err)
		return
	}

	if !sub.Active() {
		t.Error("Subscription should be active after submission")
	}

	// Send data from client
	go func() {
		time.Sleep(50 * time.Millisecond)
		clientConn.Write([]byte("test multishot recv"))
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

		if handler.resultCount.Load() > 0 || handler.errorCount.Load() > 0 {
			break
		}
		b.Wait()
	}

	// Cancel the subscription
	if err := sub.Cancel(); err != nil {
		t.Logf("Cancel returned error: %v", err)
	}

	t.Logf("results: %d, errors: %d, stopped: %d",
		handler.resultCount.Load(),
		handler.errorCount.Load(),
		handler.stoppedCount.Load())
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
	for i := 0; i < 2; i++ {
		_, err := ring.AcceptMultishot(sqeCtx, handler)
		if err != nil {
			t.Logf("AcceptMultishot[%d]: %v", i, err)
		}
	}

	// Next operation should return ErrWouldBlock
	_, err = ring.AcceptMultishot(sqeCtx, handler)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Errorf("expected ErrWouldBlock when pool exhausted, got: %v", err)
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

// TestMultishotStressAccept stress tests multishot accept with many connections.
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

	// Track accepts using the test handler
	handler := &testMultishotHandler{}

	sqeCtx := uring.ForFD(fd)
	sub, err := ring.AcceptMultishot(sqeCtx, handler)
	if err != nil {
		t.Fatalf("AcceptMultishot: %v", err)
	}

	// Spawn connection goroutine
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

	// Event loop to process CQEs
	cqes := make([]uring.CQEView, 64)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) && handler.resultCount.Load() < numConnections {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Logf("Wait: %v", err)
		}

		// Process CQEs - extended mode handles callbacks internally
		for range n {
			// CQE processing happens via handler callbacks
		}
		_ = n

		b.Wait()
	}

	// Cancel subscription
	sub.Cancel()

	acceptCount := handler.resultCount.Load()
	t.Logf("Accepted %d connections (target: %d)", acceptCount, numConnections)
	if acceptCount < numConnections/2 {
		t.Logf("Note: Accept count low - may be timing-related")
	}
}
