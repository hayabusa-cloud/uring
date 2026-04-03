// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring

// bufferGroupIndex identifies a tier in the 12-tier buffer system.
type bufferGroupIndex uint16

// Buffer group tier indices.
//
// The 12-tier buffer system uses a power-of-4 size progression:
//
//	Tier    Name    Size        Use Case
//	0       Pico    32 B        Tiny metadata, headers
//	1       Nano    128 B       Small protocol frames
//	2       Micro   512 B       Standard protocol messages
//	3       Small   2 KiB       Typical HTTP requests
//	4       Medium  8 KiB       Page-sized operations
//	5       Big     32 KiB      Large messages
//	6       Large   128 KiB     Bulk transfers
//	7       Great   512 KiB     Large file chunks
//	8       Huge    2 MiB       Very large buffers
//	9       Vast    8 MiB       Streaming media
//	10      Giant   32 MiB      Maximum standard
//	11      Titan   128 MiB     Extreme cases
const (
	bufferGroupIndexPico = iota
	bufferGroupIndexNano
	bufferGroupIndexMicro
	bufferGroupIndexSmall
	bufferGroupIndexMedium
	bufferGroupIndexBig
	bufferGroupIndexLarge
	bufferGroupIndexGreat
	bufferGroupIndexHuge
	bufferGroupIndexVast
	bufferGroupIndexGiant
	bufferGroupIndexTitan
	bufferGroupIndexEnd
)

// Default buffer counts per tier.
// Smaller buffers have more instances to handle high-frequency small I/O.
// Larger buffers have fewer instances due to memory constraints.
const (
	DefaultBufferNumPico   = 1 << 15 // 32768 × 32 B = 1 MiB
	DefaultBufferNumNano   = 1 << 14 // 16384 × 128 B = 2 MiB
	DefaultBufferNumMicro  = 1 << 13 // 8192 × 512 B = 4 MiB
	DefaultBufferNumSmall  = 1 << 12 // 4096 × 2 KiB = 8 MiB
	DefaultBufferNumMedium = 1 << 11 // 2048 × 8 KiB = 16 MiB
	DefaultBufferNumBig    = 1 << 10 // 1024 × 32 KiB = 32 MiB
	DefaultBufferNumLarge  = 1 << 9  // 512 × 128 KiB = 64 MiB
	DefaultBufferNumGreat  = 1 << 8  // 256 × 512 KiB = 128 MiB
	DefaultBufferNumHuge   = 1 << 7  // 128 × 2 MiB = 256 MiB
	DefaultBufferNumVast   = 1 << 6  // 64 × 8 MiB = 512 MiB
	DefaultBufferNumGiant  = 1 << 5  // 32 × 32 MiB = 1 GiB
	DefaultBufferNumTitan  = 1 << 4  // 16 × 128 MiB = 2 GiB
)

// BufferGroupsConfig configures buffer counts for each tier.
//
// Each field specifies the number of buffers to allocate for that tier.
// A zero count disables the tier (no memory allocated).
//
// Memory usage calculation:
//
//	Total = Sum(TierSize × TierCount × Scale)
//
// Example with the default config (Scale=1):
//
//	Pico:   32768 × 32 B   = 1 MiB
//	Nano:   16384 × 128 B  = 2 MiB
//	Micro:  8192 × 512 B   = 4 MiB
//	Small:  4096 × 2 KiB   = 8 MiB
//	Medium: 2048 × 8 KiB   = 16 MiB
//	Big:    1024 × 32 KiB  = 32 MiB
//	Large:  512 × 128 KiB  = 64 MiB
//	                Total  ≈ 127 MiB per scale
type BufferGroupsConfig struct {
	PicoNum   int // 32 B buffers
	NanoNum   int // 128 B buffers
	MicroNum  int // 512 B buffers
	SmallNum  int // 2 KiB buffers
	MediumNum int // 8 KiB buffers
	BigNum    int // 32 KiB buffers
	LargeNum  int // 128 KiB buffers
	GreatNum  int // 512 KiB buffers
	HugeNum   int // 2 MiB buffers
	VastNum   int // 8 MiB buffers
	GiantNum  int // 32 MiB buffers
	TitanNum  int // 128 MiB buffers
}

// DefaultBufferGroupsConfig returns the default configuration.
// It enables the first 7 tiers (Pico through Large), totaling ~127 MiB per scale.
func DefaultBufferGroupsConfig() BufferGroupsConfig {
	return BufferGroupsConfig{
		PicoNum:   DefaultBufferNumPico,
		NanoNum:   DefaultBufferNumNano,
		MicroNum:  DefaultBufferNumMicro,
		SmallNum:  DefaultBufferNumSmall,
		MediumNum: DefaultBufferNumMedium,
		BigNum:    DefaultBufferNumBig,
		LargeNum:  DefaultBufferNumLarge,
	}
}

// FullBufferGroupsConfig returns configuration with all 12 tiers enabled.
// It uses ~4 GiB per scale. Use it only on high-memory systems.
func FullBufferGroupsConfig() BufferGroupsConfig {
	return BufferGroupsConfig{
		PicoNum:   DefaultBufferNumPico,
		NanoNum:   DefaultBufferNumNano,
		MicroNum:  DefaultBufferNumMicro,
		SmallNum:  DefaultBufferNumSmall,
		MediumNum: DefaultBufferNumMedium,
		BigNum:    DefaultBufferNumBig,
		LargeNum:  DefaultBufferNumLarge,
		GreatNum:  DefaultBufferNumGreat,
		HugeNum:   DefaultBufferNumHuge,
		VastNum:   DefaultBufferNumVast,
		GiantNum:  DefaultBufferNumGiant,
		TitanNum:  DefaultBufferNumTitan,
	}
}

// MinimalBufferGroupsConfig returns a reduced configuration.
// It enables the first 5 tiers (Pico through Medium), totaling ~31 MiB per scale.
func MinimalBufferGroupsConfig() BufferGroupsConfig {
	return BufferGroupsConfig{
		PicoNum:   DefaultBufferNumPico,
		NanoNum:   DefaultBufferNumNano,
		MicroNum:  DefaultBufferNumMicro,
		SmallNum:  DefaultBufferNumSmall,
		MediumNum: DefaultBufferNumMedium,
	}
}
