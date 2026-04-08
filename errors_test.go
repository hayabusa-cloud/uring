// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"testing"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/zcall"
)

func TestErrFromErrno(t *testing.T) {
	tests := []struct {
		name     string
		errno    uintptr
		expected error
	}{
		{"zero", 0, nil},
		{"EINTR", EINTR, ErrInterrupted},
		{"EAGAIN", EAGAIN, iox.ErrWouldBlock},
		{"EINPROGRESS", EINPROGRESS, ErrInProgress},
		{"EFAULT", EFAULT, ErrFaultParams},
		{"EINVAL", EINVAL, ErrInvalidParam},
		{"EMFILE", EMFILE, ErrProcessFileLimit},
		{"ENFILE", ENFILE, ErrSystemFileLimit},
		{"ENODEV", ENODEV, ErrNoDevice},
		{"ENOMEM", ENOMEM, ErrNoMemory},
		{"EACCES", EACCES, ErrPermission},
		{"EPERM", EPERM, ErrPermission},
		{"ENOSYS", ENOSYS, ErrNotSupported},
		{"ENOTSUP", ENOTSUP, ErrNotSupported},
		{"EBUSY", EBUSY, ErrBusy},
		{"EEXIST", EEXIST, ErrExists},
		{"EALREADY", EALREADY, ErrInProgress},
		{"ECANCELED", ECANCELED, ErrCanceled},
		{"ETIMEDOUT", ETIMEDOUT, ErrTimedOut},
		{"ECONNREFUSED", ECONNREFUSED, ErrConnectionRefused},
		{"ECONNRESET", ECONNRESET, ErrConnectionReset},
		{"ECONNABORTED", ECONNABORTED, ErrConnectionReset},
		{"ENOTCONN", ENOTCONN, ErrNotConnected},
		{"EISCONN", EISCONN, ErrAlreadyConnected},
		{"EADDRINUSE", EADDRINUSE, ErrAddressInUse},
		{"ENETUNREACH", ENETUNREACH, ErrNetworkUnreachable},
		{"EHOSTUNREACH", EHOSTUNREACH, ErrHostUnreachable},
		{"EHOSTDOWN", EHOSTDOWN, ErrHostUnreachable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errFromErrno(tt.errno)
			if got != tt.expected {
				t.Errorf("errFromErrno(%d) = %v, want %v", tt.errno, got, tt.expected)
			}
		})
	}
}

func TestErrFromErrno_UnmappedReturnsRawErrno(t *testing.T) {
	// Use an errno that's not mapped
	unmapped := uintptr(999)
	got := errFromErrno(unmapped)
	if got != zcall.Errno(unmapped) {
		t.Errorf("unmapped errno should return zcall.Errno, got %T", got)
	}
}

func TestErrorMessages(t *testing.T) {
	// Verify error messages are non-empty
	errors := []error{
		ErrInvalidParam,
		ErrInterrupted,
		ErrInProgress,
		ErrFaultParams,
		ErrProcessFileLimit,
		ErrSystemFileLimit,
		ErrNoDevice,
		ErrNoMemory,
		ErrPermission,
		ErrNotSupported,
		ErrBusy,
		ErrExists,
		ErrNotFound,
		ErrCQOverflow,
		ErrCanceled,
		ErrTimedOut,
		ErrConnectionRefused,
		ErrConnectionReset,
		ErrNotConnected,
		ErrAlreadyConnected,
		ErrAddressInUse,
		ErrNetworkUnreachable,
		ErrHostUnreachable,
		ErrBrokenPipe,
		ErrNoBufferSpace,
	}

	for _, err := range errors {
		msg := err.Error()
		if msg == "" {
			t.Errorf("error %v has empty message", err)
		}
		if len(msg) < 10 {
			t.Errorf("error %v has suspiciously short message: %q", err, msg)
		}
	}
}

func TestErrnoConstants(t *testing.T) {
	// Verify errno constants match expected Linux values from zcall
	tests := []struct {
		name     string
		got      uintptr
		expected uintptr
	}{
		{"EINTR", EINTR, uintptr(zcall.EINTR)},
		{"EAGAIN", EAGAIN, uintptr(zcall.EAGAIN)},
		{"EWOULDBLOCK", EWOULDBLOCK, EAGAIN},
		{"ENOMEM", ENOMEM, uintptr(zcall.ENOMEM)},
		{"EACCES", EACCES, uintptr(zcall.EACCES)},
		{"EFAULT", EFAULT, uintptr(zcall.EFAULT)},
		{"EBUSY", EBUSY, uintptr(zcall.EBUSY)},
		{"EEXIST", EEXIST, uintptr(zcall.EEXIST)},
		{"ENODEV", ENODEV, uintptr(zcall.ENODEV)},
		{"EINVAL", EINVAL, uintptr(zcall.EINVAL)},
		{"ENFILE", ENFILE, uintptr(zcall.ENFILE)},
		{"EMFILE", EMFILE, uintptr(zcall.EMFILE)},
		{"ENOSYS", ENOSYS, uintptr(zcall.ENOSYS)},
		{"ENOTSUP", ENOTSUP, uintptr(zcall.ENOTSUP)},
		{"EADDRINUSE", EADDRINUSE, uintptr(zcall.EADDRINUSE)},
		{"ECONNREFUSED", ECONNREFUSED, uintptr(zcall.ECONNREFUSED)},
		{"ETIMEDOUT", ETIMEDOUT, uintptr(zcall.ETIMEDOUT)},
		{"EINPROGRESS", EINPROGRESS, uintptr(zcall.EINPROGRESS)},
		{"ECANCELED", ECANCELED, uintptr(zcall.ECANCELED)},
		{"EPERM", EPERM, uintptr(zcall.EPERM)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.expected)
			}
		})
	}
}
