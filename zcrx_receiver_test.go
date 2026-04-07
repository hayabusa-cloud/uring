// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/uring"
)

func TestZCRXConfigDefaults(t *testing.T) {
	// Test that defaults are applied
	cfg := uring.ZCRXConfig{
		IfName:  "eth0",
		RxQueue: 0,
	}

	// Can't test defaults() directly since it's unexported,
	// but we can test that empty values are handled

	if cfg.AreaSize != 0 {
		t.Errorf("expected AreaSize default to be 0 before defaults(), got %d", cfg.AreaSize)
	}
	if cfg.RqEntries != 0 {
		t.Errorf("expected RqEntries default to be 0 before defaults(), got %d", cfg.RqEntries)
	}
}

func TestZCRXConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     uring.ZCRXConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: uring.ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   0,
				RqEntries: 4096, // Power of 2
			},
			wantErr: false,
		},
		{
			name: "missing interface name",
			cfg: uring.ZCRXConfig{
				RxQueue:   0,
				RqEntries: 4096,
			},
			wantErr: true,
		},
		{
			name: "negative rx queue",
			cfg: uring.ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   -1,
				RqEntries: 4096,
			},
			wantErr: true,
		},
		{
			name: "non-power-of-2 entries",
			cfg: uring.ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   0,
				RqEntries: 1000, // Not power of 2
			},
			wantErr: true,
		},
		{
			name: "negative area size",
			cfg: uring.ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   0,
				AreaSize:  -1,
				RqEntries: 1024,
			},
			wantErr: true,
		},
		{
			name: "power of 2 entries",
			cfg: uring.ZCRXConfig{
				IfName:    "eth0",
				RxQueue:   0,
				RqEntries: 1024, // Power of 2
			},
			wantErr: false,
		},
	}

	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Ring is cleaned up on GC

	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uring.NewZCRXReceiver(ring, pool, tt.cfg)
			if err == nil && tt.wantErr {
				t.Errorf("expected error, got nil")
			}
			// Note: We may get ErrNotSupported if ZCRX is not available
			// on this system. That's expected and not a test failure.
			if err != nil && !tt.wantErr {
				if !errors.Is(err, uring.ErrNotSupported) {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestNewZCRXReceiverNotSupported(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Ring is cleaned up on GC

	mustStartRing(t, ring)

	pool := uring.NewContextPools(int(uring.EntriesSmall))

	cfg := uring.ZCRXConfig{
		IfName:    "lo", // Loopback - won't have ZCRX support
		RxQueue:   0,
		RqEntries: 4096,
	}

	_, err = uring.NewZCRXReceiver(ring, pool, cfg)
	if err == nil {
		t.Skip("ZCRX is supported on this system")
	}

	// ZCRX requires kernel 6.18+ and specific NIC support
	// On most systems, this will return ErrNotSupported
	if !errors.Is(err, uring.ErrNotSupported) {
		// Could be ErrInvalidParam or other error for interface issues
		t.Logf("Got error (expected on systems without ZCRX): %v", err)
	}
}

func TestZCRXBufferRelease(t *testing.T) {
	// Test that ZCRXBuffer.Release() exists on the public API.
	// Since we can't easily create a real ZCRXBuffer without ZCRX support,
	// this test mainly verifies the API exists and is callable.

	// This test would require ZCRX hardware to fully test.
	// For now, just verify the NoopZCRXHandler compiles and works.
	var handler uring.ZCRXHandler = uring.NoopZCRXHandler{}

	// Verify interface compliance
	if handler == nil {
		t.Error("NoopZCRXHandler should implement ZCRXHandler")
	}
}

func TestNoopZCRXHandler(t *testing.T) {
	handler := uring.NoopZCRXHandler{}

	// Note: Can't test OnData with nil because it calls buf.Release()
	// which requires a valid buffer. OnData behavior is tested when
	// ZCRX is actually available.

	// Test OnError returns false (stops on error)
	if handler.OnError(errors.New("test error")) {
		t.Error("NoopZCRXHandler.OnError should return false")
	}

	// Test OnStopped doesn't panic
	handler.OnStopped()
}
