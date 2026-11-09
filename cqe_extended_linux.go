// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// ExtCQE is a compact copied CQE for Extended mode operations.
// It stores the completion result, CQE flags, and borrowed ExtSQE pointer.
//
// Use [Uring.WaitExtended] when every submitted operation uses [PackExtended].
//
// Layout: 16 bytes on supported 64-bit platforms.
type ExtCQE struct {
	Res   int32   // Completion result (bytes transferred or negative errno)
	Flags uint32  // CQE flags (IORING_CQE_F_*)
	Ext   *ExtSQE // Borrowed ExtSQE with full context
}

// IsSuccess reports whether the operation completed successfully.
//
//go:nosplit
func (c *ExtCQE) IsSuccess() bool {
	return cqeIsSuccess(c.Res)
}

// HasMore reports IORING_CQE_F_MORE: the request has further completions,
// including multishot results or a zero-copy notification.
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

// WaitExtended copies completion events into ExtCQE values containing the
// result, CQE flags, and borrowed ExtSQE pointer.
// Use it when every submitted operation uses Extended mode ([PackExtended]).
//
// On single-issuer rings it is not safe for concurrent use with submit, Wait,
// WaitDirect, WaitExtended, Stop, or ResizeRings; caller must serialize those
// operations.
// On IOPOLL rings WaitExtended also performs the nonblocking poll enter needed
// to make completions visible.
// Caller-side completion code must keep completion referents reachable until
// CQE reap and serialize retirement.
// Return Ext to its owning pool only after all request completions, including
// zero-copy notifications, and borrowed access finish; see [Uring.PutExtSQE].
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

	claim, available, err := ur.claimCQ(len(cqes))
	if err != nil {
		return 0, err
	}

	h, mask := uint32(claim), *ur.cq.kRingMask
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
	ur.publishCQ(claim, available)
	return n, nil
}
