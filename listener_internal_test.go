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

const listenerInternalTestTimeout = 30 * time.Second

type tokenListenerHandler struct {
	ready       chan struct{}
	socketCalls atomic.Int32
	abortAfter  string
}

func newTokenListenerHandler() *tokenListenerHandler {
	return &tokenListenerHandler{ready: make(chan struct{})}
}

func (h *tokenListenerHandler) OnSocketCreated(iofd.FD) bool {
	h.socketCalls.Add(1)
	return h.abortAfter != "socket"
}

func (h *tokenListenerHandler) OnBound() bool {
	return true
}

func (h *tokenListenerHandler) OnListening() {
	select {
	case <-h.ready:
	default:
		close(h.ready)
	}
}

func (h *tokenListenerHandler) OnError(uint8, error) {
	select {
	case <-h.ready:
	default:
		close(h.ready)
	}
}

// TestListenerDecodeProducesValidStep verifies that after a SOCKET CQE,
// DecodeListenerCQE returns a valid step with correct fields.
func TestListenerDecodeProducesValidStep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping listener decode test in short mode (requires reliable io_uring)")
	}

	ring := newTestRing(t, func(opts *uring.Options) {
		opts.Entries = uring.EntriesSmall
	})

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)
	handler := newTokenListenerHandler()

	op, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer op.Close()

	if op.Ext() == nil {
		t.Fatal("listener op missing ExtSQE after submission")
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(listenerInternalTestTimeout)
	b := iox.Backoff{}
	listenerProgress := false

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		progressed := false
		for i := range n {
			step, ok := uring.DecodeListenerCQE(cqes[i])
			if !ok {
				continue
			}
			listenerProgress = true
			progressed = true

			if step.Op == uring.IORING_OP_SOCKET {
				if step.FD <= 0 {
					t.Fatalf("SOCKET step FD = %d, want > 0", step.FD)
				}
				if step.State != uring.ListenerStateSocket {
					t.Fatalf("SOCKET step State = %d, want ListenerStateSocket(%d)", step.State, uring.ListenerStateSocket)
				}
				if step.Handler == nil {
					t.Fatal("SOCKET step Handler is nil")
				}
				if step.Ext == nil {
					t.Fatal("SOCKET step Ext is nil")
				}
				if step.Err != nil {
					t.Fatalf("SOCKET step Err = %v, want nil", step.Err)
				}
				t.Logf("Decoded valid SOCKET step: fd=%d, state=%d", step.FD, step.State)
				return
			}

			// Drive non-SOCKET CQEs forward
			driveListenerCQE(t, ring, op, cqes[i])
		}

		if progressed {
			b.Reset()
			continue
		}
		b.Wait()
	}

	skipIfNoListenerProgress(t, listenerProgress)
	t.Fatal("listener did not produce SOCKET CQE before timeout")
}

// TestListenerAbortPreservesResources verifies that after aborting at socket
// stage, the caller still owns the ExtSQE and FD lifecycle.
func TestListenerAbortPreservesResources(t *testing.T) {
	ring := newTestRing(t)

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)
	handler := newTokenListenerHandler()
	handler.abortAfter = "socket"

	op, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}

	if op.Ext() == nil {
		t.Fatal("listener op missing ExtSQE after submission")
	}
	t.Cleanup(op.Close)

	done, listenerProgress := waitForListenerCondition(t, ring, op, listenerInternalTestTimeout, func() bool {
		return handler.socketCalls.Load() > 0
	})
	if !done {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Fatal("listener did not abort at socket stage before timeout")
	}

	// After abort, caller still owns the ExtSQE
	if op.Ext() == nil {
		t.Fatal("ExtSQE is nil after abort — caller should still own it")
	}
	// FD was set by driveListenerCQE via op.SetFD
	fd := op.FD()
	if fd < 0 {
		t.Fatalf("FD = %d after socket abort, want >= 0", fd)
	}
	t.Logf("After abort: ExtSQE valid, FD=%d", fd)
	op.Close()
}

// TestListenerOpCloseReleasesResources verifies that op.Close() closes the FD
// and releases the ExtSQE back to the pool.
func TestListenerOpCloseReleasesResources(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping listener close test in short mode (requires reliable io_uring)")
	}

	ring := newTestRing(t, func(opts *uring.Options) {
		opts.Entries = uring.EntriesSmall
	})

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)
	handler := newTokenListenerHandler()

	op, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}

	if op.Ext() == nil {
		t.Fatal("listener op missing ExtSQE after submission")
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			op.Close()
		}
	})

	done, listenerProgress := waitForListenerCondition(t, ring, op, listenerInternalTestTimeout, func() bool {
		select {
		case <-handler.ready:
			return true
		default:
			return false
		}
	})
	if !done {
		skipIfNoListenerProgress(t, listenerProgress)
		t.Fatal("listener did not reach ready state before timeout")
	}

	fd := op.FD()
	if fd < 0 {
		t.Fatalf("FD = %d before Close, want >= 0", fd)
	}

	op.Close()
	closed = true

	if op.Ext() != nil {
		t.Fatal("ExtSQE not nil after Close — should be released to pool")
	}
	if got := op.FD(); got >= 0 {
		t.Fatalf("FD = %d after Close, want < 0", got)
	}

	t.Logf("Close released resources: fd was %d, ext released to pool", fd)
}

func TestListenerOpCloseWhilePendingKeepsExtUntilCQEDrained(t *testing.T) {
	ring := newTestRing(t, func(opts *uring.Options) {
		opts.Entries = uring.EntriesSmall
	})

	pool := uring.NewContextPools(16)
	manager := uring.NewListenerManager(ring, pool)
	handler := newTokenListenerHandler()

	op, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, 128, handler)
	if err != nil {
		if errors.Is(err, iox.ErrWouldBlock) {
			t.Skip("context pool exhausted")
		}
		t.Fatalf("ListenTCP4: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			op.Close()
		}
	})

	if op.Ext() == nil {
		t.Fatal("listener op missing ExtSQE after submission")
	}

	op.Close()
	if op.Ext() == nil {
		t.Fatal("Close released ExtSQE while listener setup SQE was still pending")
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(listenerInternalTestTimeout)
	b := iox.Backoff{}
	listenerProgress := false

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		progressed := false
		for i := range n {
			step, ok := uring.DecodeListenerCQE(cqes[i])
			if !ok {
				continue
			}
			listenerProgress = true
			progressed = true

			if step.Err != nil {
				t.Fatalf("listener setup failed after early Close: %v", step.Err)
			}
			if step.Op != uring.IORING_OP_SOCKET {
				t.Fatalf("first listener step after early Close: got %d, want SOCKET", step.Op)
			}
			if step.Ext == nil {
				t.Fatal("decoded listener step lost ExtSQE after early Close")
			}

			op.SetFD(step.FD)
			if got := op.FD(); got >= 0 {
				t.Fatalf("late SetFD stored fd %d after Close", got)
			}
			assertListenerFDClosed(t, step.FD)

			op.Close()
			closed = true
			if op.Ext() != nil {
				t.Fatal("ExtSQE not released after pending listener CQE was drained")
			}
			return
		}

		if progressed {
			b.Reset()
			continue
		}
		b.Wait()
	}

	skipIfNoListenerProgress(t, listenerProgress)
	t.Fatal("listener CQE did not arrive after early Close")
}
