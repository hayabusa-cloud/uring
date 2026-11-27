// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func TestSendZeroCopyFixed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TCP integration test in short mode")
	}
	ring := newTestRing(t)
	checkRegisteredStream(t, ring, bytes.Repeat([]byte("registered send "), 128))
}

// singleTarget implements SendTargets for a single FD
type singleTarget struct {
	fd iofd.FD
}

func (t *singleTarget) Count() int       { return 1 }
func (t *singleTarget) FD(i int) iofd.FD { return iofd.FD(t.fd) }

func TestZeroCopyNotificationFlags(t *testing.T) {
	ring := newTestRing(t)
	fd, peer := newTestUDPPair(t)
	ctx := uring.ForFD(int32(fd))
	id := ctx.WithOp(uring.IORING_OP_SEND_ZC)

	// An oversized IPv4 datagram fails at execution. MORE still promises a notification.
	accepted, err := ring.MulticastZeroCopy(ctx, &singleTarget{fd: fd}, 0, 0, 1<<16)
	if accepted != 1 || err != nil {
		t.Fatalf("accepted %d targets, error %v", accepted, err)
	}
	if res := waitRegisteredSends(t, ring, id)[id]; res != -int32(uring.EMSGSIZE) {
		t.Fatalf("data result %d, want -EMSGSIZE", res)
	}

	// The failed send has released the slot. Send a valid datagram with the same identity.
	checkRegisteredDatagrams(t, ring, fd, peer, uring.IORING_OP_SEND_ZC,
		bytes.Repeat([]byte("Z"), 2048))
}

func TestSendZeroCopyFixedMultiTarget(t *testing.T) {
	ring := newTestRing(t)
	var fds [4]iofd.FD
	var peers [len(fds)]*sock.UDPConn
	for i := range fds {
		if i == 1 {
			fds[i] = -1
			continue
		}
		fds[i], peers[i] = newTestUDPPair(t)
	}
	payload := bytes.Repeat([]byte("multicast payload "), 128)
	copy(ring.RegisteredBuffer(0), payload)
	accepted, err := ring.MulticastZeroCopy(uring.ForFD(0), &multiTargets{fds: fds[:]}, 0, 0, len(payload))
	if accepted != len(fds) || err != nil {
		t.Fatalf("accepted %d of %d targets, error %v", accepted, len(fds), err)
	}
	if _, err := ring.Wait(nil); err != nil {
		t.Fatal(err)
	}
	var ids [len(fds)]uring.SQEContext
	for i, fd := range fds {
		ids[i] = uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, 0, int32(fd))
		if peers[i] != nil {
			requireDatagram(t, peers[i], payload)
		}
	}
	results := waitRegisteredSends(t, ring, ids[:]...)
	for i, id := range ids {
		want := int32(len(payload))
		if peers[i] == nil {
			want = -int32(zcall.EBADF)
		}
		if res := results[id]; res != want {
			t.Fatalf("target %d: data result %d, want %d", i, res, want)
		}
	}
}

type multiTargets struct {
	fds []iofd.FD
}

func (t *multiTargets) Count() int {
	return len(t.fds)
}

func (t *multiTargets) FD(i int) iofd.FD {
	if i < 0 || i >= len(t.fds) {
		return -1
	}
	return t.fds[i]
}

func TestZeroCopyBufferSafety(t *testing.T) {
	ring := newTestRing(t)
	checkRegisteredStream(t, ring, bytes.Repeat([]byte("A"), 2048), bytes.Repeat([]byte("B"), 2048))
}

func TestZeroCopyLargeBuffer(t *testing.T) {
	ring := newTestRing(t)
	payload := make([]byte, len(ring.RegisteredBuffer(0)))
	if len(payload) == 0 {
		t.Fatal("missing registered buffer")
	}
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	checkRegisteredStream(t, ring, payload)
}

// For two outstanding sends to the same FD, only the slot differs in user_data.
func TestZeroCopyMultipleBuffers(t *testing.T) {
	t.Run("SEND", func(t *testing.T) {
		ring := newTestRing(t, testTwoRegisteredBuffersOptions)
		fd, peer := newTestUDPPair(t)
		checkRegisteredDatagrams(t, ring, fd, peer, uring.IORING_OP_SEND,
			[]byte("first slot"), []byte("second slot"))
	})
	t.Run("SEND_ZC", func(t *testing.T) {
		ring := newTestRing(t, testTwoRegisteredBuffersOptions)
		fd, peer := newTestUDPPair(t)
		checkRegisteredDatagrams(t, ring, fd, peer, uring.IORING_OP_SEND_ZC,
			bytes.Repeat([]byte("A"), 2048), bytes.Repeat([]byte("B"), 2049))
	})
}

func TestZeroCopyErrorHandling(t *testing.T) {
	for _, name := range []string{"invalid_fd", "closed_socket"} {
		t.Run(name, func(t *testing.T) {
			ring := newTestRing(t)
			fd := iofd.FD(-1)
			if name == "closed_socket" {
				fds, err := newUnixSocketPairForTest()
				if err != nil {
					t.Fatal(err)
				}
				fd = fds[1]
				closeTestFds(fds)
			}
			ctx := uring.ForFD(int32(fd))
			accepted, err := ring.MulticastZeroCopy(ctx, &singleTarget{fd: fd}, 0, 0, 2048)
			if accepted != 1 || err != nil {
				t.Fatalf("accepted %d targets, error %v", accepted, err)
			}
			id := ctx.WithOp(uring.IORING_OP_SEND_ZC)
			if res := waitRegisteredSends(t, ring, id)[id]; res != -int32(zcall.EBADF) {
				t.Fatalf("data result %d, want -EBADF", res)
			}
		})
	}
}

func TestZeroCopySequentialSends(t *testing.T) {
	ring := newTestRing(t)
	fd, peer := newTestUDPPair(t)
	for seq := range 5 {
		payload := bytes.Repeat([]byte{byte('0' + seq)}, 2048)
		// Each call drains this send before the next overwrite of slot 0.
		checkRegisteredDatagrams(t, ring, fd, peer, uring.IORING_OP_SEND_ZC, payload)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func newTestUDPPair(t *testing.T) (iofd.FD, *sock.UDPConn) {
	t.Helper()
	peer, err := sock.ListenUDP4(&sock.UDPAddr{IP: sock.IPv4LoopBack})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	sender, err := sock.DialUDP4(nil, peer.LocalAddr().(*sock.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sender.Close() })
	return iofd.FD(sender.FD().Raw()), peer
}

func checkRegisteredDatagrams(t *testing.T, ring *uring.Uring, fd iofd.FD, peer *sock.UDPConn, op uint8, payloads ...[]byte) {
	t.Helper()
	var ids []uring.SQEContext
	for slot, payload := range payloads {
		offset := slot + 1
		copy(ring.RegisteredBuffer(slot)[offset:], payload)
		// Both paths replace the caller's group with the selected slot.
		ctx := uring.PackDirect(0, 0, 99, 0)
		accepted, err := ring.MulticastZeroCopy(ctx, &singleTarget{fd: fd}, slot, int64(offset), len(payload))
		if accepted != 1 || err != nil {
			t.Fatalf("slot %d: accepted %d targets, error %v", slot, accepted, err)
		}
		ids = append(ids, uring.PackDirect(op, 0, uint16(slot), int32(fd)))

		// Read before sending the next datagram. Keep every CQE queued for dispatch.
		if _, err := ring.Wait(nil); err != nil {
			t.Fatal(err)
		}
		requireDatagram(t, peer, payload)
	}
	results := waitRegisteredSends(t, ring, ids...)
	for slot, id := range ids {
		if res := results[id]; res != int32(len(payloads[slot])) {
			t.Fatalf("slot %d: data result %d, want %d", slot, res, len(payloads[slot]))
		}
	}
}

func requireDatagram(t *testing.T, peer *sock.UDPConn, want []byte) {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(want)+1)
	n, err := peer.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("unexpected datagram: %d bytes, prefix %x", n, buf[:min(n, 8)])
	}
}

func checkRegisteredStream(t *testing.T, ring *uring.Uring, messages ...[]byte) {
	t.Helper()
	fd, peer := newTestTCPPair(t)
	want := bytes.Join(messages, nil)
	got := make([]byte, len(want))
	if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(peer, got)
		read <- err
	}()

	ctx := uring.ForFD(int32(fd))
	for _, payload := range messages {
		// The previous use of this slot has released every byte before this copy.
		if n := copy(ring.RegisteredBuffer(0), payload); n != len(payload) {
			t.Fatalf("copied %d of %d bytes into the registered slot", n, len(payload))
		}
		for sent := 0; sent < len(payload); {
			left := len(payload) - sent
			accepted, err := ring.MulticastZeroCopy(ctx, &singleTarget{fd: fd}, 0, int64(sent), left)
			if accepted != 1 || err != nil {
				t.Fatalf("offset %d: accepted %d targets, error %v", sent, accepted, err)
			}
			id := ctx.WithOp(uring.IORING_OP_SEND_ZC)
			if left < 1536 { // A short suffix can select ordinary SEND for a single target.
				id = ctx.WithOp(uring.IORING_OP_SEND)
			}
			res := waitRegisteredSends(t, ring, id)[id]
			if res <= 0 || int(res) > left {
				t.Fatalf("offset %d: data result %d for %d remaining bytes", sent, res, left)
			}
			sent += int(res)
		}
	}
	if err := <-read; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("TCP peer received different bytes for %d-byte stream", len(want))
	}
}

// Return the signed data results after every send has released its range.
func waitRegisteredSends(t *testing.T, ring *uring.Uring, ids ...uring.SQEContext) map[uring.SQEContext]int32 {
	t.Helper()
	pending := make(map[uring.SQEContext]bool, len(ids)) // value records an early notification
	results := make(map[uring.SQEContext]int32, len(ids))
	for _, id := range ids {
		pending[id] = false
	}
	for len(pending) > 0 {
		cqe := waitForCQEs(t, ring, 1)[0]
		id := cqe.Context()
		notified, live := pending[id]
		_, haveData := results[id]
		switch {
		case !live:
			t.Fatalf("unexpected send completion: context %#x, result %d", id, cqe.Res)
		case cqe.IsNotification():
			if id.Op() != uring.IORING_OP_SEND_ZC || notified {
				t.Fatalf("context %#x: unexpected notification", id)
			}
			if haveData {
				delete(pending, id)
			} else {
				pending[id] = true
			}
		default:
			if haveData || id.Op() == uring.IORING_OP_SEND && cqe.HasMore() {
				t.Fatalf("context %#x: unexpected data completion %+v", id, cqe)
			}
			results[id] = cqe.Res
			if !cqe.HasMore() || notified {
				delete(pending, id)
			}
		}
	}
	return results
}
