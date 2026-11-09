// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"errors"
	"net"
	"os"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func TestIncrementalReceiverInterleavedBuffers(t *testing.T) {
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	const groupID = 60000 // newIncrementalTestReceiver's group.
	receiver, backing := newIncrementalTestReceiver(t, ring, uring.NewContextPools(2), 8, 2)
	var files [2]*os.File
	var clients [2]*net.UDPConn
	for i := range files {
		server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()
		files[i], err = server.File()
		if err != nil {
			t.Fatal(err)
		}
		defer files[i].Close()
		clients[i], err = net.DialUDP("udp4", nil, server.LocalAddr().(*net.UDPAddr))
		if err != nil {
			t.Fatal(err)
		}
		defer clients[i].Close()
	}
	send := func(i int, payload string) {
		t.Helper()
		if n, err := clients[i].Write([]byte(payload)); err != nil || n != len(payload) {
			t.Fatalf("send %d: n=%d err=%v", i, n, err)
		}
	}
	receive := func(i int, handler *testIncHandler) {
		t.Helper()
		if err := receiver.Recv(iofd.FD(files[i].Fd()), handler); err != nil {
			t.Fatal(err)
		}
	}
	next := func() uring.CQEView {
		t.Helper()
		var cqes [1]uring.CQEView
		var backoff iox.Backoff
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			n, err := ring.Wait(cqes[:])
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				t.Fatal(err)
			}
			if n > 0 {
				backoff.Reset()
				return cqes[0]
			}
			backoff.Wait()
		}
		t.Fatal("missing completion")
		return uring.CQEView{}
	}
	dispatch := func(cqe uring.CQEView, id uint16) {
		t.Helper()
		if cqe.Op() != uring.IORING_OP_RECV || cqe.Res != 2 || !cqe.HasBuffer() || cqe.BufID() != id {
			t.Fatalf("unexpected completion: op=%d res=%d flags=%#x", cqe.Op(), cqe.Res, cqe.Flags)
		}
		if !receiver.HandleCQE(cqe) {
			t.Fatal("unhandled receive")
		}
	}

	send(0, "ab")
	first := &testIncHandler{}
	receive(0, first)
	dispatch(next(), 0)
	if !first.lastHasMore.Load() || !bytes.Equal(first.received, []byte("ab")) {
		t.Fatalf("first receive = %q, more=%v", first.received, first.lastHasMore.Load())
	}

	// An asynchronous link sends the following receive to io-wq, where
	// selection commits the remaining buffer before receiving data.
	if err := ring.Nop(uring.ForFD(11), uring.WithFlags(uring.IOSQE_ASYNC|uring.IOSQE_IO_LINK)); err != nil {
		t.Fatal(err)
	}
	delayed := &testIncHandler{}
	receive(0, delayed)
	if cqe := next(); cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
		t.Fatalf("unexpected link completion: op=%d res=%d", cqe.Op(), cqe.Res)
	}
	var status struct {
		group    uint32
		head     uint32
		reserved [8]uint32
	}
	status.group = groupID
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, errno := zcall.IoUringRegister(uintptr(ring.RingFD()), uring.IORING_REGISTER_PBUF_STATUS, unsafe.Pointer(&status), 1)
		if errno != 0 {
			t.Fatalf("buffer status: %v", zcall.Errno(errno))
		}
		if status.head == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not reserve buffer 0: head=%d", status.head)
		}
		backoff.Wait()
	}

	send(1, "XY")
	second := &testIncHandler{}
	receive(1, second)
	dispatch(next(), 1)
	if !second.lastHasMore.Load() || !bytes.Equal(second.received, []byte("XY")) {
		t.Fatalf("second buffer = %q, more=%v", second.received, second.lastHasMore.Load())
	}
	send(0, "cd")
	dispatch(next(), 0)
	if !bytes.Equal(delayed.received, []byte("cd")) {
		t.Fatalf("delayed buffer = %q, want cd; backing=%q", delayed.received, backing)
	}
	if delayed.lastHasMore.Load() {
		t.Fatal("worker receive retained its reserved buffer")
	}

	// Returning buffer 0 must preserve buffer 1's remaining suffix.
	send(1, "ZW")
	last := &testIncHandler{}
	receive(1, last)
	dispatch(next(), 1)
	if !bytes.Equal(last.received, []byte("ZW")) {
		t.Fatalf("second buffer suffix = %q, want ZW", last.received)
	}
}
