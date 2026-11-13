// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/zcall"
)

const exampleTimeout = 3 * time.Second

func tempFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("Close temporary file: %v", err)
		}
	})
	return f
}

// createPipe transfers ownership of both nonblocking pipe ends to the caller.
// Keep them open until their submitted operations have completed.
func createPipe() (r, w iofd.FD, err error) {
	var fds [2]int32
	if errno := zcall.Pipe2(&fds, zcall.O_NONBLOCK|zcall.O_CLOEXEC); errno != 0 {
		return iofd.InvalidFD, iofd.InvalidFD, zcall.Errno(errno)
	}
	return iofd.NewFD(int(fds[0])), iofd.NewFD(int(fds[1])), nil
}

// These fixtures create ordinary TCP peers, without submitting uring work.
// Returned descriptor values are borrowed; the sock object owns Close until
// cleanup. Finish all operations using the descriptor before that cleanup.
func tcpListener(t *testing.T) (iofd.FD, *sock.TCPAddr) {
	t.Helper()
	listener, err := sock.ListenTCP4(&sock.TCPAddr{IP: sock.IPv4LoopBack})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("listener Close: %v", err)
		}
	})
	return iofd.FD(listener.FD().Raw()), listener.Addr().(*sock.TCPAddr)
}

func tcpPair(t *testing.T) (iofd.FD, *sock.TCPConn) {
	t.Helper()
	listener, err := sock.ListenTCP4(&sock.TCPAddr{IP: sock.IPv4LoopBack})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("pair listener Close: %v", err)
		}
	}()
	peer := dialTCP(t, listener.Addr().(*sock.TCPAddr))
	if err := listener.SetDeadline(time.Now().Add(exampleTimeout)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("pair connection Close: %v", err)
		}
	})
	return iofd.FD(conn.FD().Raw()), peer
}

func dialTCP(t *testing.T, addr *sock.TCPAddr) *sock.TCPConn {
	t.Helper()
	dialer := sock.TCPDialer{Timeout: exampleTimeout}
	peer, err := dialer.Dial4(nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := peer.Close(); err != nil {
			t.Errorf("peer Close: %v", err)
		}
	})
	// A deadline enables sock's adaptive backoff; no outer retry loop is needed.
	if err := peer.SetDeadline(time.Now().Add(exampleTimeout)); err != nil {
		t.Fatal(err)
	}
	return peer
}
