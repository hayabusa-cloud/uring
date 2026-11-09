// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code.hybscloud.com/iox"
)

func awaitVisibleCQ(t *testing.T, ring *Uring, count uint32) {
	t.Helper()
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := ring.ioUring.enter(); err != nil {
			t.Fatal(err)
		}
		cq := &ring.ioUring.cq
		if atomic.LoadUint32(cq.kTail)-atomic.LoadUint32(cq.kHead) >= count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("completion did not become visible")
		}
		backoff.Wait()
	}
}

func TestCQReapIndependentOfSubmitState(t *testing.T) {
	for _, mode := range []string{"single", "generic", "direct", "extended"} {
		t.Run(mode, func(t *testing.T) {
			ring := newStartedSharedTestRing(t)
			ctx := PackDirect(IORING_OP_NOP, 0, 0, 7)
			if mode == "extended" {
				ext := ring.ExtSQE()
				if ext == nil {
					t.Fatal("context pool exhausted")
				}
				t.Cleanup(func() { ring.PutExtSQE(ext) })
				ctx = PackExtended(ext).WithFD(7)
			}
			if err := ring.Nop(ctx); err != nil {
				t.Fatal(err)
			}
			awaitVisibleCQ(t, ring, 1)

			type result struct {
				n     int
				res   int32
				flags uint32
				fd    int32
				err   error
			}
			done := make(chan result, 1)
			started := make(chan struct{})
			ring.ioUring.lockSubmitState()
			go func() {
				close(started)
				var got result
				switch mode {
				case "single":
					cqe, err := ring.ioUring.wait()
					got = result{1, cqe.res, cqe.flags, SQEContextFromRaw(cqe.userData).FD(), err}
				case "generic":
					var cqes [1]CQEView
					got.n, got.err = ring.ioUring.waitBatch(cqes[:])
					got.res, got.flags, got.fd = cqes[0].Res, cqes[0].Flags, int32(cqes[0].FD())
				case "direct":
					var cqes [1]DirectCQE
					got.n, got.err = ring.ioUring.waitBatchDirect(cqes[:])
					got.res, got.flags, got.fd = cqes[0].Res, cqes[0].Flags, int32(cqes[0].FD)
				case "extended":
					var cqes [1]ExtCQE
					got.n, got.err = ring.ioUring.waitBatchExtended(cqes[:])
					if got.n > 0 {
						got.res, got.flags, got.fd = cqes[0].Res, cqes[0].Flags, int32(cqes[0].FD())
					}
				}
				done <- got
			}()
			<-started
			var got result
			blocked := false
			select {
			case got = <-done:
			case <-time.After(time.Second):
				blocked = true
			}
			ring.ioUring.unlockSubmitState()
			if blocked {
				got = <-done // Drain before teardown, including on the old implementation.
			}
			if got.err != nil || got.n != 1 || got.res != 0 || got.flags != 0 || got.fd != 7 {
				t.Fatalf("completion = %+v", got)
			}
			if blocked {
				t.Fatal("visible CQ consumption waited for the submission lock")
			}
		})
	}
}

func TestCQClaimProtectsUncopiedRange(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	for fd := int32(1); fd <= 2; fd++ {
		if err := ring.Nop(PackDirect(IORING_OP_NOP, 0, 0, fd)); err != nil {
			t.Fatal(err)
		}
	}
	awaitVisibleCQ(t, ring, 2)
	ur := ring.ioUring
	claim, count, err := ur.claimCQ(1)
	if err != nil || count != 1 {
		t.Fatalf("first claim = (%d, %d, %v)", claim, count, err)
	}
	head := uint32(claim)

	type result struct {
		cqe ioUringCqe
		err error
	}
	done := make(chan result, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		cqe, err := ur.wait()
		done <- result{cqe, err}
	}()
	<-started
	returned := false
	var second result
	select {
	case second = <-done:
		returned = true
	case <-time.After(10 * time.Millisecond):
	}
	claimed := atomic.LoadUint64(&ur.cq.claimState)
	published := atomic.LoadUint32(ur.cq.kHead)
	// Copy and release the first reservation even if an assertion will fail.
	first := *ur.cq.cqeAt(head & *ur.cq.kRingMask)
	ur.publishCQ(claim, count)
	if !returned {
		second = <-done
	}
	if claimed != claim|cqClaimBusy || published != head || returned {
		t.Fatalf("held first copy: claim=%#x head=%d returned=%v; want claim=%#x head=%d returned=false", claimed, published, returned, claim|cqClaimBusy, head)
	}
	if first.res != 0 || SQEContextFromRaw(first.userData).FD() != 1 || second.err != nil || second.cqe.res != 0 || SQEContextFromRaw(second.cqe.userData).FD() != 2 {
		t.Fatalf("completion order: first=%+v second=%+v", first, second)
	}
	if got := atomic.LoadUint32(ur.cq.kHead); got != head+2 {
		t.Fatalf("released head = %d, want %d", got, head+2)
	}
}

// This checks sequence arithmetic and CQE32 indexing, not kernel publication.
func TestCQClaimCounterWrap(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.submit.shared = true
	ur.cq.claimState = 2<<32 - 2
	*ur.cq.kHead = ^uint32(0) - 1
	*ur.cq.kTail = 2
	*ur.cq.kRingMask = 3
	ur.cq.cqes = make([]ioUringCqe, 8)
	for i := range uint32(4) {
		index := (*ur.cq.kHead + i) & *ur.cq.kRingMask
		*ur.cq.cqeAt(index) = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, int32(i+1)).Raw()}
	}
	var generic [2]CQEView
	if n, err := ur.waitBatch(generic[:]); err != nil || n != 2 || generic[0].FD() != 1 || generic[1].FD() != 2 {
		t.Fatalf("generic wrap = (%d, %v), cqes=%+v", n, err, generic)
	}
	var direct [1]DirectCQE
	if n, err := ur.waitBatchDirect(direct[:]); err != nil || n != 1 || direct[0].FD != 3 {
		t.Fatalf("direct after wrap = (%d, %v), cqe=%+v", n, err, direct[0])
	}
	cqe, err := ur.wait()
	if err != nil || SQEContextFromRaw(cqe.userData).FD() != 4 {
		t.Fatalf("single after wrap = (%+v, %v)", cqe, err)
	}
	if got := atomic.LoadUint64(&ur.cq.claimState); got != 2<<32+2 {
		t.Fatalf("claim sequence = %d, want %d", got, uint64(2<<32+2))
	}
	if got := atomic.LoadUint32(ur.cq.kHead); got != 2 {
		t.Fatalf("released head = %d, want 2", got)
	}

	// Crossing the private sequence's high bit must release an unowned word.
	ur.cq.claimState = cqClaimBusy - 1
	*ur.cq.kHead = ^uint32(0)
	*ur.cq.kTail = 0
	claim, n, err := ur.claimCQ(1)
	if err != nil || n != 1 {
		t.Fatalf("private wrap claim = (%d, %d, %v)", claim, n, err)
	}
	ur.publishCQ(claim, n)
	if ur.cq.claimState != 0 || *ur.cq.kHead != 0 {
		t.Fatalf("private wrap release: claim=%d head=%d", ur.cq.claimState, *ur.cq.kHead)
	}
}

func TestCQPendingDuringSharedReap(t *testing.T) {
	const workers, perWorker = 8, 1024
	ring := newStartedSharedTestRing(t)
	capacity := int(*ring.ioUring.cq.kRingEntries)
	var completed atomic.Uint32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range workers {
		wg.Go(func() {
			<-start
			var cqes [1]DirectCQE
			var backoff iox.Backoff
			for range perWorker {
				if err := ring.Nop(PackDirect(IORING_OP_NOP, 0, 0, 0)); err != nil {
					t.Error(err)
					return
				}
				for {
					n, err := ring.WaitDirect(cqes[:])
					if err != nil && err != iox.ErrWouldBlock {
						t.Error(err)
						return
					}
					if n > 0 {
						if cqes[0].Res != 0 {
							t.Errorf("NOP result = %d", cqes[0].Res)
						}
						completed.Add(uint32(n))
						backoff.Reset()
						break
					}
					backoff.Wait()
				}
			}
		})
	}
	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	var backoff iox.Backoff
	for {
		if pending := ring.CQPending(); pending < 0 || pending > capacity {
			t.Errorf("CQPending = %d, capacity = %d", pending, capacity)
		}
		select {
		case <-done:
			if got := completed.Load(); got != workers*perWorker {
				t.Errorf("reaped %d completions, want %d", got, workers*perWorker)
			}
			if got := ring.CQPending(); got != 0 {
				t.Errorf("CQPending after drain = %d, want 0", got)
			}
			return
		default:
			backoff.Wait()
		}
	}
}
