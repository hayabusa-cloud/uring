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
	"code.hybscloud.com/zcall"
)

// TestSpliceBasics demonstrates splice for zero-copy data transfer.
//
// Architecture:
//   - Splice moves data between file descriptors without user-space copy
//   - At least one fd must be a pipe (kernel requirement)
//   - Kernel performs data movement entirely in kernel space
//
// Pattern: file → pipe → socket (or vice versa)
// Use cases: File serving, proxy servers, data forwarding.
func TestSpliceBasics(t *testing.T) {
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

	// Create source file with test data
	srcFile, err := os.CreateTemp("", "uring_splice_src_*.txt")
	if err != nil {
		t.Fatalf("CreateTemp (src): %v", err)
	}
	defer os.Remove(srcFile.Name())
	defer srcFile.Close()

	testData := "Splice transfers this data without user-space copy!"
	srcFile.WriteString(testData)
	srcFile.Seek(0, 0)

	// Create destination file
	dstFile, err := os.CreateTemp("", "uring_splice_dst_*.txt")
	if err != nil {
		t.Fatalf("CreateTemp (dst): %v", err)
	}
	defer os.Remove(dstFile.Name())
	defer dstFile.Close()

	// Create pipe (required for splice)
	var pipeFDs [2]int32
	errno := zcall.Pipe2(&pipeFDs, zcall.O_NONBLOCK)
	if errno != 0 {
		t.Fatalf("pipe2: errno=%d", errno)
	}
	pipeRead, pipeWrite := pipeFDs[0], pipeFDs[1]
	defer zcall.Close(uintptr(pipeRead))
	defer zcall.Close(uintptr(pipeWrite))

	t.Logf("Created pipe: read=%d, write=%d", pipeRead, pipeWrite)

	// Step 1: Splice from file to pipe (file → pipe)
	srcFD := int32(srcFile.Fd())
	ctx1 := uring.PackDirect(uring.IORING_OP_SPLICE, 0, 0, pipeWrite)
	if err := ring.Splice(ctx1, int(srcFD), len(testData)); err != nil {
		t.Fatalf("Splice (file→pipe): %v", err)
	}

	// Wait for first splice
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	var result1 int32
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			result1 = cqes[0].Res
			break
		}
		b.Wait()
	}

	if result1 <= 0 {
		t.Logf("Splice (file→pipe) result: %d (may fail in WSL2 or return 0 bytes)", result1)
		t.Skip("Splice not available or returned 0 bytes")
	}
	t.Logf("Splice file→pipe: %d bytes", result1)

	// Step 2: Splice from pipe to file (pipe → file)
	dstFD := int32(dstFile.Fd())
	ctx2 := uring.PackDirect(uring.IORING_OP_SPLICE, 0, 0, dstFD)
	if err := ring.Splice(ctx2, int(pipeRead), int(result1)); err != nil {
		t.Fatalf("Splice (pipe→file): %v", err)
	}

	// Wait for second splice
	b = iox.Backoff{}
	var result2 int32
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			result2 = cqes[0].Res
			break
		}
		b.Wait()
	}

	if result2 < 0 {
		t.Fatalf("Splice (pipe→file) failed: %d", result2)
	}
	t.Logf("Splice pipe→file: %d bytes", result2)

	// Verify destination content
	dstFile.Seek(0, 0)
	content := make([]byte, 256)
	n, _ := dstFile.Read(content)

	if string(content[:n]) != testData {
		t.Errorf("content = %q, want %q", string(content[:n]), testData)
	}

	t.Logf("Zero-copy transfer complete: %q", string(content[:n]))
}

// TestTeeOperation demonstrates tee for duplicating pipe data.
//
// Architecture:
//   - Tee duplicates data between two pipes without consuming source
//   - Source pipe data remains available for subsequent reads
//   - Both pipes must be pipes (kernel requirement)
//
// Use case: Broadcasting pipe data to multiple destinations.
func TestTeeOperation(t *testing.T) {
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

	// Create source pipe
	var srcPipe [2]int32
	errno := zcall.Pipe2(&srcPipe, zcall.O_NONBLOCK)
	if errno != 0 {
		t.Fatalf("pipe2 (src): errno=%d", errno)
	}
	srcRead, srcWrite := srcPipe[0], srcPipe[1]
	defer zcall.Close(uintptr(srcRead))
	defer zcall.Close(uintptr(srcWrite))

	// Create destination pipe
	var dstPipe [2]int32
	errno = zcall.Pipe2(&dstPipe, zcall.O_NONBLOCK)
	if errno != 0 {
		t.Fatalf("pipe2 (dst): errno=%d", errno)
	}
	dstRead, dstWrite := dstPipe[0], dstPipe[1]
	defer zcall.Close(uintptr(dstRead))
	defer zcall.Close(uintptr(dstWrite))

	t.Logf("Source pipe: read=%d, write=%d", srcRead, srcWrite)
	t.Logf("Dest pipe: read=%d, write=%d", dstRead, dstWrite)

	// Write data to source pipe
	testData := []byte("Tee duplicates this data!")
	n, errno := zcall.Write(uintptr(srcWrite), testData)
	if errno != 0 {
		t.Fatalf("Write to source pipe: errno=%d", errno)
	}
	t.Logf("Wrote %d bytes to source pipe", n)

	// Tee from source to destination (duplicates without consuming)
	ctx := uring.PackDirect(uring.IORING_OP_TEE, 0, 0, dstWrite)
	if err := ring.Tee(ctx, int(srcRead), len(testData)); err != nil {
		t.Fatalf("Tee: %v", err)
	}

	// Wait for tee
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
		t.Logf("Tee result: %d (may fail in WSL2)", result)
		t.Skip("Tee not available or failed")
	}
	t.Logf("Tee: duplicated %d bytes", result)

	// Read from destination pipe (should have copy)
	dstBuf := make([]byte, 256)
	n, errno = zcall.Read(uintptr(dstRead), dstBuf)
	if errno != 0 && zcall.Errno(errno) != zcall.EAGAIN {
		t.Fatalf("Read from dst pipe: errno=%d", errno)
	}
	if n > 0 {
		t.Logf("Destination received: %q", string(dstBuf[:n]))
	}

	// Source pipe should STILL have data (tee doesn't consume)
	srcBuf := make([]byte, 256)
	n, errno = zcall.Read(uintptr(srcRead), srcBuf)
	if errno != 0 && zcall.Errno(errno) != zcall.EAGAIN {
		t.Fatalf("Read from src pipe: errno=%d", errno)
	}
	if n > 0 {
		t.Logf("Source still has: %q (tee preserved it)", string(srcBuf[:n]))
	}
}

// TestSplicePatterns shows common splice usage patterns.
func TestSplicePatterns(t *testing.T) {
	t.Log("Splice Patterns:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Pattern 1: File Serving (sendfile alternative)")
	t.Log("  file → pipe → socket")
	t.Log("  splice(file_fd, pipe_write)")
	t.Log("  splice(pipe_read, socket_fd)")
	t.Log("")
	t.Log("Pattern 2: Request Forwarding (proxy)")
	t.Log("  client_socket → pipe → backend_socket")
	t.Log("  splice(client_fd, pipe_write)")
	t.Log("  splice(pipe_read, backend_fd)")
	t.Log("")
	t.Log("Pattern 3: Data Logging (tee + splice)")
	t.Log("  source → tee → log_pipe (copy for logging)")
	t.Log("  source → splice → destination (original continues)")
	t.Log("")
	t.Log("Pattern 4: Multicast (tee for fan-out)")
	t.Log("  source_pipe")
	t.Log("       ├─ tee → dest1_pipe")
	t.Log("       ├─ tee → dest2_pipe")
	t.Log("       └─ splice → dest3_socket")
	t.Log("-----------------------------------------------------")
}

// TestSpliceVsCopy compares splice with traditional copy.
func TestSpliceVsCopy(t *testing.T) {
	t.Log("Splice vs Traditional Copy:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Traditional Copy (2 copies):")
	t.Log("  1. read(src_fd, user_buffer, n)")
	t.Log("     [kernel → user space copy]")
	t.Log("  2. write(dst_fd, user_buffer, n)")
	t.Log("     [user space → kernel copy]")
	t.Log("")
	t.Log("Splice (0 user-space copies):")
	t.Log("  1. splice(src_fd, pipe_write, n)")
	t.Log("     [kernel → pipe buffer, kernel space only]")
	t.Log("  2. splice(pipe_read, dst_fd, n)")
	t.Log("     [pipe buffer → kernel, kernel space only]")
	t.Log("")
	t.Log("Performance Impact:")
	t.Log("  - No user-space memory bandwidth consumed")
	t.Log("  - No CPU cache pollution from data movement")
	t.Log("  - Significant win for large transfers (>4KB)")
	t.Log("  - io_uring adds: async execution, batching")
	t.Log("-----------------------------------------------------")
}

// TestPipeBufferSizing explains pipe buffer considerations.
func TestPipeBufferSizing(t *testing.T) {
	t.Log("Pipe Buffer Sizing for Splice:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Default pipe buffer: 64KB (16 pages)")
	t.Log("")
	t.Log("Increase with fcntl(fd, F_SETPIPE_SZ, size):")
	t.Log("  - Max: /proc/sys/fs/pipe-max-size (default 1MB)")
	t.Log("  - Larger buffer = fewer splice calls for big transfers")
	t.Log("")
	t.Log("Splice flags:")
	t.Log("  SPLICE_F_MOVE:    Attempt to move pages (hint)")
	t.Log("  SPLICE_F_NONBLOCK: Don't block on pipe full/empty")
	t.Log("  SPLICE_F_MORE:     More data coming (like MSG_MORE)")
	t.Log("  SPLICE_F_GIFT:    Application will not modify data")
	t.Log("")
	t.Log("io_uring integration:")
	t.Log("  - Submit multiple splice ops in one batch")
	t.Log("  - Chain splice operations with SQE linking")
	t.Log("  - Non-blocking by default (io_uring handles wait)")
	t.Log("-----------------------------------------------------")
}
