// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"

	"code.hybscloud.com/dwcas"
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/spin"
)

// DirectCQE is a zero-overhead CQE for Direct mode operations.
// It contains the completion result and unpacked context fields
// without any mode checking or pointer indirection.
//
// Use WaitDirect when your application exclusively uses Direct mode
// (PackDirect) for all submissions. This avoids the 3-way mode check
// that the generic Wait/CQEView path requires per-CQE.
//
// Layout: 24 bytes (fits in 1/3 cache line, no padding needed)
type DirectCQE struct {
	Res      int32  // Completion result (bytes transferred or negative errno)
	Flags    uint32 // CQE flags (IORING_CQE_F_*)
	Op       uint8  // IORING_OP_* opcode
	SQEFlags uint8  // SQE flags (IOSQE_*)
	BufGroup uint16 // Buffer group index
	FD       iofd.FD
}

// IsSuccess reports whether the operation completed successfully.
//
//go:nosplit
func (c *DirectCQE) IsSuccess() bool {
	return cqeIsSuccess(c.Res)
}

// HasMore reports whether more completions are coming (multishot).
//
//go:nosplit
func (c *DirectCQE) HasMore() bool {
	return cqeHasMore(c.Flags)
}

// HasBuffer reports whether a buffer ID is available.
//
//go:nosplit
func (c *DirectCQE) HasBuffer() bool {
	return cqeHasBuffer(c.Flags)
}

// BufID returns the buffer ID from CQE flags.
// Only valid when HasBuffer() returns true.
//
//go:nosplit
func (c *DirectCQE) BufID() uint16 {
	return cqeBufID(c.Flags)
}

// IsNotification reports whether this is a zero-copy notification CQE.
//
//go:nosplit
func (c *DirectCQE) IsNotification() bool {
	return cqeIsNotification(c.Flags)
}

// WaitDirect retrieves completion events using Direct mode fast-path.
// This method skips mode detection since all CQEs are assumed to be
// from Direct mode submissions (PackDirect).
//
// For applications using only Direct mode, this skips the mode dispatch
// that Wait([]CQEView) performs per CQE.
//
// Returns the number of CQEs retrieved, or iox.ErrWouldBlock if none available.
func (ur *Uring) WaitDirect(cqes []DirectCQE) (int, error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatchDirect(cqes)
}

// waitBatchDirect is the internal Direct mode batch retrieval.
func (ur *ioUring) waitBatchDirect(cqes []DirectCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	sw := spin.Wait{}
	for {
		h := atomic.LoadUint32(ur.cq.kHead)
		t := atomic.LoadUint32(ur.cq.kTail)
		if h == t {
			return 0, iox.ErrWouldBlock
		}

		// Calculate batch size
		available := t - h
		want := uint32(len(cqes))
		if available > want {
			available = want
		}

		// Acquire barrier before reading CQE data
		dwcas.BarrierAcquire()

		// Claim the batch with single CAS
		if !atomic.CompareAndSwapUint32(ur.cq.kHead, h, h+available) {
			sw.Once()
			continue
		}

		// Unpack Direct mode context directly - no mode checking
		mask := *ur.cq.kRingMask
		n := int(available)
		for i := range n {
			e := &ur.cq.cqes[(h+uint32(i))&mask]
			ctx := SQEContextFromRaw(e.userData)

			cqes[i] = DirectCQE{
				Res:      e.res,
				Flags:    e.flags,
				Op:       ctx.Op(),
				SQEFlags: ctx.Flags(),
				BufGroup: ctx.BufGroup(),
				FD:       iofd.FD(ctx.FD()),
			}
		}
		return n, nil
	}
}
