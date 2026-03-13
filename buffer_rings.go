// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/dwcas"
	"code.hybscloud.com/spin"
)

func newUringBufferRings() *uringBufferRings {
	ret := uringBufferRings{
		rings:    make([]*ioUringBufRing, 0),
		backings: make([][]byte, 0),
		counts:   make([]uintptr, 0),
		masks:    make([]uintptr, 0),
		sizes:    make([]int, 0),
		buffers:  make([][]byte, 0),
	}
	return &ret
}

// uringBufferRings manages buffer ring registration and tail advancement.
//
// Synchronization model:
//   - provide() uses FAA for concurrent producer offset allocation
//   - advance() uses spinlock for batch tail update + count reset
//   - br.tail is read by provide() and written by advance()
//   - Caller contract: provide() and advance() must not be concurrent
//
// Memory ordering:
//   - BarrierRelease before tail write ensures buffer data visible to kernel
//   - Kernel uses smp_load_acquire to read tail
type uringBufferRings struct {
	rings    []*ioUringBufRing
	backings [][]byte // keeps backing memory alive to prevent GC
	locks    []spin.Lock
	counts   []uintptr
	masks    []uintptr
	sizes    []int    // per-group buffer size
	buffers  [][]byte // per-group buffer memory (GC-safe)
}

func (rings *uringBufferRings) appendGroup(br *ioUringBufRing, n int) {
	rings.rings = append(rings.rings, br)
	rings.locks = append(rings.locks, spin.Lock{})
	rings.counts = append(rings.counts, uintptr(n))
	rings.masks = append(rings.masks, uintptr(n-1))
}

func (rings *uringBufferRings) registerBuffers(ur *ioUring, b *uringProvideBuffers) error {
	for i := range b.gn {
		gid := uint16(i) + b.gidOffset
		buf := b.mem[b.size*b.n*i : b.size*b.n*(i+1)]
		br, err := rings.registerGroup(ur, b.n, gid, buf, b.size)
		if err != nil {
			return err
		}
		rings.appendGroup(br, b.n)
	}
	return nil
}

func (rings *uringBufferRings) registerGroups(ur *ioUring, g *uringProvideBufferGroups) error {
	for i := range g.scale {
		if g.cfg.PicoNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexPico + g.gidOffset
			buf := g.picoMem[BufferSizePico*g.cfg.PicoNum*i : BufferSizePico*g.cfg.PicoNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.PicoNum, gid, buf, BufferSizePico)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.PicoNum)
		}

		if g.cfg.NanoNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexNano + g.gidOffset
			buf := g.nanoMem[BufferSizeNano*g.cfg.NanoNum*i : BufferSizeNano*g.cfg.NanoNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.NanoNum, gid, buf, BufferSizeNano)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.NanoNum)
		}

		if g.cfg.MicroNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexMicro + g.gidOffset
			buf := g.microMem[BufferSizeMicro*g.cfg.MicroNum*i : BufferSizeMicro*g.cfg.MicroNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.MicroNum, gid, buf, BufferSizeMicro)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.MicroNum)
		}

		if g.cfg.SmallNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexSmall + g.gidOffset
			buf := g.smallMem[BufferSizeSmall*g.cfg.SmallNum*i : BufferSizeSmall*g.cfg.SmallNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.SmallNum, gid, buf, BufferSizeSmall)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.SmallNum)
		}

		if g.cfg.MediumNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexMedium + g.gidOffset
			buf := g.mediumMem[BufferSizeMedium*g.cfg.MediumNum*i : BufferSizeMedium*g.cfg.MediumNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.MediumNum, gid, buf, BufferSizeMedium)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.MediumNum)
		}

		if g.cfg.BigNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexBig + g.gidOffset
			buf := g.bigMem[BufferSizeBig*g.cfg.BigNum*i : BufferSizeBig*g.cfg.BigNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.BigNum, gid, buf, BufferSizeBig)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.BigNum)
		}

		if g.cfg.LargeNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexLarge + g.gidOffset
			buf := g.largeMem[BufferSizeLarge*g.cfg.LargeNum*i : BufferSizeLarge*g.cfg.LargeNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.LargeNum, gid, buf, BufferSizeLarge)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.LargeNum)
		}

		if g.cfg.GreatNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexGreat + g.gidOffset
			buf := g.greatMem[BufferSizeGreat*g.cfg.GreatNum*i : BufferSizeGreat*g.cfg.GreatNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.GreatNum, gid, buf, BufferSizeGreat)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.GreatNum)
		}

		if g.cfg.HugeNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexHuge + g.gidOffset
			buf := g.hugeMem[BufferSizeHuge*g.cfg.HugeNum*i : BufferSizeHuge*g.cfg.HugeNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.HugeNum, gid, buf, BufferSizeHuge)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.HugeNum)
		}

		if g.cfg.VastNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexVast + g.gidOffset
			buf := g.vastMem[BufferSizeVast*g.cfg.VastNum*i : BufferSizeVast*g.cfg.VastNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.VastNum, gid, buf, BufferSizeVast)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.VastNum)
		}

		if g.cfg.GiantNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexGiant + g.gidOffset
			buf := g.giantMem[BufferSizeGiant*g.cfg.GiantNum*i : BufferSizeGiant*g.cfg.GiantNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.GiantNum, gid, buf, BufferSizeGiant)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.GiantNum)
		}

		if g.cfg.TitanNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexTitan + g.gidOffset
			buf := g.titanMem[BufferSizeTitan*g.cfg.TitanNum*i : BufferSizeTitan*g.cfg.TitanNum*(i+1)]
			br, err := rings.registerGroup(ur, g.cfg.TitanNum, gid, buf, BufferSizeTitan)
			if err != nil {
				return err
			}
			rings.appendGroup(br, g.cfg.TitanNum)
		}
	}
	return nil
}

func (rings *uringBufferRings) registerGroup(ur *ioUring, entries int, gid uint16, buf []byte, size int) (*ioUringBufRing, error) {
	r, backing, err := ur.registerBufRing(entries, gid)
	if err != nil {
		return nil, err
	}
	// Store backing slice to prevent GC (nil for mmap mode)
	rings.backings = append(rings.backings, backing)
	rings.sizes = append(rings.sizes, size)
	rings.buffers = append(rings.buffers, buf)
	ur.bufRingInit(r)
	mask := entries - 1
	base := uintptr(unsafe.Pointer(unsafe.SliceData(buf)))
	for i := range entries {
		ur.bufRingAdd(r, base+uintptr(i*size), size, uint16(i), uintptr(mask), uintptr(i))
	}
	return r, nil
}

// provide adds a buffer to the ring at the next available slot.
// Uses FAA to get unique offset, allowing concurrent provide() calls.
//
// Caller contract: Must not be called concurrently with advance().
// The br.tail field is read without synchronization for slot calculation.
func (rings *uringBufferRings) provide(ur *ioUring, gidOffset, group uint16, addr unsafe.Pointer, size int, index uint32) {
	br := rings.rings[group-gidOffset]
	offset := atomic.AddUintptr(&rings.counts[group-gidOffset], 1)
	ur.bufRingAdd(br, uintptr(addr), size, uint16(index), rings.masks[group-gidOffset], offset-1)
}

// advance commits pending buffers by updating the kernel-visible tail.
// Batches all provides since last advance, then resets count.
//
// Caller contract: Must not be called concurrently with provide().
// Writes br.tail after BarrierRelease to ensure buffer data visibility.
func (rings *uringBufferRings) advance(ur *ioUring) {
	for i, r := range rings.rings {
		if r == nil {
			break
		}
		rings.locks[i].Lock()
		// Release barrier ensures buffer writes are visible to kernel before tail update
		dwcas.BarrierRelease()
		ur.bufRingAdvance(r, int(rings.counts[i]))
		rings.counts[i] = 0
		rings.locks[i].Unlock()
	}
}

// bundleIterator constructs a BundleIterator from a CQE and the internal index
// of the buffer ring group. The index is the position in the rings slice
// (i.e., group - gidOffset). gidOffset and group are captured in the
// iterator for use by Recycle.
func (rings *uringBufferRings) bundleIterator(cqe CQEView, index int, gidOffset, group uint16) *BundleIterator {
	if index < 0 || index >= len(rings.rings) {
		return nil
	}
	it := newBundleIterator(
		cqe,
		unsafe.Pointer(unsafe.SliceData(rings.buffers[index])),
		rings.sizes[index],
		uint16(rings.masks[index]),
	)
	if it != nil {
		it.gidOffset = gidOffset
		it.group = group
	}
	return it
}
