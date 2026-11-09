// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

// Refactored from code.hybscloud.com/sox.

package uring

import (
	"iter"
	"unsafe"
)

const maxInt = int(^uint(0) >> 1)

// BundleIterator visits the buffers consumed by one bundle receive CQE in
// publication order, starting at the CQE's first buffer ID.
// Managed iterators preserve that order across out-of-order recycling.
//
// The iterator and its buffer slices borrow the original storage. Copying an
// iterator or collecting its slices does not extend the buffers' lifetime.
// Finish all borrowed access before recycling buffers or tearing down the ring.
type BundleIterator struct {
	bufBacking   unsafe.Pointer    // Base address of buffer backing memory
	recycleOwner *uringBufferRings // Ring manager authorized to recycle this iterator
	nextIDs      []uint16          // Publication successors; nil for consecutive-ID layout
	bufSize      int               // Size of each buffer
	count        int               // Number of buffers consumed
	ringIndex    int               // Dense ring index for Recycle; -1 for free iterators
	totalBytes   int32             // Total bytes received (from CQE result)
	startID      uint16            // Starting buffer ID from CQE
	group        uint16            // Exact registered buffer group for Recycle
	ringMask     uint16            // Buffer ring mask (entries - 1) for wrap-around
}

// bufAt returns the buffer slice at the given logical position within the bundle.
// Handles ring wrap-around and partial last buffer.
//
//go:nosplit
func (it BundleIterator) bufAt(pos int) []byte {
	return it.buffer(it.SlotID(pos), pos)
}

func (it BundleIterator) buffer(id uint16, pos int) []byte {
	bufPtr := unsafe.Add(it.bufBacking, int(id)*it.bufSize)
	size := it.bufSize
	if rem := int(it.totalBytes) - pos*it.bufSize; rem < size {
		size = rem
	}
	return unsafe.Slice((*byte)(bufPtr), size)
}

func (it BundleIterator) nextID(id uint16) uint16 {
	if it.nextIDs != nil {
		return it.nextIDs[id]
	}
	return (id + 1) & it.ringMask
}

// NewBundleIterator creates an iterator for the buffers consumed by a bundle CQE.
//
// Parameters:
//   - cqe: the CQE from a bundle receive operation
//   - bufBacking: backing memory for the full ring, such as the slice returned by AlignedMem
//   - bufSize: size of each buffer in the ring
//   - ringEntries: number of entries in the buffer ring; must be a power of two less than 65536
//
// bufBacking must remain alive for the iterator's lifetime and must cover at
// least bufSize*ringEntries bytes.
// This constructor requires buffers to have been published in consecutive ID
// order modulo ringEntries, with buffer ID i stored at offset i*bufSize.
// A CQE alone cannot recover arbitrary publication order. For package-managed
// rings, use [Uring.BundleIterator], which retains the actual order.
//
// The returned iterator is read-only and has no Uring recycle authority.
// Recycle returns ErrInvalidParam for it. Use [Uring.BundleIterator] when the
// consumed buffers belong to a package-managed ring and must be recycled.
//
// Returns a zero BundleIterator and false if the CQE indicates no data was
// received or if the constructor arguments are invalid.
func NewBundleIterator(cqe CQEView, bufBacking []byte, bufSize int, ringEntries int) (BundleIterator, bool) {
	if cqe.Res <= 0 || bufSize <= 0 || ringEntries <= 0 || ringEntries >= 1<<16 || ringEntries&(ringEntries-1) != 0 {
		return BundleIterator{}, false
	}
	if ringEntries > maxInt/bufSize || len(bufBacking) < ringEntries*bufSize {
		return BundleIterator{}, false
	}
	return newBundleIterator(cqe, unsafe.Pointer(unsafe.SliceData(bufBacking)), bufSize, uint16(ringEntries-1))
}

func newBundleIterator(cqe CQEView, bufBacking unsafe.Pointer, bufSize int, ringMask uint16) (BundleIterator, bool) {
	startID, count := cqe.BundleBuffers(bufSize)
	if count == 0 {
		return BundleIterator{}, false
	}

	return BundleIterator{
		bufBacking: bufBacking,
		bufSize:    bufSize,
		count:      count,
		ringIndex:  -1,
		totalBytes: cqe.Res,
		startID:    startID,
		ringMask:   ringMask,
	}, true
}

// Count returns the number of buffers consumed in this bundle.
func (it BundleIterator) Count() int {
	return it.count
}

// TotalBytes returns the total bytes received in this bundle.
func (it BundleIterator) TotalBytes() int {
	return int(it.totalBytes)
}

// All yields borrowed buffer slices in publication order.
//
// Usage:
//
//	for buf := range iter.All() {
//	    process(buf)
//	}
func (it BundleIterator) All() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		id := it.startID & it.ringMask
		for pos := range it.count {
			if !yield(it.buffer(id, pos)) {
				return
			}
			if pos+1 < it.count {
				id = it.nextID(id)
			}
		}
	}
}

// AllWithSlotID yields each buffer ID and its borrowed data in publication order.
//
// Usage:
//
//	for id, buf := range iter.AllWithSlotID() {
//	    fmt.Printf("Slot ID %d: %d bytes\n", id, len(buf))
//	}
func (it BundleIterator) AllWithSlotID() iter.Seq2[uint16, []byte] {
	return func(yield func(uint16, []byte) bool) {
		id := it.startID & it.ringMask
		for pos := range it.count {
			if !yield(id, it.buffer(id, pos)) {
				return
			}
			if pos+1 < it.count {
				id = it.nextID(id)
			}
		}
	}
}

// SlotID returns the buffer ID at the given index in the bundle.
// Managed iterators traverse publication order in O(index) time. Use
// AllWithSlotID for O(Count()) sequential iteration.
// Index must be in range [0, Count()).
//
//go:nosplit
func (it BundleIterator) SlotID(index int) uint16 {
	if it.nextIDs != nil {
		id := it.startID
		for range index {
			id = it.nextIDs[id]
		}
		return id
	}
	return (it.startID + uint16(index)) & it.ringMask
}

// Buffer returns a borrowed slice at index, or nil if index is outside
// [0, Count()). The last buffer may be partial.
func (it BundleIterator) Buffer(index int) []byte {
	if index < 0 || index >= it.count {
		return nil
	}
	return it.bufAt(index)
}

// Collect returns borrowed buffer slices in publication order. It allocates
// the outer slice for a nonempty bundle; payload bytes remain in the original
// buffers. Use [BundleIterator.All] to iterate without allocating a result slice,
// or [BundleIterator.CopyTo] to copy payload bytes into caller-owned storage.
func (it BundleIterator) Collect() [][]byte {
	result := make([][]byte, 0, it.count)
	for buf := range it.All() {
		result = append(result, buf)
	}
	return result
}

// CopyTo copies bundle data into dst in publication order and returns
// min(len(dst), TotalBytes()). Copy into separate caller-owned storage to keep
// the bytes after recycling the buffers.
func (it BundleIterator) CopyTo(dst []byte) int {
	copied := 0
	for buf := range it.All() {
		n := copy(dst[copied:], buf)
		copied += n
		if copied >= len(dst) {
			break // early exit; copy already clamps to remaining dst capacity
		}
	}
	return copied
}

// Recycle returns all consumed buffers to the package-managed buffer ring and
// publishes them for kernel reuse.
//
// Call Recycle after the bundle data has been fully processed and exactly once
// across all copies of an iterator. After Recycle returns, do not read any
// buffer yielded by the iterator or one of its copies; ownership of every
// consumed slot has returned to the kernel.
//
// Recycle is single-threaded: do not call it concurrently with another
// Recycle on the same Uring, or with any other path that can race with buffer
// ring provide/advance.
//
// Recycle returns ErrInvalidParam when the iterator came from
// NewBundleIterator or when ur is not the Uring that created the iterator.
func (it BundleIterator) Recycle(ur *Uring) error {
	if it.recycleOwner == nil || ur == nil || ur.bufferRings != it.recycleOwner ||
		it.ringIndex < 0 || it.ringIndex >= len(it.recycleOwner.gids) ||
		it.recycleOwner.gids[it.ringIndex] != it.group {
		return ErrInvalidParam
	}
	id := it.startID & it.ringMask
	for pos := range it.count {
		bufPtr := unsafe.Add(it.bufBacking, int(id)*it.bufSize)
		it.recycleOwner.provide(ur.ioUring, it.ringIndex, bufPtr, it.bufSize, uint32(id))
		if pos+1 < it.count {
			id = it.nextID(id)
		}
	}
	it.recycleOwner.advance(ur.ioUring)
	return nil
}
