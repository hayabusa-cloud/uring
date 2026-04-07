// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// dispatchSimplifiedMultishotCQE is a test helper for direct handler exercises.
// It intentionally skips the internal MultishotSubscription state machine and therefore
// must not be read as the authoritative runtime lifecycle.
func dispatchSimplifiedMultishotCQE(handler uring.MultishotHandler, cqe uring.CQEView) bool {
	step := uring.MultishotStep{CQE: cqe}
	if cqe.Res < 0 {
		step.Err = zcall.Errno(uintptr(-cqe.Res))
		step.Cancelled = cqe.Res == -int32(uring.ECANCELED)
	}

	keep := handler.OnMultishotStep(step) == uring.MultishotContinue
	if step.Final() {
		handler.OnMultishotStop(step.Err, step.Cancelled)
	}
	return keep
}
