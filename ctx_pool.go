// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"math"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iobuf"
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

// ContextPools holds aligned pools for IndirectSQE and ExtSQE.
type ContextPools struct {
	indirect      []IndirectSQE
	indirectQueue *contextPoolQueue

	extended      []ExtSQE
	extendedQueue *contextPoolQueue
}

func NewContextPools(capacity int) *ContextPools {
	n := normalizeContextPoolCapacity(capacity)

	indirectMem := iobuf.AlignedMem(n*int(unsafe.Sizeof(IndirectSQE{})), iobuf.PageSize)
	indirect := unsafe.Slice((*IndirectSQE)(unsafe.Pointer(unsafe.SliceData(indirectMem))), n)

	extendedMem := iobuf.AlignedMem(n*int(unsafe.Sizeof(ExtSQE{})), iobuf.PageSize)
	extended := unsafe.Slice((*ExtSQE)(unsafe.Pointer(unsafe.SliceData(extendedMem))), n)

	return &ContextPools{
		indirect:      indirect,
		indirectQueue: newContextPoolQueue(n),
		extended:      extended,
		extendedQueue: newContextPoolQueue(n),
	}
}

func normalizeContextPoolCapacity(capacity int) int {
	if capacity < 1 || capacity > math.MaxUint32 {
		panic("capacity must be between 1 and MaxUint32")
	}
	capacity--
	capacity |= capacity >> 1
	capacity |= capacity >> 2
	capacity |= capacity >> 4
	capacity |= capacity >> 8
	capacity |= capacity >> 16
	capacity++
	return capacity
}

//go:nosplit
func (p *ContextPools) GetIndirect() *IndirectSQE {
	entry, ok := p.indirectQueue.get()
	if !ok {
		return nil
	}
	return &p.indirect[entry]
}

//go:nosplit
func (p *ContextPools) PutIndirect(indirect *IndirectSQE) {
	p.indirectQueue.put(p.indirectIndex(indirect))
}

//go:nosplit
func (p *ContextPools) GetExtended() *ExtSQE {
	entry, ok := p.extendedQueue.get()
	if !ok {
		return nil
	}
	return &p.extended[entry]
}

//go:nosplit
func (p *ContextPools) PutExtended(ext *ExtSQE) {
	p.extendedQueue.put(p.extendedIndex(ext))
}

func (p *ContextPools) IndirectAvailable() int {
	return p.indirectQueue.available()
}

func (p *ContextPools) ExtendedAvailable() int {
	return p.extendedQueue.available()
}

func (p *ContextPools) Capacity() int {
	return int(p.indirectQueue.capacity)
}

func (p *ContextPools) Reset() {
	p.indirectQueue.reset()
	p.extendedQueue.reset()
}

func (p *ContextPools) Init() {
	p.Reset()
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
		if t == h+q.capacity {
			panic("context pool overflow")
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
	return uint32((uintptr(unsafe.Pointer(ext)) - uintptr(base)) / unsafe.Sizeof(ExtSQE{}))
}
