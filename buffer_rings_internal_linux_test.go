// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"math"
	"testing"
	"unsafe"

	"code.hybscloud.com/zcall"
)

func TestBufferRingAddAdvance(t *testing.T) {
	const entries = 4
	slots := make([]ioUringBuf, entries)
	raw := (*ioUringBufRing)(unsafe.Pointer(unsafe.SliceData(slots)))
	ring := &ioUring{}
	br := &BufferRing{ring: ring, bufRing: raw, entries: entries, mask: entries - 1}
	ring.bufRingInit(raw)

	first := []byte("first")
	second := []byte("second")
	if err := br.Add(first, 7, 0); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if err := br.Add(second, 9, 1); err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if err := br.Advance(2); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	if got := raw.tail; got != 2 {
		t.Fatalf("tail = %d, want 2", got)
	}
	if got, want := slots[0].addr, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(first)))); got != want {
		t.Fatalf("first addr = %#x, want %#x", got, want)
	}
	if got := slots[0].len; got != uint32(len(first)) {
		t.Fatalf("first len = %d, want %d", got, len(first))
	}
	if got := slots[0].bid; got != 7 {
		t.Fatalf("first bid = %d, want 7", got)
	}
	if got, want := slots[1].addr, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(second)))); got != want {
		t.Fatalf("second addr = %#x, want %#x", got, want)
	}
	if got := slots[1].len; got != uint32(len(second)) {
		t.Fatalf("second len = %d, want %d", got, len(second))
	}
	if got := slots[1].bid; got != 9 {
		t.Fatalf("second bid = %d, want 9", got)
	}
}

func TestBufferRingRejectsInvalidInputBeforeMutation(t *testing.T) {
	const entries = 4
	slots := make([]ioUringBuf, entries)
	raw := (*ioUringBufRing)(unsafe.Pointer(unsafe.SliceData(slots)))
	br := &BufferRing{ring: &ioUring{}, bufRing: raw, entries: entries, mask: entries - 1}

	for _, tc := range []struct {
		name   string
		buf    []byte
		offset int
	}{
		{name: "empty buffer", offset: 0},
		{name: "negative offset", buf: []byte{1}, offset: -1},
		{name: "offset at entries", buf: []byte{1}, offset: entries},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := br.Add(tc.buf, 1, tc.offset); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("Add error = %v, want %v", err, ErrInvalidParam)
			}
			if slots[0] != (ioUringBuf{}) {
				t.Fatalf("invalid Add mutated first slot: %+v", slots[0])
			}
		})
	}

	for _, count := range []int{-1, entries + 1} {
		if err := br.Advance(count); !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("Advance(%d) error = %v, want %v", count, err, ErrInvalidParam)
		}
		if raw.tail != 0 {
			t.Fatalf("invalid Advance(%d) changed tail to %d", count, raw.tail)
		}
	}
}

func TestBufferRingAddLengthDomain(t *testing.T) {
	if math.MaxInt <= math.MaxInt32 {
		t.Skip("the u32 endpoint is not representable as int on this architecture")
	}

	mappingLength := uint64(math.MaxUint32) + 1
	ptr, errno := zcall.Mmap(nil, uintptr(mappingLength), zcall.PROT_NONE, zcall.MAP_PRIVATE|zcall.MAP_ANONYMOUS, ^uintptr(0), 0)
	if errno != 0 {
		t.Skipf("map buffer-ring length domain: errno %d", errno)
	}
	t.Cleanup(func() {
		if errno := zcall.Munmap(ptr, uintptr(mappingLength)); errno != 0 {
			t.Errorf("unmap buffer-ring length domain: errno %d", errno)
		}
	})
	mapped := unsafe.Slice((*byte)(ptr), int(mappingLength))

	const entries = 4
	slots := make([]ioUringBuf, entries)
	raw := (*ioUringBufRing)(unsafe.Pointer(unsafe.SliceData(slots)))
	br := &BufferRing{ring: &ioUring{}, bufRing: raw, entries: entries, mask: entries - 1}

	aboveSigned := int(uint64(math.MaxInt32) + 1)
	if err := br.Add(mapped[:aboveSigned], 3, 0); err != nil {
		t.Fatalf("Add length %d: %v", aboveSigned, err)
	}
	if got := slots[0].len; got != uint32(aboveSigned) {
		t.Fatalf("length above signed maximum encoded as %d, want %d", got, aboveSigned)
	}

	unsignedMaximum := int(uint64(math.MaxUint32))
	if err := br.Add(mapped[:unsignedMaximum], 5, 1); err != nil {
		t.Fatalf("Add length %d: %v", unsignedMaximum, err)
	}
	if got := slots[1].len; got != math.MaxUint32 {
		t.Fatalf("unsigned maximum encoded as %d, want %d", got, uint64(math.MaxUint32))
	}

	if err := br.Add(mapped, 7, 2); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("Add length %d error = %v, want %v", len(mapped), err, ErrInvalidParam)
	}
	if slots[2] != (ioUringBuf{}) {
		t.Fatalf("oversized Add mutated slot: %+v", slots[2])
	}
}

func TestBufferRingAddAdvanceAllocations(t *testing.T) {
	const entries = 2
	slots := make([]ioUringBuf, entries)
	raw := (*ioUringBufRing)(unsafe.Pointer(unsafe.SliceData(slots)))
	br := &BufferRing{ring: &ioUring{}, bufRing: raw, entries: entries, mask: entries - 1}
	buf := make([]byte, 64)

	allocs := testing.AllocsPerRun(100, func() {
		raw.tail = 0
		if err := br.Add(buf, 0, 0); err != nil {
			panic(err)
		}
		if err := br.Advance(1); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("BufferRing Add/Advance allocations = %v, want 0", allocs)
	}
}
