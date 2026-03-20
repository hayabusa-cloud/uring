// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "testing"

func TestDarwinSQEContextDirectRoundTrip(t *testing.T) {
	ctx := PackDirect(RecvOp, IOSQE_BUFFER_SELECT, 17, -9)

	if !ctx.IsDirect() {
		t.Fatal("direct context should report direct mode")
	}
	if ctx.IsIndirect() || ctx.IsExtended() {
		t.Fatal("direct context should not report pointer modes")
	}
	if got := ctx.Op(); got != RecvOp {
		t.Fatalf("Op() = %d, want %d", got, RecvOp)
	}
	if got := ctx.Flags(); got != IOSQE_BUFFER_SELECT {
		t.Fatalf("Flags() = %d, want %d", got, IOSQE_BUFFER_SELECT)
	}
	if got := ctx.BufGroup(); got != 17 {
		t.Fatalf("BufGroup() = %d, want 17", got)
	}
	if got := ctx.FD(); got != -9 {
		t.Fatalf("FD() = %d, want -9", got)
	}
	if !ctx.HasBufferSelect() {
		t.Fatal("HasBufferSelect() = false, want true")
	}
	if got := SQEContextFromRaw(ctx.Raw()); got != ctx {
		t.Fatalf("SQEContextFromRaw(Raw()) = %v, want %v", got, ctx)
	}
}

func TestDarwinSQEContextPointerRoundTrip(t *testing.T) {
	indirect := &IndirectSQE{}
	ctxIndirect := PackIndirect(indirect)
	if !ctxIndirect.IsIndirect() {
		t.Fatal("indirect context should report indirect mode")
	}
	if got := ctxIndirect.IndirectSQE(); got != indirect {
		t.Fatalf("IndirectSQE() = %p, want %p", got, indirect)
	}

	ext := &ExtSQE{}
	ctxExtended := PackExtended(ext)
	if !ctxExtended.IsExtended() {
		t.Fatal("extended context should report extended mode")
	}
	if got := ctxExtended.ExtSQE(); got != ext {
		t.Fatalf("ExtSQE() = %p, want %p", got, ext)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("ExtSQE() on direct context should panic")
		}
	}()
	PackDirect(0, 0, 0, 0).ExtSQE()
}

func TestDarwinContextPoolsRoundTrip(t *testing.T) {
	pools := NewContextPools(2)
	pools.Init()

	if got := pools.Capacity(); got != 2 {
		t.Fatalf("Capacity() = %d, want 2", got)
	}
	if got := pools.IndirectAvailable(); got != 2 {
		t.Fatalf("IndirectAvailable() = %d, want 2", got)
	}
	if got := pools.ExtendedAvailable(); got != 2 {
		t.Fatalf("ExtendedAvailable() = %d, want 2", got)
	}

	indirect1 := pools.GetIndirect()
	indirect2 := pools.GetIndirect()
	if indirect1 == nil || indirect2 == nil {
		t.Fatal("GetIndirect() returned nil before exhaustion")
	}
	if got := pools.GetIndirect(); got != nil {
		t.Fatalf("GetIndirect() after exhaustion = %p, want nil", got)
	}
	pools.PutIndirect(indirect1)
	if got := pools.IndirectAvailable(); got != 1 {
		t.Fatalf("IndirectAvailable() after PutIndirect = %d, want 1", got)
	}

	ext1 := pools.GetExtended()
	ext2 := pools.GetExtended()
	if ext1 == nil || ext2 == nil {
		t.Fatal("GetExtended() returned nil before exhaustion")
	}
	if got := pools.GetExtended(); got != nil {
		t.Fatalf("GetExtended() after exhaustion = %p, want nil", got)
	}
	pools.PutExtended(ext1)
	if got := pools.ExtendedAvailable(); got != 1 {
		t.Fatalf("ExtendedAvailable() after PutExtended = %d, want 1", got)
	}
}
