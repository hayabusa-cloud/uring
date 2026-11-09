// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
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
	received     []byte
}

func (h *testIncHandler) OnData(buf []byte, hasMore bool) {
	h.dataCount.Add(1)
	h.totalBytes.Add(int32(len(buf)))
	h.lastHasMore.Store(hasMore)
	h.received = append(h.received, buf...)
}

func (h *testIncHandler) OnComplete() {
	h.completeCall.Add(1)
}

func (h *testIncHandler) OnError(err error) {
	h.errorCall.Add(1)
	h.lastErr.Store(err)
}

func newIncrementalTestReceiver(t *testing.T, ring *uring.Uring, pool *uring.ContextPools, bufSize, entries int) (*uring.IncrementalReceiver, []byte) {
	t.Helper()

	const groupID uint16 = 60000
	backing := make([]byte, entries*bufSize)
	bufRing, err := ring.RegisterBufRingIncremental(entries, groupID)
	if err != nil {
		t.Fatalf("RegisterBufRingIncremental: %v", err)
	}
	for i := range entries {
		buf := backing[i*bufSize : (i+1)*bufSize]
		if err := bufRing.Add(buf, uint16(i), i); err != nil {
			t.Fatalf("BufferRing.Add(%d): %v", i, err)
		}
	}
	if err := bufRing.Advance(entries); err != nil {
		t.Fatalf("BufferRing.Advance: %v", err)
	}
	return uring.NewIncrementalReceiver(ring, pool, groupID, bufSize, backing, entries), backing
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

	const bufCount = 4
	const bufSize = 1024
	receiver, _ := newIncrementalTestReceiver(t, ring, pool, bufSize, bufCount)
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
		t.Fatalf("Recv: %v", err)
	}

	// Wait for completion
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
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

	if handler.errorCall.Load() != 0 {
		lastErr, _ := handler.lastErr.Load().(error)
		t.Fatalf("incremental receive error: %v", lastErr)
	}
	if handler.completeCall.Load() != 1 {
		t.Fatalf("complete calls = %d, want 1", handler.completeCall.Load())
	}
	if handler.dataCount.Load() != 1 {
		t.Fatalf("data calls = %d, want 1", handler.dataCount.Load())
	}
	if !bytes.Equal(handler.received, testData) {
		t.Fatalf("received data = %q, want %q", handler.received, testData)
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
	receiver, _ := newIncrementalTestReceiver(t, ring, pool, bufSize, bufCount)

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

	// Exhaust the pool without creating live kernel requests.
	held := make([]*uring.ExtSQE, 0, 2)
	for range 2 {
		ext := pool.Extended()
		if ext == nil {
			t.Fatal("Extended returned nil before pool was exhausted")
		}
		held = append(held, ext)
	}

	// Recv should return ErrWouldBlock and leave the receiver reusable.
	err = receiver.Recv(iofd.FD(serverFile.Fd()), handler)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Errorf("expected ErrWouldBlock when pool exhausted, got: %v", err)
	}
	for _, ext := range held {
		pool.PutExtended(ext)
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
	receiver, _ := newIncrementalTestReceiver(t, ring, pool, bufSize, bufCount)

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
		_, _ = clientConn.Write([]byte("order test"))
		_ = clientConn.Close()
	}()

	err = receiver.Recv(iofd.FD(serverFile.Fd()), orderHandler)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) && len(callOrder) < 2 {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
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
		t.Fatal("no incremental callbacks observed")
	}
	if len(callOrder) == 1 && callOrder[0] == "error" {
		t.Fatal("incremental receive returned only an error callback")
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
