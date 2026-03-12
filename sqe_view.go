// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// SQEView provides read-only access to submission queue entry fields.
// It wraps the internal ioUringSqe structure and exposes its fields
// through public accessor methods.
//
// # Usage
//
//	cqe := cqes[i]
//	if cqe.FullSQE() {
//	    sqe := cqe.SQE()
//	    op := sqe.Opcode()
//	    fd := sqe.FD()
//	    addr := sqe.Addr()
//	    // ...
//	}
//
// # Field Layout (64 bytes)
//
//	┌────────────┬────────────┬────────────┬────────────┐
//	│ opcode (1) │ flags (1)  │ ioprio (2) │   fd (4)   │  bytes 0-7
//	├────────────┴────────────┴────────────┴────────────┤
//	│                    off (8)                        │  bytes 8-15
//	├───────────────────────────────────────────────────┤
//	│                   addr (8)                        │  bytes 16-23
//	├────────────────────────┬──────────────────────────┤
//	│       len (4)          │      uflags (4)          │  bytes 24-31
//	├────────────────────────┴──────────────────────────┤
//	│                  userData (8)                     │  bytes 32-39
//	├────────────┬───────────┬──────────────────────────┤
//	│bufIndex(2) │person.(2) │     spliceFdIn (4)       │  bytes 40-47
//	├────────────┴───────────┴──────────────────────────┤
//	│                   pad[0] (8)                      │  bytes 48-55
//	├───────────────────────────────────────────────────┤
//	│                   pad[1] (8)                      │  bytes 56-63
//	└───────────────────────────────────────────────────┘
type SQEView struct {
	sqe *ioUringSqe
}

// ViewSQE creates an SQEView from an IndirectSQE.
//
//go:nosplit
func ViewSQE(indirect *IndirectSQE) SQEView {
	return SQEView{sqe: &indirect.ioUringSqe}
}

// ViewExtSQE creates an SQEView from an ExtSQE.
//
//go:nosplit
func ViewExtSQE(ext *ExtSQE) SQEView {
	return SQEView{sqe: &ext.SQE}
}

// Valid reports whether the view points to a valid SQE.
//
//go:nosplit
func (v SQEView) Valid() bool {
	return v.sqe != nil
}

// Opcode returns the IORING_OP_* operation code.
//
//go:nosplit
func (v SQEView) Opcode() uint8 {
	return v.sqe.opcode
}

// Flags returns the IOSQE_* submission flags.
//
//go:nosplit
func (v SQEView) Flags() uint8 {
	return v.sqe.flags
}

// IoPrio returns the I/O priority.
//
//go:nosplit
func (v SQEView) IoPrio() uint16 {
	return v.sqe.ioprio
}

// FD returns the file descriptor for the operation.
//
//go:nosplit
func (v SQEView) FD() iofd.FD {
	return iofd.FD(v.sqe.fd)
}

// RawFD returns the raw file descriptor as int32.
//
//go:nosplit
func (v SQEView) RawFD() int32 {
	return v.sqe.fd
}

// Off returns the offset field.
// Interpretation depends on the operation:
//   - For read/write: file offset
//   - For accept: sockaddr length pointer
//   - For timeout: count or flags
//
//go:nosplit
func (v SQEView) Off() uint64 {
	return v.sqe.off
}

// Addr returns the address/pointer field.
// Interpretation depends on the operation:
//   - For read/write: buffer address
//   - For accept: sockaddr pointer
//   - For socket: protocol
//
//go:nosplit
func (v SQEView) Addr() uint64 {
	return v.sqe.addr
}

// Len returns the length field.
// Interpretation depends on the operation:
//   - For read/write: buffer length
//   - For accept: file flags
//   - For socket: socket type
//
//go:nosplit
func (v SQEView) Len() uint32 {
	return v.sqe.len
}

// UFlags returns the union flags field.
// Interpretation depends on the operation:
//   - For read/write: RW flags
//   - For poll: poll events
//   - For timeout: timeout flags
//   - For accept: accept flags
//   - For open: open flags
//   - For send/recv: msg flags
//
//go:nosplit
func (v SQEView) UFlags() uint32 {
	return v.sqe.uflags
}

// UserData returns the user data field (the packed SQEContext).
//
//go:nosplit
func (v SQEView) UserData() uint64 {
	return v.sqe.userData
}

// BufIndex returns the buffer index for fixed buffer operations.
//
//go:nosplit
func (v SQEView) BufIndex() uint16 {
	return v.sqe.bufIndex
}

// BufGroup returns the buffer group ID.
// Only valid when IOSQE_BUFFER_SELECT flag is set.
//
//go:nosplit
func (v SQEView) BufGroup() uint16 {
	return v.sqe.bufIndex // Same field, different interpretation
}

// Personality returns the personality ID for credentials.
//
//go:nosplit
func (v SQEView) Personality() uint16 {
	return v.sqe.personality
}

// SpliceFDIn returns the splice input file descriptor.
//
//go:nosplit
func (v SQEView) SpliceFDIn() int32 {
	return v.sqe.spliceFdIn
}

// FileIndex returns the file index for direct file operations.
// This is a union with spliceFdIn.
//
//go:nosplit
func (v SQEView) FileIndex() uint32 {
	return uint32(v.sqe.spliceFdIn)
}

// HasBufferSelect reports whether IOSQE_BUFFER_SELECT flag is set.
//
//go:nosplit
func (v SQEView) HasBufferSelect() bool {
	return v.sqe.flags&IOSQE_BUFFER_SELECT != 0
}

// HasFixedFile reports whether IOSQE_FIXED_FILE flag is set.
//
//go:nosplit
func (v SQEView) HasFixedFile() bool {
	return v.sqe.flags&IOSQE_FIXED_FILE != 0
}

// HasIODrain reports whether IOSQE_IO_DRAIN flag is set.
//
//go:nosplit
func (v SQEView) HasIODrain() bool {
	return v.sqe.flags&IOSQE_IO_DRAIN != 0
}

// HasIOLink reports whether IOSQE_IO_LINK flag is set.
//
//go:nosplit
func (v SQEView) HasIOLink() bool {
	return v.sqe.flags&IOSQE_IO_LINK != 0
}

// HasIOHardlink reports whether IOSQE_IO_HARDLINK flag is set.
//
//go:nosplit
func (v SQEView) HasIOHardlink() bool {
	return v.sqe.flags&IOSQE_IO_HARDLINK != 0
}

// HasAsync reports whether IOSQE_ASYNC flag is set.
//
//go:nosplit
func (v SQEView) HasAsync() bool {
	return v.sqe.flags&IOSQE_ASYNC != 0
}

// HasCQESkipSuccess reports whether IOSQE_CQE_SKIP_SUCCESS flag is set.
//
//go:nosplit
func (v SQEView) HasCQESkipSuccess() bool {
	return v.sqe.flags&IOSQE_CQE_SKIP_SUCCESS != 0
}
