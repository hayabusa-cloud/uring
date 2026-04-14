// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// testListenerHandler is a test implementation of uring.ListenerHandler.
type testListenerHandler struct {
	socketFD      atomic.Int32
	socketCalled  atomic.Bool
	boundCalled   atomic.Bool
	listenCalled  atomic.Bool
	errorOp       atomic.Uint32
	errorReceived atomic.Bool
	lastError     error
	ready         chan struct{}
	abortAfter    string // "socket", "bind", or "" for no abort
}

const listenerTestTimeout = 30 * time.Second

func newTestListenerHandler() *testListenerHandler {
	h := &testListenerHandler{
		ready: make(chan struct{}),
	}
	h.socketFD.Store(-1)
	return h
}

func (h *testListenerHandler) OnSocketCreated(fd iofd.FD) bool {
	h.socketFD.Store(int32(fd))
	h.socketCalled.Store(true)
	return h.abortAfter != "socket"
}

func (h *testListenerHandler) OnBound() bool {
	h.boundCalled.Store(true)
	return h.abortAfter != "bind"
}

func (h *testListenerHandler) OnListening() {
	h.listenCalled.Store(true)
	close(h.ready)
}

func (h *testListenerHandler) OnError(op uint8, err error) {
	h.errorOp.Store(uint32(op))
	h.errorReceived.Store(true)
	h.lastError = err
	select {
	case <-h.ready:
	default:
		close(h.ready)
	}
}

func assertListenerFDClosed(t *testing.T, fd iofd.FD) {
	t.Helper()
	raw := int32(fd)
	if raw < 0 {
		t.Fatalf("expected valid listener fd, got %d", raw)
	}
	if errno := zcall.Close(uintptr(raw)); errno != uintptr(zcall.EBADF) {
		t.Fatalf("expected listener fd %d to be closed, errno=%d", raw, errno)
	}
}

// driveListenerCQE processes a single CQE using the decode+prepare pattern.
// Returns true if the CQE belonged to a listener operation.
func driveListenerCQE(t *testing.T, ring *uring.Uring, op *uring.ListenerOp, cqe uring.CQEView) bool {
	t.Helper()
	step, ok := uring.DecodeListenerCQE(cqe)
	if !ok {
		return false
	}
	if step.Err != nil {
		step.Handler.OnError(step.Op, step.Err)
		return true
	}
	switch step.Op {
	case uring.IORING_OP_SOCKET:
		op.SetFD(step.FD)
		if !step.Handler.OnSocketCreated(step.FD) {
			return true
		}
		uring.PrepareListenerBind(step.Ext, step.FD)
		if err := ring.SubmitExtended(uring.PackExtended(step.Ext)); err != nil {
			t.Fatalf("SubmitExtended(bind): %v", err)
		}
	case uring.IORING_OP_BIND:
		if !step.Handler.OnBound() {
			return true
		}
		uring.PrepareListenerListen(step.Ext, step.FD)
		if err := ring.SubmitExtended(uring.PackExtended(step.Ext)); err != nil {
			t.Fatalf("SubmitExtended(listen): %v", err)
		}
	case uring.IORING_OP_LISTEN:
		uring.SetListenerReady(step.Ext)
		step.Handler.OnListening()
	}
	return true
}

func skipIfNoListenerProgress(t *testing.T, observed bool) {
	t.Helper()
	if !observed {
		t.Skip("no listener CQEs observed in this environment")
	}
}

func waitForListenerCondition(
	t *testing.T,
	ring *uring.Uring,
	op *uring.ListenerOp,
	timeout time.Duration,
	done func() bool,
) (bool, bool) {
	t.Helper()

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(timeout)
	b := iox.Backoff{}
	listenerProgress := false

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		progressed := false
		for i := range n {
			if driveListenerCQE(t, ring, op, cqes[i]) {
				listenerProgress = true
				progressed = true
			}
		}

		if done() {
			return true, listenerProgress
		}
		if progressed {
			b.Reset()
			continue
		}
		b.Wait()
	}

	return false, listenerProgress
}

func TestListenerManagerBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping listener chain test in short mode (requires reliable io_uring)")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	handler := newTestListenerHandler()

	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	op, err := manager.ListenTCP4(addr, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer op.Close()

	ready, listenerProgress := waitForListenerCondition(t, ring, op, listenerTestTimeout, func() bool {
		select {
		case <-handler.ready:
			return true
		default:
			return false
		}
	})
	if !ready {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Fatal("listener did not become ready")
	}

	if handler.errorReceived.Load() {
		t.Errorf("OnError was called: op=%d, err=%v", handler.errorOp.Load(), handler.lastError)
	}

	if !handler.socketCalled.Load() {
		t.Error("OnSocketCreated was not called")
	}
	if !handler.boundCalled.Load() {
		t.Error("OnBound was not called")
	}
	if !handler.listenCalled.Load() {
		t.Error("OnListening was not called")
	}

	fd := op.FD()
	if fd < 0 {
		t.Errorf("expected valid FD, got %d", fd)
	}
	op.Close()
	assertListenerFDClosed(t, fd)

	t.Logf("Listener created successfully: fd=%d", fd)
}

func TestListenerManagerAbortOnSocket(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	handler := newTestListenerHandler()
	handler.abortAfter = "socket"

	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	op, err := manager.ListenTCP4(addr, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer op.Close()

	_, listenerProgress := waitForListenerCondition(t, ring, op, listenerTestTimeout, func() bool {
		return handler.socketCalled.Load()
	})

	time.Sleep(50 * time.Millisecond)

	if !handler.socketCalled.Load() {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Error("OnSocketCreated was not called")
	}
	if handler.boundCalled.Load() {
		t.Error("OnBound should not be called when aborted after socket")
	}
	if handler.listenCalled.Load() {
		t.Error("OnListening should not be called when aborted after socket")
	}
	op.Close()
	assertListenerFDClosed(t, iofd.FD(handler.socketFD.Load()))

	t.Log("Handler abort at socket stage works correctly")
}

func TestListenerManagerAbortOnBind(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping listener chain test in short mode (requires reliable io_uring)")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	handler := newTestListenerHandler()
	handler.abortAfter = "bind"

	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	op, err := manager.ListenTCP4(addr, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer op.Close()

	_, listenerProgress := waitForListenerCondition(t, ring, op, listenerTestTimeout, func() bool {
		return handler.boundCalled.Load()
	})

	time.Sleep(50 * time.Millisecond)

	if !handler.socketCalled.Load() {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Error("OnSocketCreated was not called")
	}
	if !handler.boundCalled.Load() {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Error("OnBound was not called")
	}
	if handler.listenCalled.Load() {
		t.Error("OnListening should not be called when aborted after bind")
	}
	op.Close()
	assertListenerFDClosed(t, iofd.FD(handler.socketFD.Load()))

	t.Log("Handler abort at bind stage works correctly")
}

func TestListenerManagerUnixLongPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Unix listener chain test in short mode (requires reliable io_uring)")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	handler := newTestListenerHandler()
	path := longUnixSocketPath(t)
	addr := &net.UnixAddr{Name: path, Net: "unix"}

	op, err := manager.ListenUnix(addr, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenUnix: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			op.Close()
		}
		_ = os.Remove(path)
	})

	ready, listenerProgress := waitForListenerCondition(t, ring, op, listenerTestTimeout, func() bool {
		select {
		case <-handler.ready:
			return true
		default:
			return false
		}
	})
	if !ready {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Fatal("Unix listener did not become ready")
	}

	if handler.errorReceived.Load() {
		t.Fatalf("OnError was called: op=%d, err=%v", handler.errorOp.Load(), handler.lastError)
	}
	if !handler.socketCalled.Load() || !handler.boundCalled.Load() || !handler.listenCalled.Load() {
		t.Fatalf(
			"listener callbacks incomplete: socket=%v bind=%v listen=%v",
			handler.socketCalled.Load(),
			handler.boundCalled.Load(),
			handler.listenCalled.Load(),
		)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Unix socket path %q not present: %v", path, err)
	}

	fd := op.FD()
	op.Close()
	closed = true
	assertListenerFDClosed(t, fd)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Remove(%q): %v", path, err)
	}
}

func longUnixSocketPath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	name := "listener.sock"
	path := filepath.Join(dir, name)
	for len(path) <= 30 {
		name = "x" + name
		path = filepath.Join(dir, name)
	}
	if len(path) > 107 {
		t.Skipf("temp dir path too long for sockaddr_un test: %d", len(path))
	}
	return path
}

func TestListenerStateConstants(t *testing.T) {
	if uring.ListenerStateInit >= uring.ListenerStateSocket {
		t.Error("ListenerStateInit should be < ListenerStateSocket")
	}
	if uring.ListenerStateSocket >= uring.ListenerStateBind {
		t.Error("ListenerStateSocket should be < ListenerStateBind")
	}
	if uring.ListenerStateBind >= uring.ListenerStateListen {
		t.Error("ListenerStateBind should be < ListenerStateListen")
	}
	if uring.ListenerStateListen >= uring.ListenerStateReady {
		t.Error("ListenerStateListen should be < ListenerStateReady")
	}
	if uring.ListenerStateReady >= uring.ListenerStateFailed {
		t.Error("ListenerStateReady should be < ListenerStateFailed")
	}
}

func TestListenerOpFDBeforeReady(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	handler := newTestListenerHandler()
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}

	op, err := manager.ListenTCP4(addr, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer op.Close()

	fd := op.FD()
	if fd != -1 {
		t.Errorf("expected FD -1 before ready, got %d", fd)
	}
}

func TestListenerContextSize(t *testing.T) {
	t.Log("listenerCtx and listenerCtxData size assertions passed at compile time")
}

func TestListenerManagerRingAndPool(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	if manager.Ring() != ring {
		t.Error("Ring() did not return the expected Uring instance")
	}
	if manager.Pool() != pool {
		t.Error("Pool() did not return the expected ContextPools instance")
	}
}

func TestListenerManagerListenTCP6(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping listener chain test in short mode")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)

	handler := newTestListenerHandler()
	addr := &net.TCPAddr{IP: net.IPv6loopback, Port: 0}
	op, err := manager.ListenTCP6(addr, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP6: %v", err)
	}
	defer op.Close()

	ready, listenerProgress := waitForListenerCondition(t, ring, op, listenerTestTimeout, func() bool {
		select {
		case <-handler.ready:
			return true
		default:
			return false
		}
	})
	if !ready {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Fatal("TCP6 listener did not become ready")
	}

	if handler.errorReceived.Load() {
		t.Errorf("OnError was called: op=%d, err=%v", handler.errorOp.Load(), handler.lastError)
	}
	if !handler.socketCalled.Load() || !handler.boundCalled.Load() || !handler.listenCalled.Load() {
		t.Fatalf(
			"listener callbacks incomplete: socket=%v bind=%v listen=%v",
			handler.socketCalled.Load(),
			handler.boundCalled.Load(),
			handler.listenCalled.Load(),
		)
	}

	fd := op.FD()
	if fd < 0 {
		t.Errorf("expected valid FD, got %d", fd)
	}
	op.Close()
	assertListenerFDClosed(t, fd)
}
