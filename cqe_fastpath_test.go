// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// TestWaitDirectNOP tests the Direct mode fast-path with NOP operations.
func TestWaitDirectNOP(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit a NOP using Direct mode
	sqeCtx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, -1)
	if err := ring.Nop(sqeCtx); err != nil {
		t.Fatalf("Nop: %v", err)
	}

	// Wait using fast-path
	cqes := make([]uring.DirectCQE, 16)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.WaitDirect(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("WaitDirect: %v", err)
		}
		if n > 0 {
			cqe := cqes[0]
			if !cqe.IsSuccess() {
				t.Errorf("expected success, got res=%d", cqe.Res)
			}
			if cqe.Op != uring.IORING_OP_NOP {
				t.Errorf("expected NOP, got op=%d", cqe.Op)
			}
			t.Logf("DirectCQE: Op=%d, Res=%d, FD=%d", cqe.Op, cqe.Res, cqe.FD)
			return
		}
		b.Wait()
	}
	t.Fatal("timeout waiting for CQE")
}

// TestWaitExtendedNOP tests the Extended mode fast-path with NOP operations.
func TestWaitExtendedNOP(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get ExtSQE
	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	// Submit NOP using Extended mode context through the normal packed submit path.
	sqeCtx := uring.PackExtended(ext).WithFD(iofd.FD(77))
	if err := ring.Nop(sqeCtx); err != nil {
		ring.PutExtSQE(ext)
		t.Fatalf("Nop: %v", err)
	}

	// Wait using fast-path
	cqes := make([]uring.ExtCQE, 16)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.WaitExtended(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("WaitExtended: %v", err)
		}
		if n > 0 {
			cqe := cqes[0]
			if !cqe.IsSuccess() {
				t.Errorf("expected success, got res=%d", cqe.Res)
			}
			if cqe.Ext == nil {
				t.Error("expected ExtSQE pointer")
			} else {
				if got := cqe.Op(); got != uring.IORING_OP_NOP {
					t.Errorf("Op: got %d, want %d", got, uring.IORING_OP_NOP)
				}
				if got := cqe.FD(); got != iofd.FD(77) {
					t.Errorf("FD: got %d, want 77", got)
				}
				t.Logf("ExtCQE: Res=%d, Ext=%p", cqe.Res, cqe.Ext)
				ring.PutExtSQE(cqe.Ext)
			}
			return
		}
		b.Wait()
	}
	t.Fatal("timeout waiting for CQE")
}

// TestDirectCQEFlags tests DirectCQE flag helper methods.
func TestDirectCQEFlags(t *testing.T) {
	cqe := uring.DirectCQE{
		Res:   100,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER | (42 << uring.IORING_CQE_BUFFER_SHIFT),
	}

	if !cqe.IsSuccess() {
		t.Error("IsSuccess should be true for positive Res")
	}
	if !cqe.HasMore() {
		t.Error("HasMore should be true")
	}
	if !cqe.HasBuffer() {
		t.Error("HasBuffer should be true")
	}
	if id := cqe.BufID(); id != 42 {
		t.Errorf("BufID: got %d, want 42", id)
	}

	// Test notification flag
	cqeNotif := uring.DirectCQE{Flags: uring.IORING_CQE_F_NOTIF}
	if !cqeNotif.IsNotification() {
		t.Error("IsNotification should be true")
	}
}

// TestExtCQEFlags tests ExtCQE flag helper methods.
func TestExtCQEFlags(t *testing.T) {
	// Test flag helper methods with various flag combinations
	cqe := uring.ExtCQE{
		Res:   100,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUF_MORE,
		Ext:   nil, // ExtSQE not needed for flag tests
	}

	if !cqe.IsSuccess() {
		t.Error("IsSuccess should be true")
	}
	if !cqe.HasMore() {
		t.Error("HasMore should be true")
	}
	if !cqe.HasBufferMore() {
		t.Error("HasBufferMore should be true")
	}

	// Test negative result
	cqeErr := uring.ExtCQE{Res: -1}
	if cqeErr.IsSuccess() {
		t.Error("IsSuccess should be false for negative Res")
	}

	// Test notification flag
	cqeNotif := uring.ExtCQE{Flags: uring.IORING_CQE_F_NOTIF}
	if !cqeNotif.IsNotification() {
		t.Error("IsNotification should be true")
	}

	// Test buffer flag
	cqeBuf := uring.ExtCQE{
		Flags: uring.IORING_CQE_F_BUFFER | (99 << uring.IORING_CQE_BUFFER_SHIFT),
	}
	if !cqeBuf.HasBuffer() {
		t.Error("HasBuffer should be true")
	}
	if id := cqeBuf.BufID(); id != 99 {
		t.Errorf("BufID: got %d, want 99", id)
	}
}

func TestExtCQEMethods(t *testing.T) {
	pool := uring.NewContextPools(16)

	t.Run("HasBufferMore", func(t *testing.T) {
		ext := pool.Extended()
		if ext == nil {
			t.Skip("pool exhausted")
		}
		defer pool.PutExtended(ext)

		cqeNoMore := uring.ExtCQE{
			Res:   0,
			Flags: 0,
			Ext:   ext,
		}
		if cqeNoMore.HasBufferMore() {
			t.Error("HasBufferMore should be false when flag not set")
		}

		cqeWithMore := uring.ExtCQE{
			Res:   0,
			Flags: uring.IORING_CQE_F_BUF_MORE,
			Ext:   ext,
		}
		if !cqeWithMore.HasBufferMore() {
			t.Error("HasBufferMore should be true when flag set")
		}
	})
}

// BenchmarkWaitDirect benchmarks the Direct mode fast-path.
func BenchmarkWaitDirect(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	cqes := make([]uring.DirectCQE, 16)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Submit NOP
		sqeCtx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, -1)
		_ = ring.Nop(sqeCtx)

		// Wait for completion
		for {
			n, _ := ring.WaitDirect(cqes)
			if n > 0 {
				break
			}
		}
	}
}

// BenchmarkWaitExtended benchmarks the Extended mode fast-path.
// Note: WSL2 has a kernel bug that truncates lower bits of CQE userData,
// causing ExtSQE pointer corruption. Skip on WSL2.
func BenchmarkWaitExtended(b *testing.B) {
	if isWSL2() {
		b.Skip("WSL2 userData truncation bug corrupts ExtSQE pointers")
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	cqes := make([]uring.ExtCQE, 16)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ext := ring.ExtSQE()
		if ext == nil {
			b.Fatal("pool exhausted")
		}

		sqeCtx := uring.PackExtended(ext)
		_ = ring.Nop(sqeCtx)

		// Wait for completion and release all returned ExtSQEs
		for {
			n, _ := ring.WaitExtended(cqes)
			if n > 0 {
				// Release ALL returned ExtSQEs, not just the first one
				for j := range n {
					if cqes[j].Ext != nil {
						ring.PutExtSQE(cqes[j].Ext)
					}
				}
				break
			}
		}
	}
}

// TestWaitDirectEdgeCases tests edge cases for WaitDirect.
func TestWaitDirectEdgeCases(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Run("empty slice", func(t *testing.T) {
		cqes := make([]uring.DirectCQE, 0)
		n, err := ring.WaitDirect(cqes)
		if err != nil {
			t.Errorf("WaitDirect(empty): %v", err)
		}
		if n != 0 {
			t.Errorf("WaitDirect(empty): n=%d, want 0", n)
		}
	})

	t.Run("no CQEs available", func(t *testing.T) {
		cqes := make([]uring.DirectCQE, 16)
		n, err := ring.WaitDirect(cqes)
		if !errors.Is(err, iox.ErrWouldBlock) {
			t.Errorf("WaitDirect(no CQEs): expected ErrWouldBlock, got %v", err)
		}
		if n != 0 {
			t.Errorf("WaitDirect(no CQEs): n=%d, want 0", n)
		}
	})
}

// TestWaitExtendedEdgeCases tests edge cases for WaitExtended.
func TestWaitExtendedEdgeCases(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Run("empty slice", func(t *testing.T) {
		cqes := make([]uring.ExtCQE, 0)
		n, err := ring.WaitExtended(cqes)
		if err != nil {
			t.Errorf("WaitExtended(empty): %v", err)
		}
		if n != 0 {
			t.Errorf("WaitExtended(empty): n=%d, want 0", n)
		}
	})

	t.Run("no CQEs available", func(t *testing.T) {
		cqes := make([]uring.ExtCQE, 16)
		n, err := ring.WaitExtended(cqes)
		if !errors.Is(err, iox.ErrWouldBlock) {
			t.Errorf("WaitExtended(no CQEs): expected ErrWouldBlock, got %v", err)
		}
		if n != 0 {
			t.Errorf("WaitExtended(no CQEs): n=%d, want 0", n)
		}
	})
}

// BenchmarkWaitGeneric benchmarks the generic CQEView path for comparison.
func BenchmarkWaitGeneric(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	cqes := make([]uring.CQEView, 16)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sqeCtx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, -1)
		_ = ring.Nop(sqeCtx)

		for {
			n, _ := ring.Wait(cqes)
			if n > 0 {
				break
			}
		}
	}
}
