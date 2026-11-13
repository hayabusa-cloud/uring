// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// Splice transfers through a pipe without a userspace payload buffer.
// At least one endpoint of each splice must be a pipe:
//
//	source file -> pipe write end ... pipe read end -> destination file
//
// The first CQE confirms bytes entered the pipe before the second splice drains
// them. The context FD is the destination; the separate FD argument is the source.
func TestSpliceBasics(t *testing.T) {
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
	src, dst := tempFile(t), tempFile(t)
	payload := []byte("file to pipe to file")
	if _, err := src.WriteAt(payload, 0); err != nil {
		t.Fatal(err)
	}
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

	ctx := uring.PackDirect(uring.IORING_OP_SPLICE, 0, 0, int32(w))
	if err := ring.Splice(ctx, iofd.FD(src.Fd()), len(payload)); err != nil {
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
			t.Fatal("timed out waiting for SPLICE completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_SPLICE || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	// The first transfer has completed. Now move those bytes from the pipe
	// into the destination file, and wait again before reading the file.
	if err := ring.Splice(ctx.WithFD(iofd.FD(dst.Fd())), r, len(payload)); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(exampleTimeout)
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
			t.Fatal("timed out waiting for SPLICE completion")
		}
		backoff.Wait()
	}
	cqe = cqes[0]
	if cqe.Op() != uring.IORING_OP_SPLICE || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	got := make([]byte, len(payload))
	if _, err := dst.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

// Tee duplicates pipe data without consuming the source pipe.
// Both endpoints must be pipes; after completion, both copies can be read:
//
//	source pipe --Tee--> destination pipe
//	     |                     |
//	 original bytes       duplicate bytes
func TestTeeOperation(t *testing.T) {
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
	r1, w1, err := createPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r1.Close(); err != nil {
			t.Errorf("close first pipe reader: %v", err)
		}
		if err := w1.Close(); err != nil {
			t.Errorf("close first pipe writer: %v", err)
		}
	}()
	r2, w2, err := createPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r2.Close(); err != nil {
			t.Errorf("close second pipe reader: %v", err)
		}
		if err := w2.Close(); err != nil {
			t.Errorf("close second pipe writer: %v", err)
		}
	}()

	payload := []byte("both pipes retain this payload")
	if n, err := w1.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("pipe write: n=%d err=%v", n, err)
	}
	ctx := uring.PackDirect(uring.IORING_OP_TEE, 0, 0, int32(w2))
	if err := ring.Tee(ctx, r1, len(payload)); err != nil {
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
			t.Fatal("timed out waiting for TEE completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_TEE || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	for _, fd := range [2]*iofd.FD{&r1, &r2} {
		got := make([]byte, len(payload))
		if n, err := fd.Read(got); err != nil || n != len(got) {
			t.Fatalf("pipe read: n=%d err=%v", n, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("got %q, want %q", got, payload)
		}
	}
}
