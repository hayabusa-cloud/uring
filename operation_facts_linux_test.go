// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"slices"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func TestBundleFollowsRecycledBufferOrder(t *testing.T) {
	const size, entries = 64, 4
	ring := newTestRing(t, func(o *uring.Options) {
		o.ReadBufferSize = size
		o.ReadBufferNum = entries
	})
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	var held [entries]uring.BundleIterator
	for i := range held {
		if n, errno := writeTestFD(fds[1], []byte{byte(i)}); errno != 0 || n != 1 {
			t.Fatalf("seed write: n=%d errno=%d", n, errno)
		}
		if err := ring.ReceiveFD(uring.ForFD(0), fds[0], nil, uring.WithReadBufferSize(1)); err != nil {
			t.Fatal(err)
		}
		cqe, ok := waitForOp(t, ring, uring.IORING_OP_RECV, 2*time.Second)
		if !ok || cqe.Res != 1 || !cqe.HasBuffer() {
			t.Fatalf("seed completion: present=%v cqe=%+v", ok, cqe)
		}
		held[i], ok = ring.BundleIterator(cqe, cqe.BufGroup())
		if !ok {
			t.Fatal("seed iterator unavailable")
		}
	}
	order := [...]int{2, 0, 3, 1}
	for _, i := range order {
		if err := held[i].Recycle(ring); err != nil {
			t.Fatal(err)
		}
	}
	payload := make([]byte, 3*size+13)
	for i := range payload {
		payload[i] = byte(i/size + 40)
	}
	if n, errno := writeTestFD(fds[1], payload); errno != 0 || n != len(payload) {
		t.Fatalf("bundle write: n=%d errno=%d", n, errno)
	}
	seen, buffers, largest := 0, 0, 0
	for seen < len(payload) {
		if err := ring.ReceiveBundle(uring.ForFD(0), testPollFd{fd: fds[0]}); err != nil {
			t.Fatal(err)
		}
		cqe, ok := waitForOp(t, ring, uring.IORING_OP_RECV, 2*time.Second)
		if !ok || cqe.Res <= 0 || !cqe.HasBuffer() {
			t.Fatalf("bundle completion: present=%v cqe=%+v", ok, cqe)
		}
		it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
		if !ok {
			t.Fatal("bundle iterator unavailable")
		}
		largest = max(largest, it.Count())
		for id, buf := range it.AllWithSlotID() {
			wantID := held[order[buffers]].SlotID(0)
			if id != wantID {
				t.Errorf("buffer %d ID=%d, want published ID %d", buffers, id, wantID)
			}
			if !bytes.Equal(buf, payload[seen:seen+len(buf)]) {
				t.Errorf("buffer %d contains the wrong payload", buffers)
			}
			seen += len(buf)
			buffers++
		}
	}
	if largest < 2 {
		t.Fatal("kernel did not produce a multi-buffer bundle")
	}
}

// Both ends use plain TCP so the kernel can select its TCP zero-copy path.
func newTestTCPPair(t *testing.T) (iofd.FD, net.Conn) {
	t.Helper()
	var listenConfig net.ListenConfig
	listenConfig.SetMultipathTCP(false)
	listener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dialer := net.Dialer{Timeout: 2 * time.Second}
	dialer.SetMultipathTCP(false)
	peer, err := dialer.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	sender, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	file, err := sender.(*net.TCPConn).File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return iofd.FD(file.Fd()), peer
}

func TestZCTrackerDefaultNotifications(t *testing.T) {
	const pollAndReport = uring.IORING_RECVSEND_POLL_FIRST | uring.IORING_SEND_ZC_REPORT_USAGE
	for _, tc := range []struct {
		name     string
		slot     int // -1 selects the caller's slice
		subrange bool
		ioprio   uint16
	}{
		{"user", -1, false, 0},
		{"registered", 0, false, 0},
		{"user-range", -1, true, pollAndReport},
		{"registered-range", 1, true, pollAndReport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte("zero-copy completion is required")
			var pin runtime.Pinner
			pin.Pin(&payload[0])
			t.Cleanup(pin.Unpin)

			// Two registered slots, with the default NotifySucceed=false.
			ring := newTestRing(t, func(o *uring.Options) { o.LockedBufferMem = 2 * uring.BufferSizeLarge })
			fd, client := newTestTCPPair(t)
			tracker := uring.NewZCTracker(ring, uring.NewContextPools(2))
			handler := &testZCHandler{}
			offset, length := 0, len(payload)
			want := payload
			var opts []uring.OpOptionFunc
			var wantIndex uint16
			wantPrio := tc.ioprio
			if tc.subrange {
				offset, length = 3, 12
				opts = []uring.OpOptionFunc{uring.WithOffset(2), uring.WithN(7), uring.WithIOPrio(tc.ioprio)}
				want = payload[offset+2 : offset+2+7]
			}
			var err error
			if tc.slot >= 0 {
				wantIndex = uint16(tc.slot)
				wantPrio |= uring.IORING_RECVSEND_FIXED_BUF
				copy(ring.RegisteredBuffer(tc.slot), payload)
				err = tracker.SendZCFixed(fd, tc.slot, offset, length, handler, opts...)
			} else {
				err = tracker.SendZC(fd, payload[offset:offset+length], handler, opts...)
			}
			if err != nil {
				t.Fatal(err)
			}
			var cqes [4]uring.CQEView
			deadline := time.Now().Add(2 * time.Second)
			var backoff iox.Backoff
			for handler.notifyCalls.Load() == 0 && time.Now().Before(deadline) {
				n, err := ring.Wait(cqes[:])
				if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
					t.Fatal(err)
				}
				for _, cqe := range cqes[:n] {
					sqe := cqe.SQE()
					if sqe.IoPrio() != wantPrio || sqe.BufIndex() != wantIndex || sqe.Len() != uint32(len(want)) {
						t.Fatalf("send SQE: ioprio=%#x index=%d length=%d", sqe.IoPrio(), sqe.BufIndex(), sqe.Len())
					}
					if !tracker.HandleCQE(cqe) {
						t.Fatalf("unhandled completion: %+v", cqe)
					}
				}
				if n > 0 {
					backoff.Reset()
				} else {
					backoff.Wait()
				}
			}
			if handler.completedCalls.Load() != 1 || handler.notifyCalls.Load() != 1 {
				t.Fatalf("callbacks: completed=%d released=%d", handler.completedCalls.Load(), handler.notifyCalls.Load())
			}
			if got := handler.completedRes.Load(); got != int32(len(want)) || handler.notifyRes.Load() != got {
				t.Fatalf("send=%d release=%d, want %d", got, handler.notifyRes.Load(), len(want))
			}
			if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			received := make([]byte, len(want))
			if _, err := io.ReadFull(client, received); err != nil || !bytes.Equal(received, want) {
				t.Fatalf("receive: data=%q error=%v", received, err)
			}
		})
	}
}

func TestZCTrackerFullSQReturnsContext(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) {
		o.Entries = uring.EntriesPico
		o.NotifySucceed = true
	})
	for range uring.EntriesPico {
		if err := ring.Nop(uring.ForFD(0)); err != nil {
			t.Fatal(err)
		}
	}

	pool := uring.NewContextPools(1)
	tracker := uring.NewZCTracker(ring, pool)
	buf := ring.RegisteredBuffer(0)
	if err := tracker.SendZC(-1, buf[:1], nil); !iox.IsWouldBlock(err) || pool.ExtendedAvailable() != 1 {
		t.Fatalf("SendZC on full SQ: err=%v available=%d", err, pool.ExtendedAvailable())
	}
	if err := tracker.SendZCFixed(-1, 0, 0, 1, nil); !iox.IsWouldBlock(err) || pool.ExtendedAvailable() != 1 {
		t.Fatalf("SendZCFixed on full SQ: err=%v available=%d", err, pool.ExtendedAvailable())
	}
	for _, cqe := range waitForCQEs(t, ring, uring.EntriesPico) {
		if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
			t.Fatalf("unexpected completion: %+v", cqe)
		}
	}
}

type operationTargets struct{ fds []iofd.FD }

func (t *operationTargets) Count() int       { return len(t.fds) }
func (t *operationTargets) FD(i int) iofd.FD { return t.fds[i] }

type multicastPolicyTargets int

func (t multicastPolicyTargets) Count() int     { return int(t) }
func (t multicastPolicyTargets) FD(int) iofd.FD { return -1 }

// Leave one SQ slot available so target-count thresholds can be exercised
// without thousands of sockets. The real kernel reports the selected opcode
// with EBADF; accepted progress remains one target even at large fanout.
func TestMulticastAutomaticSelection(t *testing.T) {
	for _, policy := range []struct {
		name       string
		bufIndex   int
		aggressive bool
		tiers      [][2]int
	}{
		{"Multicast", 0, false, [][2]int{{1, 8192}, {7, 8192}, {8, 4096}, {63, 4096}, {64, 3072}, {511, 3072}, {512, 2048}, {4095, 2048}, {4096, 1536}}},
		{"MulticastZeroCopy", 0, true, [][2]int{{1, 1536}, {3, 1536}, {4, 1024}, {15, 1024}, {16, 512}, {63, 512}, {64, 128}, {255, 128}, {256, 0}}},
		{"Unregistered", -1, false, [][2]int{{1, 8192}, {4096, 1536}}},
	} {
		for _, tier := range policy.tiers {
			for _, size := range []int{tier[1] - 1, tier[1]} {
				if size < 0 {
					continue
				}
				t.Run(fmt.Sprintf("%s/targets=%d/bytes=%d", policy.name, tier[0], size), func(t *testing.T) {
					ring := newTestRing(t, func(o *uring.Options) { o.Entries = uring.EntriesPico })
					for range uring.EntriesPico - 1 {
						if err := ring.Nop(uring.ForFD(0)); err != nil {
							t.Fatal(err)
						}
					}
					targets := multicastPolicyTargets(tier[0])
					var accepted int
					var err error
					if policy.aggressive {
						accepted, err = ring.MulticastZeroCopy(uring.ForFD(0), targets, 0, 0, size)
					} else {
						accepted, err = ring.Multicast(uring.ForFD(0), targets, policy.bufIndex, ring.RegisteredBuffer(0), 0, size)
					}
					if accepted != 1 || (targets.Count() == 1 && err != nil) || (targets.Count() > 1 && !errors.Is(err, iox.ErrWouldBlock)) {
						t.Fatalf("accepted=%d error=%v", accepted, err)
					}
					wantOp := uint8(uring.IORING_OP_SEND)
					if policy.bufIndex >= 0 && size >= tier[1] {
						wantOp = uring.IORING_OP_SEND_ZC
					}
					var cqes [uring.EntriesPico * 2]uring.CQEView
					nops, data, released := 0, false, false
					var backoff iox.Backoff
					deadline := time.Now().Add(2 * time.Second)
					for (nops < uring.EntriesPico-1 || !data || !released) && time.Now().Before(deadline) {
						n, err := ring.Wait(cqes[:])
						if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
							t.Fatal(err)
						}
						if n == 0 {
							backoff.Wait()
							continue
						}
						backoff.Reset()
						for _, cqe := range cqes[:n] {
							if cqe.Op() == uring.IORING_OP_NOP {
								nops++
								continue
							}
							if cqe.Op() != wantOp || cqe.FD() != -1 {
								t.Fatalf("completion=%+v, want opcode=%d and FD=-1", cqe, wantOp)
							}
							if cqe.IsNotification() {
								released = true
							} else {
								if data || cqe.Res != -int32(zcall.EBADF) {
									t.Fatalf("data=%v result=%d, want one EBADF completion", data, cqe.Res)
								}
								data = true
								released = released || !cqe.HasMore()
							}
						}
					}
					if nops != uring.EntriesPico-1 || !data || !released {
						t.Fatalf("undrained work: nops=%d data=%v released=%v", nops, data, released)
					}
				})
			}
		}
	}
}

func TestMulticastAcceptedPrefix(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) {
		o.Entries = uring.EntriesPico
		o.NotifySucceed = true
	})
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	for range uring.EntriesPico - 1 {
		if err := ring.Nop(uring.ForFD(0)); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("one")
	targets := &operationTargets{fds: []iofd.FD{fds[0], fds[0]}}
	n, err := ring.Multicast(uring.ForFD(0), targets, -1, payload, 0, len(payload))
	if n != 1 || !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("accepted=%d error=%v, want 1 and ErrWouldBlock", n, err)
	}
	cqe, ok := waitForOp(t, ring, uring.IORING_OP_SEND, 2*time.Second)
	if !ok || cqe.Res != int32(len(payload)) {
		t.Fatalf("first send: %+v present=%v", cqe, ok)
	}
	remaining := &operationTargets{fds: targets.fds[n:]}
	n, err = ring.Multicast(uring.ForFD(0), remaining, -1, payload, 0, len(payload))
	if n != 1 || err != nil {
		t.Fatalf("resumed accepted=%d error=%v", n, err)
	}
	cqe, ok = waitForOp(t, ring, uring.IORING_OP_SEND, 2*time.Second)
	if !ok || cqe.Res != int32(len(payload)) {
		t.Fatalf("resumed send: %+v present=%v", cqe, ok)
	}
	var got [6]byte
	read := 0
	for read < len(got) {
		n, errno := readTestFD(fds[1], got[read:])
		if n <= 0 || errno != 0 {
			t.Fatalf("read n=%d errno=%d", n, errno)
		}
		read += n
	}
	if string(got[:]) != "oneone" {
		t.Fatalf("received %q", got)
	}
}

func TestMulticastZeroCopyRetryChangesOpcode(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) {
		o.Entries = uring.EntriesPico
		o.NotifySucceed = true // Observe ordinary sends used to finish short writes.
	})
	var fds [4]iofd.FD
	var peers [len(fds)]net.Conn
	var sent [len(fds)]int
	for i := range fds {
		fds[i], peers[i] = newTestTCPPair(t)
		sent[i] = -1 // No data CQE for this target yet.
	}
	var payload [1024]byte
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	buf := ring.RegisteredBuffer(0)[:len(payload)]
	copy(buf, payload[:])

	const padding = uring.EntriesPico - 1
	ctx := uring.ForFD(0)
	for range padding {
		if err := ring.Nop(ctx); err != nil {
			t.Fatal(err)
		}
	}
	accepted, err := ring.MulticastZeroCopy(ctx, &operationTargets{fds: fds[:]}, 0, 0, len(buf))
	if accepted != 1 || !iox.IsWouldBlock(err) {
		t.Fatalf("first call accepted %d targets, error %v", accepted, err)
	}

	nopsLeft, dataLeft := padding, len(fds)
	var more, notified bool
	dispatch := func(batch []uring.CQEView, admitted []iofd.FD) {
		t.Helper()
		for _, cqe := range batch {
			if cqe.Op() == uring.IORING_OP_NOP {
				if nopsLeft == 0 || cqe.Res != 0 {
					t.Fatalf("unexpected padding CQE: %+v", cqe)
				}
				nopsLeft--
				continue
			}
			i := slices.Index(admitted, cqe.FD())
			if i < 0 {
				t.Fatalf("completion for unaccepted FD %d", cqe.FD())
			}
			wantOp := uint8(uring.IORING_OP_SEND)
			if i == 0 {
				wantOp = uring.IORING_OP_SEND_ZC
			}
			if cqe.Op() != wantOp {
				t.Fatalf("target %d opcode=%d, want %d", i, cqe.Op(), wantOp)
			}
			if cqe.IsNotification() {
				if i != 0 || notified {
					t.Fatalf("unexpected notification for target %d", i)
				}
				notified = true
				continue
			}
			if sent[i] >= 0 || cqe.Res < 0 || int(cqe.Res) > len(buf) || (i != 0 && cqe.HasMore()) {
				t.Fatalf("target %d previous result=%d, unexpected data CQE: %+v", i, sent[i], cqe)
			}
			sent[i] = int(cqe.Res)
			dataLeft--
			if i == 0 {
				more = cqe.HasMore()
			}
		}
	}

	// Drive the first submission and account for every CQE reaped with the NOPs.
	dispatch(waitForCQEs(t, ring, padding), fds[:accepted])
	suffix := &operationTargets{fds: fds[accepted:]}
	if available := ring.SQAvailable(); available < suffix.Count() {
		t.Fatalf("available SQ entries=%d, need %d", available, suffix.Count())
	}
	n, err := ring.MulticastZeroCopy(ctx, suffix, 0, 0, len(buf))
	if n != suffix.Count() || err != nil {
		t.Fatalf("retry accepted %d of %d targets, error %v", n, suffix.Count(), err)
	}
	for {
		pending := nopsLeft + dataLeft
		if more && !notified {
			pending++
		}
		if pending == 0 {
			break
		}
		dispatch(waitForCQEs(t, ring, pending), fds[:])
	}
	t.Logf("SEND_ZC MORE=%t NOTIF=%t, initial byte counts=%v", more, notified, sent)

	for i, fd := range fds {
		// All original borrows have ended. Finish only this peer's unsent bytes.
		for offset := sent[i]; offset < len(buf); {
			if err := ring.SendFD(ctx, fd, buf[offset:]); err != nil {
				t.Fatal(err)
			}
			cqe := waitForCQEs(t, ring, 1)[0]
			if cqe.Op() != uring.IORING_OP_SEND || cqe.FD() != fd || cqe.HasMore() || cqe.IsNotification() || cqe.Res <= 0 || int(cqe.Res) > len(buf)-offset {
				t.Fatalf("target %d suffix at %d: unexpected completion %+v", i, offset, cqe)
			}
			offset += int(cqe.Res)
		}
		if errno := zcall.Shutdown(uintptr(fd), zcall.SHUT_WR); errno != 0 {
			t.Fatalf("target %d shutdown: %v", i, zcall.Errno(errno))
		}
		if err := peers[i].SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(io.LimitReader(peers[i], int64(len(payload)+1)))
		if err != nil || !bytes.Equal(got, payload[:]) {
			t.Fatalf("peer %d received %d bytes, want one matching payload of %d bytes: %v", i, len(got), len(payload), err)
		}
	}
}

func TestMulticastZeroCopySmallPayloadCompletion(t *testing.T) {
	ring := newTestRing(t) // The helper retains completion evidence by default.
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	payload := []byte("automatic copy path")
	copy(ring.RegisteredBuffer(0), payload)
	targets := &operationTargets{fds: []iofd.FD{fds[0]}}
	accepted, err := ring.MulticastZeroCopy(uring.ForFD(0), targets, 0, 0, len(payload))
	if accepted != 1 || err != nil {
		t.Fatalf("accepted=%d error=%v", accepted, err)
	}
	cqe, ok := waitForOp(t, ring, uring.IORING_OP_SEND, 2*time.Second)
	if !ok || cqe.Res != int32(len(payload)) || cqe.HasMore() || cqe.IsNotification() {
		t.Fatalf("ordinary completion: %+v present=%v", cqe, ok)
	}
	got := make([]byte, len(payload))
	n, errno := readTestFD(fds[1], got)
	if n != len(payload) || errno != 0 || !bytes.Equal(got, payload) {
		t.Fatalf("read=%d errno=%d data=%q", n, errno, got)
	}
}

func TestMulticastRejectsSharedContext(t *testing.T) {
	ring := newTestRing(t)
	indirect := ring.IndirectSQE()
	extended := ring.ExtSQE()
	defer ring.PutIndirectSQE(indirect)
	defer ring.PutExtSQE(extended)
	targets := &operationTargets{fds: []iofd.FD{-1, -1}}
	for _, ctx := range []uring.SQEContext{uring.PackIndirect(indirect), uring.PackExtended(extended)} {
		if n, err := ring.Multicast(ctx, targets, -1, []byte{1}, 0, 1); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
			t.Fatalf("ordinary accepted=%d error=%v", n, err)
		}
		if n, err := ring.MulticastZeroCopy(ctx, targets, 0, 0, 1); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
			t.Fatalf("zero-copy accepted=%d error=%v", n, err)
		}
	}
	var cqes [8]uring.CQEView
	if n, err := ring.Wait(cqes[:]); n != 0 || !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("invalid contexts published work: n=%d error=%v", n, err)
	}
}

func TestMulticastEmptyAndInvalidRangesDoNotSubmit(t *testing.T) {
	ring := newTestRing(t)
	ctx := uring.ForFD(0)
	payload := []byte("payload")
	for _, tc := range []struct {
		name string
		size int
		send func(uring.SendTargets, int64, int) (int, error)
	}{
		{"user buffer", len(payload), func(targets uring.SendTargets, off int64, n int) (int, error) {
			return ring.Multicast(ctx, targets, -1, payload, off, n)
		}},
		{"registered buffer", len(ring.RegisteredBuffer(0)), func(targets uring.SendTargets, off int64, n int) (int, error) {
			return ring.Multicast(ctx, targets, 0, nil, off, n)
		}},
		{"zero copy", len(ring.RegisteredBuffer(0)), func(targets uring.SendTargets, off int64, n int) (int, error) {
			return ring.MulticastZeroCopy(ctx, targets, 0, off, n)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			available := ring.SQAvailable()
			if n, err := tc.send(&operationTargets{}, 0, 1); n != 0 || err != nil {
				t.Fatalf("empty targets: accepted=%d error=%v", n, err)
			}
			targets := &operationTargets{fds: []iofd.FD{-1}}
			for _, bounds := range [][2]int{{-1, 1}, {0, -1}, {tc.size + 1, 0}, {tc.size, 1}} {
				if n, err := tc.send(targets, int64(bounds[0]), bounds[1]); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
					t.Fatalf("range %v: accepted=%d error=%v", bounds, n, err)
				}
			}
			if got := ring.SQAvailable(); got != available {
				t.Fatalf("available SQEs=%d, want %d", got, available)
			}
		})
	}
	targets := &operationTargets{fds: []iofd.FD{-1}}
	missing := ring.RegisteredBufferCount()
	if n, err := ring.Multicast(ctx, targets, missing, nil, 0, 1); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("missing registered buffer: accepted=%d error=%v", n, err)
	}
	if n, err := ring.MulticastZeroCopy(ctx, targets, missing, 0, 1); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("missing zero-copy buffer: accepted=%d error=%v", n, err)
	}
	var cqes [1]uring.CQEView
	if n, err := ring.Wait(cqes[:]); n != 0 || !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("empty or invalid fanout published work: n=%d error=%v", n, err)
	}
}

func TestZeroCopyRejectsSkippedCompletion(t *testing.T) {
	ring := newTestRing(t)
	pool := uring.NewContextPools(1)
	tracker := uring.NewZCTracker(ring, pool)
	option := uring.WithFlags(uring.IOSQE_CQE_SKIP_SUCCESS)
	if err := tracker.SendZC(-1, []byte{1}, nil, option); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("SendZC: %v", err)
	}
	if err := tracker.SendZCFixed(-1, 0, 0, 1, nil, option); !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("SendZCFixed: %v", err)
	}
	targets := &operationTargets{fds: []iofd.FD{-1}}
	if n, err := ring.MulticastZeroCopy(uring.ForFD(0), targets, 0, 0, 1, option); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("MulticastZeroCopy: n=%d error=%v", n, err)
	}
	if n, err := ring.Multicast(uring.ForFD(0), targets, 0, nil, 0, 8192, option); n != 0 || !errors.Is(err, uring.ErrInvalidParam) {
		t.Fatalf("Multicast automatic zero-copy: n=%d error=%v", n, err)
	}
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("invalid operation retained the context")
	}
	pool.PutExtended(ext)
	var cqes [8]uring.CQEView
	if n, err := ring.Wait(cqes[:]); n != 0 || !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("invalid flags published work: n=%d error=%v", n, err)
	}
}

func BenchmarkMulticastFullSQ(b *testing.B) {
	ring, err := uring.New(testMinimalBufferOptions, func(o *uring.Options) { o.Entries = uring.EntriesPico })
	if err != nil {
		b.Fatal(err)
	}
	mustStartRing(b, ring)
	for range uring.EntriesPico {
		if err := ring.Nop(uring.ForFD(0)); err != nil {
			b.Fatal(err)
		}
	}
	var payload [1]byte
	targets := &operationTargets{fds: []iofd.FD{-1}}
	b.ReportAllocs()
	for _, tc := range []struct {
		name       string
		bufIndex   int
		size       int
		aggressive bool
	}{
		{"UserBuffer", -1, 1, false},
		{"FixedCopy", 0, 1, false},
		{"FixedZeroCopy", 0, 8192, false},
		{"AggressiveCopy", 0, 1, true},
		{"AggressiveZeroCopy", 0, 1536, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var n int
				var err error
				if tc.aggressive {
					n, err = ring.MulticastZeroCopy(uring.ForFD(0), targets, tc.bufIndex, 0, tc.size)
				} else {
					n, err = ring.Multicast(uring.ForFD(0), targets, tc.bufIndex, payload[:], 0, tc.size)
				}
				if n != 0 || err != iox.ErrWouldBlock {
					b.Fatalf("accepted=%d error=%v", n, err)
				}
			}
		})
	}
	var cqes [uring.EntriesPico]uring.CQEView
	for seen := 0; seen < len(cqes); {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			b.Fatal(err)
		}
		seen += n
	}
}

func BenchmarkBundleReceiveRecycle(b *testing.B) {
	const size, entries = 64, 4
	ring, err := uring.New(testMinimalBufferOptions, func(o *uring.Options) {
		o.ReadBufferSize = size
		o.ReadBufferNum = entries
	})
	if err != nil {
		b.Fatal(err)
	}
	mustStartRing(b, ring)
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestFds(fds)
	var payload [size * entries]byte
	var cqes [4]uring.CQEView
	var backoff iox.Backoff
	pollFD := testPollFd{fd: fds[0]}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if n, errno := writeTestFD(fds[1], payload[:]); errno != 0 || n != len(payload) {
			b.Fatalf("write n=%d errno=%d", n, errno)
		}
		for remaining := len(payload); remaining > 0; {
			if err := ring.ReceiveBundle(uring.ForFD(0), &pollFD); err != nil {
				b.Fatal(err)
			}
			for {
				n, err := ring.Wait(cqes[:])
				if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
					b.Fatal(err)
				}
				if n == 0 {
					backoff.Wait()
					continue
				}
				backoff.Reset()
				for _, cqe := range cqes[:n] {
					it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
					if !ok {
						b.Fatalf("completion: %+v", cqe)
					}
					for buf := range it.All() {
						if buf[0] != 0 {
							b.Fatal("unexpected payload")
						}
						remaining -= len(buf)
					}
					if err := it.Recycle(ring); err != nil {
						b.Fatal(err)
					}
				}
				break
			}
		}
	}
}
