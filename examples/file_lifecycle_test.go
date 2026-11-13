// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// Each CQE enables the next operation on the file:
//
//	OPENAT -> own FD -> WRITE(s) -> FSYNC -> CLOSE -> stop ring
//
// The submission method returns an admission error; CQE.Res carries the opened
// descriptor, transferred byte count, or operation result. Waiting between these
// dependent operations makes their ordering explicit.
func TestFileOpenSyncClose(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true // Observe successful writes before syncing.
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

	path := filepath.Join(t.TempDir(), "message.txt")
	// O_EXCL rejects an existing path; O_CLOEXEC prevents inheritance across exec.
	// AT_FDCWD anchors relative paths at the working directory; this path is absolute.
	if err := ring.OpenAt(uring.ForFD(uring.AT_FDCWD), path,
		zcall.O_WRONLY|zcall.O_CREAT|zcall.O_EXCL|zcall.O_CLOEXEC, 0600); err != nil {
		t.Fatalf("OpenAt: %v", err)
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
			t.Fatal("timed out waiting for open completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_OPENAT || cqe.Err() != nil {
		t.Fatalf("open completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	// OPENAT returns a newly owned descriptor in Res, not the context's directory FD.
	fd := iofd.NewFD(int(cqe.Res))
	ctx := uring.ForFD(fd.Raw())
	defer func() {
		// Clean up if the lesson exits before submitting its asynchronous close.
		if err := fd.Close(); err != nil {
			t.Errorf("close file: %v", err)
		}
	}()

	payload := []byte("A file written through io_uring.\n")
	// Pin during setup and retain the payload until every write has completed.
	var pin runtime.Pinner
	pin.Pin(&payload[0])
	defer pin.Unpin()
	for written := 0; written < len(payload); {
		// File offset and slice position both advance by the completed byte count.
		// Keep the payload unchanged until the write CQE is observed.
		if err := ring.Write(ctx, payload[written:], uring.WithOffset(int64(written))); err != nil {
			t.Fatalf("Write: %v", err)
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
				t.Fatal("timed out waiting for write completion")
			}
			backoff.Wait()
		}
		cqe = cqes[0]
		if cqe.Op() != uring.IORING_OP_WRITE || cqe.FD() != iofd.FD(ctx.FD()) ||
			cqe.Res <= 0 || int(cqe.Res) > len(payload)-written {
			t.Fatalf("write completion: opcode=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
		}
		written += int(cqe.Res)
	}

	// A write completion is separate from syncing the file to storage. Sync uses
	// FSYNC by default; persisting a newly created directory entry also needs a
	// directory sync, which is outside this file-operation lesson.
	if err := ring.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
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
			t.Fatal("timed out waiting for sync completion")
		}
		backoff.Wait()
	}
	cqe = cqes[0]
	if cqe.Op() != uring.IORING_OP_FSYNC || cqe.FD() != iofd.FD(ctx.FD()) || cqe.Res != 0 {
		t.Fatalf("sync completion: opcode=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
	}

	// Close targets the file; Stop tears down the ring after all CQEs are reaped.
	if err := ring.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	fd = iofd.InvalidFD // Close is admitted: never use or synchronously close this FD again.
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
			t.Fatal("timed out waiting for close completion")
		}
		backoff.Wait()
	}
	cqe = cqes[0]
	if cqe.Op() != uring.IORING_OP_CLOSE || cqe.FD() != iofd.FD(ctx.FD()) || cqe.Res != 0 {
		t.Fatalf("close completion: opcode=%d fd=%d result=%d error=%v", cqe.Op(), cqe.FD(), cqe.Res, cqe.Err())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("file contains %q, want %q", string(got), string(payload))
	}
}
