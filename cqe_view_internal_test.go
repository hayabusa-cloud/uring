// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "testing"

func TestCQEViewBufGroupForIndirectAndExtendedModes(t *testing.T) {
	t.Run("indirect with buffer select", func(t *testing.T) {
		sqe := &IndirectSQE{}
		sqe.flags = IOSQE_BUFFER_SELECT
		sqe.bufIndex = 23

		view := CQEView{ctx: PackIndirect(sqe)}
		if got := view.BufGroup(); got != 23 {
			t.Fatalf("BufGroup() = %d, want 23", got)
		}
	})

	t.Run("extended with buffer select", func(t *testing.T) {
		ext := &ExtSQE{}
		ext.SQE.flags = IOSQE_BUFFER_SELECT
		ext.SQE.bufIndex = 41

		view := CQEView{ctx: PackExtended(ext)}
		if got := view.BufGroup(); got != 41 {
			t.Fatalf("BufGroup() = %d, want 41", got)
		}
	})

	t.Run("non direct without buffer select", func(t *testing.T) {
		sqe := &IndirectSQE{}
		view := CQEView{ctx: PackIndirect(sqe)}
		if got := view.BufGroup(); got != 0 {
			t.Fatalf("BufGroup() = %d, want 0", got)
		}
	})
}
