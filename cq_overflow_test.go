// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"testing"

	"code.hybscloud.com/iox"
)

func newCQOverflowTestRing() (*ioUring, *uint32, *uint32, *uint32) {
	head := uint32(0)
	tail := uint32(0)
	mask := uint32(0)
	sqFlags := uint32(0)
	overflow := uint32(0)

	return &ioUring{
			sq: ioUringSq{
				kFlags: &sqFlags,
			},
			cq: ioUringCq{
				kHead:     &head,
				kTail:     &tail,
				kRingMask: &mask,
				kOverflow: &overflow,
				cqes:      make([]ioUringCqe, 1),
			},
		},
		&tail,
		&sqFlags,
		&overflow
}

func TestWaitSurfacesCQOverflowWhenCQAppearsEmpty(t *testing.T) {
	ur, _, sqFlags, _ := newCQOverflowTestRing()
	*sqFlags = IORING_SQ_CQ_OVERFLOW

	tests := []struct {
		name string
		wait func() error
	}{
		{
			name: "wait",
			wait: func() error {
				_, err := ur.wait()
				return err
			},
		},
		{
			name: "waitBatch",
			wait: func() error {
				_, err := ur.waitBatch(make([]CQEView, 1))
				return err
			},
		},
		{
			name: "waitBatchDirect",
			wait: func() error {
				_, err := ur.waitBatchDirect(make([]DirectCQE, 1))
				return err
			},
		},
		{
			name: "waitBatchExtended",
			wait: func() error {
				_, err := ur.waitBatchExtended(make([]ExtCQE, 1))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.wait(); !errors.Is(err, ErrCQOverflow) {
				t.Fatalf("wait error = %v, want %v", err, ErrCQOverflow)
			}
		})
	}
}

func TestWaitReportsDroppedCQOverflowOncePerCounterValue(t *testing.T) {
	ur, _, _, overflow := newCQOverflowTestRing()
	cqes := make([]CQEView, 1)

	*overflow = 1
	if _, err := ur.waitBatch(cqes); !errors.Is(err, ErrCQOverflow) {
		t.Fatalf("first waitBatch error = %v, want %v", err, ErrCQOverflow)
	}
	if _, err := ur.waitBatch(cqes); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("second waitBatch error = %v, want %v", err, iox.ErrWouldBlock)
	}

	*overflow = 2
	if _, err := ur.waitBatch(cqes); !errors.Is(err, ErrCQOverflow) {
		t.Fatalf("third waitBatch error = %v, want %v", err, ErrCQOverflow)
	}
}

func TestWaitStillDrainsVisibleCQEsDuringCQOverflow(t *testing.T) {
	ur, tail, sqFlags, overflow := newCQOverflowTestRing()
	*sqFlags = IORING_SQ_CQ_OVERFLOW
	*overflow = 1
	*tail = 1
	ur.cq.cqes[0] = ioUringCqe{
		userData: PackDirect(IORING_OP_NOP, 0, 0, 7).Raw(),
		res:      42,
	}

	cqe, err := ur.wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if cqe == nil {
		t.Fatal("wait returned nil CQE")
	}
	if cqe.res != 42 {
		t.Fatalf("cqe.res = %d, want 42", cqe.res)
	}
}
