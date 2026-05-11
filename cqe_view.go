// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// CQEView provides a view into a completion queue entry.
// It exposes kernel completion facts directly and lets caller-side runtime
// code decide how to route or interpret them. When available,
// it also exposes the submission context that produced those facts.
//
// # Property Patterns
//
// | FullSQE() | Extended() | Mode     | Available Data                                |
// |-----------|------------|----------|-----------------------------------------------|
// | false     | false      | Direct   | Op, SQE flags, BufGroup, FD, Res, CQE flags   |
// | true      | false      | Indirect | + full ioUringSqe copy                        |
// | true      | true       | Extended | + borrowed `ExtSQE` escape hatch              |
//
// # Usage
//
//	n, err := ring.Wait(cqes)
//	for i := range n {
//	    cqe := cqes[i]
//	    // Observe the kernel facts first.
//	    if err := cqe.Err(); err != nil {
//	        return fmt.Errorf("completion failed: op=%d fd=%d: %w", cqe.Op(), cqe.FD(), err)
//	    }
//	    fmt.Printf("completed op=%d on fd=%d with res=%d\n", cqe.Op(), cqe.FD(), cqe.Res)
//	    if cqe.HasMore() {
//	        // Caller-side runtime code decides whether to keep routing this live stream.
//	    }
//	    if cqe.FullSQE() {
//	        // Indirect and Extended modes also expose the submitted SQE.
//	        fmt.Printf("submitted opcode=%d\n", cqe.SQE().Opcode())
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

// ExtSQE returns the borrowed ExtSQE backing Extended mode contexts.
// Caller should check Extended() first.
//
//go:nosplit
func (c *CQEView) ExtSQE() *ExtSQE {
	return c.ctx.ExtSQE()
}

// SQE returns a view of the submitted SQE when the context retains one.
// Caller should check FullSQE() first; Direct mode returns an invalid view
// because it keeps only compact completion-context facts.
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
// Use this for advanced mode-specific inspection beyond the CQEView helpers.
//
//go:nosplit
func (c *CQEView) Context() SQEContext {
	return c.ctx
}

// BufGroup returns the observed submission buffer group index.
// It is non-zero only when buffer selection was part of the submission.
//
//go:nosplit
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
//
//go:nosplit
func (c *CQEView) BufID() uint16 {
	return cqeBufID(c.Flags)
}

// HasMore reports whether more completions are coming (multishot).
//
//go:nosplit
func (c *CQEView) HasMore() bool {
	return cqeHasMore(c.Flags)
}

// HasBuffer reports whether a buffer ID is available in the flags.
//
//go:nosplit
func (c *CQEView) HasBuffer() bool {
	return cqeHasBuffer(c.Flags)
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
	return cqeIsNotification(c.Flags)
}

// HasBufferMore reports whether the buffer was partially consumed (incremental mode).
// When set, the same buffer ID remains valid
// for additional data.
//
//go:nosplit
func (c *CQEView) HasBufferMore() bool {
	return cqeHasBufferMore(c.Flags)
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

// BundleBuffers returns the logical range of buffer IDs consumed.
// The returned startID is the first buffer ID; count is the number of buffers.
// The range [startID, startID+count) is logical and may wrap around the ring.
// Callers must apply (id & ringMask) to obtain physical buffer IDs, or use
// BundleIterator which handles wrap-around automatically.
//
//go:nosplit
func (c *CQEView) BundleBuffers(bufferSize int) (startID uint16, count int) {
	startID = c.BundleStartID()
	count = c.BundleCount(bufferSize)
	return startID, count
}
