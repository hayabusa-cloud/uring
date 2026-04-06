// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"testing"

	"code.hybscloud.com/zcall"
)

const testInternalLockedBufferMem = 1 << 18

func testMinimalBufferOptions(opt *Options) {
	opt.LockedBufferMem = testInternalLockedBufferMem
}

func mustStartRing(tb testing.TB, ring *Uring) {
	tb.Helper()

	if err := ring.Start(); err != nil {
		tb.Fatalf("Start: %v", err)
	}
	tb.Cleanup(func() {
		if err := ring.Stop(); err != nil {
			tb.Fatalf("Stop: %v", err)
		}
	})
}

func dispatchSimplifiedMultishotCQE(handler MultishotHandler, cqe CQEView) bool {
	step := MultishotStep{CQE: cqe}
	if cqe.Res < 0 {
		step.Err = zcall.Errno(uintptr(-cqe.Res))
		step.Cancelled = cqe.Res == -int32(ECANCELED)
	}
	keep := handler.OnMultishotStep(step) == MultishotContinue
	if step.Final() {
		handler.OnMultishotStop(step.Err, step.Cancelled)
	}
	return keep
}
