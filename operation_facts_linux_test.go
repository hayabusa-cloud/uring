// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

func TestBundleFollowsRecycledBufferOrder(t *testing.T) {
	const size, entries = 64, 4
	ring := newTestRing(t, func(o *uring.Options) {
		o.ReadBufferSize = size
		o.ReadBufferNum = entries
	})
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	var held [entries]uring.BundleIterator
	for i := range held {
		if n, errno := writeTestFD(fds[1], []byte{byte(i)}); errno != 0 || n != 1 {
			t.Fatalf("seed write: n=%d errno=%d", n, errno)
		}
		if err := ring.ReceiveFD(uring.ForFD(0), fds[0], nil, uring.WithReadBufferSize(1)); err != nil {
			t.Fatal(err)
		}
		cqe, ok := waitForOp(t, ring, uring.IORING_OP_RECV, 2*time.Second)
		if !ok || cqe.Res != 1 || !cqe.HasBuffer() {
			t.Fatalf("seed completion: present=%v cqe=%+v", ok, cqe)
		}
		held[i], ok = ring.BundleIterator(cqe, cqe.BufGroup())
		if !ok {
			t.Fatal("seed iterator unavailable")
		}
	}
	order := [...]int{2, 0, 3, 1}
	for _, i := range order {
		if err := held[i].Recycle(ring); err != nil {
			t.Fatal(err)
		}
	}
	payload := make([]byte, 3*size+13)
	for i := range payload {
		payload[i] = byte(i/size + 40)
	}
	if n, errno := writeTestFD(fds[1], payload); errno != 0 || n != len(payload) {
		t.Fatalf("bundle write: n=%d errno=%d", n, errno)
	}
	seen, buffers, largest := 0, 0, 0
	for seen < len(payload) {
		if err := ring.ReceiveBundle(uring.ForFD(0), testPollFd{fd: fds[0]}); err != nil {
			t.Fatal(err)
		}
		cqe, ok := waitForOp(t, ring, uring.IORING_OP_RECV, 2*time.Second)
		if !ok || cqe.Res <= 0 || !cqe.HasBuffer() {
			t.Fatalf("bundle completion: present=%v cqe=%+v", ok, cqe)
		}
		it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
		if !ok {
			t.Fatal("bundle iterator unavailable")
		}
		largest = max(largest, it.Count())
		for id, buf := range it.AllWithSlotID() {
			wantID := held[order[buffers]].SlotID(0)
			if id != wantID {
				t.Errorf("buffer %d ID=%d, want published ID %d", buffers, id, wantID)
			}
			if !bytes.Equal(buf, payload[seen:seen+len(buf)]) {
				t.Errorf("buffer %d contains the wrong payload", buffers)
			}
			seen += len(buf)
			buffers++
		}
	}
	if largest < 2 {
		t.Fatal("kernel did not produce a multi-buffer bundle")
	}
}

func BenchmarkBundleReceiveRecycle(b *testing.B) {
	const size, entries = 64, 4
	ring, err := uring.New(testMinimalBufferOptions, func(o *uring.Options) {
		o.ReadBufferSize = size
		o.ReadBufferNum = entries
	})
	if err != nil {
		b.Fatal(err)
	}
	mustStartRing(b, ring)
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestFds(fds)
	var payload [size * entries]byte
	var cqes [4]uring.CQEView
	var backoff iox.Backoff
	pollFD := testPollFd{fd: fds[0]}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if n, errno := writeTestFD(fds[1], payload[:]); errno != 0 || n != len(payload) {
			b.Fatalf("write n=%d errno=%d", n, errno)
		}
		for remaining := len(payload); remaining > 0; {
			if err := ring.ReceiveBundle(uring.ForFD(0), &pollFD); err != nil {
				b.Fatal(err)
			}
			for {
				n, err := ring.Wait(cqes[:])
				if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
					b.Fatal(err)
				}
				if n == 0 {
					backoff.Wait()
					continue
				}
				backoff.Reset()
				for _, cqe := range cqes[:n] {
					it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
					if !ok {
						b.Fatalf("completion: %+v", cqe)
					}
					for buf := range it.All() {
						if buf[0] != 0 {
							b.Fatal("unexpected payload")
						}
						remaining -= len(buf)
					}
					if err := it.Recycle(ring); err != nil {
						b.Fatal(err)
					}
				}
				break
			}
		}
	}
}
