// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux || darwin

package uring

import (
	"math"
	"runtime"
	"strconv"
	"testing"
	"unsafe"

	"code.hybscloud.com/zcall"
)

func oversizedLen32(t *testing.T) int {
	t.Helper()
	if strconv.IntSize == 32 {
		t.Skip("oversized Len32 guard requires 64-bit int")
	}
	return int(uint64(math.MaxUint32) + 1)
}

func oversizedSignedInt32(t *testing.T) int {
	t.Helper()
	if strconv.IntSize == 32 {
		t.Skip("oversized signed 32-bit carrier requires 64-bit int")
	}
	return int(int64(math.MaxInt32) + 1)
}

func mappedTestSlice[T any](t *testing.T, length int) []T {
	t.Helper()

	var value T
	elementSize := unsafe.Sizeof(value)
	if length < 1 || elementSize == 0 || uint64(length) > uint64(^uintptr(0)/elementSize) {
		t.Fatalf("invalid mapped test slice length %d for element size %d", length, elementSize)
	}
	mappingLength := uintptr(length) * elementSize
	flags := uintptr(zcall.MAP_PRIVATE | zcall.MAP_ANONYMOUS)
	if runtime.GOOS == "linux" {
		const mapNoReserve = 0x4000
		flags |= mapNoReserve
	}
	ptr, errno := zcall.Mmap(nil, mappingLength, zcall.PROT_READ|zcall.PROT_WRITE, flags, ^uintptr(0), 0)
	if errno != 0 {
		t.Skipf("map %d-byte test slice: errno %d", mappingLength, errno)
	}
	t.Cleanup(func() {
		if errno := zcall.Munmap(ptr, mappingLength); errno != 0 {
			t.Errorf("unmap %d-byte test slice: errno %d", mappingLength, errno)
		}
	})
	return unsafe.Slice((*T)(ptr), length)
}
