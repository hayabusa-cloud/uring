// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux || darwin

package uring_test

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"code.hybscloud.com/uring"
)

// Seed a reachable full pool two turns before counter wrap. Move the counters
// and occupied-slot positions together; operations then use the public API.
func contextPoolsBeforeWrap(capacity int) *uring.ContextPools {
	pool := uring.NewContextPools(capacity)
	start := uint32(0) - 2*uint32(capacity)
	for _, name := range []string{"indirectQueue", "extendedQueue"} {
		queue := reflect.ValueOf(pool).Elem().FieldByName(name).Elem()
		(*atomic.Uint32)(queue.FieldByName("head").Addr().UnsafePointer()).Store(start)
		(*atomic.Uint32)(queue.FieldByName("tail").Addr().UnsafePointer()).Store(start + uint32(capacity))
		entries := queue.FieldByName("entries")
		for i := range capacity {
			entry := (*atomic.Uint64)(entries.Index(i).Addr().UnsafePointer())
			entry.Store(uint64(start+uint32(i))<<32 | uint64(i))
		}
	}
	return pool
}

func TestContextPoolsCapacityLimit(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("capacity that rounds beyond uint32 positions did not panic")
		}
	}()
	uring.NewContextPools(1<<31 + 1)
}

func TestContextPoolsCounterWrap(t *testing.T) {
	for _, capacity := range []int{1, 2, 4, 8, 32, 2048} {
		t.Run(fmt.Sprintf("Indirect/%d", capacity), func(t *testing.T) {
			pool := contextPoolsBeforeWrap(capacity)
			checkContextPoolWrap(t, capacity, pool.Indirect, pool.PutIndirect, pool.IndirectAvailable)
		})
		t.Run(fmt.Sprintf("Extended/%d", capacity), func(t *testing.T) {
			pool := contextPoolsBeforeWrap(capacity)
			checkContextPoolWrap(t, capacity, pool.Extended, pool.PutExtended, pool.ExtendedAvailable)
		})
	}
}

func checkContextPoolWrap[T any](t *testing.T, capacity int, borrow func() *T, release func(*T), available func() int) {
	t.Helper()
	for range 3 * capacity {
		slot := borrow()
		if slot == nil {
			t.Fatal("borrow from full pool returned nil")
		}
		release(slot)
		if got := available(); got != capacity {
			t.Fatalf("available = %d, want %d", got, capacity)
		}
	}
	slots := make([]*T, capacity)
	seen := make(map[*T]bool, capacity)
	for i := range slots {
		slot := borrow()
		if slot == nil || seen[slot] {
			t.Fatalf("slot %d: missing or duplicate borrow %p", i, slot)
		}
		slots[i], seen[slot] = slot, true
	}
	if borrow() != nil || available() != 0 {
		t.Fatal("exhausted pool still has a slot")
	}
	for i := len(slots) - 1; i >= 0; i-- {
		release(slots[i])
	}
	if got := available(); got != capacity {
		t.Fatalf("available after reverse returns = %d, want %d", got, capacity)
	}
}

func TestContextPoolsConcurrentCounterWrap(t *testing.T) {
	for _, capacity := range []int{8, 32, 2048} {
		t.Run(fmt.Sprintf("Indirect/%d", capacity), func(t *testing.T) {
			pool := contextPoolsBeforeWrap(capacity)
			checkContextPoolConcurrentWrap(t, capacity, pool.Indirect, pool.PutIndirect, pool.IndirectAvailable)
		})
		t.Run(fmt.Sprintf("Extended/%d", capacity), func(t *testing.T) {
			pool := contextPoolsBeforeWrap(capacity)
			checkContextPoolConcurrentWrap(t, capacity, pool.Extended, pool.PutExtended, pool.ExtendedAvailable)
		})
	}
}

func checkContextPoolConcurrentWrap[T any](t *testing.T, capacity int, borrow func() *T, release func(*T), available func() int) {
	t.Helper()
	owners := make(map[*T]*atomic.Bool, capacity)
	for range capacity {
		slot := borrow()
		if slot == nil || owners[slot] != nil {
			t.Fatal("initial pool has a missing or duplicate slot")
		}
		owners[slot] = new(atomic.Bool)
	}
	for slot := range owners {
		release(slot)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 2 * capacity {
				slot := borrow()
				for slot == nil {
					runtime.Gosched()
					slot = borrow()
				}
				owner := owners[slot]
				if owner == nil || !owner.CompareAndSwap(false, true) {
					t.Errorf("concurrent duplicate or foreign slot %p", slot)
					return
				}
				runtime.Gosched()
				owner.Store(false)
				release(slot)
			}
		})
	}
	workers.Wait()
	if got := available(); got != capacity {
		t.Fatalf("available after concurrent returns = %d, want %d", got, capacity)
	}
	for range capacity {
		slot := borrow()
		owner := owners[slot]
		if owner == nil || !owner.CompareAndSwap(false, true) {
			t.Fatalf("final pool has a missing or duplicate slot %p", slot)
		}
	}
	if borrow() != nil {
		t.Fatal("final pool did not exhaust")
	}
	for slot := range owners {
		release(slot)
	}
}

func BenchmarkContextPools(b *testing.B) {
	b.Run("Indirect", func(b *testing.B) {
		pool := uring.NewContextPools(2048)
		b.ReportAllocs()
		for b.Loop() {
			pool.PutIndirect(pool.Indirect())
		}
	})
	b.Run("Extended", func(b *testing.B) {
		pool := uring.NewContextPools(2048)
		b.ReportAllocs()
		for b.Loop() {
			pool.PutExtended(pool.Extended())
		}
	})
}
