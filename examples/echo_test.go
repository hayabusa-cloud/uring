// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/framer"
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func boundPort(t *testing.T, fd iofd.FD) int {
	t.Helper()
	var addr [16]byte
	size := uint32(len(addr))
	if errno := zcall.Getsockname(uintptr(fd), unsafe.Pointer(&addr), unsafe.Pointer(&size)); errno != 0 {
		t.Fatalf("Getsockname: %v", zcall.Errno(errno))
	}
	return int(binary.BigEndian.Uint16(addr[2:4]))
}

// Each setup CQE supplies the facts needed to submit the next operation:
//
//	SOCKET -> socket FD -> BIND -> LISTEN -> ACCEPT -> connection FD
//
// TCP carries bytes without message boundaries. The echo loops advance by each
// CQE's byte count until this example's known payload has been received and sent.
func TestTCPEchoServer(t *testing.T) {
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
	listener, fd := iofd.InvalidFD, iofd.InvalidFD
	t.Cleanup(func() {
		if err := fd.Close(); err != nil {
			t.Errorf("connection Close: %v", err)
		}
		if err := listener.Close(); err != nil {
			t.Errorf("listener Close: %v", err)
		}
	})
	if err := ring.TCP4Socket(uring.ForFD(0)); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}
	var client *sock.TCPConn
	for fd < 0 {
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
		if err := cqe.Err(); err != nil {
			t.Fatalf("setup opcode %d: %v", cqe.Op(), err)
		}
		switch cqe.Op() {
		case uring.IORING_OP_SOCKET:
			listener = iofd.NewFD(int(cqe.Res))
			if err := ring.Bind(uring.ForFD(int32(listener)), &sock.TCPAddr{IP: sock.IPv4LoopBack}); err != nil {
				t.Fatalf("Bind: %v", err)
			}
		case uring.IORING_OP_BIND:
			if err := ring.Listen(uring.ForFD(int32(listener))); err != nil {
				t.Fatalf("Listen: %v", err)
			}
		case uring.IORING_OP_LISTEN:
			client = dialTCP(t, &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: boundPort(t, listener)})
			if err := ring.Accept(uring.ForFD(int32(listener))); err != nil {
				t.Fatalf("Accept: %v", err)
			}
		case uring.IORING_OP_ACCEPT:
			fd = iofd.NewFD(int(cqe.Res))
		default:
			t.Fatalf("unexpected setup opcode %d", cqe.Op())
		}
	}
	payload := []byte("echo through io_uring")
	for sent := 0; sent < len(payload); {
		n, err := client.Write(payload[sent:])
		if err != nil || n == 0 {
			t.Fatalf("client Write: n=%d err=%v", n, err)
		}
		sent += n
	}
	got := make([]byte, len(payload))
	for received := 0; received < len(got); {
		if err := ring.ReceiveFD(uring.ForFD(int32(fd)), fd, got[received:]); err != nil {
			t.Fatalf("ReceiveFD: %v", err)
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
		if cqe.Op() != uring.IORING_OP_RECV || cqe.FD() != fd || cqe.Res <= 0 || int(cqe.Res) > len(got)-received {
			t.Fatalf("unexpected receive completion: %+v", cqe)
		}
		received += int(cqe.Res)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("server received %q", got)
	}
	// Retain the received bytes through every send; retry only the unsent suffix.
	for sent := 0; sent < len(got); {
		if err := ring.SendFD(uring.ForFD(int32(fd)), fd, got[sent:]); err != nil {
			t.Fatalf("SendFD: %v", err)
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
		if cqe.Op() != uring.IORING_OP_SEND || cqe.FD() != fd || cqe.Res <= 0 || int(cqe.Res) > len(got)-sent {
			t.Fatalf("unexpected send completion: %+v", cqe)
		}
		sent += int(cqe.Res)
	}
	runtime.KeepAlive(got)
	clear(got)
	for received := 0; received < len(got); {
		n, err := client.Read(got[received:])
		received += n
		if err != nil && received < len(got) {
			t.Fatalf("client Read: received=%d err=%v", received, err)
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("client received %q", got)
	}
}

// Framer owns message boundaries; uring transfers the encoded bytes:
//
//	message -> Framer -> [length][payload] -> SEND -> peer's Framer -> message
//
// This finite example knows the encoded frame size. It receives that frame
// completely before decoding it, then broadcasts it with explicit short sends.
func TestBroadcastServerWithFramer(t *testing.T) {
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
	var clients [3]*sock.TCPConn
	var fds [3]iofd.FD
	for i := range clients {
		fds[i], clients[i] = tcpPair(t)
	}
	for _, payload := range [][]byte{[]byte("first message"), bytes.Repeat([]byte("x"), 300)} {
		var frame bytes.Buffer
		encoder := framer.NewWriter(&frame, framer.WithWriteTCP())
		if n, err := encoder.Write(payload); err != nil || n != len(payload) {
			t.Fatalf("encode: n=%d err=%v", n, err)
		}
		for sent := 0; sent < frame.Len(); {
			n, err := clients[0].Write(frame.Bytes()[sent:])
			if err != nil || n == 0 {
				t.Fatalf("client Write: n=%d err=%v", n, err)
			}
			sent += n
		}
		wire := make([]byte, frame.Len())
		for received := 0; received < len(wire); {
			if err := ring.ReceiveFD(uring.ForFD(int32(fds[0])), fds[0], wire[received:]); err != nil {
				t.Fatalf("ReceiveFD: %v", err)
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
			if cqe.Op() != uring.IORING_OP_RECV || cqe.FD() != fds[0] || cqe.Res <= 0 || int(cqe.Res) > len(wire)-received {
				t.Fatalf("unexpected frame completion: %+v", cqe)
			}
			received += int(cqe.Res)
		}
		var message [512]byte
		decoder := framer.NewReader(bytes.NewReader(wire), framer.WithReadTCP())
		if n, err := decoder.Read(message[:]); err != nil || !bytes.Equal(message[:n], payload) {
			t.Fatalf("decode: n=%d err=%v", n, err)
		}
		for i, fd := range fds {
			for sent := 0; sent < len(wire); {
				if err := ring.SendFD(uring.ForFD(int32(fd)), fd, wire[sent:]); err != nil {
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
				if cqe.Op() != uring.IORING_OP_SEND || cqe.FD() != fd || cqe.Res <= 0 || int(cqe.Res) > len(wire)-sent {
					t.Fatalf("unexpected broadcast completion: %+v", cqe)
				}
				sent += int(cqe.Res)
			}
		}
		runtime.KeepAlive(wire)
		for i, client := range clients {
			var received [512]byte
			decoder := framer.NewReader(client, framer.WithReadTCP())
			if n, err := decoder.Read(received[:]); err != nil || !bytes.Equal(received[:n], payload) {
				t.Fatalf("client %d: decode n=%d err=%v", i, n, err)
			}
		}
	}
}

// UDP preserves datagram boundaries. Connecting this socket selects its peer;
// each receive/send below transfers one complete ping or pong datagram.
func TestUDPPingPong(t *testing.T) {
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
	client, err := sock.ListenUDP4(&sock.UDPAddr{IP: sock.IPv4LoopBack})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("client Close: %v", err)
		}
	}()
	if err := client.SetDeadline(time.Now().Add(exampleTimeout)); err != nil {
		t.Fatal(err)
	}
	if err := ring.UDP4Socket(uring.ForFD(0)); err != nil {
		t.Fatalf("UDP4Socket: %v", err)
	}
	fd := iofd.InvalidFD
	t.Cleanup(func() {
		if err := fd.Close(); err != nil {
			t.Errorf("UDP Close: %v", err)
		}
	})
	for connected := false; !connected; {
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
		if err := cqe.Err(); err != nil {
			t.Fatalf("setup opcode %d: %v", cqe.Op(), err)
		}
		switch cqe.Op() {
		case uring.IORING_OP_SOCKET:
			fd = iofd.NewFD(int(cqe.Res))
			if err := ring.Bind(uring.ForFD(int32(fd)), &sock.UDPAddr{IP: sock.IPv4LoopBack}); err != nil {
				t.Fatalf("Bind: %v", err)
			}
		case uring.IORING_OP_BIND:
			if err := ring.Connect(uring.ForFD(int32(fd)), client.LocalAddr()); err != nil {
				t.Fatalf("Connect: %v", err)
			}
		case uring.IORING_OP_CONNECT:
			connected = true
		default:
			t.Fatalf("unexpected UDP setup opcode %d", cqe.Op())
		}
	}
	serverAddr := &sock.UDPAddr{IP: sock.IPv4LoopBack, Port: boundPort(t, fd)}
	payload := []byte("ping")
	if n, err := client.WriteTo(payload, serverAddr); err != nil || n != len(payload) {
		t.Fatalf("UDP write: n=%d err=%v", n, err)
	}
	// Ring-owned storage has a stable address through both asynchronous operations.
	// Read it after the receive CQE; reuse it only after the send CQE.
	buf := ring.RegisteredBuffer(0)[:64]
	if err := ring.ReceiveFD(uring.ForFD(int32(fd)), fd, buf); err != nil {
		t.Fatalf("ReceiveFD: %v", err)
	}
	{
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
		if cqe.Op() != uring.IORING_OP_RECV || cqe.FD() != fd || cqe.Res != int32(len(payload)) {
			t.Fatalf("unexpected UDP receive completion: %+v", cqe)
		}
	}
	if !bytes.Equal(buf[:len(payload)], payload) {
		t.Fatalf("server received %q", buf[:len(payload)])
	}
	if err := ring.SendFD(uring.ForFD(int32(fd)), fd, buf[:len(payload)]); err != nil {
		t.Fatalf("SendFD: %v", err)
	}
	{
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
		if cqe.Op() != uring.IORING_OP_SEND || cqe.FD() != fd || cqe.Res != int32(len(payload)) {
			t.Fatalf("unexpected UDP send completion: %+v", cqe)
		}
	}
	n, from, err := client.ReadFrom(buf)
	if err != nil || from.String() != serverAddr.String() || !bytes.Equal(buf[:n], payload) {
		t.Fatalf("UDP response: n=%d from=%v err=%v", n, from, err)
	}
}
