// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/uring"
)

func TestPackDirectBlackBox(t *testing.T) {
	tests := []struct {
		name     string
		op       uint8
		flags    uint8
		bufGroup uint16
		fd       int32
	}{
		{"zero values", 0, 0, 0, 0},
		{"typical accept", uring.IORING_OP_ACCEPT, 0, 0, 5},
		{"recv with buffer select", uring.IORING_OP_RECV, uring.IOSQE_BUFFER_SELECT, 1, 10},
		{"send with cqe skip", uring.IORING_OP_SEND, uring.IOSQE_CQE_SKIP_SUCCESS, 0, 100},
		{"max values", 255, 255, 65535, 536870911},
		{"negative fd", uring.IORING_OP_CLOSE, 0, 0, -1},
		{"all flags", uring.IORING_OP_NOP, uring.IOSQE_FIXED_FILE | uring.IOSQE_IO_DRAIN | uring.IOSQE_IO_LINK, 100, 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := uring.PackDirect(tt.op, tt.flags, tt.bufGroup, tt.fd)

			if got := ctx.Op(); got != tt.op {
				t.Errorf("Op() = %d, want %d", got, tt.op)
			}
			if got := ctx.Flags(); got != tt.flags {
				t.Errorf("Flags() = %d, want %d", got, tt.flags)
			}
			if got := ctx.BufGroup(); got != tt.bufGroup {
				t.Errorf("BufGroup() = %d, want %d", got, tt.bufGroup)
			}
			if got := ctx.FD(); got != tt.fd {
				t.Errorf("FD() = %d, want %d", got, tt.fd)
			}
		})
	}
}

func TestSQEContextWithBlackBox(t *testing.T) {
	base := uring.PackDirect(uring.IORING_OP_READ, uring.IOSQE_FIXED_FILE, 10, 5)

	t.Run("WithOp", func(t *testing.T) {
		ctx := base.WithOp(uring.IORING_OP_WRITE)
		if ctx.Op() != uring.IORING_OP_WRITE {
			t.Errorf("WithOp failed: got %d, want %d", ctx.Op(), uring.IORING_OP_WRITE)
		}
		if ctx.Flags() != base.Flags() {
			t.Error("WithOp modified flags")
		}
		if ctx.BufGroup() != base.BufGroup() {
			t.Error("WithOp modified bufGroup")
		}
		if ctx.FD() != base.FD() {
			t.Error("WithOp modified fd")
		}
	})

	t.Run("WithFlags", func(t *testing.T) {
		ctx := base.WithFlags(uring.IOSQE_IO_LINK)
		if ctx.Flags() != uring.IOSQE_IO_LINK {
			t.Errorf("WithFlags failed: got %d, want %d", ctx.Flags(), uring.IOSQE_IO_LINK)
		}
		if ctx.Op() != base.Op() {
			t.Error("WithFlags modified op")
		}
	})

	t.Run("WithBufGroup", func(t *testing.T) {
		ctx := base.WithBufGroup(200)
		if ctx.BufGroup() != 200 {
			t.Errorf("WithBufGroup failed: got %d, want %d", ctx.BufGroup(), 200)
		}
		if ctx.Op() != base.Op() {
			t.Error("WithBufGroup modified op")
		}
	})

	t.Run("WithFD", func(t *testing.T) {
		ctx := base.WithFD(iofd.FD(999))
		if ctx.FD() != 999 {
			t.Errorf("WithFD failed: got %d, want %d", ctx.FD(), 999)
		}
		if ctx.Op() != base.Op() {
			t.Error("WithFD modified op")
		}
	})

	t.Run("chained", func(t *testing.T) {
		ctx := base.WithOp(uring.IORING_OP_SEND).WithFlags(0).WithBufGroup(50).WithFD(iofd.FD(123))
		if ctx.Op() != uring.IORING_OP_SEND {
			t.Errorf("chained Op failed")
		}
		if ctx.Flags() != 0 {
			t.Errorf("chained Flags failed")
		}
		if ctx.BufGroup() != 50 {
			t.Errorf("chained BufGroup failed")
		}
		if ctx.FD() != 123 {
			t.Errorf("chained FD failed")
		}
	})
}

func TestSQEContextHasBufferSelectBlackBox(t *testing.T) {
	tests := []struct {
		name     string
		flags    uint8
		expected bool
	}{
		{"no flags", 0, false},
		{"buffer select", uring.IOSQE_BUFFER_SELECT, true},
		{"other flags", uring.IOSQE_FIXED_FILE | uring.IOSQE_IO_LINK, false},
		{"buffer select with others", uring.IOSQE_BUFFER_SELECT | uring.IOSQE_IO_DRAIN, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := uring.PackDirect(0, tt.flags, 0, 0)
			if got := ctx.HasBufferSelect(); got != tt.expected {
				t.Errorf("HasBufferSelect() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSQEContextFromRawBlackBox(t *testing.T) {
	original := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_BUFFER_SELECT, 42, 100)
	raw := original.Raw()
	restored := uring.SQEContextFromRaw(raw)

	if restored.Op() != original.Op() {
		t.Errorf("Op mismatch: got %d, want %d", restored.Op(), original.Op())
	}
	if restored.Flags() != original.Flags() {
		t.Errorf("Flags mismatch: got %d, want %d", restored.Flags(), original.Flags())
	}
	if restored.BufGroup() != original.BufGroup() {
		t.Errorf("BufGroup mismatch: got %d, want %d", restored.BufGroup(), original.BufGroup())
	}
	if restored.FD() != original.FD() {
		t.Errorf("FD mismatch: got %d, want %d", restored.FD(), original.FD())
	}
}

func TestSQEContextBitLayoutBlackBox(t *testing.T) {
	ctx := uring.PackDirect(0xAB, 0xCD, 0x1234, 0x16789ABC)
	raw := ctx.Raw()

	if uint8(raw&0xFF) != 0xAB {
		t.Errorf("Op bits wrong: got %02X, want AB", uint8(raw&0xFF))
	}
	if uint8((raw>>8)&0xFF) != 0xCD {
		t.Errorf("Flags bits wrong: got %02X, want CD", uint8((raw>>8)&0xFF))
	}
	if uint16((raw>>16)&0xFFFF) != 0x1234 {
		t.Errorf("BufGroup bits wrong: got %04X, want 1234", uint16((raw>>16)&0xFFFF))
	}
	fdBits := (raw >> 32) & 0x3FFFFFFF
	if fdBits != 0x16789ABC {
		t.Errorf("FD bits wrong: got %08X, want 16789ABC", fdBits)
	}
	if modeBits := raw >> 62; modeBits != 0 {
		t.Errorf("Mode bits wrong: got %d, want 0 (Direct)", modeBits)
	}
}

func TestSQEContextModesBlackBox(t *testing.T) {
	t.Run("Direct mode", func(t *testing.T) {
		ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 5)
		if !ctx.IsDirect() {
			t.Error("expected IsDirect() to be true")
		}
		if ctx.IsIndirect() {
			t.Error("expected IsIndirect() to be false")
		}
		if ctx.IsExtended() {
			t.Error("expected IsExtended() to be false")
		}
		if ctx.Mode() != uring.CtxModeDirect {
			t.Errorf("Mode() = %d, want %d", ctx.Mode(), uring.CtxModeDirect)
		}
	})

	t.Run("FD sign extension", func(t *testing.T) {
		ctx := uring.PackDirect(0, 0, 0, -1)
		if ctx.FD() != -1 {
			t.Errorf("FD() = %d, want -1", ctx.FD())
		}

		ctx = uring.PackDirect(0, 0, 0, -100)
		if ctx.FD() != -100 {
			t.Errorf("FD() = %d, want -100", ctx.FD())
		}

		ctx = uring.PackDirect(0, 0, 0, 12345)
		if ctx.FD() != 12345 {
			t.Errorf("FD() = %d, want 12345", ctx.FD())
		}
	})
}
