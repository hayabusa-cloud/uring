// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// TestSQEContextDirect demonstrates Direct mode context encoding.
//
// Direct mode packs all context into a single 64-bit value:
//
//	┌─────────┬─────────┬──────────────┬────────────────────────────┬────┐
//	│ Op (8b) │Flags(8b)│ BufGrp (16b) │        FD (30b)            │ 00 │
//	└─────────┴─────────┴──────────────┴────────────────────────────┴────┘
//
// Benefits:
//   - Zero heap allocation
//   - Single register operation
//   - Fits in io_uring's user_data field
//
// Use when: Simple operations with minimal context needs.
func TestSQEContextDirect(t *testing.T) {
	// Pack context for a read operation
	ctx := uring.PackDirect(
		uring.IORING_OP_READ, // op: operation type
		uring.IOSQE_ASYNC,    // flags: run asynchronously
		5,                    // bufGroup: buffer group ID
		42,                   // fd: file descriptor
	)

	// Verify we can extract fields
	if ctx.Op() != uring.IORING_OP_READ {
		t.Errorf("Op: got %d, want %d", ctx.Op(), uring.IORING_OP_READ)
	}
	if ctx.Flags() != uring.IOSQE_ASYNC {
		t.Errorf("Flags: got %d, want %d", ctx.Flags(), uring.IOSQE_ASYNC)
	}
	if ctx.BufGroup() != 5 {
		t.Errorf("BufGroup: got %d, want 5", ctx.BufGroup())
	}
	if ctx.FD() != 42 {
		t.Errorf("FD: got %d, want 42", ctx.FD())
	}

	// Mode detection
	if !ctx.IsDirect() {
		t.Error("Expected Direct mode")
	}
	if ctx.IsIndirect() || ctx.IsExtended() {
		t.Error("Should not be Indirect or Extended")
	}

	t.Logf("Direct context: 0x%016x", ctx.Raw())
	t.Logf("  Op=%d Flags=%d BufGroup=%d FD=%d",
		ctx.Op(), ctx.Flags(), ctx.BufGroup(), ctx.FD())
}

// TestSQEContextWithFlags demonstrates flag manipulation.
//
// Common SQE flags:
//   - IOSQE_FIXED_FILE: Use registered file index instead of fd
//   - IOSQE_IO_DRAIN: Wait for previous SQEs to complete
//   - IOSQE_IO_LINK: Link to next SQE (chain operations)
//   - IOSQE_ASYNC: Force async execution
//   - IOSQE_BUFFER_SELECT: Let kernel select buffer from group
//   - IOSQE_CQE_SKIP_SUCCESS: Don't generate CQE on success
func TestSQEContextWithFlags(t *testing.T) {
	// Create context with initial flags
	flags := uint8(uring.IOSQE_ASYNC | uring.IOSQE_IO_LINK)
	ctx := uring.PackDirect(uring.IORING_OP_WRITE, flags, 0, 10)

	if ctx.Flags() != flags {
		t.Errorf("Flags: got %d, want %d", ctx.Flags(), flags)
	}

	// WithFlags REPLACES flags (not OR) - useful for changing or clearing the
	// exact flag set.
	newFlags := uint8(uring.IOSQE_IO_DRAIN)
	ctx2 := ctx.WithFlags(newFlags)
	if ctx2.Flags() != newFlags {
		t.Errorf("WithFlags: got %d, want %d", ctx2.Flags(), newFlags)
	}

	// To add flags while preserving existing bits, use OrFlags.
	combinedFlags := flags | uring.IOSQE_IO_DRAIN
	ctx3 := ctx.OrFlags(uring.IOSQE_IO_DRAIN)
	if ctx3.Flags() != combinedFlags {
		t.Errorf("Combined flags: got %d, want %d", ctx3.Flags(), combinedFlags)
	}

	// Original unchanged (immutable)
	if ctx.Flags() != flags {
		t.Error("Original context was mutated")
	}

	t.Logf("Original flags: 0b%08b", ctx.Flags())
	t.Logf("Replaced flags: 0b%08b", ctx2.Flags())
	t.Logf("Combined flags: 0b%08b", ctx3.Flags())
}

// TestSQEContextIndirect demonstrates Indirect mode for full SQE access.
//
// Indirect mode stores a pointer to a 64-byte IndirectSQE structure.
// The ring populates the SQE fields internally when you submit operations.
// On completion, you recover the pointer to access the full SQE context.
//
// Use when: You need the complete SQE fields on completion (e.g., multi-shot ops).
func TestSQEContextIndirect(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get IndirectSQE from pool - these are 64-byte aligned
	indirect := ring.IndirectSQE()
	if indirect == nil {
		t.Fatal("Pool exhausted")
	}

	// Pack as Indirect context (pointer with mode bits)
	ctx := uring.PackIndirect(indirect)

	// Verify mode detection
	if !ctx.IsIndirect() {
		t.Error("Expected Indirect mode")
	}
	if ctx.IsDirect() || ctx.IsExtended() {
		t.Error("Should not be Direct or Extended")
	}

	// Extract pointer back - this is how completion handlers recover context
	ptr := ctx.IndirectSQE()
	if ptr != indirect {
		t.Error("Pointer round-trip failed")
	}

	// Return to pool after use
	ring.PutIndirectSQE(indirect)

	t.Logf("Indirect context: 0x%016x", ctx.Raw())
	t.Logf("  Mode bits: %d (1=Indirect)", ctx.Mode()>>62)
}

// TestSQEContextExtended demonstrates Extended mode for user data.
//
// Extended mode provides 128 bytes:
//   - 64 bytes: Full SQE (populated internally by ring operations)
//   - 64 bytes: Raw scalar user data
//
// The UserData field is [64]byte, giving you flexible storage for:
//   - Connection/request IDs
//   - Fixed-size tags
//   - Pointer-free application state snapshots
//
// Use when: You need to associate custom data with operations.
func TestSQEContextExtended(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get ExtSQE from the pool.
	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("Pool exhausted")
	}

	// Store user data (64 bytes available as [64]byte)
	// You can overlay structures or store raw bytes
	copy(ext.UserData[:8], []byte("CONN_001")) // Connection tag
	ext.UserData[8] = 0x42                     // Request type
	ext.UserData[9] = 0x01                     // Flags

	// Pack as Extended context
	ctx := uring.PackExtended(ext)

	// Verify mode detection
	if !ctx.IsExtended() {
		t.Error("Expected Extended mode")
	}
	if ctx.IsDirect() || ctx.IsIndirect() {
		t.Error("Should not be Direct or Indirect")
	}

	// On completion, extract the ExtSQE pointer
	ptr := ctx.ExtSQE()
	if ptr != ext {
		t.Error("Pointer round-trip failed")
	}

	// Access user data on completion
	tag := string(ptr.UserData[:8])
	if tag != "CONN_001" {
		t.Errorf("UserData tag: got %q, want CONN_001", tag)
	}
	if ptr.UserData[8] != 0x42 {
		t.Errorf("UserData[8]: got 0x%x, want 0x42", ptr.UserData[8])
	}

	// Return to pool
	ring.PutExtSQE(ext)

	t.Logf("Extended context: 0x%016x", ctx.Raw())
	t.Logf("  Mode bits: %d (2=Extended)", ctx.Mode()>>62)
	t.Logf("  UserData tag: %s", tag)
}

// TestCQEViewAccess demonstrates reading completion results.
//
// CQEView provides access to:
//   - Res: Result (bytes transferred, new fd, or -errno)
//   - Flags: CQE flags (IORING_CQE_F_*)
//   - Context extraction based on mode
func TestCQEViewAccess(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit a NOP with identifiable context
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 99, 77)
	if err := ring.Nop(ctx); err != nil {
		t.Fatalf("Nop: %v", err)
	}

	// Wait and examine CQE
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]

			// Extract operation info
			op := cqe.Op()
			if op != uring.IORING_OP_NOP {
				continue
			}

			t.Logf("CQE received:")
			t.Logf("  Res=%d (0 = success for NOP)", cqe.Res)
			t.Logf("  Flags=0x%x", cqe.Flags)
			t.Logf("  Op=%d BufGroup=%d FD=%d",
				cqe.Op(), cqe.BufGroup(), cqe.FD())

			// Verify context fields
			if cqe.BufGroup() != 99 {
				t.Errorf("BufGroup: got %d, want 99", cqe.BufGroup())
			}
			if cqe.FD() != 77 {
				t.Errorf("FD: got %d, want 77", cqe.FD())
			}

			return
		}
		b.Wait()
	}
	t.Error("NOP completion not received")
}
