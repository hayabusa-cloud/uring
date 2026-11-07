// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

import (
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/spin"
)

// Entries pair a uint32 position with a 31-bit slot index or an empty marker.
const contextPoolEntryEmpty = 1 << 31

type contextPoolQueue struct {
	entries  []atomic.Uint64
	head     atomic.Uint32
	tail     atomic.Uint32
	capacity uint32
	mask     uint32
}

// ContextPools holds independent pools of IndirectSQE and ExtSQE contexts.
// The zero value is not usable; construct pools with NewContextPools.
// All methods except Reset are safe for concurrent use.
type ContextPools struct {
	indirect      []IndirectSQE
	indirectQueue *contextPoolQueue

	extended      []pooledExtSQE
	extendedQueue *contextPoolQueue
}

// NewContextPools creates pooled IndirectSQE and ExtSQE contexts with the given
// per-pool capacity. New pools are ready for immediate use.
// Capacity is rounded up to a power of two. Values outside [1, 1<<31] panic.
func NewContextPools(capacity int) *ContextPools {
	n := alignPoolCapacity(capacity)
	return &ContextPools{
		indirect:      newIndirectSQEPool(n),
		indirectQueue: newContextPoolQueue(n),
		extended:      make([]pooledExtSQE, n),
		extendedQueue: newContextPoolQueue(n),
	}
}

func alignPoolCapacity(capacity int) int {
	if capacity < 1 || capacity > 1<<31 {
		panic("uring: capacity must be between 1 and 1<<31")
	}
	return roundToPowerOf2(capacity)
}

// Indirect borrows an IndirectSQE from the pool. Returns nil if exhausted.
// The slot retains its previous contents; initialize it before use.
// Return it with PutIndirect after all operations and borrowed access finish.
//
//go:nosplit
func (p *ContextPools) Indirect() *IndirectSQE {
	entry, ok := p.indirectQueue.get()
	if !ok {
		return nil
	}
	return &p.indirect[entry]
}

// PutIndirect returns an IndirectSQE without clearing its contents.
// It must be an outstanding borrow from this pool with no pending operations
// or borrowed access. Do not access or return it again after this call.
//
//go:nosplit
func (p *ContextPools) PutIndirect(indirect *IndirectSQE) {
	p.indirectQueue.put(p.indirectIndex(indirect))
}

// Extended borrows a cleared ExtSQE from the pool. Returns nil if exhausted.
// Return it with PutExtended after all operations and borrowed access finish.
//
//go:nosplit
func (p *ContextPools) Extended() *ExtSQE {
	entry, ok := p.extendedQueue.get()
	if !ok {
		return nil
	}
	return &p.extended[entry].ext
}

// PutExtended clears an ExtSQE's SQE, UserData, and sidecar anchors, then returns
// it to the pool. It must be an outstanding borrow from this pool with no pending
// operations or borrowed access. Do not access or return it again afterward.
//
//go:nosplit
func (p *ContextPools) PutExtended(ext *ExtSQE) {
	i := p.extendedIndex(ext)
	slot := &p.extended[i]
	slot.ext.SQE = ioUringSqe{}
	slot.ext.UserData = [64]byte{}
	slot.anchors.clear()
	p.extendedQueue.put(i)
}

// IndirectAvailable returns the number of IndirectSQE slots available.
// The count is exact only when no borrow or return overlaps the call.
// Use Indirect to acquire a slot; this count does not reserve one.
func (p *ContextPools) IndirectAvailable() int {
	return p.indirectQueue.available()
}

// ExtendedAvailable returns the number of ExtSQE slots available.
// The count is exact only when no borrow or return overlaps the call.
// Use Extended to acquire a slot; this count does not reserve one.
func (p *ContextPools) ExtendedAvailable() int {
	return p.extendedQueue.available()
}

// Capacity returns the per-pool slot count.
func (p *ContextPools) Capacity() int {
	return int(p.indirectQueue.capacity)
}

// Reset scrubs pooled slot state and reinitializes both pool queues, making all
// slots available again. Caller must stop all pool access and finish operations
// using its contexts before calling Reset. Reset invalidates outstanding borrows;
// do not access or return those contexts afterward. Reset is not safe for
// concurrent use.
func (p *ContextPools) Reset() {
	for i := range p.indirect {
		p.indirect[i] = IndirectSQE{}
	}
	for i := range p.extended {
		p.extended[i].ext.SQE = ioUringSqe{}
		p.extended[i].ext.UserData = [64]byte{}
		p.extended[i].anchors.clear()
	}
	p.indirectQueue.reset()
	p.extendedQueue.reset()
}

func newContextPoolQueue(capacity int) *contextPoolQueue {
	q := &contextPoolQueue{
		entries:  make([]atomic.Uint64, capacity),
		capacity: uint32(capacity),
		mask:     uint32(capacity - 1),
	}
	q.reset()
	return q
}

func (q *contextPoolQueue) reset() {
	for i := range q.entries {
		q.entries[i].Store(uint64(i)<<32 | uint64(i))
	}
	q.head.Store(0)
	q.tail.Store(q.capacity)
}

//go:nosplit
func (q *contextPoolQueue) get() (uint32, bool) {
	sw := spin.Wait{}
	for {
		h, t := q.head.Load(), q.tail.Load()
		i := h & q.mask
		e := q.entries[i].Load()

		if h != q.head.Load() {
			sw.Once()
			continue
		}
		if h == t {
			return 0, false
		}

		// The next producer position wraps with the uint32 queue counters.
		empty := uint64(h+q.capacity)<<32 | contextPoolEntryEmpty
		if e == empty {
			q.head.CompareAndSwap(h, h+1)
			sw.Once()
			continue
		}
		ok := q.entries[i].CompareAndSwap(e, empty)
		q.head.CompareAndSwap(h, h+1)
		if ok {
			return uint32(e & uint64(q.mask)), true
		}
		sw.Once()
	}
}

//go:nosplit
func (q *contextPoolQueue) put(entry uint32) {
	sw := spin.Wait{}
	e := uint64(entry)
	for {
		h, t := q.head.Load(), q.tail.Load()
		if t != q.tail.Load() {
			sw.Once()
			continue
		}
		if h != q.head.Load() {
			sw.Once()
			continue
		}
		if t == h+q.capacity {
			panic("uring: context pool overflow")
		}

		i := t & q.mask
		position := uint64(t) << 32
		ok := q.entries[i].CompareAndSwap(position|contextPoolEntryEmpty, position|e)
		q.tail.CompareAndSwap(t, t+1)
		if ok {
			return
		}
		sw.Once()
	}
}

func (q *contextPoolQueue) available() int {
	return int(q.tail.Load() - q.head.Load())
}

func (p *ContextPools) indirectIndex(indirect *IndirectSQE) uint32 {
	base := unsafe.Pointer(unsafe.SliceData(p.indirect))
	return uint32((uintptr(unsafe.Pointer(indirect)) - uintptr(base)) / unsafe.Sizeof(IndirectSQE{}))
}

func (p *ContextPools) extendedIndex(ext *ExtSQE) uint32 {
	base := unsafe.Pointer(unsafe.SliceData(p.extended))
	return uint32((uintptr(unsafe.Pointer(ext)) - uintptr(base)) / unsafe.Sizeof(pooledExtSQE{}))
}
