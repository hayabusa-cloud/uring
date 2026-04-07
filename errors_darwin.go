// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"errors"
	"syscall"

	"code.hybscloud.com/iox"
)

// Common errors for semantic consistency across the ecosystem (Darwin stub).
var (
	ErrInvalidParam = errors.New("invalid parameter")
	ErrInterrupted  = errors.New("interrupted")
	ErrNoMemory     = errors.New("no memory")
	ErrPermission   = errors.New("permission denied")
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
	EPERM           = 1
	EINTR           = 4
	ENODEV          = 19
	EINVAL          = 22
	EAGAIN          = 35
	EWOULDBLOCK     = EAGAIN
	EINPROGRESS     = 36
	EALREADY        = 37
	ENOTSOCK        = 38
	EDESTADDRREQ    = 39
	EMSGSIZE        = 40
	EPROTOTYPE      = 41
	ENOPROTOOPT     = 42
	EPROTONOSUPPORT = 43
	EOPNOTSUPP      = 45
	EAFNOSUPPORT    = 47
	EADDRINUSE      = 48
	EADDRNOTAVAIL   = 49
	ENETDOWN        = 50
	ENETUNREACH     = 51
	ENETRESET       = 52
	ECONNABORTED    = 53
	ECONNRESET      = 54
	ENOBUFS         = 55
	EISCONN         = 56
	ENOTCONN        = 57
	ESHUTDOWN       = 58
	ETIMEDOUT       = 60
	ECONNREFUSED    = 61
	EHOSTDOWN       = 64
	EHOSTUNREACH    = 65
	ENOMEM          = 12
	EACCES          = 13
	EFAULT          = 14
	EBUSY           = 16
	EEXIST          = 17
	ENAMETOOLONG    = 63
	EMFILE          = 24
	ENFILE          = 23
	ENOSYS          = 78
	ENOTSUP         = 45
	EPIPE           = 32
	ECANCELED       = 89
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
