// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"slices"
	"sync/atomic"
	"testing"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// =============================================================================
// ZCTracker Tests
// =============================================================================

// testZCHandler implements ZCHandler for testing.
type testZCHandler struct {
	completedCalls atomic.Int32
	notifyCalls    atomic.Int32
	completedRes   atomic.Int32
	notifyRes      atomic.Int32
}

func (h *testZCHandler) OnCompleted(result int32) {
	h.completedRes.Store(result)
	h.completedCalls.Add(1)
}

func (h *testZCHandler) OnNotification(result int32) {
	h.notifyRes.Store(result)
	h.notifyCalls.Add(1)
}

// Drain the data CQE for each admitted send and every notification it promises.
// A notification may already have been dispatched when its data CQE arrives.
func drainTrackedSends(t *testing.T, ring *uring.Uring, tracker *uring.ZCTracker, sends int) {
	t.Helper()
	var data, promised, notified int
	for data < sends || notified < promised {
		cqe := waitForCQEs(t, ring, 1)[0]
		if cqe.IsNotification() {
			notified++
		} else {
			data++
			if cqe.HasMore() {
				promised++
			}
		}
		if !tracker.HandleCQE(cqe) {
			t.Fatalf("unhandled send CQE: result=%d flags=%#x", cqe.Res, cqe.Flags)
		}
	}
}

func (h *testZCHandler) check(t *testing.T, calls, result int32) {
	t.Helper()
	if got, released := h.completedCalls.Load(), h.notifyCalls.Load(); got != calls || released != calls {
		t.Errorf("completion and release calls = (%d, %d), want (%d, %d)", got, released, calls, calls)
	}
	if got, released := h.completedRes.Load(), h.notifyRes.Load(); got != result || released != result {
		t.Errorf("completion and release results = (%d, %d), want (%d, %d)", got, released, result, result)
	}
}

func TestZCTrackerBasic(t *testing.T) {
	ring := newTestRing(t)
	fd, _ := newTestTCPPair(t)
	pool := uring.NewContextPools(1)
	tracker := uring.NewZCTracker(ring, pool)
	var h testZCHandler
	if err := tracker.SendZC(fd, ring.RegisteredBuffer(0)[:1], &h); err != nil {
		t.Fatal(err)
	}
	drainTrackedSends(t, ring, tracker, 1)
	h.check(t, 1, 1)
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available after release = %d, want 1", got)
	}
}

func TestZCTrackerPoolExhaustion(t *testing.T) {
	ring := newTestRing(t)
	fd, _ := newTestTCPPair(t)
	pool := uring.NewContextPools(2)
	tracker := uring.NewZCTracker(ring, pool)
	buf := ring.RegisteredBuffer(0)[:1]
	var handlers [3]testZCHandler
	for i := range 2 {
		if err := tracker.SendZC(fd, buf, &handlers[i]); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := tracker.SendZC(fd, buf, &handlers[2]); !iox.IsWouldBlock(err) {
		t.Fatalf("send with exhausted pool = %v, want ErrWouldBlock", err)
	}
	drainTrackedSends(t, ring, tracker, 2)
	handlers[0].check(t, 1, 1)
	handlers[1].check(t, 1, 1)
	handlers[2].check(t, 0, 0)
	if got := pool.ExtendedAvailable(); got != 2 {
		t.Fatalf("available after both releases = %d, want 2", got)
	}

	if err := tracker.SendZC(fd, buf, &handlers[2]); err != nil {
		t.Fatalf("retry: %v", err)
	}
	drainTrackedSends(t, ring, tracker, 1)
	handlers[2].check(t, 1, 1)
	if got := pool.ExtendedAvailable(); got != 2 {
		t.Fatalf("available after retry = %d, want 2", got)
	}
}

func TestZCTrackerHandlerOrder(t *testing.T) {
	ring := newTestRing(t)
	fd, _ := newTestTCPPair(t)
	pool := uring.NewContextPools(1)
	tracker := uring.NewZCTracker(ring, pool)
	var order []string
	handler := uring.NewZCSubscriber().
		OnCompleted(func(int32) { order = append(order, "completed") }).
		OnNotification(func(int32) { order = append(order, "released") }).
		Handler()
	if err := tracker.SendZC(fd, ring.RegisteredBuffer(0)[:1], handler); err != nil {
		t.Fatal(err)
	}
	drainTrackedSends(t, ring, tracker, 1)
	if !slices.Equal(order, []string{"completed", "released"}) {
		t.Fatalf("callback order = %v", order)
	}
}

// Reuse the same pool slot for a successful send and a failed send. Each
// lifetime has its own handler and receives its own data result twice.
func TestZCTrackerExactlyOnce(t *testing.T) {
	ring := newTestRing(t)
	fd, _ := newTestTCPPair(t)
	pool := uring.NewContextPools(1)
	tracker := uring.NewZCTracker(ring, pool)
	buf := ring.RegisteredBuffer(0)[:1]
	var first, second testZCHandler
	if err := tracker.SendZC(fd, buf, &first); err != nil {
		t.Fatal(err)
	}
	drainTrackedSends(t, ring, tracker, 1)
	first.check(t, 1, 1)
	if err := tracker.SendZC(-1, buf, &second); err != nil {
		t.Fatal(err)
	}
	drainTrackedSends(t, ring, tracker, 1)
	first.check(t, 1, 1)
	second.check(t, 1, -int32(zcall.EBADF))
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available after slot reuse = %d, want 1", got)
	}
}
