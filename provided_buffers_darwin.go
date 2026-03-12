// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"unsafe"

	"code.hybscloud.com/iobuf"
)

// bufferGroupIndex is an index into the buffer group registry.
type bufferGroupIndex uint16

// uringProvideBuffers manages kernel-provided buffers for buffer selection.
type uringProvideBuffers struct {
	size      int
	n         int
	gidOffset int
	gMask     int
	ptr       unsafe.Pointer
}

func newUringProvideBuffers(size, n int) *uringProvideBuffers {
	if size < 1 || n < 1 {
		panic("size and n must be positive")
	}
	// Round up n to power of 2
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n++

	return &uringProvideBuffers{
		size:  size,
		n:     n,
		gMask: n - 1,
	}
}

func (g *uringProvideBuffers) setGIDOffset(offset int) {
	g.gidOffset = offset
}

func (g *uringProvideBuffers) register(ur *ioUring, flags uint8) error {
	mem := iobuf.AlignedMem(g.size*g.n, iobuf.PageSize)
	g.ptr = unsafe.Pointer(&mem[0])

	gid := uint16(g.gidOffset)
	sqeCtx := PackDirect(0, flags, gid, 0)
	err := ur.provideBuffers(sqeCtx, g.n, 0, g.ptr, g.size, gid)
	if err != nil {
		return err
	}
	return nil
}

func (g *uringProvideBuffers) provide(ur *ioUring, flags uint8, group, index uint16, n int) error {
	offset := int(index) * g.size
	ptr := unsafe.Add(g.ptr, offset)
	sqeCtx := PackDirect(0, flags, group, 0)
	err := ur.provideBuffers(sqeCtx, 1, int(index), ptr, n, group)
	if err != nil {
		return err
	}
	return nil
}

func (g *uringProvideBuffers) bufGroup(f PollFd) uint16 {
	return uint16(int(f.Fd())&g.gMask) + uint16(g.gidOffset)
}

func (g *uringProvideBuffers) buf(bufGroup bufferGroupIndex, bufIndex uint32) []byte {
	i := (int(bufGroup)-g.gidOffset)*g.n + int(bufIndex)
	return unsafe.Slice((*byte)(unsafe.Add(g.ptr, g.size*i)), g.size)
}

func (g *uringProvideBuffers) data(f PollFd, bufIndex uint32, length int) []byte {
	i := (int(f.Fd())&g.gMask)*g.n + int(bufIndex)
	return unsafe.Slice((*byte)(unsafe.Add(g.ptr, g.size*i)), length)
}

// uringProvideBufferGroups manages multiple buffer groups with different sizes.
type uringProvideBufferGroups struct {
	groups    []*uringProvideBuffers
	gidOffset int
	gMask     int
}

func newUringBufferGroups(scale int) *uringProvideBufferGroups {
	if scale < 1 || scale > (1<<12) {
		panic("scale must be between 1 and 4096")
	}
	mask := scale - 1
	mask |= mask >> 1
	mask |= mask >> 2
	mask |= mask >> 4
	mask |= mask >> 8
	mask++
	mask--

	return &uringProvideBufferGroups{
		groups: make([]*uringProvideBuffers, 0),
		gMask:  mask,
	}
}

func (g *uringProvideBufferGroups) setGIDOffset(offset int) {
	g.gidOffset = offset
}

func (g *uringProvideBufferGroups) register(ur *ioUring, flags uint8) error {
	for _, group := range g.groups {
		err := group.register(ur, flags)
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *uringProvideBufferGroups) bufGroupBySize(f PollFd, size int) uint16 {
	for i, group := range g.groups {
		if group.size >= size {
			return uint16(int(f.Fd())&g.gMask) + uint16(g.gidOffset) + uint16(i)
		}
	}
	if len(g.groups) > 0 {
		return uint16(int(f.Fd())&g.gMask) + uint16(g.gidOffset) + uint16(len(g.groups)-1)
	}
	return uint16(g.gidOffset)
}

func (g *uringProvideBufferGroups) buf(bufGroup bufferGroupIndex, bufIndex uint32) []byte {
	idx := int(bufGroup) - g.gidOffset
	if idx < 0 || idx >= len(g.groups) {
		return nil
	}
	return g.groups[idx].buf(bufGroup, bufIndex)
}
