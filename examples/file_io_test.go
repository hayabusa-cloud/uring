// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// Submission accepts work; the CQE tells the caller how it finished:
//
//  1. PackDirect -> Write -> Wait -> inspect write CQE
//  2. ctx.WithOp -> Read  -> Wait -> inspect read CQE and bytes
//
// Keep each payload or destination valid until its completion. Wait may return
// no CQEs yet; back off between attempts and reset the backoff after progress.
func TestFileWriteRead(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Successful writes must produce CQEs.
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
	payload := []byte("Hello, io_uring!")
	// Zero flags add no submission modifiers; ordinary Write leaves BufGroup unused.
	ctx := uring.PackDirect(uring.IORING_OP_WRITE, 0, 0, int32(f.Fd()))
	// Write queues work; Wait submits it and reaps available completions.
	if err := ring.Write(ctx, payload); err != nil {
		t.Fatalf("Write: %v", err)
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
			t.Fatal("timed out waiting for write completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_WRITE || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}

	// Start the read only after the write has completed.
	buf := make([]byte, len(payload))
	if err := ring.Read(ctx.WithOp(uring.IORING_OP_READ), buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
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
			t.Fatal("timed out waiting for read completion")
		}
		backoff.Wait()
	}
	cqe = cqes[0]
	if cqe.Op() != uring.IORING_OP_READ || cqe.Res != int32(len(payload)) {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("read %q, want %q", buf, payload)
	}
}

// Read a regular file in small chunks, using each CQE to decide what comes next:
//
//	CQE.Res < 0   operation failed
//	CQE.Res > 0   consume these bytes and advance the file offset
//	CQE.Res = 0   EOF, when the requested buffer is nonempty
//
// A short read is still progress; submit another read to observe EOF. Explicit
// offsets leave the file's shared position unchanged. WithOffset(-1) instead
// uses and advances that shared position, so such operations need serialization.
func TestFileReadUntilEOF(t *testing.T) {
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
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	const content = "A file read in small chunks."
	f := tempFile(t)
	// WriteAt prepares the fixture without advancing its shared file position.
	if _, err := f.WriteAt([]byte(content), 0); err != nil {
		t.Fatalf("prepare file: %v", err)
	}
	ctx := uring.ForFD(int32(f.Fd()))
	var buf [8]byte
	// Pin once so the kernel's buffer address stays valid between submission and
	// completion, even if the goroutine's stack moves while waiting.
	var pin runtime.Pinner
	pin.Pin(&buf[0])
	defer pin.Unpin() // All reads have completed before the buffer is unpinned.
	var got [len(content)]byte
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	offset := 0
	for {
		// The default offset is zero; supply the next position on every read.
		if err := ring.Read(ctx, buf[:], uring.WithOffset(int64(offset))); err != nil {
			t.Fatalf("Read: %v", err)
		}
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
				t.Fatal("timed out waiting for read completion")
			}
			backoff.Wait()
		}
		cqe := cqes[0]
		if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != iofd.FD(ctx.FD()) || cqe.Err() != nil {
			t.Fatalf("read completion: opcode=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
		}
		n := int(cqe.Res)
		if n == 0 {
			break // EOF is a successful zero-byte read, not an empty CQ.
		}
		if n > len(buf) || n > len(got)-offset {
			t.Fatalf("unexpected read length %d at offset %d", n, offset)
		}
		// Consume only the reported bytes before reusing buf for another request.
		copy(got[offset:], buf[:n])
		offset += n
	}
	if offset != len(content) || string(got[:offset]) != content {
		t.Fatalf("read %q (%d bytes), want %q", string(got[:offset]), offset, content)
	}
}

// Queue independent writes before collecting their completions. Each request
// carries an ID because CQEs need not arrive in submission order or one at a time.
func TestBatchedFileOps(t *testing.T) {
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

	var files [4]*os.File
	var payloads [4][]byte
	for i := range files {
		files[i] = tempFile(t)
		payloads[i] = fmt.Appendf(nil, "file %d", i)
		ctx := uring.PackDirect(uring.IORING_OP_WRITE, 0, uint16(i), int32(files[i].Fd()))
		if err := ring.Write(ctx, payloads[i]); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	var seen [4]bool
	var cqes [4]uring.CQEView
	var backoff iox.Backoff
	completed := 0
	deadline := time.Now().Add(3 * time.Second)
	for completed < len(files) {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for writes")
			}
			backoff.Wait()
			continue
		}
		backoff.Reset()
		// Process every returned CQE, even when several arrive together.
		for _, cqe := range cqes[:n] {
			i := int(cqe.BufGroup()) // Caller ID on an ordinary write.
			if cqe.Op() != uring.IORING_OP_WRITE || i >= len(files) || seen[i] {
				t.Fatalf("unexpected or duplicate completion: %+v", cqe)
			}
			if cqe.FD() != iofd.FD(files[i].Fd()) || cqe.Res != int32(len(payloads[i])) {
				t.Fatalf("file %d: fd=%d result=%d error=%v", i, cqe.FD(), cqe.Res, cqe.Err())
			}
			seen[i] = true
			completed++
		}
	}
	// The caller retains every submitted payload until its completion.
	runtime.KeepAlive(payloads)
	for i, f := range files {
		got := make([]byte, len(payloads[i]))
		if _, err := f.ReadAt(got, 0); err != nil {
			t.Fatalf("ReadAt: %v", err)
		}
		if !bytes.Equal(got, payloads[i]) {
			t.Fatalf("file %d: got %q, want %q", i, got, payloads[i])
		}
	}
}

// TestBatchedNops handles SQ saturation and accounts for every accepted request.
// ErrWouldBlock leaves the current ID unsubmitted. Keep separate submitted and
// completed counts so a retry cannot duplicate work already accepted by the ring.
func TestBatchedNops(t *testing.T) {
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

	const count = 1000
	var seen [count]bool
	var cqes [32]uring.CQEView
	var backoff iox.Backoff
	submitted, completed := 0, 0
	deadline := time.Now().Add(3 * time.Second)
	for completed < count && time.Now().Before(deadline) {
		for submitted < count {
			ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, uint16(submitted), 0)
			err := ring.Nop(ctx)
			if iox.IsWouldBlock(err) {
				break // Reap/submit progress before retrying this same ID.
			}
			if err != nil {
				t.Fatalf("Nop %d: %v", submitted, err)
			}
			submitted++
		}
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			id := int(cqe.BufGroup())
			if cqe.Op() != uring.IORING_OP_NOP || id >= submitted || seen[id] {
				t.Fatalf("unexpected or duplicate NOP completion: %+v", cqe)
			}
			if cqe.Res != 0 {
				t.Fatalf("NOP %d: result=%d error=%v", id, cqe.Res, cqe.Err())
			}
			seen[id] = true
			completed++
		}
	}
	if submitted != count || completed != submitted {
		t.Fatalf("submitted=%d completed=%d, want %d", submitted, completed, count)
	}
}
