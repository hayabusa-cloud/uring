// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

import (
	"math"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/spin"
)

const (
	contextPoolEntryEmpty    = 1 << 62
	contextPoolEntryTurnMask = contextPoolEntryEmpty>>32 - 1
)

type contextPoolQueue struct {
	entries  []atomic.Uint64
	head     atomic.Uint32
	tail     atomic.Uint32
	capacity uint32
	mask     uint32
}

// ContextPools holds pooled IndirectSQE and ExtSQE contexts.
// IndirectSQE slots use explicit aligned backing; extended slots pair each
// ExtSQE with adjacent GC-visible sidecar anchors.
type ContextPools struct {
	indirect      []IndirectSQE
	indirectQueue *contextPoolQueue

	extended      []pooledExtSQE
	extendedQueue *contextPoolQueue
}

// NewContextPools creates pooled IndirectSQE and ExtSQE contexts with the given
// per-pool capacity. New pools are ready for immediate use.
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
	if capacity < 1 || capacity > math.MaxUint32 {
		panic("uring: capacity must be between 1 and MaxUint32")
	}
	return roundToPowerOf2(capacity)
}

// Indirect borrows an IndirectSQE from the pool. Returns nil if exhausted.
//
//go:nosplit
func (p *ContextPools) Indirect() *IndirectSQE {
	entry, ok := p.indirectQueue.get()
	if !ok {
		return nil
	}
	return &p.indirect[entry]
}

// PutIndirect returns an IndirectSQE to the pool.
//
//go:nosplit
func (p *ContextPools) PutIndirect(indirect *IndirectSQE) {
	p.indirectQueue.put(p.indirectIndex(indirect))
}

// Extended borrows an ExtSQE from the pool. Returns nil if exhausted.
//
//go:nosplit
func (p *ContextPools) Extended() *ExtSQE {
	entry, ok := p.extendedQueue.get()
	if !ok {
		return nil
	}
	return &p.extended[entry].ext
}

// PutExtended returns an ExtSQE to the pool and clears its sidecar anchors.
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
func (p *ContextPools) IndirectAvailable() int {
	return p.indirectQueue.available()
}

// ExtendedAvailable returns the number of ExtSQE slots available.
func (p *ContextPools) ExtendedAvailable() int {
	return p.extendedQueue.available()
}

// Capacity returns the per-pool slot count.
func (p *ContextPools) Capacity() int {
	return int(p.indirectQueue.capacity)
}

// Reset scrubs pooled slot state and reinitializes both pool queues, making all
// slots available again.
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
		q.entries[i].Store(uint64(i))
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

		nextTurn := (h/q.capacity + 1) & contextPoolEntryTurnMask
		if e == q.empty(nextTurn) {
			q.head.CompareAndSwap(h, h+1)
			sw.Once()
			continue
		}
		ok := q.entries[i].CompareAndSwap(e, q.empty(nextTurn))
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

		turn := (t / q.capacity) & contextPoolEntryTurnMask
		i := t & q.mask
		ok := q.entries[i].CompareAndSwap(q.empty(turn), e)
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

func (q *contextPoolQueue) empty(turn uint32) uint64 {
	return contextPoolEntryEmpty | uint64(turn&contextPoolEntryTurnMask)
}

func (p *ContextPools) indirectIndex(indirect *IndirectSQE) uint32 {
	base := unsafe.Pointer(unsafe.SliceData(p.indirect))
	return uint32((uintptr(unsafe.Pointer(indirect)) - uintptr(base)) / unsafe.Sizeof(IndirectSQE{}))
}

func (p *ContextPools) extendedIndex(ext *ExtSQE) uint32 {
	base := unsafe.Pointer(unsafe.SliceData(p.extended))
	return uint32((uintptr(unsafe.Pointer(ext)) - uintptr(base)) / unsafe.Sizeof(pooledExtSQE{}))
}
