// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "testing"

func newCQStrideTestRing() *ioUring {
	head := uint32(0)
	tail := uint32(2)
	mask := uint32(1)
	overflow := uint32(0)

	return &ioUring{
		cq: ioUringCq{
			kHead:     &head,
			kTail:     &tail,
			kRingMask: &mask,
			kOverflow: &overflow,
			cqes:      make([]ioUringCqe, 4),
			cqeStride: cqeStrideFromFlags(IORING_SETUP_CQE32),
		},
	}
}

func TestWaitUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 2).Raw(), res: 22}

	first, err := ur.wait()
	if err != nil {
		t.Fatalf("wait first: %v", err)
	}
	if got := first.res; got != 11 {
		t.Fatalf("first.res = %d, want 11", got)
	}

	second, err := ur.wait()
	if err != nil {
		t.Fatalf("wait second: %v", err)
	}
	if got := second.res; got != 22 {
		t.Fatalf("second.res = %d, want 22", got)
	}
	if got := SQEContextFromRaw(second.userData).FD(); got != 2 {
		t.Fatalf("second.userData FD = %d, want 2", got)
	}
}

func TestWaitBatchUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 2).Raw(), res: 22}

	cqes := make([]CQEView, 2)
	n, err := ur.waitBatch(cqes)
	if err != nil {
		t.Fatalf("waitBatch: %v", err)
	}
	if n != 2 {
		t.Fatalf("waitBatch count = %d, want 2", n)
	}
	if got := cqes[1].Res; got != 22 {
		t.Fatalf("cqes[1].Res = %d, want 22", got)
	}
	if got := cqes[1].ctx.FD(); got != 2 {
		t.Fatalf("cqes[1].ctx.FD = %d, want 2", got)
	}
}

func TestWaitBatchDirectUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, IOSQE_IO_LINK, 3, 1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackDirect(IORING_OP_RECV, IOSQE_BUFFER_SELECT, 7, 2).Raw(), res: 22}

	cqes := make([]DirectCQE, 2)
	n, err := ur.waitBatchDirect(cqes)
	if err != nil {
		t.Fatalf("waitBatchDirect: %v", err)
	}
	if n != 2 {
		t.Fatalf("waitBatchDirect count = %d, want 2", n)
	}
	if got := cqes[1].Res; got != 22 {
		t.Fatalf("cqes[1].Res = %d, want 22", got)
	}
	if got := cqes[1].FD; got != 2 {
		t.Fatalf("cqes[1].FD = %d, want 2", got)
	}
	if got := cqes[1].BufGroup; got != 7 {
		t.Fatalf("cqes[1].BufGroup = %d, want 7", got)
	}
}

func TestWaitBatchExtendedUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ext1 := &ExtSQE{}
	ext2 := &ExtSQE{}
	ur.cq.cqes[0] = ioUringCqe{userData: PackExtended(ext1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackExtended(ext2).Raw(), res: 22}

	cqes := make([]ExtCQE, 2)
	n, err := ur.waitBatchExtended(cqes)
	if err != nil {
		t.Fatalf("waitBatchExtended: %v", err)
	}
	if n != 2 {
		t.Fatalf("waitBatchExtended count = %d, want 2", n)
	}
	if got := cqes[1].Res; got != 22 {
		t.Fatalf("cqes[1].Res = %d, want 22", got)
	}
	if cqes[1].Ext != ext2 {
		t.Fatalf("cqes[1].Ext = %p, want %p", cqes[1].Ext, ext2)
	}
}
