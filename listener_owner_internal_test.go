// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"net"
	"strings"
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/sock"
)

type listenerOwnerTestHandler struct{}

func (listenerOwnerTestHandler) OnSocketCreated(iofd.FD) bool { return true }
func (listenerOwnerTestHandler) OnBound() bool                { return true }
func (listenerOwnerTestHandler) OnListening()                 {}
func (listenerOwnerTestHandler) OnError(uint8, error)         {}

func TestListenerManagerRootsListenerOwner(t *testing.T) {
	ring, err := New(func(opt *Options) {
		opt.LockedBufferMem = testInternalLockedBufferMem
		opt.Entries = EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	pool := NewContextPools(4)
	manager := NewListenerManager(ring, pool)

	op, err := manager.ListenTCP4(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, 128, listenerOwnerTestHandler{})
	if err != nil {
		t.Fatalf("ListenTCP4: %v", err)
	}
	defer op.Close()

	anchors := extAnchors(op.Ext())
	if anchors.owner != op {
		t.Fatalf("owner anchor: got %#v, want listener op %#v", anchors.owner, op)
	}
}

func TestListenerInitCtxNormalizesNilHandler(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer pool.PutExtended(ext)

	if err := PrepareListenerSocket(ext, AF_INET, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, nil, 128, nil); err != nil {
		t.Fatalf("PrepareListenerSocket: %v", err)
	}

	handler := listenerGetHandler(ext)
	if handler == nil {
		t.Fatal("nil handler was not normalized to NoopListenerHandler")
	}
	if _, ok := handler.(NoopListenerHandler); !ok {
		t.Fatalf("handler type: got %T, want NoopListenerHandler", handler)
	}
}

func TestPrepareListenerSocketRootsLongUnixSockaddr(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer pool.PutExtended(ext)

	sa := sock.NewSockaddrUnix("/" + strings.Repeat("a", 40))
	ptr, n := sa.Raw()
	if err := PrepareListenerSocket(ext, AF_LOCAL, SOCK_STREAM|SOCK_CLOEXEC, 0, sa, 128, listenerOwnerTestHandler{}); err != nil {
		t.Fatalf("PrepareListenerSocket: %v", err)
	}

	anchors := extAnchors(ext)
	if anchors.sockaddr != sa {
		t.Fatalf("sockaddr anchor: got %#v, want %#v", anchors.sockaddr, sa)
	}

	PrepareListenerBind(ext, 17)
	if ext.SQE.addr != uint64(uintptr(ptr)) {
		t.Fatalf("bind addr: got %#x, want %#x", ext.SQE.addr, uint64(uintptr(ptr)))
	}
	if ext.SQE.off != uint64(n) {
		t.Fatalf("bind addrlen: got %d, want %d", ext.SQE.off, n)
	}

	PrepareListenerListen(ext, 17)
	if anchors.sockaddr != nil {
		t.Fatalf("sockaddr anchor not cleared after PrepareListenerListen: %#v", anchors.sockaddr)
	}
}
