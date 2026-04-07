// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"code.hybscloud.com/iofd"
	"unsafe"
)

// SQEContext encodes `io_uring.user_data`.
// Direct mode packs opcode, flags, buffer group, and fd inline.
// Indirect and extended modes store aligned pointers in the low 62 bits.
type SQEContext uint64

// Context mode constants (bits 62-63).
const (
	// CtxModeDirect indicates inline context (8B, zero allocation).
	CtxModeDirect SQEContext = 0 << 62

	// CtxModeIndirect indicates pointer to IndirectSQE (64B).
	CtxModeIndirect SQEContext = 1 << 62

	// CtxModeExtended indicates pointer to ExtSQE (128B).
	CtxModeExtended SQEContext = 2 << 62
)

// Context field bit positions and masks.
const (
	ctxModeMask    SQEContext = 3 << 62      // Bits 62-63
	ctxPtrMask     SQEContext = ^ctxModeMask // Lower 62 bits for pointer
	ctxOpMask      SQEContext = 0xFF         // 8 bits
	ctxFlagsShift             = 8            // Bits 8-15
	ctxFlagsMask   SQEContext = 0xFF << ctxFlagsShift
	ctxBufGrpShift            = 16 // Bits 16-31
	ctxBufGrpMask  SQEContext = 0xFFFF << ctxBufGrpShift
	ctxFDShift                = 32                       // Bits 32-61 (30 bits)
	ctxFDMask      SQEContext = 0x3FFFFFFF << ctxFDShift // 30 bits
)

// Mode returns the context mode (Direct, Indirect, Extended, or Reserved).
//
//go:nosplit
func (c SQEContext) Mode() SQEContext {
	return c & ctxModeMask
}

// IsDirect reports whether this is a Direct mode context (inline data).
//
//go:nosplit
func (c SQEContext) IsDirect() bool {
	return c.Mode() == CtxModeDirect
}

// IsIndirect reports whether this is an Indirect mode context (pointer to 64B).
//
//go:nosplit
func (c SQEContext) IsIndirect() bool {
	return c.Mode() == CtxModeIndirect
}

// IsExtended reports whether this is an Extended mode context (pointer to 128B).
//
//go:nosplit
func (c SQEContext) IsExtended() bool {
	return c.Mode() == CtxModeExtended
}

// PackDirect packs direct-mode submission context.
func PackDirect(op, flags uint8, bufGroup uint16, fd int32) SQEContext {
	// Extract lower 30 bits of FD
	fdBits := SQEContext(uint32(fd)&0x3FFFFFFF) << ctxFDShift
	return SQEContext(op) |
		SQEContext(flags)<<ctxFlagsShift |
		SQEContext(bufGroup)<<ctxBufGrpShift |
		fdBits |
		CtxModeDirect
}

// ForFD returns a direct-mode context with only the fd set.
//
//go:nosplit
func ForFD(fd int32) SQEContext {
	return PackDirect(0, 0, 0, fd)
}

// Op returns the `IORING_OP_*` opcode.
//
//go:nosplit
func (c SQEContext) Op() uint8 {
	if c.IsDirect() {
		return uint8(c & ctxOpMask)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.opcode
	}
	return c.IndirectSQE().opcode
}

// Flags returns the `IOSQE_*` flags.
//
//go:nosplit
func (c SQEContext) Flags() uint8 {
	if c.IsDirect() {
		return uint8((c >> ctxFlagsShift) & 0xFF)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.flags
	}
	return c.IndirectSQE().flags
}

// BufGroup returns the buffer group index.
//
//go:nosplit
func (c SQEContext) BufGroup() uint16 {
	if c.IsDirect() {
		return uint16((c >> ctxBufGrpShift) & 0xFFFF)
	}
	if c.IsExtended() {
		return c.ExtSQE().SQE.bufIndex
	}
	return c.IndirectSQE().bufIndex
}

// FD returns the sign-extended 30-bit file descriptor.
//
//go:nosplit
func (c SQEContext) FD() int32 {
	if c.IsDirect() {
		raw := (c >> ctxFDShift) & 0x3FFFFFFF
		if raw&0x20000000 != 0 {
			raw |= 0xC0000000
		}
		return int32(raw)
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
		return (c &^ ctxOpMask) | SQEContext(op)
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
		return (c &^ ctxFlagsMask) | SQEContext(flags)<<ctxFlagsShift
	}
	if c.IsExtended() {
		c.ExtSQE().SQE.flags = flags
		return c
	}
	c.IndirectSQE().flags = flags
	return c
}

// WithBufGroup returns a new context with the buffer group replaced.
// For Direct mode, modifies the inline bits.
// For Indirect/Extended modes, writes to the pointed-to SQE struct.
func (c SQEContext) WithBufGroup(bufGroup uint16) SQEContext {
	if c.IsDirect() {
		return (c &^ ctxBufGrpMask) | SQEContext(bufGroup)<<ctxBufGrpShift
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

// withFD returns a new context with the raw file descriptor replaced.
// It keeps the hot path in raw `int32` form for internal call sites that
// already operate on kernel-shaped fd values.
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
func (c SQEContext) HasBufferSelect() bool {
	return c.Flags()&IOSQE_BUFFER_SELECT != 0
}

// IndirectSQE stores a full SQE copy for indirect context.
// Callers must stop using it after the matching pool release.
type IndirectSQE struct {
	ioUringSqe // 64 bytes - mirrors kernel SQE structure
}

// Compile-time size assertion.
var _ [64 - unsafe.Sizeof(IndirectSQE{})]struct{} // IndirectSQE must be 64 bytes

// PackIndirect packs an indirect-mode pointer.
func PackIndirect(sqe *IndirectSQE) SQEContext {
	ptr := uintptr(unsafe.Pointer(sqe))
	if ptr&uintptr(ctxModeMask) != 0 {
		panic("uring: IndirectSQE pointer has bits 62-63 set")
	}
	return SQEContext(ptr) | CtxModeIndirect
}

// IndirectSQE returns the indirect pointer stored in `c`.
//
//go:nosplit
//go:nocheckptr
func (c SQEContext) IndirectSQE() *IndirectSQE {
	return (*IndirectSQE)(pointerFromTagged(uintptr(c & ctxPtrMask)))
}

// ExtSQE stores a full SQE and 64 bytes of user data.
// Callers must stop using it after the matching pool release.
type ExtSQE struct {
	SQE      ioUringSqe // 64 bytes - full system context
	UserData [64]byte   // 64 bytes - flexible user interpretation
}

// Compile-time size assertion.
var _ [128 - unsafe.Sizeof(ExtSQE{})]struct{} // ExtSQE must be 128 bytes

// PackExtended packs an extended-mode pointer.
func PackExtended(sqe *ExtSQE) SQEContext {
	ptr := uintptr(unsafe.Pointer(sqe))
	if ptr&uintptr(ctxModeMask) != 0 {
		panic("uring: ExtSQE pointer has bits 62-63 set")
	}
	return SQEContext(ptr) | CtxModeExtended
}

// ExtSQE returns the extended pointer stored in `c`.
//
//go:nosplit
//go:nocheckptr
func (c SQEContext) ExtSQE() *ExtSQE {
	return (*ExtSQE)(pointerFromTagged(uintptr(c & ctxPtrMask)))
}

// CastUserData casts `ExtSQE.UserData` to `*T`.
// The returned pointer is borrowed from `ext` and is valid only until release.
// `T` must fit within `ExtSQE.UserData`.
//
// `ExtSQE.UserData` is raw caller-beware storage. Prefer scalar payloads here;
// if a raw overlay stores Go pointers, interfaces, func values, maps, slices,
// strings, chans, or structs containing them in these bytes, caller code must
// keep the live roots outside `UserData`.
//
//go:nosplit
func CastUserData[T any](ext *ExtSQE) *T {
	var zero T
	if unsafe.Sizeof(zero) > uintptr(len(ext.UserData)) {
		panic("uring: CastUserData type exceeds 64-byte ExtSQE.UserData")
	}
	return (*T)(unsafe.Pointer(&ext.UserData[0]))
}

// pointerFromTagged recovers an unsafe.Pointer from a uintptr that was
// originally obtained from unsafe.Pointer (e.g. a tagged pointer stored in
// SQEContext). The reinterpret cast avoids the go vet rule-4 false positive
// on integer-to-Pointer conversion.
//
//go:nosplit
func pointerFromTagged(u uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

// Raw returns the underlying uint64 value for direct use in SQE.userData.
//
//go:nosplit
func (c SQEContext) Raw() uint64 {
	return uint64(c)
}

// SQEContextFromRaw creates an SQEContext from a raw uint64 value.
// Used when decoding CQE.userData.
//
//go:nosplit
func SQEContextFromRaw(v uint64) SQEContext {
	return SQEContext(v)
}

// Context returns the packed `SQEContext` from `CQE.user_data`.
func (cqe *ioUringCqe) Context() SQEContext {
	return SQEContextFromRaw(cqe.userData)
}
