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
	FileIndex uint32
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
	// File offset for positioned I/O operations.
	Offset int64
	// Size limit (nil = use buffer length).
	N *int
	// Completion count for timeout operations.
	Count int
	// Timeout clock and behavior flags (IORING_TIMEOUT_ABS, _BOOTTIME, _REALTIME, etc.)
	TimeoutFlags uint32
	// Splice/tee operation flags (SPLICE_F_MOVE, _NONBLOCK, _MORE, _GIFT).
	SpliceFlags uint32
	// Fsync behavior flags (IORING_FSYNC_DATASYNC).
	FsyncFlags uint32
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

// OpOptionFunc is a type that defines a functional option for configuring a OpOption.
type OpOptionFunc func(opt *OpOption)

func (uo *OpOption) Apply(opts ...OpOptionFunc) {
	for _, f := range opts {
		f(uo)
	}
}

// WithFileIndex sets the file index for direct descriptor operations.
// For socket/accept direct, pass IORING_FILE_INDEX_ALLOC for auto-allocation,
// or a specific slot index (0-based) into the registered file table.
func WithFileIndex(idx uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.FileIndex = idx
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

// WithOffset sets the file offset for positioned I/O operations.
func WithOffset(offset int64) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Offset = offset
	}
}

// WithN sets the size limit for read/write/splice operations.
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
func WithCount(cnt int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Count = cnt
	}
}

// WithReadBufferSize sets the kernel-provided buffer size for receive operations
// with buffer selection.
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
