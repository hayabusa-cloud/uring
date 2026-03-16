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

// BundleIterator iterates over buffers consumed in a bundle receive operation.
// Bundle receives allow receiving multiple buffers in a single syscall, with
// data spanning the logical sequence of buffer IDs starting at the CQE's first
// ID.
//
// The iterator handles buffer ring wrap-around using the ring mask.
type BundleIterator struct {
	bufBacking unsafe.Pointer // Base address of buffer backing memory
	bufSize    int            // Size of each buffer
	startID    uint16         // Starting buffer ID from CQE
	count      int            // Number of buffers consumed
	gidOffset  uint16         // Group ID offset for Recycle
	group      uint16         // Buffer group index for Recycle
	ringMask   uint16         // Buffer ring mask (entries - 1) for wrap-around
	totalBytes int32          // Total bytes received (from CQE result)
}

// bufAt returns the buffer slice at the given logical position within the bundle.
// Handles ring wrap-around and partial last buffer.
//
//go:nosplit
func (it *BundleIterator) bufAt(pos int) []byte {
	id := (it.startID + uint16(pos)) & it.ringMask
	bufPtr := unsafe.Add(it.bufBacking, int(id)*it.bufSize)
	size := it.bufSize
	if rem := int(it.totalBytes) - pos*it.bufSize; rem < size {
		size = rem
	}
	return unsafe.Slice((*byte)(bufPtr), size)
}

// NewBundleIterator creates an iterator for the buffers consumed by a bundle CQE.
//
// Parameters:
//   - cqe: the CQE from a bundle receive operation
//   - bufBacking: backing memory for the full ring, such as the slice returned by AlignedMem
//   - bufSize: size of each buffer in the ring
//   - ringEntries: number of entries in the buffer ring; must be a power of two
//
// bufBacking must remain alive for the iterator's lifetime and must cover at
// least bufSize*ringEntries bytes.
//
// Returns nil if the CQE indicates no data was received or if the constructor
// arguments are invalid.
func NewBundleIterator(cqe CQEView, bufBacking []byte, bufSize int, ringEntries int) *BundleIterator {
	if cqe.Res <= 0 || bufSize <= 0 || ringEntries <= 0 || ringEntries&(ringEntries-1) != 0 {
		return nil
	}
	if ringEntries > maxInt/bufSize || len(bufBacking) < ringEntries*bufSize {
		return nil
	}
	return newBundleIterator(cqe, unsafe.Pointer(unsafe.SliceData(bufBacking)), bufSize, uint16(ringEntries-1))
}

func newBundleIterator(cqe CQEView, bufBacking unsafe.Pointer, bufSize int, ringMask uint16) *BundleIterator {
	startID, count := cqe.BundleBuffers(bufSize)
	if count == 0 {
		return nil
	}

	return &BundleIterator{
		bufBacking: bufBacking,
		bufSize:    bufSize,
		startID:    startID,
		count:      count,
		ringMask:   ringMask,
		totalBytes: cqe.Res,
	}
}

// Count returns the number of buffers consumed in this bundle.
func (it *BundleIterator) Count() int {
	return it.count
}

// TotalBytes returns the total bytes received in this bundle.
func (it *BundleIterator) TotalBytes() int {
	return int(it.totalBytes)
}

// All returns an iterator function for use with Go 1.23+ range-over-func.
// Each iteration yields one buffer from the bundle.
//
// Usage:
//
//	for buf := range iter.All() {
//	    process(buf)
//	}
func (it *BundleIterator) All() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for pos := range it.count {
			if !yield(it.bufAt(pos)) {
				return
			}
		}
	}
}

// AllWithSlotID returns an iterator that yields both buffer data and masked
// ring slot ID.
// Useful when you need to track which ring slots were consumed.
//
// Usage:
//
//	for id, buf := range iter.AllWithSlotID() {
//	    fmt.Printf("Slot ID %d: %d bytes\n", id, len(buf))
//	}
func (it *BundleIterator) AllWithSlotID() iter.Seq2[uint16, []byte] {
	return func(yield func(uint16, []byte) bool) {
		for pos := range it.count {
			if !yield(it.SlotID(pos), it.bufAt(pos)) {
				return
			}
		}
	}
}

// SlotID returns the masked ring slot ID at the given index in the bundle.
// Handles ring wrap-around automatically.
// Index must be in range [0, Count()).
//
//go:nosplit
func (it *BundleIterator) SlotID(index int) uint16 {
	return (it.startID + uint16(index)) & it.ringMask
}

// Buffer returns the buffer at the given index without advancing the iterator.
// Index must be in range [0, Count()).
// The last buffer may be partial.
func (it *BundleIterator) Buffer(index int) []byte {
	if index < 0 || index >= it.count {
		return nil
	}
	return it.bufAt(index)
}

// Collect returns all buffers as a slice.
// This allocates a new slice; for zero-allocation iteration, use All().
func (it *BundleIterator) Collect() [][]byte {
	result := make([][]byte, 0, it.count)
	for buf := range it.All() {
		result = append(result, buf)
	}
	return result
}

// CopyTo copies all bundle data to the destination slice.
// Returns the number of bytes copied.
func (it *BundleIterator) CopyTo(dst []byte) int {
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

// Recycle returns all consumed buffers to the buffer ring via provide
// and commits them with advance. This MUST be called after the bundle data has
// been fully processed to prevent buffer ring entry leaks.
//
// Recycle is single-threaded: do not call it concurrently with another
// Recycle on the same Uring, or with any other path that can race with buffer
// ring provide/advance.
//
// The group info (gidOffset, group) is captured at construction time.
func (it *BundleIterator) Recycle(ur *Uring) {
	for pos := range it.count {
		id := it.SlotID(pos)
		bufPtr := unsafe.Add(it.bufBacking, int(id)*it.bufSize)
		ur.bufferRings.provide(ur.ioUring, it.gidOffset, it.group, bufPtr, it.bufSize, uint32(id))
	}
	ur.bufferRings.advance(ur.ioUring)
}
