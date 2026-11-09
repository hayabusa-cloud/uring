// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
	"os"
	"time"
)

// OpOption holds per-operation configuration.
// Each operation reads only the fields it uses, and ignores the rest.
type OpOption struct {
	// SQE control flags (IOSQE_IO_LINK, IOSQE_IO_DRAIN, IOSQE_ASYNC, etc.)
	Flags uint8
	// I/O priority. For file I/O: (class<<13)|level where class ∈ {RT=1,BE=2,IDLE=3}.
	// For io_uring-specific ops: operation-defined flags (e.g. IORING_ACCEPT_MULTISHOT).
	IOPrio uint16
	// Registered file table slot index for direct descriptor operations.
	// nil leaves the target unspecified.
	FileIndex *uint32
	// Registered credential personality ID (0 = caller's credentials).
	Personality uint16
	// Listen backlog depth.
	Backlog int
	// Kernel-provided buffer size for receive with buffer selection.
	ReadBufferSize int
	// Write buffer sizing hint.
	WriteBufferSize int
	// Timeout duration for timeout-class operations.
	Duration time.Duration
	// File permission mode for open/mkdir operations.
	FileMode os.FileMode
	// Advisory hint for fadvise/madvise operations.
	Fadvise int
	// File position or socket-buffer byte offset; see [WithOffset].
	Offset int64
	// Optional byte limit; nil uses the operation's default. See [WithN].
	N *int
	// Completion count for timeout operations. Values must fit uint32.
	Count int
	// Timeout clock and behavior flags (IORING_TIMEOUT_ABS, _BOOTTIME, _REALTIME, etc.)
	TimeoutFlags uint32
	// Splice/tee operation flags (SPLICE_F_MOVE, _NONBLOCK, _MORE, _GIFT).
	SpliceFlags uint32
	// Fsync behavior flags (IORING_FSYNC_DATASYNC).
	FsyncFlags uint32
	// URING_CMD behavior flags (IORING_URING_CMD_FIXED, _MULTISHOT).
	UringCmdFlags uint32
}

const (
	uringOpFlagsNone = iota
)

const (
	uringOpIOPrioNone = iota
)

// I/O priority class constants for WithIOPrioClass.
const (
	IOPrioClassNone = 0
	IOPrioClassRT   = 1 // Real-time
	IOPrioClassBE   = 2 // Best-effort
	IOPrioClassIDLE = 3 // Idle
)

var defaultUringOpOption = OpOption{
	Flags:           uringOpFlagsNone,
	IOPrio:          uringOpIOPrioNone,
	Backlog:         defaultBacklog,
	ReadBufferSize:  bufferSizeDefault,
	WriteBufferSize: bufferSizeDefault,
	Duration:        0, // Caller must specify duration
	FileMode:        0644,
	N:               nil,
	Count:           1,
}

// OpOptionFunc configures an [OpOption].
type OpOptionFunc func(opt *OpOption)

// Apply applies option functions in order.
func (uo *OpOption) Apply(opts ...OpOptionFunc) {
	for _, f := range opts {
		f(uo)
	}
}

// WithFileIndex makes SocketRaw create a direct descriptor at the specified
// zero-based slot. IORING_FILE_INDEX_ALLOC requests auto-allocation.
func WithFileIndex(idx uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.FileIndex = &idx
	}
}

// WithFlags sets SQE control flags (IOSQE_IO_LINK, IOSQE_IO_DRAIN, etc.).
func WithFlags(flags uint8) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Flags = flags
	}
}

// WithIOPrio sets the raw I/O priority value.
func WithIOPrio(ioprio uint16) OpOptionFunc {
	return func(opt *OpOption) {
		opt.IOPrio = ioprio
	}
}

// WithIOPrioClass sets I/O priority from class (RT=1, BE=2, IDLE=3) and level (0-7).
func WithIOPrioClass(class, level uint8) OpOptionFunc {
	return func(opt *OpOption) {
		opt.IOPrio = uint16(class)<<13 | uint16(level&7)
	}
}

// WithPersonality sets the registered credential personality ID for per-operation
// credential override. Register personalities via IORING_REGISTER_PERSONALITY.
func WithPersonality(id uint16) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Personality = id
	}
}

// WithBacklog sets the listen backlog depth.
func WithBacklog(n int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Backlog = n
	}
}

// WithOffset sets the file position or buffer offset used by an operation.
// [Uring.Read], [Uring.Write], [Uring.ReadFixed], and [Uring.WriteFixed] use a
// file offset; -1 uses and advances the file's current position.
// [Uring.Send], [Uring.SendFD], [Uring.Receive], and [Uring.ReceiveFD] use a
// nonnegative byte offset within the supplied buffer. Receive operations
// with buffer selection ignore this option.
// [ZCTracker.SendZC] uses a byte offset within its buffer;
// [ZCTracker.SendZCFixed] uses one within its explicit offset/length range.
func WithOffset(offset int64) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Offset = offset
	}
}

// WithN sets the byte limit for operations that use it.
// [Uring.Read], [Uring.Write], and [Uring.ReadFixed] cap it at the buffer length.
// [Uring.Send], [Uring.SendFD], [Uring.Receive], and [Uring.ReceiveFD] require
// it to fit the supplied buffer after [WithOffset]. Receive operations with
// buffer selection use [WithReadBufferSize] instead.
// [ZCTracker.SendZC] and [ZCTracker.SendZCFixed] require a positive limit that
// fits the selected buffer range after [WithOffset].
// [Uring.WriteFixed], [Uring.Splice], and [Uring.Tee] use their explicit lengths.
func WithN(n int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.N = &n
	}
}

// WithDuration sets the timeout duration.
func WithDuration(d time.Duration) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Duration = d
	}
}

// WithFileMode sets the file permission mode for open/mkdir operations.
func WithFileMode(mode os.FileMode) OpOptionFunc {
	return func(opt *OpOption) {
		opt.FileMode = mode
	}
}

// WithFadvise sets the advisory hint for fadvise/madvise operations.
func WithFadvise(advice int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Fadvise = advice
	}
}

// WithCount sets the completion count for timeout operations.
// Values must be in the inclusive uint32 domain.
func WithCount(cnt int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Count = cnt
	}
}

// WithReadBufferSize sets the kernel-provided buffer size for receive operations
// with buffer selection. For raw multishot receive, zero leaves the kernel
// length uncapped and selects the default-size buffer group. Single-shot
// receive requires a positive size.
func WithReadBufferSize(n int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.ReadBufferSize = n
	}
}

// WithWriteBufferSize sets the write buffer sizing hint.
func WithWriteBufferSize(n int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.WriteBufferSize = n
	}
}

// WithTimeoutFlags sets timeout clock and behavior flags.
// Combine IORING_TIMEOUT_ABS, IORING_TIMEOUT_BOOTTIME, IORING_TIMEOUT_REALTIME,
// IORING_TIMEOUT_ETIME_SUCCESS, or IORING_TIMEOUT_MULTISHOT.
func WithTimeoutFlags(flags uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags = flags
	}
}

// WithTimeoutAbsolute treats the timeout duration as an absolute timestamp.
func WithTimeoutAbsolute() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_ABS
	}
}

// WithTimeoutBootTime uses CLOCK_BOOTTIME (survives system suspend).
func WithTimeoutBootTime() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_BOOTTIME
	}
}

// WithTimeoutRealTime uses CLOCK_REALTIME (wall clock, NTP-adjusted).
func WithTimeoutRealTime() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_REALTIME
	}
}

// WithTimeoutEtimeSuccess converts -ETIME into successful completion.
func WithTimeoutEtimeSuccess() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_ETIME_SUCCESS
	}
}

// WithTimeoutMultishot makes the timeout fire repeatedly.
func WithTimeoutMultishot() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_MULTISHOT
	}
}

// WithSpliceFlags sets splice/tee operation flags.
// Combine SPLICE_F_MOVE, SPLICE_F_NONBLOCK, SPLICE_F_MORE, SPLICE_F_GIFT.
func WithSpliceFlags(flags uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.SpliceFlags = flags
	}
}

// WithFsyncDataSync uses fdatasync semantics (sync data only, skip metadata).
func WithFsyncDataSync() OpOptionFunc {
	return func(opt *OpOption) {
		opt.FsyncFlags = 1
	}
}

// WithUringCmdFlags sets IORING_OP_URING_CMD behavior flags.
// IORING_URING_CMD_FIXED interprets sqeCtx.BufGroup as a registered buffer
// index. IORING_URING_CMD_MULTISHOT requires IOSQE_BUFFER_SELECT and
// interprets sqeCtx.BufGroup as a provided-buffer group. FIXED and MULTISHOT
// are mutually exclusive.
func WithUringCmdFlags(flags uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.UringCmdFlags = flags
	}
}
