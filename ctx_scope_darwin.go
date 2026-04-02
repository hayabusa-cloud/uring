// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"code.hybscloud.com/iox"
)

// ScopedExtSQE manages ExtSQE lifecycle to prevent pool leaks.
// The wrapped ExtSQE is borrowed until either Submitted transfers release
// responsibility to completion handling or Release returns it to the pool.
type ScopedExtSQE struct {
	Ext       *ExtSQE
	ring      *Uring
	submitted bool
}

// NewScopedExtSQE gets an ExtSQE from the pool wrapped in a scope.
func (ur *Uring) NewScopedExtSQE() ScopedExtSQE {
	return ScopedExtSQE{
		Ext:  ur.ExtSQE(),
		ring: ur,
	}
}

// Valid reports whether the scope has a valid ExtSQE.
//
//go:nosplit
func (s *ScopedExtSQE) Valid() bool {
	return s.Ext != nil
}

// Submitted marks the ExtSQE as submitted.
//
//go:nosplit
func (s *ScopedExtSQE) Submitted() {
	s.submitted = true
}

// Release returns the ExtSQE to the pool if not submitted.
// After Release, the ExtSQE and every typed/raw view derived from it are invalid.
//
//go:nosplit
func (s *ScopedExtSQE) Release() {
	if s.Ext != nil && !s.submitted {
		s.ring.PutExtSQE(s.Ext)
		s.Ext = nil
	}
}

// WithExtSQE executes fn with an ExtSQE from the pool.
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
