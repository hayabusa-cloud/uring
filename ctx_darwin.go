// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "unsafe"

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

//go:nosplit
func (c SQEContext) Mode() SQEContext { return c & ctxModeMask }

//go:nosplit
func (c SQEContext) IsDirect() bool { return c.Mode() == CtxModeDirect }

//go:nosplit
func (c SQEContext) IsIndirect() bool { return c.Mode() == CtxModeIndirect }

//go:nosplit
func (c SQEContext) IsExtended() bool { return c.Mode() == CtxModeExtended }

func PackDirect(op, flags uint8, bufGroup uint16, fd int32) SQEContext {
	fdBits := SQEContext(uint32(fd)&0x3FFFFFFF) << ctxFDShift
	return SQEContext(op) |
		SQEContext(flags)<<ctxFlagsShift |
		SQEContext(bufGroup)<<ctxBufGrpShift |
		fdBits |
		CtxModeDirect
}

func ForFD(fd int32) SQEContext { return PackDirect(0, 0, 0, fd) }

func (c SQEContext) Op() uint8 { return uint8((c & ctxOpMask) >> ctxOpShift) }

func (c SQEContext) Flags() uint8 { return uint8((c & ctxFlagsMask) >> ctxFlagsShift) }

func (c SQEContext) BufGroup() uint16 { return uint16((c & ctxBufGrpMask) >> ctxBufGrpShift) }

func (c SQEContext) FD() int32 {
	fdBits := c & ctxFDMask
	if fdBits&ctxFDSignBit != 0 {
		fdBits |= ctxFDSignExt
	}
	return int32(fdBits >> ctxFDShift)
}

func (c SQEContext) WithOp(op uint8) SQEContext {
	return (c &^ (ctxOpMask | ctxModeMask)) | SQEContext(op) | CtxModeDirect
}

func (c SQEContext) WithFlags(flags uint8) SQEContext {
	return (c &^ (ctxFlagsMask | ctxModeMask)) | SQEContext(flags)<<ctxFlagsShift | CtxModeDirect
}

func (c SQEContext) WithBufGroup(bufGroup uint16) SQEContext {
	return (c &^ (ctxBufGrpMask | ctxModeMask)) | SQEContext(bufGroup)<<ctxBufGrpShift | CtxModeDirect
}

func (c SQEContext) WithFD(fd int32) SQEContext {
	fdBits := SQEContext(uint32(fd)&0x3FFFFFFF) << ctxFDShift
	return (c &^ (ctxFDMask | ctxModeMask)) | fdBits | CtxModeDirect
}

func (c SQEContext) HasBufferSelect() bool { return c.Flags()&IOSQE_BUFFER_SELECT != 0 }

//go:nosplit
func (c SQEContext) Raw() uint64 { return uint64(c) }

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

func PackIndirect(sqe *IndirectSQE) SQEContext {
	ptr := uintptr(unsafe.Pointer(sqe))
	if ptr&uintptr(ctxModeMask) != 0 {
		panic("uring: IndirectSQE pointer has bits 62-63 set")
	}
	return SQEContext(ptr) | CtxModeIndirect
}

func PackExtended(sqe *ExtSQE) SQEContext {
	ptr := uintptr(unsafe.Pointer(sqe))
	if ptr&uintptr(ctxModeMask) != 0 {
		panic("uring: ExtSQE pointer has bits 62-63 set")
	}
	return SQEContext(ptr) | CtxModeExtended
}

//go:nosplit
//go:nocheckptr
func (c SQEContext) IndirectSQE() *IndirectSQE {
	if !c.IsIndirect() {
		panic("uring: SQEContext is not in Indirect mode")
	}
	return (*IndirectSQE)(unsafe.Pointer(uintptr(c & ctxPtrMask)))
}

//go:nosplit
//go:nocheckptr
func (c SQEContext) ExtSQE() *ExtSQE {
	if !c.IsExtended() {
		panic("uring: SQEContext is not in Extended mode")
	}
	return (*ExtSQE)(unsafe.Pointer(uintptr(c & ctxPtrMask)))
}

// CastUserData casts ExtSQE.UserData to a user-defined type on Darwin too.
//
// Prefer the typed user-data view helpers (ViewCtx, ViewCtx1, ..., ViewCtx7) for
// new code. They make the intended layout explicit and are the primary surface
// for higher-level integrations.
//
// CastUserData is the low-level escape hatch for code that needs a custom raw
// struct overlay. Any pointer returned by CastUserData is borrowed from ext and
// is valid only until the matching PutExtSQE. Callers must not retain the
// returned pointer, or pointers reachable from it, after release.
func CastUserData[T any](ext *ExtSQE) *T {
	var zero T
	if unsafe.Sizeof(zero) > uintptr(len(ext.UserData)) {
		panic("uring: CastUserData type exceeds 64-byte ExtSQE.UserData")
	}
	return (*T)(unsafe.Pointer(&ext.UserData[0]))
}
