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

// WriteV submits several payload slices as one operation. The caller retains
// all backing arrays until completion. The CQE reports the total bytes written
// across the slices; a short write may consume only a prefix.
//
//	["header:"] ["payload"] [":trailer"]
//	                 | WriteV gathers in slice order
//	                 v
//	       "header:payload:trailer"
//
// The header and body can stay in separate buffers. bytes.Join below creates
// the expected test value; WriteV receives the original slices.
func TestVectoredWrite(t *testing.T) {
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
	parts := [][]byte{[]byte("header:"), []byte("payload"), []byte(":trailer")}
	want := bytes.Join(parts, nil)
	ctx := uring.PackDirect(uring.IORING_OP_WRITEV, 0, 0, int32(f.Fd()))
	if err := ring.WriteV(ctx, parts); err != nil {
		t.Fatalf("WriteV: %v", err)
	}
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
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
	if cqe.Op() != uring.IORING_OP_WRITEV || cqe.Res != int32(len(want)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	runtime.KeepAlive(parts)
	got := make([]byte, len(want))
	if _, err := f.ReadAt(got, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ReadV scatters bytes across destination slices in order:
//
//	       "header:payload:trailer"
//	                 | ReadV
//	                 v
//	[header: 7 bytes] [payload: 7 bytes] [trailer: 8 bytes]
//
// Slice lengths define the boundaries. A short read may fill only a prefix;
// inspect the CQE byte count before using the destination bytes.
func TestVectoredRead(t *testing.T) {
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
	want := []byte("header:payload:trailer")
	if _, err := f.WriteAt(want, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	var header [7]byte
	var payload [7]byte
	var trailer [8]byte
	parts := [][]byte{header[:], payload[:], trailer[:]}
	ctx := uring.PackDirect(uring.IORING_OP_READV, 0, 0, int32(f.Fd()))
	if err := ring.ReadV(ctx, parts); err != nil {
		t.Fatalf("ReadV: %v", err)
	}
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
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
	if cqe.Op() != uring.IORING_OP_READV || cqe.Res != int32(len(want)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	if got := bytes.Join(parts, nil); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
