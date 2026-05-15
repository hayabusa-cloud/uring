// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

var publicPerformanceSocketIOSizes = [...]struct {
	name string
	size int
}{
	{name: "512B", size: 512},
	{name: "4KiB", size: 4 * 1024},
	{name: "16KiB", size: 16 * 1024},
}

var publicPerformanceStorageIOSizes = [...]struct {
	name string
	size int
}{
	{name: "512B", size: 512},
	{name: "4KiB", size: 4 * 1024},
	{name: "64KiB", size: 64 * 1024},
}

var publicPerformanceZeroCopySizes = [...]struct {
	name string
	size int
}{
	{name: "2KiB", size: 2 * 1024},
	{name: "8KiB", size: 8 * 1024},
	{name: "64KiB", size: 64 * 1024},
}

var publicPerformanceZeroCopyDestinationCounts = [...]int{1, 4, 16}

func BenchmarkPublicPerformanceSocketIORoundTrip(b *testing.B) {
	for _, tc := range publicPerformanceSocketIOSizes {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkPublicPerformanceSocketIORoundTrip(b, tc.size)
		})
	}
}

func benchmarkPublicPerformanceSocketIORoundTrip(b *testing.B, payloadSize int) {
	ring := newPublicPerformanceIORing(b)

	fds, err := newUnixSocketPairForTest()
	if err != nil {
		b.Skipf("socketpair: %v", err)
	}
	b.Cleanup(func() {
		closeTestFds(fds)
	})

	sendFD := &publicPerformancePollFD{fd: fds[0]}
	recvFD := &publicPerformancePollFD{fd: fds[1]}
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	recvBuf := make([]byte, len(payload))
	cqes := make([]uring.CQEView, 4)
	expected := int32(len(payload))

	b.SetBytes(int64(len(payload) * 2))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sendCtx := uring.PackDirect(uring.IORING_OP_SEND, 0, uint16(i), int32(fds[0]))
		recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, uint16(i), int32(fds[1]))

		if err := ring.Receive(recvCtx, recvFD, recvBuf); err != nil {
			b.Fatalf("Receive: %v", err)
		}
		if err := ring.Send(sendCtx, sendFD, payload); err != nil {
			b.Fatalf("Send: %v", err)
		}

		benchmarkPublicPerformanceInt32Sink ^= waitPublicPerformanceCQEs(b, ring, cqes, 2, expected)
	}
}

func BenchmarkPublicPerformanceStorageIOWriteRead(b *testing.B) {
	for _, tc := range publicPerformanceStorageIOSizes {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkPublicPerformanceStorageIOWriteRead(b, tc.size)
		})
	}
}

func benchmarkPublicPerformanceStorageIOWriteRead(b *testing.B, payloadSize int) {
	ring := newPublicPerformanceIORing(b)

	f, err := os.CreateTemp("", "uring-public-performance-storage-*")
	if err != nil {
		b.Fatalf("CreateTemp: %v", err)
	}
	b.Cleanup(func() {
		name := f.Name()
		if err := f.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
		if err := os.Remove(name); err != nil {
			b.Fatalf("Remove: %v", err)
		}
	})

	fd := int32(f.Fd())
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	readBuf := make([]byte, len(payload))
	cqes := make([]uring.CQEView, 4)
	expected := int32(len(payload))

	b.SetBytes(int64(len(payload) * 2))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writeCtx := uring.PackDirect(uring.IORING_OP_WRITE, 0, uint16(i), fd)
		readCtx := uring.PackDirect(uring.IORING_OP_READ, 0, uint16(i), fd)

		if err := ring.Write(writeCtx, payload); err != nil {
			b.Fatalf("Write: %v", err)
		}
		benchmarkPublicPerformanceInt32Sink ^= waitPublicPerformanceCQEs(b, ring, cqes, 1, expected)

		if err := ring.Read(readCtx, readBuf); err != nil {
			b.Fatalf("Read: %v", err)
		}
		benchmarkPublicPerformanceInt32Sink ^= waitPublicPerformanceCQEs(b, ring, cqes, 1, expected)
	}
}

func BenchmarkPublicPerformanceZeroCopyIO(b *testing.B) {
	for _, size := range publicPerformanceZeroCopySizes {
		for _, dests := range publicPerformanceZeroCopyDestinationCounts {
			targetName := fmt.Sprintf("%dTargets", dests)
			if dests == 1 {
				targetName = "1Target"
			}
			b.Run(fmt.Sprintf("%s/%s", size.name, targetName), func(b *testing.B) {
				benchmarkPublicPerformanceZeroCopyIO(b, size.size, dests)
			})
		}
	}
}

func benchmarkPublicPerformanceZeroCopyIO(b *testing.B, payloadSize, destinationCount int) {
	ring := newPublicPerformanceIORing(b)

	if ring.RegisteredBufferCount() == 0 {
		b.Skip("no registered buffers")
	}
	regBuf := ring.RegisteredBuffer(0)
	if len(regBuf) < payloadSize {
		b.Skipf("registered buffer too small: got %d, want %d", len(regBuf), payloadSize)
	}
	for i := 0; i < payloadSize; i++ {
		regBuf[i] = byte(i)
	}

	pairs := make([][2]iofd.FD, 0, destinationCount)
	writers := make([]iofd.FD, 0, destinationCount)
	stoppers := make([]func(), 0, destinationCount)
	for i := 0; i < destinationCount; i++ {
		fds, err := newUnixSocketPairForTest()
		if err != nil {
			for _, stop := range stoppers {
				stop()
			}
			for _, pair := range pairs {
				closeTestFds(pair)
			}
			b.Skipf("socketpair: %v", err)
		}
		pairs = append(pairs, fds)
		writers = append(writers, fds[1])
		stoppers = append(stoppers, startSocketPairDrainer(b, fds[0]))
	}
	b.Cleanup(func() {
		for i := len(stoppers) - 1; i >= 0; i-- {
			stoppers[i]()
		}
		for _, pair := range pairs {
			closeTestFds(pair)
		}
	})

	cqeCap := 16
	if n := destinationCount * 4; n > cqeCap {
		cqeCap = n
	}
	cqes := make([]uring.CQEView, cqeCap)
	targets := publicPerformanceTargets{fds: writers}

	b.SetBytes(int64(payloadSize * destinationCount))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx := uring.PackDirect(uring.IORING_OP_SEND_ZC, 0, uint16(i), 0)
		if err := ring.MulticastZeroCopy(ctx, targets, 0, 0, payloadSize); err != nil {
			b.Fatalf("MulticastZeroCopy: %v", err)
		}
		benchmarkPublicPerformanceInt32Sink ^= waitPublicPerformanceZeroCopyCQEs(b, ring, cqes, destinationCount)
	}
}

func newPublicPerformanceIORing(b *testing.B) *uring.Uring {
	b.Helper()

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.NotifySucceed = true
	})
	if err != nil {
		b.Skipf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		_ = ring.Stop()
		b.Skipf("Start: %v", err)
	}
	b.Cleanup(func() {
		if err := ring.Stop(); err != nil {
			b.Fatalf("Stop: %v", err)
		}
	})
	return ring
}

type publicPerformancePollFD struct {
	fd iofd.FD
}

func (p *publicPerformancePollFD) Fd() int          { return int(p.fd) }
func (p *publicPerformancePollFD) Events() uint32   { return 0 }
func (p *publicPerformancePollFD) SetEvents(uint32) {}

type publicPerformanceTargets struct {
	fds []iofd.FD
}

func (t publicPerformanceTargets) Count() int { return len(t.fds) }
func (t publicPerformanceTargets) FD(i int) iofd.FD {
	return t.fds[i]
}

func waitPublicPerformanceCQEs(b *testing.B, ring *uring.Uring, cqes []uring.CQEView, want int, wantRes int32) int32 {
	b.Helper()

	var (
		backoff  iox.Backoff
		deadline time.Time
		seen     int
		res      int32
	)
	for seen < want {
		n, err := ring.Wait(cqes)
		switch {
		case err == nil:
		case err == iox.ErrWouldBlock, err == uring.ErrExists:
		default:
			b.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			if deadline.IsZero() {
				deadline = time.Now().Add(time.Second)
			}
			if time.Now().After(deadline) {
				b.Fatal("timeout waiting for completion")
			}
			backoff.Wait()
			continue
		}

		backoff.Reset()
		for i := 0; i < n && seen < want; i++ {
			if cqes[i].Res < 0 {
				b.Fatalf("completion failed: %d", cqes[i].Res)
			}
			if cqes[i].Res != wantRes {
				b.Fatalf("completion result = %d, want %d", cqes[i].Res, wantRes)
			}
			res ^= cqes[i].Res
			seen++
		}
	}
	return res
}

func waitPublicPerformanceZeroCopyCQEs(b *testing.B, ring *uring.Uring, cqes []uring.CQEView, want int) int32 {
	b.Helper()

	var (
		backoff  iox.Backoff
		deadline time.Time
		seen     int
		res      int32
	)
	for seen < want {
		n, err := ring.Wait(cqes)
		switch {
		case err == nil:
		case err == iox.ErrWouldBlock, err == uring.ErrExists:
		default:
			b.Fatalf("Wait: %v", err)
		}
		if n == 0 {
			if deadline.IsZero() {
				deadline = time.Now().Add(time.Second)
			}
			if time.Now().After(deadline) {
				b.Fatal("timeout waiting for zero-copy completion")
			}
			backoff.Wait()
			continue
		}

		backoff.Reset()
		for i := 0; i < n && seen < want; i++ {
			cqe := cqes[i]
			switch cqe.Op() {
			case uring.IORING_OP_SEND_ZC:
				if cqe.IsNotification() {
					continue
				}
				if cqe.Res < 0 && cqe.Res != -int32(uring.EOPNOTSUPP) {
					b.Fatalf("zero-copy completion failed: %v", uring.CompletionError(cqe.Res))
				}
				res ^= cqe.Res
				seen++
			case uring.IORING_OP_SEND:
				if cqe.Res < 0 {
					b.Fatalf("send completion failed: %v", uring.CompletionError(cqe.Res))
				}
				res ^= cqe.Res
				seen++
			}
		}
	}
	return res
}
