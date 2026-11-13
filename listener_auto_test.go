// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

type automaticListenerHandler struct {
	op      *uring.ListenerOp
	calls   [3]int
	stop    int
	closeAt int
	errorOp uint8
	err     error
}

func (h *automaticListenerHandler) OnSocketCreated(fd iofd.FD) bool {
	h.calls[0]++
	if h.closeAt == 1 {
		h.op.Close()
	}
	return h.stop != 1
}

func (h *automaticListenerHandler) OnBound() bool {
	h.calls[1]++
	if h.closeAt == 2 {
		h.op.Close()
	}
	return h.stop != 2
}

func (h *automaticListenerHandler) OnListening() {
	h.calls[2]++
}

func (h *automaticListenerHandler) OnError(op uint8, err error) {
	h.errorOp, h.err = op, err
}

func nextListenerCQE(t testing.TB, ring *uring.Uring) uring.CQEView {
	t.Helper()
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 1 {
			return cqes[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("listener completion timed out")
		}
		backoff.Wait()
	}
}

func TestListenerAutomatic(t *testing.T) {
	for _, tc := range []struct {
		name    string
		network string
		addr    *net.TCPAddr
	}{
		{"tcp4", "tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}},
		{"tcp6", "tcp6", &net.TCPAddr{IP: net.IPv6loopback}},
		{"tcp4_wildcard", "tcp4", &net.TCPAddr{}},
		{"tcp6_wildcard", "tcp6", &net.TCPAddr{}},
		{"tcp4_nil", "tcp4", nil},
		{"tcp6_nil", "tcp6", nil},
		{"unix", "unix", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := newTestRing(t)
			pool := uring.NewContextPools(1)
			manager := uring.NewListenerManager(ring, pool)
			h := &automaticListenerHandler{}
			var op *uring.ListenerOp
			var err error
			switch tc.network {
			case "tcp4":
				op, err = manager.ListenTCP4(tc.addr, 16, h)
			case "tcp6":
				op, err = manager.ListenTCP6(tc.addr, 16, h)
			case "unix":
				// The long sockaddr uses the pooled slot's typed sidecar root.
				addr := &net.UnixAddr{Name: filepath.Join(t.TempDir(), "listener.sock"), Net: "unix"}
				op, err = manager.ListenUnix(addr, 16, h)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(op.Close)
			if (tc.name == "tcp4_wildcard" || tc.name == "tcp6_wildcard") && tc.addr.IP != nil {
				t.Fatal("Listen modified the caller's IP")
			}
			for _, opcode := range []uint8{uring.IORING_OP_SOCKET, uring.IORING_OP_BIND, uring.IORING_OP_LISTEN} {
				cqe := nextListenerCQE(t, ring)
				if cqe.Op() != opcode {
					t.Fatalf("opcode=%d, want %d", cqe.Op(), opcode)
				}
				runtime.GC()
				if ok, err := op.HandleCQE(cqe); !ok || err != nil {
					t.Fatalf("HandleCQE: handled=%v err=%v", ok, err)
				}
				if h.err != nil {
					t.Fatalf("kernel opcode=%d: %v", h.errorOp, h.err)
				}
			}
			if h.calls != [3]int{1, 1, 1} {
				t.Fatalf("callbacks=%v", h.calls)
			}
			var listening int32
			size := uint32(unsafe.Sizeof(listening))
			if errno := zcall.Getsockopt(uintptr(op.FD()), zcall.SOL_SOCKET, zcall.SO_ACCEPTCONN, unsafe.Pointer(&listening), unsafe.Pointer(&size)); errno != 0 || listening != 1 {
				t.Fatalf("SO_ACCEPTCONN=%d errno=%d", listening, errno)
			}
			var domain int32
			want := int32(uring.AF_LOCAL)
			if tc.network == "tcp4" {
				want = uring.AF_INET
			} else if tc.network == "tcp6" {
				want = uring.AF_INET6
			}
			if errno := zcall.Getsockopt(uintptr(op.FD()), zcall.SOL_SOCKET, zcall.SO_DOMAIN, unsafe.Pointer(&domain), unsafe.Pointer(&size)); errno != 0 || domain != want {
				t.Fatalf("SO_DOMAIN=%d errno=%d, want %d", domain, errno, want)
			}
			fd, ext := op.FD(), op.Ext()
			op.Close()
			assertListenerFDClosed(t, fd)
			if op.Ext() != nil {
				t.Fatal("context retained after Close")
			}
			if returned := pool.Extended(); returned != ext {
				t.Fatal("context not returned to pool")
			} else {
				pool.PutExtended(returned)
			}
		})
	}
}

func TestListenerAutomaticStop(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stop    int
		closeAt int
		stages  int
	}{
		{"socket", 1, 0, 1},
		{"bind", 2, 0, 2},
		{"close_socket", 0, 1, 1},
		{"close_bind", 0, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := newTestRing(t)
			h := &automaticListenerHandler{stop: tc.stop, closeAt: tc.closeAt}
			op, err := uring.NewListenerManager(ring, uring.NewContextPools(1)).ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, 16, h)
			if err != nil {
				t.Fatal(err)
			}
			h.op = op
			t.Cleanup(op.Close)
			for range tc.stages {
				if ok, err := op.HandleCQE(nextListenerCQE(t, ring)); !ok || err != nil {
					t.Fatalf("HandleCQE: handled=%v err=%v", ok, err)
				}
			}
			if h.err != nil || h.calls[0] != 1 || h.calls[1] != tc.stages-1 || h.calls[2] != 0 {
				t.Fatalf("callbacks=%v err=%v", h.calls, h.err)
			}
			if ring.SQAvailable() != int(ring.Features.SQEntries) {
				t.Fatal("successor submitted after stop")
			}
			op.Close()
			if op.Ext() != nil || op.FD() >= 0 {
				t.Fatal("listener resources retained after stop and Close")
			}
		})
	}
}

func TestListenerAutomaticClosePending(t *testing.T) {
	ring := newTestRing(t)
	h := &automaticListenerHandler{}
	op, err := uring.NewListenerManager(ring, uring.NewContextPools(1)).ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, 16, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(op.Close)
	op.Close()
	if op.Ext() == nil {
		t.Fatal("pending context released")
	}
	cqe := nextListenerCQE(t, ring)
	if cqe.Res < 0 {
		t.Fatalf("SOCKET: %d", cqe.Res)
	}
	fd := iofd.FD(cqe.Res)
	if ok, err := op.HandleCQE(cqe); !ok || err != nil {
		t.Fatalf("HandleCQE: handled=%v err=%v", ok, err)
	}
	assertListenerFDClosed(t, fd)
	if h.calls != [3]int{} || h.err != nil || ring.SQAvailable() != int(ring.Features.SQEntries) {
		t.Fatalf("callback or successor after Close: calls=%v err=%v", h.calls, h.err)
	}
	op.Close()
	if op.Ext() != nil {
		t.Fatal("context retained after drain")
	}
}

func TestListenerAutomaticBindError(t *testing.T) {
	var lc net.ListenConfig
	lc.SetMultipathTCP(false)
	occupied, err := lc.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { occupied.Close() })
	ring := newTestRing(t)
	h := &automaticListenerHandler{}
	// Wildcard normalization must preserve the requested port.
	addr := &net.TCPAddr{Port: occupied.Addr().(*net.TCPAddr).Port}
	op, err := uring.NewListenerManager(ring, uring.NewContextPools(1)).ListenTCP4(addr, 16, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(op.Close)
	for range 2 {
		if ok, err := op.HandleCQE(nextListenerCQE(t, ring)); !ok || err != nil {
			t.Fatalf("HandleCQE: handled=%v err=%v", ok, err)
		}
	}
	if h.calls != [3]int{1, 0, 0} || h.errorOp != uring.IORING_OP_BIND || !errors.Is(h.err, uring.ErrAddressInUse) {
		t.Fatalf("calls=%v opcode=%d err=%v", h.calls, h.errorOp, h.err)
	}
	if ring.SQAvailable() != int(ring.Features.SQEntries) {
		t.Fatal("LISTEN submitted after failed BIND")
	}
}

func TestListenerAutomaticRouting(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	manager := uring.NewListenerManager(ring, uring.NewContextPools(2))
	var ops [2]*uring.ListenerOp
	var handlers [2]automaticListenerHandler
	for i := range ops {
		handlers[i].stop = 1
		op, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, 16, &handlers[i])
		if err != nil {
			t.Fatal(err)
		}
		ops[i] = op
		t.Cleanup(op.Close)
	}
	for range ops {
		cqe := nextListenerCQE(t, ring)
		owner := 0
		if cqe.ExtSQE() == ops[1].Ext() {
			owner = 1
		}
		if ok, err := ops[1-owner].HandleCQE(cqe); ok || err != nil {
			t.Fatalf("foreign CQE: handled=%v err=%v", ok, err)
		}
		if ok, err := ops[owner].HandleCQE(cqe); !ok || err != nil {
			t.Fatalf("owned CQE: handled=%v err=%v", ok, err)
		}
	}
	// After setup stops, the same context can carry a non-listener completion.
	ext, fd := ops[0].Ext(), ops[0].FD()
	if err := ring.Nop(uring.PackExtended(ext)); err != nil {
		t.Fatal(err)
	}
	cqe := nextListenerCQE(t, ring)
	if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 || !cqe.Extended() || cqe.ExtSQE() != ext {
		t.Fatalf("unexpected extended NOP completion: %+v", cqe)
	}
	if ok, err := ops[0].HandleCQE(cqe); ok || err != nil {
		t.Fatalf("extended NOP: handled=%v err=%v", ok, err)
	}
	if ops[0].Ext() != ext || ops[0].FD() != fd {
		t.Fatal("extended NOP changed listener ownership")
	}
	if handlers[0].calls != [3]int{1, 0, 0} || handlers[0].err != nil {
		t.Fatalf("extended NOP invoked listener callbacks: calls=%v err=%v", handlers[0].calls, handlers[0].err)
	}
	if err := ring.Nop(uring.ForFD(-1)); err != nil {
		t.Fatal(err)
	}
	cqe = nextListenerCQE(t, ring)
	for _, op := range ops {
		if ok, err := op.HandleCQE(cqe); ok || err != nil {
			t.Fatalf("NOP: handled=%v err=%v", ok, err)
		}
		op.Close()
		if ok, err := op.HandleCQE(cqe); ok || err != nil {
			t.Fatalf("closed listener: handled=%v err=%v", ok, err)
		}
	}
}

func TestListenerAutomaticRetry(t *testing.T) {
	for _, stage := range []int{0, 1} {
		t.Run([]string{"bind", "listen"}[stage], func(t *testing.T) {
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			h := &automaticListenerHandler{}
			op, err := uring.NewListenerManager(ring, uring.NewContextPools(1)).ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, 16, h)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(op.Close)
			for range stage {
				if ok, err := op.HandleCQE(nextListenerCQE(t, ring)); !ok || err != nil {
					t.Fatalf("HandleCQE: handled=%v err=%v", ok, err)
				}
			}
			cqe := nextListenerCQE(t, ring)
			// No SQPOLL: these NOPs fill the SQ until the caller drives Wait.
			for range ring.Features.SQEntries {
				if err := ring.Nop(uring.ForFD(-1)); err != nil {
					t.Fatal(err)
				}
			}
			ext := op.Ext()
			if ok, err := op.HandleCQE(cqe); !ok || !errors.Is(err, iox.ErrWouldBlock) {
				t.Fatalf("full SQ: handled=%v err=%v", ok, err)
			}
			if op.Ext() != ext || op.FD() < 0 || h.err != nil {
				t.Fatalf("lost retry ownership: fd=%d err=%v", op.FD(), h.err)
			}
			for range ring.Features.SQEntries {
				if cqe := nextListenerCQE(t, ring); cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
					t.Fatalf("unexpected NOP completion: %+v", cqe)
				}
			}
			if err := ring.SubmitExtended(uring.PackExtended(ext)); err != nil {
				t.Fatalf("retry: %v", err)
			}
			for range 2 - stage {
				if ok, err := op.HandleCQE(nextListenerCQE(t, ring)); !ok || err != nil {
					t.Fatalf("HandleCQE: handled=%v err=%v", ok, err)
				}
			}
			if h.calls != [3]int{1, 1, 1} || h.err != nil {
				t.Fatalf("callbacks replayed or failed: calls=%v err=%v", h.calls, h.err)
			}
		})
	}
}

func BenchmarkListenerHandleCQE(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		b.Fatal(err)
	}
	mustStartRing(b, ring)
	manager := uring.NewListenerManager(ring, uring.NewContextPools(1))
	h := &automaticListenerHandler{}
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()
	for range b.N {
		op, err := manager.ListenTCP4(addr, 16, h)
		if err != nil {
			b.Fatal(err)
		}
		for range 3 {
			cqe := nextListenerCQE(b, ring)
			b.StartTimer()
			ok, err := op.HandleCQE(cqe)
			b.StopTimer()
			if !ok || err != nil || h.err != nil {
				b.Fatalf("HandleCQE: handled=%v admission=%v kernel=%v", ok, err, h.err)
			}
		}
		op.Close()
	}
}
