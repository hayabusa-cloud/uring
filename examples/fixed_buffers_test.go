// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// Registered buffers have stable slots that READ_FIXED and WRITE_FIXED select
// by index. RegisteredBuffer(index) exposes the bytes in that slot:
//
//	registered slot 0 -> buffer backing <- RegisteredBuffer(0)
//	                           ^
//	                  ReadFixed / WriteFixed
//
// The ring owns this storage until Stop; each in-flight operation borrows it.
// Registration alone does not make a transfer zero-copy.
func TestFixedBufferBasics(t *testing.T) {
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
	// Drain submitted requests before this deferred teardown.
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	if ring.RegisteredBufferCount() == 0 {
		t.Fatal("no registered buffers")
	}
	buf := ring.RegisteredBuffer(0)
	if len(buf) == 0 {
		t.Fatal("empty registered buffer")
	}
	copy(buf, "registered")
	if string(ring.RegisteredBuffer(0)[:10]) != "registered" {
		t.Fatal("registered buffer access did not retain data")
	}
}

// Fill the registered buffer before WriteFixed, then keep its bytes unchanged
// until the write CQE. The buffer index selects storage; n limits bytes written.
func TestWriteFixedOperation(t *testing.T) {
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
	// Drain submitted requests before this deferred teardown.
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	f := tempFile(t)
	payload := []byte("Write from a registered buffer")
	buf := ring.RegisteredBuffer(0)
	copy(buf, payload)
	ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, int32(f.Fd()))
	if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
		t.Fatalf("WriteFixed: %v", err)
	}
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
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
	if cqe.Op() != uring.IORING_OP_WRITE_FIXED || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	// The write CQE ends the borrow; buf may now be changed or reused.
	got := make([]byte, len(payload))
	if _, err := f.ReadAt(got, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

// ReadFixed returns a slice into registered storage before the read completes.
// Wait for its CQE before reading the bytes; a successful Res gives their count.
func TestReadFixedOperation(t *testing.T) {
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
	// Drain submitted requests before this deferred teardown.
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	f := tempFile(t)
	payload := []byte("Read into a registered buffer")
	if _, err := f.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	ctx := uring.PackDirect(uring.IORING_OP_READ_FIXED, 0, 0, int32(f.Fd()))
	buf, err := ring.ReadFixed(ctx, 0)
	if err != nil {
		t.Fatalf("ReadFixed: %v", err)
	}
	// ReadFixed returns the destination before the kernel has populated it.
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := ring.Wait(cqes[:])
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
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
	if cqe.Op() != uring.IORING_OP_READ_FIXED || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	if !bytes.Equal(buf[:len(payload)], payload) {
		t.Fatalf("got %q, want %q", buf[:len(payload)], payload)
	}
}
