// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func multishotListener(t *testing.T) (uring.SQEContext, string) {
	t.Helper()
	ln, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	file, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return uring.ForFD(int32(file.Fd())), ln.Addr().String()
}

// One cancellation was admitted for this accept on a listener with no waiting
// peers. Its terminal and cancellation CQEs can arrive in either order.
func checkAcceptCancellation(t *testing.T, ring *uring.Uring, sub *uring.MultishotSubscription) {
	t.Helper()
	var terminal, control bool
	for _, cqe := range waitForCQEs(t, ring, 2) {
		switch cqe.Op() {
		case uring.IORING_OP_ACCEPT:
			terminal = !cqe.HasMore() && errors.Is(cqe.Err(), uring.ErrCanceled)
		case uring.IORING_OP_ASYNC_CANCEL:
			control = cqe.Res == 0
		}
		if !sub.HandleCQE(cqe) {
			t.Fatal("unhandled completion")
		}
	}
	if !terminal || !control || sub.Active() {
		t.Fatalf("terminal=%v control=%v state=%v", terminal, control, sub.State())
	}
}

func TestMultishotSubscriptionCreate(t *testing.T) {
	ring := newTestRing(t)
	ctx, _ := multishotListener(t)
	sub, err := ring.AcceptMultishot(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.State() != uring.SubscriptionActive {
		t.Fatal("expected an active subscription")
	}
	if err := sub.Cancel(); err != nil {
		t.Fatal(err)
	}
	checkAcceptCancellation(t, ring, sub)
}

func TestMultishotAcceptAfterUnsubscribe(t *testing.T) {
	ring := newTestRing(t)
	ctx, addr := multishotListener(t)
	handler := uring.NewMultishotSubscriber().
		OnStep(func(uring.MultishotStep) uring.MultishotAction {
			t.Error("step callback after unsubscribe")
			return uring.MultishotContinue
		}).
		OnStop(func(error, bool) { t.Error("stop callback after unsubscribe") })
	sub, err := ring.AcceptMultishot(ctx, handler)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := net.DialTimeout("tcp4", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	// Withdraw on the same owner, after reaping the success and before dispatch.
	accepted := waitForCQEs(t, ring, 1)[0]
	if accepted.Op() != uring.IORING_OP_ACCEPT || accepted.Res < 0 || !accepted.HasMore() {
		t.Fatalf("expected live accept, got op=%d res=%d flags=%#x", accepted.Op(), accepted.Res, accepted.Flags)
	}
	fd := iofd.NewFD(int(accepted.Res))
	sub.Unsubscribe()
	if !sub.HandleCQE(accepted) {
		t.Fatal("unhandled accept")
	}
	if err := fd.Close(); err != nil {
		t.Fatal(err)
	}
	checkAcceptCancellation(t, ring, sub)

	if err := peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var buf [1]byte
	if n, err := peer.Read(buf[:]); n != 0 || err != io.EOF {
		t.Fatalf("peer read after closing accepted fd = (%d, %v), want (0, EOF)", n, err)
	}
}

// TestSubmitAcceptMultishotCancelTerminates tests raw multishot accept cancellation CQEs.
func TestSubmitAcceptMultishotCancelTerminates(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, fd)
	if err := ring.SubmitAcceptMultishot(acceptCtx); err != nil {
		// Multishot accept might not be supported on all kernels
		t.Logf("SubmitAcceptMultishot returned error (may be unsupported): %v", err)
		return
	}

	clientConn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("DialTimeout: %v", err)
	}
	defer clientConn.Close()

	acceptDeadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	accepted := 0
	hasMoreSeen := false
	cqes := make([]uring.CQEView, 16)

	for accepted == 0 && time.Now().Before(acceptDeadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() != uring.IORING_OP_ACCEPT {
				continue
			}
			if cqe.Res < 0 {
				t.Fatalf("accept CQE failed: %d", cqe.Res)
			}
			accepted++
			if cqe.HasMore() {
				hasMoreSeen = true
			}
			closeTestFD(iofd.FD(cqe.Res))
		}
		if accepted == 0 {
			b.Wait()
		}
	}

	if accepted > 0 && !hasMoreSeen {
		t.Fatal("accept multishot CQE did not advertise continuation")
	}
	if accepted == 0 {
		t.Skip("SubmitAcceptMultishot completion not received - timing-sensitive")
	}

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, fd)
	if err := ring.AsyncCancel(cancelCtx, acceptCtx.Raw()); err != nil {
		t.Fatalf("AsyncCancel: %v", err)
	}

	terminalAccept := false
	stopDeadline := time.Now().Add(3 * time.Second)
	b.Reset()
	for time.Now().Before(stopDeadline) && !terminalAccept {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			switch cqe.Op() {
			case uring.IORING_OP_ACCEPT:
				if cqe.Res >= 0 {
					accepted++
					if cqe.HasMore() {
						hasMoreSeen = true
					}
					closeTestFD(iofd.FD(cqe.Res))
				} else if cqe.Res != -int32(uring.ECANCELED) {
					t.Fatalf("terminal accept CQE failed: %d", cqe.Res)
				}
				if !cqe.HasMore() {
					terminalAccept = true
				}
			case uring.IORING_OP_ASYNC_CANCEL:
				// AsyncCancel completion is expected alongside the terminal accept CQE.
			}
		}
		if !terminalAccept {
			b.Wait()
		}
	}

	if !terminalAccept {
		t.Fatal("SubmitAcceptMultishot cancel termination not received after successful accept")
	}
}

// TestSubmitReceiveMultishot tests raw multishot receive CQEs.
func TestSubmitReceiveMultishot(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("no registered buffers available")
	}

	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)

	pollFD := fds[0]
	recvCtx := uring.ForFD(int32(fds[0]))
	if err := ring.SubmitReceiveMultishot(
		recvCtx,
		&pollFD,
		nil,
		uring.WithIOPrio(uring.IORING_RECVSEND_POLL_FIRST),
	); err != nil {
		// May not be supported without buffer ring
		t.Logf("SubmitReceiveMultishot returned error (may require buffer ring): %v", err)
		return
	}

	cqes := make([]uring.CQEView, 16)
	// Prime Wait: no data has been written yet, so any recv CQE here
	// should carry Res==0 (multishot armed, no payload).
	n, err := ring.Wait(cqes)
	if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("prime Wait: %v", err)
	}
	for i := 0; i < n; i++ {
		cqe := cqes[i]
		if cqe.Op() != uring.IORING_OP_RECV {
			continue
		}
		if cqe.Res < 0 {
			t.Fatalf("prime Wait recv CQE failed: %d", cqe.Res)
		}
		if cqe.Res > 0 {
			t.Fatalf("prime Wait returned unexpected recv CQE: bufID=%d bytes=%d", cqe.BufID(), cqe.Res)
		}
	}

	payload := []byte("test multishot recv")
	if n, errno := writeTestFD(fds[1], payload); errno != 0 {
		t.Fatalf("Write payload: %v", zcall.Errno(errno))
	} else if n != len(payload) {
		t.Fatalf("Write payload bytes: got %d, want %d", n, len(payload))
	}

	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	recvCount := 0
	totalBytes := 0
	hasMoreSeen := false

	for time.Now().Before(deadline) && totalBytes < len(payload) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}

		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() != uring.IORING_OP_RECV {
				continue
			}
			if cqe.Res < 0 {
				t.Fatalf("recv CQE failed: %d", cqe.Res)
			}
			if cqe.Res == 0 {
				continue
			}
			if !cqe.HasBuffer() {
				t.Fatal("recv CQE missing buffer-selection metadata")
			}
			recvCount++
			totalBytes += int(cqe.Res)
			if cqe.HasMore() {
				hasMoreSeen = true
			}
		}
		if totalBytes < len(payload) {
			b.Wait()
		}
	}

	if recvCount == 0 {
		t.Fatal("received 0 recv CQEs, want at least 1")
	}
	if totalBytes != len(payload) {
		t.Fatalf("received %d bytes, want %d", totalBytes, len(payload))
	}
	if !hasMoreSeen {
		t.Fatal("recv multishot CQE did not advertise continuation")
	}

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, int32(fds[0]))
	if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
		t.Fatalf("AsyncCancelFD: %v", err)
	}

	terminalRecv := false
	stopDeadline := time.Now().Add(3 * time.Second)
	b.Reset()
	for time.Now().Before(stopDeadline) && !terminalRecv {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			switch cqe.Op() {
			case uring.IORING_OP_RECV:
				if cqe.Res < 0 && cqe.Res != -int32(uring.ECANCELED) {
					t.Fatalf("terminal recv CQE failed: %d", cqe.Res)
				}
				if !cqe.HasMore() {
					terminalRecv = true
				}
			case uring.IORING_OP_ASYNC_CANCEL:
				// AsyncCancelFD completion is expected alongside the terminal recv CQE.
			}
		}
		if !terminalRecv {
			b.Wait()
		}
	}

	if !terminalRecv {
		t.Fatal("multishot recv did not terminate after cancel")
	}
}

func TestMultishotHandlerStopRequest(t *testing.T) {
	ring := newTestRing(t)
	ctx, addr := multishotListener(t)
	var successes, stops int
	handler := uring.NewMultishotSubscriber().
		OnStep(func(step uring.MultishotStep) uring.MultishotAction {
			if step.Err == nil {
				successes++
				fd := iofd.NewFD(int(step.CQE.Res))
				if err := fd.Close(); err != nil {
					t.Fatal(err)
				}
			} else if !step.Cancelled {
				t.Errorf("accept: %v", step.Err)
			}
			return uring.MultishotStop
		}).
		OnStop(func(err error, cancelled bool) {
			stops++
			if !cancelled || !errors.Is(err, uring.ErrCanceled) {
				t.Errorf("stop: err=%v cancelled=%v", err, cancelled)
			}
		})
	sub, err := ring.AcceptMultishot(ctx, handler)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := net.DialTimeout("tcp4", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	if !sub.HandleCQE(waitForCQEs(t, ring, 1)[0]) || sub.State() != uring.SubscriptionCancelling {
		t.Fatalf("handler stop left state %v", sub.State())
	}
	checkAcceptCancellation(t, ring, sub)
	if successes != 1 || stops != 1 {
		t.Fatalf("successful steps=%d stops=%d, want one of each", successes, stops)
	}
}

func TestMultishotPoolExhaustion(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) { opt.Entries = 2 })
	ctx, _ := multishotListener(t)
	var subs [2]*uring.MultishotSubscription
	for i := range subs {
		sub, err := ring.AcceptMultishot(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		subs[i] = sub
	}
	// Submit both accepts so only context capacity can limit a third request.
	var cqes [1]uring.CQEView
	if n, err := ring.Wait(cqes[:]); n != 0 || (err != nil && !iox.IsWouldBlock(err)) {
		t.Fatalf("unexpected listener activity: completions=%d err=%v", n, err)
	}
	if _, err := ring.AcceptMultishot(ctx, nil); !iox.IsWouldBlock(err) {
		t.Fatalf("expected exhausted context pool, got %v", err)
	}
	for _, sub := range subs {
		if err := sub.Cancel(); err != nil {
			t.Fatal(err)
		}
		checkAcceptCancellation(t, ring, sub)
	}
}

func TestMultishotCancelIdempotent(t *testing.T) {
	ring := newTestRing(t)
	ctx, _ := multishotListener(t)
	sub, err := ring.AcceptMultishot(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if err := sub.Cancel(); err != nil {
			t.Fatal(err)
		}
	}
	checkAcceptCancellation(t, ring, sub)
	if err := sub.Cancel(); err != nil {
		t.Fatalf("Cancel after stop: %v", err)
	}
}

// TestSubscriptionState tests the SubscriptionState constants.
func TestSubscriptionState(t *testing.T) {
	if uring.SubscriptionActive >= uring.SubscriptionCancelling {
		t.Error("SubscriptionActive should be less than SubscriptionCancelling")
	}
	if uring.SubscriptionCancelling >= uring.SubscriptionStopped {
		t.Error("SubscriptionCancelling should be less than SubscriptionStopped")
	}
}

// TestMultishotStressAccept stress tests multishot accept with many connections
// using raw SubmitAcceptMultishot and manual CQE processing.
func TestMultishotStressAccept(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesLarge
			opts.NotifySucceed = true
			opts.MultiIssuers = true
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()
	fd := int32(lnFile.Fd())

	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, fd)
	if err := ring.SubmitAcceptMultishot(acceptCtx); err != nil {
		t.Logf("SubmitAcceptMultishot: %v (may be unsupported)", err)
		return
	}

	const numConnections = 50
	go func() {
		for i := 0; i < numConnections; i++ {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				continue
			}
			conn.Close()
		}
	}()

	cqes := make([]uring.CQEView, 64)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	accepted := 0

	for time.Now().Before(deadline) && accepted < numConnections {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() != uring.IORING_OP_ACCEPT {
				continue
			}
			if cqe.Res >= 0 {
				accepted++
				closeTestFD(iofd.FD(cqe.Res))
			}
		}
		if accepted < numConnections {
			b.Wait()
		}
	}

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, fd)
	if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
		t.Fatalf("AsyncCancelFD: %v", err)
	}

	b.Reset()
	terminalSeen := false
	stopDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(stopDeadline) && !terminalSeen {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			if cqe.Op() == uring.IORING_OP_ACCEPT {
				if cqe.Res >= 0 {
					accepted++
					closeTestFD(iofd.FD(cqe.Res))
				}
				if !cqe.HasMore() {
					terminalSeen = true
				}
			}
		}
		if !terminalSeen {
			b.Wait()
		}
	}

	t.Logf("Accepted %d connections (target: %d)", accepted, numConnections)
	if accepted < numConnections/2 {
		t.Fatalf("accepted %d connections, want at least %d", accepted, numConnections/2)
	}
}
