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
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

const listenerExampleTimeout = 30 * time.Second

// TestListenerWithAccept demonstrates combining ListenerManager with
// multishot accept for a complete server setup.
//
// Architecture:
//   - ListenerManager creates the listener socket via decode+prepare
//   - After OnListening, start multishot accept
//   - Single SQE submission handles multiple incoming connections
func TestListenerWithAccept(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping listener with accept test in short mode (requires reliable networking)")
	}
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesMedium
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(32)
	manager := uring.NewListenerManager(ring, pool)

	listenerHandler := &exampleListenerHandler{
		ready:  make(chan struct{}),
		failed: make(chan struct{}),
	}

	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	listenerOp, err := manager.ListenTCP4(addr, 128, listenerHandler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer listenerOp.Close()

	ready, listenerProgress := waitForListenerCondition(t, ring, listenerOp, listenerExampleTimeout, func() bool {
		select {
		case <-listenerHandler.ready:
			return true
		case <-listenerHandler.failed:
			t.Fatalf("listener setup failed at op=%d", listenerHandler.errorOp.Load())
			return false
		default:
			return false
		}
	})
	if !ready {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Fatal("listener did not become ready")
	}
	t.Logf("Listener ready: fd=%d", listenerOp.FD())
	addr = actualTCP4AddrForFD(t, listenerOp.FD())

	// Phase 2: Start multishot accept via listenerOp
	acceptHandler := &exampleAcceptHandler{
		accepted: make(chan int32, 10),
		stopped:  make(chan struct{}),
	}

	acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted for accept")
		}
		t.Fatalf("AcceptMultishot: %v", err)
	}
	t.Log("Multishot accept started")

	// Phase 3: Connect clients
	const numClients = 3
	for i := 0; i < numClients; i++ {
		go func(idx int) {
			conn, err := net.Dial("tcp", addr.String())
			if err != nil {
				return
			}
			conn.Write([]byte("hello"))
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		}(i)
	}

	// Process accepts
	acceptCount := 0
	cqes := make([]uring.CQEView, 32)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	for acceptCount < numClients && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			if dispatchSimplifiedMultishotCQE(acceptHandler, cqes[i]) {
				select {
				case fd := <-acceptHandler.accepted:
					acceptCount++
					t.Logf("Accepted connection %d: fd=%d", acceptCount, fd)
					zcall.Close(uintptr(fd))
				default:
				}
			}
		}
		b.Wait()
	}

	acceptSub.Cancel()
	t.Logf("Accepted %d connections via multishot", acceptCount)
}

// exampleListenerHandler implements ListenerHandler for examples.
type exampleListenerHandler struct {
	socketFD     atomic.Int32
	socketCalled atomic.Bool
	boundCalled  atomic.Bool
	listenCalled atomic.Bool
	errorOp      atomic.Uint32
	ready        chan struct{}
	failed       chan struct{}
}

func (h *exampleListenerHandler) OnSocketCreated(fd iofd.FD) bool {
	h.socketFD.Store(int32(fd))
	h.socketCalled.Store(true)
	return true
}

func (h *exampleListenerHandler) OnBound() bool {
	h.boundCalled.Store(true)
	return true
}

func (h *exampleListenerHandler) OnListening() {
	h.listenCalled.Store(true)
	close(h.ready)
}

func (h *exampleListenerHandler) OnError(op uint8, err error) {
	h.errorOp.Store(uint32(op))
	select {
	case <-h.failed:
	default:
		close(h.failed)
	}
}

func actualTCP4AddrForFD(t *testing.T, fd iofd.FD) *net.TCPAddr {
	t.Helper()

	var raw [16]byte
	addrLen := uint32(len(raw))
	errno := zcall.Getsockname(uintptr(fd), unsafe.Pointer(&raw[0]), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}

	port := int(raw[2])<<8 | int(raw[3])
	return &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: port,
	}
}

// exampleAcceptHandler implements MultishotHandler for accept.
type exampleAcceptHandler struct {
	accepted chan int32
	stopped  chan struct{}
}

func (h *exampleAcceptHandler) OnMultishotStep(step uring.MultishotStep) uring.MultishotAction {
	if step.Err == nil && step.CQE.Res >= 0 {
		select {
		case h.accepted <- step.CQE.Res:
		default:
		}
		return uring.MultishotContinue
	}
	return uring.MultishotStop
}

func (h *exampleAcceptHandler) OnMultishotStop(error, bool) {
	close(h.stopped)
}
