// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "sync/atomic"

// cancelRequest keeps cancellation observation storage with its owner. A
// published cancel may outlive the original request, including after IO_DRAIN.
type cancelRequest struct {
	slot    pooledExtSQE
	pending atomic.Bool
}

type cancelOwner interface {
	finishCancellation()
}

func (c *cancelRequest) prepare(owner cancelOwner) SQEContext {
	c.slot.anchors.owner = owner
	CastUserData[Ctx0](&c.slot.ext).Fn = cancelCQEHandler
	c.pending.Store(true)
	return PackExtended(&c.slot.ext)
}

func (c *cancelRequest) complete() bool {
	if !c.pending.Swap(false) {
		return false
	}
	c.slot.anchors.clear()
	return true
}

func cancelCQEHandler(_ *Uring, _ *ioUringSqe, cqe *ioUringCqe) {
	ext := SQEContextFromRaw(cqe.userData).ExtSQE()
	owner, _ := extAnchors(ext).owner.(cancelOwner)
	if owner != nil {
		owner.finishCancellation()
	}
}
