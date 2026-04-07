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

// =============================================================================
// CQEView Method Coverage Tests
// =============================================================================

func TestCQEViewFlags(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit NOP with identifiable context
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 42, 77)
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

			// Test CQEView methods
			if cqe.BufGroup() != 42 {
				t.Errorf("BufGroup: got %d, want 42", cqe.BufGroup())
			}

			// Test flag methods (should be false for NOP)
			if cqe.HasMore() {
				t.Error("HasMore should be false for NOP")
			}
			if cqe.HasBuffer() {
				t.Error("HasBuffer should be false for NOP")
			}
			if cqe.SocketNonEmpty() {
				t.Error("SocketNonEmpty should be false for NOP")
			}
			if cqe.IsNotification() {
				t.Error("IsNotification should be false for NOP")
			}
			if cqe.HasBufferMore() {
				t.Error("HasBufferMore should be false for NOP")
			}

			// Test Context() method
			rawCtx := cqe.Context()
			if rawCtx.Op() != uring.IORING_OP_NOP {
				t.Errorf("Context().Op(): got %d, want %d", rawCtx.Op(), uring.IORING_OP_NOP)
			}

			// Test BufID (should be 0 when no buffer)
			_ = cqe.BufID() // Just exercise the method

			// Test FullSQE and Extended (Direct mode)
			if cqe.FullSQE() {
				t.Error("FullSQE should be false for Direct mode")
			}
			if cqe.Extended() {
				t.Error("Extended should be false for Direct mode")
			}

			return // Test passed
		}
		b.Wait()
	}
	t.Fatal("NOP completion not received")
}
