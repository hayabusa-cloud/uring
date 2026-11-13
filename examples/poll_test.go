// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// PollAdd reports readiness without transferring payload bytes. Its successful
// CQE result is an event mask, so test the requested bit instead of a byte count.
// EPOLLIN requests read readiness; it does not read the pipe's bytes.
func TestPollAddBasic(t *testing.T) {
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
	ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(r))
	if err := ring.PollAdd(ctx, uring.EPOLLIN); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("ready")); err != nil || n != 5 {
		t.Fatalf("pipe write: n=%d err=%v", n, err)
	}
	// Wait submits queued work and reaps CQEs. An empty result means keep
	// waiting with backoff; only the matching CQE reports poll readiness.
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
			t.Fatal("timed out waiting for POLL_ADD completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_POLL_ADD {
		t.Fatalf("unexpected opcode %d", cqe.Op())
	}
	if cqe.Err() != nil || cqe.Res&uring.EPOLLIN == 0 || cqe.FD() != r {
		t.Fatalf("poll did not report the readable pipe: %+v", cqe)
	}
}

// Cancelling this idle poll produces two independent completions:
//
//	Request         CQE result in this example
//	Original poll   -ECANCELED
//	Cancellation    0 (the cancellation succeeded)
//
// Reap both CQEs; a cancellation result alone does not retire the original poll.
func TestPollCancelByFD(t *testing.T) {
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
	// Submission order installs this idle poll before its cancellation.
	cancel := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 0, int32(r))
	if err := ring.AsyncCancelFD(cancel, false); err != nil {
		t.Fatal(err)
	}
	var pollDone, cancelDone bool
	// Read one CQE at a time so inspecting one cannot discard the other.
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
		t.Fatal("cancellation did not retire both requests")
	}
}

func TestPollMultipleFDs(t *testing.T) {
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
	// These cells own the pipe ends. Unopened cells stay invalid for cleanup.
	reads := [3]iofd.FD{iofd.InvalidFD, iofd.InvalidFD, iofd.InvalidFD}
	writes := [3]iofd.FD{iofd.InvalidFD, iofd.InvalidFD, iofd.InvalidFD}
	defer func() {
		for i := range reads {
			if err := reads[i].Close(); err != nil {
				t.Errorf("close pipe %d reader: %v", i, err)
			}
			if err := writes[i].Close(); err != nil {
				t.Errorf("close pipe %d writer: %v", i, err)
			}
		}
	}()
	for i := range reads {
		reads[i], writes[i], err = createPipe()
		if err != nil {
			t.Fatal(err)
		}
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, uint16(i), int32(reads[i]))
		if err := ring.PollAdd(ctx, uring.EPOLLIN); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(writes) - 1; i >= 0; i-- {
		if n, err := writes[i].Write([]byte{byte(i)}); err != nil || n != 1 {
			t.Fatalf("pipe write: n=%d err=%v", n, err)
		}
	}
	var seen [3]bool
	// Completion order need not match submission or write order.
	for range reads {
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
				t.Fatal("timed out waiting for POLL_ADD completion")
			}
			backoff.Wait()
		}
		cqe := cqes[0]
		if cqe.Op() != uring.IORING_OP_POLL_ADD {
			t.Fatalf("unexpected opcode %d", cqe.Op())
		}
		i := int(cqe.BufGroup())
		if i >= len(seen) || seen[i] {
			t.Fatalf("unexpected or duplicate poll ID %d", i)
		}
		seen[i] = true
		if cqe.Err() != nil || cqe.Res&uring.EPOLLIN == 0 || cqe.FD() != reads[i] {
			t.Fatalf("wrong readiness observation: %+v", cqe)
		}
	}
}
