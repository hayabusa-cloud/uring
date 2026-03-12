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

// OpOption represents configuration options for Uring operations.
type OpOption struct {
	Flags           uint8
	CallbackKind    uint8
	IOPrio          uint16
	FileIndex       uint32
	Backlog         int
	ReadBufferSize  int
	WriteBufferSize int
	Duration        time.Duration
	FileMode        os.FileMode
	Fadvise         int
	Offset          int64
	N               *int
	Count           int
}

const (
	uringOpFlagsNone = iota
)
const (
	uringOpCallbackKindNone = iota
	uringOpCallbackKindBind
	uringOpCallbackKindListen
)
const (
	uringOpIOPrioNone = iota
)

var defaultUringOpOption = OpOption{
	Flags:           uringOpFlagsNone,
	CallbackKind:    uringOpCallbackKindNone,
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
