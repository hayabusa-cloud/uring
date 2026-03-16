// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "code.hybscloud.com/iofd"

// CQEView provides a compatibility view into a completion queue entry.
type CQEView struct {
	Res   int32
	Flags uint32
	ctx   SQEContext
}

func (c *CQEView) FullSQE() bool {
	mode := c.ctx.Mode()
	return mode == CtxModeIndirect || mode == CtxModeExtended
}

func (c *CQEView) Extended() bool {
	return c.ctx.Mode() == CtxModeExtended
}

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

func (c *CQEView) ExtSQE() *ExtSQE {
	return c.ctx.ExtSQE()
}

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

func (c *CQEView) Context() SQEContext {
	return c.ctx
}

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

func (c *CQEView) BufID() uint16 {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT)
}

func (c *CQEView) HasMore() bool {
	return c.Flags&IORING_CQE_F_MORE != 0
}

func (c *CQEView) HasBuffer() bool {
	return c.Flags&IORING_CQE_F_BUFFER != 0
}

func (c *CQEView) SocketNonEmpty() bool {
	return c.Flags&IORING_CQE_F_SOCK_NONEMPTY != 0
}

func (c *CQEView) IsNotification() bool {
	return c.Flags&IORING_CQE_F_NOTIF != 0
}

func (c *CQEView) HasBufferMore() bool {
	return c.Flags&IORING_CQE_F_BUF_MORE != 0
}

func (c *CQEView) BundleStartID() uint16 {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT)
}

func (c *CQEView) BundleCount(bufferSize int) int {
	if c.Res <= 0 || bufferSize <= 0 {
		return 0
	}
	return (int(c.Res) + bufferSize - 1) / bufferSize
}

func (c *CQEView) BundleBuffers(bufferSize int) (uint16, int) {
	startID := c.BundleStartID()
	count := c.BundleCount(bufferSize)
	return startID, count
}
