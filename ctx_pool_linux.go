// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"unsafe"

	"code.hybscloud.com/iobuf"
)

func newIndirectSQEPool(capacity int) []IndirectSQE {
	indirectMem := iobuf.AlignedMem(capacity*int(unsafe.Sizeof(IndirectSQE{})), iobuf.PageSize)
	return unsafe.Slice((*IndirectSQE)(unsafe.Pointer(unsafe.SliceData(indirectMem))), capacity)
}
