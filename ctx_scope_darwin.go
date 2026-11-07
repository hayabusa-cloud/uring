// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"code.hybscloud.com/iox"
)

// ScopedExtSQE holds a borrowed ExtSQE for deferred release on submission failure.
// After a successful submission, call [ScopedExtSQE.Submitted] to transfer return
// responsibility to completion handling. The context remains borrowed until
// [Uring.PutExtSQE] returns it to the pool.
//
// A scope must have one owner: do not copy it or use it concurrently.
// Do not replace Ext or return it directly while the scope owns it.
// The zero value holds no context and is safe to Release.
type ScopedExtSQE struct {
	Ext       *ExtSQE
	ring      *Uring
	submitted bool
}

// NewScopedExtSQE borrows a cleared ExtSQE into a scope.
// It returns a scope with nil Ext if the context pool is exhausted.
func (ur *Uring) NewScopedExtSQE() ScopedExtSQE {
	return ScopedExtSQE{
		Ext:  ur.ExtSQE(),
		ring: ur,
	}
}

// Valid reports whether Ext is non-nil.
// It does not check submission state or whether the context has been returned.
//
//go:nosplit
func (s *ScopedExtSQE) Valid() bool {
	return s.Ext != nil
}

// Submitted marks a successful submission so Release becomes a no-op.
// It does not submit or return the context. Completion handling must return it
// with [Uring.PutExtSQE] after all request completions and borrowed access finish.
//
//go:nosplit
func (s *ScopedExtSQE) Submitted() {
	s.submitted = true
}

// Release returns an unsubmitted context to its ring and clears Ext.
// Returning it invalidates all pointers and views into that borrow.
// Repeated calls and calls after [ScopedExtSQE.Submitted] are no-ops.
//
//go:nosplit
func (s *ScopedExtSQE) Release() {
	if s.Ext != nil && !s.submitted {
		s.ring.PutExtSQE(s.Ext)
		s.Ext = nil
	}
}

// WithExtSQE calls fn with a cleared ExtSQE borrowed from this ring.
// It returns [iox.ErrWouldBlock] without calling fn if the pool is exhausted.
//
// If fn returns an error, the context must still be unsubmitted and borrowed;
// WithExtSQE returns it to the pool before returning that error.
//
// If fn returns nil, fn is responsible for arranging [Uring.PutExtSQE] exactly
// once after all request completions and borrowed access finish. Views into the
// context must not outlive that return. A panic in fn does not return the context.
func (ur *Uring) WithExtSQE(fn func(ext *ExtSQE) error) error {
	ext := ur.ExtSQE()
	if ext == nil {
		return iox.ErrWouldBlock
	}

	err := fn(ext)
	if err != nil {
		ur.PutExtSQE(ext)
	}
	return err
}

// MustExtSQE gets an ExtSQE from the pool, panicking if exhausted.
func (ur *Uring) MustExtSQE() *ExtSQE {
	ext := ur.ExtSQE()
	if ext == nil {
		panic("uring: ExtSQE pool exhausted")
	}
	return ext
}
