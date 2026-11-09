// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

func TestPointerContextSubmissionSnapshot(t *testing.T) {
	for _, mode := range []string{"Indirect", "Extended"} {
		t.Run(mode, func(t *testing.T) {
			ring := newTestRing(t, func(o *uring.Options) { o.NotifySucceed = true })
			file, err := os.CreateTemp(t.TempDir(), "snapshot")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.WriteString("01234payload"); err != nil {
				t.Fatal(err)
			}
			if err := ring.RegisterFilesSparse(2); err != nil {
				t.Fatal(err)
			}
			var ctx uring.SQEContext
			var ext *uring.ExtSQE
			if mode == "Indirect" {
				ind := ring.IndirectSQE()
				if ind == nil {
					t.Fatal("IndirectSQE pool exhausted")
				}
				defer ring.PutIndirectSQE(ind)
				ctx = uring.PackIndirect(ind)
			} else {
				ext = ring.ExtSQE()
				if ext == nil {
					t.Fatal("ExtSQE pool exhausted")
				}
				defer ring.PutExtSQE(ext)
				copy(ext.UserData[:], "caller data")
				ctx = uring.PackExtended(ext)
			}
			ctx = ctx.WithFD(iofd.FD(file.Fd()))
			check := func(op uint8, result int32, fd iofd.FD, addr, off uint64, length uint32) {
				t.Helper()
				cqe, ok := waitForOp(t, ring, op, 2*time.Second)
				if !ok || cqe.Res != result || !cqe.FullSQE() {
					t.Fatalf("completion: present=%v cqe=%+v, want result=%d and full SQE", ok, cqe, result)
				}
				view := cqe.SQE()
				if view.Opcode() != op || view.FD() != fd || cqe.FD() != fd ||
					view.Addr() != addr || view.Off() != off || view.Len() != length || view.UserData() != ctx.Raw() {
					t.Errorf("SQE: op=%d fd=%d completionFD=%d addr=%#x off=%d len=%d userData=%#x; want %d %d %d %#x %d %d %#x",
						view.Opcode(), view.FD(), cqe.FD(), view.Addr(), view.Off(), view.Len(), view.UserData(),
						op, fd, fd, addr, off, length, ctx.Raw())
				}
				if ext != nil && string(ext.UserData[:11]) != "caller data" {
					t.Error("submission overwrote Extended user data")
				}
			}

			buf := make([]byte, 7)
			if err := ring.Read(ctx, buf, uring.WithOffset(5)); err != nil {
				t.Fatal(err)
			}
			check(uring.IORING_OP_READ, 7, iofd.FD(file.Fd()), uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))), 5, 7)
			if string(buf) != "payload" {
				t.Fatalf("read payload=%q", buf)
			}
			runtime.KeepAlive(buf)

			// This manual encoder overrides the context FD with -1.
			fds := []int32{int32(file.Fd())}
			if err := ring.FilesUpdate(ctx, fds, 1); err != nil {
				t.Fatal(err)
			}
			check(uring.IORING_OP_FILES_UPDATE, 1, -1, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(fds)))), 1, 1)
			runtime.KeepAlive(fds)

			// Reuse only after completion; fields from earlier operations must clear.
			if err := ring.Nop(ctx); err != nil {
				t.Fatal(err)
			}
			check(uring.IORING_OP_NOP, 0, iofd.FD(ctx.FD()), 0, 0, 0)
		})
	}
}

func BenchmarkContextSubmissionCycle(b *testing.B) {
	for _, mode := range []string{"Direct", "Indirect", "Extended"} {
		b.Run(mode, func(b *testing.B) {
			ring, err := uring.New(testMinimalBufferOptions)
			if err != nil {
				b.Fatal(err)
			}
			mustStartRing(b, ring)
			ctx := uring.ForFD(0)
			switch mode {
			case "Indirect":
				ind := ring.IndirectSQE()
				if ind == nil {
					b.Fatal("IndirectSQE pool exhausted")
				}
				defer ring.PutIndirectSQE(ind)
				ctx = uring.PackIndirect(ind)
			case "Extended":
				ext := ring.ExtSQE()
				if ext == nil {
					b.Fatal("ExtSQE pool exhausted")
				}
				defer ring.PutExtSQE(ext)
				ctx = uring.PackExtended(ext)
			}
			var cqes [1]uring.CQEView
			b.ReportAllocs()
			for b.Loop() {
				if err := ring.Nop(ctx); err != nil {
					b.Fatal(err)
				}
				backoff := iox.Backoff{}
				for {
					n, err := ring.Wait(cqes[:])
					if err != nil && err != iox.ErrWouldBlock {
						b.Fatal(err)
					}
					if n != 0 {
						if cqes[0].Res != 0 || cqes[0].Op() != uring.IORING_OP_NOP {
							b.Fatalf("unexpected completion: %+v", cqes[0])
						}
						break
					}
					backoff.Wait()
				}
			}
		})
	}
}
