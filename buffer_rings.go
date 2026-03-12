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
}

func (rings *uringBufferRings) registerBuffers(ur *ioUring, b *uringProvideBuffers) error {
	for i := range b.gn {
		gid := uint16(i) + b.gidOffset
		ptr := unsafe.Add(b.ptr, b.size*b.n*i)
		br, err := rings.registerGroup(ur, b.n, gid, ptr, b.size)
		if err != nil {
			return err
		}
		rings.rings = append(rings.rings, br)
		rings.locks = append(rings.locks, spin.Lock{})
		rings.counts = append(rings.counts, uintptr(b.n))
		rings.masks = append(rings.masks, uintptr(b.n-1))
	}
	return nil
}

func (rings *uringBufferRings) registerGroups(ur *ioUring, g *uringProvideBufferGroups) error {
	for i := range g.scale {
		if g.cfg.PicoNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexPico + g.gidOffset
			ptr := unsafe.Pointer(&g.picoBuffers[i*g.cfg.PicoNum])
			br, err := rings.registerGroup(ur, g.cfg.PicoNum, gid, ptr, BufferSizePico)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.PicoNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.PicoNum-1))
		}

		if g.cfg.NanoNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexNano + g.gidOffset
			ptr := unsafe.Pointer(&g.nanoBuffers[i*g.cfg.NanoNum])
			br, err := rings.registerGroup(ur, g.cfg.NanoNum, gid, ptr, BufferSizeNano)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.NanoNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.NanoNum-1))
		}

		if g.cfg.MicroNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexMicro + g.gidOffset
			ptr := unsafe.Pointer(&g.microBuffers[i*g.cfg.MicroNum])
			br, err := rings.registerGroup(ur, g.cfg.MicroNum, gid, ptr, BufferSizeMicro)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.MicroNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.MicroNum-1))
		}

		if g.cfg.SmallNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexSmall + g.gidOffset
			ptr := unsafe.Pointer(&g.smallBuffers[i*g.cfg.SmallNum])
			br, err := rings.registerGroup(ur, g.cfg.SmallNum, gid, ptr, BufferSizeSmall)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.SmallNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.SmallNum-1))
		}

		if g.cfg.MediumNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexMedium + g.gidOffset
			ptr := unsafe.Pointer(&g.mediumBuffers[i*g.cfg.MediumNum])
			br, err := rings.registerGroup(ur, g.cfg.MediumNum, gid, ptr, BufferSizeMedium)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.MediumNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.MediumNum-1))
		}

		if g.cfg.BigNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexBig + g.gidOffset
			ptr := unsafe.Pointer(&g.bigBuffers[i*g.cfg.BigNum])
			br, err := rings.registerGroup(ur, g.cfg.BigNum, gid, ptr, BufferSizeBig)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.BigNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.BigNum-1))
		}

		if g.cfg.LargeNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexLarge + g.gidOffset
			ptr := unsafe.Pointer(&g.largeBuffers[i*g.cfg.LargeNum])
			br, err := rings.registerGroup(ur, g.cfg.LargeNum, gid, ptr, BufferSizeLarge)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.LargeNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.LargeNum-1))
		}

		if g.cfg.GreatNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexGreat + g.gidOffset
			ptr := unsafe.Pointer(&g.greatBuffers[i*g.cfg.GreatNum])
			br, err := rings.registerGroup(ur, g.cfg.GreatNum, gid, ptr, BufferSizeGreat)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.GreatNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.GreatNum-1))
		}

		if g.cfg.HugeNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexHuge + g.gidOffset
			ptr := unsafe.Pointer(&g.hugeBuffers[i*g.cfg.HugeNum])
			br, err := rings.registerGroup(ur, g.cfg.HugeNum, gid, ptr, BufferSizeHuge)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.HugeNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.HugeNum-1))
		}

		if g.cfg.VastNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexVast + g.gidOffset
			ptr := unsafe.Pointer(&g.vastBuffers[i*g.cfg.VastNum])
			br, err := rings.registerGroup(ur, g.cfg.VastNum, gid, ptr, BufferSizeVast)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.VastNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.VastNum-1))
		}

		if g.cfg.GiantNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexGiant + g.gidOffset
			ptr := unsafe.Pointer(&g.giantBuffers[i*g.cfg.GiantNum])
			br, err := rings.registerGroup(ur, g.cfg.GiantNum, gid, ptr, BufferSizeGiant)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.GiantNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.GiantNum-1))
		}

		if g.cfg.TitanNum > 0 {
			gid := uint16(i)*bufferGroupIndexEnd + bufferGroupIndexTitan + g.gidOffset
			ptr := unsafe.Pointer(&g.titanBuffers[i*g.cfg.TitanNum])
			br, err := rings.registerGroup(ur, g.cfg.TitanNum, gid, ptr, BufferSizeTitan)
			if err != nil {
				return err
			}
			rings.rings = append(rings.rings, br)
			rings.locks = append(rings.locks, spin.Lock{})
			rings.counts = append(rings.counts, uintptr(g.cfg.TitanNum))
			rings.masks = append(rings.masks, uintptr(g.cfg.TitanNum-1))
		}
	}
	return nil
}

func (rings *uringBufferRings) registerGroup(ur *ioUring, entries int, gid uint16, ptr unsafe.Pointer, size int) (*ioUringBufRing, error) {
	r, backing, err := ur.registerBufRing(entries, gid)
	if err != nil {
		return nil, err
	}
	// Store backing slice to prevent GC (nil for mmap mode)
	rings.backings = append(rings.backings, backing)
	ur.bufRingInit(r)
	mask := entries - 1
	base := uintptr(ptr)
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
