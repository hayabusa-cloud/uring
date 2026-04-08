// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"errors"
	"io"
	"net"
	"sync"
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

const exampleNetworkTimeout = 2 * time.Second

// setNonblock sets or clears O_NONBLOCK on a raw file descriptor via zcall.
func setNonblock(fd uintptr, nonblock bool) error {
	flags, errno := zcall.Syscall4(iofd.SYS_FCNTL, fd, iofd.F_GETFL, 0, 0)
	if errno != 0 {
		return zcall.Errno(errno)
	}
	if nonblock {
		flags |= iofd.O_NONBLOCK
	} else {
		flags &^= iofd.O_NONBLOCK
	}
	_, errno = zcall.Syscall4(iofd.SYS_FCNTL, fd, iofd.F_SETFL, flags, 0)
	if errno != 0 {
		return zcall.Errno(errno)
	}
	return nil
}

func waitDone(t *testing.T, done <-chan struct{}, timeout time.Duration, label string) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitWaitGroup(t *testing.T, wg *sync.WaitGroup, timeout time.Duration, label string) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	waitDone(t, done, timeout, label)
}

// TestTCPEchoServer demonstrates a TCP echo server using io_uring.
//
// Architecture:
//   - Server uses io_uring for socket creation, recv, send
//   - Server uses syscall accept (io_uring accept has issues in WSL2)
//   - Client uses standard net.Dial for simplicity
//
// This shows a practical pattern: io_uring server with standard clients.
// Note: io_uring's IORING_OP_ACCEPT doesn't work reliably in WSL2,
// so we use syscall accept for the accept step.
func TestTCPEchoServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TCP echo server test in short mode (requires reliable networking)")
	}
	// Create io_uring instance with minimal buffer options
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Helper to wait for specific operation
	waitForOp := func(op uint8, timeout time.Duration) (uring.CQEView, bool) {
		cqes := make([]uring.CQEView, 16)
		deadline := time.Now().Add(timeout)
		b := iox.Backoff{}
		for time.Now().Before(deadline) {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				t.Logf("Wait error: %v", err)
				return uring.CQEView{}, false
			}
			for j := 0; j < n; j++ {
				t.Logf("CQE: op=%d, res=%d, flags=%d", cqes[j].Op(), cqes[j].Res, cqes[j].Flags)
				if cqes[j].Op() == op {
					return cqes[j], true
				}
			}
			b.Wait()
		}
		return uring.CQEView{}, false
	}

	// Step 1: Create server socket via io_uring
	serverCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(serverCtx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}
	ev, ok := waitForOp(uring.IORING_OP_SOCKET, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Socket creation failed: %d", ev.Res)
	}
	serverFD := ev.Res
	t.Logf("Server socket: fd=%d", serverFD)

	// Step 2: Bind to ephemeral port
	bindAddr := &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0}
	bindCtx := uring.PackDirect(uring.IORING_OP_BIND, 0, 0, serverFD)
	if err := ring.Bind(bindCtx, bindAddr); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	ev, ok = waitForOp(uring.IORING_OP_BIND, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Bind failed: %d", ev.Res)
	}
	t.Logf("Server bound")

	// Get the actual bound port using getsockname syscall
	var boundAddr [16]byte
	addrLen := uint32(16)
	errno := zcall.Getsockname(uintptr(serverFD), unsafe.Pointer(&boundAddr), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}
	// Port is at bytes 2-3 in network byte order (big-endian)
	port := int(boundAddr[2])<<8 | int(boundAddr[3])
	t.Logf("Server port: %d", port)

	// Step 3: Listen
	listenCtx := uring.PackDirect(uring.IORING_OP_LISTEN, 0, 0, serverFD)
	if err := ring.Listen(listenCtx); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ev, ok = waitForOp(uring.IORING_OP_LISTEN, time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Listen failed: %d", ev.Res)
	}
	t.Logf("Server listening on port %d", port)

	// Step 4: Start client goroutine (will connect after accept is ready)
	message := []byte("Hello, io_uring!")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond) // Let accept be ready
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), exampleNetworkTimeout)
		if err != nil {
			t.Errorf("Client dial: %v", err)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(exampleNetworkTimeout))

		// Send message
		if _, err := conn.Write(message); err != nil {
			t.Errorf("Client write: %v", err)
			return
		}
		t.Logf("Client sent: %q", string(message))

		// Read echo
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Errorf("Client read: %v", err)
			return
		}
		t.Logf("Client received echo: %q", string(buf[:n]))

		if string(buf[:n]) != string(message) {
			t.Errorf("Echo mismatch: got %q, want %q", string(buf[:n]), string(message))
		}
	}()

	// Step 5: Accept using non-blocking listen socket (zcall bypasses Go's
	// entersyscall, so a blocking accept would starve other goroutines)
	if err := setNonblock(uintptr(serverFD), true); err != nil {
		t.Fatalf("SetNonblock: %v", err)
	}
	t.Log("Waiting for connection with syscall accept...")
	var connFD uintptr
	{
		deadline := time.Now().Add(exampleNetworkTimeout)
		ab := iox.Backoff{}
		for time.Now().Before(deadline) {
			fd, e := zcall.Accept4(uintptr(serverFD), nil, nil, 0)
			if e == 0 {
				connFD = fd
				break
			}
			if zcall.Errno(e) != zcall.EAGAIN {
				t.Fatalf("Accept4: errno=%d", e)
			}
			ab.Wait()
		}
		if connFD == 0 {
			t.Fatal("Accept did not complete within timeout")
		}
	}
	t.Logf("Accepted connection: fd=%d", connFD)

	// Step 6: Receive and send data using syscall

	// Wait for client data to arrive
	time.Sleep(100 * time.Millisecond)

	// Read using syscall
	recvBuf := make([]byte, 1024)
	n, errno := zcall.Read(connFD, recvBuf)
	if errno != 0 {
		t.Fatalf("Read: errno=%d", errno)
	}
	received := int(n)
	t.Logf("Server received: %q (%d bytes)", string(recvBuf[:received]), received)

	// Echo back using syscall
	_, errno = zcall.Write(connFD, recvBuf[:received])
	if errno != 0 {
		t.Fatalf("Write: errno=%d", errno)
	}
	t.Logf("Server sent echo: %d bytes", received)

	// Wait for client to finish
	waitWaitGroup(t, &wg, exampleNetworkTimeout, "TCP echo client")

	// Cleanup: close connections using syscall (io_uring close has issues with syscall-created fds in WSL2)
	zcall.Close(connFD)

	// Close server socket via io_uring
	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, serverFD)
	if err := ring.Close(closeCtx); err != nil {
		t.Logf("Close submit: %v", err)
	}
	ev, ok = waitForOp(uring.IORING_OP_CLOSE, time.Second)
	if ok && ev.Res < 0 {
		t.Logf("Close result: %d", ev.Res)
	}

	t.Log("TCP Echo test passed")
}

// TestUringSocketWithSyscallAccept tests io_uring socket creation with syscall accept.
// This verifies that the socket created via io_uring is usable.
func TestUringSocketWithSyscallAccept(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TCP socket test in short mode (requires reliable networking)")
	}
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	cqes := make([]uring.CQEView, 16)

	// Create socket
	ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(ctx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}
	b := iox.Backoff{}
	for {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			break
		}
		b.Wait()
	}
	serverFD := int(cqes[0].Res)
	t.Logf("Socket created: fd=%d", serverFD)

	// Bind using raw syscall
	var sa [16]byte
	sa[0] = 2 // AF_INET
	// Port 0 in network byte order (let kernel choose)
	// sa[2], sa[3] = 0, 0
	// 127.0.0.1
	sa[4], sa[5], sa[6], sa[7] = 127, 0, 0, 1

	errno := zcall.Bind(uintptr(serverFD), unsafe.Pointer(&sa[0]), 16)
	if errno != 0 {
		t.Fatalf("Bind syscall: errno=%d", errno)
	}

	// Get port
	var addrLen uint32 = 16
	errno = zcall.Getsockname(uintptr(serverFD), unsafe.Pointer(&sa[0]), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}
	port := int(sa[2])<<8 | int(sa[3])
	t.Logf("Bound to port %d", port)

	// Listen using raw syscall
	errno = zcall.Listen(uintptr(serverFD), 16)
	if errno != 0 {
		t.Fatalf("Listen syscall: errno=%d", errno)
	}
	t.Log("Listening")

	// Client goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(10 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), exampleNetworkTimeout)
		if err != nil {
			t.Errorf("Client dial: %v", err)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(exampleNetworkTimeout))
		conn.Write([]byte("hello"))
		t.Log("Client sent data")
	}()

	// Accept using raw syscall (non-blocking via SetNonblock on listen socket;
	// zcall bypasses Go's entersyscall so blocking accept starves goroutines)
	if err := setNonblock(uintptr(serverFD), true); err != nil {
		t.Fatalf("SetNonblock: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var clientFD uintptr
	ab := iox.Backoff{}
	for time.Now().Before(deadline) {
		fd, errno := zcall.Accept4(uintptr(serverFD), nil, nil, zcall.SOCK_NONBLOCK)
		if errno == 0 {
			clientFD = fd
			break
		}
		if zcall.Errno(errno) != zcall.EAGAIN {
			t.Fatalf("Accept4: errno=%d", errno)
		}
		ab.Wait()
	}
	if clientFD == 0 {
		t.Fatal("Accept did not complete")
	}
	t.Logf("Accepted: fd=%d", clientFD)

	waitDone(t, done, exampleNetworkTimeout, "hybrid TCP client")
	t.Log("Hybrid test passed - io_uring socket works with syscall accept")
}

// TestSimpleTCPAccept verifies that accept works with standard syscalls.
// This is a sanity check to ensure the socket setup is correct.
func TestSimpleTCPAccept(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	t.Logf("Listening on %s", addr)

	// Client connects
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := net.DialTimeout("tcp", addr, exampleNetworkTimeout)
		if err != nil {
			t.Errorf("Client dial: %v", err)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(exampleNetworkTimeout))
		conn.Write([]byte("hello"))
	}()

	// Accept
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()
	t.Logf("Accepted connection from %s", conn.RemoteAddr())

	waitDone(t, done, exampleNetworkTimeout, "simple TCP client")
	t.Log("Simple TCP Accept test passed")
}

// itoa converts int to string without fmt package.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestBroadcastServerWithFramer demonstrates a message broadcast server using
// io_uring for socket setup and framer for message boundary preservation.
//
// Architecture:
//   - Server uses io_uring for socket/bind/listen
//   - Multiple clients connect and send framed messages
//   - Server reads from each client, then broadcasts to all
//   - framer ensures message boundaries are preserved over TCP
func TestBroadcastServerWithFramer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping broadcast server test in short mode (requires reliable networking)")
	}
	// Create io_uring instance
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	cqes := make([]uring.CQEView, 16)

	// Wait helper for CQE
	waitCQE := func(timeout time.Duration) (*uring.CQEView, bool) {
		deadline := time.Now().Add(timeout)
		b := iox.Backoff{}
		for time.Now().Before(deadline) {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				return nil, false
			}
			if n > 0 {
				return &cqes[0], true
			}
			b.Wait()
		}
		return nil, false
	}

	// Step 1: Create server socket via io_uring
	ctx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.TCP4Socket(ctx); err != nil {
		t.Fatalf("TCP4Socket: %v", err)
	}
	ev, ok := waitCQE(time.Second)
	if !ok || ev.Res < 0 {
		t.Fatal("Socket creation failed")
	}
	serverFD := ev.Res
	t.Logf("Server socket: fd=%d", serverFD)

	// Step 2: Bind via io_uring
	bindAddr := &sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0}
	bindCtx := uring.PackDirect(uring.IORING_OP_BIND, 0, 0, serverFD)
	if err := ring.Bind(bindCtx, bindAddr); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	ev, ok = waitCQE(time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Bind failed: %d", ev.Res)
	}

	// Get bound port
	var boundAddr [16]byte
	addrLen := uint32(16)
	errno := zcall.Getsockname(uintptr(serverFD), unsafe.Pointer(&boundAddr), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}
	port := int(boundAddr[2])<<8 | int(boundAddr[3])
	t.Logf("Server port: %d", port)

	// Step 3: Listen via io_uring
	listenCtx := uring.PackDirect(uring.IORING_OP_LISTEN, 0, 0, serverFD)
	if err := ring.Listen(listenCtx); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ev, ok = waitCQE(time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Listen failed: %d", ev.Res)
	}
	t.Log("Server listening")

	// Start clients: each sends a unique ID, waits for broadcast
	const numClients = 3
	clientDone := make(chan int, numClients) // reports client ID when done
	serverReady := make(chan struct{})       // signals clients to send

	var clientWg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		clientWg.Add(1)
		go func(clientID int) {
			defer clientWg.Done()

			// Connect to server
			conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), exampleNetworkTimeout)
			if err != nil {
				t.Errorf("Client %d dial: %v", clientID, err)
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(exampleNetworkTimeout))

			// Create framed reader/writer
			rw := framer.NewReadWriter(conn, conn,
				framer.WithReadTCP(),
				framer.WithWriteTCP())

			// Wait for server to accept all connections
			<-serverReady

			// Send client ID as message
			msg := []byte("client:" + itoa(clientID))
			if _, err := rw.Write(msg); err != nil {
				t.Errorf("Client %d write: %v", clientID, err)
				return
			}
			t.Logf("Client %d sent: %q", clientID, string(msg))

			// Read broadcast response
			buf := make([]byte, 256)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := rw.Read(buf)
			if err != nil {
				if err != io.EOF {
					t.Errorf("Client %d read: %v", clientID, err)
				}
				return
			}
			t.Logf("Client %d received: %q", clientID, string(buf[:n]))
			clientDone <- clientID
		}(i)
	}

	// Accept all connections (non-blocking listen socket to avoid zcall goroutine starvation)
	if err := setNonblock(uintptr(serverFD), true); err != nil {
		t.Fatalf("SetNonblock: %v", err)
	}
	var clients []uintptr
	for i := 0; i < numClients; i++ {
		deadline := time.Now().Add(exampleNetworkTimeout)
		ab := iox.Backoff{}
		var clientFD uintptr
		for time.Now().Before(deadline) {
			fd, e := zcall.Accept4(uintptr(serverFD), nil, nil, 0)
			if e == 0 {
				clientFD = fd
				break
			}
			if zcall.Errno(e) != zcall.EAGAIN {
				t.Fatalf("Accept %d: errno=%d", i, e)
			}
			ab.Wait()
		}
		if clientFD == 0 {
			t.Fatalf("Accept %d did not complete within timeout", i)
		}
		clients = append(clients, clientFD)
		t.Logf("Accepted connection %d: fd=%d", i, clientFD)
	}

	// Signal clients they can send
	close(serverReady)

	// Wait a bit for messages to arrive
	time.Sleep(100 * time.Millisecond)

	// Read from all clients, collect messages
	var messages []string
	for i, clientFD := range clients {
		reader := framer.NewReader(&fdReader{fd: clientFD}, framer.WithReadTCP())
		buf := make([]byte, 256)
		n, err := reader.Read(buf)
		if err != nil {
			t.Logf("Server read from %d: %v", i, err)
			continue
		}
		msg := string(buf[:n])
		t.Logf("Server received from conn %d: %q", i, msg)
		messages = append(messages, msg)
	}

	// Broadcast a combined message to all clients
	broadcastMsg := "broadcast:" + itoa(len(messages)) + " clients"
	for i, clientFD := range clients {
		writer := framer.NewWriter(&fdWriter{fd: clientFD}, framer.WithWriteTCP())
		if _, err := writer.Write([]byte(broadcastMsg)); err != nil {
			t.Errorf("Server write to conn %d: %v", i, err)
		}
	}
	t.Logf("Server broadcast: %q", broadcastMsg)

	// Close all client connections from server side
	for _, clientFD := range clients {
		zcall.Close(clientFD)
	}

	// Wait for clients to finish
	waitWaitGroup(t, &clientWg, exampleNetworkTimeout, "broadcast clients")

	// Count successful clients
	close(clientDone)
	count := 0
	for range clientDone {
		count++
	}
	t.Logf("Clients completed: %d/%d", count, numClients)

	if count != numClients {
		t.Errorf("Expected %d clients to complete, got %d", numClients, count)
	}

	// Cleanup server socket
	zcall.Close(uintptr(serverFD))
	t.Log("Broadcast test passed")
}

// TestUDPPingPong demonstrates UDP communication using io_uring for socket setup.
//
// Architecture:
//   - Server uses io_uring for socket creation and binding
//   - Client uses io_uring for socket creation
//   - Data exchange uses syscall sendto/recvfrom (io_uring recv/send have WSL2 issues)
//   - Demonstrates simple request-response UDP pattern
func TestUDPPingPong(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UDP ping-pong test in short mode (requires reliable networking)")
	}
	// Create io_uring instance
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	cqes := make([]uring.CQEView, 16)

	// Wait helper for CQE
	waitCQE := func(timeout time.Duration) (*uring.CQEView, bool) {
		deadline := time.Now().Add(timeout)
		b := iox.Backoff{}
		for time.Now().Before(deadline) {
			n, err := ring.Wait(cqes)
			if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
				return nil, false
			}
			if n > 0 {
				return &cqes[0], true
			}
			b.Wait()
		}
		return nil, false
	}

	// Step 1: Create server UDP socket via io_uring
	serverCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.UDP4Socket(serverCtx); err != nil {
		t.Fatalf("UDP4Socket: %v", err)
	}
	ev, ok := waitCQE(time.Second)
	if !ok || ev.Res < 0 {
		t.Fatal("Server socket creation failed")
	}
	serverFD := uintptr(ev.Res)
	t.Logf("Server UDP socket: fd=%d", serverFD)

	// Step 2: Bind server via io_uring
	bindAddr := &sock.UDPAddr{IP: sock.IPv4LoopBack, Port: 0}
	bindCtx := uring.PackDirect(uring.IORING_OP_BIND, 0, 0, int32(serverFD))
	if err := ring.Bind(bindCtx, bindAddr); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	ev, ok = waitCQE(time.Second)
	if !ok || ev.Res < 0 {
		t.Fatalf("Bind failed: %d", ev.Res)
	}

	// Get bound port
	var boundAddr [16]byte
	addrLen := uint32(16)
	errno := zcall.Getsockname(serverFD, unsafe.Pointer(&boundAddr), unsafe.Pointer(&addrLen))
	if errno != 0 {
		t.Fatalf("Getsockname: errno=%d", errno)
	}
	port := int(boundAddr[2])<<8 | int(boundAddr[3])
	t.Logf("Server port: %d", port)

	// Step 3: Create client UDP socket via io_uring
	clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
	if err := ring.UDP4Socket(clientCtx); err != nil {
		t.Fatalf("UDP4Socket (client): %v", err)
	}
	ev, ok = waitCQE(time.Second)
	if !ok || ev.Res < 0 {
		t.Fatal("Client socket creation failed")
	}
	clientFD := uintptr(ev.Res)
	t.Logf("Client UDP socket: fd=%d", clientFD)

	// Build server address for sendto
	var serverAddr [16]byte
	serverAddr[0] = 2 // AF_INET
	serverAddr[2] = byte(port >> 8)
	serverAddr[3] = byte(port)
	serverAddr[4], serverAddr[5], serverAddr[6], serverAddr[7] = 127, 0, 0, 1

	// Ping-pong exchange
	const rounds = 5
	done := make(chan struct{})

	// Server goroutine: receive pings, send pongs
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		var fromAddr [16]byte
		var fromLen uint32 = 16

		for i := 0; i < rounds; i++ {
			// Receive ping (non-blocking to avoid zcall goroutine starvation)
			fromLen = 16
			var n, errno uintptr
			{
				deadline := time.Now().Add(exampleNetworkTimeout)
				rb := iox.Backoff{}
				for time.Now().Before(deadline) {
					n, errno = zcall.Recvfrom(serverFD, buf, zcall.MSG_DONTWAIT, unsafe.Pointer(&fromAddr), unsafe.Pointer(&fromLen))
					if errno == 0 {
						break
					}
					if zcall.Errno(errno) != zcall.EAGAIN {
						t.Errorf("Server recvfrom %d: errno=%d", i, errno)
						return
					}
					rb.Wait()
				}
			}
			if errno != 0 {
				t.Errorf("Server recvfrom %d: timed out", i)
				return
			}
			t.Logf("Server received: %q", string(buf[:n]))

			// Send pong
			pong := []byte("pong:" + string(buf[:n]))
			n, errno = zcall.Sendto(serverFD, pong, 0, unsafe.Pointer(&fromAddr), 16)
			if errno != 0 {
				t.Errorf("Server sendto %d: errno=%d", i, errno)
				return
			}
			t.Logf("Server sent: %q (%d bytes)", string(pong), n)
		}
		t.Log("Server completed all rounds")
	}()

	// Client: send pings, receive pongs
	time.Sleep(10 * time.Millisecond) // Let server start

	for i := 0; i < rounds; i++ {
		// Send ping
		ping := []byte("ping:" + itoa(i))
		n, errno := zcall.Sendto(clientFD, ping, 0, unsafe.Pointer(&serverAddr), 16)
		if errno != 0 {
			t.Fatalf("Client sendto %d: errno=%d", i, errno)
		}
		t.Logf("Client sent: %q (%d bytes)", string(ping), n)

		// Receive pong (non-blocking to avoid zcall goroutine starvation)
		buf := make([]byte, 256)
		var fromAddr [16]byte
		var fromLen uint32 = 16
		{
			deadline := time.Now().Add(exampleNetworkTimeout)
			rb := iox.Backoff{}
			for time.Now().Before(deadline) {
				n, errno = zcall.Recvfrom(clientFD, buf, zcall.MSG_DONTWAIT, unsafe.Pointer(&fromAddr), unsafe.Pointer(&fromLen))
				if errno == 0 {
					break
				}
				if zcall.Errno(errno) != zcall.EAGAIN {
					t.Fatalf("Client recvfrom %d: errno=%d", i, errno)
				}
				rb.Wait()
			}
			if errno != 0 {
				t.Fatalf("Client recvfrom %d: timed out", i)
			}
		}
		t.Logf("Client received: %q", string(buf[:n]))

		// Verify pong contains our ping
		expected := "pong:ping:" + itoa(i)
		if string(buf[:n]) != expected {
			t.Errorf("Round %d: got %q, want %q", i, string(buf[:n]), expected)
		}
	}

	// Wait for server to finish
	waitDone(t, done, exampleNetworkTimeout, "UDP ping-pong server")

	// Cleanup using io_uring close
	closeCtx := uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, int32(serverFD))
	ring.Close(closeCtx)
	waitCQE(time.Second)

	closeCtx = uring.PackDirect(uring.IORING_OP_CLOSE, 0, 0, int32(clientFD))
	ring.Close(closeCtx)
	waitCQE(time.Second)

	t.Log("UDP ping-pong test passed")
}

// fdReader wraps a file descriptor for io.Reader interface
type fdReader struct {
	fd uintptr
}

func (r *fdReader) Read(p []byte) (int, error) {
	n, errno := zcall.Read(r.fd, p)
	if errno != 0 {
		if zcall.Errno(errno) == zcall.EAGAIN {
			return 0, io.ErrNoProgress
		}
		return 0, zcall.Errno(errno)
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

// fdWriter wraps a file descriptor for io.Writer interface
type fdWriter struct {
	fd uintptr
}

func (w *fdWriter) Write(p []byte) (int, error) {
	n, errno := zcall.Write(w.fd, p)
	if errno != 0 {
		return 0, zcall.Errno(errno)
	}
	return int(n), nil
}
