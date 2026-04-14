// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// BenchmarkZeroCopyVsNormal compares zero-copy and normal send at various sizes.
// Run with: go test -bench=BenchmarkZeroCopyVsNormal -benchtime=1s
func BenchmarkZeroCopyVsNormal(b *testing.B) {
	// Test various payload sizes to find crossover point
	sizes := []int{
		512,   // Small packet
		1024,  // 1 KB
		2048,  // 2 KB
		3072,  // 3 KB (theoretical crossover)
		4096,  // 4 KB (page size)
		8192,  // 8 KB
		16384, // 16 KB
		32768, // 32 KB
		65536, // 64 KB
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Normal_%dB", size), func(b *testing.B) {
			benchmarkNormalSend(b, size)
		})
		b.Run(fmt.Sprintf("ZeroCopy_%dB", size), func(b *testing.B) {
			benchmarkZeroCopySend(b, size)
		})
	}
}

func benchmarkNormalSend(b *testing.B, payloadSize int) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	// Create TCP connection
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		b.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := ln.Accept()
	if err != nil {
		b.Fatalf("Accept: %v", err)
	}
	defer serverConn.Close()

	// Get raw FD
	tcpConn := serverConn.(*net.TCPConn)
	file, err := tcpConn.File()
	if err != nil {
		b.Fatalf("File: %v", err)
	}
	defer file.Close()
	fd := int(file.Fd())

	// Prepare payload
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	// Drain receiver in background
	go func() {
		buf := make([]byte, 65536)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	cqes := make([]uring.CQEView, 16)
	pollFd := &benchPollFd{fd: iofd.FD(fd)}

	b.ResetTimer()
	b.SetBytes(int64(payloadSize))

	for i := 0; i < b.N; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_SEND, 0, uint16(i), int32(fd))
		err := ring.Send(ctx, pollFd, payload)
		if err != nil {
			b.Fatalf("Send: %v", err)
		}

		// Wait for completion
		for {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
				b.Fatalf("Wait: %v", err)
			}
			if n > 0 {
				break
			}
		}
	}
}

func benchmarkZeroCopySend(b *testing.B, payloadSize int) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	if ring.RegisteredBufferCount() < 1 {
		b.Skip("no registered buffers")
	}

	regBuf := ring.RegisteredBuffer(0)
	if regBuf == nil || len(regBuf) < payloadSize {
		b.Skip("registered buffer too small")
	}

	// Create TCP connection
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		b.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := ln.Accept()
	if err != nil {
		b.Fatalf("Accept: %v", err)
	}
	defer serverConn.Close()

	// Get raw FD
	tcpConn := serverConn.(*net.TCPConn)
	file, err := tcpConn.File()
	if err != nil {
		b.Fatalf("File: %v", err)
	}
	defer file.Close()
	fd := int(file.Fd())

	// Prepare payload in registered buffer
	for i := 0; i < payloadSize; i++ {
		regBuf[i] = byte(i % 256)
	}

	// Drain receiver in background
	go func() {
		buf := make([]byte, 65536)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	cqes := make([]uring.CQEView, 16)
	targets := &singleTarget{fd: iofd.FD(fd)}

	b.ResetTimer()
	b.SetBytes(int64(payloadSize))

	eopnotsupp := false
	for i := 0; i < b.N; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, uint16(i), int32(fd))
		err := ring.MulticastZeroCopy(ctx, targets, 0, 0, payloadSize)
		if err != nil {
			b.Fatalf("MulticastZeroCopy: %v", err)
		}

		// Wait for both CQEs (operation + notification)
		opDone := false
		notifDone := false
		deadline := time.Now().Add(time.Second)
		bo := iox.Backoff{}
		for time.Now().Before(deadline) && (!opDone || !notifDone) {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
				b.Fatalf("Wait: %v", err)
			}
			if n == 0 {
				bo.Wait()
				continue
			}
			for j := 0; j < n; j++ {
				cqe := &cqes[j]
				op := cqe.Op()
				// Handle both SEND_ZC (zero-copy path) and WRITE_FIXED (fallback path)
				if op == uring.IORING_OP_SEND_ZC {
					if cqe.IsNotification() {
						notifDone = true
					} else {
						opDone = true
						if cqe.Res == -95 && !eopnotsupp {
							eopnotsupp = true
							b.Log("EOPNOTSUPP - zero-copy not supported on loopback")
						}
					}
				} else if op == uring.IORING_OP_WRITE_FIXED {
					// writeFixed path: single CQE, no notification
					opDone = true
					notifDone = true
				}
			}
		}
	}
}

type benchPollFd struct {
	fd iofd.FD
}

func (p *benchPollFd) Fd() int          { return int(p.fd) }
func (p *benchPollFd) Events() uint32   { return 0 }
func (p *benchPollFd) SetEvents(uint32) {}

func startSocketPairDrainer(tb testing.TB, fd iofd.FD) func() {
	tb.Helper()

	drainFD := fd
	if err := sock.SetNonBlock(&drainFD, true); err != nil {
		tb.Fatalf("SetNonBlock: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)

		buf := make([]byte, 65536)
		backoff := iox.Backoff{}
		for {
			select {
			case <-stop:
				return
			default:
			}

			n, errno := readTestFD(drainFD, buf)
			switch {
			case errno == 0:
				if n == 0 {
					return
				}
				backoff.Reset()
			case errno == uintptr(zcall.EAGAIN), errno == uintptr(zcall.EWOULDBLOCK):
				backoff.Wait()
			default:
				return
			}
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}

// TestZeroCopyVsNormalComparison runs a quick comparison test.
// This is for testing, not benchmarking - use the benchmark for accurate measurements.
// Note: Uses fewer iterations to avoid timeout in constrained environments (e.g., WSL2).
func TestZeroCopyVsNormalComparison(t *testing.T) {
	if raceEnabled {
		t.Skip("skipping timing comparison under race detector")
	}
	if testing.Short() {
		t.Skip("skipping comparison test in short mode")
	}

	// Use smaller sizes and fewer iterations for test (not benchmark)
	// Full benchmark should be run with: go test -bench=BenchmarkZeroCopy
	sizes := []int{1024, 4096, 16384}
	iterations := 10 // Reduced from 100 to avoid timeout in WSL2

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dB", size), func(t *testing.T) {
			normalNs := measureNormalSend(t, size, iterations)
			zcNs := measureZeroCopySend(t, size, iterations)

			t.Logf("Payload %d bytes:", size)
			t.Logf("  Normal:    %d ns/op", normalNs)
			t.Logf("  ZeroCopy:  %d ns/op", zcNs)
			if zcNs < normalNs {
				t.Logf("  Winner: ZeroCopy (%.1f%% faster)", float64(normalNs-zcNs)/float64(normalNs)*100)
			} else {
				t.Logf("  Winner: Normal (%.1f%% faster)", float64(zcNs-normalNs)/float64(zcNs)*100)
			}
		})
	}
}

func measureNormalSend(t *testing.T, payloadSize, iterations int) int64 {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)
	stopDrain := startSocketPairDrainer(t, fds[0])
	defer stopDrain()

	payload := make([]byte, payloadSize)
	cqes := make([]uring.CQEView, 16)
	pollFd := &benchPollFd{fd: fds[1]}

	start := time.Now()
	b := iox.Backoff{}
	for i := 0; i < iterations; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_SEND, 0, uint16(i), int32(fds[1]))
		err := ring.Send(ctx, pollFd, payload)
		if err != nil {
			t.Fatalf("Send: %v", err)
		}

		completed := false
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			n, _ := ring.Wait(cqes)
			if n > 0 {
				b.Reset()
				completed = true
				break
			}
			b.Wait()
		}
		if !completed {
			t.Skip("skipping comparison test: send completion did not arrive in time")
		}
	}
	elapsed := time.Since(start)
	return elapsed.Nanoseconds() / int64(iterations)
}

func measureZeroCopySend(t *testing.T, payloadSize, iterations int) int64 {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("no registered buffers")
	}

	regBuf := ring.RegisteredBuffer(0)
	if regBuf == nil || len(regBuf) < payloadSize {
		t.Skipf("registered buffer too small: need %d, have %d", payloadSize, len(regBuf))
	}

	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)
	stopDrain := startSocketPairDrainer(t, fds[0])
	defer stopDrain()

	cqes := make([]uring.CQEView, 16)
	targets := &singleTarget{fd: fds[1]}

	start := time.Now()
	eopnotsupp := false
	b := iox.Backoff{}
	for i := 0; i < iterations; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, uint16(i), int32(fds[1]))
		err := ring.MulticastZeroCopy(ctx, targets, 0, 0, payloadSize)
		if err != nil {
			t.Fatalf("MulticastZeroCopy: %v", err)
		}

		opDone := false
		notifDone := false
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && (!opDone || !notifDone) {
			n, _ := ring.Wait(cqes)
			if n == 0 {
				b.Wait()
				continue
			}
			b.Reset()
			for j := 0; j < n; j++ {
				cqe := &cqes[j]
				op := cqe.Op()
				// Handle both SEND_ZC (zero-copy path) and WRITE_FIXED (fallback path)
				if op == uring.IORING_OP_SEND_ZC {
					if cqe.IsNotification() {
						notifDone = true
					} else {
						opDone = true
						if cqe.Res == -95 {
							eopnotsupp = true
							notifDone = true // Skip notification wait
						}
					}
				} else if op == uring.IORING_OP_WRITE_FIXED {
					// writeFixed path: single CQE, no notification
					opDone = true
					notifDone = true
				}
			}
		}
		if !opDone || !notifDone {
			t.Skip("skipping comparison test: zero-copy completion did not arrive in time")
		}
	}
	elapsed := time.Since(start)

	if eopnotsupp {
		t.Log("EOPNOTSUPP - returning inflated time for comparison")
		return elapsed.Nanoseconds()/int64(iterations) + 1000000 // Add 1ms penalty
	}

	return elapsed.Nanoseconds() / int64(iterations)
}

// TestMultiDestinationComparison tests the multicast scenario.
// Note: Uses smaller destination counts to avoid timeout in constrained environments.
func TestMultiDestinationComparison(t *testing.T) {
	if raceEnabled {
		t.Skip("skipping timing comparison under race detector")
	}
	if testing.Short() {
		t.Skip("skipping multi-destination test in short mode")
	}

	// Reduced destination counts to avoid timeout in WSL2
	destCounts := []int{1, 5, 10}
	payloadSize := 4096
	iterations := 5 // Reduced from 10

	for _, numDest := range destCounts {
		t.Run(fmt.Sprintf("N%d", numDest), func(t *testing.T) {
			normalNs := measureNormalMultiSend(t, payloadSize, numDest, iterations)
			zcNs := measureZeroCopyMultiSend(t, payloadSize, numDest, iterations)

			t.Logf("Destinations: %d, Payload: %d bytes:", numDest, payloadSize)
			t.Logf("  Normal:    %d ns/op", normalNs)
			t.Logf("  ZeroCopy:  %d ns/op", zcNs)
			if zcNs < normalNs {
				improvement := float64(normalNs-zcNs) / float64(normalNs) * 100
				t.Logf("  Winner: ZeroCopy (%.1f%% faster)", improvement)
			} else {
				improvement := float64(zcNs-normalNs) / float64(zcNs) * 100
				t.Logf("  Winner: Normal (%.1f%% faster)", improvement)
			}
		})
	}
}

func measureNormalMultiSend(t *testing.T, payloadSize, numDest, iterations int) int64 {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesMedium
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create socket pairs
	var pairs [][2]iofd.FD
	var stopDrainers []func()
	for i := 0; i < numDest; i++ {
		fds, err := newUnixSocketPairForTest()
		if err != nil {
			for _, p := range pairs {
				closeTestFds(p)
			}
			t.Fatalf("socketpair: %v", err)
		}
		pairs = append(pairs, fds)
		stopDrainers = append(stopDrainers, startSocketPairDrainer(t, fds[0]))
	}
	defer func() {
		for i := len(stopDrainers) - 1; i >= 0; i-- {
			stopDrainers[i]()
		}
	}()
	defer func() {
		for _, p := range pairs {
			closeTestFds(p)
		}
	}()

	payload := make([]byte, payloadSize)
	cqes := make([]uring.CQEView, 256)

	start := time.Now()
	for iter := 0; iter < iterations; iter++ {
		// Send to all destinations
		for i, p := range pairs {
			pollFd := &benchPollFd{fd: p[1]}
			ctx := uring.PackDirect(uring.IORING_OP_SEND, 0, uint16(iter*numDest+i), int32(p[1]))
			err := ring.Send(ctx, pollFd, payload)
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
		}

		// Wait for all completions
		completed := 0
		deadline := time.Now().Add(time.Second)
		b := iox.Backoff{}
		for time.Now().Before(deadline) && completed < numDest {
			n, _ := ring.Wait(cqes)
			if n == 0 {
				b.Wait()
				continue
			}
			b.Reset()
			completed += n
		}
		if completed < numDest {
			t.Skipf("skipping comparison test: received %d/%d send completions", completed, numDest)
		}
	}
	elapsed := time.Since(start)
	return elapsed.Nanoseconds() / int64(iterations)
}

func measureZeroCopyMultiSend(t *testing.T, payloadSize, numDest, iterations int) int64 {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesMedium
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("no registered buffers")
	}

	regBuf := ring.RegisteredBuffer(0)
	if regBuf == nil || len(regBuf) < payloadSize {
		t.Skipf("registered buffer too small")
	}

	// Create socket pairs
	var pairs [][2]iofd.FD
	var writers []iofd.FD
	var stopDrainers []func()
	for i := 0; i < numDest; i++ {
		fds, err := newUnixSocketPairForTest()
		if err != nil {
			for _, p := range pairs {
				closeTestFds(p)
			}
			t.Fatalf("socketpair: %v", err)
		}
		pairs = append(pairs, fds)
		writers = append(writers, fds[1])
		stopDrainers = append(stopDrainers, startSocketPairDrainer(t, fds[0]))
	}
	defer func() {
		for i := len(stopDrainers) - 1; i >= 0; i-- {
			stopDrainers[i]()
		}
	}()
	defer func() {
		for _, p := range pairs {
			closeTestFds(p)
		}
	}()

	cqes := make([]uring.CQEView, 256)
	targets := &multiTargets{fds: writers}

	start := time.Now()
	eopnotsupp := false
	b := iox.Backoff{}
	for iter := 0; iter < iterations; iter++ {
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, uint16(iter), 0)
		err := ring.MulticastZeroCopy(ctx, targets, 0, 0, payloadSize)
		if err != nil {
			t.Fatalf("MulticastZeroCopy: %v", err)
		}

		// Wait for all operation completions (notifications may come later)
		completed := 0
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && completed < numDest {
			n, _ := ring.Wait(cqes)
			if n == 0 {
				b.Wait()
				continue
			}
			b.Reset()
			for j := 0; j < n; j++ {
				cqe := &cqes[j]
				op := cqe.Op()
				// Handle both SEND_ZC (zero-copy path) and WRITE_FIXED (fallback path)
				if op == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
					completed++
					if cqe.Res == -95 {
						eopnotsupp = true
					}
				} else if op == uring.IORING_OP_WRITE_FIXED {
					// writeFixed path: single CQE per destination
					completed++
				}
			}
		}
		if completed < numDest {
			t.Skipf("skipping comparison test: received %d/%d zero-copy completions", completed, numDest)
		}
	}
	elapsed := time.Since(start)

	if eopnotsupp {
		t.Log("EOPNOTSUPP - returning inflated time")
		return elapsed.Nanoseconds()/int64(iterations) + 1000000
	}

	return elapsed.Nanoseconds() / int64(iterations)
}
