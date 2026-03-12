// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

const uringOpCallbackKindNone = 0

type OpOption struct {
	Flags          uint8
	IOPrio         uint16
	Offset         int64
	N              *int
	Count          int
	Fadvise        int
	CallbackKind   uint8
	FileIndex      uint32
	Backlog        int
	ReadBufferSize int
}

type OpOptionFunc func(opt *OpOption)

func (uo *OpOption) Apply(opts ...OpOptionFunc) {
	for _, f := range opts {
		f(uo)
	}
}

var defaultUringOpOption = OpOption{
	Flags:          0,
	IOPrio:         0,
	Offset:         0,
	N:              nil,
	Count:          1,
	Fadvise:        0,
	CallbackKind:   uringOpCallbackKindNone,
	FileIndex:      0,
	Backlog:        defaultBacklog,
	ReadBufferSize: bufferSizeDefault,
}

// WithFileIndex sets the file index for direct descriptor operations.
// For socket/accept direct, pass IORING_FILE_INDEX_ALLOC for auto-allocation,
// or a specific slot index (0-based) into the registered file table.
func WithFileIndex(idx uint32) OpOptionFunc {
	return func(opt *OpOption) {
		opt.FileIndex = idx
	}
}
