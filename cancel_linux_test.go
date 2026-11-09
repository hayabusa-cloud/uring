// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func TestMultishotCancelIdentityLifetime(t *testing.T) {
	for _, originalFirst := range []bool{false, true} {
		name := "cancel_first"
		if originalFirst {
			name = "original_first"
		}
		t.Run(name, func(t *testing.T) {
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			var held []*uring.ExtSQE
			for range uring.EntriesNano - 1 {
				ext := ring.ExtSQE()
				if ext == nil {
					t.Fatal("context pool exhausted")
				}
				held = append(held, ext)
			}
			defer func() {
				for _, ext := range held {
					ring.PutExtSQE(ext)
				}
			}()

			var cqes [4]uring.CQEView
			wait := func(visit func(uring.CQEView), done func() bool) {
				t.Helper()
				var backoff iox.Backoff
				deadline := time.Now().Add(3 * time.Second)
				for !done() && time.Now().Before(deadline) {
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
						visit(cqe)
					}
				}
				if !done() {
					t.Fatal("completion timeout")
				}
			}
			unavailable := func() {
				t.Helper()
				if ext := ring.ExtSQE(); ext != nil {
					ring.PutExtSQE(ext)
					t.Error("original identity reusable before both completions were handled")
				}
			}

			for range 10 {
				sub, err := ring.AcceptMultishot(uring.ForFD(-1), nil)
				if err != nil {
					t.Fatal(err)
				}
				var terminal uring.CQEView
				terminalSeen := false
				wait(func(cqe uring.CQEView) {
					if !cqe.Extended() || cqe.HasMore() || cqe.Res != -int32(zcall.EBADF) {
						t.Fatalf("unexpected original completion: %+v", cqe)
					}
					terminal, terminalSeen = cqe, true
				}, func() bool { return terminalSeen })
				originalID := terminal.Context().Raw()

				// The drain can defer the following cancel to io-wq. SQ order
				// alone no longer protects a recycled cancellation target.
				if err := ring.Timeout(uring.ForFD(10), time.Millisecond, uring.WithCount(0)); err != nil {
					t.Fatal(err)
				}
				if err := ring.Nop(uring.ForFD(11), uring.WithFlags(uring.IOSQE_IO_DRAIN)); err != nil {
					t.Fatal(err)
				}
				if err := sub.Cancel(); err != nil {
					t.Fatal(err)
				}
				if originalFirst && !sub.HandleCQE(terminal) {
					t.Fatal("original CQE not handled")
				}
				unavailable()

				timeoutDone, drainDone, cancelDone := false, false, false
				wait(func(cqe uring.CQEView) {
					switch cqe.Op() {
					case uring.IORING_OP_TIMEOUT:
						if cqe.Res != -int32(zcall.ETIME) {
							t.Fatalf("timeout result=%d", cqe.Res)
						}
						timeoutDone = true
					case uring.IORING_OP_NOP:
						if cqe.Res != 0 {
							t.Fatalf("drain result=%d", cqe.Res)
						}
						drainDone = true
					case uring.IORING_OP_ASYNC_CANCEL:
						if cqe.Res != -int32(zcall.ENOENT) {
							t.Fatalf("cancel result=%d", cqe.Res)
						}
						if cqe.Context().Raw() == originalID {
							t.Error("cancellation overwrote original identity")
						}
						if !sub.HandleCQE(cqe) {
							t.Error("cancellation CQE not handled")
						}
						cancelDone = true
					default:
						t.Fatalf("unexpected completion opcode=%d", cqe.Op())
					}
				}, func() bool { return timeoutDone && drainDone && cancelDone })

				if !originalFirst {
					unavailable()
					if terminal.Op() != uring.IORING_OP_ACCEPT {
						t.Error("cancellation overwrote original SQE facts")
					}
					if !sub.HandleCQE(terminal) {
						t.Fatal("original CQE not handled")
					}
				}
				ext := ring.ExtSQE()
				if ext == nil {
					t.Fatal("original context not returned after both completions")
				}
				if uring.PackExtended(ext).Raw() != originalID {
					t.Error("unexpected recycled context")
				}
				ring.PutExtSQE(ext)
			}
		})
	}
}
