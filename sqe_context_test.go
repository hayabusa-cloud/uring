// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"math"
	"testing"
)

func TestContextPoolsCapacityRounding(t *testing.T) {
	tests := []struct {
		requested int
		want      int
	}{
		{requested: 1, want: 1},
		{requested: 2, want: 2},
		{requested: 3, want: 4},
		{requested: 5, want: 8},
		{requested: 15, want: 16},
		{requested: 33, want: 64},
	}

	for _, tt := range tests {
		pools := NewContextPools(tt.requested)
		if got := pools.Capacity(); got != tt.want {
			t.Fatalf("Capacity() for request %d = %d, want %d", tt.requested, got, tt.want)
		}
	}
}

func TestContextPoolsCapacityPanics(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
	}{
		{name: "zero", capacity: 0},
		{name: "negative", capacity: -1},
		{name: "too-large", capacity: int(uint64(math.MaxUint32) + 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewContextPools(%d) did not panic", tt.capacity)
				}
			}()
			_ = NewContextPools(tt.capacity)
		})
	}
}

func TestContextPoolsOutOfOrderPut(t *testing.T) {
	pools := NewContextPools(2)

	indirect0 := pools.Indirect()
	indirect1 := pools.Indirect()
	if indirect0 == nil || indirect1 == nil {
		t.Fatal("Indirect() returned nil before exhaustion")
	}
	if got := pools.Indirect(); got != nil {
		t.Fatalf("Indirect() after exhaustion = %p, want nil", got)
	}

	pools.PutIndirect(indirect1)
	if got := pools.Indirect(); got != indirect1 {
		t.Fatalf("Indirect() after out-of-order PutIndirect = %p, want %p", got, indirect1)
	}
	pools.PutIndirect(indirect0)

	ext0 := pools.Extended()
	ext1 := pools.Extended()
	if ext0 == nil || ext1 == nil {
		t.Fatal("Extended() returned nil before exhaustion")
	}
	if got := pools.Extended(); got != nil {
		t.Fatalf("Extended() after exhaustion = %p, want nil", got)
	}

	pools.PutExtended(ext1)
	if got := pools.Extended(); got != ext1 {
		t.Fatalf("Extended() after out-of-order PutExtended = %p, want %p", got, ext1)
	}
	pools.PutExtended(ext0)
}

func TestContextPoolsResetRestoresCapacity(t *testing.T) {
	pools := NewContextPools(3)

	for range pools.Capacity() {
		if pools.Indirect() == nil {
			t.Fatal("Indirect() returned nil before exhaustion")
		}
		if pools.Extended() == nil {
			t.Fatal("Extended() returned nil before exhaustion")
		}
	}
	if got := pools.Indirect(); got != nil {
		t.Fatalf("Indirect() after exhaustion = %p, want nil", got)
	}
	if got := pools.Extended(); got != nil {
		t.Fatalf("Extended() after exhaustion = %p, want nil", got)
	}

	pools.Reset()

	if got := pools.IndirectAvailable(); got != pools.Capacity() {
		t.Fatalf("IndirectAvailable() after Reset = %d, want %d", got, pools.Capacity())
	}
	if got := pools.ExtendedAvailable(); got != pools.Capacity() {
		t.Fatalf("ExtendedAvailable() after Reset = %d, want %d", got, pools.Capacity())
	}
	if pools.Indirect() == nil {
		t.Fatal("Indirect() after Reset = nil, want non-nil")
	}
	if pools.Extended() == nil {
		t.Fatal("Extended() after Reset = nil, want non-nil")
	}
}

func TestSQEView(t *testing.T) {
	t.Run("ViewSQE from IndirectSQE", func(t *testing.T) {
		indirect := &IndirectSQE{}
		indirect.opcode = IORING_OP_RECV
		indirect.flags = IOSQE_BUFFER_SELECT | IOSQE_ASYNC
		indirect.ioprio = 42
		indirect.fd = 100
		indirect.off = 0x1234567890
		indirect.addr = 0xDEADBEEF
		indirect.len = 4096
		indirect.uflags = 0x8000
		indirect.bufIndex = 7
		indirect.personality = 3
		indirect.spliceFdIn = 50

		view := ViewSQE(indirect)

		if !view.Valid() {
			t.Error("expected Valid() to be true")
		}
		if view.Opcode() != IORING_OP_RECV {
			t.Errorf("Opcode() = %d, want %d", view.Opcode(), IORING_OP_RECV)
		}
		if view.Flags() != IOSQE_BUFFER_SELECT|IOSQE_ASYNC {
			t.Errorf("Flags() = %d, want %d", view.Flags(), IOSQE_BUFFER_SELECT|IOSQE_ASYNC)
		}
		if view.IoPrio() != 42 {
			t.Errorf("IoPrio() = %d, want 42", view.IoPrio())
		}
		if view.RawFD() != 100 {
			t.Errorf("RawFD() = %d, want 100", view.RawFD())
		}
		if view.Off() != 0x1234567890 {
			t.Errorf("Off() = %x, want 0x1234567890", view.Off())
		}
		if view.Addr() != 0xDEADBEEF {
			t.Errorf("Addr() = %x, want 0xDEADBEEF", view.Addr())
		}
		if view.Len() != 4096 {
			t.Errorf("Len() = %d, want 4096", view.Len())
		}
		if view.UFlags() != 0x8000 {
			t.Errorf("UFlags() = %x, want 0x8000", view.UFlags())
		}
		if view.BufIndex() != 7 {
			t.Errorf("BufIndex() = %d, want 7", view.BufIndex())
		}
		if view.Personality() != 3 {
			t.Errorf("Personality() = %d, want 3", view.Personality())
		}
		if view.SpliceFDIn() != 50 {
			t.Errorf("SpliceFDIn() = %d, want 50", view.SpliceFDIn())
		}
	})

	t.Run("ViewExtSQE from ExtSQE", func(t *testing.T) {
		ext := &ExtSQE{}
		ext.SQE.opcode = IORING_OP_SEND
		ext.SQE.flags = IOSQE_CQE_SKIP_SUCCESS
		ext.SQE.fd = 200

		view := ViewExtSQE(ext)

		if !view.Valid() {
			t.Error("expected Valid() to be true")
		}
		if view.Opcode() != IORING_OP_SEND {
			t.Errorf("Opcode() = %d, want %d", view.Opcode(), IORING_OP_SEND)
		}
		if view.RawFD() != 200 {
			t.Errorf("RawFD() = %d, want 200", view.RawFD())
		}
		if !view.HasCQESkipSuccess() {
			t.Error("expected HasCQESkipSuccess() to be true")
		}
	})

	t.Run("flag helpers", func(t *testing.T) {
		indirect := &IndirectSQE{}

		// Test each flag helper
		indirect.flags = IOSQE_BUFFER_SELECT
		view := ViewSQE(indirect)
		if !view.HasBufferSelect() {
			t.Error("expected HasBufferSelect() true")
		}

		indirect.flags = IOSQE_FIXED_FILE
		view = ViewSQE(indirect)
		if !view.HasFixedFile() {
			t.Error("expected HasFixedFile() true")
		}

		indirect.flags = IOSQE_IO_DRAIN
		view = ViewSQE(indirect)
		if !view.HasIODrain() {
			t.Error("expected HasIODrain() true")
		}

		indirect.flags = IOSQE_IO_LINK
		view = ViewSQE(indirect)
		if !view.HasIOLink() {
			t.Error("expected HasIOLink() true")
		}

		indirect.flags = IOSQE_IO_HARDLINK
		view = ViewSQE(indirect)
		if !view.HasIOHardlink() {
			t.Error("expected HasIOHardlink() true")
		}

		indirect.flags = IOSQE_ASYNC
		view = ViewSQE(indirect)
		if !view.HasAsync() {
			t.Error("expected HasAsync() true")
		}
	})

	t.Run("invalid view", func(t *testing.T) {
		view := SQEView{}
		if view.Valid() {
			t.Error("expected Valid() to be false for zero view")
		}
	})

	t.Run("FD and FileIndex", func(t *testing.T) {
		indirect := &IndirectSQE{}
		indirect.fd = 42
		indirect.spliceFdIn = 99

		view := ViewSQE(indirect)
		if view.FD() != 42 {
			t.Errorf("FD() = %d, want 42", view.FD())
		}
		if view.FileIndex() != 99 {
			t.Errorf("FileIndex() = %d, want 99", view.FileIndex())
		}
	})

	t.Run("UserData and BufGroup", func(t *testing.T) {
		indirect := &IndirectSQE{}
		indirect.userData = 0xDEADBEEF12345678
		indirect.bufIndex = 123

		view := ViewSQE(indirect)
		if view.UserData() != 0xDEADBEEF12345678 {
			t.Errorf("UserData() = %x, want 0xDEADBEEF12345678", view.UserData())
		}
		if view.BufGroup() != 123 {
			t.Errorf("BufGroup() = %d, want 123", view.BufGroup())
		}
	})
}

func BenchmarkSQEView_Opcode(b *testing.B) {
	indirect := &IndirectSQE{}
	indirect.opcode = IORING_OP_RECV
	indirect.fd = 100
	view := ViewSQE(indirect)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = view.Opcode()
	}
}

func BenchmarkSQEView_AllFields(b *testing.B) {
	indirect := &IndirectSQE{}
	indirect.opcode = IORING_OP_RECV
	indirect.flags = IOSQE_BUFFER_SELECT
	indirect.fd = 100
	indirect.len = 4096
	view := ViewSQE(indirect)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = view.Opcode()
		_ = view.Flags()
		_ = view.RawFD()
		_ = view.Len()
		_ = view.HasBufferSelect()
	}
}

// TestForFD verifies the ForFD convenience constructor.
func TestForFD(t *testing.T) {
	tests := []struct {
		name string
		fd   int32
	}{
		{"zero fd", 0},
		{"positive fd", 42},
		{"negative fd", -1},
		{"large fd", 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ForFD(tt.fd)

			// FD should be set
			if got := ctx.FD(); got != tt.fd {
				t.Errorf("ForFD(%d).FD() = %d, want %d", tt.fd, got, tt.fd)
			}

			// All other fields should be zero
			if got := ctx.Op(); got != 0 {
				t.Errorf("ForFD(%d).Op() = %d, want 0", tt.fd, got)
			}
			if got := ctx.Flags(); got != 0 {
				t.Errorf("ForFD(%d).Flags() = %d, want 0", tt.fd, got)
			}
			if got := ctx.BufGroup(); got != 0 {
				t.Errorf("ForFD(%d).BufGroup() = %d, want 0", tt.fd, got)
			}

			// Should be Direct mode
			if !ctx.IsDirect() {
				t.Errorf("ForFD(%d).IsDirect() = false, want true", tt.fd)
			}
		})
	}
}

// TestForFD_Chaining verifies ForFD works with With* methods.
func TestForFD_Chaining(t *testing.T) {
	ctx := ForFD(100).WithOp(IORING_OP_READ).WithFlags(IOSQE_ASYNC)

	if got := ctx.FD(); got != 100 {
		t.Errorf("FD() = %d, want 100", got)
	}
	if got := ctx.Op(); got != IORING_OP_READ {
		t.Errorf("Op() = %d, want %d", got, IORING_OP_READ)
	}
	if got := ctx.Flags(); got != IOSQE_ASYNC {
		t.Errorf("Flags() = %d, want %d", got, IOSQE_ASYNC)
	}
}
