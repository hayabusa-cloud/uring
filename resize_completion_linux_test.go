// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"runtime"
	"testing"
)

func TestResizeRingsPendingCompletions(t *testing.T) {
	for _, width := range []struct {
		name string
		flag uint32
	}{
		{name: "CQE16"},
		{name: "CQE32", flag: IORING_SETUP_CQE32},
	} {
		t.Run(width.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ring, err := newIoUring(32, func(p *ioUringParams) {
				p.flags = IORING_SETUP_SINGLE_ISSUER | IORING_SETUP_DEFER_TASKRUN | IORING_SETUP_NO_SQARRAY | width.flag
			})
			if err != nil {
				t.Fatal(err)
			}
			defer ring.stop()
			for i := range 8 {
				if err := ring.nop(PackDirect(IORING_OP_NOP, 0, 0, int32(i+1))); err != nil {
					t.Fatal(err)
				}
			}
			if err := ring.enter(); err != nil {
				t.Fatal(err)
			}
			// Leave five real completions pending, starting at a nonzero head.
			var cqes [8]CQEView
			if n, err := ring.waitBatch(cqes[:3]); err != nil || n != 3 {
				t.Fatalf("initial completions: n=%d err=%v", n, err)
			}
			if err := ring.resizeRings(64, 128); err != nil {
				t.Fatal(err)
			}
			n, err := ring.waitBatch(cqes[:])
			if err != nil || n != 5 {
				t.Fatalf("resized completions: n=%d err=%v", n, err)
			}
			for i, cqe := range cqes[:n] {
				want := PackDirect(IORING_OP_NOP, 0, 0, int32(i+4)).Raw()
				if cqe.Context().Raw() != want || cqe.Res != 0 {
					t.Errorf("completion %d: identity %#x result %d, want %#x result 0", i, cqe.Context().Raw(), cqe.Res, want)
				}
			}
		})
	}
}
