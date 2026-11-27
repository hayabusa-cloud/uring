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
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
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

func TestSingleIssuerStartBindsOwner(t *testing.T) {
	// Keep the creator pinned so the owner must use a different OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = testLockedBufferMem
		opt.ReadBufferNum = testProvidedBufferNum
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ready := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		var err error
		pending := false
		defer func() {
			// A failed wait does not permit teardown of an unobserved request.
			// In that failure case, leave resources intact and exit locked.
			if !pending {
				err = errors.Join(err, ring.Stop())
				runtime.UnlockOSThread()
			}
			done <- err
		}()
		if err = ring.Start(); err != nil {
			return
		}
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
		if err = ring.Nop(ctx); err != nil {
			return
		}
		pending = true
		var cqes [1]uring.CQEView
		var backoff iox.Backoff
		deadline := time.Now().Add(time.Second)
		for pending && time.Now().Before(deadline) {
			n, waitErr := ring.Wait(cqes[:])
			if n > 0 {
				backoff.Reset()
				if cqes[0].Context() != ctx || cqes[0].HasMore() {
					err = fmt.Errorf("unexpected NOP completion: %+v", cqes[0])
					return
				}
				pending = false
				if cqes[0].Res != 0 {
					err = fmt.Errorf("NOP result = %d, want 0", cqes[0].Res)
					return
				}
			}
			if waitErr != nil && !errors.Is(waitErr, iox.ErrWouldBlock) {
				err = waitErr
				return
			}
			if pending {
				backoff.Wait()
			}
		}
		if pending {
			err = errors.New("owner NOP did not complete")
			return
		}
		close(ready)
		<-resume // The creator's intentional wrong-thread call must be serialized.
	}()

	select {
	case <-ready:
		var cqes [1]uring.CQEView
		_, waitErr := ring.Wait(cqes[:])
		close(resume)
		if err := <-done; err != nil {
			t.Fatalf("owner Stop: %v", err)
		}
		if !errors.Is(waitErr, uring.ErrExists) {
			t.Fatalf("creator Wait = %v, want %v", waitErr, uring.ErrExists)
		}
	case err := <-done:
		close(resume)
		t.Fatalf("owner startup or completion: %v", err)
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("owner did not finish startup and completion")
	}
}

func TestSingleIssuerWrongTaskWaitReturnsErrExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping io_uring task-identity test in short mode")
	}

	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.MultiIssuers = false
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	waitErr := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		_, err := ring.Wait(make([]uring.CQEView, 1))
		waitErr <- err
	}()

	select {
	case err := <-waitErr:
		if !errors.Is(err, uring.ErrExists) {
			t.Fatalf("wrong-task Wait error = %v, want %v", err, uring.ErrExists)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrong-task Wait did not return")
	}
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
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = testLockedBufferMem
		opt.ReadBufferNum = testProvidedBufferNum
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	if ring.MultiIssuers {
		t.Fatal("default ring enabled shared submission")
	}
	runtime.Gosched() // The default owner must remain on its locked OS thread.

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

func requireTerminalResults(t *testing.T, ring *uring.Uring, want map[uring.SQEContext]int32) []uring.CQEView {
	t.Helper()
	cqes := waitForCQEs(t, ring, len(want))
	for _, cqe := range cqes {
		ctx := cqe.Context()
		res, ok := want[ctx]
		if !ok || cqe.Res != res || cqe.HasMore() {
			t.Fatalf("CQE context=%#x result=%d flags=%#x, pending=%v", ctx.Raw(), cqe.Res, cqe.Flags, want)
		}
		delete(want, ctx)
	}
	return cqes
}

func TestUringTimeoutCycle(t *testing.T) {
	nop := uring.PackDirect(uring.IORING_OP_NOP, 0, 1, 0)
	timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 2, 0)
	linkedTimer := timer.OrFlags(uring.IOSQE_IO_LINK)
	for _, tc := range []struct {
		name    string
		ctx     uring.SQEContext
		delay   time.Duration
		opts    []uring.OpOptionFunc
		wantRes int32
		wantNOP int32
	}{
		{name: "count reached", ctx: timer, delay: time.Hour},
		{
			name: "time only", ctx: timer, delay: time.Millisecond,
			opts: []uring.OpOptionFunc{uring.WithCount(0)}, wantRes: -int32(zcall.ETIME),
		},
		{
			name: "expiry cancels successor", ctx: linkedTimer, delay: time.Millisecond,
			opts: []uring.OpOptionFunc{uring.WithCount(0)}, wantRes: -int32(zcall.ETIME), wantNOP: -int32(zcall.ECANCELED),
		},
		{
			name: "successor runs", ctx: linkedTimer, delay: time.Millisecond,
			opts: []uring.OpOptionFunc{uring.WithCount(0), uring.WithTimeoutEtimeSuccess()}, wantRes: -int32(zcall.ETIME),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			if err := ring.Timeout(tc.ctx, tc.delay, tc.opts...); err != nil {
				t.Fatal(err)
			}
			if err := ring.Nop(nop); err != nil {
				t.Fatal(err)
			}
			requireTerminalResults(t, ring, map[uring.SQEContext]int32{tc.ctx: tc.wantRes, nop: tc.wantNOP})
		})
	}
}

func TestTimeoutMultishot(t *testing.T) {
	ring := newTestRing(t)
	timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 1, 0)
	for _, tc := range []struct {
		name string
		want int
		opts []uring.OpOptionFunc
	}{
		{"default", 1, []uring.OpOptionFunc{uring.WithTimeoutMultishot()}},
		{"three expirations", 3, []uring.OpOptionFunc{uring.WithTimeoutMultishot(), uring.WithCount(3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ring.Timeout(timer, time.Millisecond, tc.opts...); err != nil {
				t.Fatal(err)
			}
			for i, cqe := range waitForCQEs(t, ring, tc.want) {
				wantMore := i+1 < tc.want
				if cqe.Context() != timer || cqe.Res != -int32(zcall.ETIME) || cqe.HasMore() != wantMore {
					t.Fatalf("expiry %d: context=%#x result=%d more=%t, want context=%#x result=-ETIME more=%t",
						i+1, cqe.Context().Raw(), cqe.Res, cqe.HasMore(), timer.Raw(), wantMore)
				}
			}
		})
	}
}

func TestTimeoutUpdate(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 42, 0)
	update := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 1, 0)
	for _, absolute := range []bool{false, true} {
		t.Run(fmt.Sprintf("absolute=%t", absolute), func(t *testing.T) {
			if err := ring.Timeout(timer, time.Hour, uring.WithCount(0), uring.WithTimeoutRealTime()); err != nil {
				t.Fatal(err)
			}
			deadline := time.Millisecond
			if absolute {
				deadline = time.Duration(time.Now().UnixNano())
			}
			if err := ring.TimeoutUpdate(update, timer.Raw(), deadline, absolute); err != nil {
				t.Fatal(err)
			}
			requireTerminalResults(t, ring, map[uring.SQEContext]int32{update: 0, timer: -int32(zcall.ETIME)})
		})
	}
}

func TestTimeoutRemove(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
	remove := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 1, 0)
	if err := ring.Timeout(timer, time.Minute, uring.WithCount(0)); err != nil {
		t.Fatal(err)
	}
	if err := ring.TimeoutRemove(remove, timer.Raw()); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{remove: 0, timer: -int32(zcall.ECANCELED)})
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
			!errors.Is(err, iox.ErrWouldBlock) {
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
	ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0xa501, 0)
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
	if got := ev.BufGroup(); got != ctx.BufGroup() {
		t.Errorf("socket completion tag = %#x, want %#x", got, ctx.BufGroup())
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

	// Buffer for the two file descriptors (read end, write end).
	fds := [2]iofd.FD{iofd.InvalidFD, iofd.InvalidFD}

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
		if !fds[0].Valid() || !fds[1].Valid() {
			t.Errorf("Invalid file descriptors: read=%d, write=%d", fds[0], fds[1])
		}
		for i := range fds {
			if err := fds[i].Close(); err != nil {
				t.Errorf("Close pipe descriptor %d: %v", i, err)
			}
		}
	}
}

// =============================================================================
// Resize Rings Tests
// =============================================================================

func TestResizeRings(t *testing.T) {
	const wantSQ, wantCQ = 1, 4
	for _, tt := range []struct {
		name     string
		sqe128   bool
		sqeBytes int
	}{
		{name: "SQE64", sqeBytes: 64},
		{name: "SQE128", sqe128: true, sqeBytes: 128},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime.LockOSThread()
			t.Cleanup(runtime.UnlockOSThread)

			ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
				opt.Entries = wantSQ
				opt.MultiIssuers = false
				opt.SQE128 = tt.sqe128
			})
			if tt.sqe128 && errors.Is(err, uring.ErrNotSupported) {
				t.Skip("SQE128 ring setup not supported on this kernel")
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			mustStartRing(t, ring)
			if got := ring.Features.SQEBytes; got != tt.sqeBytes {
				t.Fatalf("SQEBytes = %d, want %d", got, tt.sqeBytes)
			}
			if got := ring.Features.CQEntries; got != 2 {
				t.Fatalf("initial CQEntries = %d, want 2", got)
			}

			// Leave the NOP completion unread and the time-only timeout in flight.
			timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
			nop := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
			if err := ring.Timeout(timer, time.Hour, uring.WithCount(0)); err != nil {
				t.Fatal(err)
			}
			if _, err := ring.Wait(nil); err != nil {
				t.Fatal(err)
			}
			if err := ring.Nop(nop); err != nil {
				t.Fatal(err)
			}
			checkWaitWithoutReaping(t, ring, ring.Wait, 1)

			if got := ring.SQAvailable(); got != wantSQ {
				t.Fatalf("SQAvailable before resize = %d, want %d", got, wantSQ)
			}
			if err := ring.ResizeRings(0, wantCQ); err != nil {
				t.Fatalf("ResizeRings: %v", err)
			}
			if sq, cq := ring.Features.SQEntries, ring.Features.CQEntries; sq != wantSQ || cq != wantCQ {
				t.Errorf("resize geometry = %d/%d, want %d/%d", sq, cq, wantSQ, wantCQ)
			}
			if got := ring.Features.SQEBytes; got != tt.sqeBytes {
				t.Errorf("SQEBytes after resize = %d, want %d", got, tt.sqeBytes)
			}

			remove := uring.PackDirect(uring.IORING_OP_TIMEOUT_REMOVE, 0, 0, 0)
			if err := ring.TimeoutRemove(remove, timer.Raw()); err != nil {
				t.Fatal(err)
			}
			// All three completions must be visible together, beyond the old capacity.
			checkWaitWithoutReaping(t, ring, ring.Wait, 3)
			requireTerminalResults(t, ring, map[uring.SQEContext]int32{
				nop:    0,
				timer:  -int32(zcall.ECANCELED),
				remove: 0,
			})

			// Check wide-command admission without requiring a command device.
			ctx := uring.PackDirect(uring.IORING_OP_URING_CMD, 0, 0, -1)
			var command [80]byte
			err = ring.UringCmdInline(ctx, 0, command[:])
			if !tt.sqe128 {
				if !errors.Is(err, uring.ErrNotSupported) {
					t.Fatalf("UringCmdInline on SQE64 after resize: %v, want ErrNotSupported", err)
				}
			} else {
				if err != nil {
					t.Fatalf("UringCmdInline on SQE128 after resize: %v", err)
				}
				requireTerminalResults(t, ring, map[uring.SQEContext]int32{ctx: -int32(zcall.EBADF)})
			}
		})
	}
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

// TestQueryOpcodes tests the baseline io_uring opcode query interface.
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
		t.Fatalf("QueryOpcodes (Linux 6.18+ baseline): %v", err)
	}
	if info.ResultSize == 0 {
		t.Fatal("QueryOpcodes result size = 0, want kernel-reported query data")
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
		opt.MultiIssuers = true // Avoid SINGLE_ISSUER io_uring_register task affinity in CI.
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
		opt.MultiIssuers = true // Avoid SINGLE_ISSUER io_uring_register task affinity in CI.
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
	updated, err := ring.RegisterFilesUpdate(0, []int32{fd})
	if err != nil {
		t.Fatalf("RegisterFilesUpdate: %v", err)
	}
	if updated != 1 {
		t.Fatalf("RegisterFilesUpdate updated = %d, want 1", updated)
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
	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0xa502, serverFD)
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
	if got := ev.BufGroup(); got != acceptCtx.BufGroup() {
		t.Errorf("accept completion tag = %#x, want %#x", got, acceptCtx.BufGroup())
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

func TestSocketDirect(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) {
		opt.Entries = uring.EntriesPico
		opt.NotifySucceed = true
	})
	// Leave only slot 1 free so the result identifies automatic allocation.
	occupied, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	if err := ring.RegisterFiles([]int32{int32(occupied.Fd()), -1}); err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}

	// Fill the SQ with distinguishable NOPs before attempting socket creation.
	pending := make(map[uring.SQEContext]bool)
	for i := range ring.SQAvailable() {
		ctx := uring.ForFD(int32(i + 1))
		if err := ring.Nop(ctx); err != nil {
			t.Fatal(err)
		}
		pending[ctx] = true
	}
	socketCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0xa503, 0)
	if err := ring.TCP4SocketDirect(socketCtx); !iox.IsWouldBlock(err) {
		t.Fatalf("TCP4SocketDirect on full SQ: got %v, want ErrWouldBlock", err)
	}
	if free := ring.SQAvailable(); free != 0 {
		t.Fatalf("full SQ after refusal: %d free entries, want 0 before Wait", free)
	}
	for _, cqe := range waitForCQEs(t, ring, len(pending)) {
		ctx := cqe.Context()
		if !pending[ctx] || cqe.Res != 0 {
			t.Fatalf("unexpected completion while draining NOPs: %+v", cqe)
		}
		delete(pending, ctx)
	}

	if err := ring.TCP4SocketDirect(socketCtx); err != nil {
		t.Fatalf("TCP4SocketDirect after drain: %v", err)
	}
	ev := waitForCQEs(t, ring, 1)[0]
	if ev.Op() != uring.IORING_OP_SOCKET || ev.BufGroup() != socketCtx.BufGroup() || ev.Res != 1 {
		t.Fatalf("socket retry: op=%d tag=%#x slot=%d, want SOCKET tag=%#x slot=1", ev.Op(), ev.BufGroup(), ev.Res, socketCtx.BufGroup())
	}

	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, uring.IOSQE_FIXED_FILE, 0, ev.Res)
	mustCloseAndWait(t, ring, closeCtx, time.Second, "direct socket")
	if err := ring.UnregisterFiles(); err != nil {
		t.Fatalf("UnregisterFiles: %v", err)
	}
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
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
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

func TestFutexWakeWait(t *testing.T) {
	for _, tc := range []struct {
		name      string
		waitOp    uint8
		wakeIndex int32
	}{
		{"scalar", uring.IORING_OP_FUTEX_WAIT, 0},
		{"vector_first", uring.IORING_OP_FUTEX_WAITV, 0},
		{"vector_second", uring.IORING_OP_FUTEX_WAITV, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			words := [2]uint32{11, 22}
			var pin runtime.Pinner
			pin.Pin(&words[0])
			t.Cleanup(pin.Unpin)
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			word := &words[tc.wakeIndex]

			var err error
			switch tc.waitOp {
			case uring.IORING_OP_FUTEX_WAIT:
				err = ring.FutexWait(uring.ForFD(0), word, uint64(*word), uring.FUTEX_BITSET_MATCH_ANY, uring.FUTEX2_SIZE_U32)
			case uring.IORING_OP_FUTEX_WAITV:
				// Linux struct futex_waitv, one entry per word.
				var waiters [len(words)]struct {
					val   uint64
					uaddr uint64
					flags uint32
					_     uint32
				}
				pin.Pin(&waiters[0])
				for i := range waiters {
					waiters[i].val = uint64(words[i])
					waiters[i].uaddr = uint64(uintptr(unsafe.Pointer(&words[i])))
					waiters[i].flags = uring.FUTEX2_SIZE_U32
				}
				err = ring.FutexWaitV(uring.ForFD(0), unsafe.Pointer(&waiters[0]), uint32(len(waiters)), 0)
			}
			if err != nil {
				t.Fatalf("submit wait: %v", err)
			}
			var early [1]uring.CQEView
			if n, err := ring.Wait(early[:]); n != 0 || !iox.IsWouldBlock(err) {
				t.Fatalf("wait before wake: n=%d result=%d err=%v", n, early[0].Res, err)
			}
			if err := ring.FutexWake(uring.ForFD(0), word, 1, uring.FUTEX_BITSET_MATCH_ANY, uring.FUTEX2_SIZE_U32); err != nil {
				t.Fatalf("submit wake: %v", err)
			}

			cqes := waitForCQEs(t, ring, 2)
			wait, wake := cqes[0], cqes[1]
			if wait.Op() == uring.IORING_OP_FUTEX_WAKE {
				wait, wake = wake, wait
			}
			if wait.Op() != tc.waitOp || wait.Res != tc.wakeIndex {
				t.Fatalf("wait CQE: op=%d result=%d, want op=%d result=%d", wait.Op(), wait.Res, tc.waitOp, tc.wakeIndex)
			}
			if wake.Op() != uring.IORING_OP_FUTEX_WAKE || wake.Res != 1 {
				t.Fatalf("wake CQE: op=%d result=%d, want FUTEX_WAKE with one waiter", wake.Op(), wake.Res)
			}
		})
	}
}

// TestMsgRingSelf tests MSG_RING to the same ring (self-messaging).
func TestMsgRingSelf(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Get this ring's FD for self-messaging
	ringFD := ring.RingFD()
	if ringFD < 0 {
		t.Fatal("RingFD not available")
	}
	t.Logf("Self ring FD: %d", ringFD)

	// Wait must preserve the opaque target ID even when it carries the Extended tag.
	testUserData := uint64(uring.CtxModeExtended) | 0xDEADBEEF
	const testResult int32 = -99 // Signed message payload on the target CQE.
	ctx := uring.PackDirect(uring.IORING_OP_MSG_RING, 0, 0, ringFD.Raw())
	err = ring.MsgRing(ctx, int64(testUserData), testResult)
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
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			b.Wait()
			continue
		}
		b.Reset()
		for i := 0; i < n; i++ {
			cqe := cqes[i]
			switch cqe.Context().Raw() {
			case ctx.Raw():
				if sendCompleted || cqe.Res != 0 {
					t.Fatalf("unexpected source CQE: res=%d, duplicate=%t", cqe.Res, sendCompleted)
				}
				sendCompleted = true
			case testUserData:
				if recvCompleted || cqe.Res != testResult {
					t.Fatalf("unexpected target CQE: res=%d, duplicate=%t; want res=%d", cqe.Res, recvCompleted, testResult)
				}
				recvCompleted = true
			default:
				t.Fatalf("unexpected CQE: user_data=%#x, res=%d", cqe.Context(), cqe.Res)
			}
		}
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
		t.Fatal("RingFD not available")
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
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait (source): %v", err)
		}
		if n == 0 {
			b.Wait()
			continue
		}
		b.Reset()
		if n != 1 {
			t.Fatalf("source CQE count = %d, want 1", n)
		}
		cqe := srcCQEs[0]
		if cqe.Context() != ctx || cqe.Res != 0 {
			t.Fatalf("source CQE: user_data=%#x, res=%d; want user_data=%#x, res=0", cqe.Context(), cqe.Res, ctx)
		}
		srcDone = true
	}

	if !srcDone {
		t.Fatal("MSG_RING source completion was not received")
	}

	// Wait for target ring to receive the CQE
	dstCQEs := make([]uring.CQEView, 4)
	deadline = time.Now().Add(5 * time.Second)
	dstDone := false
	b = iox.Backoff{}

	for time.Now().Before(deadline) && !dstDone {
		n, err := dstRing.Wait(dstCQEs)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait (target): %v", err)
		}
		if n == 0 {
			b.Wait()
			continue
		}
		b.Reset()
		if n != 1 {
			t.Fatalf("target CQE count = %d, want 1", n)
		}
		cqe := dstCQEs[0]
		if cqe.Context().Raw() != uint64(testUserData) || cqe.Res != testResult {
			t.Fatalf("target CQE: user_data=%#x, res=%d; want user_data=%#x, res=%d", cqe.Context(), cqe.Res, testUserData, testResult)
		}
		dstDone = true
	}

	if !dstDone {
		t.Fatal("MSG_RING target completion was not received")
	}
	t.Log("MSG_RING cross-ring communication test passed")
}

// TestMsgRingFD checks both completion results and the installed file reference.
func TestMsgRingFD(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dstSlot    uint32
		wantResult int32
	}{
		{"explicit_zero", 0, 0},
		{"explicit_one", 1, 0},
		{"allocate_one", uring.IORING_FILE_INDEX_ALLOC, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Slot zero keeps the default CQE policy. The nonzero cases also
			// observe successful sends through both registrations.
			checkFiles := tc.dstSlot != 0
			options := func(o *uring.Options) { o.NotifySucceed = checkFiles }
			src := newTestRing(t, options)
			dst := newTestRing(t, options)
			fds, err := newUnixSocketPairForTest()
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestFds(fds)
			if err := src.RegisterFiles([]int32{int32(fds[0])}); err != nil {
				t.Fatalf("register source socket: %v", err)
			}
			if err := dst.RegisterFilesSparse(2); err != nil {
				t.Fatalf("register target table: %v", err)
			}
			if checkFiles {
				// Leave just slot 1 free for either kind of installation.
				other, err := os.Open(os.DevNull)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				n, err := dst.RegisterFilesUpdate(0, []int32{int32(other.Fd())})
				if err != nil || n != 1 {
					t.Fatalf("occupy target slot 0: n=%d err=%v", n, err)
				}
			}

			const messageID = 0xFD5E7D
			targetFD := dst.RingFD()
			if targetFD < 0 {
				t.Fatal("target ring has no descriptor")
			}
			ctx := uring.PackDirect(uring.IORING_OP_MSG_RING, 0, 0, targetFD.Raw())
			if err := src.MsgRingFD(ctx, 0, tc.dstSlot, messageID, false); err != nil {
				t.Fatal(err)
			}
			requireMsgRingResult(t, src, ctx.Raw(), tc.wantResult)
			reported := requireMsgRingResult(t, dst, messageID, tc.wantResult)
			if !checkFiles {
				return
			}
			slot := iofd.FD(tc.dstSlot)
			if tc.dstSlot == uring.IORING_FILE_INDEX_ALLOC {
				slot = iofd.FD(reported)
			}

			for _, send := range []struct {
				ring   *uring.Uring
				slot   iofd.FD
				marker byte
			}{
				{src, 0, 'S'},
				{dst, slot, 'T'},
			} {
				buf := send.ring.RegisteredBuffer(0)[:1]
				buf[0] = send.marker
				sendCtx := uring.PackDirect(uring.IORING_OP_SEND, uring.IOSQE_FIXED_FILE, 0, send.slot.Raw())
				if err := send.ring.SendFD(sendCtx, send.slot, buf); err != nil {
					t.Fatalf("send marker %c through slot %d: %v", send.marker, send.slot, err)
				}
				requireMsgRingResult(t, send.ring, sendCtx.Raw(), 1)
				var got [1]byte
				n, errno := zcall.Recvfrom(uintptr(fds[1]), got[:], zcall.MSG_DONTWAIT, nil, nil)
				if errno != 0 || n != 1 || got[0] != send.marker {
					t.Fatalf("peer read after sending %c: n=%d errno=%d byte=%q", send.marker, n, errno, got)
				}
			}
		})
	}
}

func requireMsgRingResult(t *testing.T, ring *uring.Uring, userData uint64, want int32) int32 {
	t.Helper()
	var cqes [4]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("wait for user_data=%#x: %v", userData, err)
		}
		if n == 0 {
			backoff.Wait()
			continue
		}
		if n != 1 {
			t.Fatalf("received %d CQEs, want one for user_data=%#x", n, userData)
		}
		cqe := cqes[0]
		if cqe.Context().Raw() != userData || cqe.Res != want {
			t.Fatalf("CQE user_data=%#x res=%d, want user_data=%#x res=%d", cqe.Context().Raw(), cqe.Res, userData, want)
		}
		return cqe.Res
	}
	t.Fatalf("timed out waiting for user_data=%#x", userData)
	return 0
}

// TestNAPIRegistration tests NAPI busy polling registration (Linux 6.9+).
// NAPI provides ultra-low latency network packet processing.
func TestNAPIRegistration(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Try to register NAPI with dynamic tracking
	// Registration can still fail when the host or NIC does not support it.
	err = ring.RegisterNAPI(
		100,   // 100µs busy poll timeout
		false, // don't prefer busy poll
		uring.IO_URING_NAPI_TRACKING_DYNAMIC,
	)
	if err != nil {
		// Check for unsupported operation
		if errors.Is(err, uring.ErrNotSupported) {
			t.Skip("NAPI not supported on this host")
		}
		// EINVAL also indicates not supported on this system
		t.Logf("RegisterNAPI: %v (may require host or NIC support)", err)
		t.Skip("NAPI registration failed - may not be supported")
	}

	t.Log("NAPI registered successfully")

	// Unregister
	err = ring.UnregisterNAPI()
	if err != nil {
		t.Fatalf("UnregisterNAPI: %v", err)
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

	inlineCmdCtx := uring.PackDirect(uring.IORING_OP_URING_CMD, 0, 3, 0)
	err = ring.UringCmdInline(inlineCmdCtx, 0, nil)
	if !errors.Is(err, uring.ErrNotSupported) {
		t.Fatalf("UringCmdInline: got %v, want ErrNotSupported", err)
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

func TestAsyncCancel(t *testing.T) {
	ring := newTestRing(t)
	timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 1, 0)
	nop := uring.PackDirect(uring.IORING_OP_NOP, 0, 2, 0)
	cancel := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 3, 0)

	// The timer must stay pending across unrelated completions.
	if err := ring.Timeout(timer, time.Hour, uring.WithCount(0)); err != nil {
		t.Fatal(err)
	}
	if err := ring.Nop(nop); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{nop: 0})

	if err := ring.AsyncCancel(cancel, timer.Raw()); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{timer: -int32(zcall.ECANCELED), cancel: 0})

	if err := ring.AsyncCancel(cancel, timer.Raw()); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{cancel: -int32(zcall.ENOENT)})
}

func TestAsyncCancelExtended(t *testing.T) {
	const canceled = -int32(zcall.ECANCELED)
	for _, name := range []string{"CancelFD", "CancelOpcode", "CancelAny", "CancelAll"} {
		t.Run(name, func(t *testing.T) {
			fd, errno := zcall.Eventfd2(0, zcall.EFD_NONBLOCK|zcall.EFD_CLOEXEC)
			if errno != 0 {
				t.Fatal(zcall.Errno(errno))
			}
			t.Cleanup(func() { zcall.Close(fd) })
			ring := newTestRing(t)
			poll := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 1, int32(fd))
			timer := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 2, 0)
			cancel := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 3, int32(fd))
			if err := ring.PollAdd(poll, uring.EPOLLIN); err != nil {
				t.Fatal(err)
			}
			if err := ring.Timeout(timer, time.Hour, uring.WithCount(0)); err != nil {
				t.Fatal(err)
			}
			if _, err := ring.Wait(nil); err != nil {
				t.Fatal(err)
			}

			var target, remaining uring.SQEContext
			var err error
			switch name {
			case "CancelFD":
				err = ring.AsyncCancelFD(cancel, false)
				target, remaining = poll, timer
			case "CancelOpcode":
				err = ring.AsyncCancelOpcode(cancel, uring.IORING_OP_TIMEOUT, false)
				target, remaining = timer, poll
			case "CancelAny":
				err = ring.AsyncCancelAny(cancel)
			case "CancelAll":
				err = ring.AsyncCancelAll(cancel)
			}
			if err != nil {
				t.Fatal(err)
			}

			if target == 0 {
				requireTerminalResults(t, ring, map[uring.SQEContext]int32{poll: canceled, timer: canceled, cancel: 2})
				return
			}
			requireTerminalResults(t, ring, map[uring.SQEContext]int32{target: canceled, cancel: 0})
			// The unselected request is still pending and cancellable by identity.
			if err := ring.AsyncCancel(cancel, remaining.Raw()); err != nil {
				t.Fatal(err)
			}
			requireTerminalResults(t, ring, map[uring.SQEContext]int32{remaining: canceled, cancel: 0})
		})
	}
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
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
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
	if err := ring.PollRemove(cancelCtx, pollCtx.Raw()); err != nil {
		t.Logf("PollRemove: %v (may already be cancelled)", err)
	}

	t.Log("Poll multishot test complete")
}

// TestPollModes validates the public response for unsupported level poll modes.
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

	// Linux defines the level flag but does not admit it in POLL_ADD.
	t.Run("LevelAPI", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 1, int32(readFD))
		if err := ring.PollAddLevel(ctx, uring.EPOLLIN); !errors.Is(err, uring.ErrNotSupported) {
			t.Fatalf("PollAddLevel: got %v, want %v", err, uring.ErrNotSupported)
		}
	})

	t.Run("MultishotLevelAPI", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 2, int32(readFD))
		if err := ring.PollAddMultishotLevel(ctx, uring.EPOLLIN); !errors.Is(err, uring.ErrNotSupported) {
			t.Fatalf("PollAddMultishotLevel: got %v, want %v", err, uring.ErrNotSupported)
		}
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

func TestPollUpdate(t *testing.T) {
	ring := newTestRing(t)
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)

	old := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 1, int32(fds[0]))
	next := old.WithBufGroup(2)
	changeMask := uring.PackDirect(uring.IORING_OP_POLL_REMOVE, 0, 3, 0)
	changeID := changeMask.WithBufGroup(4)
	removeOld := changeMask.WithBufGroup(5)
	removeNext := changeMask.WithBufGroup(6)

	requireReady := func(want uring.SQEContext, events int32) {
		t.Helper()
		cqe := waitForCQEs(t, ring, 1)[0]
		if cqe.Context() != want || cqe.Res != events || !cqe.HasMore() {
			t.Fatalf("poll CQE context=%#x result=%#x flags=%#x, want context=%#x events=%#x with F_MORE",
				cqe.Context().Raw(), cqe.Res, cqe.Flags, want.Raw(), events)
		}
	}

	if err := ring.PollAddMultishot(old, uring.EPOLLOUT); err != nil {
		t.Fatal(err)
	}
	requireReady(old, uring.EPOLLOUT)

	// The socket is writable but has no data to read until the peer writes.
	if err := ring.PollUpdate(changeMask, old.Raw(), 0, uring.EPOLLIN, uring.IORING_POLL_UPDATE_EVENTS); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{changeMask: 0})
	if err := ring.PollUpdate(changeID, old.Raw(), next.Raw(), 0, uring.IORING_POLL_UPDATE_USER_DATA); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{changeID: 0})

	if n, errno := writeTestFD(fds[1], []byte{1}); n != 1 || errno != 0 {
		t.Fatalf("peer write: n=%d errno=%v", n, zcall.Errno(errno))
	}
	requireReady(next, uring.EPOLLIN)
	var buf [1]byte
	if n, errno := readTestFD(fds[0], buf[:]); n != 1 || errno != 0 {
		t.Fatalf("read: n=%d errno=%v", n, zcall.Errno(errno))
	}

	if err := ring.PollRemove(removeOld, old.Raw()); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{removeOld: -int32(zcall.ENOENT)})
	if err := ring.PollRemove(removeNext, next.Raw()); err != nil {
		t.Fatal(err)
	}
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{removeNext: 0, next: -int32(zcall.ECANCELED)})
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

func TestUringWritevFixedCycle(t *testing.T) {
	ring := newTestRing(t, testTwoRegisteredBuffersOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	if ring.RegisteredBufferCount() < 2 {
		t.Skip("WritevFixed requires two registered buffers")
	}

	f, err := os.CreateTemp("", "uring_writevfixed_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	const bufIndex = 1
	buf := ring.RegisteredBuffer(bufIndex)
	iovs := [][]byte{buf[:5], buf[16:21], buf[32:37]}
	copy(iovs[0], "AAAAA")
	copy(iovs[1], "BBBBB")
	copy(iovs[2], "CCCCC")

	ctx := uring.PackDirect(uring.IORING_OP_WRITEV_FIXED, 0, 0, int32(f.Fd()))
	if err := ring.WritevFixed(ctx, 0, bufIndex, iovs); err != nil {
		t.Fatalf("WritevFixed: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_WRITEV_FIXED, 5*time.Second)
	if !ok {
		t.Fatal("WritevFixed did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("WritevFixed failed: %d", ev.Res)
	}
	if ev.Res != 15 {
		t.Fatalf("WritevFixed result = %d, want 15", ev.Res)
	}

	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got := make([]byte, 15)
	if _, err := f.Read(got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "AAAAABBBBBCCCCC" {
		t.Fatalf("file content = %q, want %q", got, "AAAAABBBBBCCCCC")
	}
}

func TestUringReadvFixedCycle(t *testing.T) {
	ring := newTestRing(t, testTwoRegisteredBuffersOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})

	if ring.RegisteredBufferCount() < 2 {
		t.Skip("ReadvFixed requires two registered buffers")
	}

	f, err := os.CreateTemp("", "uring_readvfixed_*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	const data = "AAAAABBBBBCCCCC"
	if _, err := f.WriteString(data); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	const bufIndex = 1
	buf := ring.RegisteredBuffer(bufIndex)
	iovs := [][]byte{buf[:5], buf[16:21], buf[32:37]}
	ctx := uring.PackDirect(uring.IORING_OP_READV_FIXED, 0, 0, int32(f.Fd()))
	if err := ring.ReadvFixed(ctx, 0, bufIndex, iovs); err != nil {
		t.Fatalf("ReadvFixed: %v", err)
	}

	ev, ok := waitForOp(t, ring, uring.IORING_OP_READV_FIXED, 5*time.Second)
	if !ok {
		t.Fatal("ReadvFixed did not complete")
	}
	if ev.Res < 0 {
		t.Fatalf("ReadvFixed failed: %d", ev.Res)
	}
	if ev.Res != int32(len(data)) {
		t.Fatalf("ReadvFixed result = %d, want %d", ev.Res, len(data))
	}
	if string(iovs[0])+string(iovs[1])+string(iovs[2]) != data {
		t.Fatalf("ReadvFixed data = %q%q%q, want %q", iovs[0], iovs[1], iovs[2], data)
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
	if err := ring.Splice(ctx1, iofd.FD(srcFD), len(testData)); err != nil {
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
	if err := ring.Splice(ctx2, iofd.FD(pipeRead), int(ev1.Res)); err != nil {
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
	if err := ring.Tee(ctx, iofd.FD(srcPipe[0]), len(testData)); err != nil {
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
	var cqes [256]uring.CQEView
	var backoff iox.Backoff
	b.ReportAllocs()
	b.ResetTimer()

	for submitted := 0; submitted < b.N; {
		batch := min(ring.Features.SQEntries, b.N-submitted)
		for range batch {
			if err := ring.Nop(ctx); err != nil {
				b.Fatalf("Nop: %v", err)
			}
		}
		submitted += batch
		// Measure accepted SQ submissions; flush and drain outside the timer.
		b.StopTimer()
		for pending := batch; pending > 0; {
			n, err := ring.Wait(cqes[:])
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				b.Fatalf("Wait: %v", err)
			}
			if n == 0 {
				backoff.Wait()
				continue
			}
			backoff.Reset()
			for _, cqe := range cqes[:n] {
				if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
					b.Fatalf("NOP completion: op=%d result=%d", cqe.Op(), cqe.Res)
				}
			}
			pending -= n
		}
		b.StartTimer()
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

	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := ring.Nop(ctx); err != nil {
			b.Fatalf("Nop: %v", err)
		}

		for {
			n, err := ring.Wait(cqes[:])
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				b.Fatalf("Wait: %v", err)
			}
			if n > 0 {
				backoff.Reset()
				if cqes[0].Op() != uring.IORING_OP_NOP || cqes[0].Res != 0 {
					b.Fatalf("NOP completion: op=%d result=%d", cqes[0].Op(), cqes[0].Res)
				}
				break
			}
			backoff.Wait()
		}
	}
}
