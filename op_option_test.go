// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/uring"
)

// =============================================================================
// OpOption Tests
// =============================================================================

func TestUringOpOptionApply(t *testing.T) {
	opt := &uring.OpOption{}

	// Apply multiple options
	opt.Apply(
		uring.WithFileIndex(42),
	)

	if opt.FileIndex != 42 {
		t.Errorf("FileIndex: got %d, want 42", opt.FileIndex)
	}
}

func TestWithFileIndex(t *testing.T) {
	opt := &uring.OpOption{}
	fn := uring.WithFileIndex(123)
	fn(opt)

	if opt.FileIndex != 123 {
		t.Errorf("FileIndex: got %d, want 123", opt.FileIndex)
	}
}
