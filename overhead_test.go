// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// TestOverheadComparison measures the pure overhead of zero-copy vs normal operations.
// Uses NOP operations to isolate the io_uring submission/completion overhead.
func TestOverheadComparison(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	const iterations = 1000
	cqes := make([]uring.CQEView, 16)

	// Measure NOP operation overhead (baseline)
	start := time.Now()
	for i := 0; i < iterations; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, uint16(i), 0)
		err := ring.Nop(ctx)
		if err != nil {
			t.Fatalf("Nop: %v", err)
		}

		// Wait for completion
		b := iox.Backoff{}
		dl := time.Now().Add(2 * time.Second)
		for time.Now().Before(dl) {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
				t.Fatalf("Wait: %v", err)
			}
			if n > 0 {
				break
			}
			b.Wait()
		}
	}
	nopDuration := time.Since(start)
	nopNsPerOp := nopDuration.Nanoseconds() / iterations

	t.Logf("NOP baseline: %d ns/op (%d iterations)", nopNsPerOp, iterations)
	t.Logf("This represents the pure io_uring SQ/CQ overhead")
}

// TestCQEProcessingOverhead measures the overhead of processing CQE flags.
func TestCQEProcessingOverhead(t *testing.T) {
	const iterations = 1000000

	// Simulate processing CQE flags
	cqe := uring.CQEView{
		Res:   4096,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER | (42 << uring.IORING_CQE_BUFFER_SHIFT),
	}

	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = cqe.HasMore()
		_ = cqe.HasBuffer()
		_ = cqe.IsNotification()
		_ = cqe.BufID()
	}
	flagsDuration := time.Since(start)

	t.Logf("CQE flag processing: %d ns/%d checks", flagsDuration.Nanoseconds()/iterations, 4)
}

// TestTwoCQEOverhead measures the overhead of the two-CQE notification model.
// Compares single CQE (normal send) vs two CQEs (zero-copy send).
func TestTwoCQEOverhead(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("no registered buffers available")
	}

	// Create socket pair
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)

	cqes := make([]uring.CQEView, 16)
	targets := &singleTarget{fd: fds[1]}

	// Submit a single zero-copy send to measure CQE pattern
	// Note: Data must be >= 1536 bytes to trigger zero-copy path (threshold for single target)
	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(fds[1]))
	err = ring.MulticastZeroCopy(ctx, targets, 0, 0, 2048)
	if err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	// Track CQE timing
	submitTime := time.Now()
	var opTime, notifTime time.Time
	opReceived := false
	notifReceived := false
	eopnotsupp := false

	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	for time.Now().Before(deadline) && (!opReceived || !notifReceived) {
		n, _ := ring.Wait(cqes)
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC {
				if cqe.IsNotification() {
					notifTime = time.Now()
					notifReceived = true
				} else {
					opTime = time.Now()
					opReceived = true
					if cqe.Res == -95 {
						eopnotsupp = true
					}
				}
			}
		}
		b.Wait()
	}

	if !opReceived {
		t.Fatal("operation CQE not received")
	}

	t.Logf("Submit → Operation CQE: %v", opTime.Sub(submitTime))
	if notifReceived {
		t.Logf("Operation CQE → Notification CQE: %v", notifTime.Sub(opTime))
		t.Logf("Total (two-CQE model): %v", notifTime.Sub(submitTime))
	} else if eopnotsupp {
		t.Log("EOPNOTSUPP - notification timing not available on Unix sockets")
	}
}

// TestMemoryCopyOverhead estimates memory copy cost at various sizes.
func TestMemoryCopyOverhead(t *testing.T) {
	sizes := []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
	const iterations = 10000

	for _, size := range sizes {
		src := make([]byte, size)
		dst := make([]byte, size)

		// Fill source with pattern
		for i := range src {
			src[i] = byte(i)
		}

		start := time.Now()
		for i := 0; i < iterations; i++ {
			copy(dst, src)
		}
		duration := time.Since(start)
		nsPerOp := duration.Nanoseconds() / iterations

		// Calculate bandwidth
		bytesPerSec := float64(size) * float64(iterations) / duration.Seconds()
		gbPerSec := bytesPerSec / (1024 * 1024 * 1024)

		t.Logf("%6d bytes: %4d ns/copy, %.2f GB/s", size, nsPerOp, gbPerSec)
	}
}

// TestCrossoverAnalysis provides an analysis of where zero-copy becomes beneficial.
func TestCrossoverAnalysis(t *testing.T) {
	t.Log("=== Zero-Copy vs Normal I/O Crossover Analysis ===")
	t.Log("")

	// Theoretical costs (from research)
	const (
		// io_uring overhead per operation (approximate)
		sqeSubmitNs     = 100 // SQE submission
		cqeProcessNs    = 50  // CQE processing
		memcpyPerByteNs = 0.1 // ~10 GB/s memory bandwidth

		// Zero-copy additional overhead
		pinnedLookupNs = 100 // io_import_fixed() lookup
		notifCQENs     = 50  // Second CQE processing

		// Page pinning (amortized with registered buffers)
		registeredPinNs = 0 // Pre-registered, no per-send cost
	)

	t.Log("Theoretical Cost Model:")
	t.Logf("  SQE submission:     %d ns", sqeSubmitNs)
	t.Logf("  CQE processing:     %d ns", cqeProcessNs)
	t.Logf("  Memory copy:        %.1f ns/byte", memcpyPerByteNs)
	t.Logf("  Fixed buffer lookup: %d ns", pinnedLookupNs)
	t.Logf("  Notification CQE:   %d ns", notifCQENs)
	t.Log("")

	t.Log("Cost Analysis by Payload Size:")
	t.Log("")
	t.Log("| Size    | Normal Cost | ZC Cost | Winner    | Savings |")
	t.Log("|---------|-------------|---------|-----------|---------|")

	sizes := []int{512, 1024, 2048, 3072, 4096, 8192, 16384, 32768, 65536}
	for _, size := range sizes {
		// Normal send: submit + copy + completion
		normalCost := float64(sqeSubmitNs) + float64(size)*memcpyPerByteNs + float64(cqeProcessNs)

		// Zero-copy send: submit + lookup + completion + notification
		zcCost := float64(sqeSubmitNs) + float64(pinnedLookupNs) + float64(cqeProcessNs) + float64(notifCQENs)

		var winner string
		var savings float64
		if zcCost < normalCost {
			winner = "ZeroCopy"
			savings = (normalCost - zcCost) / normalCost * 100
		} else {
			winner = "Normal"
			savings = (zcCost - normalCost) / zcCost * 100
		}

		t.Logf("| %5dB  | %7.0f ns  | %5.0f ns | %-9s | %5.1f%% |",
			size, normalCost, zcCost, winner, savings)
	}

	t.Log("")
	t.Log("Crossover Point Calculation:")
	// Solve: sqeSubmitNs + size*memcpyPerByteNs + cqeProcessNs = sqeSubmitNs + pinnedLookupNs + cqeProcessNs + notifCQENs
	// size*memcpyPerByteNs = pinnedLookupNs + notifCQENs
	// size = (pinnedLookupNs + notifCQENs) / memcpyPerByteNs
	crossover := float64(pinnedLookupNs+notifCQENs) / memcpyPerByteNs
	t.Logf("  Theoretical crossover: %.0f bytes", crossover)
	t.Log("")

	t.Log("Multi-Destination Scaling (4KB payload):")
	t.Log("")
	t.Log("| Dests | Normal Cost | ZC Cost   | ZC Savings |")
	t.Log("|-------|-------------|-----------|------------|")

	payload := 4096
	for _, n := range []int{1, 10, 50, 100, 500, 1000} {
		// Normal: N copies
		normalCost := float64(n) * (float64(sqeSubmitNs) + float64(payload)*memcpyPerByteNs + float64(cqeProcessNs))

		// Zero-copy: 1 setup + N sends (no copy) + N notifications
		zcCost := float64(pinnedLookupNs) + float64(n)*(float64(sqeSubmitNs)+float64(cqeProcessNs)+float64(notifCQENs))

		savings := (normalCost - zcCost) / normalCost * 100
		t.Logf("| %5d | %9.0f ns | %9.0f ns | %6.1f%%    |",
			n, normalCost, zcCost, savings)
	}
}

// TestRegisteredBufferVsDynamic compares registered vs dynamic buffer performance.
func TestRegisteredBufferVsDynamic(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	bufCount := ring.RegisteredBufferCount()
	t.Logf("Registered buffers: %d", bufCount)

	if bufCount > 0 {
		buf := ring.RegisteredBuffer(0)
		t.Logf("Buffer 0 size: %d bytes", len(buf))
	}

	// Measure registered buffer access time
	if bufCount > 0 {
		const iterations = 1000000
		start := time.Now()
		for i := 0; i < iterations; i++ {
			buf := ring.RegisteredBuffer(0)
			if buf == nil {
				t.Fatal("unexpected nil")
			}
		}
		duration := time.Since(start)
		t.Logf("RegisteredBuffer(0) lookup: %d ns/op", duration.Nanoseconds()/iterations)
	}

	// Compare with dynamic allocation
	const iterations = 1000000
	size := 4096
	start := time.Now()
	for i := 0; i < iterations; i++ {
		buf := make([]byte, size)
		_ = buf[0] // Use to prevent optimization
	}
	duration := time.Since(start)
	t.Logf("Dynamic allocation (%d bytes): %d ns/op", size, duration.Nanoseconds()/iterations)
}

// BenchmarkMemcpySizes benchmarks memory copy at various sizes.
func BenchmarkMemcpySizes(b *testing.B) {
	sizes := []int{512, 1024, 2048, 4096, 8192, 16384, 65536}

	for _, size := range sizes {
		src := make([]byte, size)
		dst := make([]byte, size)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				copy(dst, src)
			}
		})
	}
}

// BenchmarkRegisteredBufferLookup benchmarks registered buffer access.
func BenchmarkRegisteredBufferLookup(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	if ring.RegisteredBufferCount() < 1 {
		b.Skip("no registered buffers")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ring.RegisteredBuffer(0)
	}
}
