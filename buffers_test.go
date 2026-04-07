// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"code.hybscloud.com/spin"
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

type mockPollFd struct {
	fd int
}

func (m *mockPollFd) Fd() int { return m.fd }

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
