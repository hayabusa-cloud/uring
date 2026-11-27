// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

import "code.hybscloud.com/iofd"

// DirectCQE is a compact copied CQE for Direct mode operations.
// It stores the completion result, CQE flags, and decoded context fields.
//
// Use [Uring.WaitDirect] when every incoming CQE carries a [PackDirect] context.
//
// Layout: 16 bytes on supported platforms.
type DirectCQE struct {
	Res      int32   // Raw CQE result
	Flags    uint32  // CQE flags (IORING_CQE_F_*)
	Op       uint8   // IORING_OP_* opcode
	SQEFlags uint8   // SQE flags (IOSQE_*)
	BufGroup uint16  // Buffer group index
	FD       iofd.FD // Submitted descriptor or registered-file index
}

// IsSuccess reports whether Res is nonnegative. Use it on a matched operation result.
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

// IsNotification reports whether IORING_CQE_F_NOTIF is set.
//
//go:nosplit
func (c *DirectCQE) IsNotification() bool {
	return cqeIsNotification(c.Flags)
}

// WaitDirect copies completion events into DirectCQE values, decoding the
// packed context into the Op, SQEFlags, BufGroup, and FD fields.
// Use it when every incoming CQE carries a [PackDirect] context.
// For rings receiving message IDs or Linux resource tags, use [Uring.Wait].
//
// On single-issuer rings it is not safe for concurrent use with submit, Wait,
// WaitDirect, WaitExtended, Stop, or ResizeRings; caller must serialize those
// operations.
// WaitDirect drives pending submissions on Linux. After a successful enter,
// a zero-length destination returns (0, nil) and consumes no CQEs.
// With a nonempty destination, polling and empty-CQ errors follow [Uring.Wait].
func (ur *Uring) WaitDirect(cqes []DirectCQE) (int, error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatchDirect(cqes)
}
