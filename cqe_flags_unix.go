//go:build unix

// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring

//go:nosplit
func cqeIsSuccess(res int32) bool {
	return res >= 0
}

//go:nosplit
func cqeHasMore(flags uint32) bool {
	return flags&IORING_CQE_F_MORE != 0
}

//go:nosplit
func cqeHasBuffer(flags uint32) bool {
	return flags&IORING_CQE_F_BUFFER != 0
}

//go:nosplit
func cqeBufID(flags uint32) uint16 {
	return uint16(flags >> IORING_CQE_BUFFER_SHIFT)
}

//go:nosplit
func cqeIsNotification(flags uint32) bool {
	return flags&IORING_CQE_F_NOTIF != 0
}

//go:nosplit
func cqeHasBufferMore(flags uint32) bool {
	return flags&IORING_CQE_F_BUF_MORE != 0
}
