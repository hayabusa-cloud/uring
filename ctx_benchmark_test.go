// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/uring"
)

var (
	benchmarkMultishotSubscriberSink *uring.MultishotSubscriber
	benchmarkMultishotHandlerSink    uring.MultishotHandler
	benchmarkMultishotActionSink     uring.MultishotAction
)

// Benchmark PackDirect - the most common context packing operation
func BenchmarkPackDirect(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, 1, 5)
	}
}

// Benchmark context field extraction
func BenchmarkSQEContext_Op(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, 1, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.Op()
	}
}

func BenchmarkSQEContext_Flags(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, 1, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.Flags()
	}
}

func BenchmarkSQEContext_BufGroup(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, 1, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.BufGroup()
	}
}

func BenchmarkSQEContext_FD(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, 1, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.FD()
	}
}

// Benchmark all field extractions together
func BenchmarkSQEContext_AllFields(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_FIXED_FILE, 1, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.Op()
		_ = ctx.Flags()
		_ = ctx.BufGroup()
		_ = ctx.FD()
	}
}

// Benchmark With* modifier methods
func BenchmarkSQEContext_WithOp(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.WithOp(uring.IORING_OP_RECV)
	}
}

func BenchmarkSQEContext_WithFD(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.WithFD(5)
	}
}

func BenchmarkSQEContext_WithFlags(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.WithFlags(uring.IOSQE_FIXED_FILE)
	}
}

// Benchmark chained modifications
func BenchmarkSQEContext_ChainedWith(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.WithOp(uring.IORING_OP_RECV).WithFlags(uring.IOSQE_FIXED_FILE).WithFD(5)
	}
}

// Benchmark ForFD convenience constructor
func BenchmarkForFD(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = uring.ForFD(5)
	}
}

// Benchmark mode checks
func BenchmarkSQEContext_IsDirect(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.IsDirect()
	}
}

func BenchmarkSQEContext_Mode(b *testing.B) {
	ctx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.Mode()
	}
}

// Benchmark CQEView field access
func BenchmarkCQEView_AllFields(b *testing.B) {
	cqe := uring.CQEView{
		Res:   1024,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cqe.Res
		_ = cqe.Flags
		_ = cqe.HasMore()
		_ = cqe.HasBuffer()
	}
}

// Benchmark CQE flag checks
func BenchmarkCQEView_More(b *testing.B) {
	cqe := uring.CQEView{Flags: uring.IORING_CQE_F_MORE}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cqe.HasMore()
	}
}

func BenchmarkCQEView_HasBuffer(b *testing.B) {
	cqe := uring.CQEView{Flags: uring.IORING_CQE_F_BUFFER}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cqe.HasBuffer()
	}
}

func BenchmarkCQEView_BufID(b *testing.B) {
	cqe := uring.CQEView{Flags: uring.IORING_CQE_F_BUFFER | (42 << 16)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cqe.BufID()
	}
}

// Benchmark subscriber creation
func BenchmarkNewMultishotSubscriber(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkMultishotSubscriberSink = uring.NewMultishotSubscriber()
	}
}

func BenchmarkMultishotSubscriber_Build(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkMultishotSubscriberSink = uring.NewMultishotSubscriber().
			OnStep(func(step uring.MultishotStep) uring.MultishotAction {
				if step.Err == nil {
					return uring.MultishotContinue
				}
				return uring.MultishotStop
			})
	}
}

func BenchmarkMultishotSubscriber_Handler(b *testing.B) {
	sub := uring.NewMultishotSubscriber()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkMultishotHandlerSink = sub.Handler()
	}
}

// Benchmark handler dispatch overhead.
func BenchmarkMultishotHandler_OnMultishotStep(b *testing.B) {
	handler := uring.NewMultishotSubscriber().Handler()
	step := uring.MultishotStep{CQE: uring.CQEView{Res: 1, Flags: uring.IORING_CQE_F_MORE}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkMultishotActionSink = handler.OnMultishotStep(step)
	}
}

func BenchmarkNoopMultishotHandler_OnMultishotStep(b *testing.B) {
	var handler uring.NoopMultishotHandler
	step := uring.MultishotStep{CQE: uring.CQEView{Res: 1, Flags: uring.IORING_CQE_F_MORE}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkMultishotActionSink = handler.OnMultishotStep(step)
	}
}
