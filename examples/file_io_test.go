// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// TestFileWriteRead demonstrates basic file I/O using io_uring.
//
// This example shows the fundamental io_uring pattern:
//  1. Pack context with operation metadata
//  2. Submit operation (non-blocking)
//  3. Wait for completion
//  4. Check result in CQE
//
// Key insight: io_uring separates submission from completion,
// allowing batched submissions and async processing.
func TestFileWriteRead(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true // Request a CQE for every successful operation.
			opts.MultiIssuers = true  // Use the shared-submit configuration in this example.
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create temp file
	f, err := os.CreateTemp("", "uring_file_io_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())
	testData := []byte("Hello from io_uring file I/O!")

	// === WRITE ===
	// PackDirect creates zero-allocation context:
	//   - op: IORING_OP_WRITE
	//   - flags: 0 (none)
	//   - bufGroup: 0 (not using buffer selection)
	//   - fd: file descriptor
	writeCtx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, fd)
	if err := ring.Write(writeCtx, testData); err != nil {
		t.Fatalf("Write submit: %v", err)
	}

	// Wait for write completion
	cqe, ok := waitForOp(t, ring, uring.IORING_OP_WRITE, time.Second)
	if !ok {
		t.Fatal("Write did not complete")
	}
	if err := cqe.Err(); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if int(cqe.Res) != len(testData) {
		t.Errorf("Write: got %d bytes, want %d", cqe.Res, len(testData))
	}
	t.Logf("Write completed: %d bytes", cqe.Res)

	// === READ ===
	// Seek back to start
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	readBuf := make([]byte, len(testData))
	readCtx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, fd)
	if err := ring.Read(readCtx, readBuf); err != nil {
		t.Fatalf("Read submit: %v", err)
	}

	cqe, ok = waitForOp(t, ring, uring.IORING_OP_READ, time.Second)
	if !ok {
		t.Fatal("Read did not complete")
	}
	if err := cqe.Err(); err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	t.Logf("Read completed: %d bytes", cqe.Res)

	// Verify data
	if !bytes.Equal(readBuf, testData) {
		t.Errorf("Data mismatch: got %q, want %q", readBuf, testData)
	}
}

// TestBatchedFileOps demonstrates submitting multiple operations before waiting.
//
// io_uring's power comes from batching: submit many SQEs, then collect
// completions together. This reduces syscall overhead.
func TestBatchedFileOps(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true // Request a CQE for every successful operation.
			opts.MultiIssuers = true  // Use the shared-submit configuration in this example.
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create multiple temp files
	const numFiles = 4
	files := make([]*os.File, numFiles)
	for i := range numFiles {
		f, err := os.CreateTemp("", "uring_batch_*")
		if err != nil {
			t.Fatalf("CreateTemp[%d]: %v", i, err)
		}
		files[i] = f
		defer os.Remove(f.Name())
		defer f.Close()
	}

	// Submit writes to ALL files before waiting
	for i, f := range files {
		data := []byte("File " + string('A'+byte(i)) + " data")
		ctx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, int32(f.Fd()))
		if err := ring.Write(ctx, data); err != nil {
			t.Fatalf("Write[%d] submit: %v", i, err)
		}
	}
	t.Logf("Submitted %d writes", numFiles)

	// Now collect all completions
	cqes := make([]uring.CQEView, 16)
	completed := 0
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	for completed < numFiles && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_WRITE {
				if err := cqes[i].Err(); err != nil {
					t.Errorf("Write failed: %v", err)
				}
				completed++
			}
		}
		if n == 0 {
			b.Wait()
		}
	}

	if completed != numFiles {
		t.Errorf("Only %d/%d writes completed", completed, numFiles)
	}
	t.Logf("All %d writes completed", completed)
}

// TestNopThroughput measures raw SQE submission throughput using NOP operations.
//
// NOP is useful for:
//   - Benchmarking ring overhead without I/O
//   - Synchronization points in linked chains
//   - Testing ring setup and teardown
func TestNopThroughput(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesMedium // 2048 entries
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	const numOps = 1000
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)

	start := time.Now()

	// Submit all NOPs
	submitted := 0
	for i := 0; i < numOps; i++ {
		if err := ring.Nop(ctx); err != nil {
			if errors.Is(err, iox.ErrWouldBlock) {
				// Ring full, drain some completions
				drainCompletions(ring, 100)
				i-- // Retry
				continue
			}
			t.Fatalf("Nop[%d]: %v", i, err)
		}
		submitted++
	}

	// Drain remaining completions
	drainCompletions(ring, submitted)

	elapsed := time.Since(start)
	opsPerSec := float64(numOps) / elapsed.Seconds()

	t.Logf("Completed %d NOPs in %v (%.0f ops/sec)", numOps, elapsed, opsPerSec)
}
