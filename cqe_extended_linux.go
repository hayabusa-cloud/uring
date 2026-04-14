// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"

	"code.hybscloud.com/dwcas"
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/spin"
)

// ExtCQE is a zero-overhead CQE for Extended mode operations.
// It provides direct access to the ExtSQE pointer without mode checking.
//
// Use WaitExtended when your application exclusively uses Extended mode
// (PackExtended) for all submissions. This avoids the 3-way mode check
// that the generic Wait/CQEView path requires per-CQE.
//
// Layout: 16 bytes (fits in 1/4 cache line)
type ExtCQE struct {
	Res   int32   // Completion result (bytes transferred or negative errno)
	Flags uint32  // CQE flags (IORING_CQE_F_*)
	Ext   *ExtSQE // Pointer to ExtSQE with full context
}

// IsSuccess reports whether the operation completed successfully.
//
//go:nosplit
func (c *ExtCQE) IsSuccess() bool {
	return cqeIsSuccess(c.Res)
}

// HasMore reports whether more completions are coming (multishot).
//
//go:nosplit
func (c *ExtCQE) HasMore() bool {
	return cqeHasMore(c.Flags)
}

// HasBuffer reports whether a buffer ID is available.
//
//go:nosplit
func (c *ExtCQE) HasBuffer() bool {
	return cqeHasBuffer(c.Flags)
}

// BufID returns the buffer ID from CQE flags.
// Only valid when HasBuffer() returns true.
//
//go:nosplit
func (c *ExtCQE) BufID() uint16 {
	return cqeBufID(c.Flags)
}

// IsNotification reports whether this is a zero-copy notification CQE.
//
//go:nosplit
func (c *ExtCQE) IsNotification() bool {
	return cqeIsNotification(c.Flags)
}

// HasBufferMore reports whether the buffer was partially consumed.
//
//go:nosplit
func (c *ExtCQE) HasBufferMore() bool {
	return cqeHasBufferMore(c.Flags)
}

// Op returns the IORING_OP_* opcode from the stored SQE.
//
//go:nosplit
func (c *ExtCQE) Op() uint8 {
	return c.Ext.SQE.opcode
}

// FD returns the file descriptor from the stored SQE.
//
//go:nosplit
func (c *ExtCQE) FD() iofd.FD {
	return iofd.FD(c.Ext.SQE.fd)
}

// WaitExtended retrieves completion events using Extended mode fast-path.
// This method skips mode detection since all CQEs are assumed to be
// from Extended mode submissions (PackExtended).
//
// For applications using only Extended mode, this skips the mode dispatch
// that Wait([]CQEView) performs per CQE.
//
// On single-issuer rings it is not safe for concurrent use with submit, Stop,
// or ResizeRings; caller must serialize those operations.
// Returns the number of CQEs retrieved, ErrCQOverflow when the ring enters CQ
// overflow and no CQEs are immediately claimable, or iox.ErrWouldBlock if none
// are available.
func (ur *Uring) WaitExtended(cqes []ExtCQE) (int, error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatchExtended(cqes)
}

// waitBatchExtended is the internal Extended mode batch retrieval.
func (ur *ioUring) waitBatchExtended(cqes []ExtCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	ur.lockSubmitState()
	if ur.closed.Load() || ur.cq.kHead == nil || ur.cq.kTail == nil || ur.cq.kRingMask == nil || len(ur.cq.cqes) == 0 {
		ur.unlockSubmitState()
		return 0, ErrClosed
	}

	sw := spin.Wait{}
	for {
		h := atomic.LoadUint32(ur.cq.kHead)
		t := atomic.LoadUint32(ur.cq.kTail)
		if h == t {
			err := ur.cqEmptyErr()
			ur.unlockSubmitState()
			return 0, err
		}

		// Calculate batch size
		available := t - h
		want := uint32(len(cqes))
		if available > want {
			available = want
		}

		// Acquire barrier before reading CQE data
		dwcas.BarrierAcquire()

		// Snapshot before publishing the head advance so copied CQEs cannot race
		// kernel slot reuse after wrap.
		mask := *ur.cq.kRingMask
		n := int(available)
		for i := range n {
			e := ur.cq.cqeAt((h + uint32(i)) & mask)
			ctx := SQEContextFromRaw(e.userData)

			var ext *ExtSQE
			if ctx.Mode() == CtxModeExtended {
				ext = ctx.ExtSQE()
			}

			cqes[i] = ExtCQE{
				Res:   e.res,
				Flags: e.flags,
				Ext:   ext,
			}
		}
		if !atomic.CompareAndSwapUint32(ur.cq.kHead, h, h+available) {
			sw.Once()
			continue
		}
		ur.unlockSubmitState()
		return n, nil
	}
}
