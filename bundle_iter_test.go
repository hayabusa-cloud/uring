// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/uring"
)

// TestBundleIteratorBasic tests basic bundle iteration.
func TestBundleIteratorBasic(t *testing.T) {
	// Create mock buffer backing
	const bufCount = 8
	const bufSize = 256
	backing := make([]byte, bufCount*bufSize)

	// Fill buffers with test data
	for i := 0; i < bufCount; i++ {
		for j := 0; j < bufSize; j++ {
			backing[i*bufSize+j] = byte(i)
		}
	}
	// Create a mock CQE that consumed 3 buffers starting at ID 0
	// Total bytes = 3 * 256 = 768
	cqe := uring.CQEView{
		Res:   768,
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT, // start ID = 0
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	if got := iter.Count(); got != 3 {
		t.Errorf("Count: got %d, want 3", got)
	}

	if got := iter.TotalBytes(); got != 768 {
		t.Errorf("TotalBytes: got %d, want 768", got)
	}

	// Iterate and verify
	count := 0
	for buf := range iter.All() {
		if len(buf) != bufSize {
			t.Errorf("buffer %d: got len %d, want %d", count, len(buf), bufSize)
		}
		// Verify buffer content
		if buf[0] != byte(count) {
			t.Errorf("buffer %d: got content %d, want %d", count, buf[0], count)
		}
		count++
	}

	if count != 3 {
		t.Errorf("iterated %d buffers, want 3", count)
	}
}

// TestBundleIteratorPartialLastBuffer tests when the last buffer is partial.
func TestBundleIteratorPartialLastBuffer(t *testing.T) {
	const bufCount = 4
	const bufSize = 100
	backing := make([]byte, bufCount*bufSize)

	// 250 bytes = 2 full buffers (200 bytes) + partial (50 bytes)
	cqe := uring.CQEView{
		Res:   250,
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Should consume 3 buffers (ceiling(250/100) = 3)
	if got := iter.Count(); got != 3 {
		t.Errorf("Count: got %d, want 3", got)
	}

	// First two buffers should be full
	buf1 := iter.Buffer(0)
	if buf1 == nil || len(buf1) != 100 {
		t.Errorf("buffer 0: got len %d, want 100", len(buf1))
	}

	buf2 := iter.Buffer(1)
	if buf2 == nil || len(buf2) != 100 {
		t.Errorf("buffer 1: got len %d, want 100", len(buf2))
	}

	// Last buffer should be partial (50 bytes)
	buf3 := iter.Buffer(2)
	if buf3 == nil || len(buf3) != 50 {
		t.Errorf("buffer 2: got len %d, want 50", len(buf3))
	}

	// Out of range
	buf4 := iter.Buffer(3)
	if buf4 != nil {
		t.Error("expected false for out-of-range buffer")
	}
}

// TestBundleIteratorWrapAround tests buffer ring wrap-around.
func TestBundleIteratorWrapAround(t *testing.T) {
	const bufCount = 4 // Ring mask = 3
	const bufSize = 64
	backing := make([]byte, bufCount*bufSize)

	// Fill buffers with their index
	for i := 0; i < bufCount; i++ {
		backing[i*bufSize] = byte(i)
	}

	// Start at ID 3, consume 3 buffers -> IDs 3, 0, 1 (wraps around)
	cqe := uring.CQEView{
		Res:   192,                                // 3 * 64
		Flags: 3 << uring.IORING_CQE_BUFFER_SHIFT, // start ID = 3
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Verify ring slot IDs wrap correctly
	expectedIDs := []uint16{3, 0, 1}
	for i, expectedID := range expectedIDs {
		if got := iter.SlotID(i); got != expectedID {
			t.Errorf("SlotID(%d): got %d, want %d", i, got, expectedID)
		}
	}

	// Verify buffer content (index stored in first byte)
	buf0 := iter.Buffer(0)
	if buf0[0] != 3 { // Buffer at ID 3
		t.Errorf("buffer 0 content: got %d, want 3", buf0[0])
	}

	buf1 := iter.Buffer(1)
	if buf1[0] != 0 { // Buffer at ID 0 (wrapped)
		t.Errorf("buffer 1 content: got %d, want 0", buf1[0])
	}

	buf2 := iter.Buffer(2)
	if buf2[0] != 1 { // Buffer at ID 1
		t.Errorf("buffer 2 content: got %d, want 1", buf2[0])
	}
}

// TestBundleIteratorAll tests the range-over-func iterator.
func TestBundleIteratorAll(t *testing.T) {
	const bufCount = 4
	const bufSize = 32
	backing := make([]byte, bufCount*bufSize)
	for i := range backing {
		backing[i] = byte(i / bufSize)
	}

	cqe := uring.CQEView{
		Res:   128, // 4 * 32
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Iterate using All()
	count := 0
	for buf := range iter.All() {
		if len(buf) != bufSize {
			t.Errorf("buffer %d: got len %d, want %d", count, len(buf), bufSize)
		}
		if buf[0] != byte(count) {
			t.Errorf("buffer %d: got content %d, want %d", count, buf[0], count)
		}
		count++
	}

	if count != 4 {
		t.Errorf("All() yielded %d buffers, want 4", count)
	}
}

// TestBundleIteratorAllWithSlotID tests the AllWithSlotID iterator.
func TestBundleIteratorAllWithSlotID(t *testing.T) {
	const bufCount = 8
	const bufSize = 16
	backing := make([]byte, bufCount*bufSize)

	// Start at ID 6, consume 4 buffers -> IDs 6, 7, 0, 1
	cqe := uring.CQEView{
		Res:   64, // 4 * 16
		Flags: 6 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	expectedIDs := []uint16{6, 7, 0, 1}
	i := 0
	for id, buf := range iter.AllWithSlotID() {
		if id != expectedIDs[i] {
			t.Errorf("index %d: got ID %d, want %d", i, id, expectedIDs[i])
		}
		if len(buf) != bufSize {
			t.Errorf("index %d: got len %d, want %d", i, len(buf), bufSize)
		}
		i++
	}

	if i != 4 {
		t.Errorf("AllWithSlotID yielded %d items, want 4", i)
	}
}

// TestBundleIteratorBuffer tests random access via Buffer().
func TestBundleIteratorBuffer(t *testing.T) {
	const bufCount = 4
	const bufSize = 50
	backing := make([]byte, bufCount*bufSize)
	for i := 0; i < bufCount; i++ {
		backing[i*bufSize] = byte(i * 10)
	}

	cqe := uring.CQEView{
		Res:   150,                                // 3 buffers
		Flags: 1 << uring.IORING_CQE_BUFFER_SHIFT, // start ID = 1
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Buffer(0) should be ID 1
	buf := iter.Buffer(0)
	if buf[0] != 10 {
		t.Errorf("Buffer(0)[0]: got %d, want 10", buf[0])
	}

	// Buffer(1) should be ID 2
	buf = iter.Buffer(1)
	if buf[0] != 20 {
		t.Errorf("Buffer(1)[0]: got %d, want 20", buf[0])
	}

	// Buffer(2) should be ID 3
	buf = iter.Buffer(2)
	if buf[0] != 30 {
		t.Errorf("Buffer(2)[0]: got %d, want 30", buf[0])
	}

	// Out of range
	buf = iter.Buffer(-1)
	if buf != nil {
		t.Error("Buffer(-1) should return nil")
	}

	buf = iter.Buffer(10)
	if buf != nil {
		t.Error("Buffer(10) should return nil")
	}
}

// TestBundleIteratorCollect tests the Collect method.
func TestBundleIteratorCollect(t *testing.T) {
	const bufCount = 4
	const bufSize = 20
	backing := make([]byte, bufCount*bufSize)

	cqe := uring.CQEView{
		Res:   60, // 3 buffers
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	buffers := iter.Collect()
	if len(buffers) != 3 {
		t.Errorf("Collect: got %d buffers, want 3", len(buffers))
	}
}

// TestBundleIteratorCopyTo tests the CopyTo method.
func TestBundleIteratorCopyTo(t *testing.T) {
	const bufCount = 4
	const bufSize = 10
	backing := make([]byte, bufCount*bufSize)
	for i := range backing {
		backing[i] = byte(i)
	}

	cqe := uring.CQEView{
		Res:   25, // 2.5 buffers
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Copy all
	dst := make([]byte, 100)
	n := iter.CopyTo(dst)
	if n != 25 {
		t.Errorf("CopyTo: copied %d bytes, want 25", n)
	}

	// Verify copied data
	for i := 0; i < 25; i++ {
		if dst[i] != byte(i) {
			t.Errorf("dst[%d]: got %d, want %d", i, dst[i], i)
		}
	}
}

// TestBundleIteratorReIterate tests that All() can be called multiple times.
func TestBundleIteratorReIterate(t *testing.T) {
	const bufCount = 2
	const bufSize = 50
	backing := make([]byte, bufCount*bufSize)

	cqe := uring.CQEView{
		Res:   100,
		Flags: 0 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Iterate through twice using All()
	count1 := 0
	for range iter.All() {
		count1++
	}

	count2 := 0
	for range iter.All() {
		count2++
	}

	if count1 != count2 {
		t.Errorf("counts differ on re-iteration: %d vs %d", count1, count2)
	}
}

// TestBundleIteratorNilCases tests nil return cases.
func TestBundleIteratorNilCases(t *testing.T) {
	backing := make([]byte, 256)

	// Zero result
	cqe := uring.CQEView{Res: 0, Flags: 0}
	_, ok := uring.NewBundleIterator(cqe, backing, 64, 4)
	if ok {
		t.Error("expected false for zero result")
	}

	// Negative result
	cqe = uring.CQEView{Res: -1, Flags: 0}
	_, ok = uring.NewBundleIterator(cqe, backing, 64, 4)
	if ok {
		t.Error("expected false for negative result")
	}

	// Zero buffer size
	cqe = uring.CQEView{Res: 100, Flags: 0}
	_, ok = uring.NewBundleIterator(cqe, backing, 0, 4)
	if ok {
		t.Error("expected false for zero buffer size")
	}

	// Non-power-of-two ring size
	_, ok = uring.NewBundleIterator(cqe, backing, 64, 3)
	if ok {
		t.Error("expected false for non-power-of-two ring size")
	}

	// Insufficient backing memory
	_, ok = uring.NewBundleIterator(cqe, backing[:128], 64, 4)
	if ok {
		t.Error("expected false for insufficient backing memory")
	}
}

// TestBundleIteratorUint16Boundary tests wrap-around at uint16 boundary.
// Logical ID values range from 0-65535, and this test verifies correct
// handling when the starting ID is near 65535 and wraps to 0.
func TestBundleIteratorUint16Boundary(t *testing.T) {
	// Use a ring with 256 entries (mask = 0xFF)
	const bufCount = 256
	const bufSize = 32
	backing := make([]byte, bufCount*bufSize)

	// Fill buffers with their ring ID
	for i := 0; i < bufCount; i++ {
		backing[i*bufSize] = byte(i)
	}

	// Test case: start ID = 65534, consume 5 buffers
	// Logical IDs: 65534, 65535, 0, 1, 2
	// Ring IDs: 65534 & 0xFF = 254, 65535 & 0xFF = 255, 0, 1, 2
	cqe := uring.CQEView{
		Res:   5 * bufSize, // 5 buffers
		Flags: 65534 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	if got := iter.Count(); got != 5 {
		t.Errorf("Count: got %d, want 5", got)
	}

	// Verify buffer ring IDs wrap correctly through uint16 overflow
	expectedIDs := []uint16{65534 & 0xFF, 65535 & 0xFF, 0, 1, 2} // 254, 255, 0, 1, 2
	for i, expectedID := range expectedIDs {
		id := iter.SlotID(i)
		if id != expectedID {
			t.Errorf("SlotID(%d): got %d, want %d", i, id, expectedID)
		}
	}

	// Verify buffer content via iteration (using ring ID)
	idx := 0
	expectedMarkers := []byte{254, 255, 0, 1, 2}
	for buf := range iter.All() {
		if buf[0] != expectedMarkers[idx] {
			t.Errorf("buffer %d: got content %d, want %d", idx, buf[0], expectedMarkers[idx])
		}
		idx++
	}

	if idx != 5 {
		t.Errorf("iterated %d buffers, want 5", idx)
	}
}

// TestBundleIteratorMaxStartID tests with maximum possible starting ID (65535).
func TestBundleIteratorMaxStartID(t *testing.T) {
	const bufCount = 8
	const bufSize = 16
	backing := make([]byte, bufCount*bufSize)
	for i := 0; i < bufCount; i++ {
		backing[i*bufSize] = byte(i)
	}

	// Starting ID = 65535 (max uint16), consume 3 buffers
	// Logical IDs: 65535, 0, 1 (wraps through uint16 overflow)
	// Ring IDs: 65535 & 7 = 7, 0, 1
	cqe := uring.CQEView{
		Res:   3 * bufSize,
		Flags: 0xFFFF << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	expectedIDs := []byte{7, 0, 1}
	idx := 0
	for buf := range iter.All() {
		if buf[0] != expectedIDs[idx] {
			t.Errorf("buffer %d: got content %d, want %d", idx, buf[0], expectedIDs[idx])
		}
		idx++
	}
}
