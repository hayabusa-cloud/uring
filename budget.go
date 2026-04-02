// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring

// Memory size constants for budget specification.
const (
	KiB = 1 << 10
	MiB = 1 << 20
	GiB = 1 << 30
)

// Common machine memory sizes for OptionsForSystem.
const (
	MachineMemory512MB = 512 * MiB
	MachineMemory1GB   = 1 * GiB
	MachineMemory2GB   = 2 * GiB
	MachineMemory4GB   = 4 * GiB
	MachineMemory8GB   = 8 * GiB
	MachineMemory16GB  = 16 * GiB
	MachineMemory32GB  = 32 * GiB
	MachineMemory64GB  = 64 * GiB
	MachineMemory96GB  = 96 * GiB
	MachineMemory128GB = 128 * GiB
)

// Budget bounds.
const (
	minBudget = 16 * MiB           // 16 MiB minimum
	maxBudget = MachineMemory128GB // 128 GiB maximum

	// Memory allocation ratios
	lockedMemRatio = 25      // 25% for registered buffers
	minLockedMem   = 8 * MiB // 8 MiB minimum (no maximum cap)

	// System memory to io_uring budget ratio
	systemMemoryRatio = 25 // 25% of system memory for io_uring
)

// OptionsForSystem returns Options configured for a machine with the given total memory.
//
// This is the recommended entry point for configuring io_uring based on available system resources.
// It automatically calculates a generous budget (25% of system memory) for high-performance I/O.
//
// Use the MachineMemory* constants for common configurations:
//
//	// 1GB machine (e.g., Linode Nanode, small CI runner)
//	opts := OptionsForSystem(MachineMemory1GB)
//	ring, err := New(func(o *Options) { *o = opts })
//
//	// 4GB machine (e.g., medium VM)
//	opts := OptionsForSystem(MachineMemory4GB)
//
// Or pass the actual system memory:
//
//	opts := OptionsForSystem(3840 * MiB)  // 3.75 GiB
func OptionsForSystem(systemMemory int) Options {
	budget := systemMemory * systemMemoryRatio / 100
	return OptionsForBudget(budget)
}

// OptionsForBudget returns Options configured for the given memory budget.
//
// Budget specifies the total memory in bytes to allocate for the io_uring instance.
// Supports budgets from 16 MiB to 128 GiB (values outside this range are clamped).
//
// Memory is distributed as:
//   - Ring entries: sized to match expected throughput (more budget = more entries)
//   - Registered buffers: 25% for zero-copy operations (minimum 8 MiB, no maximum cap)
//   - Buffer groups: use the remaining budget after registered buffers and ring overhead
//
// Example:
//
//	// 256 MiB budget for a medium server
//	opts := OptionsForBudget(256 * MiB)
//	ring, err := New(func(o *Options) { *o = opts })
//
//	// 64 MiB budget for memory-constrained environment
//	opts := OptionsForBudget(64 * MiB)
func OptionsForBudget(budget int) Options {
	budget = clampBudget(budget)

	entries := entriesForBudget(budget)
	lockedMem := lockedMemForBudget(budget)
	bufferGroupMem := budget - lockedMem - ringOverhead(entries)
	if bufferGroupMem < 0 {
		bufferGroupMem = 0
	}

	cfg := bufferConfigForMem(bufferGroupMem)
	scale := scaleForMem(bufferGroupMem, cfg)

	return Options{
		Entries:         entries,
		LockedBufferMem: lockedMem,
		MultiSizeBuffer: scale,
		ReadBufferSize:  bufferSizeDefault,
		ReadBufferNum:   entries * 2,
		WriteBufferSize: bufferSizeDefault,
		WriteBufferNum:  entries,
	}
}

// BufferConfigForBudget returns a BufferGroupsConfig and scale for the given memory budget.
// Use this when you want fine-grained control over Options while using
// budget-based buffer configuration.
//
// Budget handling matches OptionsForBudget:
//   - registered buffers use 25% of the budget (minimum 8 MiB)
//   - ring overhead is reserved from the same budget
//   - buffer groups use the remaining memory
//
// The returned scale should be passed to Options.MultiSizeBuffer.
//
// Example:
//
//	cfg, scale := BufferConfigForBudget(256 * MiB)
//	// cfg contains tier configuration, scale is the multiplier
func BufferConfigForBudget(budget int) (BufferGroupsConfig, int) {
	budget = clampBudget(budget)
	entries := entriesForBudget(budget)
	lockedMem := lockedMemForBudget(budget)
	bufferGroupMem := budget - lockedMem - ringOverhead(entries)
	if bufferGroupMem < 0 {
		bufferGroupMem = 0
	}
	cfg := bufferConfigForMem(bufferGroupMem)
	scale := scaleForMem(bufferGroupMem, cfg)
	return cfg, scale
}

func clampBudget(budget int) int {
	if budget < minBudget {
		return minBudget
	}
	if budget > maxBudget {
		return maxBudget
	}
	return budget
}

func entriesForBudget(budget int) int {
	// Ring entries are cheap (~96 bytes each: 64B SQE + 32B CQEs).
	// Be generous with entries to maximize concurrent operations.
	switch {
	case budget < 32*MiB:
		return EntriesSmall // 512 (~48 KB overhead)
	case budget < 256*MiB:
		return EntriesMedium // 2048 (~192 KB overhead)
	case budget < 1*GiB:
		return EntriesLarge // 8192 (~768 KB overhead)
	default:
		return EntriesHuge // 32768 (~3 MB overhead)
	}
}

func lockedMemForBudget(budget int) int {
	mem := budget * lockedMemRatio / 100
	if mem < minLockedMem {
		mem = minLockedMem
	}
	// No maximum cap - let users allocate as much as their budget allows
	return roundToPowerOf2(mem)
}

func ringOverhead(entries int) int {
	// Conservative estimate: 64 bytes/SQE + 16 bytes/CQE (2x CQEs)
	return entries * 96
}

// bufferConfigForMem returns the appropriate tier configuration for available memory.
func bufferConfigForMem(mem int) BufferGroupsConfig {
	// Memory per tier at scale=1 (cumulative):
	// Tiers 0-4 (Minimal): 1+2+4+8+16 = 31 MiB
	// Tiers 0-5: +32 = 63 MiB
	// Tiers 0-6 (Default): +64 = 127 MiB
	// Tiers 0-7: +128 = 255 MiB
	// Tiers 0-8: +256 = 511 MiB
	// Tiers 0-9: +512 = 1023 MiB
	// Tiers 0-10: +1024 = 2047 MiB
	// Tiers 0-11 (Full): +2048 = 4095 MiB

	switch {
	case mem < 32*MiB:
		// Minimal: Pico-Medium (5 tiers)
		return MinimalBufferGroupsConfig()
	case mem < 64*MiB:
		// Pico-Big (6 tiers)
		return BufferGroupsConfig{
			PicoNum:   DefaultBufferNumPico,
			NanoNum:   DefaultBufferNumNano,
			MicroNum:  DefaultBufferNumMicro,
			SmallNum:  DefaultBufferNumSmall,
			MediumNum: DefaultBufferNumMedium,
			BigNum:    DefaultBufferNumBig,
		}
	case mem < 128*MiB:
		// Default: Pico-Large (7 tiers)
		return DefaultBufferGroupsConfig()
	case mem < 256*MiB:
		// Pico-Great (8 tiers)
		cfg := DefaultBufferGroupsConfig()
		cfg.GreatNum = DefaultBufferNumGreat
		return cfg
	case mem < 512*MiB:
		// Pico-Huge (9 tiers)
		cfg := DefaultBufferGroupsConfig()
		cfg.GreatNum = DefaultBufferNumGreat
		cfg.HugeNum = DefaultBufferNumHuge
		return cfg
	case mem < 1*GiB:
		// Pico-Vast (10 tiers)
		cfg := DefaultBufferGroupsConfig()
		cfg.GreatNum = DefaultBufferNumGreat
		cfg.HugeNum = DefaultBufferNumHuge
		cfg.VastNum = DefaultBufferNumVast
		return cfg
	default:
		// Full: all 12 tiers
		return FullBufferGroupsConfig()
	}
}

// scaleForMem calculates the scale factor for the given config and memory.
func scaleForMem(mem int, cfg BufferGroupsConfig) int {
	baseMem := cfgBaseMem(cfg)
	if baseMem == 0 {
		return 1
	}
	scale := mem / baseMem
	if scale < 1 {
		scale = 1
	}
	if scale > 4096 {
		scale = 4096
	}
	return roundToPowerOf2(scale)
}

// cfgBaseMem calculates the base memory usage for a config at scale=1.
func cfgBaseMem(cfg BufferGroupsConfig) int {
	return cfg.PicoNum*BufferSizePico +
		cfg.NanoNum*BufferSizeNano +
		cfg.MicroNum*BufferSizeMicro +
		cfg.SmallNum*BufferSizeSmall +
		cfg.MediumNum*BufferSizeMedium +
		cfg.BigNum*BufferSizeBig +
		cfg.LargeNum*BufferSizeLarge +
		cfg.GreatNum*BufferSizeGreat +
		cfg.HugeNum*BufferSizeHuge +
		cfg.VastNum*BufferSizeVast +
		cfg.GiantNum*BufferSizeGiant +
		cfg.TitanNum*BufferSizeTitan
}

// roundToPowerOf2 rounds n up to the nearest power of 2.
func roundToPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}
