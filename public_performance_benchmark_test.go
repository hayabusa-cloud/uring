// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring_test

import (
	"testing"

	"code.hybscloud.com/uring"
)

// These benchmarks cover small public API hot paths. CI renders their benchmem
// output in the GitHub Actions job summary so users can inspect current
// performance metrics from each workflow run.
var (
	benchmarkPublicPerformanceContextSink uring.SQEContext
	benchmarkPublicPerformanceRawSink     uint64
	benchmarkPublicPerformanceErrSink     error
	benchmarkPublicPerformanceBoolSink    bool
	benchmarkPublicPerformanceUint8Sink   uint8
	benchmarkPublicPerformanceUint16Sink  uint16
	benchmarkPublicPerformanceInt32Sink   int32
)

func BenchmarkPublicPerformanceSQEContextPackDirect(b *testing.B) {
	var (
		ctx   uring.SQEContext
		raw   uint64
		op    uint8
		flags uint8
		group uint16
		fd    int32
	)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx = uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, uint16(i), int32(i))
		raw ^= ctx.Raw()
		op ^= ctx.Op()
		flags ^= ctx.Flags()
		group ^= ctx.BufGroup()
		fd ^= ctx.FD()
	}

	benchmarkPublicPerformanceContextSink = ctx
	benchmarkPublicPerformanceRawSink = raw
	benchmarkPublicPerformanceUint8Sink = op ^ flags
	benchmarkPublicPerformanceUint16Sink = group
	benchmarkPublicPerformanceInt32Sink = fd
}

func BenchmarkPublicPerformanceCQEViewAccessors(b *testing.B) {
	cqe := uring.CQEView{
		Res:   4096,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER | (42 << uring.IORING_CQE_BUFFER_SHIFT),
	}
	var (
		more   bool
		buffer bool
		bufID  uint16
		err    error
	)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		more = cqe.HasMore()
		buffer = cqe.HasBuffer()
		bufID = cqe.BufID()
		err = cqe.Err()
	}

	benchmarkPublicPerformanceBoolSink = more && buffer
	benchmarkPublicPerformanceUint16Sink = bufID
	benchmarkPublicPerformanceErrSink = err
}

func BenchmarkPublicPerformanceDirectCQEAccessors(b *testing.B) {
	cqe := uring.DirectCQE{
		Res:   4096,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER | (7 << uring.IORING_CQE_BUFFER_SHIFT),
	}
	var (
		ok    bool
		more  bool
		bufID uint16
		err   error
	)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ok = cqe.IsSuccess()
		more = cqe.HasMore()
		bufID = cqe.BufID()
		err = cqe.Err()
	}

	benchmarkPublicPerformanceBoolSink = ok && more
	benchmarkPublicPerformanceUint16Sink = bufID
	benchmarkPublicPerformanceErrSink = err
}

func BenchmarkPublicPerformanceCompletionError(b *testing.B) {
	var err error

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		err = uring.CompletionError(-int32(uring.EAGAIN))
	}

	benchmarkPublicPerformanceErrSink = err
}
