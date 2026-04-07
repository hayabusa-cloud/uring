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

func TestZCTrackerNotificationReturnsExtSQEOnUnexpectedState(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	h := &zcContractHandler{}
	tracker := &ZCTracker{pool: pool}
	ctx := zcInitCtx(ext, tracker, h)

	ctx.Tracker.state.Store(zcStateSubmitted)
	notified := CQEView{Res: 0, Flags: IORING_CQE_F_NOTIF}
	if !tracker.dispatchCQE(notified, ext, ctx, h) {
		t.Fatal("notification CQE was not handled")
	}
	if len(h.notified) != 0 {
		t.Fatalf("unexpected notifications: got %v, want none", h.notified)
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("expected notification CQE to return ExtSQE to pool")
	}
}
