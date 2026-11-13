// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// A timeout is an asynchronous request with its own CQE. With the default flags,
// expiry reports -ETIME; the submission call itself does not wait for the timer.
func TestBasicTimeout(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Observe successful operations as well as errors.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// Drain every submitted request before destroying the ring.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	ctx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
	if err := ring.Timeout(ctx, 10*time.Millisecond); err != nil {
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
			t.Fatal("timed out waiting for TIMEOUT completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	// Wait's error describes ring progress. The request's result is in Res;
	// -ETIME is the expected timer expiry here.
	if cqe.Op() != uring.IORING_OP_TIMEOUT || cqe.Res != -int32(zcall.ETIME) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
}

// Give each timer a distinct context ID so completions can be matched without
// relying on their arrival order. TIMEOUT does not use a buffer group, so this
// example carries the ID in that Direct-context field.
func TestMultipleTimeouts(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Observe successful operations as well as errors.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// Drain every submitted request before destroying the ring.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	for i, d := range []time.Duration{30, 10, 20} {
		ctx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, uint16(i), 0)
		if err := ring.Timeout(ctx, d*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	var seen [3]bool
	for range seen {
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
				t.Fatal("timed out waiting for TIMEOUT completion")
			}
			backoff.Wait()
		}
		cqe := cqes[0]
		if cqe.Op() != uring.IORING_OP_TIMEOUT {
			t.Fatalf("unexpected opcode %d", cqe.Op())
		}
		id := int(cqe.BufGroup())
		if id >= len(seen) || seen[id] {
			t.Fatalf("unexpected or duplicate timeout ID %d", id)
		}
		seen[id] = true
		if cqe.Res != -int32(zcall.ETIME) {
			t.Fatalf("opcode %d: result=%d error=%v, want %d", cqe.Op(), cqe.Res, cqe.Err(), -int32(zcall.ETIME))
		}
	}
}

// Independent timeouts do not cancel other requests. Cancel and reap the
// losing poll explicitly before tearing down the ring.
func TestPollWithTimeout(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Observe successful operations as well as errors.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// Drain every submitted request before destroying the ring.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	r, w, err := createPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Errorf("close pipe reader: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Errorf("close pipe writer: %v", err)
		}
	}()
	poll := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(r))
	if err := ring.PollAdd(poll, uring.EPOLLIN); err != nil {
		t.Fatal(err)
	}
	timeout := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
	if err := ring.Timeout(timeout, 10*time.Millisecond); err != nil {
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
			t.Fatal("timed out waiting for TIMEOUT completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_TIMEOUT || cqe.Res != -int32(zcall.ETIME) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	cancel := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 0, 0)
	if err := ring.AsyncCancel(cancel, poll.Raw()); err != nil {
		t.Fatal(err)
	}
	var pollDone, cancelDone bool
	for range 2 {
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
				t.Fatal("timed out waiting for request completion")
			}
			backoff.Wait()
		}
		cqe := cqes[0]
		switch cqe.Op() {
		case uring.IORING_OP_POLL_ADD:
			if pollDone {
				t.Fatal("duplicate poll completion")
			}
			pollDone = true
			if cqe.Res != -int32(uring.ECANCELED) {
				t.Fatalf("opcode %d: result=%d error=%v, want %d", cqe.Op(), cqe.Res, cqe.Err(), -int32(uring.ECANCELED))
			}
		case uring.IORING_OP_ASYNC_CANCEL:
			if cancelDone {
				t.Fatal("duplicate cancellation completion")
			}
			cancelDone = true
			if cqe.Res != 0 {
				t.Fatalf("opcode %d: result=%d error=%v, want %d", cqe.Op(), cqe.Res, cqe.Err(), 0)
			}
		default:
			t.Fatalf("unexpected opcode %d", cqe.Op())
		}
	}
	if !pollDone || !cancelDone {
		t.Fatal("pending request after cancellation")
	}
}

// Put the link flag on the operation followed immediately by its timeout:
//
//	SQE 1: NOP + IOSQE_IO_LINK -> SQE 2: LINK_TIMEOUT
//
// Here the NOP completes first and the timeout reports -ECANCELED. If the timer
// expires first, it attempts to cancel the linked operation. Reap both CQEs to
// learn their actual outcomes before releasing request resources.
func TestLinkedTimeout(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Observe successful operations as well as errors.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// Drain every submitted request before destroying the ring.
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	nop := uring.PackDirect(uring.IORING_OP_NOP, uring.IOSQE_IO_LINK, 0, 0)
	if err := ring.Nop(nop); err != nil {
		t.Fatal(err)
	}
	timeout := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 0, 0)
	if err := ring.LinkTimeout(timeout, time.Second); err != nil {
		t.Fatal(err)
	}
	var nopDone, timeoutDone bool
	for range 2 {
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
				t.Fatal("timed out waiting for request completion")
			}
			backoff.Wait()
		}
		cqe := cqes[0]
		switch cqe.Op() {
		case uring.IORING_OP_NOP:
			if nopDone {
				t.Fatal("duplicate NOP completion")
			}
			nopDone = true
			if cqe.Res != 0 {
				t.Fatalf("opcode %d: result=%d error=%v, want %d", cqe.Op(), cqe.Res, cqe.Err(), 0)
			}
		case uring.IORING_OP_LINK_TIMEOUT:
			if timeoutDone {
				t.Fatal("duplicate linked timeout completion")
			}
			timeoutDone = true
			if cqe.Res != -int32(uring.ECANCELED) {
				t.Fatalf("opcode %d: result=%d error=%v, want %d", cqe.Op(), cqe.Res, cqe.Err(), -int32(uring.ECANCELED))
			}
		default:
			t.Fatalf("unexpected opcode %d", cqe.Op())
		}
	}
	if !nopDone || !timeoutDone {
		t.Fatal("linked request pair did not complete")
	}
}
