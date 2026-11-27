// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"runtime"
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// =============================================================================
// Receive Bundle Tests
// =============================================================================

// Both receives return one byte. Only the selected buffer can be recycled.
func TestReceiveBundle(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) { opt.MultiSizeBuffer = 1 })
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	if n, errno := writeTestFD(fds[1], []byte("ab")); n != 2 || errno != 0 {
		t.Fatalf("write: n=%d errno=%d", n, errno)
	}

	ctx := uring.ForFD(int32(fds[0]))
	var dst [1]byte
	var pin runtime.Pinner
	pin.Pin(&dst[0])
	defer pin.Unpin()
	if err := ring.ReceiveFD(ctx, fds[0], dst[:]); err != nil {
		t.Fatal(err)
	}
	ordinary := waitForCQEs(t, ring, 1)[0]
	if ordinary.Res != 1 || ordinary.HasBuffer() || dst[0] != 'a' {
		t.Fatalf("caller buffer: result=%d flags=%#x byte=%q", ordinary.Res, ordinary.Flags, dst[0])
	}

	if err := ring.ReceiveBundle(ctx, &fds[0]); err != nil {
		t.Fatal(err)
	}
	selected := waitForCQEs(t, ring, 1)[0]
	if selected.Res != 1 || !selected.HasBuffer() || selected.BufID() != 0 {
		t.Fatalf("first selected buffer: result=%d flags=%#x", selected.Res, selected.Flags)
	}
	group := selected.BufGroup()
	it, ok := ring.BundleIterator(selected, group)
	if !ok || it.Count() != 1 || string(it.Buffer(0)) != "b" {
		t.Fatal("missing data from selected slot zero")
	}
	if err := it.Recycle(ring); err != nil {
		t.Fatal(err)
	}
	if it, ok := ring.BundleIterator(ordinary, group); ok || it.Count() != 0 {
		t.Fatal("caller-buffer receive returned a managed iterator")
	}
}

// testPollFd implements PollFd interface for testing
type testPollFd struct {
	fd iofd.FD
}

func (p testPollFd) Fd() int          { return int(p.fd) }
func (p testPollFd) Events() uint32   { return 0 }
func (p testPollFd) SetEvents(uint32) {}

// TestBundleCQEResult verifies CQE result interpretation for bundles.
func TestBundleCQEResult(t *testing.T) {
	// Test BundleCount calculation
	testCases := []struct {
		res        int32
		bufferSize int
		wantCount  int
	}{
		{res: 0, bufferSize: 1024, wantCount: 0},               // No data
		{res: 1024, bufferSize: 1024, wantCount: 1},            // Exactly one buffer
		{res: 1025, bufferSize: 1024, wantCount: 2},            // Partial second buffer
		{res: 2048, bufferSize: 1024, wantCount: 2},            // Exactly two buffers
		{res: 4096, bufferSize: 1024, wantCount: 4},            // Four buffers
		{res: -1, bufferSize: 1024, wantCount: 0},              // Error result
		{res: 100, bufferSize: 0, wantCount: 0},                // Invalid buffer size
		{res: 100, bufferSize: -1, wantCount: 0},               // Negative buffer size
		{res: 2, bufferSize: int(^uint(0) >> 1), wantCount: 1}, // Overflow-free ceiling division
	}

	for _, tc := range testCases {
		cqe := uring.CQEView{
			Res:   tc.res,
			Flags: 0,
		}

		count := cqe.BundleCount(tc.bufferSize)
		if count != tc.wantCount {
			t.Errorf("BundleCount(res=%d, bufSize=%d): got %d, want %d",
				tc.res, tc.bufferSize, count, tc.wantCount)
		}
	}
}

// TestBundleBufferContiguity verifies that bundle buffer ring slots are contiguous.
func TestBundleBufferContiguity(t *testing.T) {
	// Test BundleBuffers helper
	testCases := []struct {
		flags       uint32
		res         int32
		bufferSize  int
		wantStartID uint16
		wantCount   int
	}{
		{
			flags:       5 << uring.IORING_CQE_BUFFER_SHIFT, // Buffer ID 5
			res:         3072,
			bufferSize:  1024,
			wantStartID: 5,
			wantCount:   3,
		},
		{
			flags:       0 << uring.IORING_CQE_BUFFER_SHIFT, // Buffer ID 0
			res:         1024,
			bufferSize:  1024,
			wantStartID: 0,
			wantCount:   1,
		},
		{
			flags:       100 << uring.IORING_CQE_BUFFER_SHIFT, // Buffer ID 100
			res:         5000,
			bufferSize:  1024,
			wantStartID: 100,
			wantCount:   5, // ceil(5000/1024) = 5
		},
	}

	for _, tc := range testCases {
		cqe := uring.CQEView{
			Res:   tc.res,
			Flags: tc.flags,
		}

		startID, count := cqe.BundleBuffers(tc.bufferSize)
		if startID != tc.wantStartID {
			t.Errorf("BundleBuffers startID: got %d, want %d", startID, tc.wantStartID)
		}
		if count != tc.wantCount {
			t.Errorf("BundleBuffers count: got %d, want %d", count, tc.wantCount)
		}
	}
}

// TestBundleStartID verifies BundleStartID extraction from CQE flags.
func TestBundleStartID(t *testing.T) {
	testCases := []struct {
		flags     uint32
		wantBufID uint16
	}{
		{flags: 0, wantBufID: 0},
		{flags: 1 << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 1},
		{flags: 255 << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 255},
		{flags: 0xFFFF << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 0xFFFF},
		// With other flags set
		{
			flags:     (42 << uring.IORING_CQE_BUFFER_SHIFT) | uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER,
			wantBufID: 42,
		},
	}

	for _, tc := range testCases {
		cqe := uring.CQEView{
			Flags: tc.flags,
		}
		got := cqe.BundleStartID()
		if got != tc.wantBufID {
			t.Errorf("BundleStartID(flags=0x%x): got %d, want %d", tc.flags, got, tc.wantBufID)
		}
	}
}

// Each data CQE returns a buffer while MORE keeps the multishot receive live.
func TestSubmitReceiveBundleMultishot(t *testing.T) {
	ring := newTestRing(t, func(opt *uring.Options) { opt.MultiSizeBuffer = 1 })
	fds, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(fds)
	ctx := uring.ForFD(int32(fds[0]))
	if err := ring.SubmitReceiveBundleMultishot(ctx, &fds[0], uring.WithReadBufferSize(0)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []byte("ab") {
		if n, errno := writeTestFD(fds[1], []byte{want}); n != 1 || errno != 0 {
			t.Fatalf("write: n=%d errno=%d", n, errno)
		}
		cqe := waitForCQEs(t, ring, 1)[0]
		if cqe.Res != 1 || !cqe.HasMore() || !cqe.HasBuffer() {
			t.Fatalf("data: result=%d flags=%#x", cqe.Res, cqe.Flags)
		}
		it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
		if !ok || it.Count() != 1 || it.Buffer(0)[0] != want {
			t.Fatalf("missing selected byte %q", want)
		}
		if err := it.Recycle(ring); err != nil {
			t.Fatal(err)
		}
	}

	if errno := zcall.Shutdown(uintptr(fds[1]), zcall.SHUT_WR); errno != 0 {
		t.Fatal(zcall.Errno(errno))
	}
	eof := waitForCQEs(t, ring, 1)[0]
	if eof.Res != 0 || eof.HasMore() {
		t.Fatalf("EOF: result=%d flags=%#x", eof.Res, eof.Flags)
	}
}

// TestBundleFeatureDetection verifies that bundle APIs are present on the Linux 6.18+ baseline.
func TestBundleFeatureDetection(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.Features == nil {
		t.Fatal("ring.Features = nil")
	}
}

// =============================================================================
// CQE Flag Helper Tests
// =============================================================================

// TestCQEFlagHelpers verifies all CQE flag extraction methods.
func TestCQEFlagHelpers(t *testing.T) {
	testCases := []struct {
		name             string
		flags            uint32
		wantHasMore      bool
		wantHasBuffer    bool
		wantIsNotif      bool
		wantBufMore      bool
		wantSockNonEmpty bool
	}{
		{
			name:  "no flags",
			flags: 0,
		},
		{
			name:        "MORE flag",
			flags:       uring.IORING_CQE_F_MORE,
			wantHasMore: true,
		},
		{
			name:          "BUFFER flag",
			flags:         uring.IORING_CQE_F_BUFFER,
			wantHasBuffer: true,
		},
		{
			name:        "NOTIF flag",
			flags:       uring.IORING_CQE_F_NOTIF,
			wantIsNotif: true,
		},
		{
			name:        "BUF_MORE flag",
			flags:       uring.IORING_CQE_F_BUF_MORE,
			wantBufMore: true,
		},
		{
			name:             "SOCK_NONEMPTY flag",
			flags:            uring.IORING_CQE_F_SOCK_NONEMPTY,
			wantSockNonEmpty: true,
		},
		{
			name:             "all flags combined",
			flags:            uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER | uring.IORING_CQE_F_NOTIF | uring.IORING_CQE_F_BUF_MORE | uring.IORING_CQE_F_SOCK_NONEMPTY,
			wantHasMore:      true,
			wantHasBuffer:    true,
			wantIsNotif:      true,
			wantBufMore:      true,
			wantSockNonEmpty: true,
		},
		{
			name:          "buffer with ID in upper bits",
			flags:         uring.IORING_CQE_F_BUFFER | (42 << uring.IORING_CQE_BUFFER_SHIFT),
			wantHasBuffer: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cqe := uring.CQEView{Flags: tc.flags}

			if got := cqe.HasMore(); got != tc.wantHasMore {
				t.Errorf("HasMore: got %v, want %v", got, tc.wantHasMore)
			}
			if got := cqe.HasBuffer(); got != tc.wantHasBuffer {
				t.Errorf("HasBuffer: got %v, want %v", got, tc.wantHasBuffer)
			}
			if got := cqe.IsNotification(); got != tc.wantIsNotif {
				t.Errorf("IsNotification: got %v, want %v", got, tc.wantIsNotif)
			}
			if got := cqe.HasBufferMore(); got != tc.wantBufMore {
				t.Errorf("HasBufferMore: got %v, want %v", got, tc.wantBufMore)
			}
			if got := cqe.SocketNonEmpty(); got != tc.wantSockNonEmpty {
				t.Errorf("SocketNonEmpty: got %v, want %v", got, tc.wantSockNonEmpty)
			}
		})
	}
}

// TestBufIDExtraction verifies BufID extraction from CQE flags.
func TestBufIDExtraction(t *testing.T) {
	testCases := []struct {
		flags     uint32
		wantBufID uint16
	}{
		{flags: 0, wantBufID: 0},
		{flags: 1 << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 1},
		{flags: 42 << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 42},
		{flags: 255 << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 255},
		{flags: 1000 << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 1000},
		{flags: 0xFFFF << uring.IORING_CQE_BUFFER_SHIFT, wantBufID: 0xFFFF},
		// With lower flag bits set
		{
			flags:     (100 << uring.IORING_CQE_BUFFER_SHIFT) | uring.IORING_CQE_F_BUFFER | uring.IORING_CQE_F_MORE,
			wantBufID: 100,
		},
	}

	for _, tc := range testCases {
		cqe := uring.CQEView{Flags: tc.flags}
		got := cqe.BufID()
		if got != tc.wantBufID {
			t.Errorf("BufID(flags=0x%x): got %d, want %d", tc.flags, got, tc.wantBufID)
		}
	}
}

// TestBundleCountEdgeCases tests BundleCount with various edge cases.
func TestBundleCountEdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		res        int32
		bufferSize int
		wantCount  int
	}{
		// Basic cases
		{"zero result", 0, 1024, 0},
		{"exactly one buffer", 1024, 1024, 1},
		{"one byte over", 1025, 1024, 2},
		{"exactly two buffers", 2048, 1024, 2},

		// Large values
		{"64KB in 4KB buffers", 65536, 4096, 16},
		{"1MB in 64KB buffers", 1048576, 65536, 16},
		{"odd size", 12345, 1024, 13}, // ceil(12345/1024) = 13

		// Small buffers
		{"small buffer 64B", 1000, 64, 16}, // ceil(1000/64) = 16
		{"tiny buffer 16B", 100, 16, 7},    // ceil(100/16) = 7

		// Error conditions
		{"negative result", -1, 1024, 0},
		{"large negative", -100, 1024, 0},
		{"zero buffer size", 1024, 0, 0},
		{"negative buffer size", 1024, -1, 0},

		// Boundary
		{"max int32 result", 2147483647, 1024, 2097152}, // ceil(2^31-1/1024)
		{"single byte", 1, 1024, 1},
		{"single byte single size", 1, 1, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cqe := uring.CQEView{Res: tc.res}
			got := cqe.BundleCount(tc.bufferSize)
			if got != tc.wantCount {
				t.Errorf("BundleCount(res=%d, bufSize=%d): got %d, want %d",
					tc.res, tc.bufferSize, got, tc.wantCount)
			}
		})
	}
}

// TestBundleBuffersComplex tests BundleBuffers with complex scenarios.
func TestBundleBuffersComplex(t *testing.T) {
	testCases := []struct {
		name        string
		flags       uint32
		res         int32
		bufferSize  int
		wantStartID uint16
		wantCount   int
	}{
		{
			name:        "start at 0",
			flags:       0,
			res:         4096,
			bufferSize:  1024,
			wantStartID: 0,
			wantCount:   4,
		},
		{
			name:        "start at high ID",
			flags:       1000 << uring.IORING_CQE_BUFFER_SHIFT,
			res:         8192,
			bufferSize:  1024,
			wantStartID: 1000,
			wantCount:   8,
		},
		{
			name:        "partial last buffer",
			flags:       50 << uring.IORING_CQE_BUFFER_SHIFT,
			res:         2500,
			bufferSize:  1024,
			wantStartID: 50,
			wantCount:   3, // ceil(2500/1024) = 3
		},
		{
			name:        "single buffer",
			flags:       255 << uring.IORING_CQE_BUFFER_SHIFT,
			res:         512,
			bufferSize:  1024,
			wantStartID: 255,
			wantCount:   1,
		},
		{
			name:        "error result",
			flags:       10 << uring.IORING_CQE_BUFFER_SHIFT,
			res:         -22, // EINVAL
			bufferSize:  1024,
			wantStartID: 10,
			wantCount:   0,
		},
		{"selected EOF", 7<<uring.IORING_CQE_BUFFER_SHIFT | uring.IORING_CQE_F_BUFFER, 0, 1024, 7, 1},
		{"selected error", uring.IORING_CQE_F_BUFFER, -int32(zcall.ENOTCONN), 1024, 0, 1},
		{"retained slot", uring.IORING_CQE_F_BUFFER | uring.IORING_CQE_F_BUF_MORE, 1, 1024, 0, 0},
		{"invalid size", uring.IORING_CQE_F_BUFFER, 0, 0, 0, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cqe := uring.CQEView{
				Res:   tc.res,
				Flags: tc.flags,
			}

			startID, count := cqe.BundleBuffers(tc.bufferSize)
			if startID != tc.wantStartID {
				t.Errorf("BundleBuffers startID: got %d, want %d", startID, tc.wantStartID)
			}
			if count != tc.wantCount {
				t.Errorf("BundleBuffers count: got %d, want %d", count, tc.wantCount)
			}
		})
	}
}

// =============================================================================
// Registered Buffer Tests
// =============================================================================

// TestRegisteredBufferBounds tests RegisteredBuffer boundary conditions.
func TestRegisteredBufferBounds(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	count := ring.RegisteredBufferCount()
	t.Logf("registered buffer count: %d", count)

	// Test valid indices
	for i := 0; i < count && i < 5; i++ {
		buf := ring.RegisteredBuffer(i)
		if buf == nil {
			t.Errorf("RegisteredBuffer(%d) returned nil, expected valid buffer", i)
		}
	}

	// Test invalid indices
	invalidIndices := []int{-1, -100, count, count + 1, count + 100}
	for _, idx := range invalidIndices {
		buf := ring.RegisteredBuffer(idx)
		if buf != nil {
			t.Errorf("RegisteredBuffer(%d) returned non-nil, expected nil for out of range", idx)
		}
	}
}

// TestRegisteredBufferContent tests that registered buffers can hold data.
func TestRegisteredBufferContent(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	if ring.RegisteredBufferCount() < 1 {
		t.Skip("no registered buffers available")
	}

	buf := ring.RegisteredBuffer(0)
	if buf == nil {
		t.Fatal("RegisteredBuffer(0) returned nil")
	}

	// Write pattern and verify
	pattern := []byte("test pattern 123")
	copy(buf, pattern)

	// Read back and verify
	for i, b := range pattern {
		if buf[i] != b {
			t.Errorf("buf[%d] = %d, want %d", i, buf[i], b)
		}
	}

	// Verify buffer length is reasonable
	if len(buf) < 1024 {
		t.Errorf("buffer length %d seems too small", len(buf))
	}
	t.Logf("registered buffer 0 length: %d", len(buf))
}

func TestBundleRecycleWithoutPayload(t *testing.T) {
	const entries = 2
	ring := newTestRing(t, func(o *uring.Options) { o.ReadBufferNum = entries })
	pair, err := newUnixSocketPairForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestFds(pair)
	if errno := zcall.Shutdown(uintptr(pair[1]), zcall.SHUT_WR); errno != 0 {
		t.Fatal(errno)
	}
	unconnected, errno := zcall.Socket(zcall.AF_INET, zcall.SOCK_STREAM|zcall.SOCK_CLOEXEC, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer closeTestFD(iofd.FD(unconnected))

	for _, tc := range []struct {
		name string
		fd   iofd.FD
		want int32
	}{
		{"EOF", pair[0], 0},
		{"unconnected", iofd.FD(unconnected), -int32(zcall.ENOTCONN)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := uring.ForFD(int32(tc.fd)).WithFlags(uring.IOSQE_ASYNC)
			// Receiving past the group's capacity requires republishing returned slots.
			for range entries + 1 {
				if err := ring.ReceiveBundle(ctx, &tc.fd); err != nil {
					t.Fatal(err)
				}
				cqe := waitForCQEs(t, ring, 1)[0]
				it, ok := ring.BundleIterator(cqe, cqe.BufGroup())
				if cqe.Res != tc.want || cqe.HasBufferMore() || ok != cqe.HasBuffer() {
					t.Fatalf("res=%d flags=%#x iterator=%v", cqe.Res, cqe.Flags, ok)
				}
				if !ok {
					continue
				}
				if it.Count() != 1 || it.SlotID(0) != cqe.BufID() || it.TotalBytes() != 0 || len(it.Buffer(0)) != 0 {
					t.Fatalf("id=%d count=%d bytes=%d", cqe.BufID(), it.Count(), it.TotalBytes())
				}
				if err := it.Recycle(ring); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
