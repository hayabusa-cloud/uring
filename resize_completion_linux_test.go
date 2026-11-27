// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"runtime"
	"slices"
	"testing"
)

func TestResizeRingsPendingCompletions(t *testing.T) {
	const submitted, reaped = 10, 3
	for _, width := range []struct {
		name                 string
		setupFlags, nopFlags uint32
	}{
		{name: "CQE16"},
		{name: "CQE32", setupFlags: IORING_SETUP_CQE32, nopFlags: IORING_NOP_CQE32},
	} {
		t.Run(width.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ring, err := newIoUring(32, func(p *ioUringParams) {
				p.flags = IORING_SETUP_SINGLE_ISSUER | IORING_SETUP_DEFER_TASKRUN | IORING_SETUP_NO_SQARRAY | width.setupFlags
			})
			if err != nil {
				t.Fatal(err)
			}
			defer ring.stop()
			for i := range submitted {
				id := uint64(i + 1)
				ctx := PackDirect(IORING_OP_NOP, 0, 0, int32(id))
				// IORING_NOP_CQE32 echoes off and addr into the two extra words.
				if err := ring.submitPacked6(ctx, 0, id, ^id, 0, width.nopFlags); err != nil {
					t.Fatal(err)
				}
			}
			awaitVisibleCQ(t, ring, submitted)
			var cqes [submitted]CQEView
			if n, err := ring.waitBatch(cqes[:reaped]); err != nil || n != reaped {
				t.Fatalf("initial completions: n=%d err=%v", n, err)
			}

			// Slots 3..9 wrap on shrink and become contiguous again on growth.
			// Snapshot the full records while the CQ still owns them.
			stride := ring.cq.cqeStride
			pending := slices.Clone(ring.cq.cqes[reaped*stride : submitted*stride])
			for _, size := range []int{8, 128} {
				if err := ring.resizeRings(uint32(size/2), uint32(size)); err != nil {
					t.Fatal(err)
				}
				for i := range submitted - reaped {
					slot := (reaped + i) & (size - 1)
					got := ring.cq.cqes[slot*stride : (slot+1)*stride]
					want := pending[i*stride : (i+1)*stride]
					if !slices.Equal(got, want) {
						t.Fatalf("CQ size %d, slot %d: got %#v, want %#v", size, slot, got, want)
					}
				}
			}
			n, err := ring.waitBatch(cqes[:])
			if err != nil || n != submitted-reaped {
				t.Fatalf("resized completions: n=%d err=%v", n, err)
			}
			for i, cqe := range cqes[:n] {
				want := PackDirect(IORING_OP_NOP, 0, 0, int32(reaped+i+1)).Raw()
				if cqe.Context().Raw() != want || cqe.Res != 0 {
					t.Errorf("completion %d: identity %#x result %d, want %#x result 0", i, cqe.Context().Raw(), cqe.Res, want)
				}
			}
		})
	}
}
