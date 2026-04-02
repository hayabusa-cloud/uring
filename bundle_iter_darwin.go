// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"iter"
)

// BundleIterator iterates over buffers consumed in a bundle receive operation.
type BundleIterator struct{}

// NewBundleIterator creates an iterator (stub on Darwin).
func NewBundleIterator(cqe CQEView, bufBacking []byte, bufSize int, ringEntries int) (BundleIterator, bool) {
	return BundleIterator{}, false
}

// Count returns the number of buffers consumed.
func (it BundleIterator) Count() int { return 0 }

// TotalBytes returns the total bytes received.
func (it BundleIterator) TotalBytes() int { return 0 }

// All returns an iterator for range-over-func.
func (it BundleIterator) All() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {}
}

// AllWithSlotID returns an iterator yielding masked ring slot ID and data.
func (it BundleIterator) AllWithSlotID() iter.Seq2[uint16, []byte] {
	return func(yield func(uint16, []byte) bool) {}
}

// SlotID returns the masked ring slot ID at the given index.
func (it BundleIterator) SlotID(index int) uint16 { return 0 }

// Buffer returns the buffer at the given index.
func (it BundleIterator) Buffer(index int) []byte { return nil }

// Collect returns all buffers as a slice.
func (it BundleIterator) Collect() [][]byte { return nil }

// CopyTo copies all bundle data to the destination.
func (it BundleIterator) CopyTo(dst []byte) int { return 0 }

// Recycle returns consumed buffers to the buffer ring (stub on Darwin).
func (it BundleIterator) Recycle(ur *Uring) {}
