// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
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

func TestSubmitAcceptMultishotCancelTerminates(t *testing.T) {
	ring := newTestRing(t)
	ctx, addr := multishotListener(t)
	if err := ring.SubmitAcceptMultishot(ctx); err != nil {
		t.Fatal(err)
	}
	peer, err := net.DialTimeout("tcp4", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	accept := ctx.WithOp(uring.IORING_OP_ACCEPT)
	cqe := waitForCQEs(t, ring, 1)[0]
	if cqe.Context() != accept || cqe.Res < 0 || !cqe.HasMore() {
		t.Fatalf("expected live accept, got %+v", cqe)
	}
	fd := iofd.FD(cqe.Res)
	if err := fd.Close(); err != nil {
		t.Fatal(err)
	}

	cancel := ctx.WithOp(uring.IORING_OP_ASYNC_CANCEL)
	if err := ring.AsyncCancel(cancel, accept.Raw()); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{accept: -int32(zcall.ECANCELED), cancel: 0})
}

func TestSubmitReceiveMultishot(t *testing.T) {
	const slots = 2
	ring := newTestRing(t, func(o *uring.Options) { o.ReadBufferNum = slots })
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	ctx := uring.ForFD(int32(fds[0]))
	if err := ring.SubmitReceiveMultishot(ctx, &fds[0], nil, uring.WithIOPrio(uring.IORING_RECVSEND_POLL_FIRST)); err != nil {
		t.Fatal(err)
	}
	// Arming an idle stream produces no completion.
	var idle [1]uring.CQEView
	if n, err := ring.Wait(idle[:]); n != 0 || (err != nil && !iox.IsWouldBlock(err)) {
		t.Fatalf("idle receive: completions=%d err=%v", n, err)
	}

	group := uint16(ring.ReadBufferGidOffset)
	recv := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_BUFFER_SELECT, group, int32(fds[0]))
	// The third byte needs a slot that the caller has already returned.
	for i := range slots + 1 {
		payload := []byte{byte('a' + i)}
		if n, errno := writeTestFD(fds[1], payload); n != 1 || errno != 0 {
			t.Fatalf("write %q: n=%d errno=%d", payload, n, errno)
		}
		cqe := waitForCQEs(t, ring, 1)[0]
		if cqe.Context() != recv || cqe.Res != 1 || !cqe.HasMore() {
			t.Fatalf("expected live receive for %q, got %+v", payload, cqe)
		}
		it, ok := ring.BundleIterator(cqe, group)
		if !ok || it.Count() != 1 || !bytes.Equal(it.Buffer(0), payload) {
			t.Fatalf("selected buffer %d: got %q, want %q", cqe.BufID(), it.Buffer(0), payload)
		}
		if err := it.Recycle(ring); err != nil {
			t.Fatal(err)
		}
	}

	cancel := ctx.WithOp(uring.IORING_OP_ASYNC_CANCEL)
	if err := ring.AsyncCancelFD(cancel, false); err != nil {
		t.Fatal(err)
	}
	for _, cqe := range requireTerminalResults(t, ring, map[uring.SQEContext]int32{recv: -int32(zcall.ECANCELED), cancel: 0}) {
		if cqe.Context() == recv && cqe.HasBuffer() {
			it, ok := ring.BundleIterator(cqe, group)
			if !ok {
				t.Fatalf("missing returned buffer %d", cqe.BufID())
			}
			if err := it.Recycle(ring); err != nil {
				t.Fatal(err)
			}
		}
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

func TestMultishotStressAccept(t *testing.T) {
	const connections = 50
	ring := newTestRing(t, func(o *uring.Options) { o.Entries = uring.EntriesMicro })
	ctx, addr := multishotListener(t)
	if err := ring.SubmitAcceptMultishot(ctx); err != nil {
		t.Fatal(err)
	}

	var clients sync.WaitGroup
	clients.Go(func() {
		for range connections {
			peer, err := net.DialTimeout("tcp4", addr, 3*time.Second)
			if err != nil {
				t.Errorf("dial: %v", err)
				return
			}
			if err := peer.Close(); err != nil {
				t.Errorf("close peer: %v", err)
				return
			}
		}
	})
	t.Cleanup(clients.Wait)

	accept := ctx.WithOp(uring.IORING_OP_ACCEPT)
	for _, cqe := range waitForCQEs(t, ring, connections) {
		if cqe.Context() != accept || cqe.Res < 0 || !cqe.HasMore() {
			t.Fatalf("expected live accept, got %+v", cqe)
		}
		fd := iofd.FD(cqe.Res)
		if err := fd.Close(); err != nil {
			t.Fatal(err)
		}
	}
	cancel := ctx.WithOp(uring.IORING_OP_ASYNC_CANCEL)
	if err := ring.AsyncCancelFD(cancel, false); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{accept: -int32(zcall.ECANCELED), cancel: 0})
}
