// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"math"
	"unsafe"

	"code.hybscloud.com/iobuf"
)

// bufferGroupIndex identifies a tier in the 12-tier buffer system.
type bufferGroupIndex uint16

// Buffer group tier indices.
//
// The 12-tier buffer system uses a power-of-4 size progression:
//
//	Tier    Name    Size        Use Case
//	0       Pico    32 B        Tiny metadata, headers
//	1       Nano    128 B       Small protocol frames
//	2       Micro   512 B       Standard protocol messages
//	3       Small   2 KiB       Typical HTTP requests
//	4       Medium  8 KiB       Page-sized operations
//	5       Big     32 KiB      Large messages
//	6       Large   128 KiB     Bulk transfers
//	7       Great   512 KiB     Large file chunks
//	8       Huge    2 MiB       Very large buffers
//	9       Vast    8 MiB       Streaming media
//	10      Giant   32 MiB      Maximum standard
//	11      Titan   128 MiB     Extreme cases
const (
	bufferGroupIndexPico = iota
	bufferGroupIndexNano
	bufferGroupIndexMicro
	bufferGroupIndexSmall
	bufferGroupIndexMedium
	bufferGroupIndexBig
	bufferGroupIndexLarge
	bufferGroupIndexGreat
	bufferGroupIndexHuge
	bufferGroupIndexVast
	bufferGroupIndexGiant
	bufferGroupIndexTitan
	bufferGroupIndexEnd
)

// Default buffer counts per tier.
// Smaller buffers have more instances to handle high-frequency small I/O.
// Larger buffers have fewer instances due to memory constraints.
const (
	DefaultBufferNumPico   = 1 << 15 // 32768 × 32 B = 1 MiB
	DefaultBufferNumNano   = 1 << 14 // 16384 × 128 B = 2 MiB
	DefaultBufferNumMicro  = 1 << 13 // 8192 × 512 B = 4 MiB
	DefaultBufferNumSmall  = 1 << 12 // 4096 × 2 KiB = 8 MiB
	DefaultBufferNumMedium = 1 << 11 // 2048 × 8 KiB = 16 MiB
	DefaultBufferNumBig    = 1 << 10 // 1024 × 32 KiB = 32 MiB
	DefaultBufferNumLarge  = 1 << 9  // 512 × 128 KiB = 64 MiB
	DefaultBufferNumGreat  = 1 << 8  // 256 × 512 KiB = 128 MiB
	DefaultBufferNumHuge   = 1 << 7  // 128 × 2 MiB = 256 MiB
	DefaultBufferNumVast   = 1 << 6  // 64 × 8 MiB = 512 MiB
	DefaultBufferNumGiant  = 1 << 5  // 32 × 32 MiB = 1 GiB
	DefaultBufferNumTitan  = 1 << 4  // 16 × 128 MiB = 2 GiB
)

// BufferGroupsConfig configures buffer counts for each tier.
//
// Each field specifies the number of buffers to allocate for that tier.
// A zero count disables the tier (no memory allocated).
//
// Memory usage calculation:
//
//	Total = Sum(TierSize × TierCount × Scale)
//
// Example with default config (Scale=1):
//
//	Pico:   32768 × 32 B   = 1 MiB
//	Nano:   16384 × 128 B  = 2 MiB
//	Micro:  8192 × 512 B   = 4 MiB
//	Small:  4096 × 2 KiB   = 8 MiB
//	Medium: 2048 × 8 KiB   = 16 MiB
//	Big:    1024 × 32 KiB  = 32 MiB
//	Large:  512 × 128 KiB  = 64 MiB
//	                Total  ≈ 127 MiB per scale
type BufferGroupsConfig struct {
	PicoNum   int // 32 B buffers
	NanoNum   int // 128 B buffers
	MicroNum  int // 512 B buffers
	SmallNum  int // 2 KiB buffers
	MediumNum int // 8 KiB buffers
	BigNum    int // 32 KiB buffers
	LargeNum  int // 128 KiB buffers
	GreatNum  int // 512 KiB buffers
	HugeNum   int // 2 MiB buffers
	VastNum   int // 8 MiB buffers
	GiantNum  int // 32 MiB buffers
	TitanNum  int // 128 MiB buffers
}

// DefaultBufferGroupsConfig returns the default configuration.
// Enables first 8 tiers (Pico through Great), totaling ~256 MiB per scale.
// Suitable for servers with 512MB+ memory.
func DefaultBufferGroupsConfig() BufferGroupsConfig {
	return BufferGroupsConfig{
		PicoNum:   DefaultBufferNumPico,
		NanoNum:   DefaultBufferNumNano,
		MicroNum:  DefaultBufferNumMicro,
		SmallNum:  DefaultBufferNumSmall,
		MediumNum: DefaultBufferNumMedium,
		BigNum:    DefaultBufferNumBig,
		LargeNum:  DefaultBufferNumLarge,
		GreatNum:  0, // Disabled by default
		HugeNum:   0, // Disabled by default
		VastNum:   0, // Disabled by default
		GiantNum:  0, // Disabled by default
		TitanNum:  0, // Disabled by default
	}
}

// FullBufferGroupsConfig returns configuration with all 12 tiers enabled.
// Requires ~4 GiB per scale. Use for high-memory servers.
func FullBufferGroupsConfig() BufferGroupsConfig {
	return BufferGroupsConfig{
		PicoNum:   DefaultBufferNumPico,
		NanoNum:   DefaultBufferNumNano,
		MicroNum:  DefaultBufferNumMicro,
		SmallNum:  DefaultBufferNumSmall,
		MediumNum: DefaultBufferNumMedium,
		BigNum:    DefaultBufferNumBig,
		LargeNum:  DefaultBufferNumLarge,
		GreatNum:  DefaultBufferNumGreat,
		HugeNum:   DefaultBufferNumHuge,
		VastNum:   DefaultBufferNumVast,
		GiantNum:  DefaultBufferNumGiant,
		TitanNum:  DefaultBufferNumTitan,
	}
}

// MinimalBufferGroupsConfig returns a reduced configuration (~32 MiB per scale).
// Enables first 6 tiers (Pico through Big), totaling ~32 MiB per scale.
// Suitable for memory-constrained environments.
func MinimalBufferGroupsConfig() BufferGroupsConfig {
	return BufferGroupsConfig{
		PicoNum:   DefaultBufferNumPico,
		NanoNum:   DefaultBufferNumNano,
		MicroNum:  DefaultBufferNumMicro,
		SmallNum:  DefaultBufferNumSmall,
		MediumNum: DefaultBufferNumMedium,
	}
}

func newUringProvideBuffers(size, n int) *uringProvideBuffers {
	if size < 2 || size > (1<<28) {
		panic("size must be between 2 and 268435456")
	}
	if n < 2 || n > (1<<28) {
		panic("n must be between 2 and 268435456")
	}
	size--
	size |= size >> 1
	size |= size >> 2
	size |= size >> 4
	size |= size >> 8
	size |= size >> 16
	size++
	mask := n - 1
	mask |= mask >> 1
	mask |= mask >> 2
	mask |= mask >> 4
	mask |= mask >> 8
	mask |= mask >> 16
	n = mask + 1
	mask |= math.MaxInt16
	gn := 1
	if n > (1 << 15) {
		gn, n = n>>15, 1<<15
	}
	mem := iobuf.AlignedMem(int(size)*n*gn, iobuf.PageSize)
	ptr := unsafe.Pointer(unsafe.SliceData(mem))
	return &uringProvideBuffers{
		gn:    gn,
		n:     n,
		gMask: gn - 1,
		mask:  mask,
		mem:   mem,
		ptr:   ptr,
		size:  size,
	}
}

type uringProvideBuffers struct {
	gn        int
	n         int
	gMask     int
	mask      int
	mem       []byte
	ptr       unsafe.Pointer
	size      int
	gidOffset uint16
}

func (g *uringProvideBuffers) setGIDOffset(offset int) {
	g.gidOffset = uint16(offset)
}

func (g *uringProvideBuffers) register(ur *ioUring, flags uint8) error {
	for i := range g.gn {
		addr := unsafe.Add(g.ptr, g.size*g.n*i)
		gid := uint16(i) + g.gidOffset
		sqeCtx := PackDirect(0, flags, gid, 0)
		err := ur.provideBuffers(sqeCtx, g.n, 0, addr, g.size, gid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *uringProvideBuffers) provide(ur *ioUring, flags uint8, group uint16, data []byte, index uint32) error {
	ptr, n := unsafe.Pointer(unsafe.SliceData(data)), len(data)
	sqeCtx := PackDirect(0, flags, group, 0)
	err := ur.provideBuffers(sqeCtx, 1, int(index), ptr, n, group)
	if err != nil {
		return err
	}
	return nil
}

func (g *uringProvideBuffers) bufGroup(f PollFd) uint16 {
	return uint16(f.Fd()&g.gMask) + g.gidOffset
}

func (g *uringProvideBuffers) buf(bufGroup bufferGroupIndex, bufIndex uint32) []byte {
	i := (int(bufGroup)-int(g.gidOffset))*g.n + int(bufIndex)
	return unsafe.Slice((*byte)(unsafe.Add(g.ptr, g.size*i)), g.size)
}

func (g *uringProvideBuffers) data(f PollFd, bufIndex uint32, length int) []byte {
	i := (f.Fd()&g.gMask)*g.n + int(bufIndex)
	return unsafe.Slice((*byte)(unsafe.Add(g.ptr, g.size*i)), length)
}

// newUringBufferGroups creates buffer groups with default configuration.
func newUringBufferGroups(scale int) *uringProvideBufferGroups {
	return newUringBufferGroupsWithConfig(scale, DefaultBufferGroupsConfig())
}

// newUringBufferGroupsWithConfig creates buffer groups with custom configuration.
// Tiers with zero count are not allocated.
func newUringBufferGroupsWithConfig(scale int, cfg BufferGroupsConfig) *uringProvideBufferGroups {
	if scale < 1 || scale > (1<<12) {
		panic("scale must be between 1 and 4096")
	}
	mask := scale - 1
	mask |= mask >> 1
	mask |= mask >> 2
	mask |= mask >> 4
	mask |= mask >> 8
	scale = mask + 1

	g := &uringProvideBufferGroups{
		scale:     scale,
		scaleMask: mask,
		cfg:       cfg,
	}

	// Allocate only tiers with non-zero count
	if cfg.PicoNum > 0 {
		g.picoMem = iobuf.AlignedMem(BufferSizePico*cfg.PicoNum*scale, iobuf.PageSize)
		g.picoBuffers = sliceOfPicoArray(g.picoMem, 0, len(g.picoMem)/BufferSizePico)
	}
	if cfg.NanoNum > 0 {
		g.nanoMem = iobuf.AlignedMem(BufferSizeNano*cfg.NanoNum*scale, iobuf.PageSize)
		g.nanoBuffers = sliceOfNanoArray(g.nanoMem, 0, len(g.nanoMem)/BufferSizeNano)
	}
	if cfg.MicroNum > 0 {
		g.microMem = iobuf.AlignedMem(BufferSizeMicro*cfg.MicroNum*scale, iobuf.PageSize)
		g.microBuffers = sliceOfMicroArray(g.microMem, 0, len(g.microMem)/BufferSizeMicro)
	}
	if cfg.SmallNum > 0 {
		g.smallMem = iobuf.AlignedMem(BufferSizeSmall*cfg.SmallNum*scale, iobuf.PageSize)
		g.smallBuffers = sliceOfSmallArray(g.smallMem, 0, len(g.smallMem)/BufferSizeSmall)
	}
	if cfg.MediumNum > 0 {
		g.mediumMem = iobuf.AlignedMem(BufferSizeMedium*cfg.MediumNum*scale, iobuf.PageSize)
		g.mediumBuffers = sliceOfMediumArray(g.mediumMem, 0, len(g.mediumMem)/BufferSizeMedium)
	}
	if cfg.BigNum > 0 {
		g.bigMem = iobuf.AlignedMem(BufferSizeBig*cfg.BigNum*scale, iobuf.PageSize)
		g.bigBuffers = sliceOfBigArray(g.bigMem, 0, len(g.bigMem)/BufferSizeBig)
	}
	if cfg.LargeNum > 0 {
		g.largeMem = iobuf.AlignedMem(BufferSizeLarge*cfg.LargeNum*scale, iobuf.PageSize)
		g.largeBuffers = sliceOfLargeArray(g.largeMem, 0, len(g.largeMem)/BufferSizeLarge)
	}
	if cfg.GreatNum > 0 {
		g.greatMem = iobuf.AlignedMem(BufferSizeGreat*cfg.GreatNum*scale, iobuf.PageSize)
		g.greatBuffers = sliceOfGreatArray(g.greatMem, 0, len(g.greatMem)/BufferSizeGreat)
	}
	if cfg.HugeNum > 0 {
		g.hugeMem = iobuf.AlignedMem(BufferSizeHuge*cfg.HugeNum*scale, iobuf.PageSize)
		g.hugeBuffers = sliceOfHugeArray(g.hugeMem, 0, len(g.hugeMem)/BufferSizeHuge)
	}
	if cfg.VastNum > 0 {
		g.vastMem = iobuf.AlignedMem(BufferSizeVast*cfg.VastNum*scale, iobuf.PageSize)
		g.vastBuffers = sliceOfVastArray(g.vastMem, 0, len(g.vastMem)/BufferSizeVast)
	}
	if cfg.GiantNum > 0 {
		g.giantMem = iobuf.AlignedMem(BufferSizeGiant*cfg.GiantNum*scale, iobuf.PageSize)
		g.giantBuffers = sliceOfGiantArray(g.giantMem, 0, len(g.giantMem)/BufferSizeGiant)
	}
	if cfg.TitanNum > 0 {
		g.titanMem = iobuf.AlignedMem(BufferSizeTitan*cfg.TitanNum*scale, iobuf.PageSize)
		g.titanBuffers = sliceOfTitanArray(g.titanMem, 0, len(g.titanMem)/BufferSizeTitan)
	}

	return g
}

type uringProvideBufferGroups struct {
	scale     int
	scaleMask int
	cfg       BufferGroupsConfig

	picoMem   []byte
	nanoMem   []byte
	microMem  []byte
	smallMem  []byte
	mediumMem []byte
	bigMem    []byte
	largeMem  []byte
	greatMem  []byte
	hugeMem   []byte
	vastMem   []byte
	giantMem  []byte
	titanMem  []byte

	_             [0][1]byte
	picoBuffers   []PicoBuffer
	nanoBuffers   []NanoBuffer
	microBuffers  []MicroBuffer
	smallBuffers  []SmallBuffer
	mediumBuffers []MediumBuffer
	bigBuffers    []BigBuffer
	largeBuffers  []LargeBuffer
	greatBuffers  []GreatBuffer
	hugeBuffers   []HugeBuffer
	vastBuffers   []VastBuffer
	giantBuffers  []GiantBuffer
	titanBuffers  []TitanBuffer

	gidOffset uint16
}

func (g *uringProvideBufferGroups) setGIDOffset(offset int) {
	g.gidOffset = uint16(offset)
}

func (g *uringProvideBufferGroups) register(ur *ioUring, flags uint8) error {
	for i := range g.scale {
		if g.cfg.PicoNum > 0 {
			addr := unsafe.Pointer(&g.picoBuffers[i*g.cfg.PicoNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexPico) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.PicoNum, 0, addr, BufferSizePico, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.NanoNum > 0 {
			addr := unsafe.Pointer(&g.nanoBuffers[i*g.cfg.NanoNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexNano) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.NanoNum, 0, addr, BufferSizeNano, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.MicroNum > 0 {
			addr := unsafe.Pointer(&g.microBuffers[i*g.cfg.MicroNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexMicro) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.MicroNum, 0, addr, BufferSizeMicro, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.SmallNum > 0 {
			addr := unsafe.Pointer(&g.smallBuffers[i*g.cfg.SmallNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexSmall) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.SmallNum, 0, addr, BufferSizeSmall, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.MediumNum > 0 {
			addr := unsafe.Pointer(&g.mediumBuffers[i*g.cfg.MediumNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexMedium) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.MediumNum, 0, addr, BufferSizeMedium, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.BigNum > 0 {
			addr := unsafe.Pointer(&g.bigBuffers[i*g.cfg.BigNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexBig) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.BigNum, 0, addr, BufferSizeBig, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.LargeNum > 0 {
			addr := unsafe.Pointer(&g.largeBuffers[i*g.cfg.LargeNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexLarge) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.LargeNum, 0, addr, BufferSizeLarge, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.GreatNum > 0 {
			addr := unsafe.Pointer(&g.greatBuffers[i*g.cfg.GreatNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexGreat) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.GreatNum, 0, addr, BufferSizeGreat, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.HugeNum > 0 {
			addr := unsafe.Pointer(&g.hugeBuffers[i*g.cfg.HugeNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexHuge) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.HugeNum, 0, addr, BufferSizeHuge, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.VastNum > 0 {
			addr := unsafe.Pointer(&g.vastBuffers[i*g.cfg.VastNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexVast) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.VastNum, 0, addr, BufferSizeVast, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.GiantNum > 0 {
			addr := unsafe.Pointer(&g.giantBuffers[i*g.cfg.GiantNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexGiant) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.GiantNum, 0, addr, BufferSizeGiant, gid)
			if err != nil {
				return err
			}
		}
		if g.cfg.TitanNum > 0 {
			addr := unsafe.Pointer(&g.titanBuffers[i*g.cfg.TitanNum])
			gid := uint16(i*bufferGroupIndexEnd+bufferGroupIndexTitan) + g.gidOffset
			sqeCtx := PackDirect(0, flags, gid, 0)
			err := ur.provideBuffers(sqeCtx, g.cfg.TitanNum, 0, addr, BufferSizeTitan, gid)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *uringProvideBufferGroups) provide(ur *ioUring, flags uint8, group uint16, data []byte, index uint32) error {
	ptr, n := unsafe.Pointer(unsafe.SliceData(data)), len(data)
	sqeCtx := PackDirect(0, flags, group, 0)
	err := ur.provideBuffers(sqeCtx, 1, int(index), ptr, n, group)
	if err != nil {
		return err
	}
	return nil
}

func (g *uringProvideBufferGroups) bufGroup(f PollFd) uint16 {
	offset := uint16(f.Fd() & g.scaleMask)
	return bufferGroupIndexEnd*offset + bufferGroupIndexMedium + g.gidOffset
}

// maxEnabledTierIndex returns the highest enabled tier index.
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
	default:
		// Return the highest enabled tier
		maxTier := g.maxEnabledTierIndex()
		if maxTier >= 0 {
			return bufferGroupIndexEnd*offset + uint16(maxTier) + g.gidOffset
		}
		return bufferGroupIndexEnd*(offset+1) + g.gidOffset
	}
}

func (g *uringProvideBufferGroups) bufGroupByData(f PollFd, b []byte) uint16 {
	if b == nil {
		return g.bufGroup(f)
	}
	return g.bufGroupBySize(f, len(b))
}

func (g *uringProvideBufferGroups) buf(bufGroup bufferGroupIndex, bufIndex uint32) []byte {
	switch (uint16(bufGroup) - g.gidOffset) % bufferGroupIndexEnd {
	case bufferGroupIndexPico:
		if g.cfg.PicoNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.PicoNum) + bufIndex
		return g.picoBuffers[i][:]
	case bufferGroupIndexNano:
		if g.cfg.NanoNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.NanoNum) + bufIndex
		return g.nanoBuffers[i][:]
	case bufferGroupIndexMicro:
		if g.cfg.MicroNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.MicroNum) + bufIndex
		return g.microBuffers[i][:]
	case bufferGroupIndexSmall:
		if g.cfg.SmallNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.SmallNum) + bufIndex
		return g.smallBuffers[i][:]
	case bufferGroupIndexMedium:
		if g.cfg.MediumNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.MediumNum) + bufIndex
		return g.mediumBuffers[i][:]
	case bufferGroupIndexBig:
		if g.cfg.BigNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.BigNum) + bufIndex
		return g.bigBuffers[i][:]
	case bufferGroupIndexLarge:
		if g.cfg.LargeNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.LargeNum) + bufIndex
		return g.largeBuffers[i][:]
	case bufferGroupIndexGreat:
		if g.cfg.GreatNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.GreatNum) + bufIndex
		return g.greatBuffers[i][:]
	case bufferGroupIndexHuge:
		if g.cfg.HugeNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.HugeNum) + bufIndex
		return g.hugeBuffers[i][:]
	case bufferGroupIndexVast:
		if g.cfg.VastNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.VastNum) + bufIndex
		return g.vastBuffers[i][:]
	case bufferGroupIndexGiant:
		if g.cfg.GiantNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.GiantNum) + bufIndex
		return g.giantBuffers[i][:]
	case bufferGroupIndexTitan:
		if g.cfg.TitanNum == 0 {
			return nil
		}
		i := (uint32(bufGroup)-uint32(g.gidOffset))/bufferGroupIndexEnd*uint32(g.cfg.TitanNum) + bufIndex
		return g.titanBuffers[i][:]
	default:
		return nil
	}
}

func (g *uringProvideBufferGroups) picoData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.PicoNum == 0 {
		return nil
	}
	return g.picoBuffers[(f.Fd()&g.scaleMask)*g.cfg.PicoNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) nanoData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.NanoNum == 0 {
		return nil
	}
	return g.nanoBuffers[(f.Fd()&g.scaleMask)*g.cfg.NanoNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) microData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.MicroNum == 0 {
		return nil
	}
	return g.microBuffers[(f.Fd()&g.scaleMask)*g.cfg.MicroNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) smallData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.SmallNum == 0 {
		return nil
	}
	return g.smallBuffers[(f.Fd()&g.scaleMask)*g.cfg.SmallNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) mediumData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.MediumNum == 0 {
		return nil
	}
	return g.mediumBuffers[(f.Fd()&g.scaleMask)*g.cfg.MediumNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) bigData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.BigNum == 0 {
		return nil
	}
	return g.bigBuffers[(f.Fd()&g.scaleMask)*g.cfg.BigNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) largeData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.LargeNum == 0 {
		return nil
	}
	return g.largeBuffers[(f.Fd()&g.scaleMask)*g.cfg.LargeNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) greatData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.GreatNum == 0 {
		return nil
	}
	return g.greatBuffers[(f.Fd()&g.scaleMask)*g.cfg.GreatNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) hugeData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.HugeNum == 0 {
		return nil
	}
	return g.hugeBuffers[(f.Fd()&g.scaleMask)*g.cfg.HugeNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) vastData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.VastNum == 0 {
		return nil
	}
	return g.vastBuffers[(f.Fd()&g.scaleMask)*g.cfg.VastNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) giantData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.GiantNum == 0 {
		return nil
	}
	return g.giantBuffers[(f.Fd()&g.scaleMask)*g.cfg.GiantNum+int(bufIndex)][:length]
}

func (g *uringProvideBufferGroups) titanData(f PollFd, bufIndex uint32, length int) []byte {
	if g.cfg.TitanNum == 0 {
		return nil
	}
	return g.titanBuffers[(f.Fd()&g.scaleMask)*g.cfg.TitanNum+int(bufIndex)][:length]
}
