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

// testIncHandler implements IncrementalHandler for testing.
type testIncHandler struct {
	dataCount    atomic.Int32
	completeCall atomic.Int32
	errorCall    atomic.Int32
	totalBytes   atomic.Int32
	lastHasMore  atomic.Bool
	lastErr      atomic.Value // stores error
}

func (h *testIncHandler) OnData(buf []byte, hasMore bool) {
	h.dataCount.Add(1)
	h.totalBytes.Add(int32(len(buf)))
	h.lastHasMore.Store(hasMore)
}

func (h *testIncHandler) OnComplete() {
	h.completeCall.Add(1)
}

func (h *testIncHandler) OnError(err error) {
	h.errorCall.Add(1)
	h.lastErr.Store(err)
}

// TestIncrementalReceiverBasic tests basic incremental receive functionality.
func TestIncrementalReceiverBasic(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))

	// Create buffer backing for incremental receive
	const bufCount = 4
	const bufSize = 1024
	bufBacking := make([]byte, bufCount*bufSize)

	receiver := uring.NewIncrementalReceiver(ring, pool, 0, bufSize, bufBacking, bufCount)
	if receiver == nil {
		t.Fatal("NewIncrementalReceiver returned nil")
	}

	// Setup TCP connection
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

	// Send data and close client before submitting recv so the kernel socket
	// already has buffered data + EOF when the SQE is processed.
	testData := []byte("Hello, incremental receive!")
	if _, err := clientConn.Write(testData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	clientConn.Close()

	handler := &testIncHandler{}

	// Submit incremental recv
	err = receiver.Recv(iofd.FD(serverFile.Fd()), handler)
	if err != nil {
		t.Skipf("incremental black-box test does not provision IOU_PBUF_RING_INC buffers: %v", err)
	}

	// Wait for completion
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		progressed := false
		for i := 0; i < n; i++ {
			if receiver.HandleCQE(cqes[i]) {
				progressed = true
			}
		}

		if handler.completeCall.Load() > 0 || handler.errorCall.Load() > 0 {
			break
		}
		if progressed {
			b.Reset()
			continue
		}
		b.Wait()
	}

	if handler.errorCall.Load() > 0 && handler.dataCount.Load() == 0 && handler.completeCall.Load() == 0 {
		lastErr, _ := handler.lastErr.Load().(error)
		t.Skipf("incremental black-box test hit the unconfigured buffer-ring path: %v", lastErr)
	}
	if handler.completeCall.Load() == 0 && handler.errorCall.Load() == 0 {
		t.Skip("no incremental callbacks observed in black-box test; receive semantics are covered in incremental_internal_test")
	}

	// Report results
	t.Logf("data calls: %d, complete calls: %d, error calls: %d, total bytes: %d",
		handler.dataCount.Load(),
		handler.completeCall.Load(),
		handler.errorCall.Load(),
		handler.totalBytes.Load())
}

// TestIncrementalReceiverPoolExhaustion verifies pool exhaustion returns ErrWouldBlock.
func TestIncrementalReceiverPoolExhaustion(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pool with minimal capacity (2 entries)
	pool := uring.NewContextPools(2)

	const bufCount = 4
	const bufSize = 256
	bufBacking := make([]byte, bufCount*bufSize)

	receiver := uring.NewIncrementalReceiver(ring, pool, 0, bufSize, bufBacking, bufCount)

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

	handler := &testIncHandler{}

	// Exhaust the pool
	for i := 0; i < 2; i++ {
		err := receiver.Recv(iofd.FD(serverFile.Fd()), handler)
		if err != nil {
			t.Logf("Recv[%d]: %v", i, err)
		}
	}

	// Next recv should return ErrWouldBlock
	err = receiver.Recv(iofd.FD(serverFile.Fd()), handler)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Errorf("expected ErrWouldBlock when pool exhausted, got: %v", err)
	}
}

// TestIncrementalReceiverHandlerOrder verifies OnData is called before OnComplete.
func TestIncrementalReceiverHandlerOrder(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))

	const bufCount = 4
	const bufSize = 1024
	bufBacking := make([]byte, bufCount*bufSize)

	receiver := uring.NewIncrementalReceiver(ring, pool, 0, bufSize, bufBacking, bufCount)

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

	// Track order
	var callOrder []string
	var mu atomic.Int32

	orderHandler := &orderTrackingIncHandler{
		onData: func(buf []byte, hasMore bool) {
			for !mu.CompareAndSwap(0, 1) {
			}
			callOrder = append(callOrder, "data")
			mu.Store(0)
		},
		onComplete: func() {
			for !mu.CompareAndSwap(0, 1) {
			}
			callOrder = append(callOrder, "complete")
			mu.Store(0)
		},
		onError: func(err error) {
			for !mu.CompareAndSwap(0, 1) {
			}
			callOrder = append(callOrder, "error")
			mu.Store(0)
		},
	}

	// Send data
	go func() {
		time.Sleep(50 * time.Millisecond)
		clientConn.Write([]byte("order test"))
	}()

	err = receiver.Recv(iofd.FD(serverFile.Fd()), orderHandler)
	if err != nil {
		t.Skipf("incremental black-box test does not provision IOU_PBUF_RING_INC buffers: %v", err)
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) && len(callOrder) < 2 {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		progressed := false
		for i := 0; i < n; i++ {
			if receiver.HandleCQE(cqes[i]) {
				progressed = true
			}
		}
		if progressed {
			b.Reset()
			continue
		}
		b.Wait()
	}

	if len(callOrder) == 0 {
		t.Skip("no incremental callbacks observed in black-box test; handler lifecycle is covered in incremental_internal_test")
	}
	if len(callOrder) == 1 && callOrder[0] == "error" {
		t.Skip("incremental black-box test hit the unconfigured buffer-ring path")
	}

	// Verify order: data should come before complete.
	if callOrder[0] != "data" {
		t.Errorf("expected first call to be 'data', got '%s'", callOrder[0])
	}
	if callOrder[len(callOrder)-1] != "complete" {
		t.Errorf("expected last call to be 'complete', got '%s'", callOrder[len(callOrder)-1])
	}
}

// orderTrackingIncHandler tracks handler call order.
type orderTrackingIncHandler struct {
	onData     func([]byte, bool)
	onComplete func()
	onError    func(error)
}

func (h *orderTrackingIncHandler) OnData(buf []byte, hasMore bool) {
	if h.onData != nil {
		h.onData(buf, hasMore)
	}
}

func (h *orderTrackingIncHandler) OnComplete() {
	if h.onComplete != nil {
		h.onComplete()
	}
}

func (h *orderTrackingIncHandler) OnError(err error) {
	if h.onError != nil {
		h.onError(err)
	}
}
