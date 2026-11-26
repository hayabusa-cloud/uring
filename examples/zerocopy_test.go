// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"runtime"
	"slices"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// A send result and permission to reuse its buffer are separate observations:
//
//	Data CQE (MORE flag)       Buffer release
//	MORE clear                Data completion ends the borrow
//	MORE set                  Retain until the NOTIF CQE is also processed
//
// MORE and NOTIF abbreviate the IORING_CQE_F_* completion flags here.
// ZCTracker reports the result through OnCompleted and release through
// OnNotification, including the fallback that needs no notification CQE.
// Keep the payload live and unchanged until that release callback.
func TestZCTrackerBasic(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

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
	tracker := uring.NewZCTracker(ring, uring.NewContextPools(2))
	payload := []byte("retain this payload until release")
	completed, released := 0, 0
	var sendResult int32
	handler := uring.NewZCSubscriber().
		OnCompleted(func(result int32) {
			completed++
			sendResult = result
			if released != 0 {
				t.Error("release preceded completion")
			}
		}).
		OnNotification(func(result int32) {
			released++
			if completed != 1 || result != sendResult {
				t.Error("incorrect release callback")
			}
		})
	callbacks := handler.Handler()
	for sent := 0; sent < len(payload); {
		completed, released = 0, 0
		if err := tracker.SendZC(fd, payload[sent:], callbacks); err != nil {
			t.Fatal(err)
		}
		// Terminal data without MORE releases the payload immediately. MORE
		// promises a notification; the tracker handles either arrival order.
		for released == 0 {
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
			if !tracker.HandleCQE(cqe) {
				t.Fatal("unhandled zero-copy completion")
			}
		}
		runtime.KeepAlive(payload)
		if completed != 1 || released != 1 || sendResult <= 0 || int(sendResult) > len(payload)-sent {
			t.Fatalf("completed=%d released=%d result=%d", completed, released, sendResult)
		}
		// A stream send may be partial. Resume only the unsent suffix.
		sent += int(sendResult)
	}
	got := make([]byte, len(payload))
	for received := 0; received < len(got); {
		n, err := client.Read(got[received:])
		received += n
		if err != nil && received < len(got) {
			t.Fatalf("client Read: received=%d err=%v", received, err)
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("received %q, want %q", got, payload)
	}
}

type fdSliceTargets []iofd.FD

func (fds fdSliceTargets) Count() int       { return len(fds) }
func (fds fdSliceTargets) FD(i int) iofd.FD { return fds[i] }

// Multicast shares one immutable payload across independent target sends.
// Advance by the accepted target count even on backpressure. Each data CQE
// reports bytes for one peer. Keep the buffer until every send releases it.
func TestMulticastZeroCopy(t *testing.T) {
	// At 1024 bytes, four targets select SEND_ZC and a retry with three
	// selects SEND. A SEND_ZC can still use kernel copying.
	for _, tc := range []struct {
		name       string
		payload    []byte
		queuedNops int
		wantZC     int
	}{
		{"copy", []byte("one immutable buffer, four independent sends"), uring.EntriesPico, 0},
		{"zero_copy", bytes.Repeat([]byte("x"), 2048), 0, 4},
		{"partial_retry", bytes.Repeat([]byte("x"), 1024), uring.EntriesPico - 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime.LockOSThread()
			t.Cleanup(runtime.UnlockOSThread)

			ring, err := uring.New(func(opt *uring.Options) {
				opt.Entries = uring.EntriesPico
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
			var clients [4]*sock.TCPConn
			var fds [4]iofd.FD
			for i := range clients {
				fds[i], clients[i] = tcpPair(t)
			}
			payload := tc.payload
			buf := ring.RegisteredBuffer(0)[:len(payload)]
			copy(buf, payload)
			ctx := uring.ForFD(0)
			// Occupy SQ slots without adding successful NOPs to the send CQEs.
			for range tc.queuedNops {
				if err := ring.Nop(ctx, uring.WithFlags(uring.IOSQE_CQE_SKIP_SUCCESS)); err != nil {
					t.Fatal(err)
				}
			}

			var states [len(fds)]struct {
				data, more, notification, released bool
				result                             int32
			}
			var cqes [1]uring.CQEView
			var backoff iox.Backoff
			var submitErr error
			deadline := time.Now().Add(exampleTimeout)
			accepted, released, zeroCopy := 0, 0, 0
			for released < len(fds) {
				if accepted < len(fds) && submitErr == nil {
					n, err := ring.MulticastZeroCopy(ctx, fdSliceTargets(fds[accepted:]), 0, 0, len(buf))
					accepted += n
					if err != nil && !iox.IsWouldBlock(err) {
						submitErr = err // Drain the accepted sends before reporting the failure.
					}
				}
				if submitErr != nil && released == accepted {
					t.Fatalf("accepted %d targets: %v", accepted, submitErr)
				}
				// Wait flushes pending submissions. SQ space can open even when
				// the CQ is empty, so return to admission after an empty poll.
				n, err := ring.Wait(cqes[:])
				if err != nil && !iox.IsWouldBlock(err) {
					t.Fatalf("Wait: %v", err)
				}
				if n == 0 {
					if time.Now().After(deadline) {
						t.Fatalf("timed out: accepted=%d released=%d", accepted, released)
					}
					backoff.Wait()
					continue
				}
				backoff.Reset()
				cqe := cqes[0]
				i := slices.Index(fds[:accepted], cqe.FD())
				if i < 0 {
					t.Fatalf("unknown target %d", cqe.FD())
				}
				s := &states[i]
				if cqe.IsNotification() {
					if cqe.Op() != uring.IORING_OP_SEND_ZC || s.notification {
						t.Fatalf("target %d: unexpected notification %+v", i, cqe)
					}
					s.notification = true
				} else {
					if s.data {
						t.Fatalf("target %d: duplicate data completion", i)
					}
					switch cqe.Op() {
					case uring.IORING_OP_SEND:
						if cqe.HasMore() {
							t.Fatalf("target %d: ordinary SEND promised another completion", i)
						}
					case uring.IORING_OP_SEND_ZC:
						zeroCopy++
					default:
						t.Fatalf("target %d: unexpected opcode %d", i, cqe.Op())
					}
					s.data, s.more, s.result = true, cqe.HasMore(), cqe.Res
				}
				if !s.released && s.data && (!s.more || s.notification) {
					s.released = true
					released++
				}
			}
			if zeroCopy != tc.wantZC {
				t.Fatalf("got %d SEND_ZC data completions, want %d", zeroCopy, tc.wantZC)
			}
			for i := range clients {
				sent := int(states[i].result)
				if sent < 0 || sent > len(payload) {
					t.Fatalf("target %d: invalid send result %d", i, sent)
				}
				// Target admission does not promise a full stream write. Once every
				// ZC borrow has ended, this caller chooses ordinary SEND for suffixes.
				for sent < len(payload) {
					if err := ring.SendFD(uring.ForFD(int32(fds[i])), fds[i], buf[sent:]); err != nil {
						t.Fatalf("SendFD target %d: %v", i, err)
					}
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
					if cqe.Op() != uring.IORING_OP_SEND || cqe.FD() != fds[i] || cqe.Res <= 0 || int(cqe.Res) > len(payload)-sent {
						t.Fatalf("target %d: unexpected send completion: %+v", i, cqe)
					}
					sent += int(cqe.Res)
				}
				// Read through EOF so replaying a target would also fail this check.
				if errno := zcall.Shutdown(uintptr(fds[i]), zcall.SHUT_WR); errno != 0 {
					t.Fatalf("target %d shutdown: %v", i, zcall.Errno(errno))
				}
				var got bytes.Buffer
				n, err := iox.Copy(&got, clients[i])
				if err != nil || !bytes.Equal(got.Bytes(), payload) {
					t.Fatalf("target %d received %d bytes, want one matching %d-byte payload: %v", i, n, len(payload), err)
				}
			}
		})
	}
}
