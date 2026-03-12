// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"syscall"
	"unsafe"

	"code.hybscloud.com/iobuf"
)

// Buffer size constants re-exported from iobuf.
// These follow a power-of-4 progression starting at 32 bytes.
const (
	BufferSizePico   = iobuf.BufferSizePico   // 32 B
	BufferSizeNano   = iobuf.BufferSizeNano   // 128 B
	BufferSizeMicro  = iobuf.BufferSizeMicro  // 512 B
	BufferSizeSmall  = iobuf.BufferSizeSmall  // 2 KiB
	BufferSizeMedium = iobuf.BufferSizeMedium // 8 KiB
	BufferSizeBig    = iobuf.BufferSizeBig    // 32 KiB
	BufferSizeLarge  = iobuf.BufferSizeLarge  // 128 KiB
	BufferSizeGreat  = iobuf.BufferSizeGreat  // 512 KiB
	BufferSizeHuge   = iobuf.BufferSizeHuge   // 2 MiB
	BufferSizeVast   = iobuf.BufferSizeVast   // 8 MiB
	BufferSizeGiant  = iobuf.BufferSizeGiant  // 32 MiB
	BufferSizeTitan  = iobuf.BufferSizeTitan  // 128 MiB
)

// Default configuration constants.
// Default uses 128 MiB for registered buffers; configure RLIMIT_MEMLOCK accordingly.
const (
	bufferSizeDefault        = BufferSizeMedium
	registerBufferSize       = BufferSizeLarge // 128 KiB, aligned with iobuf.RegisterBuffer
	registerBufferNum        = 1024            // 1024 buffers × 128 KiB = 128 MiB
	registerBufferDefaultMem = 1 << 27         // 128 MiB default
	defaultBacklog           = 1024
)

// RegisterBuffer is a fixed-size buffer for registered I/O.
type RegisterBuffer [registerBufferSize]byte

// RegisterBufferPool is a pool of RegisterBuffers.
type RegisterBufferPool struct {
	items []RegisterBuffer
	idx   int
}

// NewRegisterBufferPool creates a new RegisterBufferPool.
func NewRegisterBufferPool(n int) *RegisterBufferPool {
	return &RegisterBufferPool{
		items: make([]RegisterBuffer, n),
		idx:   0,
	}
}

// Fill fills the pool with buffers.
func (p *RegisterBufferPool) Fill(fn func() RegisterBuffer) {
	for i := range p.items {
		p.items[i] = fn()
	}
}

// Get gets a buffer from the pool.
func (p *RegisterBufferPool) Get() (int, []byte) {
	if p.idx >= len(p.items) {
		return -1, nil
	}
	idx := p.idx
	p.idx++
	return idx, p.items[idx][:]
}

// Put returns a buffer to the pool.
func (p *RegisterBufferPool) Put(idx int) {
	// Darwin pool is simple, no actual return logic needed
}

// AlignedMemBlock returns a page-aligned memory block.
func AlignedMemBlock() []byte {
	return iobuf.AlignedMem(int(iobuf.PageSize), iobuf.PageSize)
}

// sliceOfMicroArray creates a slice of micro-sized arrays.
func sliceOfMicroArray(s []byte, offset, n int) [][BufferSizeMicro]byte {
	arr := (*[1 << 20][BufferSizeMicro]byte)(unsafe.Pointer(&s[offset]))
	return arr[:n]
}

// newUnixSocketPair creates a pair of connected Unix domain sockets.
func newUnixSocketPair() ([2]int32, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return [2]int32{-1, -1}, err
	}
	return [2]int32{int32(fds[0]), int32(fds[1])}, nil
}

// maximizeMemoryLock is a no-op on Darwin (no RLIMIT_MEMLOCK support needed).
func maximizeMemoryLock() {}
