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
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
)

// A client waits for each prerequisite before submitting the next operation:
//
//	SOCKET CQE -> own the FD -> CONNECT CQE -> SEND CQE -> close the FD
//
// A nil submission error means the request was accepted, not that the socket
// is connected or the bytes were sent. Each CQE supplies that next fact.
func TestTCPClientConnect(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Keep successful SEND completions visible.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		// Every admitted request is complete before normal teardown.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	// The ordinary TCP listener is the peer fixture; uring owns the client.
	listener, err := sock.ListenTCP4(&sock.TCPAddr{IP: sock.IPv4LoopBack})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("listener Close: %v", err)
		}
	}()

	if err := ring.TCP4Socket(uring.ForFD(0)); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}
	var cqes [1]uring.CQEView // This lesson has one outstanding request at a time.
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
			t.Fatal("timed out waiting for socket creation")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_SOCKET || cqe.Err() != nil {
		t.Fatalf("socket completion: op=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	// SOCKET returns the new descriptor in Res; the caller now owns its close.
	fd := iofd.NewFD(int(cqe.Res))
	defer func() {
		if err := fd.Close(); err != nil {
			t.Errorf("client Close: %v", err)
		}
	}()

	ctx := uring.ForFD(int32(fd))
	if err := ring.Connect(ctx, listener.Addr()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	deadline = time.Now().Add(exampleTimeout)
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
			t.Fatal("timed out waiting for connect completion")
		}
		backoff.Wait()
	}
	cqe = cqes[0]
	if cqe.Op() != uring.IORING_OP_CONNECT || cqe.FD() != fd || cqe.Res != 0 {
		t.Fatalf("connect completion: op=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
	}

	// The handshake can finish while the connection waits in the listener's
	// backlog. Accept it now; no background goroutine is needed for this lesson.
	if err := listener.SetDeadline(time.Now().Add(exampleTimeout)); err != nil {
		t.Fatalf("listener SetDeadline: %v", err)
	}
	peer, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer func() {
		if err := peer.Close(); err != nil {
			t.Errorf("peer Close: %v", err)
		}
	}()

	payload := []byte("hello from an io_uring client")
	sent := 0
	for sent < len(payload) {
		// A stream send may complete short. Keep the bytes live and submit only
		// the remaining suffix after observing the preceding SEND result.
		if err := ring.SendFD(ctx, fd, payload[sent:]); err != nil {
			t.Fatalf("SendFD: %v", err)
		}
		deadline = time.Now().Add(exampleTimeout)
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
				t.Fatal("timed out waiting for send completion")
			}
			backoff.Wait()
		}
		cqe = cqes[0]
		if cqe.Op() != uring.IORING_OP_SEND || cqe.FD() != fd || cqe.Res <= 0 || int(cqe.Res) > len(payload)-sent {
			t.Fatalf("send completion: op=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
		}
		sent += int(cqe.Res)
	}

	// SEND completion permits buffer reuse; it does not prove that the remote
	// application consumed the bytes. Read them at the peer to check delivery.
	// The deadline enables sock's adaptive backoff between no-progress reads.
	if err := peer.SetReadDeadline(time.Now().Add(exampleTimeout)); err != nil {
		t.Fatalf("peer SetReadDeadline: %v", err)
	}
	got := make([]byte, len(payload))
	for received := 0; received < len(got); {
		n, err := peer.Read(got[received:])
		received += n
		if err != nil && received < len(got) {
			t.Fatalf("peer Read: received=%d err=%v", received, err)
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("peer received %q, want %q", got, payload)
	}
}
