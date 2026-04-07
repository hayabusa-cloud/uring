// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/uring"
)

func TestClampBudget(t *testing.T) {
	tests := []struct {
		name   string
		input  int
		expect int
	}{
		{"below_min", 1 * uring.MiB, 16 * uring.MiB},
		{"at_min", 16 * uring.MiB, 16 * uring.MiB},
		{"normal", 256 * uring.MiB, 256 * uring.MiB},
		{"at_max", 128 * uring.GiB, 128 * uring.GiB},
		{"above_max", 256 * uring.GiB, 128 * uring.GiB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uring.OptionsForBudget(tt.input)
			want := uring.OptionsForBudget(tt.expect)
			if got.Entries != want.Entries {
				t.Fatalf("Entries mismatch for input %d: got %d, want %d", tt.input, got.Entries, want.Entries)
			}
			if got.LockedBufferMem != want.LockedBufferMem {
				t.Fatalf("LockedBufferMem mismatch for input %d: got %d, want %d", tt.input, got.LockedBufferMem, want.LockedBufferMem)
			}
			if got.MultiSizeBuffer != want.MultiSizeBuffer {
				t.Fatalf("MultiSizeBuffer mismatch for input %d: got %d, want %d", tt.input, got.MultiSizeBuffer, want.MultiSizeBuffer)
			}
		})
	}
}

func TestEntriesForBudget(t *testing.T) {
	tests := []struct {
		name    string
		budget  int
		entries int
	}{
		// Ring entries are cheap, so we're generous
		{"16MiB", 16 * uring.MiB, 512},    // EntriesSmall
		{"31MiB", 31 * uring.MiB, 512},    // EntriesSmall
		{"32MiB", 32 * uring.MiB, 2048},   // EntriesMedium
		{"128MiB", 128 * uring.MiB, 2048}, // EntriesMedium
		{"255MiB", 255 * uring.MiB, 2048}, // EntriesMedium
		{"256MiB", 256 * uring.MiB, 8192}, // EntriesLarge
		{"999MiB", 999 * uring.MiB, 8192}, // EntriesLarge
		{"1GiB", 1 * uring.GiB, 32768},    // EntriesHuge
		{"4GiB", 4 * uring.GiB, 32768},    // EntriesHuge
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := uring.OptionsForBudget(tt.budget)
			if opts.Entries != tt.entries {
				t.Fatalf("got Entries=%d, want %d", opts.Entries, tt.entries)
			}
		})
	}
}

func TestLockedMemForBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget int
		minMem int
		maxMem int
	}{
		// 25% ratio, rounded to power of 2
		{"16MiB_uses_min", 16 * uring.MiB, 8 * uring.MiB, 8 * uring.MiB},
		{"64MiB", 64 * uring.MiB, 16 * uring.MiB, 16 * uring.MiB},
		{"256MiB", 256 * uring.MiB, 64 * uring.MiB, 64 * uring.MiB},
		{"1GiB", 1 * uring.GiB, 256 * uring.MiB, 256 * uring.MiB},
		{"4GiB", 4 * uring.GiB, 1 * uring.GiB, 1 * uring.GiB},
		{"16GiB", 16 * uring.GiB, 4 * uring.GiB, 4 * uring.GiB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := uring.OptionsForBudget(tt.budget)
			if opts.LockedBufferMem < tt.minMem || opts.LockedBufferMem > tt.maxMem {
				t.Fatalf("got LockedBufferMem=%d, want between %d and %d",
					opts.LockedBufferMem, tt.minMem, tt.maxMem)
			}
			// Verify it's a power of 2
			if opts.LockedBufferMem&(opts.LockedBufferMem-1) != 0 {
				t.Fatalf("LockedBufferMem=%d is not a power of 2", opts.LockedBufferMem)
			}
		})
	}
}

func TestMultiSizeBufferScale(t *testing.T) {
	tests := []struct {
		name     string
		budget   int
		minScale int
		maxScale int
	}{
		{"16MiB", 16 * uring.MiB, 1, 1},
		{"64MiB", 64 * uring.MiB, 1, 2},
		{"256MiB", 256 * uring.MiB, 1, 4},
		{"1GiB", 1 * uring.GiB, 1, 8},
		{"4GiB", 4 * uring.GiB, 1, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := uring.OptionsForBudget(tt.budget)
			if opts.MultiSizeBuffer < tt.minScale || opts.MultiSizeBuffer > tt.maxScale {
				t.Fatalf("got MultiSizeBuffer=%d, want between %d and %d",
					opts.MultiSizeBuffer, tt.minScale, tt.maxScale)
			}
			// Verify it's a power of 2
			if opts.MultiSizeBuffer&(opts.MultiSizeBuffer-1) != 0 {
				t.Fatalf("MultiSizeBuffer=%d is not a power of 2", opts.MultiSizeBuffer)
			}
		})
	}
}

func TestBufferConfigForBudget(t *testing.T) {
	tests := []struct {
		name     string
		budget   int
		minScale int
		maxScale int
	}{
		{"16MiB", 16 * uring.MiB, 1, 1},
		{"64MiB", 64 * uring.MiB, 1, 2},
		{"256MiB", 256 * uring.MiB, 1, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, scale := uring.BufferConfigForBudget(tt.budget)
			if scale < tt.minScale || scale > tt.maxScale {
				t.Fatalf("got scale=%d, want between %d and %d",
					scale, tt.minScale, tt.maxScale)
			}
			// Verify config has some tiers enabled
			if cfg.PicoNum == 0 {
				t.Fatal("PicoNum should not be zero")
			}
		})
	}
}

func TestBufferConfigTierProgression(t *testing.T) {
	// Verify that larger budgets enable more tiers
	budgets := []int{
		16 * uring.MiB,
		64 * uring.MiB,
		256 * uring.MiB,
		512 * uring.MiB,
		1 * uring.GiB,
	}

	var prevTiers int
	for _, budget := range budgets {
		cfg, _ := uring.BufferConfigForBudget(budget)
		tiers := countEnabledTiers(cfg)
		if tiers < prevTiers {
			t.Fatalf("budget %d has %d tiers, less than previous %d",
				budget, tiers, prevTiers)
		}
		prevTiers = tiers
	}
}

func countEnabledTiers(cfg uring.BufferGroupsConfig) int {
	count := 0
	if cfg.PicoNum > 0 {
		count++
	}
	if cfg.NanoNum > 0 {
		count++
	}
	if cfg.MicroNum > 0 {
		count++
	}
	if cfg.SmallNum > 0 {
		count++
	}
	if cfg.MediumNum > 0 {
		count++
	}
	if cfg.BigNum > 0 {
		count++
	}
	if cfg.LargeNum > 0 {
		count++
	}
	if cfg.GreatNum > 0 {
		count++
	}
	if cfg.HugeNum > 0 {
		count++
	}
	if cfg.VastNum > 0 {
		count++
	}
	if cfg.GiantNum > 0 {
		count++
	}
	if cfg.TitanNum > 0 {
		count++
	}
	return count
}

func TestOptionsForBudget_Consistency(t *testing.T) {
	// Verify that the same budget always produces the same options
	budget := 256 * uring.MiB
	opts1 := uring.OptionsForBudget(budget)
	opts2 := uring.OptionsForBudget(budget)

	if opts1.Entries != opts2.Entries {
		t.Fatalf("Entries mismatch: %d vs %d", opts1.Entries, opts2.Entries)
	}
	if opts1.LockedBufferMem != opts2.LockedBufferMem {
		t.Fatalf("LockedBufferMem mismatch: %d vs %d", opts1.LockedBufferMem, opts2.LockedBufferMem)
	}
	if opts1.MultiSizeBuffer != opts2.MultiSizeBuffer {
		t.Fatalf("MultiSizeBuffer mismatch: %d vs %d", opts1.MultiSizeBuffer, opts2.MultiSizeBuffer)
	}
}

func TestOptionsForBudget_Integration(t *testing.T) {
	// Test that we can create a Uring with budget-based options
	// Use a small budget to avoid memory issues
	opts := uring.OptionsForBudget(16 * uring.MiB)

	// Create the ring without starting (no buffer allocation)
	ring, err := uring.New(func(o *uring.Options) {
		*o = opts
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	// Ring cleanup handled by finalizer - no explicit Close needed for this test

	// Verify the ring was created with correct entries
	if ring.SQAvailable() != opts.Entries {
		t.Fatalf("SQAvailable()=%d, want %d", ring.SQAvailable(), opts.Entries)
	}
}

func TestSizeConstants(t *testing.T) {
	// Verify the size constants are correct
	if uring.KiB != 1024 {
		t.Fatalf("KiB=%d, want 1024", uring.KiB)
	}
	if uring.MiB != 1024*1024 {
		t.Fatalf("MiB=%d, want %d", uring.MiB, 1024*1024)
	}
	if uring.GiB != 1024*1024*1024 {
		t.Fatalf("GiB=%d, want %d", uring.GiB, 1024*1024*1024)
	}
}

func TestMachineMemoryConstants(t *testing.T) {
	// Verify machine memory constants are correct multiples
	tests := []struct {
		name   string
		got    int
		wantGB int
	}{
		{"512MB", uring.MachineMemory512MB, 0}, // special case
		{"1GB", uring.MachineMemory1GB, 1},
		{"2GB", uring.MachineMemory2GB, 2},
		{"4GB", uring.MachineMemory4GB, 4},
		{"8GB", uring.MachineMemory8GB, 8},
		{"16GB", uring.MachineMemory16GB, 16},
		{"32GB", uring.MachineMemory32GB, 32},
		{"64GB", uring.MachineMemory64GB, 64},
		{"96GB", uring.MachineMemory96GB, 96},
		{"128GB", uring.MachineMemory128GB, 128},
	}

	// Check 512MB special case
	if uring.MachineMemory512MB != 512*uring.MiB {
		t.Fatalf("MachineMemory512MB=%d, want %d", uring.MachineMemory512MB, 512*uring.MiB)
	}

	// Check all GB constants
	for _, tt := range tests {
		if tt.wantGB == 0 {
			continue // skip 512MB
		}
		want := tt.wantGB * uring.GiB
		if tt.got != want {
			t.Fatalf("MachineMemory%s=%d, want %d", tt.name, tt.got, want)
		}
	}
}

func TestOptionsForSystem(t *testing.T) {
	tests := []struct {
		name         string
		systemMemory int
		minEntries   int
		maxEntries   int
	}{
		// 512MB * 25% = 128 MiB budget → EntriesMedium (2048)
		{"512MB", uring.MachineMemory512MB, 2048, 2048},
		// 1GB * 25% = 256 MiB budget → EntriesLarge (8192)
		{"1GB", uring.MachineMemory1GB, 8192, 8192},
		// 2GB * 25% = 512 MiB budget → EntriesLarge (8192)
		{"2GB", uring.MachineMemory2GB, 8192, 8192},
		// 4GB * 25% = 1 GiB budget → EntriesHuge (32768)
		{"4GB", uring.MachineMemory4GB, 32768, 32768},
		// 8GB * 25% = 2 GiB budget → EntriesHuge (32768)
		{"8GB", uring.MachineMemory8GB, 32768, 32768},
		// 16GB * 25% = 4 GiB budget → EntriesHuge (32768)
		{"16GB", uring.MachineMemory16GB, 32768, 32768},
		// 32GB * 25% = 8 GiB budget → EntriesHuge (32768)
		{"32GB", uring.MachineMemory32GB, 32768, 32768},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := uring.OptionsForSystem(tt.systemMemory)
			if opts.Entries < tt.minEntries || opts.Entries > tt.maxEntries {
				t.Fatalf("got Entries=%d, want between %d and %d",
					opts.Entries, tt.minEntries, tt.maxEntries)
			}
			// Verify other options are reasonable
			if opts.LockedBufferMem == 0 {
				t.Fatal("LockedBufferMem should not be zero")
			}
			if opts.MultiSizeBuffer == 0 {
				t.Fatal("MultiSizeBuffer should not be zero")
			}
		})
	}
}

func TestOptionsForSystem_SmallMachine(t *testing.T) {
	// Test with a very small machine (e.g., 768 MiB - common CI runner)
	opts := uring.OptionsForSystem(768 * uring.MiB)

	// 768 MiB * 25% = 192 MiB budget
	// Should give us generous settings even on small machines
	if opts.Entries == 0 {
		t.Fatal("Entries should not be zero")
	}
	if opts.LockedBufferMem < 8*uring.MiB {
		t.Fatalf("LockedBufferMem=%d, should be at least 8 MiB", opts.LockedBufferMem)
	}
}

func TestOptionsForSystem_Equivalence(t *testing.T) {
	// OptionsForSystem(X) should equal OptionsForBudget(X * 25 / 100)
	systemMem := 2 * uring.GiB
	expectedBudget := systemMem * 25 / 100

	optsSystem := uring.OptionsForSystem(systemMem)
	optsBudget := uring.OptionsForBudget(expectedBudget)

	if optsSystem.Entries != optsBudget.Entries {
		t.Fatalf("Entries mismatch: system=%d, budget=%d",
			optsSystem.Entries, optsBudget.Entries)
	}
	if optsSystem.LockedBufferMem != optsBudget.LockedBufferMem {
		t.Fatalf("LockedBufferMem mismatch: system=%d, budget=%d",
			optsSystem.LockedBufferMem, optsBudget.LockedBufferMem)
	}
	if optsSystem.MultiSizeBuffer != optsBudget.MultiSizeBuffer {
		t.Fatalf("MultiSizeBuffer mismatch: system=%d, budget=%d",
			optsSystem.MultiSizeBuffer, optsBudget.MultiSizeBuffer)
	}
}
