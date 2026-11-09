// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// DirectCQE is a compact copied CQE for Direct mode operations.
// It stores the completion result, CQE flags, and decoded context fields.
//
// Use [Uring.WaitDirect] when every submitted operation uses [PackDirect].
//
// Layout: 16 bytes on supported platforms.
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

// HasMore reports IORING_CQE_F_MORE: the request has further completions,
// including multishot results or a zero-copy notification.
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

// WaitDirect copies completion events into DirectCQE values, decoding the
// packed context into the Op, SQEFlags, BufGroup, and FD fields.
// Use it when every submitted operation uses Direct mode ([PackDirect]).
//
// On single-issuer rings it is not safe for concurrent use with submit, Wait,
// WaitDirect, WaitExtended, Stop, or ResizeRings; caller must serialize those
// operations.
// On IOPOLL rings WaitDirect also performs the nonblocking poll enter needed
// to make completions visible.
// Returns the number of CQEs retrieved, ErrCQOverflow when the ring enters CQ
// overflow and no CQEs are immediately claimable, or iox.ErrWouldBlock if none
// are available.
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

	claim, available, err := ur.claimCQ(len(cqes))
	if err != nil {
		return 0, err
	}

	h, mask := uint32(claim), *ur.cq.kRingMask
	n := int(available)
	for i := range n {
		e := ur.cq.cqeAt((h + uint32(i)) & mask)
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
	ur.publishCQ(claim, available)
	return n, nil
}
