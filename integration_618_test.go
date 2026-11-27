// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// Integration tests for kernel 6.18+ runtime expectations.
// These tests verify real kernel behavior against the Linux 6.18+ baseline.

func TestIntegrationZeroCopySend(t *testing.T) {
	payload := []byte("Integration test: zero-copy send")
	var pin runtime.Pinner
	pin.Pin(&payload[0])
	t.Cleanup(pin.Unpin)

	ring := newTestRing(t)
	fd, peer := newTestTCPPair(t)
	tracker := uring.NewZCTracker(ring, uring.NewContextPools(1))
	for sent := 0; sent < len(payload); {
		var h testZCHandler
		if err := tracker.SendZC(fd, payload[sent:], &h); err != nil {
			t.Fatal(err)
		}
		drainTrackedSends(t, ring, tracker, 1)
		n := h.completedRes.Load()
		if n <= 0 || int(n) > len(payload)-sent {
			t.Fatalf("send result = %d for %d remaining bytes", n, len(payload)-sent)
		}
		h.check(t, 1, n)
		sent += int(n)
	}

	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(peer, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("received %q, want %q: %v", got, payload, err)
	}
}

func consumeAcceptCQEs(cqes []uring.CQEView) (accepted int, errRes int32) {
	for i := 0; i < len(cqes); i++ {
		cqe := cqes[i]
		if cqe.Op() != uring.IORING_OP_ACCEPT {
			continue
		}
		if cqe.Res < 0 {
			return accepted, cqe.Res
		}
		accepted++
		closeTestFD(iofd.FD(cqe.Res))
	}
	return accepted, 0
}

func cancelAcceptMultishot(t *testing.T, ring *uring.Uring, acceptCtx uring.SQEContext, cqes []uring.CQEView) (accepted int, terminal bool) {
	t.Helper()

	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, acceptCtx.FD())
	if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
		t.Fatalf("AsyncCancelFD: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	b := iox.Backoff{}
	for time.Now().Before(deadline) && !terminal {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait after cancel: %v", err)
		}
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			switch cqe.Op() {
			case uring.IORING_OP_ACCEPT:
				if cqe.Res >= 0 {
					accepted++
					closeTestFD(iofd.FD(cqe.Res))
				} else if cqe.Res != -int32(uring.ECANCELED) {
					t.Fatalf("terminal accept CQE failed: %d", cqe.Res)
				}
				if !cqe.HasMore() {
					terminal = true
				}
			case uring.IORING_OP_ASYNC_CANCEL:
				// AsyncCancel completion is expected alongside the terminal accept CQE.
			}
		}
		if !terminal {
			b.Wait()
		}
	}
	return accepted, terminal
}

// TestIntegrationMultishotAcceptPrimeWaitSurfacesImmediateError verifies that
// priming Wait callers inspect returned accept CQEs instead of silently
// consuming an immediate kernel error.
func TestIntegrationMultishotAcceptPrimeWaitSurfacesImmediateError(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	sqeCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, -1)
	err = ring.SubmitAcceptMultishot(sqeCtx, uring.WithIOPrio(uring.IORING_ACCEPT_POLL_FIRST))
	if err != nil {
		t.Logf("SubmitAcceptMultishot: %v (may be unsupported)", err)
		return
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("prime Wait: %v", err)
		}
		accepted, errRes := consumeAcceptCQEs(cqes[:n])
		if accepted != 0 {
			t.Fatalf("prime Wait unexpectedly accepted %d connections", accepted)
		}
		if errRes != 0 {
			return
		}
		b.Wait()
	}

	t.Fatal("prime Wait did not surface immediate accept error CQE")
}

// TestIntegrationMultishotAccept tests multishot accept on real listener.
func TestIntegrationMultishotAccept(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	tcpLn := ln.(*net.TCPListener)
	rawConn, err := tcpLn.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}

	var listenerFD int32
	rawConn.Control(func(fd uintptr) {
		listenerFD = int32(fd)
	})

	sqeCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, listenerFD)
	err = ring.SubmitAcceptMultishot(sqeCtx, uring.WithIOPrio(uring.IORING_ACCEPT_POLL_FIRST))
	if err != nil {
		t.Logf("SubmitAcceptMultishot: %v (may be unsupported)", err)
		return
	}

	// Wait flushes pending submissions. Prime the multishot accept before
	// dialing so the poll-first path is armed in-kernel for future connections.
	cqes := make([]uring.CQEView, 16)
	n, err := ring.Wait(cqes)
	if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("prime Wait: %v", err)
	}
	if accepted, errRes := consumeAcceptCQEs(cqes[:n]); errRes != 0 {
		t.Fatalf("prime Wait accept CQE failed: %d", errRes)
	} else if accepted != 0 {
		t.Fatalf("prime Wait returned %d unexpected accept CQEs before dialing", accepted)
	}

	// Connect multiple clients and keep them alive until the server accepts them.
	const numClients = 3
	clients := make([]net.Conn, 0, numClients)
	defer func() {
		for _, conn := range clients {
			conn.Close()
		}
	}()
	for i := 0; i < numClients; i++ {
		conn, err := net.DialTimeout("tcp", addr.String(), 2*time.Second)
		if err != nil {
			t.Fatalf("DialTimeout[%d]: %v", i, err)
		}
		clients = append(clients, conn)
	}

	// Process completions
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	accepted := 0

	for time.Now().Before(deadline) && accepted < numClients {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		delta, errRes := consumeAcceptCQEs(cqes[:n])
		if errRes != 0 {
			t.Fatalf("accept CQE failed: %d", errRes)
		}
		accepted += delta
		b.Wait()
	}

	delta, terminal := cancelAcceptMultishot(t, ring, sqeCtx, cqes)
	accepted += delta
	if !terminal {
		t.Log("SubmitAcceptMultishot terminal accept CQE not observed after cancel")
	}
	if accepted != numClients {
		if accepted == 0 {
			t.Skip("SubmitAcceptMultishot completion not received - timing-sensitive")
		}
		t.Fatalf("accepted %d connections, want %d", accepted, numClients)
	}
}

// TestIntegrationBundleIterator tests bundle iteration on real data.
func TestIntegrationBundleIterator(t *testing.T) {
	const bufCount = 8
	const bufSize = 256
	backing := make([]byte, bufCount*bufSize)

	// Fill test pattern
	for i := 0; i < bufCount; i++ {
		for j := 0; j < bufSize; j++ {
			backing[i*bufSize+j] = byte(i*10 + j%10)
		}
	}

	// Simulate a CQE that consumed 5 buffers starting at ID 3
	// This tests wrap-around: 3, 4, 5, 6, 7
	cqe := uring.CQEView{
		Res:   5 * int32(bufSize), // 5 full buffers
		Flags: 3 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Verify count
	if got := iter.Count(); got != 5 {
		t.Errorf("Count: got %d, want 5", got)
	}

	// Verify buffer ring IDs (no wrap-around in this case)
	expectedIDs := []uint16{3, 4, 5, 6, 7}
	for i, want := range expectedIDs {
		if got := iter.SlotID(i); got != want {
			t.Errorf("SlotID(%d): got %d, want %d", i, got, want)
		}
	}

	// Verify content using All() iterator
	count := 0
	for buf := range iter.All() {
		expectedFirst := byte((3+count)*10 + 0)
		if buf[0] != expectedFirst {
			t.Errorf("buffer %d[0]: got %d, want %d", count, buf[0], expectedFirst)
		}
		count++
	}

	if count != 5 {
		t.Errorf("iterated %d buffers, want 5", count)
	}
}

// TestIntegrationBundleIteratorWrapAround tests buffer ring wrap-around.
func TestIntegrationBundleIteratorWrapAround(t *testing.T) {
	const bufCount = 4 // Ring mask = 3
	const bufSize = 64
	backing := make([]byte, bufCount*bufSize)

	// Mark each buffer with its ID
	for i := 0; i < bufCount; i++ {
		backing[i*bufSize] = byte(100 + i)
	}

	// Start at buffer 2, consume 4 buffers -> IDs 2, 3, 0, 1 (wraps around)
	cqe := uring.CQEView{
		Res:   4 * int32(bufSize),
		Flags: 2 << uring.IORING_CQE_BUFFER_SHIFT,
	}

	iter, ok := uring.NewBundleIterator(cqe, backing, bufSize, bufCount)
	if !ok {
		t.Fatal("NewBundleIterator returned false")
	}

	// Verify wrap-around buffer ring IDs
	expectedIDs := []uint16{2, 3, 0, 1}
	expectedMarkers := []byte{102, 103, 100, 101}

	for i, buf := range iter.Collect() {
		gotID := iter.SlotID(i)
		if gotID != expectedIDs[i] {
			t.Errorf("SlotID(%d): got %d, want %d", i, gotID, expectedIDs[i])
		}
		if buf[0] != expectedMarkers[i] {
			t.Errorf("buffer %d marker: got %d, want %d", i, buf[0], expectedMarkers[i])
		}
	}
}

// TestIntegrationCQEFlagHelpers tests CQE flag helper methods.
func TestIntegrationCQEFlagHelpers(t *testing.T) {
	// Test HasMore flag
	cqeWithMore := uring.CQEView{
		Res:   100,
		Flags: uring.IORING_CQE_F_MORE,
	}
	if !cqeWithMore.HasMore() {
		t.Error("HasMore should return true when IORING_CQE_F_MORE is set")
	}

	// Test notification flag
	cqeNotif := uring.CQEView{
		Res:   0,
		Flags: uring.IORING_CQE_F_NOTIF,
	}
	if !cqeNotif.IsNotification() {
		t.Error("IsNotification should return true when IORING_CQE_F_NOTIF is set")
	}

	// Test buffer flag
	cqeWithBuf := uring.CQEView{
		Res:   50,
		Flags: uring.IORING_CQE_F_BUFFER | (42 << uring.IORING_CQE_BUFFER_SHIFT),
	}
	if !cqeWithBuf.HasBuffer() {
		t.Error("HasBuffer should return true when IORING_CQE_F_BUFFER is set")
	}
	if id := cqeWithBuf.BufID(); id != 42 {
		t.Errorf("BufID: got %d, want 42", id)
	}
}

// TestIntegrationContextModes checks each context after a real NOP completion.
func TestIntegrationContextModes(t *testing.T) {
	ring := newTestRing(t)
	indirect, extended := ring.IndirectSQE(), ring.ExtSQE()
	if indirect == nil || extended == nil {
		t.Fatal("context pool exhausted")
	}

	for _, tc := range []struct {
		mode uring.SQEContext
		ctx  uring.SQEContext
		fd   iofd.FD
	}{
		{uring.CtxModeDirect, uring.ForFD(0), -1 << 29},
		{uring.CtxModeIndirect, uring.PackIndirect(indirect), 1<<31 - 1},
		{uring.CtxModeExtended, uring.PackExtended(extended), -1 << 31},
	} {
		ctx := tc.ctx.WithFD(tc.fd)
		if err := ring.Nop(ctx); err != nil {
			t.Fatalf("NOP with fd %d: %v", tc.fd, err)
		}
		cqe := waitForCQEs(t, ring, 1)[0]
		if got := cqe.Context(); got != ctx || got.Mode() != tc.mode {
			t.Fatalf("context = %#x (mode %#x), want %#x (mode %#x)", got.Raw(), got.Mode(), ctx.Raw(), tc.mode)
		}
		if cqe.Res != 0 || cqe.Flags != 0 {
			t.Fatalf("NOP with fd %d: res=%d flags=%#x", tc.fd, cqe.Res, cqe.Flags)
		}
		if op, fd := cqe.Op(), cqe.FD(); op != uring.IORING_OP_NOP || fd != tc.fd {
			t.Errorf("op=%d fd=%d, want NOP with fd %d", op, fd, tc.fd)
		}
	}
	ring.PutIndirectSQE(indirect)
	ring.PutExtSQE(extended)
}
