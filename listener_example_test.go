// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"net"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// ListenerManager submits SOCKET; route each CQE to HandleCQE for automatic setup:
//
//	SOCKET -> OnSocketCreated -> BIND -> OnBound -> LISTEN -> OnListening
//
// All callbacks run on this caller. After accepting the test clients, cancel
// the subscription and dispatch both terminal and cancellation completions
// before closing the listener and ring.
func TestListenerWithAccept(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	manager := uring.NewListenerManager(ring, uring.NewContextPools(8))
	listenerHandler := &exampleListenerHandler{}
	listener, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, 128, listenerHandler)
	if err != nil {
		t.Fatalf("ListenTCP4: %v", err)
	}
	t.Cleanup(listener.Close)
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	retry := false
	deadline := time.Now().Add(3 * time.Second)
	for !listenerHandler.ready && listenerHandler.err == nil {
		// A full SQ leaves the next stage prepared. Retry only its admission;
		// replaying the preceding CQE would repeat the callback and transition.
		if retry {
			err := ring.SubmitExtended(uring.PackExtended(listener.Ext()))
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				t.Fatalf("SubmitExtended: %v", err)
			}
			retry = errors.Is(err, iox.ErrWouldBlock)
		}
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			if time.Now().After(deadline) {
				t.Fatal("listener setup timed out")
			}
			backoff.Wait()
			continue
		}
		backoff.Reset()
		ok, err := listener.HandleCQE(cqes[0])
		if !ok {
			t.Fatal("unexpected listener completion")
		}
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("HandleCQE: %v", err)
		}
		retry = errors.Is(err, iox.ErrWouldBlock)
	}
	if listenerHandler.err != nil {
		t.Fatalf("listener opcode %d: %v", listenerHandler.op, listenerHandler.err)
	}

	const clients = 3
	addr := actualTCP4AddrForFD(t, listener.FD())
	for range clients {
		conn, err := net.DialTimeout("tcp4", addr.String(), 3*time.Second)
		if err != nil {
			t.Fatalf("DialTimeout: %v", err)
		}
		t.Cleanup(func() {
			if err := conn.Close(); err != nil {
				t.Errorf("client Close: %v", err)
			}
		})
	}
	accepted, stops := 0, 0
	handler := uring.NewMultishotSubscriber().
		OnStep(func(step uring.MultishotStep) uring.MultishotAction {
			if step.Err != nil {
				if !step.Cancelled {
					t.Errorf("accept: %v", step.Err)
				}
				return uring.MultishotContinue
			}
			// Every successful accept owns an FD, even if cancellation is underway.
			if errno := zcall.Close(uintptr(step.CQE.Res)); errno != 0 {
				t.Errorf("accepted FD close: %v", zcall.Errno(errno))
			}
			accepted++
			return uring.MultishotContinue
		}).
		OnStop(func(err error, cancelled bool) {
			stops++
			if !cancelled {
				t.Errorf("expected cancellation: %v", err)
			}
		})
	sub, err := listener.AcceptMultishot(handler)
	if err != nil {
		t.Fatalf("AcceptMultishot: %v", err)
	}
	cancelSubmitted, cancelDone := false, false
	deadline = time.Now().Add(3 * time.Second)
	for stops == 0 || (cancelSubmitted && !cancelDone) {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			if time.Now().After(deadline) {
				t.Fatal("accept or cancellation timed out")
			}
			backoff.Wait()
			continue
		}
		backoff.Reset()
		cqe := cqes[0]
		// Inspect borrowed context before HandleCQE can return it to the pool.
		if cqe.Op() == uring.IORING_OP_ASYNC_CANCEL {
			if cancelDone || cqe.Res != 0 {
				t.Fatalf("unexpected cancellation: %+v", cqe)
			}
			cancelDone = true
		}
		if !sub.HandleCQE(cqe) {
			t.Fatal("unhandled accept or cancellation completion")
		}
		if accepted == clients && sub.Active() {
			if err := sub.Cancel(); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			cancelSubmitted = true
		}
	}
	if accepted != clients || stops != 1 || !cancelSubmitted || !cancelDone || sub.State() != uring.SubscriptionStopped {
		t.Fatalf("accepted=%d stops=%d cancel=%v/%v state=%v", accepted, stops, cancelSubmitted, cancelDone, sub.State())
	}
	// All setup, accept and cancellation CQEs are drained before cleanup.
}

// Callbacks share the completion loop's owner; plain fields record setup results.
type exampleListenerHandler struct {
	ready bool
	op    uint8
	err   error
}

func (h *exampleListenerHandler) OnSocketCreated(iofd.FD) bool { return true }
func (h *exampleListenerHandler) OnBound() bool                { return true }
func (h *exampleListenerHandler) OnListening()                 { h.ready = true }
func (h *exampleListenerHandler) OnError(op uint8, err error)  { h.op, h.err = op, err }

func actualTCP4AddrForFD(t *testing.T, fd iofd.FD) *net.TCPAddr {
	t.Helper()
	var raw [16]byte
	addrLen := uint32(len(raw))
	if errno := zcall.Getsockname(uintptr(fd), unsafe.Pointer(&raw), unsafe.Pointer(&addrLen)); errno != 0 {
		t.Fatalf("Getsockname: %v", zcall.Errno(errno))
	}
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(raw[2])<<8 | int(raw[3])}
}
