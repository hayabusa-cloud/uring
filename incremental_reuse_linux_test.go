// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func TestIncrementalReceiverBufferReturnWithoutData(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "inline"
		if deferred {
			name = "worker"
		}
		t.Run(name, func(t *testing.T) {
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			const groupID = 60001
			backing := make([]byte, 8)
			buffers, err := ring.RegisterBufRingIncremental(1, groupID)
			if err != nil {
				t.Fatal(err)
			}
			publish := func() {
				t.Helper()
				if err := buffers.Add(backing, 0, 0); err != nil {
					t.Fatal(err)
				}
				if err := buffers.Advance(1); err != nil {
					t.Fatal(err)
				}
			}
			publish()
			receiver := uring.NewIncrementalReceiver(ring, uring.NewContextPools(1), groupID, len(backing), backing, 1)
			server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			file, err := server.File()
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			client, err := net.DialUDP("udp4", nil, server.LocalAddr().(*net.UDPAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			send := func(data []byte) {
				t.Helper()
				if n, err := client.Write(data); err != nil || n != len(data) {
					t.Fatalf("send: n=%d err=%v", n, err)
				}
			}
			receive := func(handler *testIncHandler, withDrain bool) (int32, uint32) {
				t.Helper()
				if withDrain {
					if err := ring.Timeout(uring.ForFD(10), 10*time.Millisecond, uring.WithCount(0)); err != nil {
						t.Fatal(err)
					}
					if err := ring.Nop(uring.ForFD(11), uring.WithFlags(uring.IOSQE_IO_DRAIN)); err != nil {
						t.Fatal(err)
					}
				}
				if err := receiver.Recv(iofd.FD(file.Fd()), handler); err != nil {
					t.Fatal(err)
				}
				remaining := 1
				if withDrain {
					remaining = 3
				}
				var res int32
				var flags uint32
				var cqes [4]uring.CQEView
				var backoff iox.Backoff
				deadline := time.Now().Add(3 * time.Second)
				for remaining > 0 && time.Now().Before(deadline) {
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
						switch cqe.Op() {
						case uring.IORING_OP_RECV:
							res, flags = cqe.Res, cqe.Flags
							if !receiver.HandleCQE(cqe) {
								t.Fatal("unhandled receive")
							}
						case uring.IORING_OP_TIMEOUT:
							if !withDrain || cqe.Res != -int32(zcall.ETIME) {
								t.Fatalf("timeout result=%d", cqe.Res)
							}
						case uring.IORING_OP_NOP:
							if !withDrain || cqe.Res != 0 {
								t.Fatalf("drain result=%d", cqe.Res)
							}
						default:
							t.Fatalf("unexpected opcode=%d", cqe.Op())
						}
						remaining--
					}
				}
				if remaining != 0 {
					t.Fatalf("missing %d completions", remaining)
				}
				return res, flags
			}

			send([]byte("ab"))
			first := &testIncHandler{}
			res, flags := receive(first, false)
			if res != 2 || flags&uring.IORING_CQE_F_BUF_MORE == 0 || !bytes.Equal(first.received, []byte("ab")) {
				t.Fatalf("first receive: res=%d flags=%#x data=%q", res, flags, first.received)
			}

			send(nil)
			empty := &testIncHandler{}
			res, flags = receive(empty, deferred)
			wantBuffer := uint32(0)
			if deferred {
				wantBuffer = uring.IORING_CQE_F_BUFFER
			}
			if res != 0 || flags&(uring.IORING_CQE_F_BUFFER|uring.IORING_CQE_F_BUF_MORE) != wantBuffer {
				t.Fatalf("empty receive: res=%d flags=%#x", res, flags)
			}
			if empty.dataCount.Load() != 0 || empty.completeCall.Load() != 1 || empty.errorCall.Load() != 0 {
				t.Fatal("incorrect empty receive callbacks")
			}
			if deferred {
				publish()
			}

			send([]byte("cd"))
			last := &testIncHandler{}
			res, flags = receive(last, false)
			if res != 2 || !bytes.Equal(last.received, []byte("cd")) || last.errorCall.Load() != 0 {
				t.Fatalf("receive after empty result: res=%d flags=%#x data=%q backing=%q", res, flags, last.received, backing)
			}
		})
	}
}
