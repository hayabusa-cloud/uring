// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func reapResizeResults(t *testing.T, ring *uring.Uring, want map[uint64]int32) {
	t.Helper()
	remaining := len(want)
	var cqes [16]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for remaining > 0 && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes[:])
		if err != nil && err != iox.ErrWouldBlock {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			remaining--
			id := cqe.Context().Raw()
			res, ok := want[id]
			if !ok {
				t.Errorf("unexpected or duplicate completion %#x: result %d", id, cqe.Res)
				continue
			}
			delete(want, id)
			if cqe.Res != res {
				t.Errorf("completion %#x: result %d, want %d", id, cqe.Res, res)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing completions: %v", want)
	}
}

func TestResizeRingsPendingSubmissions(t *testing.T) {
	for _, sqe128 := range []bool{false, true} {
		for _, array := range []bool{false, true} {
			t.Run(fmt.Sprintf("SQE128=%t/array=%t", sqe128, array), func(t *testing.T) {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				ring, err := uring.New(testMinimalBufferOptions, func(o *uring.Options) {
					o.MultiIssuers = false
					o.NotifySucceed = true
					o.SQE128 = sqe128
					o.IndirectSubmissionQueue = array
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := ring.Start(); err != nil {
					t.Fatal(err)
				}
				defer ring.Stop()

				// Advance head so the pending batch crosses the old SQ boundary.
				want := make(map[uint64]int32)
				for i := range uring.EntriesNano - 1 {
					ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, int32(i+1))
					if err := ring.Nop(ctx); err != nil {
						t.Fatal(err)
					}
					want[ctx.Raw()] = 0
				}
				reapResizeResults(t, ring, want)

				for stage, size := range []struct {
					sq, cq         uint32
					wantSQ, wantCQ int
				}{
					{sq: 63, cq: 127, wantSQ: 64, wantCQ: 128},
					{sq: 31, cq: 255, wantSQ: 32, wantCQ: 256},
					{sq: 0, cq: 0, wantSQ: 32, wantCQ: 64},
				} {
					for i := range 8 {
						ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, int32(100+stage*8+i))
						if err := ring.Nop(ctx); err != nil {
							t.Fatal(err)
						}
						want[ctx.Raw()] = 0
					}
					if err := ring.ResizeRings(size.sq, size.cq); err != nil {
						t.Fatal(err)
					}
					if ring.Features.SQEntries != size.wantSQ || ring.Features.CQEntries != size.wantCQ {
						t.Errorf("resize geometry = %d/%d, want %d/%d",
							ring.Features.SQEntries, ring.Features.CQEntries, size.wantSQ, size.wantCQ)
					}
					if ring.SQAvailable() != size.wantSQ-len(want) {
						t.Errorf("resize changed the pending submission count")
					}
					reapResizeResults(t, ring, want)
				}
			})
		}
	}
}

func TestResizeRingsPendingTimeouts(t *testing.T) {
	for _, mode := range []string{"Direct", "Indirect", "Extended"} {
		t.Run(mode, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ring, err := uring.New(testMinimalBufferOptions, func(o *uring.Options) {
				o.MultiIssuers = false
				o.NotifySucceed = true
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := ring.Start(); err != nil {
				t.Fatal(err)
			}
			defer ring.Stop()
			var ctx [4]uring.SQEContext
			for i, op := range []uint8{uring.IORING_OP_TIMEOUT, uring.IORING_OP_TIMEOUT_REMOVE, uring.IORING_OP_TIMEOUT, uring.IORING_OP_LINK_TIMEOUT} {
				switch mode {
				case "Direct":
					ctx[i] = uring.PackDirect(op, 0, 0, int32(i+1))
				case "Indirect":
					ind := ring.IndirectSQE()
					defer ring.PutIndirectSQE(ind)
					ctx[i] = uring.PackIndirect(ind).WithOp(op)
				case "Extended":
					ext := ring.ExtSQE()
					defer ring.PutExtSQE(ext)
					ctx[i] = uring.PackExtended(ext).WithOp(op)
				}
			}
			ctx[2] = ctx[2].OrFlags(uring.IOSQE_IO_LINK)
			if err := ring.Timeout(ctx[0], time.Hour, uring.WithCount(0)); err != nil {
				t.Fatal(err)
			}
			if err := ring.TimeoutUpdate(ctx[1], ctx[0].Raw(), time.Millisecond, false); err != nil {
				t.Fatal(err)
			}
			if err := ring.Timeout(ctx[2], time.Hour, uring.WithCount(0)); err != nil {
				t.Fatal(err)
			}
			if err := ring.LinkTimeout(ctx[3], time.Millisecond); err != nil {
				t.Fatal(err)
			}
			for _, sq := range []uint32{64, 32, 128} {
				if err := ring.ResizeRings(sq, sq*2); err != nil {
					t.Fatal(err)
				}
				runtime.GC()
			}
			reapResizeResults(t, ring, map[uint64]int32{
				ctx[0].Raw(): -int32(zcall.ETIME),
				ctx[1].Raw(): 0,
				ctx[2].Raw(): -int32(zcall.ECANCELED),
				ctx[3].Raw(): -int32(zcall.ETIME),
			})
		})
	}
}

func TestResizeRingsFailurePreservesPending(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ring, err := uring.New(testMinimalBufferOptions, func(o *uring.Options) {
		o.Entries = 64
		o.MultiIssuers = false
		o.NotifySucceed = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Start(); err != nil {
		t.Fatal(err)
	}
	defer ring.Stop()
	want := make(map[uint64]int32)
	for i := range 40 {
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, int32(i+1))
		if err := ring.Nop(ctx); err != nil {
			t.Fatal(err)
		}
		want[ctx.Raw()] = 0
	}
	if err := ring.ResizeRings(32, 64); err != zcall.EOVERFLOW {
		t.Errorf("ResizeRings: %v, want EOVERFLOW", err)
	}
	if ring.Features.SQEntries != 64 || ring.SQAvailable() != 24 {
		t.Error("failed resize changed SQ geometry or pending count")
	}
	reapResizeResults(t, ring, want)
}
