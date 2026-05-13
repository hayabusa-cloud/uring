// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"code.hybscloud.com/iox"
)

const testLockedBufferMem = 1 << 18

var benchmarkMultishotHandleCQESink bool

type noopMultishotHandler struct{}

type stopOnProgressHandler struct{}

type recordingMultishotHandler struct {
	stepErrs []error
	stopErrs []error
	stopped  []bool
}

type raceMultishotHandler struct {
	progressSeen   chan struct{}
	releaseStop    <-chan struct{}
	progressCount  atomic.Int32
	stoppedCount   atomic.Int32
	cancelledCount atomic.Int32
	errorCount     atomic.Int32
}

func (noopMultishotHandler) OnMultishotStep(MultishotStep) MultishotAction {
	return MultishotContinue
}

func (noopMultishotHandler) OnMultishotStop(error, bool) {}

func (stopOnProgressHandler) OnMultishotStep(step MultishotStep) MultishotAction {
	if step.Err == nil {
		return MultishotStop
	}
	return MultishotContinue
}

func (stopOnProgressHandler) OnMultishotStop(error, bool) {}

func (h *recordingMultishotHandler) OnMultishotStep(step MultishotStep) MultishotAction {
	h.stepErrs = append(h.stepErrs, step.Err)
	return MultishotContinue
}

func (h *recordingMultishotHandler) OnMultishotStop(err error, cancelled bool) {
	h.stopErrs = append(h.stopErrs, err)
	h.stopped = append(h.stopped, cancelled)
}

func (h *raceMultishotHandler) OnMultishotStep(step MultishotStep) MultishotAction {
	if step.Err == nil {
		h.progressCount.Add(1)
		select {
		case h.progressSeen <- struct{}{}:
		default:
		}
		<-h.releaseStop
		return MultishotStop
	}
	h.errorCount.Add(1)
	return MultishotStop
}

func (h *raceMultishotHandler) OnMultishotStop(err error, cancelled bool) {
	h.stoppedCount.Add(1)
	if cancelled {
		h.cancelledCount.Add(1)
	}
}

func TestAcceptMultishotPreservesFlagsAndResetsSQE(t *testing.T) {
	ring, err := New(func(opt *Options) {
		opt.LockedBufferMem = testLockedBufferMem
		opt.Entries = EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := NewContextPools(1)
	ring.ctxPools = pool

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	ext.SQE.off = 99
	ext.SQE.uflags = 0xdeadbeef
	ext.SQE.bufIndex = 7
	ext.SQE.personality = 3
	ext.SQE.spliceFdIn = 11
	pool.PutExtended(ext)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer lnFile.Close()

	flags := uint8(IOSQE_ASYNC | IOSQE_IO_LINK)
	optionFlags := uint8(IOSQE_IO_DRAIN)
	ioprio := uint16(IORING_ACCEPT_POLL_FIRST)
	sub, err := ring.AcceptMultishot(
		ForFD(int32(lnFile.Fd())).WithFlags(flags),
		noopMultishotHandler{},
		WithFlags(optionFlags),
		WithIOPrio(ioprio),
	)
	if err != nil {
		t.Fatalf("AcceptMultishot: %v", err)
	}

	got := sub.ext.Load()
	if got == nil {
		t.Fatal("subscription lost ExtSQE")
	}
	if got.SQE.opcode != IORING_OP_ACCEPT {
		t.Fatalf("opcode: got %d, want %d", got.SQE.opcode, IORING_OP_ACCEPT)
	}
	if got.SQE.flags != flags|optionFlags {
		t.Fatalf("flags: got %d, want %d", got.SQE.flags, flags|optionFlags)
	}
	if got.SQE.ioprio != ioprio|IORING_ACCEPT_MULTISHOT {
		t.Fatalf("ioprio: got %#x, want %#x", got.SQE.ioprio, ioprio|IORING_ACCEPT_MULTISHOT)
	}
	if got.SQE.uflags != SOCK_NONBLOCK|SOCK_CLOEXEC {
		t.Fatalf("uflags: got %#x, want %#x", got.SQE.uflags, SOCK_NONBLOCK|SOCK_CLOEXEC)
	}
	if got.SQE.off != 0 {
		t.Fatalf("off: got %d, want 0", got.SQE.off)
	}
	if got.SQE.bufIndex != 0 {
		t.Fatalf("bufIndex: got %d, want 0", got.SQE.bufIndex)
	}
	if got.SQE.personality != 0 {
		t.Fatalf("personality: got %d, want 0", got.SQE.personality)
	}
	if got.SQE.spliceFdIn != 0 {
		t.Fatalf("spliceFdIn: got %d, want 0", got.SQE.spliceFdIn)
	}
}

func TestReceiveMultishotPreservesFlagsAndResetsSQE(t *testing.T) {
	ring, err := New(func(opt *Options) {
		opt.LockedBufferMem = testLockedBufferMem
		opt.Entries = EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := NewContextPools(1)
	ring.ctxPools = pool

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	ext.SQE.off = 123
	ext.SQE.uflags = 0xcafebabe
	ext.SQE.bufIndex = 9
	ext.SQE.personality = 5
	ext.SQE.spliceFdIn = 13
	pool.PutExtended(ext)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	clientConn, err := net.DialTimeout("tcp", addr.String(), 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer serverConn.Close()

	serverFile, err := serverConn.(*net.TCPConn).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer serverFile.Close()

	flags := uint8(IOSQE_ASYNC | IOSQE_IO_DRAIN)
	optionFlags := uint8(IOSQE_IO_LINK)
	bufGroup := uint16(17)
	sqeCtx := ForFD(int32(serverFile.Fd())).WithFlags(flags).WithBufGroup(bufGroup)
	ioprio := uint16(IORING_RECVSEND_POLL_FIRST)
	sub, err := ring.ReceiveMultishot(
		sqeCtx,
		noopMultishotHandler{},
		WithFlags(optionFlags),
		WithIOPrio(ioprio),
	)
	if err != nil {
		t.Fatalf("ReceiveMultishot: %v", err)
	}

	got := sub.ext.Load()
	if got == nil {
		t.Fatal("subscription lost ExtSQE")
	}
	if got.SQE.opcode != IORING_OP_RECV {
		t.Fatalf("opcode: got %d, want %d", got.SQE.opcode, IORING_OP_RECV)
	}
	if got.SQE.flags != flags|optionFlags|IOSQE_BUFFER_SELECT {
		t.Fatalf("flags: got %d, want %d", got.SQE.flags, flags|optionFlags|IOSQE_BUFFER_SELECT)
	}
	if got.SQE.ioprio != ioprio|IORING_RECV_MULTISHOT {
		t.Fatalf("ioprio: got %#x, want %#x", got.SQE.ioprio, ioprio|IORING_RECV_MULTISHOT)
	}
	if got.SQE.bufIndex != bufGroup {
		t.Fatalf("bufIndex: got %d, want %d", got.SQE.bufIndex, bufGroup)
	}
	if got.SQE.uflags != 0 {
		t.Fatalf("uflags: got %#x, want 0", got.SQE.uflags)
	}
	if got.SQE.off != 0 {
		t.Fatalf("off: got %d, want 0", got.SQE.off)
	}
	if got.SQE.personality != 0 {
		t.Fatalf("personality: got %d, want 0", got.SQE.personality)
	}
	if got.SQE.spliceFdIn != 0 {
		t.Fatalf("spliceFdIn: got %d, want 0", got.SQE.spliceFdIn)
	}
}

func TestMultishotSubscriptionStateSurvivesRetireExt(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	ring := &Uring{ctxPools: pool}
	sub := &MultishotSubscription{
		ring:    ring,
		handler: noopMultishotHandler{},
	}
	sub.ext.Store(ext)
	sub.state.Store(uint32(SubscriptionActive))

	sub.retireExt()

	if got := sub.State(); got != SubscriptionActive {
		t.Fatalf("State after retireExt: got %v, want %v", got, SubscriptionActive)
	}
	if !sub.Active() {
		t.Fatal("Active after retireExt: got false, want true")
	}

	if !sub.tryCancel() {
		t.Fatal("tryCancel after retireExt: got false, want true")
	}
	if got := sub.State(); got != SubscriptionCancelling {
		t.Fatalf("State after tryCancel: got %v, want %v", got, SubscriptionCancelling)
	}

	sub.markStopped()
	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State after markStopped: got %v, want %v", got, SubscriptionStopped)
	}
}

func TestMultishotSubscriptionSubmitCancelStateTracksEnqueue(t *testing.T) {
	sub := &MultishotSubscription{}
	sub.state.Store(uint32(SubscriptionActive))

	errWant := errFromErrno(uintptr(EBUSY))
	if err := sub.submitCancelUsing(func(uint64) error { return errWant }); err != errWant {
		t.Fatalf("submitCancelUsing(error): got %v, want %v", err, errWant)
	}
	if got := sub.State(); got != SubscriptionActive {
		t.Fatalf("State after failed cancel enqueue: got %v, want %v", got, SubscriptionActive)
	}
	if !sub.cancelSubmit.CompareAndSwap(false, true) {
		t.Fatal("cancel-submit guard remained set after failed enqueue")
	}
	sub.cancelSubmit.Store(false)

	if err := sub.submitCancelUsing(func(uint64) error { return nil }); err != nil {
		t.Fatalf("submitCancelUsing(success): %v", err)
	}
	if got := sub.State(); got != SubscriptionCancelling {
		t.Fatalf("State after successful cancel enqueue: got %v, want %v", got, SubscriptionCancelling)
	}
	if !sub.Active() {
		t.Fatal("Active after successful cancel enqueue: got false, want true")
	}
}

func TestMultishotSubscriptionUnsubscribeMarksSuppressedWhenStopped(t *testing.T) {
	sub := &MultishotSubscription{}
	sub.state.Store(uint32(SubscriptionStopped))

	sub.Unsubscribe()

	if !sub.unsubscribed.Load() {
		t.Fatal("Unsubscribe() did not mark the subscription unsubscribed")
	}
	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State after Unsubscribe() = %v, want %v", got, SubscriptionStopped)
	}
}

func TestMultishotCancellingStillDeliversSteps(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	handler := &recordingMultishotHandler{}
	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: handler,
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionCancelling))

	sub.handleCQE(&ioUringCqe{userData: sub.userData, res: 7, flags: IORING_CQE_F_MORE})

	if got := len(handler.stepErrs); got != 1 {
		t.Fatalf("step callbacks while cancelling: got %d, want 1", got)
	}
	if got := len(handler.stopErrs); got != 0 {
		t.Fatalf("stop callbacks while cancelling: got %d, want 0", got)
	}
	if !sub.Active() {
		t.Fatal("Active while cancelling after progress CQE: got false, want true")
	}
	if got := sub.State(); got != SubscriptionCancelling {
		t.Fatalf("State after progress CQE while cancelling: got %v, want %v", got, SubscriptionCancelling)
	}
	if sub.ext.Load() == nil {
		t.Fatal("ExtSQE retired before terminal CQE")
	}
}

func TestMultishotFinalSuccessSkipsCancelAndStops(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: stopOnProgressHandler{},
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))

	sub.handleCQE(&ioUringCqe{userData: sub.userData, res: 1, flags: 0})

	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State after final CQE: got %v, want %v", got, SubscriptionStopped)
	}
	if sub.cancelSubmit.Load() {
		t.Fatal("cancel-submit guard set after final CQE")
	}
	if sub.ext.Load() != nil {
		t.Fatal("ExtSQE not retired after final CQE")
	}
}

func TestMultishotCancelSubmitRetainsExtUntilSubmitReturns(t *testing.T) {
	pool := NewContextPools(1)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: noopMultishotHandler{},
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))

	err := sub.submitCancelUsing(func(uint64) error {
		sub.finish(nil, false)
		if sub.ext.Load() == nil {
			t.Fatal("ExtSQE retired while cancel submit still used userData")
		}
		if got := pool.Extended(); got != nil {
			t.Fatal("ExtSQE returned to pool while cancel submit still used userData")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("submitCancelUsing: %v", err)
	}
	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State after terminal/cancel race = %v, want %v", got, SubscriptionStopped)
	}
	if sub.ext.Load() != nil {
		t.Fatal("ExtSQE not retired after cancel submit returned")
	}
	if got := pool.Extended(); got == nil {
		t.Fatal("ExtSQE not returned to pool after cancel submit returned")
	}
}

func TestMultishotFinalErrorDeliversErrorThenStopped(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	handler := &recordingMultishotHandler{}
	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: handler,
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))

	sub.handleCQE(&ioUringCqe{userData: sub.userData, res: -int32(EINVAL), flags: 0})

	if got := len(handler.stepErrs); got != 1 {
		t.Fatalf("step callbacks: got %d, want 1", got)
	}
	if got := len(handler.stopErrs); got != 1 {
		t.Fatalf("stop callbacks: got %d, want 1", got)
	}
	if handler.stepErrs[0] == nil {
		t.Fatal("step callback missing Err")
	}
	if handler.stopErrs[0] == nil {
		t.Fatal("stop callback missing terminal Err")
	}
}

func TestMultishotUnsubscribedSuppressesCallbacks(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	handler := &recordingMultishotHandler{}
	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: handler,
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))
	sub.unsubscribed.Store(true)

	sub.handleCQE(&ioUringCqe{userData: sub.userData, res: 1, flags: 0})

	if got := len(handler.stepErrs); got != 0 {
		t.Fatalf("callbacks after unsubscribe: got %d, want 0", got)
	}
	if got := len(handler.stopErrs); got != 0 {
		t.Fatalf("stop callbacks after unsubscribe: got %d, want 0", got)
	}
	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State after unsubscribed final CQE: got %v, want %v", got, SubscriptionStopped)
	}
	if sub.ext.Load() != nil {
		t.Fatal("ExtSQE not retired after unsubscribe cleanup")
	}
}

func TestMultishotHandleCQEClaimsRoute(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	handler := &recordingMultishotHandler{}
	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: handler,
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))
	extAnchors(ext).owner = sub

	progress := CQEView{Res: 11, Flags: IORING_CQE_F_MORE, ctx: PackExtended(ext)}
	if !sub.HandleCQE(progress) {
		t.Fatal("HandleCQE did not claim route progress CQE")
	}
	if got := len(handler.stepErrs); got != 1 {
		t.Fatalf("step callbacks after progress: got %d, want 1", got)
	}
	if got := len(handler.stopErrs); got != 0 {
		t.Fatalf("stop callbacks after progress: got %d, want 0", got)
	}
	if got := sub.State(); got != SubscriptionActive {
		t.Fatalf("State after progress CQE: got %v, want %v", got, SubscriptionActive)
	}

	final := CQEView{Res: 0, Flags: 0, ctx: PackExtended(ext)}
	if !sub.HandleCQE(final) {
		t.Fatal("HandleCQE did not claim route final CQE")
	}
	if got := len(handler.stepErrs); got != 2 {
		t.Fatalf("step callbacks after final: got %d, want 2", got)
	}
	if got := len(handler.stopErrs); got != 1 {
		t.Fatalf("stop callbacks after final: got %d, want 1", got)
	}
	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State after final CQE: got %v, want %v", got, SubscriptionStopped)
	}
	if sub.ext.Load() != nil {
		t.Fatal("ExtSQE not retired after handled final CQE")
	}
}

func BenchmarkMultishotHandleCQE(b *testing.B) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		b.Fatal("pool exhausted")
	}

	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: noopMultishotHandler{},
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))
	extAnchors(ext).owner = sub

	cqe := CQEView{Res: 11, Flags: IORING_CQE_F_MORE, ctx: PackExtended(ext)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkMultishotHandleCQESink = sub.HandleCQE(cqe)
	}
}

func TestMultishotHandleCQERejectsForeignRoute(t *testing.T) {
	pool := NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	foreign := pool.Extended()
	if foreign == nil {
		t.Fatal("pool exhausted for foreign route")
	}

	handler := &recordingMultishotHandler{}
	sub := &MultishotSubscription{
		ring:    &Uring{ctxPools: pool},
		handler: handler,
	}
	sub.ext.Store(ext)
	sub.userData = PackExtended(ext).Raw()
	sub.state.Store(uint32(SubscriptionActive))
	extAnchors(ext).owner = sub

	if sub.HandleCQE(CQEView{Res: 1, Flags: IORING_CQE_F_MORE, ctx: PackDirect(IORING_OP_ACCEPT, 0, 0, 0)}) {
		t.Fatal("HandleCQE claimed direct CQE")
	}
	if sub.HandleCQE(CQEView{Res: 1, Flags: IORING_CQE_F_MORE, ctx: PackExtended(foreign)}) {
		t.Fatal("HandleCQE claimed foreign extended CQE")
	}
	if got := len(handler.stepErrs); got != 0 {
		t.Fatalf("step callbacks for rejected CQEs: got %d, want 0", got)
	}
	if got := sub.State(); got != SubscriptionActive {
		t.Fatalf("State after rejected CQEs: got %v, want %v", got, SubscriptionActive)
	}
}

func TestMultishotCancelRaceTerminalCQE(t *testing.T) {
	ring, err := New(func(opt *Options) {
		opt.LockedBufferMem = testLockedBufferMem
		opt.Entries = EntriesSmall
		opt.NotifySucceed = true
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	clientConn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer serverConn.Close()

	var serverFD int32
	rawConn, err := serverConn.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}
	if err := rawConn.Control(func(fd uintptr) {
		serverFD = int32(fd)
	}); err != nil {
		t.Fatalf("Control: %v", err)
	}

	releaseStop := make(chan struct{})
	handler := &raceMultishotHandler{
		progressSeen: make(chan struct{}, 1),
		releaseStop:  releaseStop,
	}

	sub, err := ring.ReceiveMultishot(ForFD(serverFD).WithBufGroup(0), handler)
	if err != nil {
		t.Logf("ReceiveMultishot returned error (may require buffer ring setup): %v", err)
		close(releaseStop)
		return
	}

	go func() {
		if _, err := clientConn.Write([]byte("race")); err != nil {
			t.Logf("Write: %v", err)
		}
	}()

	released := make(chan struct{})
	go func() {
		select {
		case <-handler.progressSeen:
			clientConn.Close()
			close(releaseStop)
		case <-time.After(2 * time.Second):
			close(releaseStop)
		}
		close(released)
	}()

	cqes := make([]CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, ErrExists) {
			t.Fatalf("Wait: %v", err)
		}

		for i := 0; i < n; i++ {
			view := cqes[i]
			if !view.Extended() {
				continue
			}
			ext := view.ExtSQE()
			ctx := CastUserData[multishotCtx](ext)
			ctx.Fn(ring, &ext.SQE, &ioUringCqe{
				userData: view.ctx.Raw(),
				res:      view.Res,
				flags:    view.Flags,
			})
		}

		if handler.stoppedCount.Load() > 0 && sub.State() == SubscriptionStopped {
			break
		}
		b.Wait()
	}

	<-released

	if got := handler.progressCount.Load(); got == 0 {
		t.Fatal("expected at least one progress callback before terminal CQE")
	}
	if got := sub.State(); got != SubscriptionStopped {
		t.Fatalf("State: got %v, want %v", got, SubscriptionStopped)
	}
	if got := handler.stoppedCount.Load(); got != 1 {
		t.Fatalf("stop callbacks: got %d, want 1", got)
	}
	if got := handler.errorCount.Load(); got > 1 {
		t.Fatalf("error callbacks: got %d, want at most 1 terminal error before stop", got)
	}

	t.Logf("progress=%d stopped=%d cancelled=%d errors=%d state=%v",
		handler.progressCount.Load(),
		handler.stoppedCount.Load(),
		handler.cancelledCount.Load(),
		handler.errorCount.Load(),
		sub.State())
}
