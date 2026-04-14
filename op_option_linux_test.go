// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"os"
	"testing"
	"time"

	"code.hybscloud.com/uring"
)

// =============================================================================
// OpOption Tests
// =============================================================================

func TestUringOpOptionApply(t *testing.T) {
	opt := &uring.OpOption{}

	opt.Apply(
		uring.WithFileIndex(42),
		uring.WithFlags(7),
		uring.WithIOPrio(9),
		uring.WithPersonality(11),
		uring.WithBacklog(13),
		uring.WithOffset(17),
		uring.WithN(19),
		uring.WithDuration(23*time.Millisecond),
		uring.WithFileMode(0600),
		uring.WithFadvise(29),
		uring.WithCount(31),
		uring.WithReadBufferSize(37),
		uring.WithWriteBufferSize(41),
		uring.WithTimeoutFlags(43),
		uring.WithSpliceFlags(47),
		uring.WithFsyncDataSync(),
	)

	if opt.FileIndex != 42 {
		t.Fatalf("FileIndex: got %d, want 42", opt.FileIndex)
	}
	if opt.Flags != 7 {
		t.Fatalf("Flags: got %d, want 7", opt.Flags)
	}
	if opt.IOPrio != 9 {
		t.Fatalf("IOPrio: got %d, want 9", opt.IOPrio)
	}
	if opt.Personality != 11 {
		t.Fatalf("Personality: got %d, want 11", opt.Personality)
	}
	if opt.Backlog != 13 {
		t.Fatalf("Backlog: got %d, want 13", opt.Backlog)
	}
	if opt.Offset != 17 {
		t.Fatalf("Offset: got %d, want 17", opt.Offset)
	}
	if opt.N == nil || *opt.N != 19 {
		t.Fatalf("N: got %v, want 19", opt.N)
	}
	if opt.Duration != 23*time.Millisecond {
		t.Fatalf("Duration: got %v, want 23ms", opt.Duration)
	}
	if opt.FileMode != 0600 {
		t.Fatalf("FileMode: got %v, want 0600", opt.FileMode)
	}
	if opt.Fadvise != 29 {
		t.Fatalf("Fadvise: got %d, want 29", opt.Fadvise)
	}
	if opt.Count != 31 {
		t.Fatalf("Count: got %d, want 31", opt.Count)
	}
	if opt.ReadBufferSize != 37 {
		t.Fatalf("ReadBufferSize: got %d, want 37", opt.ReadBufferSize)
	}
	if opt.WriteBufferSize != 41 {
		t.Fatalf("WriteBufferSize: got %d, want 41", opt.WriteBufferSize)
	}
	if opt.TimeoutFlags != 43 {
		t.Fatalf("TimeoutFlags: got %d, want 43", opt.TimeoutFlags)
	}
	if opt.SpliceFlags != 47 {
		t.Fatalf("SpliceFlags: got %d, want 47", opt.SpliceFlags)
	}
	if opt.FsyncFlags != 1 {
		t.Fatalf("FsyncFlags: got %d, want 1", opt.FsyncFlags)
	}
}

func TestWithFileIndex(t *testing.T) {
	opt := &uring.OpOption{}
	fn := uring.WithFileIndex(123)
	fn(opt)

	if opt.FileIndex != 123 {
		t.Errorf("FileIndex: got %d, want 123", opt.FileIndex)
	}
}

func TestWithIOPrioClassMasksLevel(t *testing.T) {
	opt := &uring.OpOption{}
	uring.WithIOPrioClass(uring.IOPrioClassBE, 15)(opt)

	want := uint16(uring.IOPrioClassBE)<<13 | 7
	if opt.IOPrio != want {
		t.Fatalf("IOPrio: got %d, want %d", opt.IOPrio, want)
	}
}

func TestTimeoutFlagHelpersCompose(t *testing.T) {
	opt := &uring.OpOption{}
	opt.Apply(
		uring.WithTimeoutFlags(0x80),
		uring.WithTimeoutAbsolute(),
		uring.WithTimeoutBootTime(),
		uring.WithTimeoutRealTime(),
		uring.WithTimeoutEtimeSuccess(),
		uring.WithTimeoutMultishot(),
	)

	want := uint32(0x80 |
		uring.IORING_TIMEOUT_ABS |
		uring.IORING_TIMEOUT_BOOTTIME |
		uring.IORING_TIMEOUT_REALTIME |
		uring.IORING_TIMEOUT_ETIME_SUCCESS |
		uring.IORING_TIMEOUT_MULTISHOT)
	if opt.TimeoutFlags != want {
		t.Fatalf("TimeoutFlags: got %d, want %d", opt.TimeoutFlags, want)
	}
}

func TestFieldSpecificOptionHelpers(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*uring.OpOption)
		check func(*testing.T, *uring.OpOption)
	}{
		{
			name:  "flags",
			apply: uring.WithFlags(3),
			check: func(t *testing.T, opt *uring.OpOption) {
				t.Helper()
				if opt.Flags != 3 {
					t.Fatalf("Flags: got %d, want 3", opt.Flags)
				}
			},
		},
		{
			name:  "duration",
			apply: uring.WithDuration(time.Second),
			check: func(t *testing.T, opt *uring.OpOption) {
				t.Helper()
				if opt.Duration != time.Second {
					t.Fatalf("Duration: got %v, want 1s", opt.Duration)
				}
			},
		},
		{
			name:  "file mode",
			apply: uring.WithFileMode(os.FileMode(0640)),
			check: func(t *testing.T, opt *uring.OpOption) {
				t.Helper()
				if opt.FileMode != 0640 {
					t.Fatalf("FileMode: got %v, want 0640", opt.FileMode)
				}
			},
		},
		{
			name:  "splice flags",
			apply: uring.WithSpliceFlags(12),
			check: func(t *testing.T, opt *uring.OpOption) {
				t.Helper()
				if opt.SpliceFlags != 12 {
					t.Fatalf("SpliceFlags: got %d, want 12", opt.SpliceFlags)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := &uring.OpOption{}
			tt.apply(opt)
			tt.check(t, opt)
		})
	}
}
