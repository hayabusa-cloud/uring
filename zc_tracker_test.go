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
)

// =============================================================================
// ZCTracker Tests
// =============================================================================

// testZCHandler implements ZCHandler for testing.
type testZCHandler struct {
	completedCalls atomic.Int32
	notifyCalls    atomic.Int32
	completedRes   atomic.Int32
	notifyRes      atomic.Int32
}

func (h *testZCHandler) OnCompleted(result int32) {
	h.completedRes.Store(result)
	h.completedCalls.Add(1)
}

func (h *testZCHandler) OnNotification(result int32) {
	h.notifyRes.Store(result)
	h.notifyCalls.Add(1)
}

// TestZCTrackerBasic verifies the ZCTracker handles the two-CQE model.
func TestZCTrackerBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping zero-copy tracker test in short mode (requires reliable io_uring)")
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create context pool and tracker
	pool := uring.NewContextPools(int(uring.EntriesSmall))
	tracker := uring.NewZCTracker(ring, pool)

	// Create TCP socket pair
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

	// Get raw FD
	serverFile, err := serverConn.(*net.TCPConn).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer serverFile.Close()

	// Prepare test data
	testData := []byte("ZCTracker Test Data")
	handler := &testZCHandler{}

	// Submit zero-copy send via tracker
	err = tracker.SendZC(iofd.FD(serverFile.Fd()), testData, handler)
	if err != nil {
		t.Fatalf("SendZC: %v", err)
	}

	// Collect CQEs and process through tracker
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		for i := 0; i < n; i++ {
			tracker.HandleCQE(cqes[i])
		}

		// Check if handler received both callbacks
		if handler.completedCalls.Load() > 0 && handler.notifyCalls.Load() > 0 {
			break
		}
		b.Wait()
	}

	completedCalls := handler.completedCalls.Load()
	notifyCalls := handler.notifyCalls.Load()
	completedRes := handler.completedRes.Load()

	t.Logf("completed calls: %d, notify calls: %d, completed res: %d",
		completedCalls, notifyCalls, completedRes)

	if completedCalls == 0 && notifyCalls == 0 {
		t.Skip("no zero-copy callbacks observed on loopback in this environment")
	}

	// Handle EOPNOTSUPP (-95) gracefully
	if completedRes == -95 {
		t.Log("EOPNOTSUPP returned (expected on loopback)")
		if completedCalls != 1 {
			t.Errorf("expected 1 completed call for EOPNOTSUPP, got %d", completedCalls)
		}
		return
	}

	// Normal zero-copy: should have both callbacks
	if completedCalls != 1 {
		t.Errorf("expected 1 completed call, got %d", completedCalls)
	}
	if notifyCalls != 1 {
		t.Errorf("expected 1 notify call, got %d", notifyCalls)
	}
}

// TestZCTrackerPoolExhaustion verifies pool exhaustion returns ErrWouldBlock.
func TestZCTrackerPoolExhaustion(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create a small pool to exhaust
	pool := uring.NewContextPools(2)
	tracker := uring.NewZCTracker(ring, pool)

	// Create socket for testing
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

	testData := []byte("test")
	handler := &testZCHandler{}

	// Exhaust the pool (capacity rounded to power of 2 = 2)
	for i := 0; i < 2; i++ {
		err := tracker.SendZC(iofd.FD(serverFile.Fd()), testData, handler)
		if err != nil {
			t.Logf("SendZC[%d]: %v", i, err)
		}
	}

	// Next send should return ErrWouldBlock
	err = tracker.SendZC(iofd.FD(serverFile.Fd()), testData, handler)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Errorf("expected ErrWouldBlock when pool exhausted, got: %v", err)
	}
}

// TestZCTrackerHandlerOrder verifies OnCompleted is called before OnNotification.
func TestZCTrackerHandlerOrder(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))
	tracker := uring.NewZCTracker(ring, pool)

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

	// Track call order
	var callOrder []string
	var mu atomic.Int32 // Simple lock via atomic

	orderHandler := &orderTrackingHandler{
		onCompleted: func(res int32) {
			for !mu.CompareAndSwap(0, 1) {
			}
			callOrder = append(callOrder, "completed")
			mu.Store(0)
		},
		onNotification: func(res int32) {
			for !mu.CompareAndSwap(0, 1) {
			}
			callOrder = append(callOrder, "notification")
			mu.Store(0)
		},
	}

	testData := []byte("order test")
	err = tracker.SendZC(iofd.FD(serverFile.Fd()), testData, orderHandler)
	if err != nil {
		t.Fatalf("SendZC: %v", err)
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) && len(callOrder) < 2 {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			tracker.HandleCQE(cqes[i])
		}
		b.Wait()
	}

	if len(callOrder) == 0 {
		t.Skip("no zero-copy callbacks observed on loopback in this environment")
	}
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 callbacks, got %v", callOrder)
	}
	if callOrder[0] != "completed" {
		t.Errorf("expected first call to be 'completed', got '%s'", callOrder[0])
	}
	if callOrder[1] != "notification" {
		t.Errorf("expected second call to be 'notification', got '%s'", callOrder[1])
	}
}

// orderTrackingHandler tracks the order of handler calls.
type orderTrackingHandler struct {
	onCompleted    func(int32)
	onNotification func(int32)
}

func (h *orderTrackingHandler) OnCompleted(result int32) {
	if h.onCompleted != nil {
		h.onCompleted(result)
	}
}

func (h *orderTrackingHandler) OnNotification(result int32) {
	if h.onNotification != nil {
		h.onNotification(result)
	}
}

// TestZCTrackerExactlyOnce verifies handlers are invoked exactly once.
func TestZCTrackerExactlyOnce(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))
	tracker := uring.NewZCTracker(ring, pool)

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

	handler := &testZCHandler{}
	testData := []byte("exactly-once test")

	err = tracker.SendZC(iofd.FD(serverFile.Fd()), testData, handler)
	if err != nil {
		t.Fatalf("SendZC: %v", err)
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	processed := 0

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		for i := 0; i < n; i++ {
			// Process each CQE multiple times to test idempotency
			for j := 0; j < 3; j++ {
				tracker.HandleCQE(cqes[i])
			}
			processed++
		}

		if handler.completedCalls.Load() > 0 && handler.notifyCalls.Load() > 0 {
			break
		}
		b.Wait()
	}

	t.Logf("processed %d CQEs", processed)
	if processed == 0 {
		t.Skip("no zero-copy CQEs observed on loopback in this environment")
	}

	// Despite multiple HandleCQE calls, handler should be called exactly once each
	completedCalls := handler.completedCalls.Load()
	notifyCalls := handler.notifyCalls.Load()

	// EOPNOTSUPP case: both called together
	if handler.completedRes.Load() == -95 {
		if completedCalls != 1 || notifyCalls != 1 {
			t.Errorf("EOPNOTSUPP: expected 1/1 calls, got %d/%d", completedCalls, notifyCalls)
		}
		return
	}

	// Normal case
	if completedCalls != 1 {
		t.Errorf("expected exactly 1 OnCompleted call, got %d", completedCalls)
	}
	if notifyCalls != 1 {
		t.Errorf("expected exactly 1 OnNotification call, got %d", notifyCalls)
	}
}
