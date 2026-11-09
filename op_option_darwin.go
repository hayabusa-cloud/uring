// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
	"os"
	"time"
)

// OpOption holds per-operation configuration.
// Each operation reads only the fields it uses, and ignores the rest.
type OpOption struct {
	Flags           uint8
	IOPrio          uint16
	FileIndex       *uint32
	Personality     uint16
	Backlog         int
	ReadBufferSize  int
	WriteBufferSize int
	Duration        time.Duration
	FileMode        os.FileMode
	Fadvise         int
	Offset          int64 // File position or socket-buffer byte offset; see [WithOffset].
	N               *int  // Optional byte limit; nil uses the operation's default. See [WithN].
	Count           int   // Timeout completion count; values must fit uint32.
	TimeoutFlags    uint32
	SpliceFlags     uint32
	FsyncFlags      uint32
	UringCmdFlags   uint32
}

// OpOptionFunc configures an [OpOption].
type OpOptionFunc func(opt *OpOption)

// I/O priority class constants for WithIOPrioClass.
const (
	IOPrioClassNone = 0
	IOPrioClassRT   = 1
	IOPrioClassBE   = 2
	IOPrioClassIDLE = 3
)

// Apply applies option functions in order.
func (uo *OpOption) Apply(opts ...OpOptionFunc) {
	for _, f := range opts {
		f(uo)
	}
}

var defaultUringOpOption = OpOption{
	Flags:           0,
	IOPrio:          0,
	FileIndex:       nil,
	Personality:     0,
	Backlog:         defaultBacklog,
	ReadBufferSize:  bufferSizeDefault,
	WriteBufferSize: bufferSizeDefault,
	Offset:          0,
	N:               nil,
	Count:           1,
}

// WithFileIndex makes SocketRaw request a direct descriptor. Direct descriptors
// are not supported by the Darwin backend.
func WithFileIndex(idx uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.FileIndex = &idx
	}
}

// WithFlags sets SQE control flags.
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

// WithIOPrioClass sets I/O priority from class and level.
func WithIOPrioClass(class, level uint8) OpOptionFunc {
	return func(opt *OpOption) {
		opt.IOPrio = uint16(class)<<13 | uint16(level&7)
	}
}

// WithPersonality sets the registered credential personality ID.
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

// WithFileMode sets the file permission mode.
func WithFileMode(mode os.FileMode) OpOptionFunc {
	return func(opt *OpOption) {
		opt.FileMode = mode
	}
}

// WithFadvise sets the advisory hint.
func WithFadvise(advice int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Fadvise = advice
	}
}

// WithCount sets the timeout completion count.
// Values must be in the inclusive uint32 domain.
func WithCount(cnt int) OpOptionFunc {
	return func(opt *OpOption) {
		opt.Count = cnt
	}
}

// WithReadBufferSize sets the kernel-provided buffer size.
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
func WithTimeoutFlags(flags uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags = flags
	}
}

// WithTimeoutAbsolute treats the timeout as absolute timestamp.
func WithTimeoutAbsolute() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_ABS
	}
}

// WithTimeoutBootTime uses CLOCK_BOOTTIME.
func WithTimeoutBootTime() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_BOOTTIME
	}
}

// WithTimeoutRealTime uses CLOCK_REALTIME.
func WithTimeoutRealTime() OpOptionFunc {
	return func(opt *OpOption) {
		opt.TimeoutFlags |= IORING_TIMEOUT_REALTIME
	}
}

// WithTimeoutEtimeSuccess converts -ETIME into success.
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
func WithSpliceFlags(flags uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.SpliceFlags = flags
	}
}

// WithFsyncDataSync uses fdatasync semantics.
func WithFsyncDataSync() OpOptionFunc {
	return func(opt *OpOption) {
		opt.FsyncFlags = 1
	}
}

// WithUringCmdFlags sets Linux URING_CMD behavior flags for API parity.
// IORING_URING_CMD_FIXED and IORING_URING_CMD_MULTISHOT are mutually exclusive.
func WithUringCmdFlags(flags uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.UringCmdFlags = flags
	}
}
