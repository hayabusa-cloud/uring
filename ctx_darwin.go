// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

// IndirectSQE returns the borrowed indirect target.
// Caller must ensure c.IsIndirect() and that the target remains live.
//
//go:nosplit
//go:nocheckptr
func (c SQEContext) IndirectSQE() *IndirectSQE {
	if !c.IsIndirect() {
		panic("uring: SQEContext is not in Indirect mode")
	}
	return (*IndirectSQE)(pointerFromTagged(uintptr(c & ctxPtrMask)))
}

// ExtSQE returns the borrowed extended target.
// Caller must ensure c.IsExtended() and that the target remains live.
//
//go:nosplit
//go:nocheckptr
func (c SQEContext) ExtSQE() *ExtSQE {
	if !c.IsExtended() {
		panic("uring: SQEContext is not in Extended mode")
	}
	return (*ExtSQE)(pointerFromTagged(uintptr(c & ctxPtrMask)))
}
