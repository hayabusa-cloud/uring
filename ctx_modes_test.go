// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

func TestScopedExtSQEBasic(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Test basic scope creation
	scope := ring.NewScopedExtSQE()
	if !scope.Valid() {
		t.Fatal("scope should be valid")
	}
	if scope.Ext == nil {
		t.Fatal("scope.Ext should not be nil")
	}

	// Test Release (not submitted)
	scope.Release()
	if scope.Ext != nil {
		t.Error("scope.Ext should be nil after Release")
	}

	// Test double Release is safe
	scope.Release()
}

func TestScopedExtSQESubmitted(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get a scope
	scope := ring.NewScopedExtSQE()
	if !scope.Valid() {
		t.Fatal("scope should be valid")
	}

	// Mark as submitted
	scope.Submitted()

	// Keep the Ext pointer for manual return
	ext := scope.Ext

	// Release should be no-op when submitted
	scope.Release()
	if scope.Ext == nil {
		// Ext is still set because we marked it as submitted
		// The pool return was skipped
	}

	// Manually return to pool (simulating CQE handler)
	ring.PutExtSQE(ext)
}

func TestWithExtSQE(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Run("success", func(t *testing.T) {
		var gotExt *uring.ExtSQE
		err := ring.WithExtSQE(func(ext *uring.ExtSQE) error {
			gotExt = ext
			if ext == nil {
				t.Error("ext should not be nil")
			}
			// Return nil - caller is responsible for pool return
			ring.PutExtSQE(ext)
			return nil
		})
		if err != nil {
			t.Errorf("WithExtSQE: %v", err)
		}
		if gotExt == nil {
			t.Error("callback was not called")
		}
	})

	t.Run("error", func(t *testing.T) {
		testErr := errors.New("test error")
		err := ring.WithExtSQE(func(ext *uring.ExtSQE) error {
			return testErr // Pool return happens automatically
		})
		if err != testErr {
			t.Errorf("expected test error, got: %v", err)
		}
	})
}

func TestMustExtSQE(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Should not panic when pool has items
	ext := ring.MustExtSQE()
	if ext == nil {
		t.Error("MustExtSQE returned nil")
	}
	ring.PutExtSQE(ext)
}

func TestWithExtSQEPoolExhaustion(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Exhaust the pool
	var exts []*uring.ExtSQE
	for {
		ext := ring.ExtSQE()
		if ext == nil {
			break
		}
		exts = append(exts, ext)
	}

	// WithExtSQE should return ErrWouldBlock
	err = ring.WithExtSQE(func(ext *uring.ExtSQE) error {
		t.Error("callback should not be called when pool exhausted")
		return nil
	})
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Errorf("expected ErrWouldBlock, got: %v", err)
	}

	// Return items to pool
	for _, ext := range exts {
		ring.PutExtSQE(ext)
	}
}

func TestContextPoolsAvailability(t *testing.T) {
	pool := uring.NewContextPools(16)

	// Test Capacity
	capacity := pool.Capacity()
	if capacity != 16 {
		t.Errorf("Capacity: got %d, want 16", capacity)
	}

	// Initially, all slots should be available
	indirectAvail := pool.IndirectAvailable()
	if indirectAvail != capacity {
		t.Errorf("IndirectAvailable: got %d, want %d", indirectAvail, capacity)
	}

	extendedAvail := pool.ExtendedAvailable()
	if extendedAvail != capacity {
		t.Errorf("ExtendedAvailable: got %d, want %d", extendedAvail, capacity)
	}

	// Get some items and check availability decreases
	ind1 := pool.Indirect()
	if ind1 == nil {
		t.Fatal("Indirect failed")
	}

	if pool.IndirectAvailable() != capacity-1 {
		t.Errorf("IndirectAvailable after Get: got %d, want %d", pool.IndirectAvailable(), capacity-1)
	}

	ext1 := pool.Extended()
	if ext1 == nil {
		t.Fatal("Extended failed")
	}

	if pool.ExtendedAvailable() != capacity-1 {
		t.Errorf("ExtendedAvailable after Get: got %d, want %d", pool.ExtendedAvailable(), capacity-1)
	}

	// Return items and check availability increases
	pool.PutIndirect(ind1)
	if pool.IndirectAvailable() != capacity {
		t.Errorf("IndirectAvailable after Put: got %d, want %d", pool.IndirectAvailable(), capacity)
	}

	pool.PutExtended(ext1)
	if pool.ExtendedAvailable() != capacity {
		t.Errorf("ExtendedAvailable after Put: got %d, want %d", pool.ExtendedAvailable(), capacity)
	}
}

// =============================================================================
// Context Pool Tests
// =============================================================================

func TestContextPools(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Run("ExtSQE pool", func(t *testing.T) {
		ext := ring.ExtSQE()
		if ext == nil {
			t.Fatal("ExtSQE returned nil")
		}

		// Write some data to verify it's usable
		copy(ext.UserData[:8], []byte("TEST1234"))

		// Return to pool
		ring.PutExtSQE(ext)

		// Get again (may be same or different)
		ext2 := ring.ExtSQE()
		if ext2 == nil {
			t.Fatal("ExtSQE returned nil on second get")
		}
		ring.PutExtSQE(ext2)
	})

	t.Run("IndirectSQE pool", func(t *testing.T) {
		ind := ring.IndirectSQE()
		if ind == nil {
			t.Fatal("IndirectSQE returned nil")
		}

		// Return to pool
		ring.PutIndirectSQE(ind)

		// Get again
		ind2 := ring.IndirectSQE()
		if ind2 == nil {
			t.Fatal("IndirectSQE returned nil on second get")
		}
		ring.PutIndirectSQE(ind2)
	})
}

func TestStartPreservesBorrowedContextPoolEntries(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = 1
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("ExtSQE before Start returned nil")
	}
	ind := ring.IndirectSQE()
	if ind == nil {
		t.Fatal("IndirectSQE before Start returned nil")
	}

	mustStartRing(t, ring)

	if got := ring.ExtSQE(); got != nil {
		t.Fatalf("ExtSQE after Start = %p, want nil while borrowed entry is outstanding", got)
	}
	if got := ring.IndirectSQE(); got != nil {
		t.Fatalf("IndirectSQE after Start = %p, want nil while borrowed entry is outstanding", got)
	}

	ring.PutExtSQE(ext)
	ring.PutIndirectSQE(ind)

	if got := ring.ExtSQE(); got != ext {
		t.Fatalf("ExtSQE after PutExtSQE = %p, want %p", got, ext)
	} else {
		ring.PutExtSQE(got)
	}
	if got := ring.IndirectSQE(); got != ind {
		t.Fatalf("IndirectSQE after PutIndirectSQE = %p, want %p", got, ind)
	} else {
		ring.PutIndirectSQE(got)
	}
}

func TestExtendedModeSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Extended mode test in short mode (requires reliable io_uring)")
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get ExtSQE and submit NOP with Extended mode
	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("ExtSQE returned nil")
	}

	// Store identifying data
	copy(ext.UserData[:8], []byte("EXTENDED"))

	ctx := uring.PackExtended(ext)
	if !ctx.IsExtended() {
		t.Error("Context should be Extended mode")
	}

	if err := ring.Nop(ctx); err != nil {
		t.Fatalf("Nop: %v", err)
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() != uring.IORING_OP_NOP {
				continue
			}

			// Verify Extended mode
			if !cqe.Extended() {
				t.Error("CQE should be Extended mode")
			}
			if !cqe.FullSQE() {
				t.Error("CQE should have FullSQE for Extended mode")
			}

			// Get ExtSQE - UserData may be modified by submission
			// The key test is that we can recover the pointer
			extResult := cqe.ExtSQE()
			if extResult == nil {
				t.Error("ExtSQE returned nil")
			}

			// Test SQE() method
			sqeView := cqe.SQE()
			if !sqeView.Valid() {
				t.Error("SQEView should be valid for Extended mode")
			}

			// Test FD() method
			_ = cqe.FD()

			ring.PutExtSQE(extResult)
			return // Test passed
		}
		b.Wait()
	}
	t.Fatal("Extended NOP completion not received")
}

func TestIndirectModeContext(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get IndirectSQE and verify context packing
	ind := ring.IndirectSQE()
	if ind == nil {
		t.Fatal("IndirectSQE returned nil")
	}

	ctx := uring.PackIndirect(ind)
	if !ctx.IsIndirect() {
		t.Error("Context should be Indirect mode")
	}
	if ctx.IsDirect() {
		t.Error("Context should not be Direct mode")
	}
	if ctx.IsExtended() {
		t.Error("Context should not be Extended mode")
	}

	// Verify we can recover the pointer
	recovered := ctx.IndirectSQE()
	if recovered != ind {
		t.Error("IndirectSQE pointer round-trip failed")
	}

	ring.PutIndirectSQE(ind)
	t.Log("Indirect mode context packing verified")
}

// =============================================================================
// CastUserData Tests
// =============================================================================

func TestCastUserData(t *testing.T) {
	pool := uring.NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer pool.PutExtended(ext)

	// Use CastUserData via Ctx pattern
	type myContext struct {
		Val1 int64
		Val2 int32
		Val3 uint16
	}

	ctx := uring.CastUserData[myContext](ext)
	ctx.Val1 = 12345
	ctx.Val2 = 678
	ctx.Val3 = 9

	// Read back via CastUserData
	ctx2 := uring.CastUserData[myContext](ext)
	if ctx2.Val1 != 12345 {
		t.Errorf("Val1: got %d, want 12345", ctx2.Val1)
	}
	if ctx2.Val2 != 678 {
		t.Errorf("Val2: got %d, want 678", ctx2.Val2)
	}
	if ctx2.Val3 != 9 {
		t.Errorf("Val3: got %d, want 9", ctx2.Val3)
	}
}

func TestCastUserDataTooLargePanics(t *testing.T) {
	pool := uring.NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer pool.PutExtended(ext)

	type tooLarge struct {
		Data [65]byte
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected CastUserData to panic for overlays larger than 64 bytes")
		}
	}()

	_ = uring.CastUserData[tooLarge](ext)
}
