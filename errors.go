// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/zcall"
)

// Common errors reused from iofd for semantic consistency across the ecosystem.
var (
	ErrInvalidParam = iofd.ErrInvalidParam
	ErrInterrupted  = iofd.ErrInterrupted
	ErrNoMemory     = iofd.ErrNoMemory
	ErrPermission   = iofd.ErrPermission
)

// Error definitions for uring operations.
var (
	// ErrInProgress indicates the operation is in progress.
	ErrInProgress = errors.New("uring: operation in progress")

	// ErrFaultParams indicates a fault in parameters (bad address).
	ErrFaultParams = errors.New("uring: fault in parameters")

	// ErrProcessFileLimit indicates the process file descriptor limit was reached.
	ErrProcessFileLimit = errors.New("uring: process file descriptor limit")

	// ErrSystemFileLimit indicates the system file descriptor limit was reached.
	ErrSystemFileLimit = errors.New("uring: system file descriptor limit")

	// ErrNoDevice indicates no such device.
	ErrNoDevice = errors.New("uring: no such device")

	// ErrNotSupported indicates the operation is not supported.
	ErrNotSupported = errors.New("uring: operation not supported")

	// ErrBusy indicates the resource is busy.
	ErrBusy = errors.New("uring: resource busy")

	// ErrClosed indicates the ring has already been stopped.
	ErrClosed = errors.New("uring: ring closed")

	// ErrExists indicates the resource already exists.
	ErrExists = errors.New("uring: already exists")

	// ErrNameTooLong indicates a pathname exceeds the kernel limit.
	ErrNameTooLong = errors.New("uring: name too long")

	// ErrNotFound indicates the resource was not found.
	ErrNotFound = errors.New("uring: not found")

	// ErrCanceled indicates the operation was canceled.
	ErrCanceled = errors.New("uring: operation canceled")

	// ErrTimedOut indicates the operation timed out.
	ErrTimedOut = errors.New("uring: operation timed out")

	// ErrConnectionRefused indicates the connection was refused.
	ErrConnectionRefused = errors.New("uring: connection refused")

	// ErrConnectionReset indicates the connection was reset.
	ErrConnectionReset = errors.New("uring: connection reset")

	// ErrNotConnected indicates the socket is not connected.
	ErrNotConnected = errors.New("uring: not connected")

	// ErrAlreadyConnected indicates the socket is already connected.
	ErrAlreadyConnected = errors.New("uring: already connected")

	// ErrAddressInUse indicates the address is already in use.
	ErrAddressInUse = errors.New("uring: address in use")

	// ErrNetworkUnreachable indicates the network is unreachable.
	ErrNetworkUnreachable = errors.New("uring: network unreachable")

	// ErrHostUnreachable indicates the host is unreachable.
	ErrHostUnreachable = errors.New("uring: host unreachable")

	// ErrBrokenPipe indicates the pipe is broken (EPIPE).
	ErrBrokenPipe = errors.New("uring: broken pipe")

	// ErrNoBufferSpace indicates no buffer space available (ENOBUFS).
	ErrNoBufferSpace = errors.New("uring: no buffer space available")
)

// Errno constants aliased from zcall for architecture-safe error handling.
const (
	EPERM           = uintptr(zcall.EPERM)
	EINTR           = uintptr(zcall.EINTR)
	EAGAIN          = uintptr(zcall.EAGAIN)
	EWOULDBLOCK     = EAGAIN
	ENOMEM          = uintptr(zcall.ENOMEM)
	EACCES          = uintptr(zcall.EACCES)
	EFAULT          = uintptr(zcall.EFAULT)
	EBUSY           = uintptr(zcall.EBUSY)
	EEXIST          = uintptr(zcall.EEXIST)
	ENAMETOOLONG    = uintptr(zcall.ENAMETOOLONG)
	ENODEV          = uintptr(zcall.ENODEV)
	EINVAL          = uintptr(zcall.EINVAL)
	EPIPE           = uintptr(zcall.EPIPE)
	EMFILE          = uintptr(zcall.EMFILE)
	ENFILE          = uintptr(zcall.ENFILE)
	ENOSYS          = uintptr(zcall.ENOSYS)
	ENOTSUP         = uintptr(zcall.ENOTSUP)
	EINPROGRESS     = uintptr(zcall.EINPROGRESS)
	EALREADY        = uintptr(zcall.EALREADY)
	ENOTSOCK        = uintptr(zcall.ENOTSOCK)
	EDESTADDRREQ    = uintptr(zcall.EDESTADDRREQ)
	EMSGSIZE        = uintptr(zcall.EMSGSIZE)
	EPROTOTYPE      = uintptr(zcall.EPROTOTYPE)
	ENOPROTOOPT     = uintptr(zcall.ENOPROTOOPT)
	EPROTONOSUPPORT = uintptr(zcall.EPROTONOSUPPORT)
	EOPNOTSUPP      = uintptr(zcall.EOPNOTSUPP)
	EAFNOSUPPORT    = uintptr(zcall.EAFNOSUPPORT)
	EADDRINUSE      = uintptr(zcall.EADDRINUSE)
	EADDRNOTAVAIL   = uintptr(zcall.EADDRNOTAVAIL)
	ENETDOWN        = uintptr(zcall.ENETDOWN)
	ENETUNREACH     = uintptr(zcall.ENETUNREACH)
	ENETRESET       = uintptr(zcall.ENETRESET)
	ECONNABORTED    = uintptr(zcall.ECONNABORTED)
	ECONNRESET      = uintptr(zcall.ECONNRESET)
	ENOBUFS         = uintptr(zcall.ENOBUFS)
	EISCONN         = uintptr(zcall.EISCONN)
	ENOTCONN        = uintptr(zcall.ENOTCONN)
	ESHUTDOWN       = uintptr(zcall.ESHUTDOWN)
	ETIMEDOUT       = uintptr(zcall.ETIMEDOUT)
	ECONNREFUSED    = uintptr(zcall.ECONNREFUSED)
	EHOSTDOWN       = uintptr(zcall.EHOSTDOWN)
	EHOSTUNREACH    = uintptr(zcall.EHOSTUNREACH)
	ECANCELED       = uintptr(zcall.ECANCELED)
)

// errFromErrno converts a zcall errno to a semantic error.
// It maps common errno values to uring-specific or iox errors.
func errFromErrno(errno uintptr) error {
	if errno == 0 {
		return nil
	}
	switch errno {
	case EINTR:
		return ErrInterrupted
	case EAGAIN:
		return iox.ErrWouldBlock
	case EINPROGRESS:
		return ErrInProgress
	case EFAULT:
		return ErrFaultParams
	case EINVAL:
		return ErrInvalidParam
	case EMFILE:
		return ErrProcessFileLimit
	case ENFILE:
		return ErrSystemFileLimit
	case ENODEV:
		return ErrNoDevice
	case ENOMEM:
		return ErrNoMemory
	case EACCES, EPERM:
		return ErrPermission
	case ENOSYS, ENOTSUP:
		return ErrNotSupported
	case EBUSY:
		return ErrBusy
	case EEXIST, EALREADY:
		return ErrExists
	case ENAMETOOLONG:
		return ErrNameTooLong
	case ECANCELED:
		return ErrCanceled
	case ETIMEDOUT:
		return ErrTimedOut
	case ECONNREFUSED:
		return ErrConnectionRefused
	case ECONNRESET, ECONNABORTED:
		return ErrConnectionReset
	case ENOTCONN:
		return ErrNotConnected
	case EISCONN:
		return ErrAlreadyConnected
	case EADDRINUSE:
		return ErrAddressInUse
	case ENETUNREACH:
		return ErrNetworkUnreachable
	case EHOSTUNREACH, EHOSTDOWN:
		return ErrHostUnreachable
	case EPIPE:
		return ErrBrokenPipe
	case ENOBUFS:
		return ErrNoBufferSpace
	default:
		return zcall.Errno(errno)
	}
}
