// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring

import (
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/sock"
)

type extAnchorsOwnerProbe struct {
	id int
}

type extAnchorsHandlerProbe struct {
	id int
}

func (*extAnchorsHandlerProbe) OnCompleted(int32)    {}
func (*extAnchorsHandlerProbe) OnNotification(int32) {}

func waitForCollection(ch <-chan struct{}, rounds int) bool {
	for range rounds {
		runtime.GC()
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
		select {
		case <-ch:
			return true
		default:
		}
	}
	return false
}

func TestExtSQEAnchorsKeepGoRefsAliveUntilRelease(t *testing.T) {
	pool := NewContextPools(1)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	ownerCollected := make(chan struct{})
	handlerCollected := make(chan struct{})

	owner := &extAnchorsOwnerProbe{id: 1}
	handler := &extAnchorsHandlerProbe{id: 1}

	runtime.SetFinalizer(owner, func(*extAnchorsOwnerProbe) {
		close(ownerCollected)
	})
	runtime.SetFinalizer(handler, func(*extAnchorsHandlerProbe) {
		close(handlerCollected)
	})

	anchors := extAnchors(ext)
	anchors.owner = owner
	anchors.handler = handler

	owner = nil
	handler = nil

	if waitForCollection(ownerCollected, 5) {
		t.Fatal("owner collected while ExtSQE anchors were still live")
	}
	if waitForCollection(handlerCollected, 5) {
		t.Fatal("handler collected while ExtSQE anchors were still live")
	}

	pool.PutExtended(ext)
	ext = nil
	anchors = nil

	if !waitForCollection(ownerCollected, 50) {
		t.Fatal("owner was not collected after ExtSQE release")
	}
	if !waitForCollection(handlerCollected, 50) {
		t.Fatal("handler was not collected after ExtSQE release")
	}
}

func TestPutExtendedClearsSQEAndAnchors(t *testing.T) {
	pool := NewContextPools(1)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	ext.SQE.opcode = IORING_OP_RECV
	ext.SQE.fd = 17
	ext.SQE.off = 99
	ext.SQE.uflags = 0xdeadbeef
	ext.SQE.bufIndex = 7
	ext.SQE.personality = 3
	ext.SQE.spliceFdIn = 11
	anchors := extAnchors(ext)
	anchors.owner = &extAnchorsOwnerProbe{id: 1}
	anchors.handler = &extAnchorsHandlerProbe{id: 2}
	anchors.sockaddr = sock.NewSockaddrUnix("/tmp/ext-anchors.sock")

	pool.PutExtended(ext)

	ext = pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted after PutExtended")
	}

	if ext.SQE != (ioUringSqe{}) {
		t.Fatalf("SQE not reset: %#v", ext.SQE)
	}
	anchors = extAnchors(ext)
	if anchors.owner != nil {
		t.Fatalf("owner anchor not cleared: %#v", anchors.owner)
	}
	if anchors.handler != nil {
		t.Fatalf("handler anchor not cleared: %#v", anchors.handler)
	}
	if anchors.sockaddr != nil {
		t.Fatalf("sockaddr anchor not cleared: %#v", anchors.sockaddr)
	}
}

func TestResetClearsExtendedSQEAndRoots(t *testing.T) {
	pool := NewContextPools(1)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}

	ext.SQE.opcode = IORING_OP_RECV
	ext.SQE.fd = 21
	ext.UserData[0] = 0x7f

	anchors := extAnchors(ext)
	anchors.owner = &extAnchorsOwnerProbe{id: 7}
	anchors.handler = &extAnchorsHandlerProbe{id: 9}
	anchors.sockaddr = sock.NewSockaddrUnix("/tmp/ext-reset.sock")

	pool.Reset()
	ext = nil
	anchors = nil

	ext = pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted after Reset")
	}
	defer pool.PutExtended(ext)

	if ext.SQE != (ioUringSqe{}) {
		t.Fatalf("SQE not reset after Reset: %#v", ext.SQE)
	}
	if ext.UserData != [64]byte{} {
		t.Fatalf("UserData not reset after Reset: %#v", ext.UserData)
	}
	anchors = extAnchors(ext)
	if anchors.owner != nil {
		t.Fatalf("owner anchor not cleared after Reset: %#v", anchors.owner)
	}
	if anchors.handler != nil {
		t.Fatalf("handler anchor not cleared after Reset: %#v", anchors.handler)
	}
	if anchors.sockaddr != nil {
		t.Fatalf("sockaddr anchor not cleared after Reset: %#v", anchors.sockaddr)
	}
}
