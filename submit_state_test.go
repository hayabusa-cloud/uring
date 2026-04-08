// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iox"
)

func newStartedSharedTestRing(t *testing.T) *Uring {
	t.Helper()

	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.Entries = EntriesNano
		opt.MultiIssuers = true
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	return ring
}

func waitForNopCQE(t *testing.T, ring *Uring) {
	t.Helper()

	cqes := make([]CQEView, 4)
	deadline := time.Now().Add(time.Second)
	backoff := iox.Backoff{}
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if errors.Is(err, iox.ErrWouldBlock) {
			backoff.Wait()
			continue
		}
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			if cqes[i].Op() == IORING_OP_NOP {
				return
			}
		}
		backoff.Wait()
	}
	t.Fatal("NOP completion timeout")
}

func TestSharedSubmitInvalidInputDoesNotPoisonRing(t *testing.T) {
	t.Run("invalid openat pathname", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		ctx := PackDirect(0, 0, 0, AT_FDCWD)
		if err := ring.OpenAt(ctx, "bad\x00path", 0, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("OpenAt error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 1, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid OpenAt: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid OpenAt")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid readvFixed buffer index", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.readvFixed(PackDirect(0, 0, 0, 0), 0, []int{-1}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("readvFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 2, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid readvFixed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid readvFixed")
		}

		waitForNopCQE(t, ring)
	})
}

func TestMultiIssuerSubmitAndWaitCanOverlap(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	const submits = 128
	waitCtx := PackDirect(IORING_OP_NOP, 0, 0x77, 0)

	var submitWG sync.WaitGroup
	submitWG.Add(1)
	submitErr := make(chan error, 1)
	go func() {
		defer submitWG.Done()
		backoff := iox.Backoff{}
		for range submits {
			for {
				err := ring.Nop(waitCtx)
				if errors.Is(err, iox.ErrWouldBlock) {
					backoff.Wait()
					continue
				}
				if err != nil {
					submitErr <- err
					return
				}
				break
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	got := 0
	cqes := make([]CQEView, 8)
	backoff := iox.Backoff{}
	for got < submits && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if errors.Is(err, iox.ErrWouldBlock) {
			select {
			case err := <-submitErr:
				t.Fatalf("submit goroutine: %v", err)
			default:
			}
			backoff.Wait()
			continue
		}
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		got += n
	}

	submitWG.Wait()
	select {
	case err := <-submitErr:
		t.Fatalf("submit goroutine: %v", err)
	default:
	}
	if got != submits {
		t.Fatalf("completed CQEs = %d, want %d", got, submits)
	}
}

func TestSubmitKeepAliveRetainsBindAndXattrRoots(t *testing.T) {
	t.Run("bind retains sockaddr root", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
		sa := AddrToSockaddr(addr)
		rawPtr, _, err := sockaddr(sa)
		if err != nil {
			t.Fatalf("sockaddr: %v", err)
		}
		if err := ring.ioUring.bind(PackDirect(0, 0, 0, 0), sa); err != nil {
			t.Fatalf("bind submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		kept := ring.ioUring.submit.keepAlive[idx].sockaddrArg
		if kept == nil {
			t.Fatal("bind keepAlive sockaddrArg = nil")
		}
		keptPtr, _, err := sockaddr(kept)
		if err != nil {
			t.Fatalf("sockaddr kept: %v", err)
		}
		if keptPtr != rawPtr {
			t.Fatal("bind keepAlive did not retain the submitted sockaddr root")
		}
	})

	t.Run("xattr retains value root", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		value := []byte("hello-xattr")
		if err := ring.ioUring.fsetxattr(PackDirect(0, 0, 0, 0), "user.test", value, 0); err != nil {
			t.Fatalf("fsetxattr submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		kept := ring.ioUring.submit.keepAlive[idx].bytes1
		if len(kept) != len(value) {
			t.Fatalf("xattr keepAlive len = %d, want %d", len(kept), len(value))
		}
		if unsafe.SliceData(kept) != unsafe.SliceData(value) {
			t.Fatal("xattr keepAlive did not retain the caller value root")
		}
	})

	t.Run("uringCmd keeps original command buffer visible", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		cmdData := []byte("uring-cmd")
		cmdCtx := PackDirect(0, IOSQE_IO_LINK, 0, 17)
		if err := ring.ioUring.uringCmd(cmdCtx, 99, cmdData); err != nil {
			t.Fatalf("uringCmd submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		if got, want := sqe.addr, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(cmdData)))); got != want {
			t.Fatalf("uringCmd sqe.addr = %#x, want %#x", got, want)
		}

		keep := &ring.ioUring.submit.keepAlive[idx]
		if !keep.active {
			t.Fatal("uringCmd keepAlive not active")
		}
		kept := keep.bytes1
		if len(kept) != len(cmdData) {
			t.Fatalf("uringCmd keepAlive len = %d, want %d", len(kept), len(cmdData))
		}
		if unsafe.SliceData(kept) != unsafe.SliceData(cmdData) {
			t.Fatal("uringCmd keepAlive did not retain the original caller buffer")
		}
	})

	t.Run("uringCmd128 stages inline command bytes", func(t *testing.T) {
		ring, err := New(testMinimalBufferOptions, func(opt *Options) {
			opt.Entries = EntriesNano
			opt.MultiIssuers = true
			opt.NotifySucceed = true
			opt.SQE128 = true
		})
		if errors.Is(err, ErrNotSupported) {
			t.Skip("SQE128 ring setup not supported on this kernel")
		}
		if err != nil {
			t.Fatalf("New SQE128 ring: %v", err)
		}
		mustStartRing(t, ring)

		cmdData := []byte("wide-inline-command")
		cmdCtx := PackDirect(0, IOSQE_IO_LINK, 0, 17)
		if err := ring.ioUring.uringCmd128(cmdCtx, 123, cmdData); errors.Is(err, ErrNotSupported) {
			t.Skip("URING_CMD128 opcode not supported on this kernel")
		} else if err != nil {
			t.Fatalf("uringCmd128 submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		sqe128 := ring.ioUring.sq.sqe128At(idx)
		if sqe128 == nil {
			t.Fatal("wide ring did not expose 128-byte SQE view")
		}
		if sqe.opcode != IORING_OP_URING_CMD128 {
			t.Fatalf("uringCmd128 opcode = %d, want %d", sqe.opcode, IORING_OP_URING_CMD128)
		}
		if sqe.off != 123 {
			t.Fatalf("uringCmd128 off = %d, want %d", sqe.off, 123)
		}
		inline := unsafe.Slice((*byte)(unsafe.Pointer(&sqe128.pad[0])), len(cmdData))
		if got := string(inline); got != string(cmdData) {
			t.Fatalf("uringCmd128 inline data = %q, want %q", got, string(cmdData))
		}
		if keep := &ring.ioUring.submit.keepAlive[idx]; keep.active {
			t.Fatal("uringCmd128 should not retain external command data")
		}
	})
}

func TestSubmitKeepAliveRetainsOpenHowAndStatxRoots(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	t.Run("openat2 retains OpenHow root", func(t *testing.T) {
		how := &OpenHow{}
		if err := ring.ioUring.openAt2(PackDirect(0, 0, 0, AT_FDCWD), "openat2-root", how); err != nil {
			t.Fatalf("openAt2 submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		ext := ring.ioUring.submit.keepAlive[idx].ext
		if ext == nil {
			t.Fatal("openAt2 keepAlive ext = nil")
		}
		if ext.openHow != how {
			t.Fatal("openAt2 keepAlive did not retain the submitted OpenHow root")
		}
	})

	t.Run("statx retains Statx root", func(t *testing.T) {
		stat := &Statx{}
		if err := ring.ioUring.statx(PackDirect(0, 0, 0, AT_FDCWD), "statx-root", 0, 0, stat); err != nil {
			t.Fatalf("statx submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		ext := ring.ioUring.submit.keepAlive[idx].ext
		if ext == nil {
			t.Fatal("statx keepAlive ext = nil")
		}
		if ext.statx != stat {
			t.Fatal("statx keepAlive did not retain the submitted Statx root")
		}
	})
}

func TestOpenAtStagesLongPaths(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	path := strings.Repeat("a", submitKeepAlivePathInline+64)

	if err := ring.ioUring.openAt(PackDirect(0, 0, 0, AT_FDCWD), path, 0, 0); err != nil {
		t.Fatalf("openAt submit: %v", err)
	}

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	keep := &ring.ioUring.submit.keepAlive[idx]
	if keep.ext == nil {
		t.Fatal("long-path submit did not allocate ext staging")
	}
	if len(keep.ext.path1Buf) != len(path)+1 {
		t.Fatalf("long-path buffer len = %d, want %d", len(keep.ext.path1Buf), len(path)+1)
	}
	if keep.ext.path1Buf[len(path)] != 0 {
		t.Fatal("long-path buffer is not NUL terminated")
	}
	if got := string(keep.ext.path1Buf[:len(path)]); got != path {
		t.Fatal("long-path buffer contents do not match submitted pathname")
	}
}

func TestWritePathRejectsPathsOverPathMax(t *testing.T) {
	ext := &submitKeepAliveExt{}
	path := strings.Repeat("a", submitKeepAlivePathMax)
	if _, err := ext.writePath1(path); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("writePath1 error = %v, want %v", err, ErrNameTooLong)
	}
}

func TestEpollWaitRejectsTimeoutAndStagesZeroOff(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	events := make([]EpollEvent, 1)
	ctx := PackDirect(0, 0, 0, 0)

	if err := ring.ioUring.epollWait(ctx, &events[0], len(events), 1); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("epollWait timeout error = %v, want %v", err, ErrInvalidParam)
	}

	if err := ring.ioUring.epollWait(ctx, &events[0], len(events), 0); err != nil {
		t.Fatalf("epollWait submit: %v", err)
	}

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	sqe := ring.ioUring.sq.sqeAt(idx)
	if sqe.off != 0 {
		t.Fatalf("epollWait sqe.off = %d, want 0", sqe.off)
	}
	if sqe.uflags != 0 {
		t.Fatalf("epollWait sqe.uflags = %d, want 0", sqe.uflags)
	}
	if sqe.bufIndex != 0 || sqe.spliceFdIn != 0 {
		t.Fatalf("epollWait reused stale fields: bufIndex=%d spliceFdIn=%d", sqe.bufIndex, sqe.spliceFdIn)
	}
}

func TestCloseAfterFatalResizeClosesRingState(t *testing.T) {
	cause := errors.New("remap failed")
	ur := &ioUring{
		submit: submitState{
			keepAlive: []submitKeepAlive{{active: true, bytes1: []byte("x")}},
		},
		ringFd:   -1,
		pollerFd: -1,
		bufs:     [][]byte{{1}},
		files:    []int32{1},
		ops:      []ioUringProbeOp{{op: IORING_OP_NOP}},
		sq: ioUringSq{
			sqeStride: unsafe.Sizeof(ioUringSqe{}),
			sqeMapSz:  unsafe.Sizeof(ioUringSqe{}),
		},
	}

	err := ur.closeAfterFatalResize(cause)
	if !errors.Is(err, cause) {
		t.Fatalf("closeAfterFatalResize error = %v, want wrapped cause", err)
	}
	if !ur.closed.Load() {
		t.Fatal("closeAfterFatalResize did not mark ring closed")
	}
	if ur.submit.keepAlive != nil {
		t.Fatal("closeAfterFatalResize did not clear keepAlive state")
	}
	if ur.bufs != nil || ur.files != nil || ur.ops != nil {
		t.Fatal("closeAfterFatalResize did not clear userspace state")
	}
	if ur.sq.sqesPtr != nil || ur.sq.sqeStride != 0 || ur.sq.sqeMapSz != 0 || ur.sq.kHead != nil || ur.cq.cqes != nil {
		t.Fatal("closeAfterFatalResize did not unmap ring views")
	}
	if err := ur.enter(); !errors.Is(err, ErrClosed) {
		t.Fatalf("enter after fatal resize = %v, want %v", err, ErrClosed)
	}
}

func TestClearRingViewsClearsStaleMappings(t *testing.T) {
	ur := &ioUring{
		sq: ioUringSq{
			ringPtr:   unsafe.Pointer(new(byte)),
			sqesPtr:   unsafe.Pointer(new(ioUringSqe)),
			ringSz:    128,
			sqeStride: unsafe.Sizeof(ioUringSqe{}),
			sqeMapSz:  2 * unsafe.Sizeof(ioUringSqe{}),
			kHead:     new(uint32),
			kTail:     new(uint32),
		},
		cq: ioUringCq{
			kHead: new(uint32),
			cqes:  make([]ioUringCqe, 2),
		},
	}

	ur.clearRingViews()

	if ur.sq.ringPtr != nil || ur.sq.sqesPtr != nil || ur.sq.ringSz != 0 || ur.sq.sqeStride != 0 || ur.sq.sqeMapSz != 0 || ur.sq.kHead != nil || ur.sq.kTail != nil {
		t.Fatal("clearRingViews did not clear sq state")
	}
	if ur.cq.kHead != nil || ur.cq.kTail != nil || ur.cq.cqes != nil || ur.cq.ringSz != 0 {
		t.Fatal("clearRingViews did not clear cq state")
	}

	ring := &Uring{ioUring: ur}
	if got := ring.SQAvailable(); got != 0 {
		t.Fatalf("SQAvailable after clearRingViews = %d, want 0", got)
	}
	if got := ring.CQPending(); got != 0 {
		t.Fatalf("CQPending after clearRingViews = %d, want 0", got)
	}
	if err := ur.enter(); !errors.Is(err, ErrClosed) {
		t.Fatalf("enter after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.wait(); !errors.Is(err, ErrClosed) {
		t.Fatalf("wait after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.waitBatch(make([]CQEView, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("waitBatch after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.waitBatchDirect(make([]DirectCQE, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("waitBatchDirect after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.waitBatchExtended(make([]ExtCQE, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("waitBatchExtended after clearRingViews = %v, want %v", err, ErrClosed)
	}
}

func TestSubmitKeepAliveClearReturnsExtAndClearsRoots(t *testing.T) {
	ur := &ioUring{}
	keep := &submitKeepAlive{}

	ctrl := byte(1)
	value := []byte("hello")
	keep.retainBytes(value)
	keep.iovs = keep.iovInline[:1]
	keep.iovInline[0] = IoVec{Base: &ctrl, Len: 1}
	ext := keep.ensureExt(ur)
	ext.msg = Msghdr{Control: &ctrl, Controllen: 1}
	ext.openHow = &OpenHow{}
	ext.statx = &Statx{}
	ext.path1Buf = make([]byte, 300)

	keep.clear(ur)

	if keep.active {
		t.Fatal("keepAlive remained active after clear")
	}
	if keep.bytes1 != nil {
		t.Fatal("keepAlive bytes1 was not cleared")
	}
	if keep.iovs != nil {
		t.Fatal("keepAlive iovs was not cleared")
	}
	if keep.sockaddrArg != nil {
		t.Fatal("keepAlive sockaddrArg was not cleared")
	}
	if keep.ext != nil {
		t.Fatal("keepAlive ext was not released")
	}
	if keep.iovInline[0].Base != nil {
		t.Fatal("keepAlive iovInline root was not cleared")
	}

	reused := ur.acquireSubmitKeepAliveExt()
	if reused != ext {
		t.Fatal("keepAlive ext was not returned to the free list")
	}
	if reused.msg != (Msghdr{}) {
		t.Fatal("pooled ext msg was not cleared")
	}
	if reused.openHow != nil {
		t.Fatal("pooled ext OpenHow root was not cleared")
	}
	if reused.statx != nil {
		t.Fatal("pooled ext Statx root was not cleared")
	}
	if cap(reused.path1Buf) < 300 {
		t.Fatal("pooled ext lost its reusable long-path buffer")
	}
}

func TestUringStopClosesUnstartedRing(t *testing.T) {
	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.Entries = EntriesNano
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := ring.RingFD(); got < 0 {
		t.Fatalf("RingFD before Stop = %d, want open ring fd", got)
	}

	if err := ring.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ring.RingFD(); got != -1 {
		t.Fatalf("RingFD after Stop = %d, want -1", got)
	}
	if err := ring.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := ring.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Stop = %v, want %v", err, ErrClosed)
	}
}

func TestUringStopReleasesTrackedResources(t *testing.T) {
	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.Entries = EntriesNano
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if len(ring.bufferRings.gids) == 0 {
		t.Fatal("bufferRings.gids is empty after Start")
	}
	if _, err := ring.RegisterBufRingWithFlags(16, 0x7ffe, 0); err != nil {
		t.Fatalf("RegisterBufRingWithFlags: %v", err)
	}
	if len(ring.registeredBufRings) != 1 {
		t.Fatalf("registeredBufRings len = %d, want 1", len(ring.registeredBufRings))
	}

	tmp, err := os.CreateTemp("", "uring-stop-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if err := ring.RegisterFiles([]int32{int32(tmp.Fd())}); err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	if got := ring.RegisteredFileCount(); got != 1 {
		t.Fatalf("RegisteredFileCount before Stop = %d, want 1", got)
	}

	if err := ring.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ring.RingFD(); got != -1 {
		t.Fatalf("RingFD after Stop = %d, want -1", got)
	}
	if got := ring.RegisteredBufferCount(); got != 0 {
		t.Fatalf("RegisteredBufferCount after Stop = %d, want 0", got)
	}
	if got := ring.RegisteredFileCount(); got != 0 {
		t.Fatalf("RegisteredFileCount after Stop = %d, want 0", got)
	}
	if ring.submit.keepAlive != nil {
		t.Fatal("submit.keepAlive not cleared by Stop")
	}
	if ring.submit.keepAliveExtFree != nil {
		t.Fatal("submit.keepAliveExtFree not cleared by Stop")
	}
	if len(ring.bufferRings.gids) != 0 {
		t.Fatalf("bufferRings.gids len after Stop = %d, want 0", len(ring.bufferRings.gids))
	}
	if len(ring.registeredBufRings) != 0 {
		t.Fatalf("registeredBufRings len after Stop = %d, want 0", len(ring.registeredBufRings))
	}
	if err := ring.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := ring.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Stop = %v, want %v", err, ErrClosed)
	}
}

func TestSendZeroCopyFixedStagesBufIndexAndOffset(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	if ring.RegisteredBufferCount() < 2 {
		t.Skip("need at least two registered buffers")
	}

	const (
		bufIndex = 1
		offset   = 32
		length   = 128
	)
	buf := ring.ioUring.bufs[bufIndex]
	base := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf))))

	if err := ring.ioUring.sendZeroCopyFixed(PackDirect(0, 0, 0, 1), bufIndex, offset, length, 0); err != nil {
		t.Fatalf("sendZeroCopyFixed submit: %v", err)
	}

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	sqe := ring.ioUring.sq.sqeAt(idx)

	if sqe.opcode != IORING_OP_SEND_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND_ZC)
	}
	if sqe.ioprio != IORING_RECVSEND_FIXED_BUF {
		t.Fatalf("ioprio = %d, want %d", sqe.ioprio, IORING_RECVSEND_FIXED_BUF)
	}
	if sqe.bufIndex != bufIndex {
		t.Fatalf("bufIndex = %d, want %d", sqe.bufIndex, bufIndex)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.addr != base+offset {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, base+offset)
	}
	if sqe.len != length {
		t.Fatalf("len = %d, want %d", sqe.len, length)
	}
}
