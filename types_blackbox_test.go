// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"context"
	"net"
	"testing"

	"code.hybscloud.com/uring"
)

func TestContextUserDataBlackBox(t *testing.T) {
	t.Run("string userdata", func(t *testing.T) {
		ctx := context.Background()
		ctx = uring.ContextWithUserData(ctx, "test-value")

		got := uring.ContextUserData[string](ctx)
		if got != "test-value" {
			t.Errorf("got %q, want %q", got, "test-value")
		}
	})

	t.Run("int userdata", func(t *testing.T) {
		ctx := context.Background()
		ctx = uring.ContextWithUserData(ctx, 42)

		got := uring.ContextUserData[int](ctx)
		if got != 42 {
			t.Errorf("got %d, want 42", got)
		}
	})

	t.Run("struct userdata", func(t *testing.T) {
		type myData struct {
			ID   int
			Name string
		}
		ctx := context.Background()
		data := myData{ID: 1, Name: "test"}
		ctx = uring.ContextWithUserData(ctx, data)

		got := uring.ContextUserData[myData](ctx)
		if got != data {
			t.Errorf("got %+v, want %+v", got, data)
		}
	})

	t.Run("missing userdata returns zero", func(t *testing.T) {
		ctx := context.Background()
		got := uring.ContextUserData[string](ctx)
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}

		gotInt := uring.ContextUserData[int](ctx)
		if gotInt != 0 {
			t.Errorf("got %d, want 0", gotInt)
		}
	})

	t.Run("multiple types", func(t *testing.T) {
		ctx := context.Background()
		ctx = uring.ContextWithUserData(ctx, "string-value")
		ctx = uring.ContextWithUserData(ctx, 123)
		ctx = uring.ContextWithUserData(ctx, true)

		if uring.ContextUserData[string](ctx) != "string-value" {
			t.Error("string userdata wrong")
		}
		if uring.ContextUserData[int](ctx) != 123 {
			t.Error("int userdata wrong")
		}
		if uring.ContextUserData[bool](ctx) != true {
			t.Error("bool userdata wrong")
		}
	})
}

func TestAddrToSockaddrBlackBox(t *testing.T) {
	t.Run("nil addr", func(t *testing.T) {
		sa := uring.AddrToSockaddr(nil)
		if sa != nil {
			t.Errorf("expected nil, got %v", sa)
		}
	})

	t.Run("TCP addr IPv4", func(t *testing.T) {
		addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
		sa := uring.AddrToSockaddr(addr)
		if sa == nil {
			t.Fatal("expected non-nil sockaddr")
		}
	})

	t.Run("TCP addr IPv6", func(t *testing.T) {
		addr := &net.TCPAddr{IP: net.IPv6loopback, Port: 8080}
		sa := uring.AddrToSockaddr(addr)
		if sa == nil {
			t.Fatal("expected non-nil sockaddr")
		}
	})

	t.Run("UDP addr", func(t *testing.T) {
		addr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 53}
		sa := uring.AddrToSockaddr(addr)
		if sa == nil {
			t.Fatal("expected non-nil sockaddr")
		}
	})

	t.Run("Unix addr", func(t *testing.T) {
		addr := &net.UnixAddr{Name: "/tmp/test.sock", Net: "unix"}
		sa := uring.AddrToSockaddr(addr)
		if sa == nil {
			t.Fatal("expected non-nil sockaddr")
		}
	})

	t.Run("unknown addr type", func(t *testing.T) {
		addr := &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}
		sa := uring.AddrToSockaddr(addr)
		if sa != nil {
			t.Errorf("expected nil for unknown addr type, got %v", sa)
		}
	})
}
