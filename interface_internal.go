// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

var defaultOptions = Options{
	Entries:                 EntriesMedium,
	LockedBufferMem:         registerBufferDefaultMem,
	ReadBufferSize:          bufferSizeDefault,
	ReadBufferNum:           EntriesMedium * 2,
	WriteBufferSize:         bufferSizeDefault,
	WriteBufferNum:          EntriesMedium,
	MultiIssuers:            false,
	NotifySucceed:           false,
	IndirectSubmissionQueue: false,
}

// OptionFunc mutates [Options] during [New] construction.
type OptionFunc = func(opt *Options)

// Apply applies option helpers in order.
func (uo *Options) Apply(opts ...OptionFunc) {
	for _, f := range opts {
		f(uo)
	}
}

var (
	// LargeLockedBufferMemOptions uses the maximum registered buffer memory (128 MiB).
	// This equals the default and keeps the helper-based option style available.
	LargeLockedBufferMemOptions OptionFunc = func(opt *Options) {
		opt.LockedBufferMem = registerBufferSize * registerBufferNum
	}
	// MultiSizeBufferOptions enables multi-size buffer groups.
	MultiSizeBufferOptions OptionFunc = func(opt *Options) {
		opt.MultiSizeBuffer = 1
	}
)

func (ur *Uring) operationOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) readFixedOptions(bufIndex int, opts []OpOptionFunc) (flags uint8, offset int64, n int) {
	if len(opts) < 1 {
		bufSize := len(ur.ioUring.bufs[bufIndex])
		return uringOpFlagsNone, 0, bufSize
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)

	bufSize := len(ur.ioUring.bufs[bufIndex])
	readSize := bufSize
	if opt.N != nil && *opt.N < bufSize {
		readSize = *opt.N
	}

	return opt.Flags, opt.Offset, readSize
}

func (ur *Uring) writeFixedOptions(opts []OpOptionFunc) (flags uint8, offset int64) {
	if len(opts) < 1 {
		return uringOpFlagsNone, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)

	return opt.Flags, opt.Offset
}

func (ur *Uring) pollAddOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) pollRemoveOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) msgOptions(opts []OpOptionFunc) (flags uint8, ioprio uint16) {
	return ur.flagsIOPrioOptions(opts)
}

func (ur *Uring) flagsIOPrioOptions(opts []OpOptionFunc) (flags uint8, ioprio uint16) {
	if len(opts) < 1 {
		return uringOpFlagsNone, uringOpIOPrioNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.IOPrio
}

func (ur *Uring) sendmsgOptions(opts []OpOptionFunc) (flags uint8, ioprio uint16) {
	return ur.msgOptions(opts)
}

func (ur *Uring) recvmsgOptions(opts []OpOptionFunc) (flags uint8, ioprio uint16) {
	return ur.msgOptions(opts)
}

func (ur *Uring) timeoutOptions(opts []OpOptionFunc) (flags uint8, cnt int, timeoutFlags uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, 1, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.Count, opt.TimeoutFlags
}

func (ur *Uring) timeoutRemoveOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) acceptOptions(opts []OpOptionFunc) (flags uint8, ioprio uint16) {
	return ur.flagsIOPrioOptions(opts)
}
func (ur *Uring) asyncCancelOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) connectOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) openAtOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) closeOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) statxOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) streamOptions(b []byte, opts []OpOptionFunc) (flags uint8, ioprio uint16, offset int64, n int) {
	if len(opts) < 1 {
		return uringOpFlagsNone, uringOpIOPrioNone, 0, len(b)
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)

	if opt.N != nil && *opt.N < len(b) {
		n = *opt.N
	} else {
		n = len(b)
	}

	return opt.Flags, opt.IOPrio, opt.Offset, n
}

func (ur *Uring) readOptions(b []byte, opts []OpOptionFunc) (flags uint8, ioprio uint16, offset int64, n int) {
	return ur.streamOptions(b, opts)
}

func (ur *Uring) writeOptions(b []byte, opts []OpOptionFunc) (flags uint8, ioprio uint16, offset int64, n int) {
	return ur.streamOptions(b, opts)
}

func (ur *Uring) sendOptions(b []byte, opts []OpOptionFunc) (flags uint8, ioprio uint16, offset int64, n int) {
	return ur.streamOptions(b, opts)
}

func (ur *Uring) receiveOptions(b []byte, opts []OpOptionFunc) (flags uint8, ioprio uint16, offset int64, n int) {
	return ur.streamOptions(b, opts)
}

func (ur *Uring) spliceOptions(opts []OpOptionFunc) (flags uint8, n int, spliceFlags uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, bufferSizeDefault, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)

	size := bufferSizeDefault
	if opt.N != nil {
		size = *opt.N
	}
	return opt.Flags, size, opt.SpliceFlags
}

func (ur *Uring) teeOptions(opts []OpOptionFunc) (flags uint8, n int, spliceFlags uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, bufferSizeDefault, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)

	size := bufferSizeDefault
	if opt.N != nil {
		size = *opt.N
	}
	return opt.Flags, size, opt.SpliceFlags
}

func (ur *Uring) shutdownOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) renameAtOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) unlinkAtOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) mkdirAtOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) symlinkAtOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) linkAtOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) syncOptions(opts []OpOptionFunc) (flags uint8, fsyncFlags uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.FsyncFlags
}

func (ur *Uring) linkTimeoutOptions(opts []OpOptionFunc) (flags uint8, timeoutFlags uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.TimeoutFlags
}

func (ur *Uring) timeoutUpdateOptions(opts []OpOptionFunc) (flags uint8, timeoutFlags uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.TimeoutFlags
}

func (ur *Uring) receiveWithBufferSelectOptions(f PollFd, opts []OpOptionFunc) (flags uint8, ioprio uint16, bufferSize int, bufferGroup uint16) {
	if len(opts) < 1 {
		return uringOpFlagsNone, uringOpIOPrioNone, bufferSizeDefault, ur.getBufferGid(f, bufferSizeDefault)
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.IOPrio, opt.ReadBufferSize, ur.getBufferGid(f, opt.ReadBufferSize)
}

func (ur *Uring) socketOptions(opts []OpOptionFunc) (flags uint8, i uint32) {
	if len(opts) < 1 {
		return uringOpFlagsNone, 0
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.FileIndex
}

func (ur *Uring) bindOptions(opts []OpOptionFunc) (flags uint8) {
	if len(opts) < 1 {
		return uringOpFlagsNone
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags
}

func (ur *Uring) listenOptions(opts []OpOptionFunc) (flags uint8, backlog int) {
	if len(opts) < 1 {
		return uringOpFlagsNone, defaultBacklog
	}
	opt := defaultUringOpOption
	opt.Apply(opts...)
	return opt.Flags, opt.Backlog
}

func (ur *Uring) getBufferGid(f PollFd, size int) uint16 {
	if ur.buffers != nil {
		return ur.buffers.bufGroup(f)
	} else if ur.bufferGroups != nil {
		return ur.bufferGroups.bufGroupBySize(f, size)
	}
	panic("Uring has neither buffers nor bufferGroups set")
}
