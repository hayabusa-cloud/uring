// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"net"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

func waitForGCSignal(ch <-chan struct{}, rounds int) bool {
	for range rounds {
		runtime.GC()
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
		select {
		case <-ch:
			return true
		default:
		}
	}
	return false
}

// TestGCSafetyBundleIteratorBackingRetained verifies that buffer backing
// memory remains accessible through BundleIterator after GC cycles.
// The parent (uringBufferRings or caller) holds the []byte slice;
// BundleIterator holds only unsafe.Pointer extracted from it.
// This test confirms the GC root in the caller keeps the memory alive.
func TestGCSafetyBundleIteratorBackingRetained(t *testing.T) {
	const bufCount = 16
	const bufSize = 128

	// Allocate backing and fill with a known pattern.
	backing := make([]byte, bufCount*bufSize)
	for i := range backing {
		backing[i] = byte(i % 251) // prime modulus avoids period alignment
	}

	cqe := uring.CQEView{
		Res:   int32(4 * bufSize), // 4 buffers
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Force multiple GC cycles while the iterator is live.
	for range 5 {
		runtime.GC()
	}

	// Verify data integrity after GC.
	for i := range 4 {
		buf := iter.Buffer(i)
		if buf == nil {
			t.Fatalf("Buffer(%d) returned nil after GC", i)
		}
		for j, b := range buf {
			want := byte((i*bufSize + j) % 251)
			if b != want {
				t.Fatalf("Buffer(%d)[%d]: got %d, want %d (data corrupted after GC)", i, j, b, want)
			}
		}
	}
}

// TestGCSafetyIncrementalReceiverBackingRetained verifies that
// IncrementalReceiver keeps buffer backing memory alive through GC.
func TestGCSafetyIncrementalReceiverBackingRetained(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))

	const bufCount = 8
	const bufSize = 256

	// Allocate backing, fill with pattern, then drop local alias.
	bufBacking := make([]byte, bufCount*bufSize)
	for i := range bufBacking {
		bufBacking[i] = byte(i % 239)
	}

	// Keep an unsafe.Pointer to the backing data so we can verify
	// integrity after dropping the local slice variable.
	backingPtr := unsafe.Pointer(unsafe.SliceData(bufBacking))
	backingLen := len(bufBacking)

	receiver := uring.NewIncrementalReceiver(ring, pool, 0, bufSize, bufBacking, bufCount)
	if receiver == nil {
		t.Fatal("NewIncrementalReceiver returned nil")
	}

	// Drop the local reference; only receiver.bufBacking holds the slice.
	bufBacking = nil

	// Force GC to collect unreferenced memory.
	for range 5 {
		runtime.GC()
	}

	// The receiver is still alive and holds the backing slice via its
	// bufBacking field. Read through the raw pointer to verify the data
	// survived GC. If the receiver failed to keep it alive, we would
	// see corruption or a crash.
	retained := unsafe.Slice((*byte)(backingPtr), backingLen)
	for i, b := range retained {
		want := byte(i % 239)
		if b != want {
			t.Fatalf("backing[%d]: got %d, want %d (GC corrupted receiver backing)", i, b, want)
		}
	}

	runtime.KeepAlive(receiver)
}

// TestGCSafetyBundleIteratorConcurrentGC verifies BundleIterator data
// integrity when GC runs concurrently with iteration.
func TestGCSafetyBundleIteratorConcurrentGC(t *testing.T) {
	const bufCount = 32
	const bufSize = 256

	backing := make([]byte, bufCount*bufSize)
	for i := range backing {
		backing[i] = byte(i % 251)
	}

	cqe := uring.CQEView{
		Res:   int32(8 * bufSize),
		Flags: 5 << uring.IORING_CQE_BUFFER_SHIFT, // startID=5, wraps around
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Allocate garbage to trigger GC pressure during iteration.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_ = make([]byte, 1<<16)
			runtime.Gosched()
		}
	}()

	// Iterate while GC pressure is high.
	count := 0
	for buf := range iter.All() {
		if len(buf) != bufSize {
			t.Errorf("buffer %d: got len %d, want %d", count, len(buf), bufSize)
		}
		// Verify content integrity.
		id := int(iter.SlotID(count)) * bufSize
		for j := range buf {
			want := byte((id + j) % 251)
			if buf[j] != want {
				t.Fatalf("buffer %d[%d]: got %d, want %d (corruption under GC pressure)",
					count, j, buf[j], want)
			}
		}
		count++
	}

	if count != 8 {
		t.Errorf("iterated %d buffers, want 8", count)
	}

	<-done
	runtime.KeepAlive(backing)
}

// TestGCSafetyBundleIteratorRecycleAfterGC verifies that Recycle works
// correctly after GC cycles, confirming the unsafe.Pointer in
// BundleIterator still points to valid memory.
func TestGCSafetyBundleIteratorRecycleAfterGC(t *testing.T) {
	const bufCount = 8
	const bufSize = 64

	backing := make([]byte, bufCount*bufSize)
	for i := range backing {
		backing[i] = byte(i % 197)
	}

	cqe := uring.CQEView{
		Res:   int32(3 * bufSize),
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Consume all buffers.
	consumed := 0
	for range iter.All() {
		consumed++
	}
	if consumed != 3 {
		t.Fatalf("consumed %d, want 3", consumed)
	}

	// Force GC before CopyTo (simulates delayed processing).
	for range 3 {
		runtime.GC()
	}

	// Verify CopyTo still works (reads from unsafe.Pointer-backed memory).
	dst := make([]byte, 3*bufSize)
	n := iter.CopyTo(dst)
	if n != 3*bufSize {
		t.Fatalf("CopyTo: got %d bytes, want %d", n, 3*bufSize)
	}

	// Verify copied data matches original backing.
	for i := range n {
		want := byte(i % 197)
		if dst[i] != want {
			t.Fatalf("CopyTo result[%d]: got %d, want %d", i, dst[i], want)
		}
	}

	runtime.KeepAlive(backing)
}

type zcSafetyHandler struct{}

func (h *zcSafetyHandler) OnCompleted(result int32)    {}
func (h *zcSafetyHandler) OnNotification(result int32) {}

func submitAndRetireZCProbe(t *testing.T, ring *uring.Uring, pool *uring.ContextPools, tracker *uring.ZCTracker, fd iofd.FD, buf []byte) {
	t.Helper()

	collected := make(chan struct{})
	h := &zcSafetyHandler{}
	runtime.SetFinalizer(h, func(*zcSafetyHandler) {
		close(collected)
	})

	err := tracker.SendZC(fd, buf, h)
	if err != nil {
		t.Fatalf("SendZC: %v", err)
	}

	h = nil

	if waitForGCSignal(collected, 20) {
		t.Fatal("handler collected before CQE retirement")
	}

	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	handled := false
	retired := false
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if tracker.HandleCQE(cqes[i]) {
				handled = true
			}
		}
		if handled && pool.ExtendedAvailable() == pool.Capacity() {
			retired = true
			break
		}
		b.Wait()
	}
	if !handled {
		t.Skip("no zero-copy callbacks observed on loopback in this environment")
	}
	if !retired {
		t.Fatal("zero-copy tracker slot was not retired after CQE handling")
	}

	runtime.KeepAlive(buf)
	runtime.KeepAlive(tracker)
	runtime.KeepAlive(ring)
}

func TestZCTrackerGCSafety(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(1)
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

	submitAndRetireZCProbe(t, ring, pool, tracker, iofd.FD(serverFile.Fd()), []byte("test data"))
	submitAndRetireZCProbe(t, ring, pool, tracker, iofd.FD(serverFile.Fd()), []byte("test data again"))
}
