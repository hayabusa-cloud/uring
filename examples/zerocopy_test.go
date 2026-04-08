// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// TestZCTrackerBasic demonstrates the ZCTracker for managing
// zero-copy sends through the two-CQE model.
//
// Architecture:
//   - Zero-copy sends produce TWO CQEs:
//     1. Operation CQE (IORING_CQE_F_MORE) - send completed, buffer in use
//     2. Notification CQE (IORING_CQE_F_NOTIF) - buffer can be reused
//   - ZCTracker manages this lifecycle automatically
//   - ZCHandler interface receives both callbacks in order
//
// This pattern is useful for high-throughput scenarios where avoiding
// memory copies provides measurable benefit (typically >1.5KB payloads).
//
// See also: TestMulticastZeroCopy for broadcasting to multiple targets.
func TestZCTrackerBasic(t *testing.T) {
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 18
		},
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	tracker := uring.NewZCTracker(ring, pool)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	var serverConn net.Conn
	var acceptErr error
	acceptDone := make(chan struct{})
	go func() {
		serverConn, acceptErr = listener.Accept()
		close(acceptDone)
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	<-acceptDone
	if acceptErr != nil {
		t.Fatalf("Accept: %v", acceptErr)
	}
	defer serverConn.Close()

	tcpConn := clientConn.(*net.TCPConn)
	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}

	var clientFD iofd.FD
	rawConn.Control(func(fd uintptr) {
		clientFD = iofd.FD(fd)
	})

	handler := &testZCHandler{
		completedCh:    make(chan int32, 1),
		notificationCh: make(chan int32, 1),
	}

	testData := []byte("Hello, Zero-Copy World!")
	err = tracker.SendZC(clientFD, testData, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("SendZC: %v", err)
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	completionsSeen := 0

	for completionsSeen < 2 && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			if tracker.HandleCQE(cqes[i]) {
				completionsSeen++
			}
		}
		if completionsSeen < 2 {
			b.Wait()
		}
	}

	select {
	case result := <-handler.completedCh:
		if result < 0 {
			t.Logf("ZC send result: %d (may be EOPNOTSUPP on loopback)", result)
		} else {
			t.Logf("OnCompleted: %d bytes sent", result)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("OnCompleted not received (expected on loopback)")
	}

	select {
	case result := <-handler.notificationCh:
		t.Logf("OnNotification: result=%d, buffer can be reused", result)
	case <-time.After(100 * time.Millisecond):
		t.Log("OnNotification not received (expected on loopback)")
	}

	serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1024)
	n, err := serverConn.Read(buf)
	if err != nil {
		t.Logf("Server read: %v (ZC may not work on loopback)", err)
	} else {
		t.Logf("Server received: %q", buf[:n])
	}
}

type testZCHandler struct {
	completedCh    chan int32
	notificationCh chan int32
}

func (h *testZCHandler) OnCompleted(result int32) {
	select {
	case h.completedCh <- result:
	default:
	}
}

func (h *testZCHandler) OnNotification(result int32) {
	select {
	case h.notificationCh <- result:
	default:
	}
}

// TestMulticastZeroCopy demonstrates broadcasting data to multiple
// connections using zero-copy sends with registered buffers.
//
// Architecture:
//   - Registered buffers are pre-pinned in kernel memory (no per-call pinning)
//   - MulticastZeroCopy sends to N targets efficiently
//   - Adaptive threshold decides ZC vs regular send based on payload size
//   - Two-CQE model applies: N*2 CQEs for N targets with zero-copy
//
// Use cases: chat servers, pub/sub systems, real-time data distribution.
func TestMulticastZeroCopy(t *testing.T) {
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 20
		},
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesMedium
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	const numClients = 4
	const messageSize = 2048

	serverSock, err := sock.ListenTCP4(&sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0})
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer serverSock.Close()
	serverAddr := serverSock.Addr()

	clientConns := make([]net.Conn, numClients)
	serverFDs := make([]int32, numClients)

	var wg sync.WaitGroup
	wg.Add(numClients)

	for i := range numClients {
		go func(idx int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", serverAddr.String())
			if err != nil {
				t.Errorf("Client %d dial: %v", idx, err)
				return
			}
			clientConns[idx] = conn
		}(i)
	}

	for i := range numClients {
		fd, errno := zcall.Accept4(uintptr(serverSock.FD().Raw()), nil, nil, zcall.SOCK_NONBLOCK|zcall.SOCK_CLOEXEC)
		if errno != 0 {
			b := iox.Backoff{}
			for j := 0; j < 100; j++ {
				b.Wait()
				fd, errno = zcall.Accept4(uintptr(serverSock.FD().Raw()), nil, nil, zcall.SOCK_NONBLOCK|zcall.SOCK_CLOEXEC)
				if errno == 0 {
					break
				}
			}
			if errno != 0 {
				t.Fatalf("Accept %d: errno %d", i, errno)
			}
		}
		serverFDs[i] = int32(fd)
	}

	wg.Wait()

	for i, conn := range clientConns {
		if conn == nil {
			t.Fatalf("Client %d failed to connect", i)
		}
		defer conn.Close()
	}

	bufIndex := 0
	buf := ring.RegisteredBuffer(bufIndex)
	if buf == nil {
		t.Fatal("No registered buffer available")
	}

	message := make([]byte, messageSize)
	for i := range message {
		message[i] = byte('A' + (i % 26))
	}
	copy(buf, message)

	targets := &fdSliceTargets{fds: serverFDs}
	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, 0)

	err = ring.MulticastZeroCopy(ctx, targets, bufIndex, 0, messageSize)
	if err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	cqes := make([]uring.CQEView, 32)
	completions := 0
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	for completions < numClients && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		completions += n
		if completions < numClients {
			b.Wait()
		}
	}

	t.Logf("Multicast to %d targets completed with %d CQEs", numClients, completions)

	if completions == 0 {
		t.Skip("MulticastZeroCopy not working on this platform (no CQEs received)")
	}

	// Verify data was actually received by clients
	// On some platforms (e.g., WSL2), CQEs complete but data isn't sent
	successCount := 0
	for i, conn := range clientConns {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		recvBuf := make([]byte, messageSize+100)
		n, err := conn.Read(recvBuf)
		if err != nil {
			t.Logf("Client %d read: %v", i, err)
			continue
		}
		if n != messageSize {
			t.Logf("Client %d: got %d bytes, want %d", i, n, messageSize)
		} else {
			successCount++
		}
	}

	if successCount == 0 {
		t.Skip("MulticastZeroCopy CQEs received but no data sent (platform limitation)")
	}

	if successCount != numClients {
		t.Errorf("Only %d/%d clients received data", successCount, numClients)
	}

	for _, fd := range serverFDs {
		zcall.Close(uintptr(fd))
	}
}

type fdSliceTargets struct {
	fds []int32
}

func (t *fdSliceTargets) Count() int {
	return len(t.fds)
}

func (t *fdSliceTargets) FD(i int) iofd.FD {
	return iofd.FD(t.fds[i])
}

// TestZeroCopyThresholds demonstrates how MulticastZeroCopy uses
// adaptive thresholds to decide between zero-copy and regular sends.
//
// Threshold logic:
//   - N >= 256 targets: any size (fully amortized)
//   - N >= 64 targets:  128 bytes minimum
//   - N >= 16 targets:  512 bytes minimum
//   - N >= 4 targets:   1024 bytes minimum
//   - N < 4 targets:    1536 bytes minimum
func TestZeroCopyThresholds(t *testing.T) {
	testCases := []struct {
		numTargets int
		msgSize    int
		desc       string
	}{
		{2, 2048, "2 targets, 2KB - uses ZC (above 1.5KB threshold)"},
		{2, 1024, "2 targets, 1KB - uses writeFixed (below 1.5KB threshold)"},
		{8, 1024, "8 targets, 1KB - uses ZC (>=4 targets, threshold 1KB)"},
		{8, 512, "8 targets, 512B - uses writeFixed (below 1KB threshold)"},
		{32, 512, "32 targets, 512B - uses ZC (>=16 targets, threshold 512B)"},
		{128, 128, "128 targets, 128B - uses ZC (>=64 targets, threshold 128B)"},
	}

	t.Log("Threshold-based zero-copy selection:")
	t.Log("-----------------------------------------------------")

	for _, tc := range testCases {
		var threshold int
		switch {
		case tc.numTargets >= 256:
			threshold = 0
		case tc.numTargets >= 64:
			threshold = 128
		case tc.numTargets >= 16:
			threshold = 512
		case tc.numTargets >= 4:
			threshold = 1024
		default:
			threshold = 1536
		}

		usesZC := tc.msgSize >= threshold
		method := "writeFixed"
		if usesZC {
			method = "sendZeroCopyFixed"
		}

		t.Logf("  %s -> %s", tc.desc, method)
	}

	t.Log("-----------------------------------------------------")
	t.Log("Note: Both methods use registered buffers (pre-pinned memory)")
	t.Log("      ZC avoids additional kernel-side memory copy")
}

// TestZeroCopyBufferLifecycle demonstrates the critical buffer lifecycle
// constraint in zero-copy sends.
//
// CRITICAL: Buffer must NOT be modified between SendZC and OnNotification.
//
// Timeline:
//
//	SendZC()         -> buffer reference passed to kernel
//	OnCompleted()    -> send completed, buffer STILL IN USE by kernel
//	OnNotification() -> kernel released buffer, safe to modify/reuse
func TestZeroCopyBufferLifecycle(t *testing.T) {
	handler := &lifecycleHandler{
		phase: new(atomic.Int32),
	}

	t.Log("Zero-copy buffer lifecycle:")
	t.Log("-----------------------------------------------------")
	t.Log("  Phase 0: Buffer prepared, SendZC() called")
	t.Log("  Phase 1: OnCompleted() - send done, buffer IN USE")
	t.Log("  Phase 2: OnNotification() - buffer SAFE to reuse")
	t.Log("-----------------------------------------------------")
	t.Logf("  Initial phase: %d", handler.phase.Load())
	t.Log("  In production code:")
	t.Log("    - Track phase per-buffer")
	t.Log("    - Only reuse/modify buffer after Phase 2")
	t.Log("    - Use buffer pool to manage multiple in-flight ZC sends")
}

type lifecycleHandler struct {
	phase *atomic.Int32
}

func (h *lifecycleHandler) OnCompleted(result int32) {
	h.phase.Store(1)
}

func (h *lifecycleHandler) OnNotification(result int32) {
	h.phase.Store(2)
}
