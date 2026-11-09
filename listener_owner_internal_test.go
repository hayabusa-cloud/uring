// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"math"
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
	ring, err := New(testMinimalBufferOptions)
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

func TestPrepareListenerSocketRejectsInvalidIntDomains(t *testing.T) {
	pool := NewContextPools(1)
	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer pool.PutExtended(ext)

	tests := []struct {
		name    string
		domain  int
		sockTyp int
		proto   int
		backlog int
	}{
		{name: "negative domain", domain: -1, sockTyp: SOCK_STREAM | SOCK_CLOEXEC, proto: IPPROTO_TCP, backlog: 128},
		{name: "oversized domain", domain: math.MaxUint16 + 1, sockTyp: SOCK_STREAM | SOCK_CLOEXEC, proto: IPPROTO_TCP, backlog: 128},
		{name: "negative socket type", domain: AF_INET, sockTyp: -1, proto: IPPROTO_TCP, backlog: 128},
		{name: "oversized socket type", domain: AF_INET, sockTyp: oversizedSignedInt32(t), proto: IPPROTO_TCP, backlog: 128},
		{name: "negative proto", domain: AF_INET, sockTyp: SOCK_STREAM | SOCK_CLOEXEC, proto: -1, backlog: 128},
		{name: "oversized proto", domain: AF_INET, sockTyp: SOCK_STREAM | SOCK_CLOEXEC, proto: oversizedSignedInt32(t), backlog: 128},
		{name: "negative backlog", domain: AF_INET, sockTyp: SOCK_STREAM | SOCK_CLOEXEC, proto: IPPROTO_TCP, backlog: -1},
		{name: "oversized backlog", domain: AF_INET, sockTyp: SOCK_STREAM | SOCK_CLOEXEC, proto: IPPROTO_TCP, backlog: oversizedSignedInt32(t)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PrepareListenerSocket(ext, tt.domain, tt.sockTyp, tt.proto, nil, tt.backlog, nil)
			if !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("PrepareListenerSocket error = %v, want %v", err, ErrInvalidParam)
			}
		})
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
