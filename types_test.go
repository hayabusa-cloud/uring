// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"context"
	"net"
	"sync"
	"testing"
	"unsafe"
)

var _ sync.Locker = (*noCopy)(nil)

func TestWriteCString(t *testing.T) {
	t.Run("valid string", func(t *testing.T) {
		s := "hello"
		data := make([]byte, len(s)+1)
		if err := writeCString(data, s); err != nil {
			t.Fatalf("writeCString: %v", err)
		}

		// Verify the bytes
		for i := 0; i < len(s); i++ {
			if data[i] != s[i] {
				t.Errorf("byte %d: got %c, want %c", i, data[i], s[i])
			}
		}
		// Check NUL terminator
		if data[len(s)] != 0 {
			t.Error("missing NUL terminator")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		data := make([]byte, 1)
		if err := writeCString(data, ""); err != nil {
			t.Fatalf("writeCString: %v", err)
		}
		if data[0] != 0 {
			t.Error("empty string should be just NUL")
		}
	})

	t.Run("string with NUL", func(t *testing.T) {
		s := "hello\x00world"
		data := make([]byte, len(s)+1)
		err := writeCString(data, s)
		if err != ErrInvalidParam {
			t.Errorf("expected ErrInvalidParam, got %v", err)
		}
	})

	t.Run("short destination", func(t *testing.T) {
		data := make([]byte, 3)
		err := writeCString(data, "toolong")
		if err != ErrInvalidParam {
			t.Errorf("expected ErrInvalidParam, got %v", err)
		}
	})
}

func TestIoVecFromBytesSlice(t *testing.T) {
	t.Run("multiple buffers", func(t *testing.T) {
		bufs := [][]byte{
			[]byte("hello"),
			[]byte("world"),
			[]byte("!"),
		}
		ptr, n := ioVecFromBytesSlice(bufs)
		if n != 3 {
			t.Errorf("n = %d, want 3", n)
		}
		if ptr == nil {
			t.Error("ptr is nil")
		}
		// Note: Cannot verify iovec contents here - the internal slice is freed
		// after ioVecFromBytesSlice returns. This function is designed for
		// immediate syscall use where the kernel reads before Go GC runs.
	})

	t.Run("empty slice", func(t *testing.T) {
		bufs := [][]byte{}
		_, n := ioVecFromBytesSlice(bufs)
		if n != 0 {
			t.Errorf("n = %d, want 0", n)
		}
	})

	t.Run("single buffer", func(t *testing.T) {
		bufs := [][]byte{make([]byte, 4096)}
		ptr, n := ioVecFromBytesSlice(bufs)
		if n != 1 {
			t.Errorf("n = %d, want 1", n)
		}
		if ptr == nil {
			t.Error("ptr is nil")
		}
	})
}

func TestIoVecAddrLen(t *testing.T) {
	vecs := []IoVec{
		{Base: nil, Len: 100},
		{Base: nil, Len: 200},
	}
	ptr, n := ioVecAddrLen(vecs)
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
	if ptr == nil {
		t.Error("ptr is nil")
	}
}

func TestSockaddr(t *testing.T) {
	t.Run("nil sockaddr", func(t *testing.T) {
		ptr, length, err := sockaddr(nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if ptr != nil {
			t.Error("expected nil ptr")
		}
		if length != 0 {
			t.Errorf("expected 0 length, got %d", length)
		}
	})

	t.Run("TCP sockaddr", func(t *testing.T) {
		addr := &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 8080,
		}
		sa := AddrToSockaddr(addr)
		if sa == nil {
			t.Fatal("expected non-nil sockaddr")
		}

		ptr, length, err := sockaddr(sa)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if ptr == nil {
			t.Error("expected non-nil ptr")
		}
		if length == 0 {
			t.Error("expected non-zero length")
		}
	})
}

func TestSockaddrData(t *testing.T) {
	t.Run("nil sockaddr", func(t *testing.T) {
		data, err := sockaddrData(nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if data != nil {
			t.Error("expected nil data")
		}
	})

	t.Run("TCP sockaddr", func(t *testing.T) {
		addr := &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 8080,
		}
		sa := AddrToSockaddr(addr)
		if sa == nil {
			t.Fatal("expected non-nil sockaddr")
		}

		data, err := sockaddrData(sa)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if data == nil {
			t.Error("expected non-nil data")
		}
		if len(data) == 0 {
			t.Error("expected non-empty data")
		}
		rawPtr, _ := sa.Raw()
		if unsafe.Pointer(unsafe.SliceData(data)) != rawPtr {
			t.Error("expected sockaddrData to return a borrowed view")
		}
	})
}

func TestStructSizes(t *testing.T) {
	// Verify critical struct sizes match kernel expectations
	tests := []struct {
		name     string
		size     uintptr
		expected uintptr
	}{
		{"IoVec", unsafe.Sizeof(IoVec{}), 16},
		{"Msghdr", unsafe.Sizeof(Msghdr{}), 56},
		{"OpenHow", unsafe.Sizeof(OpenHow{}), uintptr(SizeofOpenHow)},
		{"EpollEvent", unsafe.Sizeof(EpollEvent{}), sizeofEpollEvent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.size != tt.expected {
				t.Errorf("sizeof(%s) = %d, want %d", tt.name, tt.size, tt.expected)
			}
		})
	}
}

func TestEpollEventLayout(t *testing.T) {
	if off := unsafe.Offsetof(EpollEvent{}.Fd); off != epollEventFDOffset {
		t.Fatalf("offsetof(EpollEvent.Fd) = %d, want %d", off, epollEventFDOffset)
	}
	if stride := unsafe.Sizeof([2]EpollEvent{}); stride != 2*sizeofEpollEvent {
		t.Fatalf("EpollEvent array stride = %d, want %d", stride, 2*sizeofEpollEvent)
	}
}

func TestConstants(t *testing.T) {
	// Verify important constants
	if AT_FDCWD != -100 {
		t.Errorf("AT_FDCWD = %d, want -100", AT_FDCWD)
	}
	if O_LARGEFILE != 0x8000 {
		t.Errorf("O_LARGEFILE = %x, want 0x8000", O_LARGEFILE)
	}
	if SizeofOpenHow != 24 {
		t.Errorf("SizeofOpenHow = %d, want 24", SizeofOpenHow)
	}
}

// Benchmarks

func BenchmarkWriteCString(b *testing.B) {
	s := "/path/to/some/file.txt"
	dst := make([]byte, len(s)+1)
	b.ReportAllocs()

	for b.Loop() {
		_ = writeCString(dst, s)
	}
}

func BenchmarkIoVecFromBytesSlice(b *testing.B) {
	bufs := [][]byte{
		make([]byte, 4096),
		make([]byte, 4096),
		make([]byte, 4096),
	}
	b.ReportAllocs()

	for b.Loop() {
		_, _ = ioVecFromBytesSlice(bufs)
	}
}

func BenchmarkContextUserData(b *testing.B) {
	ctx := context.Background()
	ctx = ContextWithUserData(ctx, 42)
	b.ReportAllocs()

	for b.Loop() {
		_ = ContextUserData[int](ctx)
	}
}

func BenchmarkAddrToSockaddr(b *testing.B) {
	addr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 8080,
	}
	b.ReportAllocs()

	for b.Loop() {
		_ = AddrToSockaddr(addr)
	}
}
