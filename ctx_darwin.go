// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"unsafe"

	"code.hybscloud.com/iofd"
)

// SQEContext preserves the Linux bit-level context encoding on Darwin so
// completion paths can round-trip user_data consistently. Darwin remains a
// compatibility port and does not promise full Linux io_uring semantics.
type SQEContext uint64

// Context mode constants (bits 62-63).
const (
	CtxModeDirect   SQEContext = 0 << 62
	CtxModeIndirect SQEContext = 1 << 62
	CtxModeExtended SQEContext = 2 << 62
)

const (
	ctxModeMask    SQEContext = 3 << 62
	ctxPtrMask     SQEContext = ^ctxModeMask
	ctxOpShift                = 0
	ctxOpMask      SQEContext = 0xFF
	ctxFlagsShift             = 8
	ctxFlagsMask   SQEContext = 0xFF << ctxFlagsShift
	ctxBufGrpShift            = 16
	ctxBufGrpMask  SQEContext = 0xFFFF << ctxBufGrpShift
	ctxFDShift                = 32
	ctxFDMask      SQEContext = 0x3FFFFFFF << ctxFDShift
	ctxFDSignBit   SQEContext = 0x20000000 << ctxFDShift
	ctxFDSignExt   SQEContext = 0xC0000000 << ctxFDShift
)

// Mode returns the context mode (Direct, Indirect, Extended, or Reserved).
//
//go:nosplit
func (c SQEContext) Mode() SQEContext { return c & ctxModeMask }

// IsDirect reports whether this is a Direct mode context (inline data).
//
//go:nosplit
func (c SQEContext) IsDirect() bool { return c.Mode() == CtxModeDirect }

// IsIndirect reports whether this is an Indirect mode context (pointer to 64B).
//
//go:nosplit
func (c SQEContext) IsIndirect() bool { return c.Mode() == CtxModeIndirect }

// IsExtended reports whether this is an Extended mode context (pointer to 128B).
//
//go:nosplit
func (c SQEContext) IsExtended() bool { return c.Mode() == CtxModeExtended }

// PackDirect packs direct-mode submission context.
func PackDirect(op, flags uint8, bufGroup uint16, fd int32) SQEContext {
	fdBits := SQEContext(uint32(fd)&0x3FFFFFFF) << ctxFDShift
	return SQEContext(op) |
		SQEContext(flags)<<ctxFlagsShift |
		SQEContext(bufGroup)<<ctxBufGrpShift |
		fdBits |
		CtxModeDirect
}

// ForFD returns a direct-mode context with only the fd set.
func ForFD(fd int32) SQEContext { return PackDirect(0, 0, 0, fd) }

// Op returns the `IORING_OP_*` opcode.
func (c SQEContext) Op() uint8 {
	if c.IsDirect() {
		return uint8((c & ctxOpMask) >> ctxOpShift)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.opcode
	}
	return c.IndirectSQE().opcode
}

// Flags returns the `IOSQE_*` flags.
func (c SQEContext) Flags() uint8 {
	if c.IsDirect() {
		return uint8((c & ctxFlagsMask) >> ctxFlagsShift)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.flags
	}
	return c.IndirectSQE().flags
}

// BufGroup returns the buffer group index.
func (c SQEContext) BufGroup() uint16 {
	if c.IsDirect() {
		return uint16((c & ctxBufGrpMask) >> ctxBufGrpShift)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.bufIndex
	}
	return c.IndirectSQE().bufIndex
}

// FD returns the sign-extended 30-bit file descriptor.
func (c SQEContext) FD() int32 {
	if c.IsDirect() {
		fdBits := c & ctxFDMask
		if fdBits&ctxFDSignBit != 0 {
			fdBits |= ctxFDSignExt
		}
		return int32(fdBits >> ctxFDShift)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.fd
	}
	return c.IndirectSQE().fd
}

// WithOp returns a new context with the opcode replaced.
// For Direct mode, modifies the inline bits.
// For Indirect/Extended modes, writes to the pointed-to SQE struct.
func (c SQEContext) WithOp(op uint8) SQEContext {
	if c.IsDirect() {
		return (c &^ (ctxOpMask | ctxModeMask)) | SQEContext(op) | CtxModeDirect
	}
	if c.IsExtended() {
		c.ExtSQE().SQE.opcode = op
		return c
	}
	c.IndirectSQE().opcode = op
	return c
}

// WithFlags returns a new context with the flags replaced.
// For Direct mode, modifies the inline bits.
// For Indirect/Extended modes, writes to the pointed-to SQE struct.
func (c SQEContext) WithFlags(flags uint8) SQEContext {
	if c.IsDirect() {
		return (c &^ (ctxFlagsMask | ctxModeMask)) | SQEContext(flags)<<ctxFlagsShift | CtxModeDirect
	}
	if c.IsExtended() {
		c.ExtSQE().SQE.flags = flags
		return c
	}
	c.IndirectSQE().flags = flags
	return c
}

// OrFlags returns a new context with flags ORed into the existing set.
// For Direct mode, modifies the inline bits.
// For Indirect/Extended modes, writes to the pointed-to SQE struct.
func (c SQEContext) OrFlags(flags uint8) SQEContext {
	if c.IsDirect() {
		return c | SQEContext(flags)<<ctxFlagsShift
	}
	if c.IsExtended() {
		c.ExtSQE().SQE.flags |= flags
		return c
	}
	c.IndirectSQE().flags |= flags
	return c
}

// WithBufGroup returns a new context with the buffer group replaced.
// For Direct mode, modifies the inline bits.
// For Indirect/Extended modes, writes to the pointed-to SQE struct.
func (c SQEContext) WithBufGroup(bufGroup uint16) SQEContext {
	if c.IsDirect() {
		return (c &^ (ctxBufGrpMask | ctxModeMask)) | SQEContext(bufGroup)<<ctxBufGrpShift | CtxModeDirect
	}
	if c.IsExtended() {
		c.ExtSQE().SQE.bufIndex = bufGroup
		return c
	}
	c.IndirectSQE().bufIndex = bufGroup
	return c
}

// WithFD returns a new context with the file descriptor replaced.
// For Direct mode, modifies the inline bits.
// For Indirect/Extended modes, writes to the pointed-to SQE struct.
func (c SQEContext) WithFD(fd iofd.FD) SQEContext {
	return c.withFD(int32(fd))
}

func (c SQEContext) withFD(fd int32) SQEContext {
	raw := fd
	if c.IsDirect() {
		fdBits := SQEContext(uint32(raw)&0x3FFFFFFF) << ctxFDShift
		return (c &^ (ctxFDMask | ctxModeMask)) | fdBits | CtxModeDirect
	}
	if c.IsExtended() {
		c.ExtSQE().SQE.fd = raw
		return c
	}
	c.IndirectSQE().fd = raw
	return c
}

// HasBufferSelect reports whether the IOSQE_BUFFER_SELECT flag is set.
// Only valid for Direct mode contexts.
func (c SQEContext) HasBufferSelect() bool { return c.Flags()&IOSQE_BUFFER_SELECT != 0 }

// Raw returns the underlying uint64 value for direct use in SQE.userData.
//
//go:nosplit
func (c SQEContext) Raw() uint64 { return uint64(c) }

// SQEContextFromRaw creates an SQEContext from a raw uint64 value.
// Used when decoding CQE.userData.
//
//go:nosplit
func SQEContextFromRaw(v uint64) SQEContext { return SQEContext(v) }

// IndirectSQE holds a complete SQE copy for completion context on Darwin too.
// Its borrowing rules match Linux when acquired from a Uring-owned pool.
type IndirectSQE struct {
	ioUringSqe
}

// Compile-time size assertion.
var _ [64 - unsafe.Sizeof(IndirectSQE{})]struct{}

// ExtSQE stub for non-Linux (must match linux ExtSQE layout for shared Ctx types).
type ExtSQE struct {
	SQE      ioUringSqe // 64 bytes
	UserData [64]byte   // 64 bytes
}

// Compile-time size assertions to match Linux layout.
var _ [128 - unsafe.Sizeof(ExtSQE{})]struct{}

// PackIndirect packs an indirect-mode pointer.
func PackIndirect(sqe *IndirectSQE) SQEContext {
	ptr := uintptr(unsafe.Pointer(sqe))
	if ptr&uintptr(ctxModeMask) != 0 {
		panic("uring: IndirectSQE pointer has bits 62-63 set")
	}
	return SQEContext(ptr) | CtxModeIndirect
}

// PackExtended packs an extended-mode pointer.
func PackExtended(sqe *ExtSQE) SQEContext {
	ptr := uintptr(unsafe.Pointer(sqe))
	if ptr&uintptr(ctxModeMask) != 0 {
		panic("uring: ExtSQE pointer has bits 62-63 set")
	}
	return SQEContext(ptr) | CtxModeExtended
}

// IndirectSQE returns the indirect pointer stored in `c`.
//
//go:nosplit
//go:nocheckptr
func (c SQEContext) IndirectSQE() *IndirectSQE {
	if !c.IsIndirect() {
		panic("uring: SQEContext is not in Indirect mode")
	}
	return (*IndirectSQE)(pointerFromTagged(uintptr(c & ctxPtrMask)))
}

// ExtSQE returns the extended pointer stored in `c`.
//
//go:nosplit
//go:nocheckptr
func (c SQEContext) ExtSQE() *ExtSQE {
	if !c.IsExtended() {
		panic("uring: SQEContext is not in Extended mode")
	}
	return (*ExtSQE)(pointerFromTagged(uintptr(c & ctxPtrMask)))
}

//go:nosplit
func pointerFromTagged(u uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

// CastUserData casts ExtSQE.UserData to a user-defined type on Darwin too.
//
// Prefer the pointer-free typed view helpers when a fixed `UserData` layout is
// enough. Raw `UserData` remains scalar caller-beware storage; pointer-bearing
// overlays and `ViewCtx1`...`ViewCtx7` ref layouts still require live roots to
// stay outside those raw bytes.
//
// CastUserData is the low-level escape hatch for code that needs a custom raw
// struct overlay. Any pointer returned by CastUserData is borrowed from ext and
// is valid only until the matching PutExtSQE. Callers must not retain the
// returned pointer, or pointers reachable from it, after release.
//
// `ExtSQE.UserData` is raw caller-beware storage. Prefer scalar payloads here;
// if a raw overlay stores Go pointers, interfaces, func values, maps, slices,
// strings, chans, or structs containing them in these bytes, caller code must
// keep the live roots outside `UserData`.
func CastUserData[T any](ext *ExtSQE) *T {
	var zero T
	if unsafe.Sizeof(zero) > uintptr(len(ext.UserData)) {
		panic("uring: CastUserData type exceeds 64-byte ExtSQE.UserData")
	}
	return (*T)(unsafe.Pointer(&ext.UserData[0]))
}
