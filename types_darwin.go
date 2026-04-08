// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"context"
	"net"
	"unsafe"

	"code.hybscloud.com/iobuf"
	"code.hybscloud.com/sock"
)

// Sockaddr represents a socket address (Darwin stub).
type Sockaddr interface {
	Raw() (unsafe.Pointer, uint32)
}

// Addr is an alias for net.Addr (Darwin stub).
type Addr = net.Addr

// RawSockaddrAny is a generic socket address structure.
type RawSockaddrAny struct {
	Len    uint8
	Family uint8
	Data   [126]byte
}

// RawSockaddr is a generic socket address.
type RawSockaddr struct {
	Len    uint8
	Family uint8
	Data   [14]byte
}

// RawSockaddrInet4 is an IPv4 socket address.
type RawSockaddrInet4 struct {
	Len    uint8
	Family uint8
	Port   uint16
	Addr   [4]byte
	Zero   [8]byte
}

// RawSockaddrInet6 is an IPv6 socket address.
type RawSockaddrInet6 struct {
	Len      uint8
	Family   uint8
	Port     uint16
	Flowinfo uint32
	Addr     [16]byte
	Scope_id uint32
}

// RawSockaddrUnix is a Unix domain socket address.
type RawSockaddrUnix struct {
	Len    uint8
	Family uint8
	Path   [104]byte
}

// NetworkType represents a network type.
type NetworkType uint8

// UnderlyingProtocol represents an underlying protocol.
type UnderlyingProtocol uint8

// Socket is a stub interface for sockets.
type Socket interface{}

// PollFd represents a pollable file descriptor (Darwin stub).
type PollFd interface {
	Fd() int
}

// PollCloser extends PollFd with close capability (Darwin stub).
type PollCloser interface {
	PollFd
	Close() error
}

// Network type constants (Darwin stub).
const (
	NetworkUnix NetworkType = 0
	NetworkIPv4 NetworkType = 1
	NetworkIPv6 NetworkType = 2
)

// Sockaddr size constants (Darwin stub).
const (
	SizeofSockaddrAny   = sock.SizeofSockaddrAny
	SizeofSockaddrInet4 = sock.SizeofSockaddrInet4
	SizeofSockaddrInet6 = sock.SizeofSockaddrInet6
	SizeofSockaddrUnix  = sock.SizeofSockaddrUnix
)

// Socket constants (Darwin values).
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
)

// Darwin-specific constants
const (
	IPPROTO_SCTP    = sock.IPPROTO_SCTP
	IPPROTO_UDPLITE = sock.IPPROTO_UDPLITE

	MSG_WAITALL  = sock.MSG_WAITALL
	MSG_ZEROCOPY = sock.MSG_ZEROCOPY

	SHUT_RD   = sock.SHUT_RD
	SHUT_WR   = sock.SHUT_WR
	SHUT_RDWR = sock.SHUT_RDWR

	PROT_READ  = 0x1
	PROT_WRITE = 0x2

	MAP_SHARED   = 0x1
	MAP_POPULATE = 0x0 // Not available on Darwin, ignored
)

// IoVec is the scatter/gather I/O vector from iobuf.
type IoVec = iobuf.IoVec

// Timespec represents a time value with nanosecond precision.
type Timespec struct {
	Sec  int64
	Nsec int64
}

// Msghdr represents a message header for sendmsg/recvmsg.
type Msghdr struct {
	Name       *byte
	Namelen    uint32
	_          [4]byte
	Iov        *IoVec
	Iovlen     uint64
	Control    *byte
	Controllen uint64
	Flags      int32
	_          [4]byte
}

// OpenHow is the structure for openat2 syscall.
type OpenHow struct {
	Flags   uint64
	Mode    uint64
	Resolve uint64
}

// SizeofOpenHow is the size of OpenHow structure.
const SizeofOpenHow = 24

// O_LARGEFILE flag for openat (zero on Darwin).
const O_LARGEFILE = 0

// AT_FDCWD is the special value for current working directory.
const AT_FDCWD = -2

// EpollEvent represents an epoll event (Darwin uses kqueue but we keep for API compatibility).
type EpollEvent struct {
	Events uint32
	_      int32
	Fd     int32
	Pad    int32
}

const (
	EPOLL_CTL_ADD = 1
	EPOLL_CTL_DEL = 2
	EPOLL_CTL_MOD = 3

	EPOLLIN  = 0x1
	EPOLLOUT = 0x4
	EPOLLET  = 0x80000000
)

// Statx represents the statx structure.
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

// noCopy may be added to structs which must not be copied.
type noCopy struct{}

// Lock is a no-op used by go vet's -copylocks checker.
func (*noCopy) Lock() {}

// Unlock is a no-op used by go vet's -copylocks checker.
func (*noCopy) Unlock() {}

// bytePtrFromString returns a pointer to a new NUL-terminated copy of s.
func bytePtrFromString(s string) (*byte, error) {
	a := make([]byte, len(s)+1)
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return nil, ErrInvalidParam
		}
		a[i] = s[i]
	}
	return &a[0], nil
}

// ioVecFromBytesSlice converts a slice of byte slices to iovec array.
// Returns unsafe.Pointer directly to avoid uintptr → unsafe.Pointer conversion at call sites.
func ioVecFromBytesSlice(iov [][]byte) (ptr unsafe.Pointer, n int) {
	vec := make([]IoVec, len(iov))
	for i := range len(iov) {
		vec[i] = IoVec{Base: unsafe.SliceData(iov[i]), Len: uint64(len(iov[i]))}
	}
	return unsafe.Pointer(unsafe.SliceData(vec)), len(vec)
}

// ioVecAddrLen returns the address and length of an iovec slice.
// Returns unsafe.Pointer directly to avoid uintptr → unsafe.Pointer conversion at call sites.
func ioVecAddrLen(vec []IoVec) (ptr unsafe.Pointer, n int) {
	return unsafe.Pointer(unsafe.SliceData(vec)), len(vec)
}

// contextKey is a type for context keys.
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
// Darwin returns nil because socket helpers are unavailable.
func AddrToSockaddr(addr Addr) Sockaddr {
	return nil
}
