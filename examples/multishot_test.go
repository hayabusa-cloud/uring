// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// One multishot accept request produces a stream of completion events:
//
//	submit -> accept CQE (MORE) -> accept CQE (MORE) -> terminal CQE
//
// Each successful accept yields a new connection FD. Returning MultishotStop
// requests cancellation; continue dispatching until both the original request
// and any submitted cancellation have completed, in either arrival order.
func TestMultishotAcceptServer(t *testing.T) {
	for _, stopInHandler := range []bool{false, true} {
		name := "explicit_cancel"
		if stopInHandler {
			name = "handler_stop"
		}
		t.Run(name, func(t *testing.T) {
			ring, err := uring.New(func(opt *uring.Options) {
				opt.Entries = uring.EntriesNano
				opt.LockedBufferMem = uring.BufferSizeLarge
				opt.ReadBufferNum = 64
				opt.NotifySucceed = true
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := ring.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() {
				// All submitted operations must finish before the ring is destroyed.
				if err := ring.Stop(); err != nil {
					t.Errorf("Stop: %v", err)
				}
			})
			listener, addr := tcpListener(t)
			const clients = 3
			for range clients {
				dialTCP(t, addr) // Connections queue in the listener backlog.
			}
			accepted, stops := 0, 0
			handler := uring.NewMultishotSubscriber().
				OnStep(func(step uring.MultishotStep) uring.MultishotAction {
					if step.Err != nil {
						if !step.Cancelled {
							t.Errorf("accept: %v", step.Err)
						}
						return uring.MultishotContinue
					}
					// Every successful accept transfers a descriptor, including
					// accepts racing with a cancellation request.
					fd := iofd.NewFD(int(step.CQE.Res))
					if err := fd.Close(); err != nil {
						t.Errorf("accepted connection Close: %v", err)
					}
					accepted++
					if stopInHandler && accepted >= clients {
						return uring.MultishotStop
					}
					return uring.MultishotContinue
				}).
				OnStop(func(err error, cancelled bool) {
					stops++
					if !cancelled {
						t.Errorf("expected cancellation, got %v", err)
					}
				})
			sub, err := ring.AcceptMultishot(uring.ForFD(int32(listener)), handler)
			if err != nil {
				t.Fatal(err)
			}
			cancelSubmitted, cancelDone := false, false
			for stops == 0 || (cancelSubmitted && !cancelDone) {
				var cqes [1]uring.CQEView
				var backoff iox.Backoff
				deadline := time.Now().Add(exampleTimeout)
				for {
					n, err := ring.Wait(cqes[:])
					if err != nil && !iox.IsWouldBlock(err) {
						t.Fatalf("Wait: %v", err)
					}
					if n > 0 {
						backoff.Reset()
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("timed out waiting for completion")
					}
					backoff.Wait()
				}
				cqe := cqes[0]
				if cqe.Op() == uring.IORING_OP_ASYNC_CANCEL {
					if cancelDone {
						t.Fatal("duplicate cancellation completion")
					}
					cancelDone = true
					if cqe.Res != 0 {
						t.Fatalf("cancel result=%d error=%v", cqe.Res, cqe.Err())
					}
					if !sub.HandleCQE(cqe) {
						t.Fatal("unhandled cancellation completion")
					}
					continue
				}
				// HandleCQE performs callback dispatch, cancellation requested
				// by the handler, and final context retirement.
				if !sub.HandleCQE(cqe) {
					t.Fatal("unhandled accept completion")
				}
				if !stopInHandler && accepted == clients && sub.Active() {
					if err := sub.Cancel(); err != nil {
						t.Fatal(err)
					}
					cancelSubmitted = true
				}
				if sub.State() == uring.SubscriptionCancelling {
					cancelSubmitted = true
				}
			}
			if accepted != clients || stops != 1 || sub.State() != uring.SubscriptionStopped ||
				!cancelSubmitted || !cancelDone {
				t.Fatalf("accepted=%d stops=%d state=%v cancel=%v/%v",
					accepted, stops, sub.State(), cancelSubmitted, cancelDone)
			}
		})
	}
}

// A multishot receive repeatedly selects provided buffers for one subscription:
//
//	CQE -> selected bytes -> process or copy -> recycle buffer
//
// The buffer's lifetime and the subscription's lifetime are separate. Returning
// a buffer does not end the receive; EOF or cancellation ends this example's
// subscription. Copy retained payload before recycling its backing storage.
func TestMultishotRecvWithBufferSelection(t *testing.T) {
	for _, stopInHandler := range []bool{false, true} {
		name := "peer_eof"
		if stopInHandler {
			name = "handler_stop"
		}
		t.Run(name, func(t *testing.T) {
			ring, err := uring.New(func(opt *uring.Options) {
				opt.Entries = uring.EntriesNano
				opt.LockedBufferMem = uring.BufferSizeLarge
				opt.ReadBufferNum = 64
				opt.NotifySucceed = true
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := ring.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() {
				// All submitted operations must finish before the ring is destroyed.
				if err := ring.Stop(); err != nil {
					t.Errorf("Stop: %v", err)
				}
			})
			fd, client := tcpPair(t)
			payload := []byte("receive, process, then recycle")
			if n, err := client.Write(payload); err != nil || n != len(payload) {
				t.Fatalf("write: n=%d err=%v", n, err)
			}
			if !stopInHandler {
				// End the peer's byte stream; keep the socket open until cleanup.
				if err := client.Shutdown(zcall.SHUT_WR); err != nil {
					t.Fatal(err)
				}
			}

			var got [64]byte
			received, stops := 0, 0
			handler := uring.NewMultishotSubscriber().
				OnStep(func(step uring.MultishotStep) uring.MultishotAction {
					if step.Err != nil {
						if !step.Cancelled {
							t.Errorf("receive: %v", step.Err)
						}
						return uring.MultishotContinue
					}
					if step.CQE.Res == 0 {
						return uring.MultishotContinue
					}
					it, ok := ring.BundleIterator(step.CQE, step.CQE.BufGroup())
					if !ok {
						t.Error("missing selected buffer")
						return uring.MultishotStop
					}
					for buf := range it.All() {
						received += copy(got[received:], buf)
					}
					if err := it.Recycle(ring); err != nil {
						t.Errorf("recycle: %v", err)
					}
					if stopInHandler && received == len(payload) {
						return uring.MultishotStop
					}
					return uring.MultishotContinue
				}).
				OnStop(func(err error, cancelled bool) {
					stops++
					if cancelled != stopInHandler || (!cancelled && err != nil) {
						t.Errorf("receive stop: %v, cancelled=%v", err, cancelled)
					}
				})
			// The small example configuration has one provided-buffer group.
			ctx := uring.PackDirect(uring.IORING_OP_RECV, 0, uint16(ring.ReadBufferGidOffset), int32(fd))
			sub, err := ring.ReceiveMultishot(ctx, handler)
			if err != nil {
				t.Fatal(err)
			}
			cancelSubmitted, cancelDone := false, false
			for stops == 0 || (cancelSubmitted && !cancelDone) {
				var cqes [1]uring.CQEView
				var backoff iox.Backoff
				deadline := time.Now().Add(exampleTimeout)
				for {
					n, err := ring.Wait(cqes[:])
					if err != nil && !iox.IsWouldBlock(err) {
						t.Fatalf("Wait: %v", err)
					}
					if n > 0 {
						backoff.Reset()
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("timed out waiting for completion")
					}
					backoff.Wait()
				}
				cqe := cqes[0]
				// Read borrowed context facts before HandleCQE can retire them.
				if cqe.Op() == uring.IORING_OP_ASYNC_CANCEL {
					if cancelDone {
						t.Fatal("duplicate cancellation completion")
					}
					if cqe.Res != 0 {
						t.Fatalf("cancel result=%d error=%v", cqe.Res, cqe.Err())
					}
					cancelDone = true
				}
				if !sub.HandleCQE(cqe) {
					t.Fatal("unhandled receive or cancellation completion")
				}
				if sub.State() == uring.SubscriptionCancelling {
					cancelSubmitted = true
				}
			}
			if sub.State() != uring.SubscriptionStopped || stops != 1 || !bytes.Equal(got[:received], payload) {
				t.Fatalf("state=%v stops=%d payload=%q", sub.State(), stops, got[:received])
			}
			if stopInHandler && (!cancelSubmitted || !cancelDone) {
				t.Fatalf("cancellation submitted=%v completed=%v", cancelSubmitted, cancelDone)
			}
		})
	}
}
