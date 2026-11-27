// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

import "code.hybscloud.com/iofd"

// CQEView contains the result, flags, and user_data copied by [Uring.Wait].
// For local requests, user_data is the submission context. Direct fields are
// inline. Indirect and Extended contexts borrow their targets without rooting them.
// Match incoming message IDs and Linux resource tags with Context().Raw()
// before using request accessors. [Uring.MsgRing] carries its signed value in Res.
//
// Caller must keep targets live and serialize access. Copy needed facts before
// target mutation, reuse, or release; never dereference a released target.
// Caller code owns payload lifetimes, completion routing, and retirement.
//
// # Property Patterns
//
//   - Direct: FullSQE reports false and Extended reports false. Available
//     data: Op, SQE flags, BufGroup, FD, Res, and CQE flags.
//   - Indirect: FullSQE reports true and Extended reports false. Available
//     data: Direct facts plus a borrowed 64-byte base SQE.
//   - Extended: FullSQE reports true and Extended reports true. Available
//     data: Indirect facts plus the borrowed ExtSQE escape hatch.
//
// # Usage
//
// This ring submits local reads and zero-copy sends. Inspect every returned
// CQE, including notifications and failed requests.
//
//	n, err := ring.Wait(cqes)
//	for i := range n {
//	    cqe := cqes[i]
//	    if cqe.IsNotification() {
//	        fmt.Printf("send notification on fd=%d\n", cqe.FD())
//	    } else if err := cqe.Err(); err != nil {
//	        fmt.Printf("completion failed: op=%d fd=%d: %v\n", cqe.Op(), cqe.FD(), err)
//	    } else {
//	        fmt.Printf("completed op=%d on fd=%d with res=%d\n", cqe.Op(), cqe.FD(), cqe.Res)
//	    }
//	    if cqe.HasMore() {
//	        // Caller-side runtime code decides whether to keep routing this live stream.
//	    }
//	    if cqe.FullSQE() {
//	        // Indirect and Extended modes also expose the submitted SQE.
//	        fmt.Printf("submitted opcode=%d\n", cqe.SQE().Opcode())
//	    }
//	}
//	if err != nil {
//	    return err
//	}
type CQEView struct {
	Res   int32  // Completion result (directly accessible)
	Flags uint32 // CQE flags (directly accessible)
	ctx   SQEContext
}

// view copies the base CQE and leaves user_data opaque until the caller matches it.
func (e *ioUringCqe) view() CQEView {
	return CQEView{Res: e.res, Flags: e.flags, ctx: e.Context()}
}

// FullSQE reports whether the context retains a 64-byte base SQE.
// It is true for Indirect and Extended modes and does not include SQE128
// extensions. The retained fields remain borrowed from the context target.
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

// Op returns the IORING_OP_* opcode from a local request's live context.
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

// FD returns the file descriptor or registered-file index from a local request's live context.
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

// ExtSQE returns the borrowed target for an Extended context.
// Caller must check Extended() and keep the target live; copy needed facts
// before target mutation, reuse, or release.
//
//go:nosplit
func (c *CQEView) ExtSQE() *ExtSQE {
	return c.ctx.ExtSQE()
}

// SQE borrows the retained 64-byte base SQE in Indirect and Extended modes.
// Direct mode returns an invalid view. Copy needed fields before target
// mutation, reuse, or release; copying the view preserves the same alias.
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

// Context returns the CQE's user_data as an SQEContext. For a local request,
// pointer modes retain the same borrowed target. Context().Raw() also preserves
// an incoming message ID or the tag of a retired Linux resource registration.
//
//go:nosplit
func (c *CQEView) Context() SQEContext {
	return c.ctx
}

// BufGroup returns the submission buffer group index.
// Direct mode reads the packed field; pointer modes return zero unless
// IOSQE_BUFFER_SELECT is set. A zero group ID can also be valid.
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

// HasMore reports IORING_CQE_F_MORE: the request has further completions,
// including multishot results or a zero-copy notification.
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

// SocketNonEmpty reports whether the kernel observed unread socket data
// after this receive. It is a completion-time hint, not a current readiness check.
//
//go:nosplit
func (c *CQEView) SocketNonEmpty() bool {
	return c.Flags&IORING_CQE_F_SOCK_NONEMPTY != 0
}

// IsNotification reports whether IORING_CQE_F_NOTIF is set.
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
// Subsequent buffers follow publication order, not numeric ID order.
// Only valid when IORING_CQE_F_BUFFER flag is set.
//
//go:nosplit
func (c *CQEView) BundleStartID() uint16 {
	return c.BufID()
}

// BundleCount counts buffers containing payload bytes from Res, rounding up.
// bufferSize must be the uniform published buffer size. It returns zero when
// Res or bufferSize is non-positive.
//
//go:nosplit
func (c *CQEView) BundleCount(bufferSize int) int {
	if c.Res <= 0 || bufferSize <= 0 {
		return 0
	}
	return 1 + (int(c.Res)-1)/bufferSize
}

// BundleBuffers returns the first ID and the slot count for an ordinary receive
// bundle with uniform bufferSize. At EOF or on error, F_BUFFER can return one
// slot with zero payload. F_BUF_MORE requires incremental buffer accounting.
// The remaining IDs follow publication order. See [Uring.BundleIterator].
//
//go:nosplit
func (c *CQEView) BundleBuffers(bufferSize int) (startID uint16, count int) {
	startID = c.BundleStartID()
	switch {
	case bufferSize <= 0 || c.HasBufferMore():
		return startID, 0
	case c.Res <= 0 && c.HasBuffer():
		return startID, 1
	default:
		return startID, c.BundleCount(bufferSize)
	}
}

// Wait flushes pending submissions and copies the result, flags and raw user_data
// of each observed CQE. Match incoming message IDs and Linux resource tags before
// using request accessors. Serialize waits, submission, resize and teardown on a single-issuer ring.
// On Linux, Wait also drives deferred task work. After a successful enter, a
// zero-length destination returns (0, nil) and consumes no CQEs.
// With a nonempty destination, Wait polls an empty Linux IOPOLL ring as needed.
// If no CQEs are available, Wait returns iox.ErrWouldBlock or, on Linux,
// ErrCQOverflow if overflow is observed. Inspect every CQE in the returned count.
//
// Here messageID is reserved for incoming messages. Every other CQE belongs to
// a one-shot read with a context borrowed from ring.ExtSQE and a sequence in Val1.
//
//	var cqes [64]CQEView
//	n, err := ring.Wait(cqes[:])
//	for i := range n {
//	    cqe := &cqes[i]
//	    if cqe.Context().Raw() == messageID {
//	        handleMessage(cqe.Res)
//	        continue
//	    }
//	    ext := cqe.ExtSQE()
//	    seq := ViewCtx(ext).Vals1().Val1
//	    handleRead(seq, cqe.Res, cqe.Err())
//	    ring.PutExtSQE(ext)
//	}
//	if err != nil {
//	    return err
//	}
func (ur *Uring) Wait(cqes []CQEView) (n int, err error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatch(cqes)
}
