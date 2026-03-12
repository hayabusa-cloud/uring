// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// CQEView provides a view into a completion queue entry.
// It exposes the completion result and flags as direct fields for
// zero-overhead access and provides methods to access the submission context.
//
// # Property Patterns
//
// | FullSQE() | Extended() | Mode     | Available Data                                |
// |-----------|------------|----------|-----------------------------------------------|
// | false     | false      | Direct   | Op, SQE flags, BufGroup, FD, Res, CQE flags   |
// | true      | false      | Indirect | + full ioUringSqe                             |
// | true      | true       | Extended | + UserData typed overlay (Handler, refs, vals) |
//
// # Usage
//
//	n, err := ring.Wait(cqes)
//	for i := 0; i < n; i++ {
//	    cqe := cqes[i]
//	    if cqe.Extended() {
//	        ext := cqe.ExtSQE()
//	        ctx := ViewCtx1[*Conn](ext).Vals1()
//	        ctx.Fn(ring, cqe)
//	    } else if cqe.FullSQE() {
//	        sqe := cqe.SQE()
//	        // dispatch by sqe fields
//	    } else {
//	        // dispatch by Op/FD
//	    }
//	}
type CQEView struct {
	Res   int32  // Completion result (directly accessible)
	Flags uint32 // CQE flags (directly accessible)
	ctx   SQEContext
}

// FullSQE reports whether full SQE information is available.
// Returns true for Indirect and Extended modes.
//
//go:nosplit
func (c *CQEView) FullSQE() bool {
	mode := c.ctx.Mode()
	return mode == CtxModeIndirect || mode == CtxModeExtended
}

// Extended reports whether extended user data is available.
// Returns true only for Extended mode.
//
//go:nosplit
func (c *CQEView) Extended() bool {
	return c.ctx.Mode() == CtxModeExtended
}

// Op returns the IORING_OP_* opcode.
// Always available (extracted from Direct mode context or from SQE in other modes).
//
//go:nosplit
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
//
//go:nosplit
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

// ExtSQE returns the ExtSQE for Extended mode contexts.
// Caller must ensure context is in Extended mode.
//
//go:nosplit
func (c *CQEView) ExtSQE() *ExtSQE {
	return c.ctx.ExtSQE()
}

// SQE returns an SQEView for inspecting the full SQE fields.
// Caller should check FullSQE() first; returns invalid view for Direct mode.
//
//go:nosplit
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

// Context returns the underlying SQEContext.
// Use this for advanced operations or mode inspection.
//
//go:nosplit
func (c *CQEView) Context() SQEContext {
	return c.ctx
}

// BufGroup returns the buffer group index.
// Only meaningful for Direct mode or when IOSQE_BUFFER_SELECT was used.
//
//go:nosplit
func (c *CQEView) BufGroup() uint16 {
	if c.ctx.Mode() == CtxModeDirect {
		return c.ctx.BufGroup()
	}
	// For Indirect/Extended, extract from SQE flags if buffer select was used
	return 0
}

// BufID returns the buffer ID from CQE flags.
// Only valid when IORING_CQE_F_BUFFER flag is set.
//
//go:nosplit
func (c *CQEView) BufID() uint16 {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT)
}

// HasMore reports whether more completions are coming (multishot).
//
//go:nosplit
func (c *CQEView) HasMore() bool {
	return c.Flags&IORING_CQE_F_MORE != 0
}

// HasBuffer reports whether a buffer ID is available in the flags.
//
//go:nosplit
func (c *CQEView) HasBuffer() bool {
	return c.Flags&IORING_CQE_F_BUFFER != 0
}

// SocketNonEmpty reports whether the socket has more data available.
// This is set when a short read/recv occurred but more data remains.
//
//go:nosplit
func (c *CQEView) SocketNonEmpty() bool {
	return c.Flags&IORING_CQE_F_SOCK_NONEMPTY != 0
}

// IsNotification reports whether this is a zero-copy notification CQE.
// Zero-copy sends generate two CQEs: one for completion, one for notification
// when the buffer can be reused.
//
//go:nosplit
func (c *CQEView) IsNotification() bool {
	return c.Flags&IORING_CQE_F_NOTIF != 0
}

// HasBufferMore reports whether the buffer was partially consumed (incremental mode).
// Available in kernel 6.12+. When set, the same buffer ID remains valid
// for additional data.
//
//go:nosplit
func (c *CQEView) HasBufferMore() bool {
	return c.Flags&IORING_CQE_F_BUF_MORE != 0
}

// BundleStartID returns the starting buffer ID for a bundle operation.
// For bundle receives, buffers are consumed contiguously from this ID.
// Only valid when IORING_CQE_F_BUFFER flag is set.
//
//go:nosplit
func (c *CQEView) BundleStartID() uint16 {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT)
}

// BundleCount returns the number of buffers consumed in a bundle operation.
// For receive bundles, this is derived from the result (bytes received)
// divided by the buffer size. For accurate count, use with known buffer sizes.
//
//go:nosplit
func (c *CQEView) BundleCount(bufferSize int) int {
	if c.Res <= 0 || bufferSize <= 0 {
		return 0
	}
	return (int(c.Res) + bufferSize - 1) / bufferSize
}

// BundleBuffers returns the range of buffer IDs consumed [startID, startID+count).
// The mask parameter should be (bufferRingSize - 1) for proper wrap-around.
// Returns startID and the count of buffers consumed.
//
//go:nosplit
func (c *CQEView) BundleBuffers(bufferSize int) (startID uint16, count int) {
	startID = c.BundleStartID()
	count = c.BundleCount(bufferSize)
	return startID, count
}
