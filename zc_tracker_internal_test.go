// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "testing"

type zcContractHandler struct {
	completed []int32
	notified  []int32
}

func (h *zcContractHandler) OnCompleted(result int32) {
	h.completed = append(h.completed, result)
}

func (h *zcContractHandler) OnNotification(result int32) {
	h.notified = append(h.notified, result)
}

func assertNoPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	f()
}

func TestZCTrackerNotificationUsesStoredCompletionResult(t *testing.T) {
	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	h := &zcContractHandler{}
	tracker := &ZCTracker{pool: pool}
	_ = zcInitCtx(ext, tracker, h)
	ctx := asZCCtx(ext)

	completed := CQEView{Res: 23, Flags: IORING_CQE_F_MORE}
	notified := CQEView{Res: -7, Flags: IORING_CQE_F_NOTIF}

	if !tracker.dispatchCQE(completed, ext, ctx, h) {
		t.Fatal("completed CQE was not handled")
	}
	if !tracker.dispatchCQE(notified, ext, ctx, h) {
		t.Fatal("notification CQE was not handled")
	}

	if len(h.completed) != 1 || h.completed[0] != 23 {
		t.Fatalf("OnCompleted results: got %v, want [23]", h.completed)
	}
	if len(h.notified) != 1 || h.notified[0] != 23 {
		t.Fatalf("OnNotification results: got %v, want [23]", h.notified)
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected notification CQE to return ExtSQE to pool")
	}
}

func TestZCTrackerDataNotificationTerminalIsAffine(t *testing.T) {
	var data zcTrackerData
	data.meta.Store(zcPackMeta(zcStateCompleted, 23))

	notify, release := data.markNotification()
	if !notify || !release {
		t.Fatalf("first notification transition: got notify=%v release=%v, want true true", notify, release)
	}
	if got := data.result(); got != 23 {
		t.Fatalf("notification result: got %d, want 23", got)
	}

	notify, release = data.markNotification()
	if notify || release {
		t.Fatalf("second notification transition: got notify=%v release=%v, want false false", notify, release)
	}
}

func TestZCTrackerDataCompletionReplayKeepsFirstResult(t *testing.T) {
	var data zcTrackerData

	complete, notify, release := data.markCompleted(23)
	if !complete || notify || release {
		t.Fatalf("first completion transition: got complete=%v notify=%v release=%v, want true false false", complete, notify, release)
	}
	complete, notify, release = data.markCompleted(24)
	if complete || notify || release {
		t.Fatalf("replayed completion transition: got complete=%v notify=%v release=%v, want false false false", complete, notify, release)
	}

	notify, release = data.markNotification()
	if !notify || !release {
		t.Fatalf("notification transition: got notify=%v release=%v, want true true", notify, release)
	}
	if got := data.result(); got != 23 {
		t.Fatalf("notification result after replayed completion: got %d, want 23", got)
	}
}

func TestZCTrackerDataEarlyNotificationWaitsForData(t *testing.T) {
	var data zcTrackerData

	notify, release := data.markNotification()
	if notify || release {
		t.Fatalf("early notification transition: got notify=%v release=%v, want false false", notify, release)
	}
	if got := zcMetaState(data.meta.Load()); got != zcStateEarlyNotified {
		t.Fatalf("state after early notification: got %d, want %d", got, zcStateEarlyNotified)
	}

	complete, notify, release := data.markCompleted(23)
	if !complete || !notify || !release {
		t.Fatalf("data after early notification: got complete=%v notify=%v release=%v, want true true true", complete, notify, release)
	}
	if got := data.result(); got != 23 {
		t.Fatalf("completion result after early notification: got %d, want 23", got)
	}

	complete, notify, release = data.markCompleted(24)
	if complete || notify || release {
		t.Fatalf("replayed completion transition: got complete=%v notify=%v release=%v, want false false false", complete, notify, release)
	}
}

func TestZCTrackerDataEarlyNotificationTerminalData(t *testing.T) {
	var data zcTrackerData

	notify, release := data.markNotification()
	if notify || release {
		t.Fatalf("early notification transition: got notify=%v release=%v, want false false", notify, release)
	}

	complete, notify, release := data.markTerminalData(23)
	if !complete || !notify || !release {
		t.Fatalf("terminal after early notification: got complete=%v notify=%v release=%v, want true true true", complete, notify, release)
	}
	if got := data.result(); got != 23 {
		t.Fatalf("terminal data result after early notification: got %d, want 23", got)
	}
}

func TestZCTrackerDataTerminalFallbackIsAffine(t *testing.T) {
	var data zcTrackerData

	complete, notify, release := data.markTerminalData(23)
	if !complete || !notify || !release {
		t.Fatalf("terminal fallback transition: got complete=%v notify=%v release=%v, want true true true", complete, notify, release)
	}
	if got := data.result(); got != 23 {
		t.Fatalf("terminal fallback result: got %d, want 23", got)
	}
	complete, notify, release = data.markTerminalData(24)
	if complete || notify || release {
		t.Fatalf("replayed terminal fallback: got complete=%v notify=%v release=%v, want false false false", complete, notify, release)
	}
	notify, release = data.markNotification()
	if notify || release {
		t.Fatalf("late notification after fallback: got notify=%v release=%v, want false false", notify, release)
	}
}

func TestZCTrackerDataCompletionAfterNotificationReplayIsRejected(t *testing.T) {
	var data zcTrackerData

	complete, notify, release := data.markCompleted(23)
	if !complete || notify || release {
		t.Fatal("first completion transition was rejected")
	}

	notify, release = data.markNotification()
	if !notify || !release {
		t.Fatalf("notification transition: got notify=%v release=%v, want true true", notify, release)
	}
	complete, notify, release = data.markCompleted(24)
	if complete || notify || release {
		t.Fatalf("completion after notification: got complete=%v notify=%v release=%v, want false false false", complete, notify, release)
	}
}

func TestZCTrackerHandleCQEUsesNoopHandlerWhenNil(t *testing.T) {
	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	tracker := &ZCTracker{pool: pool}
	_ = zcInitCtx(ext, tracker, nil)

	completed := CQEView{Res: 23, Flags: IORING_CQE_F_MORE, ctx: PackExtended(ext)}
	notified := CQEView{Res: 0, Flags: IORING_CQE_F_NOTIF, ctx: PackExtended(ext)}

	if !tracker.HandleCQE(completed) {
		t.Fatal("completed CQE was not handled")
	}
	if got := zcGetHandler(ext); got == nil {
		t.Fatal("expected nil handler to be normalized to noop handler while ExtSQE is live")
	}
	if !tracker.HandleCQE(notified) {
		t.Fatal("notification CQE was not handled")
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected notification CQE to return ExtSQE to pool")
	}
}

func TestZCTrackerEarlyNotificationDoesNotReturnExtSQE(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	h := &zcContractHandler{}
	tracker := &ZCTracker{pool: pool}
	ctx := zcInitCtx(ext, tracker, h)

	ctx.Tracker.meta.Store(zcPackMeta(zcStateSubmitted, 0))
	notified := CQEView{Res: 0, Flags: IORING_CQE_F_NOTIF}
	if !tracker.dispatchCQE(notified, ext, ctx, h) {
		t.Fatal("notification CQE was not handled")
	}
	if len(h.notified) != 0 {
		t.Fatalf("unexpected notifications: got %v, want none", h.notified)
	}
	if got := pool.ExtendedAvailable(); got != 0 {
		t.Fatalf("available ExtSQE after early notification: got %d, want 0", got)
	}
}

func TestZCTrackerDataAfterEarlyNotificationCompletesAndReleases(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	h := &zcContractHandler{}
	tracker := &ZCTracker{pool: pool}
	ctx := zcInitCtx(ext, tracker, h)

	notified := CQEView{Res: 0, Flags: IORING_CQE_F_NOTIF}
	if !tracker.dispatchCQE(notified, ext, ctx, h) {
		t.Fatal("notification CQE was not handled")
	}
	if got := pool.Extended(); got != nil {
		t.Fatalf("ExtSQE reused after early notification: got %p, want nil", got)
	}

	completed := CQEView{Res: 23, Flags: IORING_CQE_F_MORE, ctx: PackExtended(ext)}
	if !tracker.HandleCQE(completed) {
		t.Fatal("data CQE after early notification was not handled")
	}
	if len(h.completed) != 1 || h.completed[0] != 23 {
		t.Fatalf("completed callbacks after early notification: got %v, want [23]", h.completed)
	}
	if len(h.notified) != 1 || h.notified[0] != 23 {
		t.Fatalf("notification callbacks after early notification: got %v, want [23]", h.notified)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after data CQE: got %d, want 1", got)
	}
}

func TestZCTrackerLateNotificationAfterTerminalFallbackDoesNotReleaseAgain(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	h := &zcContractHandler{}
	tracker := &ZCTracker{pool: pool}
	ctx := zcInitCtx(ext, tracker, h)

	terminal := CQEView{Res: 23}
	if !tracker.dispatchCQE(terminal, ext, ctx, h) {
		t.Fatal("terminal CQE was not handled")
	}
	if len(h.completed) != 1 || h.completed[0] != 23 {
		t.Fatalf("terminal completed callbacks: got %v, want [23]", h.completed)
	}
	if len(h.notified) != 1 || h.notified[0] != 23 {
		t.Fatalf("terminal notification callbacks: got %v, want [23]", h.notified)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after terminal fallback: got %d, want 1", got)
	}

	notified := CQEView{Res: 0, Flags: IORING_CQE_F_NOTIF, ctx: PackExtended(ext)}
	assertNoPanic(t, func() {
		if tracker.HandleCQE(notified) {
			t.Fatal("late notification CQE was handled after ExtSQE release")
		}
	})
	if len(h.completed) != 1 || len(h.notified) != 1 {
		t.Fatalf("late notification callbacks: got completed=%v notified=%v, want one each", h.completed, h.notified)
	}
	if got := pool.ExtendedAvailable(); got != 1 {
		t.Fatalf("available ExtSQE after late notification: got %d, want 1", got)
	}
}

func TestZCTrackerCQEHandlerDispatchesAnchoredTracker(t *testing.T) {
	pool := NewContextPools(4)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	h := &zcContractHandler{}
	tracker := &ZCTracker{pool: pool}
	_ = zcInitCtx(ext, tracker, h)
	userData := PackExtended(ext).Raw()

	zcTrackerCQEHandler(nil, nil, &ioUringCqe{
		userData: userData,
		res:      44,
		flags:    IORING_CQE_F_MORE,
	})
	zcTrackerCQEHandler(nil, nil, &ioUringCqe{
		userData: userData,
		res:      -7,
		flags:    IORING_CQE_F_NOTIF,
	})

	if len(h.completed) != 1 || h.completed[0] != 44 {
		t.Fatalf("OnCompleted results: got %v, want [44]", h.completed)
	}
	if len(h.notified) != 1 || h.notified[0] != 44 {
		t.Fatalf("OnNotification results: got %v, want [44]", h.notified)
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected notification CQE to return ExtSQE to pool")
	}
}

func TestZCTrackerCQEHandlerIgnoresNonExtendedAndUnownedContexts(t *testing.T) {
	zcTrackerCQEHandler(nil, nil, &ioUringCqe{})

	pool := NewContextPools(2)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	zcTrackerCQEHandler(nil, nil, &ioUringCqe{
		userData: PackExtended(ext).Raw(),
		flags:    IORING_CQE_F_MORE,
	})
}
