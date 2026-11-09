// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "code.hybscloud.com/iofd"

// CQEView contains the completion result, flags, and submission context copied
// by Wait. Direct-mode facts are inline; Indirect and Extended contexts still
// reference shared targets. Copying a CQEView does not copy or root those targets.
//
// Caller must keep targets live and serialize access. Copy needed facts before
// target mutation, reuse, or release; never dereference a released target.
// Caller code owns payload lifetimes, completion routing, and retirement.
type CQEView struct {
	Res   int32
	Flags uint32
	ctx   SQEContext
}

// FullSQE reports whether the context retains a 64-byte base SQE.
// It is true for Indirect and Extended modes and does not include SQE128
// extensions. The retained fields remain borrowed from the context target.
func (c *CQEView) FullSQE() bool {
	mode := c.ctx.Mode()
	return mode == CtxModeIndirect || mode == CtxModeExtended
}

// Extended reports whether extended user data is available.
// Returns true only for Extended mode.
func (c *CQEView) Extended() bool {
	return c.ctx.Mode() == CtxModeExtended
}

// Op returns the IORING_OP_* opcode.
// Always available (extracted from Direct mode context or from SQE in other modes).
func (c *CQEView) Op() uint8 {
	switch c.ctx.Mode() {
	case CtxModeIndirect:
		return c.ctx.IndirectSQE().opcode
	case CtxModeExtended:
		return c.ctx.ExtSQE().SQE.opcode
	default:
		return c.ctx.Op()
	}
}

// FD returns the file descriptor associated with the operation.
// Always available.
func (c *CQEView) FD() iofd.FD {
	switch c.ctx.Mode() {
	case CtxModeIndirect:
		return iofd.FD(c.ctx.IndirectSQE().fd)
	case CtxModeExtended:
		return iofd.FD(c.ctx.ExtSQE().SQE.fd)
	default:
		return iofd.FD(c.ctx.FD())
	}
}

// ExtSQE returns the borrowed target for an Extended context.
// Caller must check Extended() and keep the target live; copy needed facts
// before target mutation, reuse, or release.
func (c *CQEView) ExtSQE() *ExtSQE {
	return c.ctx.ExtSQE()
}

// SQE borrows the retained 64-byte base SQE in Indirect and Extended modes.
// Direct mode returns an invalid view. Copy needed fields before target
// mutation, reuse, or release; copying the view preserves the same alias.
func (c *CQEView) SQE() SQEView {
	switch c.ctx.Mode() {
	case CtxModeIndirect:
		return ViewSQE(c.ctx.IndirectSQE())
	case CtxModeExtended:
		return ViewExtSQE(c.ctx.ExtSQE())
	default:
		return SQEView{}
	}
}

// Context returns the packed SQEContext.
// Pointer modes retain the same borrowed target; this does not copy its fields.
func (c *CQEView) Context() SQEContext {
	return c.ctx
}

// BufGroup returns the submission buffer group index.
// Direct mode reads the packed field; pointer modes return zero unless
// IOSQE_BUFFER_SELECT is set. A zero group ID can also be valid.
func (c *CQEView) BufGroup() uint16 {
	switch c.ctx.Mode() {
	case CtxModeDirect:
		return c.ctx.BufGroup()
	case CtxModeIndirect:
		sqe := c.ctx.IndirectSQE()
		if sqe.flags&IOSQE_BUFFER_SELECT != 0 {
			return sqe.bufIndex
		}
	case CtxModeExtended:
		ext := c.ctx.ExtSQE()
		if ext.SQE.flags&IOSQE_BUFFER_SELECT != 0 {
			return ext.SQE.bufIndex
		}
	}
	return 0
}

// BufID returns the buffer ID from CQE flags.
// Only valid when IORING_CQE_F_BUFFER flag is set.
func (c *CQEView) BufID() uint16 {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT)
}

// HasMore reports IORING_CQE_F_MORE: the request has further completions,
// including multishot results or a zero-copy notification.
func (c *CQEView) HasMore() bool {
	return c.Flags&IORING_CQE_F_MORE != 0
}

// HasBuffer reports whether a buffer ID is available in the flags.
func (c *CQEView) HasBuffer() bool {
	return c.Flags&IORING_CQE_F_BUFFER != 0
}

// SocketNonEmpty reports whether the kernel observed unread socket data
// after this receive. It is a completion-time hint, not a current readiness check.
func (c *CQEView) SocketNonEmpty() bool {
	return c.Flags&IORING_CQE_F_SOCK_NONEMPTY != 0
}

// IsNotification reports whether this is a zero-copy notification CQE.
// Notification CQEs mark the buffer-reuse frontier. A terminal data CQE without
// MORE closes the data side; if no notification was observed, it is the
// no-notification fallback.
func (c *CQEView) IsNotification() bool {
	return c.Flags&IORING_CQE_F_NOTIF != 0
}

// HasBufferMore reports whether the buffer was partially consumed (incremental mode).
// When set, the same buffer ID remains valid
// for additional data.
func (c *CQEView) HasBufferMore() bool {
	return c.Flags&IORING_CQE_F_BUF_MORE != 0
}

// BundleStartID returns the starting buffer ID for a bundle operation.
// For bundle receives, buffers are consumed contiguously from this ID.
// Only valid when IORING_CQE_F_BUFFER flag is set.
func (c *CQEView) BundleStartID() uint16 {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT)
}

// BundleCount returns the number of buffers consumed in a bundle operation.
// For receive bundles, this is derived from the result (bytes received)
// divided by the buffer size. For accurate count, use with known buffer sizes.
func (c *CQEView) BundleCount(bufferSize int) int {
	if c.Res <= 0 || bufferSize <= 0 {
		return 0
	}
	return (int(c.Res) + bufferSize - 1) / bufferSize
}

// BundleBuffers returns the logical range of buffer IDs consumed.
// The returned startID is the first buffer ID; count is the number of buffers.
// The range [startID, startID+count) is logical and may wrap around the ring.
// Callers must apply (id & ringMask) to obtain physical buffer IDs, or use
// BundleIterator which handles wrap-around automatically.
func (c *CQEView) BundleBuffers(bufferSize int) (uint16, int) {
	startID := c.BundleStartID()
	count := c.BundleCount(bufferSize)
	return startID, count
}
