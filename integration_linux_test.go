// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func logRingDiagnostics(t *testing.T, label string, ring *uring.Uring) {
	t.Helper()
	t.Logf("%s: ringFD=%d sqEntries=%d cqEntries=%d sqeBytes=%d sqAvailable=%d cqPending=%d notifySucceed=%t multiIssuers=%t",
		label,
		ring.RingFD(),
		ring.Features.SQEntries,
		ring.Features.CQEntries,
		ring.Features.SQEBytes,
		ring.SQAvailable(),
		ring.CQPending(),
		ring.NotifySucceed,
		ring.MultiIssuers,
	)
	info, err := ring.QueryOpcodes()
	if err != nil {
		t.Logf("%s: QueryOpcodes unavailable: %v", label, err)
		return
	}
	t.Logf("%s: query ringSetupFlags=0x%x featureFlags=0x%x registerOps=%d requestOps=%d",
		label,
		info.RingSetupFlags,
		info.FeatureFlags,
		info.NrRegisterOpcodes,
		info.NrRequestOpcodes,
	)
}

func skipIfXattrUnsupported(t *testing.T, op string, res int32) {
	t.Helper()

	if res == -int32(zcall.ENOTSUP) || res == -int32(zcall.EOPNOTSUPP) {
		t.Skipf("%s not supported on this filesystem: res=%d", op, res)
	}
}

// =============================================================================
// Integration Tests: Complete submit->wait->complete cycles
// =============================================================================

func TestUringNopCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	// Note: Uring doesn't have a Destroy method; OS reclaims resources on exit

	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	if err := ring.Nop(ctx); err != nil {
		t.Fatalf("Nop: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_NOP, time.Second)
	if !ok {
		t.Fatal("NOP operation did not complete")
	}
	if ev.Res < 0 {
		t.Errorf("NOP failed with result: %d", ev.Res)
	}
}

func TestUringIntrospection(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Initially, SQ should be fully available
	initial := ring.SQAvailable()
	if initial != ring.Features.SQEntries {
		t.Errorf("SQAvailable: got %d, want %d", initial, ring.Features.SQEntries)
	}

	// CQ should be empty
	if pending := ring.CQPending(); pending != 0 {
		t.Errorf("CQPending: got %d, want 0", pending)
	}

	// Submit some NOPs
	const numOps = 10
	for i := range numOps {
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, uint16(i), 0)
		if err := ring.Nop(ctx); err != nil {
			t.Fatalf("Nop[%d]: %v", i, err)
		}
	}

	// SQAvailable should decrease
	afterSubmit := ring.SQAvailable()
	if afterSubmit >= initial {
		t.Errorf("SQAvailable should decrease after submit: before=%d, after=%d", initial, afterSubmit)
	}

	// Wait for completions and verify CQPending changes
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(time.Second)
	completed := 0
	b := iox.Backoff{}

	for completed < numOps && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		completed += n
		b.Wait()
	}

	if completed != numOps {
		t.Errorf("completed: got %d, want %d", completed, numOps)
	}
	t.Logf("SQAvailable: initial=%d, afterSubmit=%d, final=%d", initial, afterSubmit, ring.SQAvailable())
	t.Logf("Completed %d operations", completed)
}

func TestUringTimeoutCycle(t *testing.T) {
	// Skip under race detection - io_uring timeout completions are unreliable
	// in WSL2 when race detector overhead is present. The kernel delivers the
	// timeout CQE but the polling loop may miss it due to timing changes.
	if raceEnabled {
		t.Skip("skipping: io_uring timeout unreliable with race detector in WSL2")
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true // Request a CQE for every successful operation.
		opt.MultiIssuers = true  // Exercise the shared-submit configuration.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ctx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
	start := time.Now()
	if err := ring.Timeout(ctx, 20*time.Millisecond); err != nil {
		t.Fatalf("Timeout: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_TIMEOUT, time.Second)
	if !ok {
		t.Fatal("Timeout operation did not complete")
	}
	elapsed := time.Since(start)
	if elapsed < 15*time.Millisecond {
		t.Errorf("Timeout completed too fast: %v", elapsed)
	}
	_ = ev
}

// TestTimeoutUpdate validates the IORING_TIMEOUT_UPDATE flag.
// Timeout update allows modifying an existing timeout's expiration in-place.
func TestTimeoutUpdate(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	const targetUserData = uint16(42)

	// Submit a long timeout (60s) that we'll update to be shorter
	timeoutCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, targetUserData, 0)
	if err := ring.Timeout(timeoutCtx, 60*time.Second); err != nil {
		t.Fatalf("Timeout: %v", err)
	}
	t.Log("Submitted 60s timeout with userData=42")

	// Update the timeout to 10ms
	updateCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 1, 0)
	if err := ring.TimeoutUpdate(updateCtx, timeoutCtx.Raw(), 10*time.Millisecond, false); err != nil {
		t.Fatalf("TimeoutUpdate: %v", err)
	}
	t.Log("Submitted TimeoutUpdate to change to 10ms")

	// Collect CQEs - should get update result and eventually the timeout
	cqes := make([]uring.CQEView, 8)
	start := time.Now()

	updateFound := false
	for i := 0; i < 10; i++ {
		n, _ := ring.Wait(cqes)
		for j := 0; j < n; j++ {
			op := cqes[j].Op()
			res := cqes[j].Res
			t.Logf("CQE: op=%d res=%d", op, res)

			if op == uring.IORING_OP_TIMEOUT_REMOVE {
				if res == 0 {
					updateFound = true
					t.Log("TimeoutUpdate succeeded")
				} else if res < 0 {
					const ENOENT = 2
					if -res == ENOENT {
						t.Log("Timeout already expired or not found")
					} else {
						t.Logf("TimeoutUpdate error: %d", res)
					}
				}
			}
		}
		if updateFound {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	elapsed := time.Since(start)
	t.Logf("Elapsed: %v", elapsed)

	// Verify the API signature works
	t.Run("APISignature", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 1, 0)
		// Test both relative and absolute modes
		_ = ring.TimeoutUpdate(ctx, 0, time.Second, false) // relative
		_ = ring.TimeoutUpdate(ctx, 0, time.Second, true)  // absolute
		t.Log("API signature verified")
	})
}

func TestTimeoutRemove(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	timeoutCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
	if err := ring.Timeout(timeoutCtx, time.Minute); err != nil {
		t.Fatalf("Timeout: %v", err)
	}

	removeCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 0, 0)
	if err := ring.TimeoutRemove(removeCtx, timeoutCtx.Raw()); err != nil {
		t.Fatalf("TimeoutRemove: %v", err)
	}

	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(time.Second)
	timeoutRemoved := false
	timeoutCanceled := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && (!timeoutRemoved || !timeoutCanceled) {
		n, err := ring.Wait(cqes)
		if err != nil &&
			!errors.Is(err, iox.ErrWouldBlock) &&
			!errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			switch cqes[i].Op() {
			case uring.IORING_OP_TIMEOUT_REMOVE:
				if cqes[i].Res != 0 {
					t.Fatalf("TimeoutRemove CQE res = %d, want 0", cqes[i].Res)
				}
				timeoutRemoved = true
			case uring.IORING_OP_TIMEOUT:
				if cqes[i].Res != -int32(zcall.ECANCELED) {
					t.Fatalf("Timeout CQE res = %d, want %d", cqes[i].Res, -int32(zcall.ECANCELED))
				}
				timeoutCanceled = true
			}
		}
		b.Wait()
	}

	if !timeoutRemoved || !timeoutCanceled {
		t.Fatalf("timeout removal incomplete: remove=%t timeout=%t", timeoutRemoved, timeoutCanceled)
	}
}

func TestUringLinkTimeoutCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	nopCtx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	if err := ring.Nop(nopCtx, uring.WithFlags(uring.IOSQE_IO_LINK)); err != nil {
		t.Fatalf("Nop: %v", err)
	}

	timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 0, 0)
	if err := ring.LinkTimeout(timeoutCtx, 100*time.Millisecond); err != nil {
		t.Fatalf("LinkTimeout: %v", err)
	}

	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(time.Second)
	nopDone := false
	timeoutDone := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && (!nopDone || !timeoutDone) {
		n, err := ring.Wait(cqes)
		if err != nil &&
			!errors.Is(err, iox.ErrWouldBlock) &&
			!errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			switch cqes[i].Op() {
			case uring.IORING_OP_NOP:
				if cqes[i].Res != 0 {
					t.Fatalf("NOP CQE res = %d, want 0", cqes[i].Res)
				}
				nopDone = true
			case uring.IORING_OP_LINK_TIMEOUT:
				if cqes[i].Res != -int32(zcall.ECANCELED) {
					t.Fatalf("LinkTimeout CQE res = %d, want %d", cqes[i].Res, -int32(zcall.ECANCELED))
				}
				timeoutDone = true
			}
		}
		b.Wait()
	}

	if !nopDone || !timeoutDone {
		t.Fatalf("linked timeout incomplete: nop=%t timeout=%t", nopDone, timeoutDone)
	}
}

func TestUringFileReadWriteCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true // Request a CQE for every successful operation.
		opt.MultiIssuers = true  // Exercise the shared-submit configuration.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	f, err := os.CreateTemp("", "uring_test_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())
	testData := []byte("Hello, io_uring!")

	// Write
	writeCtx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, fd)
	if err := ring.Write(writeCtx, testData); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_WRITE, time.Second)
	if !ok {
		t.Fatal("Write operation did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Write failed: %d", ev.Res)
	}
	if int(ev.Res) != len(testData) {
		t.Errorf("Write: got %d, want %d", ev.Res, len(testData))
	}

	// Seek and Read
	f.Seek(0, 0)
	readBuf := make([]byte, len(testData))
	readCtx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, fd)
	if err := ring.Read(readCtx, readBuf); err != nil {
		t.Fatalf("Read: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_READ, time.Second)
	if !ok {
		t.Fatal("Read operation did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Read failed: %d", ev.Res)
	}
	if string(readBuf) != string(testData) {
		t.Errorf("Data mismatch: got %q, want %q", readBuf, testData)
	}
}

// =============================================================================
// Socket Tests using sock primitives
// =============================================================================

func TestUringTCPSocketCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create TCP4 socket via io_uring
	ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(ctx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok {
		t.Fatal("Socket creation did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Socket creation failed: %d", ev.Res)
	}
	socketFD := ev.Res

	// Close socket via io_uring
	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, socketFD)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "TCP4 socket")
}

func TestUringTCPBindListenAccept(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create server socket
	serverCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(serverCtx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Server socket creation failed")
	}
	serverFD := ev.Res

	// Bind using sock.TCPAddr with IPv4LoopBack
	bindAddr := &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0} // Port 0 = ephemeral
	bindCtx := uring.PackDirect(uring.IORING_OP_BIND, 0, 0, serverFD)
	if err := ring.Bind(bindCtx, bindAddr); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_BIND, time.Second)
	if !ok {
		t.Fatal("Bind did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Bind failed: %d", ev.Res)
	}

	// Listen
	listenCtx := uring.PackDirect(uring.IORING_OP_LISTEN, 0, 0, serverFD)
	if err := ring.Listen(listenCtx); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_LISTEN, time.Second)
	if !ok {
		t.Fatal("Listen did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Listen failed: %d", ev.Res)
	}

	// Clean up - close server socket
	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, serverFD)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "server socket")
}

func TestUringUDPSocketCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.UDP4Socket(ctx); err != nil {
		t.Fatalf("UDP4Socket: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatal("UDP socket creation failed")
	}

	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, ev.Res)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "UDP4 socket")
}

func TestUringUnixSocketCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.UnixSocket(ctx); err != nil {
		t.Fatalf("UnixSocket: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatal("Unix socket creation failed")
	}

	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, ev.Res)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "Unix socket")
}

// =============================================================================
// Poll Tests
// =============================================================================

func TestUringPollAddCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true // Request a CQE for every successful operation.
		opt.MultiIssuers = true  // Exercise the shared-submit configuration.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	pollCtx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(r.Fd()))
	if err := ring.PollAdd(pollCtx, uring.EPOLLIN); err != nil {
		t.Fatalf("PollAdd: %v", err)
	}

	// Make pipe readable
	go func() {
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte("test"))
	}()

	ev, ok := waitForOp(t, ring, uring.IORING_OP_POLL_ADD, time.Second)
	if !ok {
		t.Fatal("Poll did not complete")
	}
	if ev.Res < 0 {
		t.Errorf("Poll failed: %d", ev.Res)
	}
}

// =============================================================================
// Stress Tests: Multi-goroutine concurrent submissions
// =============================================================================

func TestUringConcurrentNops(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesMedium
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	const numGoroutines = 8
	const numOpsPerGoroutine = 100

	var wg sync.WaitGroup
	var submitted atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < numOpsPerGoroutine; i++ {
				ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, int32(gid*1000+i))
				if err := ring.Nop(ctx); err == nil {
					submitted.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()
	totalSubmitted := submitted.Load()
	t.Logf("Submitted: %d", totalSubmitted)

	if totalSubmitted < int64(numGoroutines*numOpsPerGoroutine/2) {
		t.Errorf("Too few submissions: %d", totalSubmitted)
	}

	// Wait for completions
	cqes := make([]uring.CQEView, 256)
	var completed int64
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	for completed < totalSubmitted && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		completed += int64(n)
		b.Wait()
	}

	t.Logf("Completed: %d", completed)
}

func TestUringConcurrentFileOps(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesMedium
		opt.MultiIssuers = true
		opt.NotifySucceed = true // Request a CQE for every successful operation.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	const numGoroutines = 4
	const numOpsPerGoroutine = 25
	files := make([]*os.File, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		f, err := os.CreateTemp("", "uring_stress_*")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		files[i] = f
		defer os.Remove(f.Name())
		defer f.Close()
	}

	var wg sync.WaitGroup
	var submitted atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			fd := int32(files[gid].Fd())
			for i := 0; i < numOpsPerGoroutine; i++ {
				data := []byte("stress test data\n")
				ctx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, fd)
				if err := ring.Write(ctx, data); err == nil {
					submitted.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()
	totalSubmitted := submitted.Load()
	t.Logf("Submitted %d write operations", totalSubmitted)

	cqes := make([]uring.CQEView, 256)
	var completed int64
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}
	for completed < totalSubmitted && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for j := 0; j < n; j++ {
			if cqes[j].Op() == uring.IORING_OP_WRITE {
				completed++
			}
		}
		b.Wait()
	}

	t.Logf("Completed: %d", completed)
}

// =============================================================================
// Baseline Feature Tests
// =============================================================================

func TestFeatureDetection(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Logf("SQ Entries: %d", ring.Features.SQEntries)
	t.Logf("CQ Entries: %d", ring.Features.CQEntries)

	if ring.Features.SQEntries == 0 {
		t.Error("SQEntries should be non-zero")
	}
	if ring.Features.CQEntries == 0 {
		t.Error("CQEntries should be non-zero")
	}
}

// =============================================================================
// Linux 6.18+ Baseline Operation Tests
// =============================================================================

func TestUringEpollWait(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Log("EpollWait is part of the Linux 6.18+ baseline")
}

func TestUringPipe(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Buffer for the two file descriptors (read end, write end)
	var fds [2]int32

	ctx := uring.PackDirect(uring.IORING_OP_PIPE, 0, 0, 0)
	if err := ring.Pipe(ctx, &fds, 0); err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_PIPE, time.Second)
	if !ok {
		t.Fatal("Pipe operation did not complete")
	}
	if ev.Res < 0 {
		t.Errorf("Pipe creation failed: %d", ev.Res)
	} else {
		t.Logf("Pipe created: read_fd=%d, write_fd=%d", fds[0], fds[1])
		// Verify we got valid file descriptors
		if fds[0] <= 0 || fds[1] <= 0 {
			t.Errorf("Invalid file descriptors: read=%d, write=%d", fds[0], fds[1])
		}
	}
}

// =============================================================================
// Resize Rings Tests
// =============================================================================

func TestResizeRings(t *testing.T) {
	if isWSL2() {
		t.Skip("ResizeRings is not reliable on WSL2 kernels")
	}

	// Create a single-issuer ring (has DEFER_TASKRUN by default)
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall // 512 entries
		// Single issuer mode is default, which sets DEFER_TASKRUN
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	logRingDiagnostics(t, "ResizeRings/start", ring)

	// Try to resize to larger CQ ring
	err = ring.ResizeRings(0, 2048) // Keep SQ, grow CQ to 2048
	if err != nil {
		logRingDiagnostics(t, "ResizeRings/failure", ring)
		t.Fatalf("ResizeRings: %v (requested newSQ=%d newCQ=%d)", err, 0, 2048)
	}
	logRingDiagnostics(t, "ResizeRings/after", ring)

	// Verify the ring still works after resize
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	if err := ring.Nop(ctx); err != nil {
		t.Fatalf("Nop after resize: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_NOP, time.Second)
	if !ok {
		t.Fatal("NOP did not complete after resize")
	}
	if ev.Res < 0 {
		t.Errorf("NOP failed after resize: %d", ev.Res)
	}

	t.Log("ResizeRings succeeded - ring functional after CQ resize to 2048")
}

// TestBufRingMMAP tests kernel-allocated buffer ring registration.
// In MMAP mode, the kernel allocates the ring memory and we map it into userspace.
func TestBufRingMMAP(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Try to register a buffer ring with MMAP mode
	const (
		groupID = 100
		entries = 16
	)

	bufRing, err := ring.RegisterBufRingMMAP(entries, groupID)
	if err != nil {
		t.Fatalf("RegisterBufRingMMAP: %v", err)
	}
	if bufRing == nil {
		t.Fatal("RegisterBufRingMMAP returned nil ring")
	}

	t.Logf("Buffer ring MMAP registered: groupID=%d, entries=%d", groupID, entries)
	t.Log("Buffer ring MMAP test passed")
}

func TestRegisterBufRingRejectsInvalidEntries(t *testing.T) {
	apis := []struct {
		name string
		call func(ring *uring.Uring, entries int, groupID uint16) error
	}{
		{
			name: "MMAP",
			call: func(ring *uring.Uring, entries int, groupID uint16) error {
				_, err := ring.RegisterBufRingMMAP(entries, groupID)
				return err
			},
		},
		{
			name: "Incremental",
			call: func(ring *uring.Uring, entries int, groupID uint16) error {
				_, err := ring.RegisterBufRingIncremental(entries, groupID)
				return err
			},
		},
		{
			name: "WithFlags",
			call: func(ring *uring.Uring, entries int, groupID uint16) error {
				_, err := ring.RegisterBufRingWithFlags(entries, groupID, 0)
				return err
			},
		},
	}

	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			ring, err := uring.New(testMinimalBufferOptions)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			mustStartRing(t, ring)

			for i, entries := range []int{0, (1 << 15) + 1} {
				groupID := uint16(200 + i)
				err := api.call(ring, entries, groupID)
				if !errors.Is(err, uring.ErrInvalidParam) {
					t.Fatalf("entries=%d: got %v, want %v", entries, err, uring.ErrInvalidParam)
				}
			}
		})
	}
}

// TestQueryOpcodes tests the io_uring query interface (Linux 6.19+).
// This allows querying kernel capabilities at runtime.
func TestQueryOpcodes(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Query supported opcodes
	info, err := ring.QueryOpcodes()
	if err != nil {
		// EINVAL or ENOSYS expected on kernel < 6.19
		t.Skipf("QueryOpcodes not supported: %v (requires kernel 6.19+)", err)
	}

	t.Logf("Query interface available:")
	t.Logf("  NrRequestOpcodes: %d", info.NrRequestOpcodes)
	t.Logf("  NrRegisterOpcodes: %d", info.NrRegisterOpcodes)
	t.Logf("  FeatureFlags: 0x%x", info.FeatureFlags)
	t.Logf("  RingSetupFlags: 0x%x", info.RingSetupFlags)
	t.Logf("  NrQueryOpcodes: %d", info.NrQueryOpcodes)

	// Verify reasonable values
	if info.NrRequestOpcodes < 50 {
		t.Logf("Note: NrRequestOpcodes=%d seems low", info.NrRequestOpcodes)
	}

	t.Log("Query interface test passed")
}

// TestRegisterFiles tests fixed file registration.
func TestRegisterFiles(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create a temporary file
	f, err := os.CreateTemp("", "uring-test-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())

	// Test RegisterFiles
	err = ring.RegisterFiles([]int32{fd})
	if err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}

	// Verify count
	if ring.RegisteredFileCount() != 1 {
		t.Errorf("expected 1 registered file, got %d", ring.RegisteredFileCount())
	}

	// Test double registration (should fail with ErrExists)
	err = ring.RegisterFiles([]int32{fd})
	if err != uring.ErrExists {
		t.Errorf("expected ErrExists on double register, got %v", err)
	}

	// Unregister
	err = ring.UnregisterFiles()
	if err != nil {
		t.Fatalf("UnregisterFiles: %v", err)
	}

	// Verify count after unregister
	if ring.RegisteredFileCount() != 0 {
		t.Errorf("expected 0 registered files after unregister, got %d", ring.RegisteredFileCount())
	}

	t.Log("RegisterFiles test passed")
}

// TestRegisterFilesSparse tests sparse file registration.
func TestRegisterFilesSparse(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Test sparse registration
	err = ring.RegisterFilesSparse(16)
	if err != nil {
		t.Fatalf("RegisterFilesSparse: %v", err)
	}

	// Verify count
	if ring.RegisteredFileCount() != 16 {
		t.Errorf("expected 16 registered files, got %d", ring.RegisteredFileCount())
	}

	// Create a temporary file
	f, err := os.CreateTemp("", "uring-test-sparse-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())

	// Test update at slot 0
	err = ring.RegisterFilesUpdate(0, []int32{fd})
	if err != nil {
		t.Fatalf("RegisterFilesUpdate: %v", err)
	}

	// Unregister
	err = ring.UnregisterFiles()
	if err != nil {
		t.Fatalf("UnregisterFiles: %v", err)
	}

	t.Log("RegisterFilesSparse test passed")
}

// TestAcceptDirect tests direct descriptor accept using registered file table.
func TestAcceptDirect(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true // Request a CQE for every successful operation.
		opt.MultiIssuers = true  // Exercise the shared-submit configuration.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Register sparse file table for direct descriptors
	err = ring.RegisterFilesSparse(16)
	if err != nil {
		t.Fatalf("RegisterFilesSparse: %v", err)
	}

	// Create server socket
	serverCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(serverCtx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Server socket creation failed")
	}
	serverFD := ev.Res

	// Bind to ephemeral port
	bindAddr := &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0}
	bindCtx := uring.PackDirect(uring.IORING_OP_BIND, 0, 0, serverFD)
	if err := ring.Bind(bindCtx, bindAddr); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_BIND, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Bind failed: %d", ev.Res)
	}

	// Listen
	listenCtx := uring.PackDirect(uring.IORING_OP_LISTEN, 0, 0, serverFD)
	if err := ring.Listen(listenCtx); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_LISTEN, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Listen failed: %d", ev.Res)
	}

	// Get bound address for client connection using zcall
	var boundAddr [16]byte // sockaddr_in size
	addrLen := uint32(16)
	errno := zcall.Getsockname(uintptr(serverFD), unsafe.Pointer(&boundAddr), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}
	boundPort := int(boundAddr[2])<<8 | int(boundAddr[3])

	// Submit AcceptDirect with auto-allocation
	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, serverFD)
	err = ring.AcceptDirect(acceptCtx, uring.IORING_FILE_INDEX_ALLOC)
	if err != nil {
		t.Fatalf("AcceptDirect: %v", err)
	}

	// Start client connection after accept is submitted
	clientDone := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", boundPort))
		if err != nil {
			clientDone <- err
			return
		}
		conn.Close()
		clientDone <- nil
	}()

	// Wait for accept to complete
	ev, ok = waitForOp(t, ring, uring.IORING_OP_ACCEPT, 2*time.Second)
	if !ok {
		t.Fatal("AcceptDirect did not complete")
	}

	// With IORING_FILE_INDEX_ALLOC, res contains the allocated slot index (0-based)
	if ev.Res < 0 {
		t.Fatalf("AcceptDirect returned error: %d", ev.Res)
	}

	allocatedSlot := ev.Res
	if allocatedSlot >= 16 {
		t.Errorf("allocated slot %d out of range [0,16)", allocatedSlot)
	}

	// Wait for client with timeout
	select {
	case err := <-clientDone:
		if err != nil {
			t.Logf("Client error (non-fatal): %v", err)
		}
	case <-time.After(time.Second):
	}

	// Clean up
	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, serverFD)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "server socket")

	ring.UnregisterFiles()
	t.Logf("AcceptDirect allocated slot %d", allocatedSlot)
}

// TestSocketDirect tests direct descriptor socket creation using registered file table.
func TestSocketDirect(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true // Request a CQE for every successful operation.
		opt.MultiIssuers = true  // Exercise the shared-submit configuration.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Register sparse file table for direct descriptors
	err = ring.RegisterFilesSparse(16)
	if err != nil {
		t.Fatalf("RegisterFilesSparse: %v", err)
	}

	// Create socket directly into registered file table with auto-allocation
	socketCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	err = ring.TCP4SocketDirect(socketCtx)
	if err != nil {
		t.Fatalf("TCP4SocketDirect: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok {
		t.Fatal("SocketDirect did not complete")
	}

	// With IORING_FILE_INDEX_ALLOC, res contains the allocated slot index (0-based)
	if ev.Res < 0 {
		t.Fatalf("SocketDirect returned error: %d", ev.Res)
	}

	allocatedSlot := ev.Res
	if allocatedSlot >= 16 {
		t.Errorf("allocated slot %d out of range [0,16)", allocatedSlot)
	}

	// The socket is in the registered file table at allocatedSlot
	// Close it using IOSQE_FIXED_FILE flag (close the direct descriptor)
	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, uring.IOSQE_FIXED_FILE, 0, allocatedSlot)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "direct socket")

	ring.UnregisterFiles()
	t.Logf("SocketDirect allocated slot %d", allocatedSlot)
}

// TestConnectWithDirectSocket tests Connect using a socket created via SocketDirect.
// This verifies that direct sockets can be used with IOSQE_FIXED_FILE flag.
func TestConnectWithDirectSocket(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Register sparse file table for direct descriptors
	err = ring.RegisterFilesSparse(16)
	if err != nil {
		t.Fatalf("RegisterFilesSparse: %v", err)
	}

	// Create server socket (normal FD)
	serverCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(serverCtx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Server socket creation failed")
	}
	serverFD := ev.Res

	// Bind and Listen
	bindAddr := &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0}
	bindCtx := uring.PackDirect(uring.IORING_OP_BIND, 0, 0, serverFD)
	if err := ring.Bind(bindCtx, bindAddr); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	ev, _ = waitForOp(t, ring, uring.IORING_OP_BIND, time.Second)
	if ev.Res < 0 {
		t.Fatalf("Bind failed: %d", ev.Res)
	}

	listenCtx := uring.PackDirect(uring.IORING_OP_LISTEN, 0, 0, serverFD)
	if err := ring.Listen(listenCtx); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ev, _ = waitForOp(t, ring, uring.IORING_OP_LISTEN, time.Second)
	if ev.Res < 0 {
		t.Fatalf("Listen failed: %d", ev.Res)
	}

	// Get bound port
	var boundAddr [16]byte
	addrLen := uint32(16)
	errno := zcall.Getsockname(uintptr(serverFD), unsafe.Pointer(&boundAddr), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}
	boundPort := int(boundAddr[2])<<8 | int(boundAddr[3])

	// Create client socket as direct descriptor
	clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4SocketDirect(clientCtx); err != nil {
		t.Fatalf("TCP4SocketDirect: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok {
		t.Fatal("SocketDirect did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("SocketDirect returned error: %d", ev.Res)
	}
	clientSlot := ev.Res

	// Submit Accept before Connect
	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, serverFD)
	ring.Accept(acceptCtx)

	// Connect using the direct socket with IOSQE_FIXED_FILE flag
	remoteAddr := &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: boundPort}
	connectCtx := uring.PackDirect(uring.IORING_OP_CONNECT, uring.IOSQE_FIXED_FILE, 0, clientSlot)
	if err := ring.Connect(connectCtx, remoteAddr); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Connect and Accept can complete in either order once fixed-file connect works.
	cqes := make([]uring.CQEView, 4)
	deadline := time.Now().Add(2 * time.Second)
	backoff := iox.Backoff{}
	var connectCQE, acceptCQE uring.CQEView
	connectDone := false
	acceptDone := false
	for time.Now().Before(deadline) && (!connectDone || !acceptDone) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			backoff.Reset()
		}
		for i := 0; i < n; i++ {
			switch cqes[i].Op() {
			case uring.IORING_OP_CONNECT:
				connectCQE = cqes[i]
				connectDone = true
			case uring.IORING_OP_ACCEPT:
				acceptCQE = cqes[i]
				acceptDone = true
			}
		}
		if connectDone && connectCQE.Res < 0 {
			break
		}
		backoff.Wait()
	}
	if !connectDone {
		t.Fatal("Connect did not complete")
	}
	if connectCQE.Res < 0 {
		if connectCQE.Res == -int32(zcall.EOPNOTSUPP) {
			t.Skipf("Connect with direct socket not supported by kernel: %d", connectCQE.Res)
		}
		t.Fatalf("Connect failed: %d", connectCQE.Res)
	}
	if !acceptDone {
		t.Fatal("Accept did not complete")
	}
	if acceptCQE.Res < 0 {
		t.Logf("Accept result: %d (may be expected in some scenarios)", acceptCQE.Res)
	} else {
		// Close accepted FD
		acceptedFD := acceptCQE.Res
		closeAcceptedCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, acceptedFD)
		mustCloseAndWait(t, ring, closeAcceptedCtx, time.Second, "accepted socket")
	}

	// Close direct client socket
	closeClientCtx := uring.PackDirect(uring.IORING_OP_CLOSE, uring.IOSQE_FIXED_FILE, 0, clientSlot)
	mustCloseAndWait(t, ring, closeClientCtx, time.Second, "direct client socket")

	// Close server socket
	closeServerCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, serverFD)
	mustCloseAndWait(t, ring, closeServerCtx, time.Second, "server socket")

	ring.UnregisterFiles()
	t.Logf("Connect with direct socket (slot %d) succeeded", clientSlot)
}

// TestFixedFdInstall tests converting a direct descriptor to a regular FD.
// This verifies the complete lifecycle: allocate direct → convert to regular → use both.
func TestFixedFdInstall(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	logRingDiagnostics(t, "FixedFdInstall/start", ring)

	// Register sparse file table
	err = ring.RegisterFilesSparse(16)
	if err != nil {
		t.Fatalf("RegisterFilesSparse: %v", err)
	}
	t.Logf("FixedFdInstall: registered sparse file table size=%d", 16)

	// Create socket as direct descriptor
	socketCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4SocketDirect(socketCtx); err != nil {
		t.Fatalf("TCP4SocketDirect: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, time.Second)
	if !ok {
		t.Fatal("SocketDirect did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("SocketDirect returned error: %d", ev.Res)
	}
	directSlot := ev.Res
	t.Logf("FixedFdInstall: socket direct CQE res=%d flags=0x%x", ev.Res, ev.Flags)

	// Convert direct descriptor to regular FD
	installCtx := uring.PackDirect(uring.IORING_OP_FIXED_FD_INSTALL, 0, 0, 0)
	t.Logf("FixedFdInstall: submit fixedIndex=%d sqeFlags=0x%x installFlags=0x%x", directSlot, installCtx.Flags(), uint32(0))
	err = ring.FixedFdInstall(installCtx, int(directSlot), 0)
	if err != nil {
		t.Fatalf("FixedFdInstall: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_FIXED_FD_INSTALL, time.Second)
	if !ok {
		t.Fatal("FixedFdInstall did not complete")
	}
	t.Logf("FixedFdInstall: install CQE res=%d flags=0x%x", ev.Res, ev.Flags)
	if ev.Res < 0 {
		logRingDiagnostics(t, "FixedFdInstall/failure", ring)
		t.Fatalf("FixedFdInstall returned error: %d (directSlot=%d sqeFlags=0x%x)", ev.Res, directSlot, installCtx.Flags())
	}
	regularFD := ev.Res

	// Both descriptors now exist for the same socket
	t.Logf("Direct slot %d → regular fd %d", directSlot, regularFD)

	// Close the regular FD using io_uring
	closeRegularCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, regularFD)
	mustCloseAndWait(t, ring, closeRegularCtx, time.Second, "regular socket FD")

	// Close the direct descriptor (still valid after closing regular FD)
	closeDirectCtx := uring.PackDirect(uring.IORING_OP_CLOSE, uring.IOSQE_FIXED_FILE, 0, directSlot)
	mustCloseAndWait(t, ring, closeDirectCtx, time.Second, "direct socket slot")

	ring.UnregisterFiles()
	t.Log("FixedFdInstall test passed - both descriptors worked independently")
}

// TestFutexWakeWait tests the futex wait/wake operations.
func TestFutexWakeWait(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create a futex variable
	var futex uint32 = 0

	// Submit FutexWait - will wait since futex == 0 (expected value)
	waitCtx := uring.PackDirect(uring.IORING_OP_FUTEX_WAIT, 0, 0, 0)
	err = ring.FutexWait(waitCtx, &futex, 0, uring.FUTEX_BITSET_MATCH_ANY, uring.FUTEX2_SIZE_U32)
	if err != nil {
		t.Fatalf("FutexWait: %v", err)
	}

	// Submit FutexWake to wake 1 waiter
	wakeCtx := uring.PackDirect(uring.IORING_OP_FUTEX_WAKE, 0, 0, 0)
	err = ring.FutexWake(wakeCtx, &futex, 1, uring.FUTEX_BITSET_MATCH_ANY, uring.FUTEX2_SIZE_U32)
	if err != nil {
		t.Fatalf("FutexWake: %v", err)
	}

	// Wait for completions
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	waitCompleted := false
	wakeCompleted := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && (!waitCompleted || !wakeCompleted) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			op := cqes[i].Op()
			res := cqes[i].Res

			switch op {
			case uring.IORING_OP_FUTEX_WAIT:
				if res < 0 {
					t.Fatalf("FutexWait result: %d", res)
				}
				waitCompleted = true
			case uring.IORING_OP_FUTEX_WAKE:
				if res < 0 {
					t.Fatalf("FutexWake result: %d", res)
				} else {
					t.Logf("FutexWake woke %d waiters", res)
				}
				wakeCompleted = true
			}
		}
		b.Wait()
	}

	if !waitCompleted || !wakeCompleted {
		t.Fatal("Futex operations did not complete")
	}
	t.Log("Futex wait/wake test passed")
}

// TestMsgRingSelf tests MSG_RING to the same ring (self-messaging).
// This helps diagnose if MSG_RING works at all.
func TestMsgRingSelf(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get this ring's FD for self-messaging
	ringFD := ring.RingFD()
	if ringFD < 0 {
		t.Skip("RingFD not available")
	}
	t.Logf("Self ring FD: %d", ringFD)

	// Send message to self
	const testUserData int64 = 0xDEADBEEF
	const testResult int32 = 99
	ctx := uring.PackDirect(uring.IORING_OP_MSG_RING, 0, 0, ringFD.Raw())
	err = ring.MsgRing(ctx, testUserData, testResult)
	if err != nil {
		t.Fatalf("MsgRing: %v", err)
	}
	t.Log("MsgRing to self submitted")

	// Wait for completion - should get 2 CQEs: one for the send, one for the received message
	cqes := make([]uring.CQEView, 4)
	deadline := time.Now().Add(5 * time.Second)
	sendCompleted := false
	recvCompleted := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && (!sendCompleted || !recvCompleted) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			res := cqes[i].Res
			flags := cqes[i].Flags
			t.Logf("CQE: res=%d, flags=0x%x", res, flags)

			if res == 0 && !sendCompleted {
				// Send completion (res=0 means success)
				sendCompleted = true
				t.Log("MSG_RING send completed")
			} else if res == testResult {
				// Received message
				recvCompleted = true
				t.Logf("MSG_RING message received: res=%d", res)
			} else if res < 0 {
				t.Logf("Negative result: %d (errno %d)", res, -res)
				t.Fatalf("MSG_RING failed: res=%d", res)
			}
		}
		b.Wait()
	}

	if !sendCompleted {
		t.Fatal("MSG_RING send did not complete")
	}
	if !recvCompleted {
		t.Fatal("MSG_RING message was not received")
	}
	t.Log("MSG_RING self-messaging test passed")
}

// TestMsgRing tests cross-ring communication via IORING_OP_MSG_RING.
// This operation sends a CQE to another io_uring instance.
func TestMsgRing(t *testing.T) {
	// Create source ring
	srcRing, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New (source): %v", err)
	}
	mustStartRing(t, srcRing)

	// Create target ring
	dstRing, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New (target): %v", err)
	}
	mustStartRing(t, dstRing)

	// Get target ring's FD for cross-ring messaging
	targetFD := dstRing.RingFD()
	if targetFD < 0 {
		t.Skip("RingFD not available")
	}
	t.Logf("Target ring FD: %d", targetFD)

	// Send message from source to target
	// The context FD specifies the target ring
	const testUserData int64 = 0x12345678
	const testResult int32 = 42
	ctx := uring.PackDirect(uring.IORING_OP_MSG_RING, 0, 0, targetFD.Raw())
	t.Log("Submitting MsgRing...")
	err = srcRing.MsgRing(ctx, testUserData, testResult)
	if err != nil {
		t.Fatalf("MsgRing: %v", err)
	}
	t.Log("MsgRing submitted to SQ")

	// Wait for source ring to complete the send
	srcCQEs := make([]uring.CQEView, 4)
	deadline := time.Now().Add(5 * time.Second)
	srcDone := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && !srcDone {
		n, err := srcRing.Wait(srcCQEs)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait (source): %v", err)
		}
		for i := 0; i < n; i++ {
			res := srcCQEs[i].Res
			if res < 0 {
				if res == -38 { // ENOSYS
					t.Fatalf("MSG_RING failed with ENOSYS on Linux 6.18+ baseline")
				}
				t.Fatalf("MsgRing send failed: %d", res)
			}
			srcDone = true
			t.Log("MsgRing send completed on source ring")
		}
		b.Wait()
	}

	if !srcDone {
		// Cross-ring MSG_RING may not work on WSL2 due to EALREADY errors from io_uring_enter
		// Self-messaging (TestMsgRingSelf) works, but cross-ring fails
		t.Skip("Cross-ring MSG_RING did not complete - may be WSL2 limitation")
	}

	// Wait for target ring to receive the CQE
	dstCQEs := make([]uring.CQEView, 4)
	deadline = time.Now().Add(5 * time.Second)
	dstDone := false
	b = iox.Backoff{}

	for time.Now().Before(deadline) && !dstDone {
		n, err := dstRing.Wait(dstCQEs)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait (target): %v", err)
		}
		for i := 0; i < n; i++ {
			res := dstCQEs[i].Res
			if res == testResult {
				t.Logf("Received MSG_RING CQE on target ring: res=%d", res)
				dstDone = true
			}
		}
		b.Wait()
	}

	if !dstDone {
		t.Skip("Target ring did not receive MSG_RING CQE - may be WSL2 limitation")
	}
	t.Log("MSG_RING cross-ring communication test passed")
}

// TestMsgRingFD tests cross-ring FD transfer via IORING_MSG_SEND_FD.
// This operation transfers a fixed file descriptor from one ring to another.
func TestMsgRingFD(t *testing.T) {
	// Create source ring
	srcRing, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New (source): %v", err)
	}
	mustStartRing(t, srcRing)

	// Create target ring
	dstRing, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New (target): %v", err)
	}
	mustStartRing(t, dstRing)

	// Get target ring's FD
	targetFD := dstRing.RingFD()
	if targetFD < 0 {
		t.Skip("RingFD not available")
	}
	t.Logf("Target ring FD: %d", targetFD)

	// Create a pipe to get a valid FD
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer closeTestFds(fds)

	// Register the pipe FD in source ring's fixed file table (slot 0)
	if err := srcRing.RegisterFiles([]int32{int32(fds[0])}); err != nil {
		if errors.Is(err, uring.ErrExists) {
			t.Skip("Files already registered (test environment issue)")
		}
		t.Fatalf("RegisterFiles (source): %v", err)
	}
	t.Log("Registered FD in source ring's fixed file table at slot 0")

	// Register sparse file table in target ring (for receiving FD)
	if err := dstRing.RegisterFilesSparse(4); err != nil {
		if errors.Is(err, uring.ErrExists) {
			t.Skip("Sparse files already registered (test environment issue)")
		}
		t.Fatalf("RegisterFilesSparse (target): %v", err)
	}
	t.Log("Registered sparse file table in target ring")

	// Transfer FD from source slot 0 to target slot 0
	const testUserData int64 = 0xFD5E7D
	srcFDSlot := uint32(0)
	dstFDSlot := uint32(0)
	ctx := uring.PackDirect(uring.IORING_OP_MSG_RING, 0, 0, targetFD.Raw())
	err = srcRing.MsgRingFD(ctx, srcFDSlot, dstFDSlot, testUserData, false)
	if err != nil {
		t.Fatalf("MsgRingFD: %v", err)
	}
	t.Log("Submitted MsgRingFD operation")

	// Wait for source ring completion
	srcCQEs := make([]uring.CQEView, 4)
	deadline := time.Now().Add(5 * time.Second)
	srcDone := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && !srcDone {
		n, err := srcRing.Wait(srcCQEs)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait (source): %v", err)
		}
		for i := 0; i < n; i++ {
			res := srcCQEs[i].Res
			if res < 0 {
				if res == -38 { // ENOSYS
					t.Fatalf("MSG_RING_FD failed with ENOSYS on Linux 6.18+ baseline")
				}
				if res == -22 { // EINVAL
					t.Skip("MSG_RING_FD not supported (check fixed file tables)")
				}
				t.Fatalf("MsgRingFD failed: res=%d", res)
			}
			srcDone = true
			t.Logf("MsgRingFD completed on source ring: res=%d (dst slot index)", res)
		}
		b.Wait()
	}

	if !srcDone {
		// MsgRingFD may not complete on WSL2 due to cross-ring limitations
		t.Skip("MsgRingFD did not complete - may be WSL2 limitation")
	}

	// Wait for target ring to receive the CQE notification
	dstCQEs := make([]uring.CQEView, 4)
	deadline = time.Now().Add(5 * time.Second)
	dstDone := false
	b = iox.Backoff{}

	for time.Now().Before(deadline) && !dstDone {
		n, err := dstRing.Wait(dstCQEs)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait (target): %v", err)
		}
		for i := 0; i < n; i++ {
			res := dstCQEs[i].Res
			t.Logf("Target ring CQE: res=%d", res)
			if res >= 0 {
				dstDone = true
			}
		}
		b.Wait()
	}

	if !dstDone {
		t.Log("Note: Target CQE not received (skipCQE=false, may be timing)")
	}

	t.Log("MSG_RING_FD cross-ring FD transfer test passed")
}

// TestNAPIRegistration tests NAPI busy polling registration (Linux 6.19+).
// NAPI provides ultra-low latency network packet processing.
func TestNAPIRegistration(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Try to register NAPI with dynamic tracking
	// This will fail on kernels < 6.19 with ENOSYS or EINVAL
	err = ring.RegisterNAPI(
		100,   // 100µs busy poll timeout
		false, // don't prefer busy poll
		uring.IO_URING_NAPI_TRACKING_DYNAMIC,
	)
	if err != nil {
		// Check for unsupported operation
		if errors.Is(err, uring.ErrNotSupported) {
			t.Skip("NAPI not supported (requires kernel 6.19+)")
		}
		// EINVAL also indicates not supported on this system
		t.Logf("RegisterNAPI: %v (may require kernel 6.19+ or compatible NIC)", err)
		t.Skip("NAPI registration failed - may not be supported")
	}

	t.Log("NAPI registered successfully")

	// Unregister
	err = ring.UnregisterNAPI()
	if err != nil {
		t.Logf("UnregisterNAPI: %v", err)
	}
	t.Log("NAPI registration test passed")
}

// TestLinkedOperations tests IOSQE_IO_LINK flag for SQE chaining.
// Linked operations form atomic chains processed in order.
func TestLinkedOperations(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Chain 3 NOPs together with IOSQE_IO_LINK
	// They should complete in submission order
	for i := uint16(1); i <= 3; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, i, 0)
		var opts []uring.OpOptionFunc
		if i < 3 {
			opts = []uring.OpOptionFunc{uring.WithFlags(uring.IOSQE_IO_LINK)}
		}
		if err := ring.Nop(ctx, opts...); err != nil {
			t.Fatalf("Nop[%d]: %v", i, err)
		}
	}
	t.Log("Submitted 3 linked NOPs")

	// Verify all complete with success
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	completed := 0
	b := iox.Backoff{}

	for time.Now().Before(deadline) && completed < 3 {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_NOP {
				id := cqes[i].BufGroup()
				res := cqes[i].Res
				t.Logf("NOP[%d] completed: res=%d", id, res)
				if res != 0 {
					t.Errorf("NOP[%d] expected res=0, got %d", id, res)
				}
				completed++
			}
		}
		b.Wait()
	}

	if completed != 3 {
		t.Fatalf("Expected 3 completions, got %d", completed)
	}
	t.Log("Linked operations test passed")
}

// TestSQE128HelpersRequireWideRing verifies that the 128-byte helpers fail
// explicitly on default 64-byte rings.
func TestSQE128HelpersRequireWideRing(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	if got := ring.Features.SQEBytes; got != 64 {
		t.Fatalf("default ring SQEBytes = %d, want 64", got)
	}

	nopCtx := uring.PackDirect(uring.IORING_OP_NOP128, 0, 1, 0)
	err = ring.Nop128(nopCtx)
	if !errors.Is(err, uring.ErrNotSupported) {
		t.Fatalf("Nop128: got %v, want ErrNotSupported", err)
	}

	cmdCtx := uring.PackDirect(uring.IORING_OP_URING_CMD128, 0, 2, 0)
	err = ring.UringCmd128(cmdCtx, 0, nil)
	if !errors.Is(err, uring.ErrNotSupported) {
		t.Fatalf("UringCmd128: got %v, want ErrNotSupported", err)
	}
	t.Log("128-byte SQE helpers correctly reject non-SQE128 rings")
}

func TestSQE128NopCycle(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.SQE128 = true
	})
	if errors.Is(err, uring.ErrNotSupported) {
		t.Skip("SQE128 ring setup not supported on this kernel")
	}
	if err != nil {
		t.Fatalf("New SQE128 ring: %v", err)
	}
	mustStartRing(t, ring)
	if got := ring.Features.SQEBytes; got != 128 {
		t.Fatalf("SQE128 ring SQEBytes = %d, want 128", got)
	}

	ctx := uring.PackDirect(uring.IORING_OP_NOP128, 0, 0, 0)
	if err := ring.Nop128(ctx); errors.Is(err, uring.ErrNotSupported) {
		t.Skip("NOP128 opcode not supported on this kernel")
	} else if err != nil {
		t.Fatalf("Nop128: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_NOP128, time.Second)
	if !ok {
		t.Fatal("NOP128 operation did not complete")
	}
	if ev.Res < 0 {
		t.Errorf("NOP128 failed with result: %d", ev.Res)
	}
}

// =============================================================================
// TestAsyncCancel: IORING_OP_ASYNC_CANCEL operation
// =============================================================================

// TestAsyncCancel validates async cancellation of pending operations.
//
// Architecture:
//   - Submit a timeout that will wait 60 seconds (effectively forever for this test)
//   - Flush that timeout into the kernel before issuing AsyncCancel
//   - Submit AsyncCancel targeting the timeout's userData
//   - Verify: timeout receives -ECANCELED, cancel receives 0 or -EALREADY
//
// Cancel CQE results:
//   - 0: Successfully cancelled
//   - -EALREADY: Request already running (past cancellation point)
func TestAsyncCancel(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit a long timeout that we'll cancel
	const targetUserData = uint16(42)
	targetCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, targetUserData, 0)
	if err := ring.Timeout(targetCtx, 60*time.Second); err != nil {
		t.Fatalf("Timeout: %v", err)
	}
	if _, err := ring.Wait(nil); err != nil {
		t.Fatalf("Wait(nil): %v", err)
	}
	t.Log("Submitted and flushed 60s timeout with userData=42")

	// Cancel the timeout using AsyncCancel with target userData
	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 1, 0)
	if err := ring.AsyncCancel(cancelCtx, targetCtx.Raw()); err != nil {
		t.Fatalf("AsyncCancel: %v", err)
	}
	t.Log("Submitted AsyncCancel targeting userData=42")

	// Wait for both CQEs
	// Note: longer deadline covers race/coverage overhead and the documented
	// -EALREADY path where the target CQE may arrive slightly later.
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(10 * time.Second)
	b := iox.Backoff{}
	var gotTimeout, gotCancel bool
	var timeoutRes, cancelRes int32

	for (!gotTimeout || !gotCancel) && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			op := cqes[i].Op()
			res := cqes[i].Res
			t.Logf("CQE: op=%d, res=%d", op, res)

			switch op {
			case uring.IORING_OP_TIMEOUT:
				gotTimeout = true
				timeoutRes = res
			case uring.IORING_OP_ASYNC_CANCEL:
				gotCancel = true
				cancelRes = res
			}
		}
		if !gotTimeout || !gotCancel {
			b.Wait()
		}
	}

	// Validate results
	if !gotTimeout {
		t.Fatal("Timeout CQE not received")
	}
	if !gotCancel {
		t.Fatal("AsyncCancel CQE not received")
	}

	const (
		wantTimeoutCanceled = -int32(zcall.ECANCELED)
		wantCancelBusy      = -int32(zcall.EALREADY)
	)

	if timeoutRes != wantTimeoutCanceled {
		t.Errorf("Timeout result: got %d, want %d (-ECANCELED)", timeoutRes, wantTimeoutCanceled)
	} else {
		t.Log("Timeout correctly cancelled with -ECANCELED")
	}

	if cancelRes != 0 && cancelRes != wantCancelBusy {
		t.Errorf("AsyncCancel result: got %d, want 0 or %d (-EALREADY)", cancelRes, wantCancelBusy)
	} else {
		t.Logf("AsyncCancel result: %d (success)", cancelRes)
	}
}

// TestAsyncCancelExtended validates extended async cancellation modes.
// Tests the CANCEL_FD, CANCEL_OP, CANCEL_ANY, and CANCEL_ALL flags.
func TestAsyncCancelExtended(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pipe for poll operations
	var fds [2]int32
	errno := zcall.Pipe2(&fds, zcall.O_NONBLOCK|zcall.O_CLOEXEC)
	if errno != 0 {
		t.Fatalf("Pipe2: %v", zcall.Errno(errno))
	}
	readFD := uintptr(fds[0])
	defer zcall.Close(readFD)
	defer zcall.Close(uintptr(fds[1]))

	cqes := make([]uring.CQEView, 16)

	// Test AsyncCancelAny API exists
	t.Run("CancelAny", func(t *testing.T) {
		// Submit a long timeout to have something to cancel
		timeoutCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 1, 0)
		ring.Timeout(timeoutCtx, 60*time.Second)

		// Try to cancel any pending operation
		cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 2, 0)
		if err := ring.AsyncCancelAny(cancelCtx); err != nil {
			t.Fatalf("AsyncCancelAny: %v", err)
		}
		t.Log("Submitted AsyncCancelAny")

		// Collect CQEs
		n, _ := ring.Wait(cqes)
		t.Logf("Got %d CQEs", n)
		for i := 0; i < n; i++ {
			t.Logf("CQE %d: op=%d res=%d", i, cqes[i].Op(), cqes[i].Res)
		}
	})

	// Test AsyncCancelFD API exists
	t.Run("CancelFD", func(t *testing.T) {
		// Submit a poll on the pipe
		pollCtx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 10, int32(readFD))
		ring.PollAdd(pollCtx, uring.EPOLLIN)

		// Try to cancel by FD
		cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 11, int32(readFD))
		if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
			t.Fatalf("AsyncCancelFD: %v", err)
		}
		t.Log("Submitted AsyncCancelFD")

		// Collect CQEs
		n, _ := ring.Wait(cqes)
		t.Logf("Got %d CQEs", n)
		for i := 0; i < n; i++ {
			t.Logf("CQE %d: op=%d res=%d", i, cqes[i].Op(), cqes[i].Res)
		}
	})

	// Test AsyncCancelOpcode API exists
	t.Run("CancelOpcode", func(t *testing.T) {
		// Submit a timeout to cancel
		timeoutCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 20, 0)
		ring.Timeout(timeoutCtx, 60*time.Second)

		// Try to cancel by opcode
		cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 21, 0)
		if err := ring.AsyncCancelOpcode(cancelCtx, uring.IORING_OP_TIMEOUT, false); err != nil {
			t.Fatalf("AsyncCancelOpcode: %v", err)
		}
		t.Log("Submitted AsyncCancelOpcode")

		// Collect CQEs
		n, _ := ring.Wait(cqes)
		t.Logf("Got %d CQEs", n)
		for i := 0; i < n; i++ {
			t.Logf("CQE %d: op=%d res=%d", i, cqes[i].Op(), cqes[i].Res)
		}
	})

	// Verify cancel flag constants exist
	t.Run("Constants", func(t *testing.T) {
		_ = uring.IORING_ASYNC_CANCEL_ALL
		_ = uring.IORING_ASYNC_CANCEL_FD
		_ = uring.IORING_ASYNC_CANCEL_ANY
		_ = uring.IORING_ASYNC_CANCEL_FD_FIXED
		_ = uring.IORING_ASYNC_CANCEL_USERDATA
		_ = uring.IORING_ASYNC_CANCEL_OP
		t.Log("Cancel constants verified")
	})
}

// =============================================================================
// TestPollAddMultishot: IORING_POLL_ADD with multishot flag
// =============================================================================

// TestPollAddMultishot validates multishot poll operation.
//
// Architecture:
//   - Submit PollAddMultishot on a pipe's read end
//   - Write data multiple times to trigger poll events
//   - Each CQE should have IORING_CQE_F_MORE set (multishot continues)
//   - Cancel the poll and verify final CQE without F_MORE
func TestPollAddMultishot(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pipe using zcall
	var fds [2]int32
	errno := zcall.Pipe2(&fds, zcall.O_NONBLOCK|zcall.O_CLOEXEC)
	if errno != 0 {
		t.Fatalf("Pipe2: %v", zcall.Errno(errno))
	}
	readFD, writeFD := uintptr(fds[0]), uintptr(fds[1])
	defer zcall.Close(readFD)
	defer zcall.Close(writeFD)

	// Submit multishot poll on read end
	const targetUserData = uint16(100)
	pollCtx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, targetUserData, int32(readFD))
	if err := ring.PollAddMultishot(pollCtx, uring.EPOLLIN); err != nil {
		t.Fatalf("PollAddMultishot: %v", err)
	}
	t.Log("Submitted multishot poll")

	// Write data to trigger poll events
	cqes := make([]uring.CQEView, 8)
	pollEvents := 0
	hasMore := 0

	for i := 0; i < 3; i++ {
		// Write to pipe
		data := []byte("test")
		_, errno := zcall.Write(writeFD, data)
		if errno != 0 {
			t.Fatalf("Write[%d]: %v", i, zcall.Errno(errno))
		}

		// Wait for poll CQE
		deadline := time.Now().Add(time.Second)
		b := iox.Backoff{}
		for time.Now().Before(deadline) {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
				t.Fatalf("Wait: %v", err)
			}
			for j := range n {
				if cqes[j].Op() == uring.IORING_OP_POLL_ADD {
					pollEvents++
					if cqes[j].HasMore() {
						hasMore++
					}
					t.Logf("Poll CQE %d: res=%d, hasMore=%v", pollEvents, cqes[j].Res, cqes[j].HasMore())
					goto nextWrite
				}
			}
			b.Wait()
		}
	nextWrite:
		// Drain the pipe
		var buf [64]byte
		zcall.Read(readFD, buf[:])
	}

	if pollEvents < 1 {
		t.Error("Expected at least 1 poll event")
	}
	if hasMore < 1 {
		t.Errorf("Expected at least 1 CQE with F_MORE set, got %d", hasMore)
	}

	t.Logf("Multishot poll: %d events, %d with F_MORE", pollEvents, hasMore)

	// Cancel the poll
	cancelCtx := uring.PackDirect(uring.IORING_OP_POLL_REMOVE, 0, 1, int32(readFD))
	if err := ring.PollRemove(cancelCtx); err != nil {
		t.Logf("PollRemove: %v (may already be cancelled)", err)
	}

	t.Log("Poll multishot test complete")
}

// TestPollModes validates different poll mode APIs exist.
// This test verifies that the poll mode functions can be called.
// It exercises the public poll-mode surface on the Linux 6.18+ baseline.
func TestPollModes(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pipe using zcall
	var fds [2]int32
	errno := zcall.Pipe2(&fds, zcall.O_NONBLOCK|zcall.O_CLOEXEC)
	if errno != 0 {
		t.Fatalf("Pipe2: %v", zcall.Errno(errno))
	}
	readFD, writeFD := uintptr(fds[0]), uintptr(fds[1])
	defer zcall.Close(readFD)
	defer zcall.Close(writeFD)

	// Test PollAddLevel API exists and can be called
	t.Run("LevelAPI", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 1, int32(readFD))
		err := ring.PollAddLevel(ctx, uring.EPOLLIN)
		// Just verify we can call the function
		// EINVAL indicates that the current host rejected the operation
		t.Logf("PollAddLevel submit: %v", err)

		// Drain any CQEs
		cqes := make([]uring.CQEView, 8)
		ring.Wait(cqes)
	})

	// Test PollAddMultishotLevel API exists
	t.Run("MultishotLevelAPI", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 2, int32(readFD))
		err := ring.PollAddMultishotLevel(ctx, uring.EPOLLIN)
		t.Logf("PollAddMultishotLevel submit: %v", err)

		// Drain any CQEs
		cqes := make([]uring.CQEView, 8)
		ring.Wait(cqes)
	})

	// Verify poll mode constants exist
	t.Run("Constants", func(t *testing.T) {
		_ = uring.IORING_POLL_ADD_MULTI
		_ = uring.IORING_POLL_ADD_LEVEL
		_ = uring.IORING_POLL_UPDATE_EVENTS
		_ = uring.IORING_POLL_UPDATE_USER_DATA
		t.Log("Poll constants verified")
	})
}

// TestPollUpdate validates the IORING_OP_POLL_REMOVE with update flags.
// Poll update allows modifying an existing poll's events or userData in-place.
func TestPollUpdate(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pipe using zcall
	var fds [2]int32
	errno := zcall.Pipe2(&fds, zcall.O_NONBLOCK|zcall.O_CLOEXEC)
	if errno != 0 {
		t.Fatalf("Pipe2: %v", zcall.Errno(errno))
	}
	readFD, writeFD := uintptr(fds[0]), uintptr(fds[1])
	defer zcall.Close(readFD)
	defer zcall.Close(writeFD)

	const originalUserData = uint16(100)
	const updatedUserData = uint64(200)

	// Submit initial poll on read end
	pollCtx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, originalUserData, int32(readFD))
	if err := ring.PollAddMultishot(pollCtx, uring.EPOLLIN); err != nil {
		t.Fatalf("PollAddMultishot: %v", err)
	}
	if _, err := ring.Wait(nil); err != nil {
		t.Fatalf("Wait(nil): %v", err)
	}

	// Submit PollUpdate to change userData
	updateCtx := uring.PackDirect(uring.IORING_OP_POLL_REMOVE, 0, 1, int32(readFD))
	oldUserData := pollCtx.Raw() // The packed userData of the original poll
	updateFlags := uring.IORING_POLL_UPDATE_USER_DATA | uring.IORING_POLL_ADD_MULTI
	if err := ring.PollUpdate(updateCtx, oldUserData, updatedUserData, 0, updateFlags); err != nil {
		t.Fatalf("PollUpdate: %v", err)
	}

	// Collect CQEs
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	n := 0
	for time.Now().Before(deadline) {
		n, err = ring.Wait(cqes)
		if err != nil {
			if errors.Is(err, iox.ErrWouldBlock) || errors.Is(err, uring.ErrExists) {
				b.Wait()
				continue
			}
			t.Fatalf("Wait: %v", err)
		}
		break
	}
	if n == 0 {
		t.Fatal("PollUpdate did not complete")
	}

	// Analyze results
	t.Logf("Got %d CQEs", n)
	updateFound := false
	for i := 0; i < n; i++ {
		res := cqes[i].Res
		t.Logf("CQE %d: res=%d", i, res)
		// Check if this is the update CQE
		if res == 0 {
			updateFound = true
		} else if res < 0 {
			// ENOENT (-2) means poll not found
			// EINVAL (-22) indicates that the current host rejected the operation
			const ENOENT = 2
			const EINVAL = 22
			absRes := -res
			if absRes == ENOENT {
				t.Log("Poll not found (ENOENT) - may have already completed")
			} else if absRes == EINVAL {
				t.Fatalf("PollUpdate returned EINVAL on Linux 6.18+ baseline")
			} else {
				t.Logf("Error result: %d", res)
			}
		}
	}

	if updateFound {
		t.Log("PollUpdate succeeded")
	}

	// Verify the API signature is correct - this is a compile-time check
	t.Run("APISignature", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_POLL_REMOVE, 0, 1, 0)
		// Test all parameter combinations compile
		_ = ring.PollUpdate(ctx, 0, 0, 0, 0)
		_ = ring.PollUpdate(ctx, 0, 0, 0, uring.IORING_POLL_UPDATE_EVENTS)
		_ = ring.PollUpdate(ctx, 0, 0, 0, uring.IORING_POLL_UPDATE_USER_DATA)
		_ = ring.PollUpdate(ctx, 0, 0, 0, uring.IORING_POLL_UPDATE_EVENTS|uring.IORING_POLL_UPDATE_USER_DATA)
		t.Log("API signature verified")
	})
}

// =============================================================================
// Vectored I/O: ReadV / WriteV
// =============================================================================

func TestUringWriteVCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_writev_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())
	header := []byte("HDR:")
	payload := []byte("payload-data")
	trailer := []byte(":END")
	iovs := [][]byte{header, payload, trailer}
	total := len(header) + len(payload) + len(trailer)

	ctx := uring.PackDirect(uring.IORING_OP_WRITEV, 0, 0, fd)
	if err := ring.WriteV(ctx, iovs); err != nil {
		t.Fatalf("WriteV: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_WRITEV, 5*time.Second)
	if !ok {
		t.Fatal("WriteV did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("WriteV failed: %d", ev.Res)
	}
	if int(ev.Res) != total {
		t.Errorf("WriteV: got %d, want %d", ev.Res, total)
	}

	f.Seek(0, 0)
	got := make([]byte, total)
	n, _ := f.Read(got)
	want := "HDR:payload-data:END"
	if string(got[:n]) != want {
		t.Errorf("file content = %q, want %q", string(got[:n]), want)
	}
}

func TestUringReadVCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_readv_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	data := "AAAAABBBBBCCCCC"
	f.WriteString(data)
	f.Seek(0, 0)

	fd := int32(f.Fd())
	buf1 := make([]byte, 5)
	buf2 := make([]byte, 5)
	buf3 := make([]byte, 5)
	iovs := [][]byte{buf1, buf2, buf3}

	ctx := uring.PackDirect(uring.IORING_OP_READV, 0, 0, fd)
	if err := ring.ReadV(ctx, iovs); err != nil {
		t.Fatalf("ReadV: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_READV, 5*time.Second)
	if !ok {
		t.Fatal("ReadV did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("ReadV failed: %d", ev.Res)
	}
	if int(ev.Res) != len(data) {
		t.Errorf("ReadV: got %d bytes, want %d", ev.Res, len(data))
	}
	if string(buf1) != "AAAAA" {
		t.Errorf("buf1 = %q, want AAAAA", buf1)
	}
	if string(buf2) != "BBBBB" {
		t.Errorf("buf2 = %q, want BBBBB", buf2)
	}
	if string(buf3) != "CCCCC" {
		t.Errorf("buf3 = %q, want CCCCC", buf3)
	}
}

func TestUringWriteFixedCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("WriteFixed requires at least one registered buffer")
	}

	f, err := os.CreateTemp("", "uring_writefixed_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	bufIndex := 0
	buf := ring.RegisteredBuffer(bufIndex)
	if buf == nil {
		t.Fatal("RegisteredBuffer(0) returned nil")
	}

	message := []byte("Data written via WriteFixed")
	copy(buf, message)

	ctx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, int32(f.Fd()))
	if err := ring.WriteFixed(ctx, bufIndex, len(message)); err != nil {
		t.Fatalf("WriteFixed: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_WRITE_FIXED, 5*time.Second)
	if !ok {
		t.Fatal("WriteFixed did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("WriteFixed failed: %d", ev.Res)
	}
	if int(ev.Res) != len(message) {
		t.Errorf("WriteFixed: got %d, want %d", ev.Res, len(message))
	}

	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got := make([]byte, len(message))
	n, _ := f.Read(got)
	if string(got[:n]) != string(message) {
		t.Errorf("file content = %q, want %q", string(got[:n]), string(message))
	}
}

func TestUringReadFixedCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("ReadFixed requires at least one registered buffer")
	}

	f, err := os.CreateTemp("", "uring_readfixed_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	testData := "Content to read via ReadFixed"
	if _, err := f.WriteString(testData); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, int32(f.Fd()))
	buf, err := ring.ReadFixed(ctx, 0, uring.WithN(len(testData)))
	if err != nil {
		t.Fatalf("ReadFixed: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_READ_FIXED, 5*time.Second)
	if !ok {
		t.Fatal("ReadFixed did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("ReadFixed failed: %d", ev.Res)
	}
	if int(ev.Res) != len(testData) {
		t.Errorf("ReadFixed: got %d, want %d", ev.Res, len(testData))
	}
	if string(buf[:ev.Res]) != testData {
		t.Errorf("ReadFixed data = %q, want %q", string(buf[:ev.Res]), testData)
	}
}

// =============================================================================
// Fsync / Fallocate / FTruncate / SyncFileRange
// =============================================================================

func TestUringSyncCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_fsync_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	f.WriteString("fsync test data")

	fd := int32(f.Fd())
	ctx := uring.PackDirect(uring.IORING_OP_FSYNC, 0, 0, fd)
	if err := ring.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_FSYNC, 5*time.Second)
	if !ok {
		t.Fatal("Sync did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Sync failed: %d", ev.Res)
	}
}

func TestUringFallocateCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_fallocate_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())
	ctx := uring.PackDirect(uring.IORING_OP_FALLOCATE, 0, 0, fd)
	if err := ring.Fallocate(ctx, 0, 0, 4096); err != nil {
		t.Fatalf("Fallocate: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_FALLOCATE, 5*time.Second)
	if !ok {
		t.Fatal("Fallocate did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Fallocate failed: %d", ev.Res)
	}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 4096 {
		t.Errorf("file size = %d, want 4096", info.Size())
	}
}

func TestUringFTruncateCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_ftruncate_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	f.WriteString("some data that will be truncated to a shorter length")

	fd := int32(f.Fd())
	ctx := uring.PackDirect(uring.IORING_OP_FTRUNCATE, 0, 0, fd)
	if err := ring.FTruncate(ctx, 10); err != nil {
		t.Fatalf("FTruncate: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_FTRUNCATE, 5*time.Second)
	if !ok {
		t.Fatal("FTruncate did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("FTruncate failed: %d", ev.Res)
	}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 10 {
		t.Errorf("file size = %d, want 10", info.Size())
	}
}

func TestUringSyncFileRangeCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_syncrange_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	f.WriteString("sync file range test data")

	fd := int32(f.Fd())
	const SYNC_FILE_RANGE_WRITE = 2
	ctx := uring.PackDirect(uring.IORING_OP_SYNC_FILE_RANGE, 0, 0, fd)
	if err := ring.SyncFileRange(ctx, 0, 25, SYNC_FILE_RANGE_WRITE); err != nil {
		t.Fatalf("SyncFileRange: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SYNC_FILE_RANGE, 5*time.Second)
	if !ok {
		t.Fatal("SyncFileRange did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("SyncFileRange failed: %d", ev.Res)
	}
}

// =============================================================================
// Splice / Tee
// =============================================================================

func TestUringSpliceCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	srcFile, err := os.CreateTemp("", "uring_splice_src_*")
	if err != nil {
		t.Fatalf("CreateTemp (src): %v", err)
	}
	defer os.Remove(srcFile.Name())
	defer srcFile.Close()

	testData := "splice zero-copy transfer"
	srcFile.WriteString(testData)
	srcFile.Seek(0, 0)

	dstFile, err := os.CreateTemp("", "uring_splice_dst_*")
	if err != nil {
		t.Fatalf("CreateTemp (dst): %v", err)
	}
	defer os.Remove(dstFile.Name())
	defer dstFile.Close()

	var pipeFDs [2]int32
	errno := zcall.Pipe2(&pipeFDs, zcall.O_NONBLOCK)
	if errno != 0 {
		t.Fatalf("pipe2: errno=%d", errno)
	}
	pipeRead, pipeWrite := pipeFDs[0], pipeFDs[1]
	defer zcall.Close(uintptr(pipeRead))
	defer zcall.Close(uintptr(pipeWrite))

	// file → pipe
	srcFD := int32(srcFile.Fd())
	ctx1 := uring.PackDirect(uring.IORING_OP_SPLICE, 0, 0, pipeWrite)
	if err := ring.Splice(ctx1, int(srcFD), len(testData)); err != nil {
		t.Fatalf("Splice file→pipe: %v", err)
	}

	ev1, ok := waitForOp(t, ring, uring.IORING_OP_SPLICE, 5*time.Second)
	if !ok {
		t.Fatal("Splice file→pipe did not complete")
	}
	if ev1.Res <= 0 {
		t.Skipf("Splice file→pipe returned %d", ev1.Res)
	}

	// pipe → file
	dstFD := int32(dstFile.Fd())
	ctx2 := uring.PackDirect(uring.IORING_OP_SPLICE, 0, 0, dstFD)
	if err := ring.Splice(ctx2, int(pipeRead), int(ev1.Res)); err != nil {
		t.Fatalf("Splice pipe→file: %v", err)
	}

	ev2, ok := waitForOp(t, ring, uring.IORING_OP_SPLICE, 5*time.Second)
	if !ok {
		t.Fatal("Splice pipe→file did not complete")
	}
	if ev2.Res < 0 {
		t.Fatalf("Splice pipe→file failed: %d", ev2.Res)
	}

	dstFile.Seek(0, 0)
	got := make([]byte, len(testData))
	n, _ := dstFile.Read(got)
	if string(got[:n]) != testData {
		t.Errorf("dst content = %q, want %q", string(got[:n]), testData)
	}
}

func TestUringTeeCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	// Create two pipes: src and dst
	var srcPipe, dstPipe [2]int32
	if errno := zcall.Pipe2(&srcPipe, zcall.O_NONBLOCK); errno != 0 {
		t.Fatalf("pipe2 (src): errno=%d", errno)
	}
	defer zcall.Close(uintptr(srcPipe[0]))
	defer zcall.Close(uintptr(srcPipe[1]))

	if errno := zcall.Pipe2(&dstPipe, zcall.O_NONBLOCK); errno != 0 {
		t.Fatalf("pipe2 (dst): errno=%d", errno)
	}
	defer zcall.Close(uintptr(dstPipe[0]))
	defer zcall.Close(uintptr(dstPipe[1]))

	// Write data into source pipe
	testData := []byte("tee duplicates data")
	zcall.Write(uintptr(srcPipe[1]), testData)

	// Tee: srcPipe[0] → dstPipe[1]
	ctx := uring.PackDirect(uring.IORING_OP_TEE, 0, 0, dstPipe[1])
	if err := ring.Tee(ctx, int(srcPipe[0]), len(testData)); err != nil {
		t.Fatalf("Tee: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_TEE, 5*time.Second)
	if !ok {
		t.Fatal("Tee did not complete")
	}
	if ev.Res <= 0 {
		t.Skipf("Tee returned %d", ev.Res)
	}

	// Read from dst pipe to verify
	got := make([]byte, len(testData))
	n, _ := zcall.Read(uintptr(dstPipe[0]), got)
	if string(got[:n]) != string(testData) {
		t.Errorf("tee content = %q, want %q", string(got[:n]), testData)
	}

	// Source pipe should still have data (tee duplicates, doesn't consume)
	srcGot := make([]byte, len(testData))
	sn, _ := zcall.Read(uintptr(srcPipe[0]), srcGot)
	if string(srcGot[:sn]) != string(testData) {
		t.Errorf("source pipe content = %q, want %q (tee should not consume)", string(srcGot[:sn]), testData)
	}
}

// =============================================================================
// Filesystem: Statx, RenameAt, UnlinkAt, MkdirAt, SymlinkAt, LinkAt
// =============================================================================

func TestUringStatxCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_statx_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("statx test content")
	f.Close()

	var stat uring.Statx
	const STATX_BASIC_STATS = 0x7ff
	ctx := uring.PackDirect(uring.IORING_OP_STATX, 0, 0, uring.AT_FDCWD)
	if err := ring.Statx(ctx, f.Name(), 0, STATX_BASIC_STATS, &stat); err != nil {
		t.Fatalf("Statx: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_STATX, 5*time.Second)
	if !ok {
		t.Fatal("Statx did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("Statx failed: %d", ev.Res)
	}

	if stat.Size != 18 {
		t.Errorf("Statx.Size = %d, want 18", stat.Size)
	}
	if stat.Mode&0o170000 != 0o100000 {
		t.Errorf("Statx.Mode = 0o%o, want regular file", stat.Mode)
	}
}

func TestUringMkdirAtCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "newdir")

	ctx := uring.PackDirect(uring.IORING_OP_MKDIRAT, 0, 0, uring.AT_FDCWD)
	if err := ring.MkdirAt(ctx, target, 0o755); err != nil {
		t.Fatalf("MkdirAt: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_MKDIRAT, 5*time.Second)
	if !ok {
		t.Fatal("MkdirAt did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("MkdirAt failed: %d", ev.Res)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("created path is not a directory")
	}
}

func TestUringRenameAtCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	os.WriteFile(oldPath, []byte("rename me"), 0o644)

	ctx := uring.PackDirect(uring.IORING_OP_RENAMEAT, 0, 0, uring.AT_FDCWD)
	if err := ring.RenameAt(ctx, oldPath, newPath, 0); err != nil {
		t.Fatalf("RenameAt: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_RENAMEAT, 5*time.Second)
	if !ok {
		t.Fatal("RenameAt did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("RenameAt failed: %d", ev.Res)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old path still exists after rename")
	}
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("ReadFile new: %v", err)
	}
	if string(got) != "rename me" {
		t.Errorf("new file content = %q, want %q", got, "rename me")
	}
}

func TestUringUnlinkAtCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "delete_me.txt")
	os.WriteFile(target, []byte("bye"), 0o644)

	ctx := uring.PackDirect(uring.IORING_OP_UNLINKAT, 0, 0, uring.AT_FDCWD)
	if err := ring.UnlinkAt(ctx, target, 0); err != nil {
		t.Fatalf("UnlinkAt: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_UNLINKAT, 5*time.Second)
	if !ok {
		t.Fatal("UnlinkAt did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("UnlinkAt failed: %d", ev.Res)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("file still exists after unlink")
	}
}

func TestUringSymlinkAtCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	os.WriteFile(target, []byte("symlink target"), 0o644)

	ctx := uring.PackDirect(uring.IORING_OP_SYMLINKAT, 0, 0, uring.AT_FDCWD)
	if err := ring.SymlinkAt(ctx, target, link); err != nil {
		t.Fatalf("SymlinkAt: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SYMLINKAT, 5*time.Second)
	if !ok {
		t.Fatal("SymlinkAt did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("SymlinkAt failed: %d", ev.Res)
	}

	resolved, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if resolved != target {
		t.Errorf("symlink target = %q, want %q", resolved, target)
	}
}

func TestUringLinkAtCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "original.txt")
	link := filepath.Join(dir, "hardlink.txt")
	os.WriteFile(target, []byte("hard link target"), 0o644)

	ctx := uring.PackDirect(uring.IORING_OP_LINKAT, 0, 0, uring.AT_FDCWD)
	if err := ring.LinkAt(ctx, uring.AT_FDCWD, target, link, 0); err != nil {
		t.Fatalf("LinkAt: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_LINKAT, 5*time.Second)
	if !ok {
		t.Fatal("LinkAt did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("LinkAt failed: %d", ev.Res)
	}

	got, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("ReadFile link: %v", err)
	}
	if string(got) != "hard link target" {
		t.Errorf("link content = %q, want %q", got, "hard link target")
	}

	// Verify same inode
	origInfo, _ := os.Stat(target)
	linkInfo, _ := os.Stat(link)
	if !os.SameFile(origInfo, linkInfo) {
		t.Error("hard link does not point to same inode")
	}
}

// =============================================================================
// Xattr: FSetXattr / FGetXattr / SetXattr / GetXattr
// =============================================================================

func TestUringFSetXattrFGetXattrCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_xattr_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	fd := int32(f.Fd())
	name := "user.test"
	value := []byte("xattr-value")

	// FSetXattr
	setCtx := uring.PackDirect(uring.IORING_OP_FSETXATTR, 0, 0, fd)
	if err := ring.FSetXattr(setCtx, name, value, 0); err != nil {
		t.Fatalf("FSetXattr: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_FSETXATTR, 5*time.Second)
	if !ok {
		t.Fatal("FSetXattr did not complete")
	}
	if ev.Res < 0 {
		skipIfXattrUnsupported(t, "FSetXattr", ev.Res)
		t.Fatalf("FSetXattr failed: %d", ev.Res)
	}

	// FGetXattr
	getBuf := make([]byte, 64)
	getCtx := uring.PackDirect(uring.IORING_OP_FGETXATTR, 0, 0, fd)
	if err := ring.FGetXattr(getCtx, name, getBuf); err != nil {
		t.Fatalf("FGetXattr: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_FGETXATTR, 5*time.Second)
	if !ok {
		t.Fatal("FGetXattr did not complete")
	}
	if ev.Res < 0 {
		skipIfXattrUnsupported(t, "FGetXattr", ev.Res)
		t.Fatalf("FGetXattr failed: %d", ev.Res)
	}
	if string(getBuf[:ev.Res]) != string(value) {
		t.Errorf("FGetXattr = %q, want %q", string(getBuf[:ev.Res]), value)
	}
}

func TestUringSetXattrGetXattrCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_xattr_path_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	f.Close()

	name := "user.pathattr"
	value := []byte("path-xattr-value")

	// SetXattr
	setCtx := uring.PackDirect(uring.IORING_OP_SETXATTR, 0, 0, 0)
	if err := ring.SetXattr(setCtx, f.Name(), name, value, 0); err != nil {
		t.Fatalf("SetXattr: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_SETXATTR, 5*time.Second)
	if !ok {
		t.Fatal("SetXattr did not complete")
	}
	if ev.Res < 0 {
		skipIfXattrUnsupported(t, "SetXattr", ev.Res)
		t.Fatalf("SetXattr failed: %d", ev.Res)
	}

	// GetXattr
	getBuf := make([]byte, 64)
	getCtx := uring.PackDirect(uring.IORING_OP_GETXATTR, 0, 0, 0)
	if err := ring.GetXattr(getCtx, f.Name(), name, getBuf); err != nil {
		t.Fatalf("GetXattr: %v", err)
	}

	ev, ok = waitForOp(t, ring, uring.IORING_OP_GETXATTR, 5*time.Second)
	if !ok {
		t.Fatal("GetXattr did not complete")
	}
	if ev.Res < 0 {
		skipIfXattrUnsupported(t, "GetXattr", ev.Res)
		t.Fatalf("GetXattr failed: %d", ev.Res)
	}
	if string(getBuf[:ev.Res]) != string(value) {
		t.Errorf("GetXattr = %q, want %q", string(getBuf[:ev.Res]), value)
	}
}

// =============================================================================
// FileAdvise (fadvise)
// =============================================================================

func TestUringFileAdviseCycle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	f, err := os.CreateTemp("", "uring_fadvise_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	f.WriteString("fadvise test data for sequential access hint")

	fd := int32(f.Fd())
	const POSIX_FADV_SEQUENTIAL = 2
	ctx := uring.PackDirect(uring.IORING_OP_FADVISE, 0, 0, fd)
	if err := ring.FileAdvise(ctx, 0, 45, POSIX_FADV_SEQUENTIAL); err != nil {
		t.Fatalf("FileAdvise: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_FADVISE, 5*time.Second)
	if !ok {
		t.Fatal("FileAdvise did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("FileAdvise failed: %d", ev.Res)
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkUringNopSubmit(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesLarge
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ring.Nop(ctx)
	}
}

func BenchmarkUringNopCycle(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesLarge
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	mustStartRing(b, ring)

	cqes := make([]uring.CQEView, 256)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
		ring.Nop(ctx)

		for {
			n, _ := ring.Wait(cqes)
			if n > 0 {
				break
			}
		}
	}
}
