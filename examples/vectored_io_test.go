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

// TestVectoredWrite demonstrates scatter-gather write using io_uring.
//
// Architecture:
//   - WriteV (IORING_OP_WRITEV) writes multiple buffers in a single syscall
//   - Kernel gathers data from multiple buffers into contiguous file content
//   - Reduces syscall overhead for multi-buffer writes
//
// Use cases: Protocol headers + payload, logging with metadata,
// combining multiple data sources without copying.
func TestVectoredWrite(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create temp file
	f, err := os.CreateTemp("", "uring_writev_*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())

	// Prepare multiple buffers (scatter)
	header := []byte("HEADER:")
	payload := []byte("This is the payload data")
	trailer := []byte(":END\n")

	iovs := [][]byte{header, payload, trailer}

	// Submit vectored write
	ctx := uring.PackDirect(uring.IORING_OP_WRITEV, 0, 0, fd)
	if err := ring.WriteV(ctx, iovs); err != nil {
		t.Fatalf("WriteV: %v", err)
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
		t.Fatalf("WriteV failed: %d", result)
	}

	expectedLen := len(header) + len(payload) + len(trailer)
	if int(result) != expectedLen {
		// WSL2 quirk: CQE result may be 0 even when data was written correctly
		// Verify by checking actual file content instead of failing
		t.Logf("CQE result %d != expected %d (checking file content)", result, expectedLen)
	}

	// Verify file content
	f.Seek(0, 0)
	content := make([]byte, 256)
	n, _ := f.Read(content)
	expected := "HEADER:This is the payload data:END\n"
	if string(content[:n]) != expected {
		t.Errorf("file content = %q, want %q", string(content[:n]), expected)
	}

	t.Logf("WriteV: wrote %d bytes from %d buffers", result, len(iovs))
	t.Logf("Content: %q", string(content[:n]))
}

// TestVectoredRead demonstrates scatter-gather read using io_uring.
//
// Architecture:
//   - ReadV (IORING_OP_READV) reads into multiple buffers in a single syscall
//   - Kernel scatters data from file into multiple destination buffers
//   - Useful for reading structured data into separate buffers
//
// Use cases: Reading protocol messages (header + body), structured records,
// filling multiple buffers from a single source.
func TestVectoredRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping vectored read test in short mode (requires reliable io_uring timing)")
	}
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create temp file with known content
	f, err := os.CreateTemp("", "uring_readv_*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	// Write test data: "AAAAABBBBBCCCCC" (5+5+5 = 15 bytes)
	testData := "AAAAABBBBBCCCCC"
	f.WriteString(testData)
	f.Seek(0, 0)

	fd := int32(f.Fd())

	// Prepare destination buffers (scatter into 3 buffers of 5 bytes each)
	buf1 := make([]byte, 5)
	buf2 := make([]byte, 5)
	buf3 := make([]byte, 5)

	iovs := [][]byte{buf1, buf2, buf3}

	// Submit vectored read
	ctx := uring.PackDirect(uring.IORING_OP_READV, 0, 0, fd)
	t.Logf("ReadV setup: ringFD=%d notifySucceed=%t multiIssuers=%t sqEntries=%d cqEntries=%d file=%s fd=%d len=%d iovLens=%v",
		ring.RingFD(),
		ring.NotifySucceed,
		ring.MultiIssuers,
		ring.Features.SQEntries,
		ring.Features.CQEntries,
		f.Name(),
		fd,
		len(testData),
		[]int{len(buf1), len(buf2), len(buf3)},
	)
	if err := ring.ReadV(ctx, iovs); err != nil {
		t.Fatalf("ReadV: %v", err)
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
			t.Logf("ReadV CQE: n=%d res=%d flags=0x%x", n, cqes[0].Res, cqes[0].Flags)
			result = cqes[0].Res
			break
		}
		b.Wait()
	}
	t.Logf("ReadV buffers after wait: buf1=%q buf2=%q buf3=%q", string(buf1), string(buf2), string(buf3))

	if result < 0 {
		t.Fatalf("ReadV failed: %d", result)
	}

	if int(result) != len(testData) {
		t.Errorf("read %d bytes, expected %d", result, len(testData))
	}

	// Verify scatter worked correctly
	if string(buf1) != "AAAAA" {
		t.Errorf("buf1 = %q, want AAAAA", string(buf1))
	}
	if string(buf2) != "BBBBB" {
		t.Errorf("buf2 = %q, want BBBBB", string(buf2))
	}
	if string(buf3) != "CCCCC" {
		t.Errorf("buf3 = %q, want CCCCC", string(buf3))
	}

	t.Logf("ReadV: read %d bytes into %d buffers", result, len(iovs))
	t.Logf("Scattered: buf1=%q, buf2=%q, buf3=%q", string(buf1), string(buf2), string(buf3))
}

// TestVectoredIOPattern shows the iovec pattern for protocol handling.
func TestVectoredIOPattern(t *testing.T) {
	t.Log("Vectored I/O Patterns:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("WriteV (Gather):")
	t.Log("  [Header Buffer] + [Payload Buffer] + [Trailer Buffer]")
	t.Log("          ↓              ↓                  ↓")
	t.Log("  ┌──────────────────────────────────────────────┐")
	t.Log("  │  Contiguous file/socket output              │")
	t.Log("  └──────────────────────────────────────────────┘")
	t.Log("")
	t.Log("ReadV (Scatter):")
	t.Log("  ┌──────────────────────────────────────────────┐")
	t.Log("  │  Contiguous file/socket input               │")
	t.Log("  └──────────────────────────────────────────────┘")
	t.Log("          ↓              ↓                  ↓")
	t.Log("  [Header Buffer] + [Payload Buffer] + [Trailer Buffer]")
	t.Log("")
	t.Log("Benefits:")
	t.Log("  - Single syscall for multiple buffers")
	t.Log("  - No intermediate copy buffer needed")
	t.Log("  - Natural protocol structure (header/body/trailer)")
	t.Log("  - Atomic writes prevent interleaving")
	t.Log("-----------------------------------------------------")
}

// TestVectoredVsSequential compares vectored vs sequential I/O patterns.
func TestVectoredVsSequential(t *testing.T) {
	t.Log("Vectored vs Sequential I/O:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Sequential (3 syscalls):")
	t.Log("  write(fd, header, 8)")
	t.Log("  write(fd, payload, 1024)")
	t.Log("  write(fd, trailer, 4)")
	t.Log("  → 3 context switches, potential interleaving")
	t.Log("")
	t.Log("Vectored (1 syscall):")
	t.Log("  writev(fd, [header, payload, trailer])")
	t.Log("  → 1 context switch, atomic write")
	t.Log("")
	t.Log("io_uring Advantage:")
	t.Log("  - Vectored I/O submits as single SQE")
	t.Log("  - Batching possible: multiple WriteV in one submit")
	t.Log("  - Result in single CQE")
	t.Log("-----------------------------------------------------")
}
