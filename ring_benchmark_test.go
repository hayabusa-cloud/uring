// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/spin"
	"code.hybscloud.com/zcall"
)

func newBenchmarkIoUring(b *testing.B, entries int) *ioUring {
	b.Helper()

	ur, err := newIoUring(entries)
	if err != nil {
		b.Skipf("newIoUring: %v", err)
	}
	b.Cleanup(func() {
		if err := ur.stop(); err != nil {
			b.Fatalf("stop: %v", err)
		}
	})
	return ur
}

// Benchmark SQE preparation (nop) - reuses single ring
func BenchmarkIoUring_Nop(b *testing.B) {
	ur := newBenchmarkIoUring(b, 4096)

	ctx := PackDirect(IORING_OP_NOP, 0, 0, 0)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := ur.nop(ctx)
		if err != nil {
			// Ring may be full, flush and retry
			_ = ur.enter()
			for {
				_, err := ur.wait()
				if err == iox.ErrWouldBlock {
					break
				}
			}
			// Retry
			_ = ur.nop(ctx)
		}
	}
}

// Benchmark SQE preparation for read
func BenchmarkIoUring_PrepareRead(b *testing.B) {
	ur := newBenchmarkIoUring(b, 4096)

	buf := make([]byte, 4096)
	ctx := PackDirect(IORING_OP_READ, 0, 0, 5)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := ur.read(ctx, 0, buf, 0, len(buf))
		if err != nil {
			_ = ur.enter()
			for {
				_, err := ur.wait()
				if err == iox.ErrWouldBlock {
					break
				}
			}
		}
	}
}

// Benchmark enter syscall
func BenchmarkIoUring_Enter(b *testing.B) {
	ur := newBenchmarkIoUring(b, 256)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ur.enter()
	}
}

// Benchmark full nop cycle (submit + wait)
func BenchmarkIoUring_NopCycle(b *testing.B) {
	ur := newBenchmarkIoUring(b, 256)

	ctx := PackDirect(IORING_OP_NOP, 0, 0, 0)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := ur.nop(ctx)
		if err != nil {
			b.Fatalf("nop: %v", err)
		}

		err = ur.enter()
		if err != nil {
			b.Fatalf("enter: %v", err)
		}

		// Wait for completion
		deadline := time.Now().Add(time.Second)
		for {
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				if time.Now().After(deadline) {
					b.Fatal("timeout waiting for completion")
				}
				continue
			}
			if err != nil {
				b.Fatalf("wait: %v", err)
			}
			if cqe.res < 0 {
				b.Fatalf("nop failed: %d", cqe.res)
			}
			break
		}
	}
}

// Benchmark Unix socket pair send/recv cycle
func BenchmarkIoUring_SocketCycle(b *testing.B) {
	ur := newBenchmarkIoUring(b, 256)

	fds, err := newUnixSocketPair()
	if err != nil {
		b.Skipf("socketpair: %v", err)
	}
	b.Cleanup(func() {
		for _, fd := range fds {
			if fd >= 0 {
				zcall.Close(uintptr(fd))
			}
		}
	})

	sendBuf := []byte("benchmark test data 1234567890")
	recvBuf := make([]byte, len(sendBuf))

	sendCtx := PackDirect(IORING_OP_SEND, 0, 0, int32(fds[0]))
	recvCtx := PackDirect(IORING_OP_RECV, 0, 0, int32(fds[1]))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Submit send
		err := ur.send(sendCtx, 0, sendBuf, 0, len(sendBuf))
		if err != nil {
			b.Fatalf("send: %v", err)
		}

		// Submit recv
		err = ur.receive(recvCtx, 0, recvBuf, 0, len(recvBuf))
		if err != nil {
			b.Fatalf("receive: %v", err)
		}

		err = ur.enter()
		if err != nil {
			b.Fatalf("enter: %v", err)
		}

		// Wait for both completions
		completed := 0
		deadline := time.Now().Add(time.Second)
		for completed < 2 {
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				if time.Now().After(deadline) {
					b.Fatal("timeout")
				}
				continue
			}
			if err != nil {
				b.Fatalf("wait: %v", err)
			}
			if cqe.res < 0 {
				b.Fatalf("op failed: %d", cqe.res)
			}
			completed++
		}
	}
}

// Benchmark timeout operation
func BenchmarkIoUring_Timeout(b *testing.B) {
	ur := newBenchmarkIoUring(b, 256)

	ctx := PackDirect(IORING_OP_TIMEOUT, 0, 0, 0)
	ts := Timespec{Sec: 0, Nsec: 1000} // 1 microsecond

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := ur.timeout(ctx, 1, &ts, 0)
		if err != nil {
			b.Fatalf("timeout: %v", err)
		}

		err = ur.enter()
		if err != nil {
			b.Fatalf("enter: %v", err)
		}

		deadline := time.Now().Add(time.Second)
		for {
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				if time.Now().After(deadline) {
					b.Fatal("timeout waiting")
				}
				continue
			}
			if err != nil {
				b.Fatalf("wait: %v", err)
			}
			// Timeout completion has -ETIME
			_ = cqe
			break
		}
	}
}

// Benchmark CQ polling overhead
func BenchmarkIoUring_Poll(b *testing.B) {
	ur := newBenchmarkIoUring(b, 256)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ur.poll(0)
	}
}

// Benchmark buffer pool creation
func BenchmarkNewRegisterBufferPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewRegisterBufferPool(64)
	}
}

// Benchmark yield
func BenchmarkYield(b *testing.B) {
	for i := 0; i < b.N; i++ {
		spin.Yield()
	}
}
