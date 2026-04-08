// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// TestFixedBufferBasics demonstrates registered (fixed) buffer usage.
//
// Architecture:
//   - Registered buffers are pre-pinned in kernel memory at ring setup
//   - IORING_OP_READ_FIXED / IORING_OP_WRITE_FIXED use buffer by index
//   - No per-operation memory pinning overhead
//   - Buffers accessed via ring.RegisteredBuffer(index)
//
// Performance benefit: Eliminates memory pinning on each I/O operation,
// significant for high-frequency small I/O patterns.
func TestFixedBufferBasics(t *testing.T) {
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 20 // 1 MiB for fixed buffers
		},
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Access pre-registered buffer by index
	buf0 := ring.RegisteredBuffer(0)
	if buf0 == nil {
		t.Fatal("No registered buffer at index 0")
	}
	t.Logf("Fixed buffer 0: %d bytes", len(buf0))

	buf1 := ring.RegisteredBuffer(1)
	if buf1 != nil {
		t.Logf("Fixed buffer 1: %d bytes", len(buf1))
	}

	// Write test data to buffer
	testData := []byte("Hello, Fixed Buffers!")
	copy(buf0, testData)
	t.Logf("Wrote to buffer 0: %q", string(buf0[:len(testData)]))

	// Buffer is ready for zero-copy operations
	t.Log("Buffer ready for WriteFixed operations")
}

// TestWriteFixedOperation demonstrates write with fixed buffer.
//
// Architecture:
//   - WriteFixed uses pre-registered buffer by index
//   - Kernel accesses pre-pinned buffer directly (no per-op pinning)
//   - Ideal for high-throughput write scenarios
func TestWriteFixedOperation(t *testing.T) {
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 20
		},
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create temp file
	f, err := os.CreateTemp("", "uring_writefixed_*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())

	// Get fixed buffer and populate
	bufIndex := 0
	buf := ring.RegisteredBuffer(bufIndex)
	if buf == nil {
		t.Skip("No registered buffers available")
	}

	message := []byte("Data written via WriteFixed - pre-pinned buffer")
	copy(buf, message)

	// Submit WriteFixed
	ctx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, fd)
	if err := ring.WriteFixed(ctx, bufIndex, len(message)); err != nil {
		t.Fatalf("WriteFixed: %v", err)
	}

	// Wait for completion
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	var result int32
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			result = cqes[0].Res
			break
		}
		b.Wait()
	}

	if result < 0 {
		t.Fatalf("WriteFixed failed: %d", result)
	}

	// Verify file content
	f.Seek(0, 0)
	content := make([]byte, 256)
	n, _ := f.Read(content)

	if string(content[:n]) != string(message) {
		t.Errorf("content = %q, want %q", string(content[:n]), string(message))
	}

	t.Logf("WriteFixed: wrote %d bytes from buffer[%d]", result, bufIndex)
}

// TestReadFixedOperation demonstrates read with fixed buffer.
//
// Architecture:
//   - ReadFixed reads directly into pre-registered buffer
//   - Kernel writes to pre-pinned buffer (no per-op pinning)
//   - Returns slice into the fixed buffer region
func TestReadFixedOperation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ReadFixed test in short mode (requires reliable io_uring timing)")
	}
	ring, err := uring.New(
		func(opts *uring.Options) {
			opts.LockedBufferMem = 1 << 20
		},
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create temp file with content
	f, err := os.CreateTemp("", "uring_readfixed_*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	testData := "Content to read via ReadFixed operation"
	f.WriteString(testData)
	f.Seek(0, 0)

	fd := int32(f.Fd())
	bufIndex := 0

	// Submit ReadFixed
	ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, fd)
	buf, err := ring.ReadFixed(ctx, bufIndex)
	if err != nil {
		t.Fatalf("ReadFixed: %v", err)
	}
	_ = buf // Buffer will be populated after CQE

	// Wait for completion
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	var result int32
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			result = cqes[0].Res
			break
		}
		b.Wait()
	}

	if result < 0 {
		t.Fatalf("ReadFixed failed: %d", result)
	}

	// Access data in fixed buffer
	fixedBuf := ring.RegisteredBuffer(bufIndex)
	if fixedBuf == nil {
		t.Fatal("Buffer not available")
	}

	readData := string(fixedBuf[:result])
	if readData != testData {
		t.Errorf("read = %q, want %q", readData, testData)
	}

	t.Logf("ReadFixed: read %d bytes into buffer[%d]", result, bufIndex)
	t.Logf("Content: %q", readData)
}

// TestFixedVsNormalBuffers compares fixed vs normal buffer patterns.
func TestFixedVsNormalBuffers(t *testing.T) {
	t.Log("Fixed vs Normal Buffers:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Normal Buffer (per-operation pinning):")
	t.Log("  1. Application allocates buffer")
	t.Log("  2. Submit SQE with buffer address")
	t.Log("  3. Kernel pins pages in memory")
	t.Log("  4. I/O operation executes")
	t.Log("  5. Kernel unpins pages")
	t.Log("  → Steps 3,5 add overhead on EVERY operation")
	t.Log("")
	t.Log("Fixed Buffer (pre-registered):")
	t.Log("  Setup: Register buffers once at ring creation")
	t.Log("  1. Submit SQE with buffer INDEX")
	t.Log("  2. Kernel uses pre-pinned buffer")
	t.Log("  3. I/O operation executes")
	t.Log("  → No per-operation pinning overhead")
	t.Log("")
	t.Log("When to use Fixed Buffers:")
	t.Log("  - High-frequency I/O (>10K ops/sec)")
	t.Log("  - Consistent buffer sizes")
	t.Log("  - Network I/O (recv/send)")
	t.Log("  - File I/O with hot data")
	t.Log("-----------------------------------------------------")
}

// TestBufferTierSelection shows how to choose appropriate buffer tiers.
func TestBufferTierSelection(t *testing.T) {
	t.Log("Buffer Tier Selection Guide:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Logf("  Pico   (%5d B): Tiny metadata, ACKs, heartbeats", uring.BufferSizePico)
	t.Logf("  Nano   (%5d B): Small headers, control messages", uring.BufferSizeNano)
	t.Logf("  Micro  (%5d B): Protocol frames, small packets", uring.BufferSizeMicro)
	t.Logf("  Small  (%5d B): Standard messages", uring.BufferSizeSmall)
	t.Logf("  Medium (%5d B): Page-sized, file blocks", uring.BufferSizeMedium)
	t.Logf("  Big    (%5d B): Large messages", uring.BufferSizeBig)
	t.Logf("  Huge   (%5d B): Bulk transfers", uring.BufferSizeHuge)
	t.Logf("  Giant  (%5d B): Maximum size buffers", uring.BufferSizeGiant)
	t.Log("")
	t.Log("Selection Strategy:")
	t.Log("  1. Use smallest tier that fits expected message size")
	t.Log("  2. Different tiers for different connection types")
	t.Log("  3. Consider MTU for network (1500B → Small tier)")
	t.Log("  4. Consider page size for files (4KB → Medium tier)")
	t.Log("-----------------------------------------------------")
}

// TestFixedBufferDataPath shows the data path with pre-pinned fixed buffers.
func TestFixedBufferDataPath(t *testing.T) {
	t.Log("Data Path with Pre-Pinned Fixed Buffers:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Network Receive Path:")
	t.Log("  NIC → DMA → Kernel → Fixed Buffer → Application")
	t.Log("        (no per-operation page pinning)")
	t.Log("")
	t.Log("Network Send Path:")
	t.Log("  Application → Fixed Buffer → Kernel → DMA → NIC")
	t.Log("             (no per-operation page pinning)")
	t.Log("")
	t.Log("File Read Path:")
	t.Log("  Disk → Page Cache → Fixed Buffer → Application")
	t.Log("                   (O_DIRECT bypasses page cache)")
	t.Log("")
	t.Log("File Write Path:")
	t.Log("  Application → Fixed Buffer → Page Cache → Disk")
	t.Log("             (O_DIRECT bypasses page cache)")
	t.Log("")
	t.Log("Pre-pinned buffers eliminate per-operation page pinning overhead.")
	t.Log("True zero-copy requires IORING_OP_SEND_ZC or O_DIRECT.")
	t.Log("-----------------------------------------------------")
}
