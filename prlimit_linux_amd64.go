// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux && amd64

package uring

const (
	prlimit64Sysno     = 302
	sizeofEpollEvent   = 12
	epollEventFDOffset = 4
)

// EpollEvent represents an epoll event.
// Layout matches struct epoll_event in Linux on amd64.
type EpollEvent struct {
	Events uint32
	Fd     int32
	Pad    int32
}
