// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/spin"
	"code.hybscloud.com/zcall"
)

func skipIfLowMemory(t *testing.T, needed int) {
	t.Helper()
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.Atoi(fields[1])
				if err != nil {
					return
				}
				if kb*1024 < needed {
					t.Skipf("skipping: need %d MiB, available %d MiB",
						needed>>20, (kb*1024)>>20)
				}
			}
			return
		}
	}
}

func waitForProvideCQEs(t *testing.T, ur *ioUring, want int) {
	t.Helper()

	if err := ur.enter(); err != nil {
		t.Fatalf("enter: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	backoff := iox.Backoff{}
	got := 0
	for got < want {
		if time.Now().After(deadline) {
			t.Fatalf("completion timeout after %d/%d CQEs", got, want)
		}
		if err := ur.poll(want - got); err != nil {
			t.Fatalf("poll: %v", err)
		}
		cqe, err := ur.wait()
		if errors.Is(err, iox.ErrWouldBlock) {
			backoff.Wait()
			continue
		}
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if cqe.res < 0 {
			t.Fatalf("completion %d: %v", got, errFromErrno(uintptr(-cqe.res)))
		}
		got++
	}
}

func TestBufferSizeConstants(t *testing.T) {
	// Verify buffer sizes follow power-of-4 progression (12 tiers)
	sizes := []struct {
		name     string
		size     int
		expected int
	}{
		{"Pico", BufferSizePico, 32},
		{"Nano", BufferSizeNano, 128},
		{"Micro", BufferSizeMicro, 512},
		{"Small", BufferSizeSmall, 2048},
		{"Medium", BufferSizeMedium, 8192},
		{"Big", BufferSizeBig, 32768},
		{"Large", BufferSizeLarge, 131072},
		{"Great", BufferSizeGreat, 524288},
		{"Huge", BufferSizeHuge, 2097152},
		{"Vast", BufferSizeVast, 8388608},
		{"Giant", BufferSizeGiant, 33554432},
		{"Titan", BufferSizeTitan, 134217728},
	}

	for _, tt := range sizes {
		t.Run(tt.name, func(t *testing.T) {
			if tt.size != tt.expected {
				t.Errorf("BufferSize%s = %d, want %d", tt.name, tt.size, tt.expected)
			}
		})
	}

	// Verify power-of-4 progression
	for i := 1; i < len(sizes); i++ {
		ratio := sizes[i].size / sizes[i-1].size
		if ratio != 4 {
			t.Errorf("ratio %s/%s = %d, want 4", sizes[i].name, sizes[i-1].name, ratio)
		}
	}
}

func TestRegisterBufferPool(t *testing.T) {
	t.Run("create pool", func(t *testing.T) {
		pool := NewRegisterBufferPool(16)
		if pool == nil {
			t.Fatal("pool is nil")
		}
		if len(pool.items) != 16 {
			t.Errorf("pool capacity = %d, want 16", len(pool.items))
		}
	})

	t.Run("fill pool", func(t *testing.T) {
		pool := NewRegisterBufferPool(8)
		counter := 0
		pool.Fill(func() RegisterBuffer {
			var buf RegisterBuffer
			buf[0] = byte(counter)
			counter++
			return buf
		})

		if counter != 8 {
			t.Errorf("factory called %d times, want 8", counter)
		}

		// Verify each buffer was filled
		for i := 0; i < 8; i++ {
			if pool.items[i][0] != byte(i) {
				t.Errorf("pool.items[%d][0] = %d, want %d", i, pool.items[i][0], i)
			}
		}
	})
}

func TestAlignedMemBlock(t *testing.T) {
	block := AlignedMemBlock()
	if block == nil {
		t.Fatal("block is nil")
	}
	if len(block) == 0 {
		t.Error("block is empty")
	}
}

func TestNewUnixSocketPair(t *testing.T) {
	fds, err := newUnixSocketPair()
	if err != nil {
		t.Fatalf("newUnixSocketPair: %v", err)
	}

	if fds[0] < 0 {
		t.Errorf("fds[0] = %d, want >= 0", fds[0])
	}
	if fds[1] < 0 {
		t.Errorf("fds[1] = %d, want >= 0", fds[1])
	}
	if fds[0] == fds[1] {
		t.Error("fds should be different")
	}

	// Clean up
	closefd(int(fds[0]))
	closefd(int(fds[1]))
}

func TestYield(t *testing.T) {
	// Just verify it doesn't panic
	for i := 0; i < 10; i++ {
		spin.Yield()
	}
}

func TestBufferGroupIndexConstants(t *testing.T) {
	// Verify buffer group index constants (12 tiers)
	if bufferGroupIndexPico != 0 {
		t.Errorf("bufferGroupIndexPico = %d, want 0", bufferGroupIndexPico)
	}
	if bufferGroupIndexNano != 1 {
		t.Errorf("bufferGroupIndexNano = %d, want 1", bufferGroupIndexNano)
	}
	if bufferGroupIndexMicro != 2 {
		t.Errorf("bufferGroupIndexMicro = %d, want 2", bufferGroupIndexMicro)
	}
	if bufferGroupIndexSmall != 3 {
		t.Errorf("bufferGroupIndexSmall = %d, want 3", bufferGroupIndexSmall)
	}
	if bufferGroupIndexMedium != 4 {
		t.Errorf("bufferGroupIndexMedium = %d, want 4", bufferGroupIndexMedium)
	}
	if bufferGroupIndexBig != 5 {
		t.Errorf("bufferGroupIndexBig = %d, want 5", bufferGroupIndexBig)
	}
	if bufferGroupIndexLarge != 6 {
		t.Errorf("bufferGroupIndexLarge = %d, want 6", bufferGroupIndexLarge)
	}
	if bufferGroupIndexGreat != 7 {
		t.Errorf("bufferGroupIndexGreat = %d, want 7", bufferGroupIndexGreat)
	}
	if bufferGroupIndexHuge != 8 {
		t.Errorf("bufferGroupIndexHuge = %d, want 8", bufferGroupIndexHuge)
	}
	if bufferGroupIndexVast != 9 {
		t.Errorf("bufferGroupIndexVast = %d, want 9", bufferGroupIndexVast)
	}
	if bufferGroupIndexGiant != 10 {
		t.Errorf("bufferGroupIndexGiant = %d, want 10", bufferGroupIndexGiant)
	}
	if bufferGroupIndexTitan != 11 {
		t.Errorf("bufferGroupIndexTitan = %d, want 11", bufferGroupIndexTitan)
	}
	if bufferGroupIndexEnd != 12 {
		t.Errorf("bufferGroupIndexEnd = %d, want 12", bufferGroupIndexEnd)
	}
}

func TestNewUringProvideBuffers(t *testing.T) {
	t.Run("valid params", func(t *testing.T) {
		pb := newUringProvideBuffers(4096, 16)
		if pb == nil {
			t.Fatal("provide buffers is nil")
		}
		if pb.size != 4096 {
			t.Errorf("size = %d, want 4096", pb.size)
		}
		if pb.n != 16 {
			t.Errorf("n = %d, want 16", pb.n)
		}
		if pb.mem == nil {
			t.Error("mem is nil")
		}
	})

	t.Run("rounds up to power of 2", func(t *testing.T) {
		pb := newUringProvideBuffers(1000, 10)
		// 1000 rounds up to 1024
		if pb.size != 1024 {
			t.Errorf("size = %d, want 1024", pb.size)
		}
		// 10 rounds up to 16
		if pb.n != 16 {
			t.Errorf("n = %d, want 16", pb.n)
		}
	})

	t.Run("panic on invalid size", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for size 0")
			}
		}()
		newUringProvideBuffers(0, 16)
	})

	t.Run("panic on invalid n", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for n 0")
			}
		}()
		newUringProvideBuffers(4096, 0)
	})
}

func TestUringProvideBuffers_SetGIDOffset(t *testing.T) {
	pb := newUringProvideBuffers(4096, 16)
	pb.setGIDOffset(100)
	if pb.gidOffset != 100 {
		t.Errorf("gidOffset = %d, want 100", pb.gidOffset)
	}
}

func TestUringProvideBuffersAccessors(t *testing.T) {
	pb := newUringProvideBuffers(3, 1<<16)
	pb.setGIDOffset(41)

	if pb.size != 4 {
		t.Fatalf("size = %d, want 4", pb.size)
	}
	if pb.gn != 2 {
		t.Fatalf("gn = %d, want 2", pb.gn)
	}
	if pb.n != 1<<15 {
		t.Fatalf("n = %d, want %d", pb.n, 1<<15)
	}
	if pb.gMask != 1 {
		t.Fatalf("gMask = %d, want 1", pb.gMask)
	}

	group0 := pb.buf(bufferGroupIndex(pb.gidOffset), 5)
	group1 := pb.buf(bufferGroupIndex(pb.gidOffset+1), 7)
	if len(group0) != pb.size {
		t.Fatalf("group0 len = %d, want %d", len(group0), pb.size)
	}
	if len(group1) != pb.size {
		t.Fatalf("group1 len = %d, want %d", len(group1), pb.size)
	}

	wantPtr0 := unsafe.Pointer(unsafe.Add(pb.ptr, pb.size*5))
	wantPtr1 := unsafe.Pointer(unsafe.Add(pb.ptr, pb.size*(pb.n+7)))
	gotPtr0 := unsafe.Pointer(unsafe.SliceData(group0))
	gotPtr1 := unsafe.Pointer(unsafe.SliceData(group1))
	if gotPtr0 != wantPtr0 {
		t.Fatalf("group0 ptr = %p, want %p", gotPtr0, wantPtr0)
	}
	if gotPtr1 != wantPtr1 {
		t.Fatalf("group1 ptr = %p, want %p", gotPtr1, wantPtr1)
	}

	group0[0] = 0x5a
	group1[0] = 0x6b
	group1[1] = 0x7c

	if pb.mem[pb.size*5] != 0x5a {
		t.Fatalf("backing mem[0] = %x, want 0x5a", pb.mem[pb.size*5])
	}

	fd := &mockPollFd{fd: 3}
	data := pb.data(fd, 7, 2)
	if len(data) != 2 {
		t.Fatalf("data len = %d, want 2", len(data))
	}
	if unsafe.Pointer(unsafe.SliceData(data)) != wantPtr1 {
		t.Fatalf("data ptr = %p, want %p", unsafe.Pointer(unsafe.SliceData(data)), wantPtr1)
	}
	if data[0] != 0x6b || data[1] != 0x7c {
		t.Fatalf("data = %#v, want [%#x %#x]", data, byte(0x6b), byte(0x7c))
	}
}

func TestUringProvideBuffersRegister(t *testing.T) {
	ur := newTestIoUring(t)
	pb := newUringProvideBuffers(64, 4)
	pb.setGIDOffset(9)

	if err := pb.register(ur, 0); err != nil {
		t.Fatalf("register: %v", err)
	}
	waitForProvideCQEs(t, ur, pb.gn)
	runtime.KeepAlive(pb)
}

func TestUringProvideBuffersProvide(t *testing.T) {
	ur := newTestIoUring(t)
	pb := newUringProvideBuffers(64, 4)
	data := make([]byte, 48)

	if err := pb.provide(ur, 0, 23, data, 7); err != nil {
		t.Fatalf("provide: %v", err)
	}
	waitForProvideCQEs(t, ur, 1)
	runtime.KeepAlive(data)
}

func TestNewUringBufferGroups(t *testing.T) {
	t.Run("valid scale", func(t *testing.T) {
		bg := newUringBufferGroups(1)
		if bg == nil {
			t.Fatal("buffer groups is nil")
		}
		if bg.scale != 1 {
			t.Errorf("scale = %d, want 1", bg.scale)
		}
	})

	t.Run("rounds up scale", func(t *testing.T) {
		bg := newUringBufferGroups(3)
		// 3 rounds up to 4
		if bg.scale != 4 {
			t.Errorf("scale = %d, want 4", bg.scale)
		}
	})

	t.Run("panic on invalid scale", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for scale 0")
			}
		}()
		newUringBufferGroups(0)
	})
}

func TestUringBufferGroups_BufGroupBySize(t *testing.T) {
	// Test with default config (first 8 tiers enabled: Pico through Great)
	t.Run("default config", func(t *testing.T) {
		bg := newUringBufferGroups(1)
		bg.setGIDOffset(0)
		fd := &mockPollFd{fd: 0}

		// Test first 8 tier boundaries (default config) with new sizes
		tests := []struct {
			size     int
			expected uint16
		}{
			{1, bufferGroupIndexPico},
			{32, bufferGroupIndexPico},
			{33, bufferGroupIndexNano},
			{128, bufferGroupIndexNano},
			{129, bufferGroupIndexMicro},
			{512, bufferGroupIndexMicro},
			{513, bufferGroupIndexSmall},
			{2048, bufferGroupIndexSmall},
			{2049, bufferGroupIndexMedium},
			{8192, bufferGroupIndexMedium},
			{8193, bufferGroupIndexBig},
			{32768, bufferGroupIndexBig},
			{32769, bufferGroupIndexLarge},
			{131072, bufferGroupIndexLarge},
			// Sizes above Large fall back to max enabled tier (Large) since Great is disabled
			{131073, bufferGroupIndexLarge},
			{524288, bufferGroupIndexLarge},
			{524289, bufferGroupIndexLarge},
			{2097152, bufferGroupIndexLarge},
			{134217728, bufferGroupIndexLarge},
		}

		for _, tt := range tests {
			got := bg.bufGroupBySize(fd, tt.size)
			if got != tt.expected {
				t.Errorf("bufGroupBySize(fd, %d) = %d, want %d", tt.size, got, tt.expected)
			}
		}
	})

	// Test with full config (all 12 tiers enabled)
	t.Run("full config", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping full config test in short mode (requires ~4 GiB)")
		}
		skipIfLowMemory(t, 6<<30)
		bg := newUringBufferGroupsWithConfig(1, FullBufferGroupsConfig())
		bg.setGIDOffset(0)
		fd := &mockPollFd{fd: 0}

		// Test all 12 tier boundaries with new sizes
		tests := []struct {
			size     int
			expected uint16
		}{
			{1, bufferGroupIndexPico},
			{32, bufferGroupIndexPico},
			{33, bufferGroupIndexNano},
			{128, bufferGroupIndexNano},
			{129, bufferGroupIndexMicro},
			{512, bufferGroupIndexMicro},
			{513, bufferGroupIndexSmall},
			{2048, bufferGroupIndexSmall},
			{2049, bufferGroupIndexMedium},
			{8192, bufferGroupIndexMedium},
			{8193, bufferGroupIndexBig},
			{32768, bufferGroupIndexBig},
			{32769, bufferGroupIndexLarge},
			{131072, bufferGroupIndexLarge},
			{131073, bufferGroupIndexGreat},
			{524288, bufferGroupIndexGreat},
			{524289, bufferGroupIndexHuge},
			{2097152, bufferGroupIndexHuge},
			{2097153, bufferGroupIndexVast},
			{8388608, bufferGroupIndexVast},
			{8388609, bufferGroupIndexGiant},
			{33554432, bufferGroupIndexGiant},
			{33554433, bufferGroupIndexTitan},
			{134217728, bufferGroupIndexTitan},
		}

		for _, tt := range tests {
			got := bg.bufGroupBySize(fd, tt.size)
			if got != tt.expected {
				t.Errorf("bufGroupBySize(fd, %d) = %d, want %d", tt.size, got, tt.expected)
			}
		}
	})
}

// Helper to close file descriptor
func closefd(fd int) {
	// Intentionally ignoring error for cleanup
	_ = fd
}

func TestMinimalBufferGroupsConfig(t *testing.T) {
	cfg := MinimalBufferGroupsConfig()

	// First 5 tiers should be enabled
	if cfg.PicoNum != DefaultBufferNumPico {
		t.Errorf("PicoNum = %d, want %d", cfg.PicoNum, DefaultBufferNumPico)
	}
	if cfg.NanoNum != DefaultBufferNumNano {
		t.Errorf("NanoNum = %d, want %d", cfg.NanoNum, DefaultBufferNumNano)
	}
	if cfg.MicroNum != DefaultBufferNumMicro {
		t.Errorf("MicroNum = %d, want %d", cfg.MicroNum, DefaultBufferNumMicro)
	}
	if cfg.SmallNum != DefaultBufferNumSmall {
		t.Errorf("SmallNum = %d, want %d", cfg.SmallNum, DefaultBufferNumSmall)
	}
	if cfg.MediumNum != DefaultBufferNumMedium {
		t.Errorf("MediumNum = %d, want %d", cfg.MediumNum, DefaultBufferNumMedium)
	}

	// Remaining tiers should be disabled
	if cfg.BigNum != 0 {
		t.Errorf("BigNum = %d, want 0", cfg.BigNum)
	}
	if cfg.LargeNum != 0 {
		t.Errorf("LargeNum = %d, want 0", cfg.LargeNum)
	}
	if cfg.GreatNum != 0 {
		t.Errorf("GreatNum = %d, want 0", cfg.GreatNum)
	}
}

func TestUringBufferGroups_MaxEnabledTierIndex(t *testing.T) {
	tests := []struct {
		name     string
		cfg      BufferGroupsConfig
		expected int
		highMem  bool // requires ~4 GiB
	}{
		{"full config", FullBufferGroupsConfig(), bufferGroupIndexTitan, true},
		{"default config", DefaultBufferGroupsConfig(), bufferGroupIndexLarge, false},
		{"minimal config", MinimalBufferGroupsConfig(), bufferGroupIndexMedium, false},
		{"pico only", BufferGroupsConfig{PicoNum: 1}, bufferGroupIndexPico, false},
		{"nano only", BufferGroupsConfig{NanoNum: 1}, bufferGroupIndexNano, false},
		{"micro only", BufferGroupsConfig{MicroNum: 1}, bufferGroupIndexMicro, false},
		{"small only", BufferGroupsConfig{SmallNum: 1}, bufferGroupIndexSmall, false},
		{"medium only", BufferGroupsConfig{MediumNum: 1}, bufferGroupIndexMedium, false},
		{"big only", BufferGroupsConfig{BigNum: 1}, bufferGroupIndexBig, false},
		{"large only", BufferGroupsConfig{LargeNum: 1}, bufferGroupIndexLarge, false},
		{"great only", BufferGroupsConfig{GreatNum: 1}, bufferGroupIndexGreat, false},
		{"huge only", BufferGroupsConfig{HugeNum: 1}, bufferGroupIndexHuge, false},
		{"vast only", BufferGroupsConfig{VastNum: 1}, bufferGroupIndexVast, false},
		{"giant only", BufferGroupsConfig{GiantNum: 1}, bufferGroupIndexGiant, false},
		{"titan only", BufferGroupsConfig{TitanNum: 1}, bufferGroupIndexTitan, false},
		{"empty config", BufferGroupsConfig{}, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.highMem && testing.Short() {
				t.Skip("skipping high memory test in short mode")
			}
			if tt.highMem {
				skipIfLowMemory(t, 6<<30)
			}
			bg := newUringBufferGroupsWithConfig(1, tt.cfg)
			got := bg.maxEnabledTierIndex()
			if got != tt.expected {
				t.Errorf("maxEnabledTierIndex() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestUringBufferGroups_BufGroupByDataNil(t *testing.T) {
	bg := newUringBufferGroups(1)
	bg.setGIDOffset(0)
	fd := &mockPollFd{fd: 0}

	// nil data should use default bufGroup
	gid := bg.bufGroupByData(fd, nil)
	expectedGid := bg.bufGroup(fd)
	if gid != expectedGid {
		t.Errorf("bufGroupByData(nil) = %d, want %d", gid, expectedGid)
	}
}

func TestUringBufferGroups_BufGroupByDataNonNil(t *testing.T) {
	bg := newUringBufferGroups(1)
	bg.setGIDOffset(0)
	fd := &mockPollFd{fd: 0}

	data := make([]byte, 1024)
	gid := bg.bufGroupByData(fd, data)
	expectedGid := bg.bufGroupBySize(fd, len(data))
	if gid != expectedGid {
		t.Errorf("bufGroupByData(1024) = %d, want %d", gid, expectedGid)
	}
}

func TestUringBufferGroups_BufGroup(t *testing.T) {
	bg := newUringBufferGroups(4)
	bg.setGIDOffset(100)

	tests := []struct {
		fd       int
		expected uint16
	}{
		{0, 100 + bufferGroupIndexEnd*0 + bufferGroupIndexMedium},
		{1, 100 + bufferGroupIndexEnd*1 + bufferGroupIndexMedium},
		{2, 100 + bufferGroupIndexEnd*2 + bufferGroupIndexMedium},
		{3, 100 + bufferGroupIndexEnd*3 + bufferGroupIndexMedium},
		{4, 100 + bufferGroupIndexEnd*0 + bufferGroupIndexMedium}, // wraps
	}

	for _, tt := range tests {
		fd := &mockPollFd{fd: tt.fd}
		got := bg.bufGroup(fd)
		if got != tt.expected {
			t.Errorf("bufGroup(fd=%d) = %d, want %d", tt.fd, got, tt.expected)
		}
	}
}

func TestUringBufferGroups_Buf(t *testing.T) {
	// Create buffer groups with minimal config
	cfg := BufferGroupsConfig{
		PicoNum:  16,
		NanoNum:  16,
		MicroNum: 16,
	}
	bg := newUringBufferGroupsWithConfig(1, cfg)
	bg.setGIDOffset(0)

	t.Run("pico buffer", func(t *testing.T) {
		buf := bg.buf(bufferGroupIndexPico, 0)
		if buf == nil {
			t.Fatal("buf returned nil")
		}
		if len(buf) != BufferSizePico {
			t.Errorf("buf len = %d, want %d", len(buf), BufferSizePico)
		}
	})

	t.Run("nano buffer", func(t *testing.T) {
		buf := bg.buf(bufferGroupIndexNano, 0)
		if buf == nil {
			t.Fatal("buf returned nil")
		}
		if len(buf) != BufferSizeNano {
			t.Errorf("buf len = %d, want %d", len(buf), BufferSizeNano)
		}
	})

	t.Run("micro buffer", func(t *testing.T) {
		buf := bg.buf(bufferGroupIndexMicro, 0)
		if buf == nil {
			t.Fatal("buf returned nil")
		}
		if len(buf) != BufferSizeMicro {
			t.Errorf("buf len = %d, want %d", len(buf), BufferSizeMicro)
		}
	})

	t.Run("disabled tier", func(t *testing.T) {
		buf := bg.buf(bufferGroupIndexSmall, 0)
		if buf != nil {
			t.Error("disabled tier should return nil")
		}
	})

	t.Run("unknown tier", func(t *testing.T) {
		buf := bg.buf(255, 0)
		if buf != nil {
			t.Error("unknown tier should return nil")
		}
	})
}

func TestUringBufferGroups_DataAccessors(t *testing.T) {
	cfg := BufferGroupsConfig{
		PicoNum:   16,
		NanoNum:   16,
		MicroNum:  16,
		SmallNum:  16,
		MediumNum: 16,
		BigNum:    16,
		LargeNum:  16,
		GreatNum:  16,
		HugeNum:   4,
		VastNum:   2,
		GiantNum:  1,
		TitanNum:  1,
	}
	bg := newUringBufferGroupsWithConfig(1, cfg)
	bg.setGIDOffset(0)
	fd := &mockPollFd{fd: 0}

	tests := []struct {
		name     string
		accessor func(PollFd, uint32, int) []byte
		size     int
	}{
		{"pico", bg.picoData, BufferSizePico},
		{"nano", bg.nanoData, BufferSizeNano},
		{"micro", bg.microData, BufferSizeMicro},
		{"small", bg.smallData, BufferSizeSmall},
		{"medium", bg.mediumData, BufferSizeMedium},
		{"big", bg.bigData, BufferSizeBig},
		{"large", bg.largeData, BufferSizeLarge},
		{"great", bg.greatData, BufferSizeGreat},
		{"huge", bg.hugeData, BufferSizeHuge},
		{"vast", bg.vastData, BufferSizeVast},
		{"giant", bg.giantData, BufferSizeGiant},
		{"titan", bg.titanData, BufferSizeTitan},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.accessor(fd, 0, tt.size)
			if data == nil {
				t.Fatal("data is nil")
			}
			if len(data) != tt.size {
				t.Errorf("data len = %d, want %d", len(data), tt.size)
			}
		})
	}
}

func TestUringBufferGroups_DataAccessorsDisabled(t *testing.T) {
	// Create with no tiers enabled
	cfg := BufferGroupsConfig{}
	bg := newUringBufferGroupsWithConfig(1, cfg)
	bg.setGIDOffset(0)
	fd := &mockPollFd{fd: 0}

	// All accessors should return nil when disabled
	if bg.picoData(fd, 0, 32) != nil {
		t.Error("picoData should return nil when disabled")
	}
	if bg.nanoData(fd, 0, 128) != nil {
		t.Error("nanoData should return nil when disabled")
	}
	if bg.microData(fd, 0, 512) != nil {
		t.Error("microData should return nil when disabled")
	}
	if bg.smallData(fd, 0, 2048) != nil {
		t.Error("smallData should return nil when disabled")
	}
	if bg.mediumData(fd, 0, 8192) != nil {
		t.Error("mediumData should return nil when disabled")
	}
	if bg.bigData(fd, 0, 32768) != nil {
		t.Error("bigData should return nil when disabled")
	}
	if bg.largeData(fd, 0, 131072) != nil {
		t.Error("largeData should return nil when disabled")
	}
	if bg.greatData(fd, 0, 524288) != nil {
		t.Error("greatData should return nil when disabled")
	}
	if bg.hugeData(fd, 0, 2097152) != nil {
		t.Error("hugeData should return nil when disabled")
	}
	if bg.vastData(fd, 0, 8388608) != nil {
		t.Error("vastData should return nil when disabled")
	}
	if bg.giantData(fd, 0, 33554432) != nil {
		t.Error("giantData should return nil when disabled")
	}
	if bg.titanData(fd, 0, 134217728) != nil {
		t.Error("titanData should return nil when disabled")
	}
}

func TestUringBufferGroups_Buf_AllTiers(t *testing.T) {
	// Test all 12 tiers with modulo-based tier extraction
	cfg := BufferGroupsConfig{
		PicoNum:   4,
		NanoNum:   4,
		MicroNum:  4,
		SmallNum:  4,
		MediumNum: 4,
		BigNum:    4,
		LargeNum:  4,
		GreatNum:  4,
		HugeNum:   2,
		VastNum:   1,
		GiantNum:  1,
		TitanNum:  1,
	}
	bg := newUringBufferGroupsWithConfig(1, cfg)
	bg.setGIDOffset(0)

	tests := []struct {
		tier bufferGroupIndex
		size int
	}{
		{bufferGroupIndexPico, BufferSizePico},
		{bufferGroupIndexNano, BufferSizeNano},
		{bufferGroupIndexMicro, BufferSizeMicro},
		{bufferGroupIndexSmall, BufferSizeSmall},
		{bufferGroupIndexMedium, BufferSizeMedium},
		{bufferGroupIndexBig, BufferSizeBig},
		{bufferGroupIndexLarge, BufferSizeLarge},
		{bufferGroupIndexGreat, BufferSizeGreat},
		{bufferGroupIndexHuge, BufferSizeHuge},
		{bufferGroupIndexVast, BufferSizeVast},
		{bufferGroupIndexGiant, BufferSizeGiant},
		{bufferGroupIndexTitan, BufferSizeTitan},
	}

	for _, tt := range tests {
		buf := bg.buf(tt.tier, 0)
		if buf == nil {
			t.Errorf("tier %d: buf returned nil", tt.tier)
			continue
		}
		if len(buf) != tt.size {
			t.Errorf("tier %d: buf len = %d, want %d", tt.tier, len(buf), tt.size)
		}
	}
}

func TestUringProvideBufferGroupsRegister(t *testing.T) {
	ur := newTestIoUring(t)
	cfg := BufferGroupsConfig{
		PicoNum:   2,
		NanoNum:   2,
		MicroNum:  2,
		SmallNum:  2,
		MediumNum: 2,
		BigNum:    2,
		LargeNum:  2,
		GreatNum:  2,
		HugeNum:   2,
		VastNum:   2,
	}
	bg := newUringBufferGroupsWithConfig(1, cfg)
	bg.setGIDOffset(30)

	if err := bg.register(ur, 0); err != nil {
		t.Fatalf("register: %v", err)
	}
	waitForProvideCQEs(t, ur, 10)
	runtime.KeepAlive(bg)
}

func TestUringProvideBufferGroupsProvide(t *testing.T) {
	ur := newTestIoUring(t)
	bg := newUringBufferGroupsWithConfig(2, BufferGroupsConfig{
		PicoNum:   2,
		MediumNum: 2,
	})
	bg.setGIDOffset(13)
	data := make([]byte, BufferSizePico)

	group := bg.bufGroupBySize(&mockPollFd{fd: 3}, BufferSizePico)
	if err := bg.provide(ur, 0, group, data, 5); err != nil {
		t.Fatalf("provide: %v", err)
	}
	waitForProvideCQEs(t, ur, 1)
	runtime.KeepAlive(data)
}

func TestBufferRingsBundleIteratorRecycle(t *testing.T) {
	const (
		bufSize   = 16
		ringCount = 4
		gidOffset = 11
		group     = 11
	)

	backing := make([]byte, ringCount*bufSize)
	slots := make([]ioUringBuf, ringCount)
	br := (*ioUringBufRing)(unsafe.Pointer(&slots[0]))

	rings := &uringBufferRings{
		rings:   []*ioUringBufRing{br},
		locks:   []spin.Lock{{}},
		counts:  []uintptr{0},
		masks:   []uintptr{ringCount - 1},
		sizes:   []int{bufSize},
		buffers: [][]byte{backing},
	}

	cqe := CQEView{
		Res:   2 * bufSize,
		Flags: 0 << IORING_CQE_BUFFER_SHIFT,
	}

	if _, ok := rings.bundleIterator(cqe, -1, gidOffset, group); ok {
		t.Fatal("bundleIterator accepted a negative ring index")
	}

	iter, ok := rings.bundleIterator(cqe, 0, gidOffset, group)
	if !ok {
		t.Fatal("bundleIterator returned false for a valid buffer ring")
	}
	if iter.gidOffset != gidOffset || iter.group != group {
		t.Fatalf("iterator captured (%d, %d), want (%d, %d)", iter.gidOffset, iter.group, gidOffset, group)
	}

	ur := &Uring{
		ioUring:     &ioUring{},
		bufferRings: rings,
	}
	iter.Recycle(ur)

	if rings.counts[0] != 0 {
		t.Fatalf("counts after Recycle = %d, want 0", rings.counts[0])
	}
	if br.tail != 2 {
		t.Fatalf("ring tail after Recycle = %d, want 2", br.tail)
	}

	wantAddr0 := uint64(uintptr(unsafe.Pointer(&backing[0])))
	wantAddr1 := uint64(uintptr(unsafe.Pointer(&backing[bufSize])))

	if slots[0].addr != wantAddr0 || slots[0].len != bufSize || slots[0].bid != 0 {
		t.Fatalf("slot 0 = {addr:%d len:%d bid:%d}, want {addr:%d len:%d bid:%d}", slots[0].addr, slots[0].len, slots[0].bid, wantAddr0, bufSize, 0)
	}
	if slots[1].addr != wantAddr1 || slots[1].len != bufSize || slots[1].bid != 1 {
		t.Fatalf("slot 1 = {addr:%d len:%d bid:%d}, want {addr:%d len:%d bid:%d}", slots[1].addr, slots[1].len, slots[1].bid, wantAddr1, bufSize, 1)
	}
}

func TestBufferRingsReleaseUnmapsAndResetsState(t *testing.T) {
	ptr, errno := zcall.Mmap(nil, ioUringBufSize, zcall.PROT_READ|zcall.PROT_WRITE, zcall.MAP_SHARED|zcall.MAP_ANONYMOUS, ^uintptr(0), 0)
	if errno != 0 {
		t.Fatalf("mmap: %v", errFromErrno(errno))
	}

	rings := &uringBufferRings{
		rings:    []*ioUringBufRing{(*ioUringBufRing)(ptr)},
		backings: [][]byte{nil},
		gids:     []uint16{23},
		locks:    []spin.Lock{{}},
		counts:   []uintptr{1},
		masks:    []uintptr{0},
		sizes:    []int{64},
		buffers:  [][]byte{make([]byte, 64)},
	}

	if err := rings.release(nil); err != nil {
		t.Fatalf("release: %v", err)
	}

	if rings.rings != nil || rings.backings != nil || rings.gids != nil || rings.locks != nil ||
		rings.counts != nil || rings.masks != nil || rings.sizes != nil || rings.buffers != nil {
		t.Fatalf("release did not reset state: %+v", rings)
	}
}

func TestBufferRingsRegisterBuffersTracksGroups(t *testing.T) {
	ur := newTestIoUring(t)
	rings := newUringBufferRings()
	buffers := newUringProvideBuffers(2, 1<<16)
	buffers.setGIDOffset(41)

	if err := rings.registerBuffers(ur, buffers); err != nil {
		t.Fatalf("registerBuffers: %v", err)
	}

	if len(rings.rings) != buffers.gn {
		t.Fatalf("len(rings) = %d, want %d", len(rings.rings), buffers.gn)
	}
	for i := range buffers.gn {
		if rings.rings[i] == nil {
			t.Fatalf("rings[%d] = nil", i)
		}
		if rings.gids[i] != uint16(i)+buffers.gidOffset {
			t.Fatalf("gid[%d] = %d, want %d", i, rings.gids[i], uint16(i)+buffers.gidOffset)
		}
		if rings.counts[i] != uintptr(buffers.n) {
			t.Fatalf("count[%d] = %d, want %d", i, rings.counts[i], buffers.n)
		}
		if rings.masks[i] != uintptr(buffers.n-1) {
			t.Fatalf("mask[%d] = %d, want %d", i, rings.masks[i], buffers.n-1)
		}
		if rings.sizes[i] != buffers.size {
			t.Fatalf("size[%d] = %d, want %d", i, rings.sizes[i], buffers.size)
		}

		wantBuf := buffers.mem[buffers.size*buffers.n*i : buffers.size*buffers.n*(i+1)]
		if got, want := unsafe.Pointer(unsafe.SliceData(rings.buffers[i])), unsafe.Pointer(unsafe.SliceData(wantBuf)); got != want {
			t.Fatalf("buffers[%d] ptr = %p, want %p", i, got, want)
		}
		if len(rings.backings[i]) == 0 {
			t.Fatalf("backing[%d] is empty", i)
		}
	}

	rings.advance(ur)
	for i, br := range rings.rings {
		if rings.counts[i] != 0 {
			t.Fatalf("count[%d] after advance = %d, want 0", i, rings.counts[i])
		}
		if br.tail != uint16(buffers.n) {
			t.Fatalf("tail[%d] after advance = %d, want %d", i, br.tail, buffers.n)
		}
	}

	if err := rings.release(ur); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestBufferRingsRegisterGroupRejectsNonPowerOfTwoEntries(t *testing.T) {
	ur := newTestIoUring(t)
	rings := newUringBufferRings()

	_, err := rings.registerGroup(ur, 3, 9, make([]byte, 3*64), 64)
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("registerGroup(non-power-of-two) = %v, want %v", err, ErrInvalidParam)
	}
}

func TestBufferRingsRegisterGroupsCoversEnabledTiers(t *testing.T) {
	tests := []struct {
		name string
		cfg  BufferGroupsConfig
		tier uint16
		size int
	}{
		{"pico", BufferGroupsConfig{PicoNum: 1}, bufferGroupIndexPico, BufferSizePico},
		{"nano", BufferGroupsConfig{NanoNum: 1}, bufferGroupIndexNano, BufferSizeNano},
		{"micro", BufferGroupsConfig{MicroNum: 1}, bufferGroupIndexMicro, BufferSizeMicro},
		{"small", BufferGroupsConfig{SmallNum: 1}, bufferGroupIndexSmall, BufferSizeSmall},
		{"medium", BufferGroupsConfig{MediumNum: 1}, bufferGroupIndexMedium, BufferSizeMedium},
		{"big", BufferGroupsConfig{BigNum: 1}, bufferGroupIndexBig, BufferSizeBig},
		{"large", BufferGroupsConfig{LargeNum: 1}, bufferGroupIndexLarge, BufferSizeLarge},
		{"great", BufferGroupsConfig{GreatNum: 1}, bufferGroupIndexGreat, BufferSizeGreat},
		{"huge", BufferGroupsConfig{HugeNum: 1}, bufferGroupIndexHuge, BufferSizeHuge},
		{"vast", BufferGroupsConfig{VastNum: 1}, bufferGroupIndexVast, BufferSizeVast},
		{"giant", BufferGroupsConfig{GiantNum: 1}, bufferGroupIndexGiant, BufferSizeGiant},
		{"titan", BufferGroupsConfig{TitanNum: 1}, bufferGroupIndexTitan, BufferSizeTitan},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ur := newTestIoUring(t)
			rings := newUringBufferRings()
			groups := newUringBufferGroupsWithConfig(1, tt.cfg)
			groups.setGIDOffset(13)

			if err := rings.registerGroups(ur, groups); err != nil {
				t.Fatalf("registerGroups: %v", err)
			}
			if len(rings.rings) != 1 {
				t.Fatalf("len(rings) = %d, want 1", len(rings.rings))
			}
			if rings.rings[0] == nil {
				t.Fatal("rings[0] = nil")
			}
			if rings.gids[0] != 13+tt.tier {
				t.Fatalf("gid = %d, want %d", rings.gids[0], 13+tt.tier)
			}
			if rings.sizes[0] != tt.size {
				t.Fatalf("size = %d, want %d", rings.sizes[0], tt.size)
			}
			if rings.counts[0] != 1 {
				t.Fatalf("count = %d, want 1", rings.counts[0])
			}
			if rings.masks[0] != 0 {
				t.Fatalf("mask = %d, want 0", rings.masks[0])
			}

			if err := rings.release(ur); err != nil {
				t.Fatalf("release: %v", err)
			}
		})
	}
}
