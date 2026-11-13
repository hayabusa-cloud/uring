// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// Submission, reaping, and request completion report different facts:
//
//	Observation           Meaning
//	Read/Nop returns nil  Request accepted for submission
//	Wait returns n > 0    n CQEs removed from the ring for caller processing
//	CQE.Res >= 0          Successful result, interpreted by opcode
//	CQE.Res < 0           Request failed with kernel -errno
//
// One request's failure does not cancel unrelated requests. Process every CQE
// in each returned batch and correlate by context, not arrival order.
func TestCompletionBatchErrors(t *testing.T) {
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

	// A valid descriptor opened write-only gives a deterministic READ error.
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "write-only"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("file Close: %v", err)
		}
	}()
	// Ring-owned storage stays at a stable address until Stop. Even a request
	// expected to fail retains its buffer until its CQE has been processed.
	buf := ring.RegisteredBuffer(0)[:1]
	ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, int32(file.Fd()))
	if err := ring.Read(ctx, buf); err != nil {
		t.Fatalf("Read admission: %v", err)
	}
	for id := uint16(1); id < 3; id++ {
		// Neither READ nor NOP selects a buffer here, so BufGroup carries our
		// caller ID. No IO_LINK flag joins these independent requests.
		ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, id, 0)
		if err := ring.Nop(ctx); err != nil {
			t.Fatalf("Nop admission: %v", err)
		}
	}

	var cqes [3]uring.CQEView
	var seen [3]bool
	var backoff iox.Backoff
	completed := 0
	deadline := time.Now().Add(exampleTimeout)
	for completed < len(seen) {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for completions")
			}
			backoff.Wait()
			continue
		}
		backoff.Reset()
		for _, cqe := range cqes[:n] {
			id := int(cqe.BufGroup())
			if id >= len(seen) || seen[id] {
				t.Errorf("unexpected or duplicate completion ID: %d", id)
				continue
			}
			seen[id] = true
			completed++ // A failed request is still a completed request.
			if id == 0 {
				if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != iofd.FD(file.Fd()) || cqe.Res != -int32(zcall.EBADF) || cqe.Err() == nil {
					t.Errorf("read completion: op=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
				}
			} else if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 || cqe.Err() != nil {
				t.Errorf("nop %d: op=%d result=%d error=%v", id, cqe.Op(), cqe.Res, cqe.Err())
			}
			// Continue through the whole batch, including CQEs after the error.
		}
	}
}
