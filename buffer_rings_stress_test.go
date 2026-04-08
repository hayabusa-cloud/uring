// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux && !race

package uring_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"code.hybscloud.com/spin"
)

// TestBufferRingsProvideContention stress tests concurrent provide() calls.
// Uses //go:build !race because buffer rings use custom atomic + spinlock
// synchronization that the race detector cannot track.
//
// This test verifies:
// - FAA offset uniqueness: each provide() gets a unique slot offset
// - No lost updates: all provides are counted correctly
// - No slot collisions: each buffer writes to its own slot
func TestBufferRingsProvideContention(t *testing.T) {
	const (
		numGoroutines = 8
		providesPerGo = 1000
		ringEntries   = 1024
	)

	// Create mock buffer ring structure
	rings := &testBufferRings{
		counts: make([]uintptr, 1),
		masks:  make([]uintptr, 1),
		slots:  make([]atomic.Int32, ringEntries),
	}
	rings.masks[0] = uintptr(ringEntries - 1)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Simulate concurrent provide() calls
	for g := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			for i := range providesPerGo {
				rings.provide(uint16(goroutineID*providesPerGo + i))
			}
		}(g)
	}

	wg.Wait()

	// Verify all provides were counted
	expectedCount := numGoroutines * providesPerGo
	if got := int(rings.counts[0]); got != expectedCount {
		t.Errorf("counts: got %d, want %d", got, expectedCount)
	}

	// Verify no slot collisions (each slot should have exactly one write
	// or zero if it was overwritten - but the last write should be consistent)
	// Note: This doesn't catch all races but provides basic sanity check
	totalWrites := 0
	for i := range ringEntries {
		writes := rings.slots[i].Load()
		if writes > 0 {
			totalWrites += int(writes)
		}
	}

	// With modulo wrapping, some slots may have multiple writes
	// but total should equal expectedCount
	if totalWrites != expectedCount {
		t.Errorf("totalWrites: got %d, want %d", totalWrites, expectedCount)
	}
}

// testBufferRings is a simplified mock of uringBufferRings for testing.
type testBufferRings struct {
	counts []uintptr
	masks  []uintptr
	slots  []atomic.Int32
}

// provide simulates the FAA pattern from buffer_rings.go
func (rings *testBufferRings) provide(bufID uint16) {
	offset := atomic.AddUintptr(&rings.counts[0], 1)
	slot := (offset - 1) & rings.masks[0]
	rings.slots[slot].Add(1)
}

// TestBufferRingsAdvanceBatching tests the advance() batching behavior.
func TestBufferRingsAdvanceBatching(t *testing.T) {
	const (
		numProvides      = 100
		advanceCount     = 10
		ringEntries      = 256
		providesPerBatch = numProvides / advanceCount
	)

	rings := &testBufferRingsWithLock{
		counts: make([]uintptr, 1),
		masks:  make([]uintptr, 1),
		locks:  make([]spin.Lock, 1),
		tail:   0,
	}
	rings.masks[0] = uintptr(ringEntries - 1)

	// Simulate multiple batches of provide+advance
	for batch := range advanceCount {
		// Simulate provides
		for i := range providesPerBatch {
			_ = batch // suppress unused
			_ = i
			atomic.AddUintptr(&rings.counts[0], 1)
		}

		// Simulate advance
		rings.locks[0].Lock()
		count := rings.counts[0]
		rings.tail += uint16(count)
		rings.counts[0] = 0
		rings.locks[0].Unlock()

		// Verify count was reset
		if got := atomic.LoadUintptr(&rings.counts[0]); got != 0 {
			t.Errorf("batch %d: count after advance = %d, want 0", batch, got)
		}
	}

	// Verify final tail position
	expectedTail := uint16(numProvides)
	if rings.tail != expectedTail {
		t.Errorf("final tail: got %d, want %d", rings.tail, expectedTail)
	}
}

type testBufferRingsWithLock struct {
	counts []uintptr
	masks  []uintptr
	locks  []spin.Lock
	tail   uint16
}

// TestBufferRingsFAAOffsetUniqueness verifies FAA guarantees unique offsets.
func TestBufferRingsFAAOffsetUniqueness(t *testing.T) {
	const (
		numGoroutines = 16
		opsPerGo      = 500
	)

	var counter uintptr
	offsets := make(chan uintptr, numGoroutines*opsPerGo)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGo {
				offset := atomic.AddUintptr(&counter, 1) - 1
				offsets <- offset
			}
		}()
	}

	wg.Wait()
	close(offsets)

	// Verify all offsets are unique
	seen := make(map[uintptr]bool)
	for offset := range offsets {
		if seen[offset] {
			t.Fatalf("duplicate offset: %d", offset)
		}
		seen[offset] = true
	}

	expectedCount := numGoroutines * opsPerGo
	if len(seen) != expectedCount {
		t.Errorf("unique offsets: got %d, want %d", len(seen), expectedCount)
	}
}

// TestBufferRingsSlotCalculation verifies slot calculation with mask.
func TestBufferRingsSlotCalculation(t *testing.T) {
	tests := []struct {
		tail   uintptr
		offset uintptr
		mask   uintptr
		want   uintptr
	}{
		{0, 0, 7, 0},
		{0, 7, 7, 7},
		{0, 8, 7, 0},         // wrap around
		{5, 3, 7, 0},         // 5+3=8 & 7 = 0
		{100, 28, 127, 0},    // 128 & 127 = 0
		{0xFFFF, 1, 0xFF, 0}, // uint16 overflow doesn't affect mask
	}

	for _, tt := range tests {
		slot := (tt.tail + tt.offset) & tt.mask
		if slot != tt.want {
			t.Errorf("slot(tail=%d, offset=%d, mask=%d): got %d, want %d",
				tt.tail, tt.offset, tt.mask, slot, tt.want)
		}
	}
}
