// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"

	"code.hybscloud.com/dwcas"
	"code.hybscloud.com/iox"
)

// lockSubmitState takes the shared-submit lock when submit-state operations may
// arrive from multiple goroutines. Single-issuer rings intentionally skip this
// lock and rely on caller serialization of submit, Wait/enter, Stop, and
// ResizeRings so the hot path avoids shared synchronization overhead.
func (ur *ioUring) lockSubmitState() {
	if !ur.submit.shared {
		return
	}
	ur.submit.lock.Lock()
}

func (ur *ioUring) unlockSubmitState() {
	if !ur.submit.shared {
		return
	}
	ur.submit.lock.Unlock()
}

// reserveSubmitSlot reserves the next SQ slot and stores staging roots that
// must survive until kernel consumption.
func (ur *ioUring) reserveSubmitSlot() (submitSlot, error) {
	ur.lockSubmitState()
	ur.releaseConsumedKeepAlive()

	h := atomic.LoadUint32(ur.sq.kHead)
	t := atomic.LoadUint32(ur.sq.kTail)
	if t-h >= *ur.sq.kRingEntries {
		ur.unlockSubmitState()
		return submitSlot{}, iox.ErrWouldBlock
	}

	index := t & *ur.sq.kRingMask
	return submitSlot{
		index: index,
		tail:  t,
		sqe:   &ur.sq.sqes[index],
		keep:  &ur.submit.keepAlive[index],
	}, nil
}

// abortSubmitSlot drops an unpublished reservation and unlocks submit state.
func (ur *ioUring) abortSubmitSlot(slot submitSlot) {
	if slot.keep.active {
		slot.keep.clear(ur)
	}
	ur.unlockSubmitState()
}

// publishSubmitSlot makes the SQE visible to the kernel and unlocks submit
// state.
func (ur *ioUring) publishSubmitSlot(slot submitSlot, sqeCtx SQEContext) {
	slot.sqe.userData = sqeCtx.Raw()
	if ur.params.flags&IORING_SETUP_NO_SQARRAY == 0 {
		ur.sq.array[slot.index] = slot.index
	}

	// Release barrier makes SQE writes visible before the tail update.
	dwcas.BarrierRelease()
	atomic.StoreUint32(ur.sq.kTail, slot.tail+1)

	ur.unlockSubmitState()
}
