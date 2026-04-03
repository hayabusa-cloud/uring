// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"unsafe"

	"code.hybscloud.com/iobuf"
)

// uringProvideBuffers manages kernel-provided buffers for buffer selection.
type uringProvideBuffers struct {
	mem       []byte
	size      int
	n         int
	gidOffset uint16
	gMask     int
	ptr       unsafe.Pointer
}

func newUringProvideBuffers(size, n int) *uringProvideBuffers {
	if size < 1 || n < 1 {
		panic("size and n must be positive")
	}
	n = roundToPowerOf2(n)

	return &uringProvideBuffers{
		size:  size,
		n:     n,
		gMask: n - 1,
	}
}

func (g *uringProvideBuffers) setGIDOffset(offset int) {
	g.gidOffset = uint16(offset)
}

func (g *uringProvideBuffers) register(ur *ioUring, flags uint8) error {
	g.mem = iobuf.AlignedMem(g.size*g.n, iobuf.PageSize)
	g.ptr = unsafe.Pointer(unsafe.SliceData(g.mem))

	sqeCtx := PackDirect(0, flags, g.gidOffset, 0)
	err := ur.provideBuffers(sqeCtx, g.n, 0, g.ptr, g.size, g.gidOffset)
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
	return uint16(int(f.Fd())&g.gMask) + g.gidOffset
}

func (g *uringProvideBuffers) buf(bufGroup bufferGroupIndex, bufIndex uint32) []byte {
	i := (int(bufGroup)-int(g.gidOffset))*g.n + int(bufIndex)
	if i < 0 || i >= g.n {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Add(g.ptr, g.size*i)), g.size)
}

func (g *uringProvideBuffers) data(f PollFd, bufIndex uint32, length int) []byte {
	i := (int(f.Fd())&g.gMask)*g.n + int(bufIndex)
	return unsafe.Slice((*byte)(unsafe.Add(g.ptr, g.size*i)), length)
}

// uringProvideBufferGroups manages multiple buffer groups with different sizes.
type uringProvideBufferGroups struct {
	scale     int
	scaleMask int
	cfg       BufferGroupsConfig
	gidOffset uint16
	groups    []*uringProvideBuffers
}

func newUringBufferGroups(scale int) *uringProvideBufferGroups {
	return newUringBufferGroupsWithConfig(scale, DefaultBufferGroupsConfig())
}

func newUringBufferGroupsWithConfig(scale int, cfg BufferGroupsConfig) *uringProvideBufferGroups {
	if scale < 1 || scale > (1<<12) {
		panic("scale must be between 1 and 4096")
	}
	scale = roundToPowerOf2(scale)
	if cfg.PicoNum > 0 {
		cfg.PicoNum = roundToPowerOf2(cfg.PicoNum)
	}
	if cfg.NanoNum > 0 {
		cfg.NanoNum = roundToPowerOf2(cfg.NanoNum)
	}
	if cfg.MicroNum > 0 {
		cfg.MicroNum = roundToPowerOf2(cfg.MicroNum)
	}
	if cfg.SmallNum > 0 {
		cfg.SmallNum = roundToPowerOf2(cfg.SmallNum)
	}
	if cfg.MediumNum > 0 {
		cfg.MediumNum = roundToPowerOf2(cfg.MediumNum)
	}
	if cfg.BigNum > 0 {
		cfg.BigNum = roundToPowerOf2(cfg.BigNum)
	}
	if cfg.LargeNum > 0 {
		cfg.LargeNum = roundToPowerOf2(cfg.LargeNum)
	}
	if cfg.GreatNum > 0 {
		cfg.GreatNum = roundToPowerOf2(cfg.GreatNum)
	}
	if cfg.HugeNum > 0 {
		cfg.HugeNum = roundToPowerOf2(cfg.HugeNum)
	}
	if cfg.VastNum > 0 {
		cfg.VastNum = roundToPowerOf2(cfg.VastNum)
	}
	if cfg.GiantNum > 0 {
		cfg.GiantNum = roundToPowerOf2(cfg.GiantNum)
	}
	if cfg.TitanNum > 0 {
		cfg.TitanNum = roundToPowerOf2(cfg.TitanNum)
	}

	g := &uringProvideBufferGroups{
		scale:     scale,
		scaleMask: scale - 1,
		cfg:       cfg,
		groups:    make([]*uringProvideBuffers, scale*int(bufferGroupIndexEnd)),
	}
	g.initTier(bufferGroupIndexPico, BufferSizePico, cfg.PicoNum)
	g.initTier(bufferGroupIndexNano, BufferSizeNano, cfg.NanoNum)
	g.initTier(bufferGroupIndexMicro, BufferSizeMicro, cfg.MicroNum)
	g.initTier(bufferGroupIndexSmall, BufferSizeSmall, cfg.SmallNum)
	g.initTier(bufferGroupIndexMedium, BufferSizeMedium, cfg.MediumNum)
	g.initTier(bufferGroupIndexBig, BufferSizeBig, cfg.BigNum)
	g.initTier(bufferGroupIndexLarge, BufferSizeLarge, cfg.LargeNum)
	g.initTier(bufferGroupIndexGreat, BufferSizeGreat, cfg.GreatNum)
	g.initTier(bufferGroupIndexHuge, BufferSizeHuge, cfg.HugeNum)
	g.initTier(bufferGroupIndexVast, BufferSizeVast, cfg.VastNum)
	g.initTier(bufferGroupIndexGiant, BufferSizeGiant, cfg.GiantNum)
	g.initTier(bufferGroupIndexTitan, BufferSizeTitan, cfg.TitanNum)
	return g
}

func (g *uringProvideBufferGroups) setGIDOffset(offset int) {
	g.gidOffset = uint16(offset)
	for i, group := range g.groups {
		if group != nil {
			group.setGIDOffset(offset + i)
		}
	}
}

func (g *uringProvideBufferGroups) register(ur *ioUring, flags uint8) error {
	for _, group := range g.groups {
		if group == nil {
			continue
		}
		if err := group.register(ur, flags); err != nil {
			return err
		}
	}
	return nil
}

func (g *uringProvideBufferGroups) bufGroupBySize(f PollFd, size int) uint16 {
	offset := uint16(f.Fd() & g.scaleMask)
	switch {
	case size <= BufferSizePico && g.cfg.PicoNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexPico + g.gidOffset
	case size <= BufferSizeNano && g.cfg.NanoNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexNano + g.gidOffset
	case size <= BufferSizeMicro && g.cfg.MicroNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexMicro + g.gidOffset
	case size <= BufferSizeSmall && g.cfg.SmallNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexSmall + g.gidOffset
	case size <= BufferSizeMedium && g.cfg.MediumNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexMedium + g.gidOffset
	case size <= BufferSizeBig && g.cfg.BigNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexBig + g.gidOffset
	case size <= BufferSizeLarge && g.cfg.LargeNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexLarge + g.gidOffset
	case size <= BufferSizeGreat && g.cfg.GreatNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexGreat + g.gidOffset
	case size <= BufferSizeHuge && g.cfg.HugeNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexHuge + g.gidOffset
	case size <= BufferSizeVast && g.cfg.VastNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexVast + g.gidOffset
	case size <= BufferSizeGiant && g.cfg.GiantNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexGiant + g.gidOffset
	case size <= BufferSizeTitan && g.cfg.TitanNum > 0:
		return bufferGroupIndexEnd*offset + bufferGroupIndexTitan + g.gidOffset
	}
	maxTier := g.maxEnabledTierIndex()
	if maxTier >= 0 {
		return bufferGroupIndexEnd*offset + uint16(maxTier) + g.gidOffset
	}
	return bufferGroupIndexEnd*(offset+1) + g.gidOffset
}

func (g *uringProvideBufferGroups) buf(bufGroup bufferGroupIndex, bufIndex uint32) []byte {
	if uint16(bufGroup) < g.gidOffset {
		return nil
	}
	rel := int(uint16(bufGroup) - g.gidOffset)
	offset := rel / int(bufferGroupIndexEnd)
	tier := rel % int(bufferGroupIndexEnd)
	if offset < 0 || offset >= g.scale {
		return nil
	}
	group := g.groups[offset*int(bufferGroupIndexEnd)+tier]
	if group == nil {
		return nil
	}
	return group.buf(bufGroup, bufIndex)
}

func (g *uringProvideBufferGroups) initTier(tier bufferGroupIndex, size, count int) {
	if count == 0 {
		return
	}
	for i := range g.scale {
		g.groups[i*int(bufferGroupIndexEnd)+int(tier)] = newUringProvideBuffers(size, count)
	}
}

func (g *uringProvideBufferGroups) bufGroup(f PollFd) uint16 {
	offset := uint16(f.Fd() & g.scaleMask)
	return bufferGroupIndexEnd*offset + bufferGroupIndexMedium + g.gidOffset
}

func (g *uringProvideBufferGroups) maxEnabledTierIndex() int {
	if g.cfg.TitanNum > 0 {
		return bufferGroupIndexTitan
	}
	if g.cfg.GiantNum > 0 {
		return bufferGroupIndexGiant
	}
	if g.cfg.VastNum > 0 {
		return bufferGroupIndexVast
	}
	if g.cfg.HugeNum > 0 {
		return bufferGroupIndexHuge
	}
	if g.cfg.GreatNum > 0 {
		return bufferGroupIndexGreat
	}
	if g.cfg.LargeNum > 0 {
		return bufferGroupIndexLarge
	}
	if g.cfg.BigNum > 0 {
		return bufferGroupIndexBig
	}
	if g.cfg.MediumNum > 0 {
		return bufferGroupIndexMedium
	}
	if g.cfg.SmallNum > 0 {
		return bufferGroupIndexSmall
	}
	if g.cfg.MicroNum > 0 {
		return bufferGroupIndexMicro
	}
	if g.cfg.NanoNum > 0 {
		return bufferGroupIndexNano
	}
	if g.cfg.PicoNum > 0 {
		return bufferGroupIndexPico
	}
	return -1
}

func (g *uringProvideBufferGroups) bufGroupByData(f PollFd, b []byte) uint16 {
	if b == nil {
		return g.bufGroup(f)
	}
	return g.bufGroupBySize(f, len(b))
}
