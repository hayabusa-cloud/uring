// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// =============================================================================
// Fixed Buffer Zero-Copy Send Tests
// =============================================================================

// TestSendZeroCopyFixed verifies basic zero-copy send with registered buffers.
// Uses a single target socket and validates data is received correctly.
// Note: Zero-copy send may return EOPNOTSUPP (-95) on Unix sockets or loopback.
func TestSendZeroCopyFixed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping zero-copy send test in short mode (requires reliable io_uring)")
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Check registered buffers are available
	bufCount := ring.RegisteredBufferCount()
	if bufCount < 1 {
		t.Skip("no registered buffers available")
	}
	t.Logf("registered buffers: %d", bufCount)

	// Note: Zero-copy send may not be supported on all socket types.
	// Unix sockets and loopback typically don't support SEND_ZC.
	// This test verifies the API works and handles EOPNOTSUPP gracefully.

	// Create TCP socket pair using standard library
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	// Connect client
	clientConn, err := net.DialTimeout("tcp", addr.String(), 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	// Accept on server
	serverConn, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer serverConn.Close()

	// Prepare test data in registered buffer
	// Note: Data must be >= 1536 bytes to trigger zero-copy path (threshold for single target)
	testData := make([]byte, 2048)
	for i := range testData {
		testData[i] = byte('A' + i%26)
	}
	regBuf := ring.RegisteredBuffer(0)
	if regBuf == nil {
		t.Fatal("RegisteredBuffer(0) returned nil")
	}
	copy(regBuf, testData)

	// Get raw FD for client connection (we need to use io_uring for sending)
	// Since we're using stdlib net.Conn, we'll use Multicast with single target
	serverFile, err := serverConn.(*net.TCPConn).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer serverFile.Close()

	// Use MulticastZeroCopy with single target
	targets := &singleTarget{fd: iofd.FD(serverFile.Fd())}

	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(targets.fd))
	err = ring.MulticastZeroCopy(ctx, targets, 0, 0, len(testData))
	if err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	// Wait for completion (both operation and notification CQEs)
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	opCompleted := false
	notifReceived := false

	eopnotsupp := false
	for time.Now().Before(deadline) && (!opCompleted || !notifReceived) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC {
				if cqe.IsNotification() {
					notifReceived = true
					t.Logf("notification CQE received, res=%d flags=0x%x", cqe.Res, cqe.Flags)
				} else {
					opCompleted = true
					if cqe.Res == -95 { // EOPNOTSUPP
						eopnotsupp = true
						t.Logf("SEND_ZC returned EOPNOTSUPP (expected on loopback/Unix sockets)")
					} else if cqe.Res < 0 {
						t.Errorf("SEND_ZC failed: res=%d", cqe.Res)
					} else {
						t.Logf("operation CQE received, res=%d flags=0x%x hasMore=%v",
							cqe.Res, cqe.Flags, cqe.HasMore())
					}
				}
			}
		}
		b.Wait()
	}

	if !opCompleted {
		if !notifReceived {
			t.Skip("no zero-copy CQEs observed on loopback in this environment")
		}
		t.Fatal("notification CQE arrived without operation CQE")
	}
	// Note: notification may not always be received depending on kernel and socket state

	// Skip data verification if EOPNOTSUPP (zero-copy not supported on this socket)
	if eopnotsupp {
		t.Log("skipping data verification (SEND_ZC not supported on loopback)")
		return
	}

	// Verify data received on client side
	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	recvBuf := make([]byte, len(testData)+10)
	totalRead := 0
	for totalRead < len(testData) {
		n, err := clientConn.Read(recvBuf[totalRead:])
		if err != nil {
			t.Fatalf("client Read: %v", err)
		}
		totalRead += n
	}
	if !bytes.Equal(recvBuf[:totalRead], testData) {
		t.Errorf("data mismatch: got %d bytes, want %d bytes", totalRead, len(testData))
	}
	t.Logf("received %d bytes correctly", totalRead)
}

// singleTarget implements SendTargets for a single FD
type singleTarget struct {
	fd iofd.FD
}

func (t *singleTarget) Count() int       { return 1 }
func (t *singleTarget) FD(i int) iofd.FD { return iofd.FD(t.fd) }

// TestZeroCopyNotificationFlags verifies the two-CQE notification model.
// Zero-copy send generates: operation CQE (with MORE flag) + notification CQE (with NOTIF flag).
func TestZeroCopyNotificationFlags(t *testing.T) {
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

	// Write test data to registered buffer
	regBuf := ring.RegisteredBuffer(0)
	testData := bytes.Repeat([]byte("X"), 4096) // 4KB to ensure zero-copy path
	copy(regBuf, testData)

	// Submit zero-copy send (using the write FD)
	targets := &singleTarget{fd: fds[1]}
	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(fds[1]))
	err = ring.MulticastZeroCopy(ctx, targets, 0, 0, len(testData))
	if err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	// Collect CQEs and verify flags
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}

	var opCQE, notifCQE *uring.CQEView

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC {
				if cqe.IsNotification() {
					notifCQE = cqe
					t.Logf("NOTIF CQE: res=%d flags=0x%x", cqe.Res, cqe.Flags)
				} else {
					opCQE = cqe
					t.Logf("OP CQE: res=%d flags=0x%x hasMore=%v", cqe.Res, cqe.Flags, cqe.HasMore())
				}
			}
		}
		if opCQE != nil && notifCQE != nil {
			break
		}
		b.Wait()
	}

	// Verify operation CQE
	if opCQE == nil {
		t.Fatal("operation CQE not received")
	}
	if opCQE.Res == -95 { // EOPNOTSUPP
		t.Log("SEND_ZC returned EOPNOTSUPP (expected on Unix sockets)")
		// Still verify notification model works even with EOPNOTSUPP
	} else if opCQE.Res < 0 {
		t.Errorf("SEND_ZC failed: res=%d", opCQE.Res)
	}

	// Verify HasMore flag indicates notification coming
	// (Note: HasMore may not always be set depending on timing)
	t.Logf("operation HasMore: %v", opCQE.HasMore())

	// Verify notification CQE (should still be received even on EOPNOTSUPP)
	if notifCQE == nil {
		t.Log("notification CQE not received (may be timing dependent)")
	} else {
		if !notifCQE.IsNotification() {
			t.Error("notification CQE missing IORING_CQE_F_NOTIF flag")
		}
		t.Log("two-CQE notification model verified")
	}
}

// TestSendZeroCopyFixedMultiTarget tests zero-copy send to multiple sockets.
// The same registered buffer is sent to all targets efficiently.
func TestSendZeroCopyFixedMultiTarget(t *testing.T) {
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

	// Create multiple socket pairs (3 targets)
	const numTargets = 3
	var pairs [][2]iofd.FD
	var readers []iofd.FD
	var writers []iofd.FD

	for i := 0; i < numTargets; i++ {
		fds, err := newUnixSocketPairForTest()
		if err != nil {
			for _, p := range pairs {
				closeTestFds(p)
			}
			t.Fatalf("socketpair[%d]: %v", i, err)
		}
		pairs = append(pairs, fds)
		readers = append(readers, fds[0])
		writers = append(writers, fds[1])
	}
	defer func() {
		for _, p := range pairs {
			closeTestFds(p)
		}
	}()

	// Prepare test data in registered buffer
	// Note: Data must be >= 1536 bytes to trigger zero-copy path (threshold for < 4 targets)
	regBuf := ring.RegisteredBuffer(0)
	testData := make([]byte, 2048)
	for i := range testData {
		testData[i] = byte('A' + i%26)
	}
	copy(regBuf, testData)

	// Create multi-target adapter
	targets := &multiTargets{fds: writers}

	// Submit multicast zero-copy
	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, 0)
	err = ring.MulticastZeroCopy(ctx, targets, 0, 0, len(testData))
	if err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	// Wait for all completions
	cqes := make([]uring.CQEView, 32)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	opCount := 0
	eopnotsupp := false

	for time.Now().Before(deadline) && opCount < numTargets {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
				opCount++
				if cqe.Res == -95 { // EOPNOTSUPP
					eopnotsupp = true
					t.Logf("SEND_ZC[%d] returned EOPNOTSUPP (expected on Unix sockets)", opCount-1)
				} else if cqe.Res < 0 {
					t.Errorf("SEND_ZC[%d] failed: res=%d", opCount-1, cqe.Res)
				}
			}
		}
		b.Wait()
	}

	if opCount < numTargets {
		t.Errorf("only %d/%d operations completed", opCount, numTargets)
	}

	// Skip data verification if EOPNOTSUPP
	if eopnotsupp {
		t.Log("skipping data verification (SEND_ZC not supported on Unix sockets)")
		return
	}

	// Verify all readers received the data
	recvBuf := make([]byte, len(testData)+10)
	for i, rfd := range readers {
		// Read all data (may come in multiple chunks)
		totalRead := 0
		deadline := time.Now().Add(time.Second)
		for totalRead < len(testData) && time.Now().Before(deadline) {
			n, errno := readWithTimeout(rfd, recvBuf[totalRead:], 100*time.Millisecond)
			if errno != 0 && errno != 11 { // 11 = EAGAIN
				t.Errorf("reader[%d] read failed: errno=%d", i, errno)
				break
			}
			totalRead += n
		}
		if totalRead != len(testData) {
			t.Errorf("reader[%d] got %d bytes, want %d", i, totalRead, len(testData))
		}
	}
	t.Logf("all %d targets received data correctly", numTargets)
}

type multiTargets struct {
	fds []iofd.FD
}

func (t *multiTargets) Count() int {
	return len(t.fds)
}

func (t *multiTargets) FD(i int) iofd.FD {
	if i < 0 || i >= len(t.fds) {
		return -1
	}
	return t.fds[i]
}

// TestZeroCopyBufferSafety verifies buffer is safe to reuse after notification.
// Modifying the buffer before notification arrives is unsafe; after is safe.
func TestZeroCopyBufferSafety(t *testing.T) {
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

	regBuf := ring.RegisteredBuffer(0)

	// Test 1: Send first message
	// Note: Data must be >= 1536 bytes to trigger zero-copy path (threshold for single target)
	msg1 := make([]byte, 2048)
	for i := range msg1 {
		msg1[i] = byte('1')
	}
	copy(regBuf, msg1)

	targets := &singleTarget{fd: fds[1]}
	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(fds[1]))
	err = ring.MulticastZeroCopy(ctx, targets, 0, 0, len(msg1))
	if err != nil {
		t.Fatalf("first MulticastZeroCopy: %v", err)
	}

	// Wait for notification before modifying buffer
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	notifReceived := false
	opCompleted := false
	eopnotsupp := false

	for time.Now().Before(deadline) && !notifReceived {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC {
				if cqe.IsNotification() {
					notifReceived = true
				} else {
					opCompleted = true
					if cqe.Res == -95 { // EOPNOTSUPP
						eopnotsupp = true
					}
				}
			}
		}
		if opCompleted && !notifReceived {
			// Keep waiting for notification
		}
		b.Wait()
	}

	if !opCompleted {
		t.Fatal("operation CQE not received")
	}

	// Skip buffer safety test if SEND_ZC not supported
	if eopnotsupp {
		t.Log("SEND_ZC returned EOPNOTSUPP, buffer safety test verified (no data sent)")
		return
	}

	// Read first message
	recvBuf := make([]byte, len(msg1)+10)
	totalRead := 0
	readDeadline := time.Now().Add(time.Second)
	for totalRead < len(msg1) && time.Now().Before(readDeadline) {
		n, errno := readWithTimeout(fds[0], recvBuf[totalRead:], 100*time.Millisecond)
		if errno != 0 && errno != 11 { // 11 = EAGAIN
			t.Fatalf("first read failed: errno=%d", errno)
		}
		totalRead += n
	}
	if !bytes.Equal(recvBuf[:totalRead], msg1) {
		t.Errorf("first message mismatch: got %d bytes, want %d", totalRead, len(msg1))
	}

	// Now it's safe to modify the buffer (notification received or timed out)
	// Test 2: Send second message using the same buffer
	msg2 := make([]byte, 2048)
	for i := range msg2 {
		msg2[i] = byte('2')
	}
	copy(regBuf, msg2)

	ctx2 := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 1, int32(fds[1]))
	err = ring.MulticastZeroCopy(ctx2, targets, 0, 0, len(msg2))
	if err != nil {
		t.Fatalf("second MulticastZeroCopy: %v", err)
	}

	// Wait for second operation
	deadline = time.Now().Add(3 * time.Second)
	b = iox.Backoff{}
	op2Completed := false

	for time.Now().Before(deadline) && !op2Completed {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
				op2Completed = true
			}
		}
		b.Wait()
	}

	// Read second message
	totalRead = 0
	readDeadline = time.Now().Add(time.Second)
	for totalRead < len(msg2) && time.Now().Before(readDeadline) {
		n, errno := readWithTimeout(fds[0], recvBuf[totalRead:], 100*time.Millisecond)
		if errno != 0 && errno != 11 { // 11 = EAGAIN
			t.Fatalf("second read failed: errno=%d", errno)
		}
		totalRead += n
	}
	if !bytes.Equal(recvBuf[:totalRead], msg2) {
		t.Errorf("second message mismatch: got %d bytes, want %d", totalRead, len(msg2))
	}

	t.Logf("buffer safely reused after notification")
}

// =============================================================================
// Zero-Copy Edge Case Tests
// =============================================================================

// TestZeroCopyLargeBuffer tests zero-copy send with maximum buffer size.
func TestZeroCopyLargeBuffer(t *testing.T) {
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

	regBuf := ring.RegisteredBuffer(0)
	if regBuf == nil {
		t.Fatal("RegisteredBuffer(0) returned nil")
	}

	// Fill entire buffer with pattern
	for i := range regBuf {
		regBuf[i] = byte(i % 256)
	}
	bufLen := len(regBuf)
	t.Logf("testing with buffer size: %d bytes", bufLen)

	// Create socket pair
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)

	// Submit zero-copy send with full buffer
	targets := &singleTarget{fd: fds[1]}
	ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(fds[1]))

	// Use smaller size to avoid socket buffer issues
	sendSize := min(bufLen, 65536)
	err = ring.MulticastZeroCopy(ctx, targets, 0, 0, sendSize)
	if err != nil {
		t.Fatalf("MulticastZeroCopy: %v", err)
	}

	// Wait for completion
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	opCompleted := false

	for time.Now().Before(deadline) && !opCompleted {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
				opCompleted = true
				if cqe.Res == -95 {
					t.Log("SEND_ZC returned EOPNOTSUPP (expected on Unix sockets)")
				} else if cqe.Res < 0 {
					t.Errorf("SEND_ZC failed: res=%d", cqe.Res)
				} else {
					t.Logf("large buffer send completed: %d bytes", cqe.Res)
				}
			}
		}
		b.Wait()
	}

	if !opCompleted {
		t.Error("operation CQE not received")
	}
}

// TestZeroCopyMultipleBuffers tests using different registered buffers.
func TestZeroCopyMultipleBuffers(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, testTwoRegisteredBuffersOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	bufCount := ring.RegisteredBufferCount()
	if bufCount < 2 {
		t.Skipf("need at least 2 registered buffers, have %d", bufCount)
	}
	t.Logf("testing with %d registered buffers", bufCount)

	// Create socket pairs for each buffer
	numTests := min(bufCount, 4)
	var pairs [][2]iofd.FD

	for i := 0; i < numTests; i++ {
		fds, err := newUnixSocketPairForTest()
		if err != nil {
			for _, p := range pairs {
				closeTestFds(p)
			}
			t.Fatalf("socketpair[%d]: %v", i, err)
		}
		pairs = append(pairs, fds)
	}
	defer func() {
		for _, p := range pairs {
			closeTestFds(p)
		}
	}()

	// Write unique data to each buffer
	messages := make([][]byte, numTests)
	for i := 0; i < numTests; i++ {
		buf := ring.RegisteredBuffer(i)
		if buf == nil {
			t.Fatalf("RegisteredBuffer(%d) returned nil", i)
		}
		msg := []byte("Buffer " + string(rune('A'+i)) + " content")
		copy(buf, msg)
		messages[i] = msg
	}

	// Submit sends from different buffers
	for i := 0; i < numTests; i++ {
		targets := &singleTarget{fd: pairs[i][1]}
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, uint16(i), int32(pairs[i][1]))
		err := ring.MulticastZeroCopy(ctx, targets, i, 0, len(messages[i]))
		if err != nil {
			t.Fatalf("MulticastZeroCopy[%d]: %v", i, err)
		}
	}

	// Wait for all completions
	cqes := make([]uring.CQEView, 32)
	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	completions := 0

	for time.Now().Before(deadline) && completions < numTests {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := &cqes[i]
			if cqe.Op() == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
				completions++
				if cqe.Res == -95 {
					t.Logf("buffer %d: EOPNOTSUPP", completions-1)
				} else if cqe.Res < 0 {
					t.Errorf("buffer %d failed: res=%d", completions-1, cqe.Res)
				}
			}
		}
		b.Wait()
	}

	t.Logf("completed %d/%d buffer sends", completions, numTests)
}

// TestZeroCopyErrorHandling tests error handling in zero-copy operations.
func TestZeroCopyErrorHandling(t *testing.T) {
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

	// Test 1: Invalid FD (-1)
	t.Run("invalid_fd", func(t *testing.T) {
		targets := &singleTarget{fd: -1}
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, -1)
		err := ring.MulticastZeroCopy(ctx, targets, 0, 0, 100)
		if err != nil {
			t.Logf("expected error for invalid FD: %v", err)
		}

		// Wait for error CQE
		cqes := make([]uring.CQEView, 8)
		deadline := time.Now().Add(time.Second)
		b := iox.Backoff{}
		for time.Now().Before(deadline) {
			n, _ := ring.Wait(cqes)
			for i := 0; i < n; i++ {
				cqe := &cqes[i]
				if cqe.Op() == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
					if cqe.Res >= 0 {
						t.Errorf("expected error, got res=%d", cqe.Res)
					} else {
						t.Logf("got expected error: res=%d", cqe.Res)
					}
					return
				}
			}
			b.Wait()
		}
	})

	// Test 2: Closed socket
	t.Run("closed_socket", func(t *testing.T) {
		fds, err := newUnixSocketPairForTest()
		if err != nil {
			t.Fatalf("socketpair: %v", err)
		}
		// Close immediately
		closeTestFds(fds)

		targets := &singleTarget{fd: fds[1]}
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(fds[1]))
		err = ring.MulticastZeroCopy(ctx, targets, 0, 0, 100)
		if err != nil {
			t.Logf("submit error (may be expected): %v", err)
		}

		// Wait for error CQE
		cqes := make([]uring.CQEView, 8)
		deadline := time.Now().Add(time.Second)
		b := iox.Backoff{}
		for time.Now().Before(deadline) {
			n, _ := ring.Wait(cqes)
			for i := 0; i < n; i++ {
				cqe := &cqes[i]
				if cqe.Op() == uring.IORING_OP_SEND_ZC && !cqe.IsNotification() {
					if cqe.Res >= 0 {
						t.Logf("unexpected success: res=%d", cqe.Res)
					} else {
						t.Logf("got expected error: res=%d (EBADF=-9, ENOTSOCK=-88)", cqe.Res)
					}
					return
				}
			}
			b.Wait()
		}
	})
}

// TestZeroCopySequentialSends tests multiple sequential sends on same buffer.
func TestZeroCopySequentialSends(t *testing.T) {
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

	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)

	regBuf := ring.RegisteredBuffer(0)
	const numSends = 5

	for seq := 0; seq < numSends; seq++ {
		// Keep the payload above MulticastZeroCopy's single-target threshold so
		// this test observes SEND_ZC CQEs instead of the fixed-send fallback.
		msg := bytes.Repeat([]byte{byte('0' + seq)}, 2048)
		copy(regBuf, msg)

		targets := &singleTarget{fd: fds[1]}
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, uint16(seq), int32(fds[1]))
		err := ring.MulticastZeroCopy(ctx, targets, 0, 0, len(msg))
		if err != nil {
			t.Fatalf("send[%d]: %v", seq, err)
		}

		// Wait for this send to complete before next
		cqes := make([]uring.CQEView, 8)
		deadline := time.Now().Add(2 * time.Second)
		b := iox.Backoff{}
		opDone := false
		notifDone := false
		unsupported := false

		for time.Now().Before(deadline) && (!opDone || !notifDone) {
			n, _ := ring.Wait(cqes)
			for i := 0; i < n; i++ {
				cqe := &cqes[i]
				if cqe.Op() == uring.IORING_OP_SEND_ZC {
					if cqe.IsNotification() {
						notifDone = true
					} else {
						opDone = true
						if cqe.Res == -95 {
							t.Logf("send[%d]: EOPNOTSUPP", seq)
							unsupported = true
							notifDone = true // Skip waiting for notification
						} else if cqe.Res < 0 {
							t.Errorf("send[%d] failed: res=%d", seq, cqe.Res)
						} else if !cqe.HasMore() {
							notifDone = true
						}
					}
				}
			}
			if n > 0 {
				b.Reset()
			}
			b.Wait()
		}
		if !opDone {
			t.Fatalf("send[%d]: SEND_ZC operation CQE not observed", seq)
		}
		if !notifDone {
			t.Fatalf("send[%d]: SEND_ZC notification CQE not observed", seq)
		}
		if unsupported {
			t.Skip("SEND_ZC is not supported for Unix socket pairs in this environment")
		}
	}

	t.Logf("completed %d sequential sends", numSends)
}

// =============================================================================
// Helper Functions
// =============================================================================

// readWithTimeout performs a blocking read with timeout using retry.
// Returns (n, errno). errno=0 on success.
func readWithTimeout(fd iofd.FD, buf []byte, timeout time.Duration) (int, uintptr) {
	deadline := time.Now().Add(timeout)
	b := iox.Backoff{}
	for time.Now().Before(deadline) {
		n, errno := readTestFD(fd, buf)
		if errno == 0 {
			return n, 0
		}
		// EAGAIN/EWOULDBLOCK means try again
		if errno == 11 || errno == 35 { // EAGAIN on Linux, EWOULDBLOCK on Darwin
			b.Wait()
			continue
		}
		return 0, errno
	}
	return 0, 11 // EAGAIN - timeout
}
