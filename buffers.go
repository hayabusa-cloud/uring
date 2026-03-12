// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
	"unsafe"

	"code.hybscloud.com/iobuf"
	"code.hybscloud.com/zcall"
)

const resourceLimitMemlock = 8

type resourceLimit struct {
	Cur uint64
	Max uint64
}

// Buffer size constants re-exported from iobuf for API compatibility.
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
	registerBufferDefaultMem = 1 << 26 // 64 MiB default
	bufferSizeDefault        = BufferSizeMedium
	defaultBacklog           = 1024
	registerBufferSize       = BufferSizeLarge // 128 KiB, aligned with iobuf.RegisterBuffer
	registerBufferNum        = 512             // 512 buffers × 128 KiB = 64 MiB
)

// Buffer types re-exported from iobuf.
type (
	PicoBuffer     = iobuf.PicoBuffer
	NanoBuffer     = iobuf.NanoBuffer
	MicroBuffer    = iobuf.MicroBuffer
	SmallBuffer    = iobuf.SmallBuffer
	MediumBuffer   = iobuf.MediumBuffer
	BigBuffer      = iobuf.BigBuffer
	LargeBuffer    = iobuf.LargeBuffer
	GreatBuffer    = iobuf.GreatBuffer
	HugeBuffer     = iobuf.HugeBuffer
	VastBuffer     = iobuf.VastBuffer
	GiantBuffer    = iobuf.GiantBuffer
	TitanBuffer    = iobuf.TitanBuffer
	RegisterBuffer = [registerBufferSize]byte
)

// sliceOf* functions for buffer ring provisioning.
func sliceOfPicoArray(mem []byte, start, count int) []PicoBuffer {
	return unsafe.Slice((*PicoBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfNanoArray(mem []byte, start, count int) []NanoBuffer {
	return unsafe.Slice((*NanoBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfMicroArray(mem []byte, start, count int) []MicroBuffer {
	return unsafe.Slice((*MicroBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfSmallArray(mem []byte, start, count int) []SmallBuffer {
	return unsafe.Slice((*SmallBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfMediumArray(mem []byte, start, count int) []MediumBuffer {
	return unsafe.Slice((*MediumBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfBigArray(mem []byte, start, count int) []BigBuffer {
	return unsafe.Slice((*BigBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfLargeArray(mem []byte, start, count int) []LargeBuffer {
	return unsafe.Slice((*LargeBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfGreatArray(mem []byte, start, count int) []GreatBuffer {
	return unsafe.Slice((*GreatBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfHugeArray(mem []byte, start, count int) []HugeBuffer {
	return unsafe.Slice((*HugeBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfVastArray(mem []byte, start, count int) []VastBuffer {
	return unsafe.Slice((*VastBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfGiantArray(mem []byte, start, count int) []GiantBuffer {
	return unsafe.Slice((*GiantBuffer)(unsafe.Pointer(&mem[start])), count)
}

func sliceOfTitanArray(mem []byte, start, count int) []TitanBuffer {
	return unsafe.Slice((*TitanBuffer)(unsafe.Pointer(&mem[start])), count)
}

// RegisterBufferPool is a pool of registered buffers for io_uring.
// Uses 4KB buffers optimized for page-aligned I/O operations.
type RegisterBufferPool struct {
	items []RegisterBuffer
}

// NewRegisterBufferPool creates a new buffer pool with the given capacity.
func NewRegisterBufferPool(capacity int) *RegisterBufferPool {
	return &RegisterBufferPool{
		items: make([]RegisterBuffer, capacity),
	}
}

// Fill populates the pool using the provided factory function.
func (p *RegisterBufferPool) Fill(factory func() RegisterBuffer) {
	for i := range p.items {
		p.items[i] = factory()
	}
}

// maximizeMemoryLock attempts to maximize the memory lock limit.
// On Linux, this is best-effort only; the hard limit must still be
// configured at the system level via ulimit or /etc/security/limits.conf.
func maximizeMemoryLock() {
	var rlim resourceLimit
	_, errno := zcall.Syscall6(
		zcall.SYS_PRLIMIT64,
		0,
		resourceLimitMemlock,
		0,
		uintptr(unsafe.Pointer(&rlim)),
		0,
		0,
	)
	if errno != 0 {
		return
	}
	if rlim.Cur >= rlim.Max {
		return
	}
	rlim.Cur = rlim.Max
	_, errno = zcall.Syscall6(
		zcall.SYS_PRLIMIT64,
		0,
		resourceLimitMemlock,
		uintptr(unsafe.Pointer(&rlim)),
		0,
		0,
		0,
	)
	if errno != 0 {
		return
	}
}

// AlignedMemBlock returns a page-aligned memory block.
func AlignedMemBlock() []byte {
	return iobuf.AlignedMem(int(iobuf.PageSize), iobuf.PageSize)
}

// newUnixSocketPair creates a pair of connected Unix domain sockets.
func newUnixSocketPair() ([2]int, error) {
	var fds [2]int32
	errno := zcall.Socketpair(zcall.AF_UNIX, zcall.SOCK_STREAM|zcall.SOCK_CLOEXEC, 0, &fds)
	if errno != 0 {
		return [2]int{}, errFromErrno(errno)
	}
	return [2]int{int(fds[0]), int(fds[1])}, nil
}
