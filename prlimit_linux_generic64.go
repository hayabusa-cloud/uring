// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux && (arm64 || loong64 || riscv64)

package uring

const (
	prlimit64Sysno     = 261
	sizeofEpollEvent   = 16
	epollEventFDOffset = 8
)

// EpollEvent represents an epoll event.
// Layout matches struct epoll_event in Linux on generic 64-bit architectures.
type EpollEvent struct {
	Events uint32
	_      int32
	Fd     int32
	Pad    int32
}
