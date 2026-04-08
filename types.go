// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
	"context"
	"net"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/zcall"
)

// Network address and socket type aliases.
type (
	// Sockaddr is the socket address interface used by socket operations.
	Sockaddr = sock.Sockaddr

	// Addr is the network address interface used by connect and bind helpers.
	Addr = sock.Addr

	// RawSockaddrAny is the widest raw socket address storage type.
	RawSockaddrAny = sock.RawSockaddrAny

	// RawSockaddr is the base raw socket address structure.
	RawSockaddr = sock.RawSockaddr

	// RawSockaddrInet4 is the raw IPv4 socket address structure.
	RawSockaddrInet4 = sock.RawSockaddrInet4

	// RawSockaddrInet6 is the raw IPv6 socket address structure.
	RawSockaddrInet6 = sock.RawSockaddrInet6

	// RawSockaddrUnix is the raw Unix domain socket address structure.
	RawSockaddrUnix = sock.RawSockaddrUnix

	// NetworkType represents the network address family.
	NetworkType = sock.NetworkType

	// UnderlyingProtocol represents the transport protocol.
	UnderlyingProtocol = sock.UnderlyingProtocol

	// Socket is the minimal interface for socket operations.
	Socket = sock.Socket
)

// Pollable file-descriptor aliases.
type (
	// PollFd represents a file descriptor that can be polled by uring helpers.
	PollFd = iofd.PollFd

	// PollCloser extends PollFd with close capability.
	PollCloser = iofd.PollCloser
)

// Network family aliases.
const (
	NetworkUnix = sock.NetworkUnix
	NetworkIPv4 = sock.NetworkIPv4
	NetworkIPv6 = sock.NetworkIPv6
)

// Raw socket address size constants.
const (
	SizeofSockaddrAny   = sock.SizeofSockaddrAny
	SizeofSockaddrInet4 = sock.SizeofSockaddrInet4
	SizeofSockaddrInet6 = sock.SizeofSockaddrInet6
	SizeofSockaddrUnix  = sock.SizeofSockaddrUnix
)

// Socket, protocol, message, shutdown, and memory-mapping aliases.
const (
	AF_UNIX  = sock.AF_UNIX
	AF_LOCAL = sock.AF_LOCAL
	AF_INET  = sock.AF_INET
	AF_INET6 = sock.AF_INET6

	SOCK_STREAM    = sock.SOCK_STREAM
	SOCK_DGRAM     = sock.SOCK_DGRAM
	SOCK_RAW       = sock.SOCK_RAW
	SOCK_SEQPACKET = sock.SOCK_SEQPACKET
	SOCK_NONBLOCK  = sock.SOCK_NONBLOCK
	SOCK_CLOEXEC   = sock.SOCK_CLOEXEC

	IPPROTO_IP   = sock.IPPROTO_IP
	IPPROTO_RAW  = sock.IPPROTO_RAW
	IPPROTO_TCP  = sock.IPPROTO_TCP
	IPPROTO_UDP  = sock.IPPROTO_UDP
	IPPROTO_IPV6 = sock.IPPROTO_IPV6
	IPPROTO_SCTP = sock.IPPROTO_SCTP

	MSG_WAITALL  = sock.MSG_WAITALL
	MSG_ZEROCOPY = sock.MSG_ZEROCOPY

	SHUT_RD   = sock.SHUT_RD
	SHUT_WR   = sock.SHUT_WR
	SHUT_RDWR = sock.SHUT_RDWR

	PROT_READ  = zcall.PROT_READ
	PROT_WRITE = zcall.PROT_WRITE

	MAP_SHARED   = zcall.MAP_SHARED
	MAP_POPULATE = zcall.MAP_POPULATE
)

// IPPROTO_UDPLITE is UDP-Lite protocol number.
const IPPROTO_UDPLITE = sock.IPPROTO_UDPLITE

// IoVec is the scatter/gather I/O vector type.
type IoVec = zcall.Iovec

// Timespec represents a time value with nanosecond precision.
// Layout matches struct timespec in Linux.
type Timespec = zcall.Timespec

// Msghdr represents a message header for sendmsg/recvmsg.
// Layout matches struct msghdr in Linux (LP64).
type Msghdr = zcall.Msghdr

// OpenHow is the structure for openat2 syscall.
// Layout matches struct open_how in Linux.
type OpenHow struct {
	Flags   uint64
	Mode    uint64
	Resolve uint64
}

// SizeofOpenHow is the size of OpenHow structure.
const SizeofOpenHow = 24

// O_LARGEFILE flag for openat.
const O_LARGEFILE = 0x8000

// AT_FDCWD is the special value for current working directory.
const AT_FDCWD = -100

// EpollEvent represents an epoll event.
// Layout matches struct epoll_event in Linux.
type EpollEvent struct {
	Events uint32
	_      int32 // padding
	Fd     int32
	Pad    int32
}

// Epoll constants.
const (
	EPOLL_CTL_ADD = 1
	EPOLL_CTL_DEL = 2
	EPOLL_CTL_MOD = 3

	EPOLLIN  = 0x1
	EPOLLOUT = 0x4
	EPOLLET  = 0x80000000
)

// Statx represents the statx structure.
// Layout matches struct statx in Linux.
type Statx struct {
	Mask             uint32
	Blksize          uint32
	Attributes       uint64
	Nlink            uint32
	Uid              uint32
	Gid              uint32
	Mode             uint16
	_                uint16
	Ino              uint64
	Size             uint64
	Blocks           uint64
	Attributes_mask  uint64
	Atime            StatxTimestamp
	Btime            StatxTimestamp
	Ctime            StatxTimestamp
	Mtime            StatxTimestamp
	Rdev_major       uint32
	Rdev_minor       uint32
	Dev_major        uint32
	Dev_minor        uint32
	Mnt_id           uint64
	Dio_mem_align    uint32
	Dio_offset_align uint32
	_                [12]uint64
}

// StatxTimestamp represents a timestamp in statx.
type StatxTimestamp struct {
	Sec  int64
	Nsec uint32
	_    int32
}

// noCopy may be added to structs which must not be copied
// after the first use.
type noCopy struct{}

// Lock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Lock() {}

// Unlock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Unlock() {}

// writeCString writes s into dst as a NUL-terminated C string.
// dst must have capacity at least len(s)+1.
func writeCString(dst []byte, s string) error {
	if len(dst) < len(s)+1 {
		return ErrInvalidParam
	}
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return ErrInvalidParam
		}
		dst[i] = s[i]
	}
	dst[len(s)] = 0
	return nil
}

// ioVecSliceFromBytesSlice converts a slice of byte slices to an IoVec slice.
func ioVecSliceFromBytesSlice(iov [][]byte) []IoVec {
	vec := make([]IoVec, len(iov))
	for i := range len(iov) {
		vec[i] = IoVec{Base: unsafe.SliceData(iov[i]), Len: uint64(len(iov[i]))}
	}
	return vec
}

// ioVecFromBytesSlice converts a slice of byte slices to iovec array.
// Returns unsafe.Pointer directly to avoid uintptr → unsafe.Pointer conversion at call sites.
func ioVecFromBytesSlice(iov [][]byte) (ptr unsafe.Pointer, n int) {
	vec := ioVecSliceFromBytesSlice(iov)
	return unsafe.Pointer(unsafe.SliceData(vec)), len(vec)
}

// ioVecAddrLen returns the address and length of an iovec slice.
// Returns unsafe.Pointer directly to avoid uintptr → unsafe.Pointer conversion at call sites.
func ioVecAddrLen(vec []IoVec) (ptr unsafe.Pointer, n int) {
	return unsafe.Pointer(unsafe.SliceData(vec)), len(vec)
}

// contextKey is a type for context keys to avoid collisions.
type contextKey[T any] struct{}

// ContextUserData extracts a typed value from context.
// Returns the zero value of T if not found.
func ContextUserData[T any](ctx context.Context) T {
	if v := ctx.Value(contextKey[T]{}); v != nil {
		return v.(T)
	}
	var zero T
	return zero
}

// ContextWithUserData returns a new context with the typed value stored.
func ContextWithUserData[T any](ctx context.Context, val T) context.Context {
	return context.WithValue(ctx, contextKey[T]{}, val)
}

// poller is an interface for event polling mechanisms.
type poller interface {
	add(fd int, events uint32) error
	del(fd int) error
	wait(events []EpollEvent, timeout int) (int, error)
}

// sockaddr returns the borrowed raw address pointer and length.
// Async submit paths must keep the Sockaddr root alive until kernel consumption.
func sockaddr(sa Sockaddr) (unsafe.Pointer, int, error) {
	if sa == nil {
		return nil, 0, nil
	}
	ptr, length := sa.Raw()
	return ptr, int(length), nil
}

// sockaddrData returns a borrowed byte view over a socket address.
// Callers must retain the Sockaddr root while the returned slice is in use.
func sockaddrData(sa Sockaddr) ([]byte, error) {
	if sa == nil {
		return nil, nil
	}
	ptr, length := sa.Raw()
	return unsafe.Slice((*byte)(ptr), length), nil
}

// AddrToSockaddr converts an Addr to a Sockaddr.
// Submission paths that stage the result keep the returned Sockaddr alive.
func AddrToSockaddr(addr Addr) Sockaddr {
	if addr == nil {
		return nil
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		return sock.TCPAddrToSockaddr(a)
	case *net.UDPAddr:
		return sock.UDPAddrToSockaddr(a)
	case *net.UnixAddr:
		return sock.UnixAddrToSockaddr(a)
	}
	return nil
}
