// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"sync/atomic"
	"unsafe"
)

const (
	submitKeepAlivePathInline = 112
	submitKeepAlivePathMax    = 4096
)

// submitKeepAlive keeps slot-owned staging roots live until the kernel consumes
// the SQE. Caller-owned buffers still follow each operation's lifetime contract.
type submitKeepAlive struct {
	active bool

	bytes1 []byte

	iovs []IoVec

	sockaddrArg Sockaddr
	timespec    Timespec
	iovInline   [8]IoVec

	ext *submitKeepAliveExt
}

type submitKeepAliveExt struct {
	next *submitKeepAliveExt

	msg          Msghdr
	recvSockaddr RawSockaddrAny
	epollEvent   EpollEvent

	path1Inline [submitKeepAlivePathInline]byte
	path2Inline [submitKeepAlivePathInline]byte
	path1Buf    []byte
	path2Buf    []byte

	openHow *OpenHow
	statx   *Statx
}

func (k *submitKeepAlive) retainBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	k.active = true
	k.bytes1 = b
}

func (k *submitKeepAlive) retainSockaddr(sa Sockaddr) {
	if sa == nil {
		return
	}
	k.active = true
	k.sockaddrArg = sa
}

func (k *submitKeepAlive) ensureExt(ur *ioUring) *submitKeepAliveExt {
	if k.ext != nil {
		return k.ext
	}
	k.active = true
	k.ext = ur.acquireSubmitKeepAliveExt()
	return k.ext
}

func (k *submitKeepAlive) allocIovs(n int) []IoVec {
	k.active = true
	if n <= len(k.iovInline) {
		k.iovs = k.iovInline[:n]
		return k.iovs
	}
	k.iovs = make([]IoVec, n)
	return k.iovs
}

func (k *submitKeepAlive) iovsFromBytes(buffers [][]byte) []IoVec {
	vec := k.allocIovs(len(buffers))
	for i := range buffers {
		vec[i] = IoVec{
			Base: unsafe.SliceData(buffers[i]),
			Len:  uint64(len(buffers[i])),
		}
	}
	return vec
}

func (k *submitKeepAlive) stagePath1(ur *ioUring, s string) (uint64, error) {
	ext := k.ensureExt(ur)
	buf, err := ext.writePath1(s)
	if err != nil {
		return 0, err
	}
	return uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))), nil
}

func (k *submitKeepAlive) stagePath2(ur *ioUring, s string) (uint64, error) {
	ext := k.ensureExt(ur)
	buf, err := ext.writePath2(s)
	if err != nil {
		return 0, err
	}
	return uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))), nil
}

func (k *submitKeepAlive) clear(ur *ioUring) {
	if k.ext != nil {
		ur.releaseSubmitKeepAliveExt(k.ext)
		k.ext = nil
	}
	k.active = false
	k.bytes1 = nil
	k.iovs = nil
	k.sockaddrArg = nil
	k.iovInline = [8]IoVec{}
}

func (ext *submitKeepAliveExt) writePath1(s string) ([]byte, error) {
	return ext.writePath(&ext.path1Buf, ext.path1Inline[:], s)
}

func (ext *submitKeepAliveExt) writePath2(s string) ([]byte, error) {
	return ext.writePath(&ext.path2Buf, ext.path2Inline[:], s)
}

func (ext *submitKeepAliveExt) writePath(dst *[]byte, inline []byte, s string) ([]byte, error) {
	n := len(s) + 1
	if n > submitKeepAlivePathMax {
		return nil, ErrNameTooLong
	}
	var buf []byte
	if n <= len(inline) {
		buf = inline[:n]
	} else {
		if cap(*dst) < n {
			*dst = make([]byte, n)
		}
		buf = (*dst)[:n]
	}
	if err := writeCString(buf, s); err != nil {
		return nil, err
	}
	return buf, nil
}

func (ext *submitKeepAliveExt) resetForReuse() {
	ext.msg = Msghdr{}
	ext.openHow = nil
	ext.statx = nil
	ext.next = nil
}

func (ur *ioUring) acquireSubmitKeepAliveExt() *submitKeepAliveExt {
	if ext := ur.submit.keepAliveExtFree; ext != nil {
		ur.submit.keepAliveExtFree = ext.next
		ext.next = nil
		return ext
	}
	return &submitKeepAliveExt{}
}

func (ur *ioUring) releaseSubmitKeepAliveExt(ext *submitKeepAliveExt) {
	ext.resetForReuse()
	ext.next = ur.submit.keepAliveExtFree
	ur.submit.keepAliveExtFree = ext
}

// releaseConsumedKeepAlive clears staging roots for SQ slots the kernel has
// already consumed.
func (ur *ioUring) releaseConsumedKeepAlive() {
	if len(ur.submit.keepAlive) == 0 {
		return
	}

	head := atomic.LoadUint32(ur.sq.kHead)
	mask := *ur.sq.kRingMask
	for ur.submit.keepAliveHead != head {
		slot := &ur.submit.keepAlive[ur.submit.keepAliveHead&mask]
		if slot.active {
			slot.clear(ur)
		}
		ur.submit.keepAliveHead++
	}
}

func (ur *ioUring) clearAllKeepAlive() {
	for i := range ur.submit.keepAlive {
		slot := &ur.submit.keepAlive[i]
		if slot.active {
			slot.clear(ur)
		}
	}
	ur.submit.keepAlive = nil
	ur.submit.keepAliveHead = 0
	ur.submit.keepAliveExtFree = nil
}
