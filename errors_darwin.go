// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"errors"
	"syscall"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
)

// Common errors for semantic consistency across the ecosystem (Darwin stub).
// Aliased to iofd so message identity matches the Linux build.
var (
	ErrInvalidParam = iofd.ErrInvalidParam
	ErrInterrupted  = iofd.ErrInterrupted
	ErrNoMemory     = iofd.ErrNoMemory
	ErrPermission   = iofd.ErrPermission
)

// Error definitions for uring operations.
var (
	ErrInProgress         = errors.New("uring: operation in progress")
	ErrFaultParams        = errors.New("uring: fault in parameters")
	ErrProcessFileLimit   = errors.New("uring: process file descriptor limit")
	ErrSystemFileLimit    = errors.New("uring: system file descriptor limit")
	ErrNoDevice           = errors.New("uring: no such device")
	ErrNotSupported       = errors.New("uring: operation not supported")
	ErrBusy               = errors.New("uring: resource busy")
	ErrClosed             = errors.New("uring: ring closed")
	ErrCQOverflow         = errors.New("uring: completion queue overflow")
	ErrExists             = errors.New("uring: already exists")
	ErrNameTooLong        = errors.New("uring: name too long")
	ErrNotFound           = errors.New("uring: not found")
	ErrCanceled           = errors.New("uring: operation canceled")
	ErrTimedOut           = errors.New("uring: operation timed out")
	ErrConnectionRefused  = errors.New("uring: connection refused")
	ErrConnectionReset    = errors.New("uring: connection reset")
	ErrNotConnected       = errors.New("uring: not connected")
	ErrAlreadyConnected   = errors.New("uring: already connected")
	ErrAddressInUse       = errors.New("uring: address in use")
	ErrNetworkUnreachable = errors.New("uring: network unreachable")
	ErrHostUnreachable    = errors.New("uring: host unreachable")
	ErrBrokenPipe         = errors.New("uring: broken pipe")
	ErrNoBufferSpace      = errors.New("uring: no buffer space available")
)

// Darwin errno constants
const (
	EPERM           = uintptr(1)
	EINTR           = uintptr(4)
	ENODEV          = uintptr(19)
	EINVAL          = uintptr(22)
	EAGAIN          = uintptr(35)
	EWOULDBLOCK     = EAGAIN
	EINPROGRESS     = uintptr(36)
	EALREADY        = uintptr(37)
	ENOTSOCK        = uintptr(38)
	EDESTADDRREQ    = uintptr(39)
	EMSGSIZE        = uintptr(40)
	EPROTOTYPE      = uintptr(41)
	ENOPROTOOPT     = uintptr(42)
	EPROTONOSUPPORT = uintptr(43)
	EOPNOTSUPP      = uintptr(45)
	EAFNOSUPPORT    = uintptr(47)
	EADDRINUSE      = uintptr(48)
	EADDRNOTAVAIL   = uintptr(49)
	ENETDOWN        = uintptr(50)
	ENETUNREACH     = uintptr(51)
	ENETRESET       = uintptr(52)
	ECONNABORTED    = uintptr(53)
	ECONNRESET      = uintptr(54)
	ENOBUFS         = uintptr(55)
	EISCONN         = uintptr(56)
	ENOTCONN        = uintptr(57)
	ESHUTDOWN       = uintptr(58)
	ETIMEDOUT       = uintptr(60)
	ECONNREFUSED    = uintptr(61)
	EHOSTDOWN       = uintptr(64)
	EHOSTUNREACH    = uintptr(65)
	ENOMEM          = uintptr(12)
	EACCES          = uintptr(13)
	EFAULT          = uintptr(14)
	EBUSY           = uintptr(16)
	EEXIST          = uintptr(17)
	ENAMETOOLONG    = uintptr(63)
	EMFILE          = uintptr(24)
	ENFILE          = uintptr(23)
	ENOSYS          = uintptr(78)
	ENOTSUP         = uintptr(45)
	EPIPE           = uintptr(32)
	ECANCELED       = uintptr(89)
)

// errFromErrno converts a zcall errno to a semantic error.
func errFromErrno(errno uintptr) error {
	if errno == 0 {
		return nil
	}
	switch errno {
	case EINTR:
		return ErrInterrupted
	case EAGAIN:
		return iox.ErrWouldBlock
	case EINPROGRESS, EALREADY:
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
	case EEXIST:
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
		return syscall.Errno(errno)
	}
}

// CompletionError decodes a CQE result into the package error model.
// Non-negative results are successful byte counts or operation values.
// Negative results are kernel -errno values.
func CompletionError(res int32) error {
	if res >= 0 {
		return nil
	}
	return errFromErrno(uintptr(-int64(res)))
}

// Err decodes c.Res as a completion error.
func (c *CQEView) Err() error {
	return CompletionError(c.Res)
}

// Err decodes c.Res as a completion error.
func (c *DirectCQE) Err() error {
	return CompletionError(c.Res)
}

// Err decodes c.Res as a completion error.
func (c *ExtCQE) Err() error {
	return CompletionError(c.Res)
}
