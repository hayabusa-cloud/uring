// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"testing"
	"unsafe"

	"code.hybscloud.com/uring"
)

// TestBufferRingBasics demonstrates fixed (registered) buffer access and the
// buffer architecture in io_uring.
//
// io_uring supports two buffer mechanisms:
//   - Fixed buffers: pre-registered at ring setup, accessed by index via
//     ring.RegisteredBuffer(idx). Eliminates per-operation page pinning.
//   - Provided buffer rings: registered via IORING_REGISTER_PBUF_RING,
//     kernel selects from ring and returns BufID in CQE flags.
//
// This example demonstrates fixed buffer access. For provided buffer ring
// usage with kernel buffer selection, see TestMultishotRecvWithBufferSelection.
func TestBufferRingBasics(t *testing.T) {
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 20
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Log("Fixed Buffer Architecture:")
	t.Log("-----------------------------------------------------")
	t.Log("  +-----------------------------------------------------+")
	t.Log("  |  User Space                                         |")
	t.Log("  |  +-------------------------------------------------+|")
	t.Log("  |  | Fixed Buffers (pre-pinned at ring setup)        ||")
	t.Log("  |  | [buf0][buf1][buf2]...                           ||")
	t.Log("  |  | ring.RegisteredBuffer(idx) -> []byte            ||")
	t.Log("  |  +-------------------------------------------------+|")
	t.Log("  +-----------------------------------------------------+")
	t.Log("                    | IORING_REGISTER_BUFFERS")
	t.Log("                    v")
	t.Log("  +-----------------------------------------------------+")
	t.Log("  |  Kernel: pages pre-pinned, no per-op pinning cost  |")
	t.Log("  +-----------------------------------------------------+")
	t.Log("-----------------------------------------------------")

	buf0 := ring.RegisteredBuffer(0)
	if buf0 != nil {
		t.Logf("Fixed buffer 0: %d bytes at %p", len(buf0), unsafe.Pointer(&buf0[0]))
	}

	buf1 := ring.RegisteredBuffer(1)
	if buf1 != nil {
		t.Logf("Fixed buffer 1: %d bytes at %p", len(buf1), unsafe.Pointer(&buf1[0]))
	}

	t.Log("")
	t.Log("Buffer usage patterns:")
	t.Log("  1. Fixed buffers: ring.RegisteredBuffer(idx) - direct access by index")
	t.Log("  2. Buffer selection: IOSQE_BUFFER_SELECT - kernel picks")
	t.Log("  3. Bundle recv: multiple buffers consumed per CQE")
}

// TestBundleIteratorUsage demonstrates BundleIterator for processing
// bundle receive results where multiple buffers are consumed per CQE.
//
// Bundle operations (IORING_RECV_BUNDLE) receive data into multiple
// contiguous buffers from a buffer ring. The CQE contains:
//   - Res: total bytes received
//   - BufID: starting buffer ID encoded by the kernel
//   - Buffer count is computed: ceil(Res / bufSize)
//
// See also: TestMultishotRecvWithBufferSelection for multishot recv.
func TestBundleIteratorUsage(t *testing.T) {
	t.Log("BundleIterator for multi-buffer receives:")
	t.Log("-----------------------------------------------------")

	type simulatedBundle struct {
		totalBytes int
		startID    uint16
		bufSize    int
		ringSize   int
	}

	examples := []simulatedBundle{
		{totalBytes: 5000, startID: 0, bufSize: 1024, ringSize: 16},
		{totalBytes: 3500, startID: 14, bufSize: 1024, ringSize: 16},
		{totalBytes: 512, startID: 5, bufSize: 1024, ringSize: 16},
	}

	for i, ex := range examples {
		bufCount := (ex.totalBytes + ex.bufSize - 1) / ex.bufSize
		t.Logf("\nExample %d: %d bytes, startID=%d, bufSize=%d",
			i+1, ex.totalBytes, ex.startID, ex.bufSize)
		t.Logf("  Buffers consumed: %d", bufCount)

		mask := uint16(ex.ringSize - 1)
		t.Log("  Buffer IDs:")
		for j := range bufCount {
			id := (ex.startID + uint16(j)) & mask
			size := ex.bufSize
			if remaining := ex.totalBytes - j*ex.bufSize; remaining < size {
				size = remaining
			}
			t.Logf("    [%d] id=%d, size=%d", j, id, size)
		}
	}

	t.Log("\n-----------------------------------------------------")
	t.Log("BundleIterator methods:")
	t.Log("  iter.All()        -> iter.Seq[[]byte]  (Go 1.23+)")
	t.Log("  iter.AllWithSlotID() -> iter.Seq2[uint16, []byte] (slot ID, data)")
	t.Log("  iter.Buffer(idx)  -> []byte (random access)")
	t.Log("  iter.CopyTo(dst)  -> int (copy all to dst)")
}

// TestBufferRingTiers shows the tiered buffer system for different message sizes.
//
// The uring package supports multiple buffer tiers, each optimized for
// different payload sizes. This reduces memory waste by matching buffer
// size to expected message size.
func TestBufferRingTiers(t *testing.T) {
	t.Log("Buffer Tier System:")
	t.Log("-----------------------------------------------------")
	t.Log("  Tier    | Size    | Use Case")
	t.Log("  --------+---------+--------------------------------")
	t.Logf("  Pico    | %5d B | Tiny metadata, ACKs", uring.BufferSizePico)
	t.Logf("  Nano    | %5d B | Small headers, control msgs", uring.BufferSizeNano)
	t.Logf("  Micro   | %5d B | Protocol frames", uring.BufferSizeMicro)
	t.Logf("  Small   | %5d B | Small messages", uring.BufferSizeSmall)
	t.Logf("  Medium  | %5d B | Page-sized operations", uring.BufferSizeMedium)
	t.Logf("  Big     | %5d B | Large messages", uring.BufferSizeBig)
	t.Logf("  Huge    | %5d B | Bulk transfers", uring.BufferSizeHuge)
	t.Logf("  Giant   | %5d B | Maximum buffers", uring.BufferSizeGiant)
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Selection strategy:")
	t.Log("  - Use smallest tier that fits expected message size")
	t.Log("  - Different buffer groups for different protocols")
	t.Log("  - Multishot recv can use specific buffer group")
}

// TestBufferSelectionWorkflow demonstrates the complete workflow for
// kernel buffer selection in recv operations.
func TestBufferSelectionWorkflow(t *testing.T) {
	t.Log("Kernel Buffer Selection Workflow:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Setup phase:")
	t.Log("  1. Allocate backing memory (page-aligned)")
	t.Log("  2. Create buffer ring structure (16B per entry)")
	t.Log("  3. Register with IORING_REGISTER_PBUF_RING")
	t.Log("  4. Populate ring with buffer addresses")
	t.Log("")
	t.Log("Operation phase:")
	t.Log("  1. Submit recv SQE with IOSQE_BUFFER_SELECT flag")
	t.Log("  2. Set bufIndex = buffer group ID")
	t.Log("  3. Kernel picks buffer from ring when data arrives")
	t.Log("  4. CQE contains:")
	t.Log("     - res: bytes received (or negative errno)")
	t.Log("     - flags: IORING_CQE_F_BUFFER set")
	t.Log("     - BufID(): first logical buffer ID")
	t.Log("")
	t.Log("Return phase:")
	t.Log("  1. Process data in buffer[BufID() & mask]")
	t.Log("  2. Re-add buffer to ring (advance tail)")
	t.Log("  3. Ring automatically wraps around")
	t.Log("-----------------------------------------------------")
}

// TestIncrementalBufferConsumption shows how IOU_PBUF_RING_INC works
// for incremental buffer consumption.
//
// With incremental mode (IOU_PBUF_RING_INC), a single buffer can
// span multiple CQEs until fully consumed. IORING_CQE_F_BUF_MORE
// indicates more data coming in the same buffer.
func TestIncrementalBufferConsumption(t *testing.T) {
	t.Log("Incremental Buffer Consumption (IOU_PBUF_RING_INC):")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Standard mode:")
	t.Log("  CQE1: BufID=0, 1024B -> buffer returned to ring")
	t.Log("  CQE2: BufID=1, 1024B -> buffer returned to ring")
	t.Log("")
	t.Log("Incremental mode:")
	t.Log("  CQE1: BufID=0, 512B, F_BUF_MORE -> buffer still in use")
	t.Log("  CQE2: BufID=0, 512B, no flag   -> buffer now returned")
	t.Log("")
	t.Log("Use case: Large messages arriving in fragments")
	t.Log("Benefit: Same buffer ID until complete, no reassembly")
	t.Log("-----------------------------------------------------")
}

// TestBufferRingWrapAround demonstrates how the ring handles wrap-around.
//
// Buffer rings are circular: when the head/tail reaches the end,
// it wraps back to the beginning using modular arithmetic.
func TestBufferRingWrapAround(t *testing.T) {
	t.Log("Buffer Ring Wrap-Around:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Ring with 16 entries (mask = 15 = 0x0F):")
	t.Log("")

	ringSize := 16
	mask := uint16(ringSize - 1)

	scenarios := []struct {
		startID uint16
		count   int
	}{
		{0, 3},
		{14, 5},
		{15, 2},
	}

	for _, s := range scenarios {
		t.Logf("  StartID=%d, Count=%d:", s.startID, s.count)
		for i := range s.count {
			id := (s.startID + uint16(i)) & mask
			t.Logf("    index %d -> ID %d", i, id)
		}
		t.Log("")
	}

	t.Log("Formula: id = (startID + index) & (ringSize - 1)")
	t.Log("-----------------------------------------------------")
}
