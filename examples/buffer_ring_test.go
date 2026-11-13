// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// Fixed buffers are selected by the caller's registered index. Provided buffers
// are published in a group for the kernel to select; the CQE identifies the
// selected buffer. A receive bundle can span several published buffers:
//
//	CQE: first buffer ID + total bytes -> BundleIterator -> borrowed slices
//	                                                        |
//	                                           process, then Recycle
//
// BundleIterator follows publication order, including after ring wraparound.
// Buffer IDs are identities; their numeric order does not define a bundle.
func TestBundleIteratorUsage(t *testing.T) {
	const size, entries = 64, 4
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferSize = size
		opt.ReadBufferNum = entries
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// All submitted operations must finish before the ring is destroyed.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	fd, client := tcpPair(t)
	payload := make([]byte, 3*size+13)
	for i := range payload {
		payload[i] = byte(i)
	}
	largest := 0
	for range 3 {
		if n, err := client.Write(payload); err != nil || n != len(payload) {
			t.Fatalf("write: n=%d err=%v", n, err)
		}
		received := 0
		for received < len(payload) {
			if err := ring.ReceiveBundle(uring.ForFD(int32(fd)), &fd); err != nil {
				t.Fatal(err)
			}
			var cqes [1]uring.CQEView
			var backoff iox.Backoff
			deadline := time.Now().Add(exampleTimeout)
			for {
				n, err := ring.Wait(cqes[:])
				if err != nil && !iox.IsWouldBlock(err) {
					t.Fatalf("Wait: %v", err)
				}
				if n > 0 {
					backoff.Reset()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("timed out waiting for completion")
				}
				backoff.Wait()
			}
			cqe := cqes[0]
			if cqe.Op() != uring.IORING_OP_RECV || cqe.Res <= 0 {
				t.Fatalf("bundle result=%d", cqe.Res)
			}
			it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
			if !ok {
				t.Fatal("missing bundle buffers")
			}
			largest = max(largest, it.Count())
			for buf := range it.All() {
				end := received + len(buf)
				if end > len(payload) || !bytes.Equal(buf, payload[received:end]) {
					t.Error("bundle contains incorrect payload bytes")
				}
				received = end
			}
			// Process all borrowed slices before returning any buffer.
			if err := it.Recycle(ring); err != nil {
				t.Fatal(err)
			}
		}
		if received != len(payload) {
			t.Fatalf("received %d bytes", received)
		}
	}
	if largest < 2 {
		t.Fatal("no completion spanned multiple buffers")
	}
}

// BUF_MORE describes buffer ownership, independently of request completion.
// Each IncrementalReceiver.Recv below is a separate single-shot request.
//
//	BUF_MORE set    Consume this fragment; leave the buffer with the kernel
//	BUF_MORE clear  Consume the final fragment; the buffer may be republished
//
// Add stages a buffer descriptor; Advance publishes it. Keep the backing array
// live throughout, and republish only after consuming its final fragment.
func TestIncrementalBufferConsumption(t *testing.T) {
	const size, entries = 64, 2
	const group uint16 = 60000
	backing := make([]byte, size*entries)
	// Register this cleanup before the ring cleanup so roots outlive Stop.
	t.Cleanup(func() { runtime.KeepAlive(backing) })
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// All submitted operations must finish before the ring is destroyed.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	bufRing, err := ring.RegisterBufRingIncremental(entries, group)
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if err := bufRing.Add(backing[i*size:(i+1)*size], uint16(i), i); err != nil {
			t.Fatal(err)
		}
	}
	if err := bufRing.Advance(entries); err != nil {
		t.Fatal(err)
	}
	receiver := uring.NewIncrementalReceiver(ring, uring.NewContextPools(2), group, size, backing, entries)
	fd, client := tcpPair(t)
	var got [size]byte
	received, submitted, completed := 0, 0, 0
	handler := uring.NewIncrementalSubscriber().
		OnData(func(buf []byte, bufferMore bool) {
			received += copy(got[received:], buf)
			if bufferMore != (received < size) {
				t.Error("incorrect buffer continuation")
			}
		}).
		OnComplete(func() { completed++ }).
		OnError(func(err error) { t.Errorf("incremental receive: %v", err) }).Handler()
	var firstID uint16
	for i, length := range [...]int{1, 7, 24, 5, 27} {
		payload := bytes.Repeat([]byte{byte('A' + i)}, length)
		start, end := received, received+len(payload)
		if n, err := client.Write(payload); err != nil || n != len(payload) {
			t.Fatalf("write: n=%d err=%v", n, err)
		}
		// TCP may split a write across several receive completions.
		for received < end {
			if err := receiver.Recv(fd, handler); err != nil {
				t.Fatal(err)
			}
			submitted++
			var cqes [1]uring.CQEView
			var backoff iox.Backoff
			deadline := time.Now().Add(exampleTimeout)
			for {
				n, err := ring.Wait(cqes[:])
				if err != nil && !iox.IsWouldBlock(err) {
					t.Fatalf("Wait: %v", err)
				}
				if n > 0 {
					backoff.Reset()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("timed out waiting for completion")
				}
				backoff.Wait()
			}
			cqe := cqes[0]
			if cqe.Op() != uring.IORING_OP_RECV || cqe.Res <= 0 || int(cqe.Res) > end-received {
				t.Fatalf("incremental result=%d, remaining=%d", cqe.Res, end-received)
			}
			id, bufferMore := cqe.BufID(), cqe.HasBufferMore()
			if submitted == 1 {
				firstID = id
			}
			if id != firstID || cqe.HasMore() {
				t.Error("incorrect incremental request identity")
			}
			if !receiver.HandleCQE(cqe) {
				t.Fatal("unhandled incremental completion")
			}
			// HandleCQE retired the request context. Only copied facts survive.
			if !bufferMore {
				if err := bufRing.Add(backing[int(id)*size:(int(id)+1)*size], id, 0); err != nil {
					t.Fatal(err)
				}
				if err := bufRing.Advance(1); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !bytes.Equal(got[start:end], payload) {
			t.Error("incorrect incremental fragment")
		}
	}
	if completed != submitted || received != size {
		t.Fatalf("completed=%d submitted=%d received=%d", completed, submitted, received)
	}
}
