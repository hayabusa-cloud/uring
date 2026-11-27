// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

func headerContext(t *testing.T, ring *uring.Uring, mode int, id uint16) uring.SQEContext {
	t.Helper()
	switch mode {
	case 0:
		return uring.PackDirect(0, 0, id, 0)
	case 1:
		sqe := ring.IndirectSQE()
		if sqe == nil {
			t.Fatal("IndirectSQE pool exhausted")
		}
		*sqe = uring.IndirectSQE{}
		t.Cleanup(func() { ring.PutIndirectSQE(sqe) })
		return uring.PackIndirect(sqe)
	default:
		sqe := ring.ExtSQE()
		if sqe == nil {
			t.Fatal("ExtSQE pool exhausted")
		}
		*sqe = uring.ExtSQE{}
		sqe.UserData[0] = 0xa5
		t.Cleanup(func() { ring.PutExtSQE(sqe) })
		return uring.PackExtended(sqe)
	}
}

func headerUDPSocket(t *testing.T, family int) (iofd.FD, sock.Sockaddr) {
	t.Helper()
	fd, errno := zcall.Socket(uintptr(family), zcall.SOCK_DGRAM|zcall.SOCK_CLOEXEC, 0)
	if errno != 0 {
		t.Fatal(zcall.Errno(errno))
	}
	t.Cleanup(func() { zcall.Close(fd) })
	var addr sock.Sockaddr = sock.NewSockaddrInet4([4]byte{127, 0, 0, 1}, 0)
	if family == zcall.AF_INET6 {
		addr = sock.NewSockaddrInet6([16]byte{15: 1}, 0, 0)
	}
	raw, size := addr.Raw()
	if errno := zcall.Bind(fd, raw, uintptr(size)); errno != 0 {
		t.Fatal(zcall.Errno(errno))
	}
	if errno := zcall.Getsockname(fd, raw, unsafe.Pointer(&size)); errno != 0 {
		t.Fatal(zcall.Errno(errno))
	}
	return iofd.FD(fd), addr
}

func TestSendMsgHeaderDatagrams(t *testing.T) {
	for _, family := range []int{zcall.AF_INET, zcall.AF_INET6} {
		for _, connected := range []bool{false, true} {
			t.Run(fmt.Sprintf("family%d/connected%t", family, connected), func(t *testing.T) {
				sender, source := headerUDPSocket(t, family)
				receiver, destination := headerUDPSocket(t, family)
				raw, size := destination.Raw()
				if connected {
					if errno := zcall.Connect(uintptr(sender), raw, uintptr(size)); errno != 0 {
						t.Fatal(zcall.Errno(errno))
					}
				}
				ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
				var requests [3]struct {
					msg uring.Msghdr
					iov [2]uring.IoVec
					buf [16]byte
				}
				var contexts [3]uring.SQEContext
				payloads := [3]string{"first packet", "", "third datagram"}
				want := make(map[uint64]int32)
				for i := range requests {
					r := &requests[i]
					n := copy(r.buf[:], payloads[i])
					if n != 0 {
						r.iov = [2]uring.IoVec{{Base: &r.buf[0], Len: 3}, {Base: &r.buf[3], Len: uint64(n - 3)}}
						r.msg.Iov, r.msg.Iovlen = &r.iov[0], 2
					}
					if !connected {
						r.msg.Name, r.msg.Namelen = (*byte)(raw), size
					}
					r.msg.Flags = -1 // Receive-only header flags must not become send flags.
					contexts[i] = headerContext(t, ring, i, uint16(i+1))
					if err := ring.SendMsgHeader(contexts[i], sender, &r.msg); err != nil {
						t.Fatal(err)
					}
					id := contexts[i].Raw()
					if contexts[i].IsDirect() {
						id = contexts[i].WithFD(sender).WithOp(uring.IORING_OP_SENDMSG).Raw()
					}
					want[id] = int32(n)
				}
				runtime.GC()
				for _, cqe := range waitForCQEs(t, ring, len(requests)) {
					id := cqe.Context().Raw()
					n, ok := want[id]
					if !ok || cqe.Res != n || cqe.Flags != 0 {
						t.Fatalf("completion %#x: %+v, expected %v", id, cqe, want)
					}
					delete(want, id)
				}
				var received [len(payloads)]string
				for i := range received {
					var buf [32]byte
					var peer sock.RawSockaddrAny
					iov := uring.IoVec{Base: &buf[0], Len: uint64(len(buf))}
					msg := uring.Msghdr{Iov: &iov, Iovlen: 1, Name: (*byte)(unsafe.Pointer(&peer)), Namelen: uint32(unsafe.Sizeof(peer))}
					n, errno := zcall.Recvmsg(uintptr(receiver), unsafe.Pointer(&msg), zcall.MSG_DONTWAIT)
					if errno != 0 {
						t.Fatalf("receive datagram: %v", zcall.Errno(errno))
					}
					received[i] = string(buf[:n])
					sourceRaw, sourceSize := source.Raw()
					if msg.Namelen != sourceSize || !bytes.Equal(unsafe.Slice(msg.Name, msg.Namelen), unsafe.Slice((*byte)(sourceRaw), sourceSize)) {
						t.Fatal("datagram source address changed")
					}
				}
				// Independent sends can reach the peer in any order.
				slices.Sort(received[:])
				slices.Sort(payloads[:])
				if received != payloads {
					t.Fatalf("received datagrams %q, want %q", received, payloads)
				}
				if contexts[2].ExtSQE().UserData[0] != 0xa5 {
					t.Fatal("extended user data changed")
				}
				runtime.KeepAlive(&requests)
			})
		}
	}
}

func TestSendMsgHeaderRejectionPreservesContexts(t *testing.T) {
	for mode := range 3 {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			ctx := headerContext(t, ring, mode, 99).WithOp(uring.IORING_OP_READ).WithFD(17)
			var indirect uring.IndirectSQE
			var extended uring.ExtSQE
			if mode == 1 {
				indirect = *ctx.IndirectSQE()
			}
			if mode == 2 {
				extended = *ctx.ExtSQE()
			}
			capacity := ring.SQAvailable()
			for i := range capacity {
				if err := ring.Nop(uring.PackDirect(uring.IORING_OP_NOP, 0, uint16(i), 0)); err != nil {
					t.Fatal(err)
				}
			}
			msg := uring.Msghdr{Flags: 7}
			if err := ring.SendMsgHeader(ctx, -1, &msg); !iox.IsWouldBlock(err) {
				t.Fatalf("full SQ: %v", err)
			}
			if ring.SQAvailable() != 0 || msg != (uring.Msghdr{Flags: 7}) || ctx.Op() != uring.IORING_OP_READ || ctx.FD() != 17 {
				t.Fatal("rejection changed caller storage or SQ capacity")
			}
			if mode == 1 && *ctx.IndirectSQE() != indirect || mode == 2 && *ctx.ExtSQE() != extended {
				t.Fatal("rejection mutated pointer-backed context")
			}
			for _, cqe := range waitForCQEs(t, ring, capacity) {
				if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
					t.Fatalf("rejected send was published: %+v", cqe)
				}
			}
			if err := ring.SendMsgHeader(ctx, -1, &msg); err != nil {
				t.Fatal(err)
			}
			cqe := waitForCQEs(t, ring, 1)[0]
			want := ctx
			if ctx.IsDirect() {
				want = ctx.WithFD(-1).WithOp(uring.IORING_OP_SENDMSG)
			}
			if cqe.Context() != want || cqe.Op() != uring.IORING_OP_SENDMSG || cqe.Context().FD() != -1 || cqe.Res != -int32(zcall.EBADF) {
				t.Fatalf("retried send = %+v, want EBADF for %#x", cqe, want.Raw())
			}
		})
	}
}

func TestSendMsgHeaderFixedFileOptions(t *testing.T) {
	sender, _ := headerUDPSocket(t, zcall.AF_INET)
	receiver, destination := headerUDPSocket(t, zcall.AF_INET)
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	if err := ring.RegisterFiles([]int32{int32(sender)}); err != nil {
		t.Fatal(err)
	}
	defer ring.UnregisterFiles()
	raw, size := destination.Raw()
	msg := uring.Msghdr{Name: (*byte)(raw), Namelen: size, Flags: -1}
	ctx := headerContext(t, ring, 2, 0).WithFlags(uring.IOSQE_FIXED_FILE)
	if err := ring.SendMsgHeader(ctx, 0, &msg,
		uring.WithFlags(uring.IOSQE_IO_LINK), uring.WithFlags(uring.IOSQE_ASYNC),
		uring.WithIOPrio(uring.IORING_RECVSEND_POLL_FIRST),
		uring.WithOffset(-1), uring.WithN(-1), uring.WithPersonality(65535)); err != nil {
		t.Fatal(err)
	}
	cqe := waitForCQEs(t, ring, 1)[0]
	if cqe.Res != 0 || cqe.Context() != ctx || cqe.Context().Flags() != uring.IOSQE_FIXED_FILE|uring.IOSQE_ASYNC {
		t.Fatalf("fixed send completion: %+v", cqe)
	}
	var buf [1]byte
	iov := uring.IoVec{Base: &buf[0], Len: 1}
	recv := uring.Msghdr{Iov: &iov, Iovlen: 1}
	if n, errno := zcall.Recvmsg(uintptr(receiver), unsafe.Pointer(&recv), zcall.MSG_DONTWAIT); n != 0 || errno != 0 {
		t.Fatalf("empty fixed-file datagram: %d, errno %d", n, errno)
	}
	runtime.KeepAlive(&msg)
}

func TestSendMsgHeaderAncillary(t *testing.T) {
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	payload := []byte("rights")
	control := sock.UnixRights(int(fds[0]))
	sendIov := uring.IoVec{Base: &payload[0], Len: uint64(len(payload))}
	send := uring.Msghdr{Iov: &sendIov, Iovlen: 1, Control: &control[0], Controllen: uint64(len(control))}
	buf := make([]byte, len(payload))
	oob := make([]byte, len(control))
	recvIov := uring.IoVec{Base: &buf[0], Len: uint64(len(buf))}
	recv := uring.Msghdr{Iov: &recvIov, Iovlen: 1, Control: &oob[0], Controllen: uint64(len(oob))}
	sendCtx := headerContext(t, ring, 1, 0)
	recvCtx := headerContext(t, ring, 2, 0)
	if err := ring.RecvMsg(recvCtx, &fds[1], &recv); err != nil {
		t.Fatal(err)
	}
	if err := ring.SendMsgHeader(sendCtx, fds[0], &send); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	requireTerminalResults(t, ring, map[uring.SQEContext]int32{sendCtx: int32(len(payload)), recvCtx: int32(len(payload))})
	if !bytes.Equal(buf, payload) || recv.Flags != 0 {
		t.Fatalf("received %q, flags %#x", buf, recv.Flags)
	}
	received := sock.ParseUnixRights(oob[:recv.Controllen])
	for _, fd := range received {
		defer zcall.Close(uintptr(fd))
	}
	if len(received) != 1 {
		t.Fatalf("received %d descriptors", len(received))
	}
	if n, errno := zcall.Write(uintptr(received[0]), []byte("x")); n != 1 || errno != 0 {
		t.Fatalf("passed descriptor write: %d, errno %d", n, errno)
	}
	if n, errno := zcall.Read(uintptr(fds[1]), buf[:1]); n != 1 || errno != 0 || buf[0] != 'x' {
		t.Fatalf("passed descriptor peer: %d, errno %d, %q", n, errno, buf[:1])
	}
	runtime.KeepAlive(&send)
	runtime.KeepAlive(&recv)
}

func TestSendMsgHeaderNotificationPolicy(t *testing.T) {
	for _, notify := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("notify%t/explicit%t", notify, explicit), func(t *testing.T) {
				fds, err := newUnixSocketPairForTest()
				if err != nil {
					t.Fatal(err)
				}
				defer closeTestFds(fds)
				ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = notify })
				payload := []byte("send")
				iov := uring.IoVec{Base: &payload[0], Len: uint64(len(payload))}
				msg := uring.Msghdr{Iov: &iov, Iovlen: 1}
				flags := uint8(uring.IOSQE_IO_LINK)
				if explicit {
					flags |= uring.IOSQE_CQE_SKIP_SUCCESS
				}
				ctx := uring.PackDirect(0, flags, 1, 0)
				if err := ring.SendMsgHeader(ctx, fds[0], &msg); err != nil {
					t.Fatal(err)
				}
				// A successful linked NOP proves the send finished even when its CQE is suppressed.
				if err := ring.Nop(uring.PackDirect(uring.IORING_OP_NOP, 0, 2, 0)); err != nil {
					t.Fatal(err)
				}
				count := 1
				if notify && !explicit {
					count++
				}
				seenSend, seenNop := false, false
				for _, cqe := range waitForCQEs(t, ring, count) {
					switch cqe.Op() {
					case uring.IORING_OP_SENDMSG:
						if cqe.Res != int32(len(payload)) || seenSend || !notify || explicit {
							t.Fatalf("unexpected send CQE: %+v", cqe)
						}
						seenSend = true
					case uring.IORING_OP_NOP:
						if cqe.Res != 0 || seenNop {
							t.Fatalf("linked NOP: %+v", cqe)
						}
						seenNop = true
					default:
						t.Fatalf("unexpected completion: %+v", cqe)
					}
				}
				if !seenNop || seenSend != (notify && !explicit) || ring.CQPending() != 0 {
					t.Fatal("notification policy did not preserve the linked release evidence")
				}
				var buf [4]byte
				if n, errno := zcall.Read(uintptr(fds[1]), buf[:]); errno != 0 || n != 4 || string(buf[:]) != "send" {
					t.Fatalf("received %q (%d), errno %d", buf[:], n, errno)
				}
				runtime.KeepAlive(&msg)
			})
		}
	}
}

func TestSendMsgHeaderShortStream(t *testing.T) {
	payload := bytes.Repeat([]byte("send"), 16384)
	for _, tc := range []struct {
		name       string
		sendBuffer int32
		notify     bool
		wantShort  bool
	}{
		{"short-reported", 4096, true, true},
		{"short-suppressed", 4096, false, true},
		{"full-reported", 65536, true, false},
		{"full-suppressed", 65536, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fds, err := newUnixSocketPairForTest()
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestFds(fds)
			if errno := zcall.Setsockopt(uintptr(fds[0]), zcall.SOL_SOCKET, zcall.SO_SNDBUF, unsafe.Pointer(&tc.sendBuffer), unsafe.Sizeof(tc.sendBuffer)); errno != 0 {
				t.Fatal(zcall.Errno(errno))
			}
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = tc.notify })
			iov := uring.IoVec{Base: &payload[0], Len: uint64(len(payload))}
			msg := uring.Msghdr{Iov: &iov, Iovlen: 1, Flags: zcall.MSG_WAITALL}
			send := uring.PackDirect(uring.IORING_OP_SENDMSG, uring.IOSQE_IO_LINK, 1, int32(fds[0]))
			nop := uring.PackDirect(uring.IORING_OP_NOP, 0, 2, 0)
			if err := ring.SendMsgHeader(send, fds[0], &msg); err != nil {
				t.Fatal(err)
			}
			if err := ring.Nop(nop); err != nil {
				t.Fatal(err)
			}
			wantCQEs := 1
			if tc.notify {
				wantCQEs++
			}
			reported := int32(-1)
			seenNop := false
			for _, cqe := range waitForCQEs(t, ring, wantCQEs) {
				switch cqe.Context() {
				case send:
					if !tc.notify || reported >= 0 || cqe.Res <= 0 {
						t.Fatalf("unexpected send CQE: %+v", cqe)
					}
					reported = cqe.Res
				case nop:
					if seenNop || cqe.Res != 0 || cqe.Flags != 0 {
						t.Fatalf("linked NOP: %+v", cqe)
					}
					seenNop = true
				default:
					t.Fatalf("unexpected completion: %+v", cqe)
				}
			}
			if !seenNop || ring.CQPending() != 0 {
				t.Fatal("unexpected completion count for linked send")
			}
			// Read through EOF to measure the prefix independently of the send CQE.
			if errno := zcall.Shutdown(uintptr(fds[0]), zcall.SHUT_WR); errno != 0 {
				t.Fatal(zcall.Errno(errno))
			}
			buf := make([]byte, len(payload)+1)
			n, errno := zcall.Recvfrom(uintptr(fds[1]), buf, zcall.MSG_WAITALL, nil, nil)
			if errno != 0 || n == 0 || n > uintptr(len(payload)) || !bytes.Equal(buf[:n], payload[:n]) {
				t.Fatalf("received prefix: %d bytes, errno %d", n, errno)
			}
			if short := n < uintptr(len(payload)); short != tc.wantShort || tc.notify && int32(n) != reported {
				t.Fatalf("offered %d, peer received %d, send CQE count %d", len(payload), n, reported)
			}
			t.Logf("peer received %d of %d bytes, %d CQEs", n, len(payload), wantCQEs)
			runtime.KeepAlive(&msg)
		})
	}
}

func TestSendMsgHeaderDeferredCancellation(t *testing.T) {
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	size := int32(4096)
	if errno := zcall.Setsockopt(uintptr(fds[0]), zcall.SOL_SOCKET, zcall.SO_SNDBUF, unsafe.Pointer(&size), unsafe.Sizeof(size)); errno != 0 {
		t.Fatal(zcall.Errno(errno))
	}
	payload := make([]byte, 8192)
	iov := uring.IoVec{Base: &payload[0], Len: uint64(len(payload))}
	msg := uring.Msghdr{Iov: &iov, Iovlen: 1}
	for {
		_, errno := zcall.Sendmsg(uintptr(fds[0]), unsafe.Pointer(&msg), zcall.MSG_DONTWAIT|zcall.MSG_NOSIGNAL)
		if errno == uintptr(zcall.EAGAIN) {
			break
		}
		if errno != 0 {
			t.Fatal(zcall.Errno(errno))
		}
	}
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	ctx := headerContext(t, ring, 2, 0)
	if err := ring.SendMsgHeader(ctx, fds[0], &msg); err != nil {
		t.Fatal(err)
	}
	if err := ring.Nop(uring.PackDirect(uring.IORING_OP_NOP, 0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	cqe := waitForCQEs(t, ring, 1)[0]
	if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
		t.Fatalf("send completed without peer capacity: %+v", cqe)
	}
	// The next reservation releases consumed SQ roots while the send is still pending.
	cancel := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 2, 0)
	if err := ring.AsyncCancel(cancel, ctx.Raw()); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	seenTarget, seenCancel := false, false
	for _, cqe := range waitForCQEs(t, ring, 2) {
		switch cqe.Context() {
		case ctx:
			if cqe.Res != -int32(zcall.ECANCELED) || seenTarget {
				t.Fatalf("target completion: %+v", cqe)
			}
			seenTarget = true
		case cancel:
			if cqe.Res != 0 || seenCancel {
				t.Fatalf("cancel completion: %+v", cqe)
			}
			seenCancel = true
		default:
			t.Fatalf("unexpected cancellation identity: %+v", cqe)
		}
	}
	if !seenTarget || !seenCancel || ctx.ExtSQE().UserData[0] != 0xa5 {
		t.Fatal("target and cancellation lifetimes were not retained independently")
	}
	runtime.KeepAlive(&msg)
}

func TestMessageHeaderAllocations(t *testing.T) {
	for _, receive := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for mode := range 3 {
				t.Run(fmt.Sprintf("receive%t/shared%t/mode%d", receive, shared, mode), func(t *testing.T) {
					if !shared {
						runtime.LockOSThread()
						t.Cleanup(runtime.UnlockOSThread)
					}
					ring := newTestRing(t, func(o *uring.Options) {
						o.NotifySucceed = true
						o.MultiIssuers = shared
					})
					fd := iofd.FD(-1)
					messages := make([]uring.Msghdr, ring.SQAvailable())
					contexts := make([]uring.SQEContext, len(messages))
					for i := range contexts {
						contexts[i] = headerContext(t, ring, mode, uint16(i))
					}
					for _, pass := range []string{"fresh", "reused"} {
						warmup := true
						allocs := testing.AllocsPerRun(1, func() {
							// The untimed call must not warm the slots being measured.
							if warmup {
								warmup = false
								return
							}
							for i, ctx := range contexts {
								var err error
								if receive {
									err = ring.RecvMsg(ctx, &fd, &messages[i])
								} else {
									err = ring.SendMsgHeader(ctx, fd, &messages[i])
								}
								if err != nil {
									t.Fatal(err)
								}
							}
						})
						for _, cqe := range waitForCQEs(t, ring, len(messages)) {
							if cqe.Res != -int32(zcall.EBADF) {
								t.Fatalf("invalid descriptor completion: %+v", cqe)
							}
						}
						if allocs != 0 {
							t.Errorf("%s slots: %g allocations, want zero", pass, allocs)
						}
					}
					runtime.KeepAlive(messages)
				})
			}
		}
	}
}

func TestRecvMsgRightsControlLength(t *testing.T) {
	pair, err := sock.UnixConnPair("unixgram")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pair[0].Close()
		pair[1].Close()
	})
	tx, rx := pair[0].FD(), pair[1].FD()
	one := sock.UnixRights(tx.Fd())
	two := sock.UnixRights(tx.Fd(), tx.Fd())
	three := sock.UnixRights(tx.Fd(), tx.Fd(), tx.Fd())
	var send uring.Msghdr
	control := make([]byte, sock.CmsgSpace(3*4))
	payload := byte(0xa5)
	iov := uring.IoVec{Base: &payload, Len: 1}
	recv := uring.Msghdr{Iov: &iov, Iovlen: 1, Control: &control[0]}
	var pin runtime.Pinner
	t.Cleanup(pin.Unpin)
	for _, p := range []any{&send, &one[0], &two[0], &three[0], &recv, &iov, &payload, &control[0]} {
		pin.Pin(p)
	}
	ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
	sendID := uring.ForFD(tx.Raw()).WithOp(uring.IORING_OP_SENDMSG)
	recvID := uring.ForFD(rx.Raw()).WithOp(uring.IORING_OP_RECVMSG)

	exact, padded := sock.CmsgLen(4), sock.CmsgSpace(4)
	for _, tc := range []struct {
		rights        []byte
		offered       int
		wantBytes     int
		wantFDs       int
		wantTruncated bool
	}{
		{three, len(control), len(control), 3, false},
		{three, exact, exact, 1, true},
		{three, padded, padded, 2, true},
		{one, len(control), padded, 1, false},
		{two, len(control), padded, 2, false},
		{three, 0, 0, 0, true},
		{nil, len(control), 0, 0, false},
	} {
		send.Control = unsafe.SliceData(tc.rights)
		send.Controllen = uint64(len(tc.rights))
		recv.Controllen = uint64(tc.offered)
		if err := ring.SendMsgHeader(sendID, *tx, &send); err != nil {
			t.Fatal(err)
		}
		if err := ring.RecvMsg(recvID, rx, &recv); err != nil {
			t.Fatal(err)
		}
		requireTerminalResults(t, ring, map[uring.SQEContext]int32{sendID: 0, recvID: 0})

		installed := sock.ParseUnixRights(control[:recv.Controllen])
		for _, fd := range installed {
			if errno := zcall.Close(uintptr(fd)); errno != 0 {
				t.Errorf("close received fd %d: %v", fd, zcall.Errno(errno))
			}
		}
		if recv.Controllen != uint64(tc.wantBytes) {
			t.Fatalf("offered %d control bytes: returned %d, want %d", tc.offered, recv.Controllen, tc.wantBytes)
		}
		if payload != 0xa5 {
			t.Fatalf("empty datagram changed the payload byte to %#x", payload)
		}
		truncated := recv.Flags&sock.MSG_CTRUNC != 0
		if len(installed) != tc.wantFDs || truncated != tc.wantTruncated {
			t.Fatalf("offered %d control bytes: received %d fds, MSG_CTRUNC=%t, want %d fds, MSG_CTRUNC=%t",
				tc.offered, len(installed), truncated, tc.wantFDs, tc.wantTruncated)
		}
	}
}
