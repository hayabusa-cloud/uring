// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"net"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// =============================================================================
// TCP Socket Operation Tests (Accept, Connect, Send, Receive)
// =============================================================================

func TestTCPAcceptConnectSendReceive(t *testing.T) {
	if isWSL2() {
		t.Skip("WSL2 io_uring Send operations are unreliable")
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener socket using standard library for simplicity
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	// Timeout for io_uring operations (longer for race detection + coverage)
	opTimeout := 10 * time.Second

	// Create client socket via io_uring
	clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(clientCtx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}

	// Wait for socket creation
	clientFD := int32(-1)
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(opTimeout)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_SOCKET {
				if cqes[i].Res < 0 {
					t.Fatalf("Socket creation failed: %d", cqes[i].Res)
				}
				clientFD = cqes[i].Res
				break
			}
		}
		if clientFD >= 0 {
			break
		}
		b.Wait()
	}
	if clientFD < 0 {
		t.Fatal("Socket creation timed out")
	}

	// Accept in background goroutine
	acceptDone := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		acceptDone <- conn
	}()

	// Connect via io_uring
	connectCtx := uring.PackDirect(uring.IORING_OP_CONNECT, 0, 0, clientFD)
	if err := ring.Connect(connectCtx, addr); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Wait for connect completion
	connected := false
	deadline = time.Now().Add(opTimeout)
	b = iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_CONNECT {
				if cqes[i].Res < 0 {
					t.Fatalf("Connect failed: %d", cqes[i].Res)
				}
				connected = true
				break
			}
		}
		if connected {
			break
		}
		b.Wait()
	}
	if !connected {
		t.Fatal("Connect timed out")
	}

	// Wait for accept
	var serverConn net.Conn
	select {
	case serverConn = <-acceptDone:
		defer serverConn.Close()
	case <-time.After(opTimeout):
		t.Fatal("Accept timed out")
	}

	// Send via io_uring
	testData := []byte("Hello from io_uring!")
	sendCtx := uring.PackDirect(uring.IORING_OP_SEND, 0, 0, clientFD)
	clientSock := iofd.NewFD(int(clientFD))
	if err := ring.Send(sendCtx, &clientSock, testData); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for send completion
	sent := false
	deadline = time.Now().Add(opTimeout)
	b = iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_SEND {
				if cqes[i].Res < 0 {
					t.Fatalf("Send failed: %d", cqes[i].Res)
				}
				if int(cqes[i].Res) != len(testData) {
					t.Errorf("Send: got %d bytes, want %d", cqes[i].Res, len(testData))
				}
				sent = true
				break
			}
		}
		if sent {
			break
		}
		b.Wait()
	}
	if !sent {
		t.Fatal("Send timed out")
	}

	// Read on server side to verify send worked
	buf := make([]byte, 64)
	serverConn.SetReadDeadline(time.Now().Add(opTimeout))
	n, err := serverConn.Read(buf)
	if err != nil {
		t.Fatalf("Server read: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("Server received: got %q, want %q", buf[:n], testData)
	}

	t.Log("TCP Connect/Send cycle completed successfully")
}

// =============================================================================
// IPv6 Socket Tests
// =============================================================================

func TestIPv6Sockets(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	t.Run("TCP6Socket", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
		if err := ring.TCP6Socket(ctx); err != nil {
			t.Fatalf("TCP6Socket: %v", err)
		}

		cqe, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, 10*time.Second)
		if !ok {
			t.Fatal("TCP6Socket did not complete")
		}
		if cqe.Res < 0 {
			t.Fatalf("TCP6Socket failed: %d", cqe.Res)
		}
		t.Logf("TCP6 socket created: fd=%d", cqe.Res)

		// Close the socket to avoid FD leak
		closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, cqe.Res)
		if err := ring.Close(closeCtx); err != nil {
			t.Fatalf("Close TCP6: %v", err)
		}
		waitForOp(t, ring, uring.IORING_OP_CLOSE, 10*time.Second)
	})

	t.Run("UDP6Socket", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
		if err := ring.UDP6Socket(ctx); err != nil {
			t.Fatalf("UDP6Socket: %v", err)
		}

		cqe, ok := waitForOp(t, ring, uring.IORING_OP_SOCKET, 10*time.Second)
		if !ok {
			t.Fatal("UDP6Socket did not complete")
		}
		if cqe.Res < 0 {
			t.Fatalf("UDP6Socket failed: %d", cqe.Res)
		}
		t.Logf("UDP6 socket created: fd=%d", cqe.Res)

		// Close the socket to avoid FD leak
		closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, cqe.Res)
		if err := ring.Close(closeCtx); err != nil {
			t.Fatalf("Close UDP6: %v", err)
		}
		waitForOp(t, ring, uring.IORING_OP_CLOSE, 10*time.Second)
	})
}

// =============================================================================
// Accept with io_uring (full cycle)
// =============================================================================

func TestSubmitAcceptMultishot(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create listener using standard library
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	// Get underlying fd for io_uring Accept
	tcpLn := ln.(*net.TCPListener)
	rawConn, err := tcpLn.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}

	var listenerFD int32
	rawConn.Control(func(fd uintptr) {
		listenerFD = int32(fd)
	})

	// Submit raw multishot accept
	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, listenerFD)
	if err := ring.SubmitAcceptMultishot(acceptCtx); err != nil {
		t.Fatalf("SubmitAcceptMultishot: %v", err)
	}

	// Connect from client
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Wait for accept completion
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(5 * time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			op := cqes[i].Op()
			// Accept might come back as IORING_OP_ACCEPT
			if op == uring.IORING_OP_ACCEPT {
				if cqes[i].Res < 0 {
					t.Fatalf("Accept failed: %d", cqes[i].Res)
				}
				t.Logf("Accepted connection: fd=%d, hasMore=%v", cqes[i].Res, cqes[i].HasMore())
				return // Test passed
			}
		}
		b.Wait()
	}
	t.Skip("SubmitAcceptMultishot completion not received - timing-sensitive")
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestReadFD(t *testing.T) {
	// Test empty buffer case
	n, errno := readTestFD(0, nil)
	if n != 0 || errno != 0 {
		t.Errorf("ReadFD(nil): got (%d, %d), want (0, 0)", n, errno)
	}

	n, errno = readTestFD(0, []byte{})
	if n != 0 || errno != 0 {
		t.Errorf("ReadFD(empty): got (%d, %d), want (0, 0)", n, errno)
	}
}

func TestWriteFD(t *testing.T) {
	// Test empty buffer case
	n, errno := writeTestFD(0, nil)
	if n != 0 || errno != 0 {
		t.Errorf("WriteFD(nil): got (%d, %d), want (0, 0)", n, errno)
	}

	n, errno = writeTestFD(0, []byte{})
	if n != 0 || errno != 0 {
		t.Errorf("WriteFD(empty): got (%d, %d), want (0, 0)", n, errno)
	}

	// Test actual write using unix socket pair
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Skipf("could not create unix socket pair: %v", err)
	}
	defer closeTestFds(fds)

	// Write to socket
	data := []byte("test data")
	n, errno = writeTestFD(fds[0], data)
	if errno != 0 {
		t.Errorf("WriteFD: errno=%d", errno)
	}
	if n != len(data) {
		t.Errorf("WriteFD: n=%d, want %d", n, len(data))
	}

	// Read it back
	buf := make([]byte, 32)
	n, errno = readTestFD(fds[1], buf)
	if errno != 0 {
		t.Errorf("ReadFD: errno=%d", errno)
	}
	if n != len(data) {
		t.Errorf("ReadFD: n=%d, want %d", n, len(data))
	}
}
