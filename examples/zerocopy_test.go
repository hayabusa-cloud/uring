// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime"
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

// TestZCTrackerBasic demonstrates the ZCTracker for managing zero-copy sends
// through the notification path and terminal no-notification fallback.
//
// Architecture:
//   - The notification path pairs an operation CQE with MORE and a notification
//     CQE; the tracker releases only after the pair is complete
//   - A terminal operation CQE without MORE is release-ready; it is the
//     no-notification fallback only when no notification CQE was observed first
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
	// Release can follow one terminal data CQE or a data/notification pair.

	for len(handler.notificationCh) == 0 && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			b.Wait()
			continue
		}
		b.Reset()
		for i := range n {
			if !tracker.HandleCQE(cqes[i]) {
				t.Fatal("unhandled zero-copy completion")
			}
		}
	}

	runtime.KeepAlive(testData)
	if len(handler.completedCh) != 1 || len(handler.notificationCh) != 1 {
		t.Fatal("missing zero-copy completion or release callback")
	}
	result, releasedResult := <-handler.completedCh, <-handler.notificationCh
	if result < 0 || result != releasedResult {
		t.Fatalf("completion=%d release=%d", result, releasedResult)
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
//   - MulticastZeroCopy chooses copy or zero-copy by payload size and fanout
//   - Notification-path zero-copy can emit one data CQE plus one notification
//     CQE per target; terminal data CQEs without MORE use the fallback path
//
// Use cases: chat servers, pub/sub systems, real-time data distribution.
func TestMulticastZeroCopy(t *testing.T) {
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 20
			opts.NotifySucceed = true // Observe ordinary sends used for any unsent suffix.
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
	ctx := uring.ForFD(0)

	accepted, submitErr := ring.MulticastZeroCopy(ctx, targets, bufIndex, 0, messageSize)
	// Even a partial submission leaves the accepted prefix borrowing the buffer.
	var states [numClients]struct {
		data, more, notification, released bool
		result                             int32
	}
	remaining := accepted
	cqes := make([]uring.CQEView, 32)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	for remaining > 0 && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			b.Wait()
			continue
		}
		b.Reset()
		for _, cqe := range cqes[:n] {
			if cqe.Op() != uring.IORING_OP_SEND_ZC && cqe.Op() != uring.IORING_OP_SEND {
				t.Fatalf("unexpected opcode %d", cqe.Op())
			}
			i := 0
			for i < accepted && serverFDs[i] != int32(cqe.FD()) {
				i++
			}
			if i == accepted {
				t.Fatalf("unknown target %d", cqe.FD())
			}
			state := &states[i]
			if cqe.IsNotification() {
				if state.notification {
					t.Fatal("duplicate notification")
				}
				state.notification = true
			} else {
				if state.data {
					t.Fatal("duplicate data completion")
				}
				state.data, state.more, state.result = true, cqe.HasMore(), cqe.Res
			}
			if !state.released && state.data && (!state.more || state.notification) {
				state.released = true
				remaining--
			}
		}
	}
	if remaining != 0 {
		t.Fatalf("%d targets still borrow the registered buffer", remaining)
	}
	if submitErr != nil || accepted != numClients {
		t.Fatalf("accepted=%d err=%v", accepted, submitErr)
	}
	for i, conn := range clientConns {
		sent := int(states[i].result)
		if sent < 0 || sent > messageSize {
			t.Fatalf("target %d: result=%d", i, sent)
		}
		// Admission counts targets, not bytes. Resume this target's suffix only.
		for sent < messageSize {
			if err := ring.SendFD(uring.ForFD(serverFDs[i]), iofd.FD(serverFDs[i]), buf[sent:messageSize]); err != nil {
				t.Fatal(err)
			}
			cqe, ok := waitForOp(t, ring, uring.IORING_OP_SEND, time.Second)
			if !ok || cqe.Res <= 0 || int(cqe.Res) > messageSize-sent {
				t.Fatalf("suffix send: received=%v result=%d", ok, cqe.Res)
			}
			sent += int(cqe.Res)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		received := make([]byte, messageSize)
		if _, err := io.ReadFull(conn, received); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(received, message) {
			t.Fatalf("target %d: payload mismatch", i)
		}
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
