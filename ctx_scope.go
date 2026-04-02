// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"code.hybscloud.com/iox"
)

// ScopedExtSQE manages ExtSQE lifecycle to prevent pool leaks.
// It ensures the ExtSQE is returned to the pool on error paths.
// The wrapped ExtSQE is borrowed until either Submitted transfers release
// responsibility to completion handling or Release returns it to the pool.
//
// Usage pattern:
//
//	scope := ring.NewScopedExtSQE()
//	if scope.Ext == nil {
//	    return iox.ErrWouldBlock
//	}
//	defer scope.Release()  // Returns to pool if not submitted
//
//	// ... setup ExtSQE ...
//
//	if err := ring.Submit(ctx); err != nil {
//	    return err  // Release() will return ExtSQE to pool
//	}
//	scope.Submitted()  // Mark as submitted, Release() becomes no-op
type ScopedExtSQE struct {
	Ext       *ExtSQE
	ring      *Uring
	submitted bool
}

// NewScopedExtSQE gets an ExtSQE from the pool wrapped in a scope.
// Returns a scope with nil Ext if the pool is exhausted.
// The borrowed ExtSQE and all derived UserData views remain valid only until
// Submitted or Release closes the scope.
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
// After this call, Release() becomes a no-op.
// The CQE handler is responsible for returning the ExtSQE to the pool, and any
// state that must outlive PutExtSQE must be copied before release.
//
//go:nosplit
func (s *ScopedExtSQE) Submitted() {
	s.submitted = true
}

// Release returns the ExtSQE to the pool if not submitted.
// This is safe to call multiple times and after Submitted().
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
// The ExtSQE is automatically returned to the pool if fn returns an error.
//
// If fn returns nil, the caller is responsible for eventually returning
// the ExtSQE to the pool (typically via CQE handler). Typed or raw UserData
// views produced inside fn are borrowed and must not escape past PutExtSQE.
//
// Returns iox.ErrWouldBlock if the pool is exhausted.
//
// Example:
//
//	err := ring.WithExtSQE(func(ext *uring.ExtSQE) error {
//	    ctx := uring.ViewCtx(ext).Vals0()
//	    ctx.Fn = handler
//	    ctx.Data = data
//
//	    sqeCtx := uring.PackExtended(ext)
//	    return ring.Read(sqeCtx, fd, buf)
//	})
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
// Use only in contexts where pool exhaustion is a programming error.
func (ur *Uring) MustExtSQE() *ExtSQE {
	ext := ur.ExtSQE()
	if ext == nil {
		panic("uring: ExtSQE pool exhausted")
	}
	return ext
}
