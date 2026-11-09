// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iox"
)

func newStartedSharedTestRing(t *testing.T) *Uring {
	t.Helper()

	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.MultiIssuers = true
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	return ring
}

func newStartedTwoRegisteredBuffersSharedTestRing(t *testing.T) *Uring {
	t.Helper()

	ring, err := New(testMinimalBufferOptions, testTwoRegisteredBuffersOptions, func(opt *Options) {
		opt.MultiIssuers = true
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)
	return ring
}

func waitForNopCQE(t *testing.T, ring *Uring) {
	t.Helper()

	cqes := make([]CQEView, 4)
	deadline := time.Now().Add(time.Second)
	backoff := iox.Backoff{}
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if errors.Is(err, iox.ErrWouldBlock) {
			backoff.Wait()
			continue
		}
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			if cqes[i].Op() == IORING_OP_NOP {
				return
			}
		}
		backoff.Wait()
	}
	t.Fatal("NOP completion timeout")
}

func submitNopAfterInvalidInput(t *testing.T, ring *Uring, id uint16, label string) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, id, 0))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Nop after %s: %v", label, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Nop blocked after %s", label)
	}

	waitForNopCQE(t, ring)
}

func TestPublicInputDomainRejectsInvalidLengthAndIndex(t *testing.T) {
	t.Run("read negative WithN", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Read(PackDirect(0, 0, 0, 0), []byte{1}, WithN(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Read error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x10, "negative Read WithN")
	})

	t.Run("write negative WithN", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Write(PackDirect(0, 0, 0, 0), []byte{1}, WithN(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Write error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x11, "negative Write WithN")
	})

	t.Run("read negative WithOffset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Read(PackDirect(0, 0, 0, 0), []byte{1}, WithOffset(-2)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Read error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2d, "Read offset below current-position sentinel")
	})

	t.Run("write negative WithOffset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Write(PackDirect(0, 0, 0, 0), []byte{1}, WithOffset(-2)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Write error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2e, "Write offset below current-position sentinel")
	})

	t.Run("send negative WithN", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Send(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 1}, []byte{1}, WithN(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Send error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x12, "negative Send WithN")
	})

	t.Run("send negative WithOffset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Send(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 1}, []byte{1}, WithOffset(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Send error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x31, "negative Send WithOffset")
	})

	t.Run("receive negative WithN", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Receive(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 2}, []byte{1}, WithN(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Receive error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x13, "negative Receive WithN")
	})

	t.Run("receive negative WithOffset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Receive(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 2}, []byte{1}, WithOffset(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Receive error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x32, "negative Receive WithOffset")
	})

	t.Run("receive multishot caller buffer", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.SubmitReceiveMultishot(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 2}, []byte{1}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("SubmitReceiveMultishot error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x33, "SubmitReceiveMultishot caller buffer")
	})

	t.Run("receive buffer select zero size", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Receive(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 3}, nil, WithReadBufferSize(0)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Receive buffer-select error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x14, "zero Receive buffer size")
	})

	t.Run("receive buffer select oversized size", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Receive(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 3}, nil, WithReadBufferSize(oversizedSignedInt32(t))); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Receive buffer-select error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x25, "oversized Receive buffer size")
	})

	t.Run("receive bundle negative size", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ReceiveBundle(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 4}, WithReadBufferSize(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReceiveBundle error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x15, "negative ReceiveBundle size")
	})

	t.Run("receive bundle oversized size", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ReceiveBundle(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 4}, WithReadBufferSize(oversizedSignedInt32(t))); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReceiveBundle error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x26, "oversized ReceiveBundle size")
	})

	t.Run("readFixed public invalid buffer index", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if _, err := ring.ReadFixed(PackDirect(0, 0, 0, 0), -1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReadFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x16, "invalid public ReadFixed buffer index")
	})

	t.Run("readFixed negative WithN", func(t *testing.T) {
		ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
		if _, err := ring.ReadFixed(PackDirect(0, 0, 0, 0), 0, WithN(-1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReadFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x17, "negative ReadFixed WithN")
	})

	t.Run("readFixed negative WithOffset", func(t *testing.T) {
		ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
		if _, err := ring.ReadFixed(PackDirect(0, 0, 0, 0), 0, WithOffset(-2)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReadFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2f, "ReadFixed offset below current-position sentinel")
	})

	t.Run("writeFixed negative WithOffset", func(t *testing.T) {
		ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
		if err := ring.WriteFixed(PackDirect(0, 0, 0, 0), 0, 1, WithOffset(-2)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("WriteFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x30, "WriteFixed offset below current-position sentinel")
	})

	t.Run("internal read buffer select oversized length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.readWithBufferSelect(PackDirect(0, 0, 0, 0), oversizedLen32(t), 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("readWithBufferSelect error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x27, "oversized readWithBufferSelect length")
	})

	t.Run("splice negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Splice(PackDirect(0, 0, 0, 0), 0, -1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Splice error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x18, "negative Splice length")
	})

	t.Run("splice oversized length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Splice(PackDirect(0, 0, 0, 0), 0, oversizedLen32(t)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Splice error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x28, "oversized Splice length")
	})

	t.Run("tee negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Tee(PackDirect(0, 0, 0, 0), 0, -1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Tee error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x19, "negative Tee length")
	})

	t.Run("tee oversized length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Tee(PackDirect(0, 0, 0, 0), 0, oversizedLen32(t)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Tee error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x29, "oversized Tee length")
	})

	t.Run("receive zero-copy negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 5}, -1, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReceiveZeroCopy error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x1a, "negative ReceiveZeroCopy length")
	})

	t.Run("receive zero-copy oversized length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ReceiveZeroCopy(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 5}, oversizedLen32(t), 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReceiveZeroCopy error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2a, "oversized ReceiveZeroCopy length")
	})

	t.Run("writevFixed iovec outside selected buffer", func(t *testing.T) {
		ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
		iovs := [][]byte{ring.RegisteredBuffer(1)[:1]}
		if err := ring.WritevFixed(PackDirect(0, 0, 0, 0), 0, 0, iovs); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("WritevFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x1b, "WritevFixed cross-buffer iovec")
	})

	t.Run("readvFixed oversized vector count", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		if err := ring.ReadvFixed(PackDirect(0, 0, 0, 0), 0, 0, make([][]byte, maxIovecs+1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReadvFixed error = %v, want %v", err, ErrInvalidParam)
		}
		if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
			t.Fatalf("SQ tail = %d, want %d", got, tail)
		}
		submitNopAfterInvalidInput(t, ring, 0x34, "oversized ReadvFixed vector count")
	})

	t.Run("writevFixed oversized vector count", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		if err := ring.WritevFixed(PackDirect(0, 0, 0, 0), 0, 0, make([][]byte, maxIovecs+1)); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("WritevFixed error = %v, want %v", err, ErrInvalidParam)
		}
		if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
			t.Fatalf("SQ tail = %d, want %d", got, tail)
		}
		submitNopAfterInvalidInput(t, ring, 0x35, "oversized WritevFixed vector count")
	})

	t.Run("readvFixed negative offset", func(t *testing.T) {
		ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
		if err := ring.ReadvFixed(PackDirect(0, 0, 0, 0), -2, 0, [][]byte{ring.RegisteredBuffer(0)[:1]}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReadvFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x31, "ReadvFixed offset below current-position sentinel")
	})

	t.Run("writevFixed negative offset", func(t *testing.T) {
		ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
		if err := ring.WritevFixed(PackDirect(0, 0, 0, 0), -2, 0, [][]byte{ring.RegisteredBuffer(0)[:1]}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("WritevFixed error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x32, "WritevFixed offset below current-position sentinel")
	})

	t.Run("internal send zero-copy negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.sendZeroCopy(PackDirect(0, 0, 0, 0), []byte{1}, 0, -1, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("sendZeroCopy error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x1c, "negative sendZeroCopy length")
	})

	t.Run("internal sendto zero-copy negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.sendtoZeroCopy(PackDirect(0, 0, 0, 0), []byte{1}, 0, -1, nil, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("sendtoZeroCopy error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x1d, "negative sendtoZeroCopy length")
	})

	t.Run("file advise negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.FileAdvise(PackDirect(0, 0, 0, 0), 0, -1, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FileAdvise error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x1e, "negative FileAdvise length")
	})

	t.Run("file advise oversized length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.FileAdvise(PackDirect(0, 0, 0, 0), 0, oversizedLen32(t), 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FileAdvise error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2b, "oversized FileAdvise length")
	})

	t.Run("file advise negative offset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.FileAdvise(PackDirect(0, 0, 0, 0), -1, 1, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FileAdvise error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x22, "negative FileAdvise offset")
	})

	t.Run("sync file range negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.SyncFileRange(PackDirect(0, 0, 0, 0), 0, -1, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("SyncFileRange error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x1f, "negative SyncFileRange length")
	})

	t.Run("sync file range oversized length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.SyncFileRange(PackDirect(0, 0, 0, 0), 0, oversizedLen32(t), 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("SyncFileRange error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2c, "oversized SyncFileRange length")
	})

	t.Run("sync file range negative offset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.SyncFileRange(PackDirect(0, 0, 0, 0), -1, 1, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("SyncFileRange error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x23, "negative SyncFileRange offset")
	})

	t.Run("fallocate negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Fallocate(PackDirect(0, 0, 0, 0), 0, 0, -1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Fallocate error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x20, "negative Fallocate length")
	})

	t.Run("fallocate zero length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Fallocate(PackDirect(0, 0, 0, 0), 0, 0, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Fallocate error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x2d, "zero Fallocate length")
	})

	t.Run("fallocate negative offset", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.Fallocate(PackDirect(0, 0, 0, 0), 0, -1, 1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Fallocate error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x24, "negative Fallocate offset")
	})

	t.Run("ftruncate negative length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.FTruncate(PackDirect(0, 0, 0, 0), -1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("FTruncate error = %v, want %v", err, ErrInvalidParam)
		}
		submitNopAfterInvalidInput(t, ring, 0x21, "negative FTruncate length")
	})
}

func TestSignedKernelCarrierDomainsRejectOutOfRange(t *testing.T) {
	oversized := oversizedSignedInt32(t)
	payload := mappedTestSlice[byte](t, oversized)
	events := mappedTestSlice[EpollEvent](t, oversized)
	ctx := PackDirect(0, 0, 0, 7)

	tests := []struct {
		name string
		call func(*ioUring) error
	}{
		{name: "send length", call: func(ur *ioUring) error { return ur.send(ctx, 0, payload, 0, len(payload)) }},
		{name: "receive length", call: func(ur *ioUring) error { return ur.receive(ctx, 0, payload, 0, len(payload)) }},
		{name: "receive buffer-select length", call: func(ur *ioUring) error { return ur.receiveWithBufferSelect(ctx, 0, oversized, 1) }},
		{name: "receive bundle length", call: func(ur *ioUring) error { return ur.receiveBundle(ctx, 0, oversized, 1) }},
		{name: "send zero-copy length", call: func(ur *ioUring) error { return ur.sendZeroCopy(ctx, payload, 0, len(payload), 0) }},
		{name: "sendto zero-copy length", call: func(ur *ioUring) error { return ur.sendtoZeroCopy(ctx, payload, 0, len(payload), nil, 0) }},
		{name: "epoll maxevents", call: func(ur *ioUring) error { return ur.epollWait(ctx, events, 0) }},
		{name: "socket domain", call: func(ur *ioUring) error { return ur.socket(ctx, oversized, SOCK_STREAM, 0) }},
		{name: "socket type", call: func(ur *ioUring) error { return ur.socket(ctx, AF_INET, oversized, 0) }},
		{name: "socket protocol", call: func(ur *ioUring) error { return ur.socket(ctx, AF_INET, SOCK_STREAM, oversized) }},
		{name: "socket direct protocol", call: func(ur *ioUring) error { return ur.socketDirect(ctx, AF_INET, SOCK_STREAM, oversized, 0) }},
		{name: "listen backlog", call: func(ur *ioUring) error { return ur.listen(ctx, oversized) }},
		{name: "shutdown mode", call: func(ur *ioUring) error { return ur.shutdown(ctx, SHUT_RDWR+1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperTestRing(t)
			tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
			if err := tt.call(ring.ioUring); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
			}
			if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
				t.Fatalf("SQ tail = %d, want %d", got, tail)
			}
		})
	}

}

func TestSharedSubmitInvalidInputDoesNotPoisonRing(t *testing.T) {
	t.Run("invalid openat pathname", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		ctx := PackDirect(0, 0, 0, AT_FDCWD)
		if err := ring.OpenAt(ctx, "bad\x00path", 0, 0); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("OpenAt error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 1, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid OpenAt: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid OpenAt")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid readvFixed buffer index", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.readvFixed(PackDirect(0, 0, 0, 0), 0, -1, [][]byte{{1}}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("readvFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 2, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid readvFixed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid readvFixed")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid readvFixed empty buffer list", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.readvFixed(PackDirect(0, 0, 0, 0), 0, 0, nil); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("readvFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 3, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after empty readvFixed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after empty readvFixed")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid writevFixed empty iovec", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if err := ring.ioUring.writevFixed(PackDirect(0, 0, 0, 0), 0, 0, [][]byte{{}}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("writevFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 4, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid writevFixed empty iovec: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid writevFixed empty iovec")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid writevFixed external iovec", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		buf := ring.RegisteredBuffer(0)
		if buf == nil {
			t.Fatal("RegisteredBuffer(0) = nil")
		}
		if err := ring.ioUring.writevFixed(PackDirect(0, 0, 0, 0), 0, 0, [][]byte{make([]byte, 1)}); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("writevFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 5, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid writevFixed external iovec: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid writevFixed external iovec")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid readFixed buffer index", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		if _, err := ring.ioUring.readFixed(PackDirect(0, 0, 0, 0), -1, 0, 8); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("readFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 6, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid readFixed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid readFixed")
		}

		waitForNopCQE(t, ring)
	})

	t.Run("invalid writeFixed length", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		buf := ring.RegisteredBuffer(0)
		if buf == nil {
			t.Fatal("RegisteredBuffer(0) = nil")
		}
		if err := ring.ioUring.writeFixed(PackDirect(0, 0, 0, 0), 0, 0, len(buf)+1); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("writeFixed error = %v, want %v", err, ErrInvalidParam)
		}

		done := make(chan error, 1)
		go func() {
			done <- ring.Nop(PackDirect(IORING_OP_NOP, 0, 7, 0))
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Nop after invalid writeFixed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Nop blocked after invalid writeFixed")
		}

		waitForNopCQE(t, ring)
	})
}

func TestMultiIssuerSubmitAndWaitCanOverlap(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	const submits = 128
	waitCtx := PackDirect(IORING_OP_NOP, 0, 0x77, 0)

	var submitWG sync.WaitGroup
	submitWG.Add(1)
	submitErr := make(chan error, 1)
	go func() {
		defer submitWG.Done()
		backoff := iox.Backoff{}
		for range submits {
			for {
				err := ring.Nop(waitCtx)
				if errors.Is(err, iox.ErrWouldBlock) {
					backoff.Wait()
					continue
				}
				if err != nil {
					submitErr <- err
					return
				}
				break
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	got := 0
	cqes := make([]CQEView, 8)
	backoff := iox.Backoff{}
	for got < submits && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if errors.Is(err, iox.ErrWouldBlock) {
			select {
			case err := <-submitErr:
				t.Fatalf("submit goroutine: %v", err)
			default:
			}
			backoff.Wait()
			continue
		}
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		got += n
	}

	submitWG.Wait()
	select {
	case err := <-submitErr:
		t.Fatalf("submit goroutine: %v", err)
	default:
	}
	if got != submits {
		t.Fatalf("completed CQEs = %d, want %d", got, submits)
	}
}

func TestSubmitKeepAliveRetainsBindAndXattrRoots(t *testing.T) {
	t.Run("bind retains sockaddr root", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
		sa := AddrToSockaddr(addr)
		rawPtr, _, err := sockaddr(sa)
		if err != nil {
			t.Fatalf("sockaddr: %v", err)
		}
		if err := ring.ioUring.bind(PackDirect(0, 0, 0, 0), sa); err != nil {
			t.Fatalf("bind submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		kept := ring.ioUring.submit.keepAlive[idx].sockaddrArg
		if kept == nil {
			t.Fatal("bind keepAlive sockaddrArg = nil")
		}
		keptPtr, _, err := sockaddr(kept)
		if err != nil {
			t.Fatalf("sockaddr kept: %v", err)
		}
		if keptPtr != rawPtr {
			t.Fatal("bind keepAlive did not retain the submitted sockaddr root")
		}
	})

	t.Run("xattr retains value root", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		value := []byte("hello-xattr")
		if err := ring.ioUring.fsetxattr(PackDirect(0, 0, 0, 0), "user.test", value, 0); err != nil {
			t.Fatalf("fsetxattr submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		kept := ring.ioUring.submit.keepAlive[idx].bytes1
		if len(kept) != len(value) {
			t.Fatalf("xattr keepAlive len = %d, want %d", len(kept), len(value))
		}
		if unsafe.SliceData(kept) != unsafe.SliceData(value) {
			t.Fatal("xattr keepAlive did not retain the caller value root")
		}
	})

	t.Run("uringCmd keeps original command buffer visible", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		cmdData := []byte("uring-cmd")
		cmdCtx := PackDirect(0, IOSQE_IO_LINK, 0, 17)
		if err := ring.ioUring.uringCmd(cmdCtx, 99, cmdData, 0); err != nil {
			t.Fatalf("uringCmd submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		if got, want := sqe.addr, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(cmdData)))); got != want {
			t.Fatalf("uringCmd sqe.addr = %#x, want %#x", got, want)
		}

		keep := &ring.ioUring.submit.keepAlive[idx]
		if !keep.active {
			t.Fatal("uringCmd keepAlive not active")
		}
		kept := keep.bytes1
		if len(kept) != len(cmdData) {
			t.Fatalf("uringCmd keepAlive len = %d, want %d", len(kept), len(cmdData))
		}
		if unsafe.SliceData(kept) != unsafe.SliceData(cmdData) {
			t.Fatal("uringCmd keepAlive did not retain the original caller buffer")
		}
	})

	t.Run("uringCmd128 stages inline command bytes", func(t *testing.T) {
		ring, err := New(testMinimalBufferOptions, func(opt *Options) {
			opt.MultiIssuers = true
			opt.NotifySucceed = true
			opt.SQE128 = true
		})
		if errors.Is(err, ErrNotSupported) {
			t.Skip("SQE128 ring setup not supported on this kernel")
		}
		if err != nil {
			t.Fatalf("New SQE128 ring: %v", err)
		}
		mustStartRing(t, ring)

		cmdData := []byte("wide-inline-command")
		cmdCtx := PackDirect(0, IOSQE_IO_LINK, 0, 17)
		if err := ring.ioUring.uringCmd128(cmdCtx, 123, cmdData, 0); errors.Is(err, ErrNotSupported) {
			t.Skip("URING_CMD128 opcode not supported on this kernel")
		} else if err != nil {
			t.Fatalf("uringCmd128 submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		sqe128 := ring.ioUring.sq.sqe128At(idx)
		if sqe128 == nil {
			t.Fatal("wide ring did not expose 128-byte SQE view")
		}
		if sqe.opcode != IORING_OP_URING_CMD128 {
			t.Fatalf("uringCmd128 opcode = %d, want %d", sqe.opcode, IORING_OP_URING_CMD128)
		}
		if sqe.off != 123 {
			t.Fatalf("uringCmd128 off = %d, want %d", sqe.off, 123)
		}
		inline := unsafe.Slice((*byte)(unsafe.Pointer(&sqe128.pad[0])), len(cmdData))
		if got := string(inline); got != string(cmdData) {
			t.Fatalf("uringCmd128 inline data = %q, want %q", got, string(cmdData))
		}
		if keep := &ring.ioUring.submit.keepAlive[idx]; keep.active {
			t.Fatal("uringCmd128 should not retain external command data")
		}
	})
}

func TestRecvMsgUsesCallerOwnedCompletionStorage(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 2)
	ctx := PackDirect(0, IOSQE_ASYNC, 0, 17)
	msg1 := Msghdr{}

	if err := ring.ioUring.recvmsg(ctx, 3, &msg1); err != nil {
		t.Fatalf("first recvmsg: %v", err)
	}
	sqe1 := ring.ioUring.sq.sqeAt(0)
	if got, want := sqe1.addr, uint64(uintptr(unsafe.Pointer(&msg1))); got != want {
		t.Fatalf("first SQE addr = %#x, want %#x", got, want)
	}
	if got, want := sqe1.userData, ctx.WithOp(IORING_OP_RECVMSG).Raw(); got != want {
		t.Fatalf("first SQE user_data = %#x, want %#x", got, want)
	}
	ext1 := ring.ioUring.submit.keepAlive[0].ext
	if ext1 == nil || ext1.recvMsg != &msg1 {
		t.Fatal("first submit did not retain the caller header")
	}

	atomic.StoreUint32(ring.ioUring.sq.kHead, 1)
	ring.ioUring.lockSubmitState()
	ring.ioUring.releaseConsumedKeepAlive()
	ring.ioUring.unlockSubmitState()
	if ring.ioUring.submit.keepAlive[0].active {
		t.Fatal("consumed first SQ slot remained active")
	}
	if ext1.recvMsg != nil {
		t.Fatal("recycled submit scratch retained the first caller header")
	}

	msg2 := Msghdr{}
	if err := ring.ioUring.recvmsg(ctx, 4, &msg2); err != nil {
		t.Fatalf("second recvmsg: %v", err)
	}
	sqe2 := ring.ioUring.sq.sqeAt(1)
	if got, want := sqe2.addr, uint64(uintptr(unsafe.Pointer(&msg2))); got != want {
		t.Fatalf("second SQE addr = %#x, want %#x", got, want)
	}
	if sqe1.addr == sqe2.addr {
		t.Fatal("independent recvmsg submissions share completion storage")
	}
	ext2 := ring.ioUring.submit.keepAlive[1].ext
	if ext2 != ext1 {
		t.Fatal("submit scratch was not reused by the regression witness")
	}
	if ext2.recvMsg != &msg2 {
		t.Fatal("reused submit scratch did not retain the second caller header")
	}

	msg1.Controllen = 9
	if msg2.Controllen != 0 {
		t.Fatal("first completion metadata aliases the second caller header")
	}
}

func TestRecvMsgRejectsInvalidCallerHeaderBeforeSQMutation(t *testing.T) {
	tests := []struct {
		name string
		msg  *Msghdr
	}{
		{name: "nil header"},
		{name: "oversized iovlen", msg: &Msghdr{Iov: new(IoVec), Iovlen: maxIovecs + 1}},
		{name: "missing iovec", msg: &Msghdr{Iovlen: 1}},
		{name: "missing control", msg: &Msghdr{Controllen: 1}},
		{name: "missing name", msg: &Msghdr{Namelen: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 1)
			if err := ring.ioUring.recvmsg(PackDirect(0, 0, 0, 3), 0, tt.msg); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("recvmsg error = %v, want %v", err, ErrInvalidParam)
			}
			if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != 0 {
				t.Fatalf("SQ tail = %d, want 0", got)
			}
			if keep := &ring.ioUring.submit.keepAlive[0]; keep.active || keep.ext != nil {
				t.Fatal("invalid caller header reached submit staging")
			}
		})
	}
}

func TestSubmitKeepAliveRetainsLinkTimeoutTimespec(t *testing.T) {
	t.Run("linkTimeout retains timespec root", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		ts := &Timespec{Sec: 1, Nsec: 2}
		ctx := PackDirect(0, IOSQE_IO_LINK, 0, 0)
		if err := ring.ioUring.linkTimeout(ctx, ts, IORING_TIMEOUT_ABS); err != nil {
			t.Fatalf("linkTimeout submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		keep := &ring.ioUring.submit.keepAlive[idx]
		if sqe.opcode != IORING_OP_LINK_TIMEOUT {
			t.Fatalf("linkTimeout opcode = %d, want %d", sqe.opcode, IORING_OP_LINK_TIMEOUT)
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(&keep.timespec))) {
			t.Fatal("linkTimeout did not stage the keepAlive timespec root")
		}
		if keep.timespec != *ts {
			t.Fatalf("linkTimeout keepAlive timespec = %+v, want %+v", keep.timespec, *ts)
		}
		if sqe.len != 1 {
			t.Fatalf("linkTimeout len = %d, want 1", sqe.len)
		}
		if sqe.uflags != IORING_TIMEOUT_ABS {
			t.Fatalf("linkTimeout uflags = %#x, want %#x", sqe.uflags, IORING_TIMEOUT_ABS)
		}
	})

	t.Run("linkTimeout nil timespec leaves addr zero", func(t *testing.T) {
		ring := newStartedSharedTestRing(t)
		ctx := PackDirect(0, IOSQE_IO_LINK, 0, 0)
		if err := ring.ioUring.linkTimeout(ctx, nil, 0); err != nil {
			t.Fatalf("linkTimeout submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		keep := &ring.ioUring.submit.keepAlive[idx]
		if sqe.addr != 0 {
			t.Fatalf("linkTimeout addr = %#x, want 0", sqe.addr)
		}
		if keep.timespec != (Timespec{}) {
			t.Fatalf("linkTimeout keepAlive timespec = %+v, want zero value", keep.timespec)
		}
	})
}

func TestSubmitKeepAliveRetainsOpenHowAndStatxRoots(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	t.Run("openat2 retains OpenHow root", func(t *testing.T) {
		how := &OpenHow{}
		if err := ring.ioUring.openAt2(PackDirect(0, 0, 0, AT_FDCWD), "openat2-root", how); err != nil {
			t.Fatalf("openAt2 submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		ext := ring.ioUring.submit.keepAlive[idx].ext
		if ext == nil {
			t.Fatal("openAt2 keepAlive ext = nil")
		}
		if ext.openHow != how {
			t.Fatal("openAt2 keepAlive did not retain the submitted OpenHow root")
		}
	})

	t.Run("statx retains Statx root", func(t *testing.T) {
		stat := &Statx{}
		if err := ring.ioUring.statx(PackDirect(0, 0, 0, AT_FDCWD), "statx-root", 0, 0, stat); err != nil {
			t.Fatalf("statx submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		ext := ring.ioUring.submit.keepAlive[idx].ext
		if ext == nil {
			t.Fatal("statx keepAlive ext = nil")
		}
		if ext.statx != stat {
			t.Fatal("statx keepAlive did not retain the submitted Statx root")
		}
	})
}

func TestSubmitStagesFixedVectoredBuffers(t *testing.T) {
	t.Run("readvFixed stages registered buffer iovecs", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		ring.ioUring.bufs = [][]byte{make([]byte, 96), make([]byte, 128)}
		buf := ring.RegisteredBuffer(1)
		iovs := [][]byte{buf[7:23], buf[64:93]}
		ctx := PackDirect(0, 0, 0, 17)
		if err := ring.ReadvFixed(ctx, 41, 1, iovs); err != nil {
			t.Fatalf("ReadvFixed submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		keep := &ring.ioUring.submit.keepAlive[idx]

		if sqe.opcode != IORING_OP_READV_FIXED {
			t.Fatalf("readvFixed opcode = %d, want %d", sqe.opcode, IORING_OP_READV_FIXED)
		}
		if sqe.flags != ring.readLikeOpFlags {
			t.Fatalf("readvFixed flags = %#x, want %#x", sqe.flags, ring.readLikeOpFlags)
		}
		if sqe.fd != 17 {
			t.Fatalf("readvFixed fd = %d, want 17", sqe.fd)
		}
		if sqe.off != 41 {
			t.Fatalf("readvFixed off = %d, want 41", sqe.off)
		}
		if sqe.len != 2 {
			t.Fatalf("readvFixed len = %d, want 2", sqe.len)
		}
		if sqe.bufIndex != 1 {
			t.Fatalf("readvFixed bufIndex = %d, want 1", sqe.bufIndex)
		}
		if !keep.active {
			t.Fatal("readvFixed keepAlive not active")
		}
		if len(keep.iovs) != 2 {
			t.Fatalf("readvFixed iov count = %d, want 2", len(keep.iovs))
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(&keep.iovs[0]))) {
			t.Fatal("readvFixed sqe.addr does not point at staged iovecs")
		}
		if keep.iovs[0].Base != unsafe.SliceData(iovs[0]) || keep.iovs[0].Len != uint64(len(iovs[0])) {
			t.Fatal("readvFixed first iovec does not match the first selected-buffer range")
		}
		if keep.iovs[1].Base != unsafe.SliceData(iovs[1]) || keep.iovs[1].Len != uint64(len(iovs[1])) {
			t.Fatal("readvFixed second iovec does not match the second selected-buffer range")
		}
	})

	t.Run("writevFixed stages registered buffer iovecs", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		ring.ioUring.bufs = [][]byte{make([]byte, 96), make([]byte, 128)}
		buf := ring.RegisteredBuffer(1)
		iovs := [][]byte{buf[:17], buf[48:77]}
		ctx := PackDirect(0, 0, 0, 19)
		if err := ring.WritevFixed(ctx, 73, 1, iovs); err != nil {
			t.Fatalf("WritevFixed submit: %v", err)
		}

		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		idx := (tail - 1) & *ring.ioUring.sq.kRingMask
		sqe := ring.ioUring.sq.sqeAt(idx)
		keep := &ring.ioUring.submit.keepAlive[idx]

		if sqe.opcode != IORING_OP_WRITEV_FIXED {
			t.Fatalf("writevFixed opcode = %d, want %d", sqe.opcode, IORING_OP_WRITEV_FIXED)
		}
		if sqe.flags != ring.writeLikeOpFlags {
			t.Fatalf("writevFixed flags = %#x, want %#x", sqe.flags, ring.writeLikeOpFlags)
		}
		if sqe.fd != 19 {
			t.Fatalf("writevFixed fd = %d, want 19", sqe.fd)
		}
		if sqe.off != 73 {
			t.Fatalf("writevFixed off = %d, want 73", sqe.off)
		}
		if sqe.len != 2 {
			t.Fatalf("writevFixed len = %d, want 2", sqe.len)
		}
		if sqe.bufIndex != 1 {
			t.Fatalf("writevFixed bufIndex = %d, want 1", sqe.bufIndex)
		}
		if !keep.active {
			t.Fatal("writevFixed keepAlive not active")
		}
		if len(keep.iovs) != 2 {
			t.Fatalf("writevFixed iov count = %d, want 2", len(keep.iovs))
		}
		if sqe.addr != uint64(uintptr(unsafe.Pointer(&keep.iovs[0]))) {
			t.Fatal("writevFixed sqe.addr does not point at staged iovecs")
		}
		if keep.iovs[0].Base != unsafe.SliceData(iovs[0]) || keep.iovs[0].Len != uint64(len(iovs[0])) {
			t.Fatal("writevFixed first iovec does not match the first selected-buffer range")
		}
		if keep.iovs[1].Base != unsafe.SliceData(iovs[1]) || keep.iovs[1].Len != uint64(len(iovs[1])) {
			t.Fatal("writevFixed second iovec does not match the second selected-buffer range")
		}
	})

	t.Run("rejects iovec from another registered buffer before reservation", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 8)
		ring.ioUring.bufs = [][]byte{make([]byte, 32), make([]byte, 32)}
		tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
		err := ring.ReadvFixed(PackDirect(0, 0, 0, 23), 0, 0, [][]byte{ring.RegisteredBuffer(1)[:1]})
		if !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("ReadvFixed error = %v, want %v", err, ErrInvalidParam)
		}
		if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
			t.Fatalf("SQ tail = %d, want %d", got, tail)
		}
		if ring.ioUring.submit.keepAlive[0].active {
			t.Fatal("cross-buffer iovec reached submit staging")
		}
	})
}

func TestFixedVectoredInlineSubmitAllocations(t *testing.T) {
	tests := []struct {
		name string
		call func(*Uring, [][]byte) error
	}{
		{
			name: "read",
			call: func(ring *Uring, iovs [][]byte) error {
				return ring.ReadvFixed(PackDirect(0, 0, 0, 11), 0, 0, iovs)
			},
		},
		{
			name: "write",
			call: func(ring *Uring, iovs [][]byte) error {
				return ring.WritevFixed(PackDirect(0, 0, 0, 13), 0, 0, iovs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 1)
			ring.ioUring.bufs = [][]byte{make([]byte, 64)}
			buf := ring.RegisteredBuffer(0)
			iovs := [][]byte{buf[:16], buf[32:48]}

			allocs := testing.AllocsPerRun(100, func() {
				if err := tt.call(ring, iovs); err != nil {
					panic(err)
				}
				tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
				atomic.StoreUint32(ring.ioUring.sq.kHead, tail)
				ring.ioUring.releaseConsumedKeepAlive()
			})
			if allocs != 0 {
				t.Fatalf("allocations = %v, want 0", allocs)
			}
		})
	}
}

func TestOpenAtStagesLongPaths(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	path := strings.Repeat("a", submitKeepAlivePathInline+64)

	if err := ring.ioUring.openAt(PackDirect(0, 0, 0, AT_FDCWD), path, 0, 0); err != nil {
		t.Fatalf("openAt submit: %v", err)
	}

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	keep := &ring.ioUring.submit.keepAlive[idx]
	if keep.ext == nil {
		t.Fatal("long-path submit did not allocate ext staging")
	}
	if len(keep.ext.path1Buf) != len(path)+1 {
		t.Fatalf("long-path buffer len = %d, want %d", len(keep.ext.path1Buf), len(path)+1)
	}
	if keep.ext.path1Buf[len(path)] != 0 {
		t.Fatal("long-path buffer is not NUL terminated")
	}
	if got := string(keep.ext.path1Buf[:len(path)]); got != path {
		t.Fatal("long-path buffer contents do not match submitted pathname")
	}
}

func TestWritePathRejectsPathsOverPathMax(t *testing.T) {
	ext := &submitKeepAliveExt{}
	path := strings.Repeat("a", submitKeepAlivePathMax)
	if _, err := ext.writePath1(path); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("writePath1 error = %v, want %v", err, ErrNameTooLong)
	}
}

func TestEpollWaitRejectsTimeoutAndStagesZeroOff(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	events := make([]EpollEvent, 1)
	ctx := PackDirect(0, 0, 0, 0)

	if err := ring.ioUring.epollWait(ctx, events, 1); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("epollWait timeout error = %v, want %v", err, ErrInvalidParam)
	}

	if err := ring.ioUring.epollWait(ctx, events, 0); err != nil {
		t.Fatalf("epollWait submit: %v", err)
	}

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	sqe := ring.ioUring.sq.sqeAt(idx)
	keep := &ring.ioUring.submit.keepAlive[idx]
	if keep.ext == nil {
		t.Fatal("epollWait keepAlive ext = nil")
	}
	if len(keep.ext.epollWaitEvents) != len(events) {
		t.Fatalf("epollWait keepAlive events len = %d, want %d", len(keep.ext.epollWaitEvents), len(events))
	}
	if &keep.ext.epollWaitEvents[0] != &events[0] {
		t.Fatal("epollWait keepAlive did not retain the submitted events root")
	}
	if sqe.off != 0 {
		t.Fatalf("epollWait sqe.off = %d, want 0", sqe.off)
	}
	if sqe.uflags != 0 {
		t.Fatalf("epollWait sqe.uflags = %d, want 0", sqe.uflags)
	}
	if sqe.bufIndex != 0 || sqe.spliceFdIn != 0 {
		t.Fatalf("epollWait reused stale fields: bufIndex=%d spliceFdIn=%d", sqe.bufIndex, sqe.spliceFdIn)
	}
}

func TestEpollWaitRejectsOversizedEvents(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	events := mappedTestSlice[EpollEvent](t, oversizedSignedInt32(t))
	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)

	if err := ring.EpollWait(ForFD(41), events, 0); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("EpollWait oversized events error = %v, want %v", err, ErrInvalidParam)
	}
	if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
		t.Fatalf("SQ tail = %d, want %d", got, tail)
	}
}

func TestEpollWaitMirrorsExtendedSQE(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	events := make([]EpollEvent, 2)
	ext := &ExtSQE{}
	const fd int32 = 33
	ctx := PackExtended(ext).withFD(fd)

	if err := ring.ioUring.epollWait(ctx, events, 0); err != nil {
		t.Fatalf("epollWait submit: %v", err)
	}

	sqe := ViewExtSQE(ext)
	if !sqe.Valid() {
		t.Fatal("extended SQE view is invalid")
	}
	if got := sqe.Opcode(); got != IORING_OP_EPOLL_WAIT {
		t.Fatalf("extended epollWait opcode = %d, want %d", got, IORING_OP_EPOLL_WAIT)
	}
	if got := sqe.RawFD(); got != fd {
		t.Fatalf("extended epollWait fd = %d, want %d", got, fd)
	}
	wantAddr := uint64(uintptr(unsafe.Pointer(&events[0])))
	if got := sqe.Addr(); got != wantAddr {
		t.Fatalf("extended epollWait addr = %#x, want %#x", got, wantAddr)
	}
	if got := sqe.Len(); got != uint32(len(events)) {
		t.Fatalf("extended epollWait len = %d, want %d", got, len(events))
	}
	if got := sqe.UserData(); got != ctx.Raw() {
		t.Fatalf("extended epollWait userData = %#x, want %#x", got, ctx.Raw())
	}
}

func TestPublishSubmitSlotMirrorsExtendedSQE(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	ext := &ExtSQE{}
	const fd int32 = 44
	ctx := PackExtended(ext).WithOp(IORING_OP_POLL_ADD).withFD(fd)

	slot, err := ring.ioUring.reserveSubmitSlot()
	if err != nil {
		t.Fatalf("reserveSubmitSlot: %v", err)
	}
	slot.fill6(ctx, 7, 11, 22, 33, 44)
	ring.ioUring.publishSubmitSlot(slot, ctx)

	sqe := ViewExtSQE(ext)
	if !sqe.Valid() {
		t.Fatal("extended SQE view is invalid")
	}
	if got := sqe.Opcode(); got != IORING_OP_POLL_ADD {
		t.Fatalf("extended publish opcode = %d, want %d", got, IORING_OP_POLL_ADD)
	}
	if got := sqe.RawFD(); got != fd {
		t.Fatalf("extended publish fd = %d, want %d", got, fd)
	}
	if got := sqe.Addr(); got != 22 {
		t.Fatalf("extended publish addr = %d, want 22", got)
	}
	if got := sqe.Len(); got != 33 {
		t.Fatalf("extended publish len = %d, want 33", got)
	}
	if got := sqe.UserData(); got != ctx.Raw() {
		t.Fatalf("extended publish userData = %#x, want %#x", got, ctx.Raw())
	}
}

func TestCloseAfterFatalResizeClosesRingState(t *testing.T) {
	cause := errors.New("remap failed")
	ur := &ioUring{
		submit: submitState{
			keepAlive: []submitKeepAlive{{active: true, bytes1: []byte("x")}},
		},
		ringFd:   -1,
		pollerFd: -1,
		bufs:     [][]byte{{1}},
		files:    []int32{1},
		ops:      []ioUringProbeOp{{op: IORING_OP_NOP}},
		sq: ioUringSq{
			sqeStride: unsafe.Sizeof(ioUringSqe{}),
			sqeMapSz:  unsafe.Sizeof(ioUringSqe{}),
		},
	}

	err := ur.closeAfterFatalResize(cause)
	if !errors.Is(err, cause) {
		t.Fatalf("closeAfterFatalResize error = %v, want wrapped cause", err)
	}
	if !ur.closed.Load() {
		t.Fatal("closeAfterFatalResize did not mark ring closed")
	}
	if ur.submit.keepAlive != nil {
		t.Fatal("closeAfterFatalResize did not clear keepAlive state")
	}
	if ur.bufs != nil || ur.files != nil || ur.ops != nil {
		t.Fatal("closeAfterFatalResize did not clear userspace state")
	}
	if ur.sq.sqesPtr != nil || ur.sq.sqeStride != 0 || ur.sq.sqeMapSz != 0 || ur.sq.kHead != nil || ur.cq.cqes != nil {
		t.Fatal("closeAfterFatalResize did not unmap ring views")
	}
	if err := ur.enter(); !errors.Is(err, ErrClosed) {
		t.Fatalf("enter after fatal resize = %v, want %v", err, ErrClosed)
	}
}

func TestClearRingViewsClearsStaleMappings(t *testing.T) {
	ur := &ioUring{
		sq: ioUringSq{
			ringPtr:   unsafe.Pointer(new(byte)),
			sqesPtr:   unsafe.Pointer(new(ioUringSqe)),
			ringSz:    128,
			sqeStride: unsafe.Sizeof(ioUringSqe{}),
			sqeMapSz:  2 * unsafe.Sizeof(ioUringSqe{}),
			kHead:     new(uint32),
			kTail:     new(uint32),
		},
		cq: ioUringCq{
			kHead: new(uint32),
			cqes:  make([]ioUringCqe, 2),
		},
	}

	ur.clearRingViews()

	if ur.sq.ringPtr != nil || ur.sq.sqesPtr != nil || ur.sq.ringSz != 0 || ur.sq.sqeStride != 0 || ur.sq.sqeMapSz != 0 || ur.sq.kHead != nil || ur.sq.kTail != nil {
		t.Fatal("clearRingViews did not clear sq state")
	}
	if ur.cq.kHead != nil || ur.cq.kTail != nil || ur.cq.cqes != nil || ur.cq.ringSz != 0 {
		t.Fatal("clearRingViews did not clear cq state")
	}

	ring := &Uring{ioUring: ur}
	if got := ring.SQAvailable(); got != 0 {
		t.Fatalf("SQAvailable after clearRingViews = %d, want 0", got)
	}
	if got := ring.CQPending(); got != 0 {
		t.Fatalf("CQPending after clearRingViews = %d, want 0", got)
	}
	if err := ur.enter(); !errors.Is(err, ErrClosed) {
		t.Fatalf("enter after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.wait(); !errors.Is(err, ErrClosed) {
		t.Fatalf("wait after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.waitBatch(make([]CQEView, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("waitBatch after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.waitBatchDirect(make([]DirectCQE, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("waitBatchDirect after clearRingViews = %v, want %v", err, ErrClosed)
	}
	if _, err := ur.waitBatchExtended(make([]ExtCQE, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("waitBatchExtended after clearRingViews = %v, want %v", err, ErrClosed)
	}
}

func TestSubmitKeepAliveClearReturnsExtAndClearsRoots(t *testing.T) {
	ur := &ioUring{}
	keep := &submitKeepAlive{}

	ctrl := byte(1)
	value := []byte("hello")
	keep.retainBytes(value)
	keep.iovs = keep.iovInline[:1]
	keep.iovInline[0] = IoVec{Base: &ctrl, Len: 1}
	ext := keep.ensureExt(ur)
	ext.msg = Msghdr{Control: &ctrl, Controllen: 1}
	ext.epollCtlEvent = EpollEvent{Events: EPOLLIN, Fd: 1}
	ext.epollWaitEvents = []EpollEvent{{Events: EPOLLIN, Fd: 1}}
	ext.openHow = &OpenHow{}
	ext.statx = &Statx{}
	ext.path1Buf = make([]byte, 300)

	keep.clear(ur)

	if keep.active {
		t.Fatal("keepAlive remained active after clear")
	}
	if keep.bytes1 != nil {
		t.Fatal("keepAlive bytes1 was not cleared")
	}
	if keep.iovs != nil {
		t.Fatal("keepAlive iovs was not cleared")
	}
	if keep.sockaddrArg != nil {
		t.Fatal("keepAlive sockaddrArg was not cleared")
	}
	if keep.ext != nil {
		t.Fatal("keepAlive ext was not released")
	}
	if keep.iovInline[0].Base != nil {
		t.Fatal("keepAlive iovInline root was not cleared")
	}

	reused := ur.acquireSubmitKeepAliveExt()
	if reused != ext {
		t.Fatal("keepAlive ext was not returned to the free list")
	}
	if reused.msg != (Msghdr{}) {
		t.Fatal("pooled ext msg was not cleared")
	}
	if reused.epollCtlEvent != (EpollEvent{}) {
		t.Fatal("pooled ext epollCtlEvent was not cleared")
	}
	if reused.epollWaitEvents != nil {
		t.Fatal("pooled ext epollWaitEvents root was not cleared")
	}
	if reused.openHow != nil {
		t.Fatal("pooled ext OpenHow root was not cleared")
	}
	if reused.statx != nil {
		t.Fatal("pooled ext Statx root was not cleared")
	}
	if cap(reused.path1Buf) < 300 {
		t.Fatal("pooled ext lost its reusable long-path buffer")
	}
}

func TestUringStopClosesUnstartedRing(t *testing.T) {
	ring, err := New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := ring.RingFD(); got < 0 {
		t.Fatalf("RingFD before Stop = %d, want open ring fd", got)
	}

	if err := ring.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ring.RingFD(); got != -1 {
		t.Fatalf("RingFD after Stop = %d, want -1", got)
	}
	if err := ring.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := ring.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Stop = %v, want %v", err, ErrClosed)
	}
}

func TestUringStopReleasesTrackedResources(t *testing.T) {
	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if len(ring.bufferRings.gids) == 0 {
		t.Fatal("bufferRings.gids is empty after Start")
	}
	if _, err := ring.RegisterBufRingWithFlags(16, 0x7ffe, 0); err != nil {
		t.Fatalf("RegisterBufRingWithFlags: %v", err)
	}
	if len(ring.registeredBufRings) != 1 {
		t.Fatalf("registeredBufRings len = %d, want 1", len(ring.registeredBufRings))
	}

	tmp, err := os.CreateTemp("", "uring-stop-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if err := ring.RegisterFiles([]int32{int32(tmp.Fd())}); err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	if got := ring.RegisteredFileCount(); got != 1 {
		t.Fatalf("RegisteredFileCount before Stop = %d, want 1", got)
	}

	if err := ring.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ring.RingFD(); got != -1 {
		t.Fatalf("RingFD after Stop = %d, want -1", got)
	}
	if got := ring.RegisteredBufferCount(); got != 0 {
		t.Fatalf("RegisteredBufferCount after Stop = %d, want 0", got)
	}
	if got := ring.RegisteredFileCount(); got != 0 {
		t.Fatalf("RegisteredFileCount after Stop = %d, want 0", got)
	}
	if ring.submit.keepAlive != nil {
		t.Fatal("submit.keepAlive not cleared by Stop")
	}
	if ring.submit.keepAliveExtFree != nil {
		t.Fatal("submit.keepAliveExtFree not cleared by Stop")
	}
	if len(ring.bufferRings.gids) != 0 {
		t.Fatalf("bufferRings.gids len after Stop = %d, want 0", len(ring.bufferRings.gids))
	}
	if len(ring.registeredBufRings) != 0 {
		t.Fatalf("registeredBufRings len after Stop = %d, want 0", len(ring.registeredBufRings))
	}
	if err := ring.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := ring.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Stop = %v, want %v", err, ErrClosed)
	}
}

func TestSendZeroCopyFixedStagesBufIndexAndOffset(t *testing.T) {
	ring := newStartedTwoRegisteredBuffersSharedTestRing(t)
	if ring.RegisteredBufferCount() < 2 {
		t.Skip("need at least two registered buffers")
	}

	const (
		bufIndex = 1
		offset   = 32
		length   = 128
	)
	buf := ring.ioUring.bufs[bufIndex]
	base := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf))))

	if err := ring.ioUring.sendZeroCopyFixed(PackDirect(0, 0, 0, 1), bufIndex, offset, length, 0); err != nil {
		t.Fatalf("sendZeroCopyFixed submit: %v", err)
	}

	tail := atomic.LoadUint32(ring.ioUring.sq.kTail)
	idx := (tail - 1) & *ring.ioUring.sq.kRingMask
	sqe := ring.ioUring.sq.sqeAt(idx)

	if sqe.opcode != IORING_OP_SEND_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND_ZC)
	}
	if sqe.ioprio != IORING_RECVSEND_FIXED_BUF {
		t.Fatalf("ioprio = %d, want %d", sqe.ioprio, IORING_RECVSEND_FIXED_BUF)
	}
	if sqe.bufIndex != bufIndex {
		t.Fatalf("bufIndex = %d, want %d", sqe.bufIndex, bufIndex)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.addr != base+offset {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, base+offset)
	}
	if sqe.len != length {
		t.Fatalf("len = %d, want %d", sqe.len, length)
	}
}
