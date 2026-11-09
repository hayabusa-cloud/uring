// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

// This file is part of the `uring` package refactored from `code.hybscloud.com/sox`.

import (
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

// IoVec is the scatter/gather I/O vector type.
type IoVec = zcall.Iovec

// Timespec represents a time value with nanosecond precision.
type Timespec = zcall.Timespec
