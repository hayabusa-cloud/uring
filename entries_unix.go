// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

import "unsafe"

// Base entries from the Linux io_uring ABI. The Darwin simulator uses the same layout.
type ioUringSqe struct {
	opcode   uint8
	flags    uint8
	ioprio   uint16
	fd       int32
	off      uint64
	addr     uint64
	len      uint32
	uflags   uint32
	userData uint64

	bufIndex    uint16
	personality uint16
	spliceFdIn  int32
	pad         [2]uint64
}

type ioUringCqe struct {
	userData uint64
	res      int32
	flags    uint32
}

var (
	_ [64]byte = [unsafe.Sizeof(ioUringSqe{})]byte{}
	_ [16]byte = [unsafe.Sizeof(ioUringCqe{})]byte{}
)
