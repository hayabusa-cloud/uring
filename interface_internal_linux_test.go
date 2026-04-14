// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "testing"

func TestOptionsApplySupportsPackageOptionHelpers(t *testing.T) {
	opt := &Options{}
	opt.Apply(LargeLockedBufferMemOptions, MultiSizeBufferOptions)

	if opt.LockedBufferMem != registerBufferSize*registerBufferNum {
		t.Fatalf("LockedBufferMem = %d, want %d", opt.LockedBufferMem, registerBufferSize*registerBufferNum)
	}
	if opt.MultiSizeBuffer != 1 {
		t.Fatalf("MultiSizeBuffer = %d, want 1", opt.MultiSizeBuffer)
	}
}

func TestFlagOnlyOptionHelpersDefaultAndOverride(t *testing.T) {
	var ur Uring
	opts := []OpOptionFunc{WithFlags(7)}

	tests := []struct {
		name string
		fn   func([]OpOptionFunc) uint8
	}{
		{"operationOptions", ur.operationOptions},
		{"pollAddOptions", ur.pollAddOptions},
		{"pollRemoveOptions", ur.pollRemoveOptions},
		{"timeoutRemoveOptions", ur.timeoutRemoveOptions},
		{"asyncCancelOptions", ur.asyncCancelOptions},
		{"connectOptions", ur.connectOptions},
		{"openAtOptions", ur.openAtOptions},
		{"closeOptions", ur.closeOptions},
		{"statxOptions", ur.statxOptions},
		{"shutdownOptions", ur.shutdownOptions},
		{"renameAtOptions", ur.renameAtOptions},
		{"unlinkAtOptions", ur.unlinkAtOptions},
		{"mkdirAtOptions", ur.mkdirAtOptions},
		{"symlinkAtOptions", ur.symlinkAtOptions},
		{"linkAtOptions", ur.linkAtOptions},
		{"bindOptions", ur.bindOptions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(nil); got != uringOpFlagsNone {
				t.Fatalf("%s(nil) = %d, want %d", tt.name, got, uringOpFlagsNone)
			}
			if got := tt.fn(opts); got != 7 {
				t.Fatalf("%s(opts) = %d, want 7", tt.name, got)
			}
		})
	}
}

func TestStreamOptionsSharedAcrossReadWriteSendReceive(t *testing.T) {
	var ur Uring
	buf := make([]byte, 8)
	opts := []OpOptionFunc{
		WithFlags(3),
		WithIOPrio(7),
		WithOffset(11),
		WithN(5),
	}

	wantFlags, wantIOPrio, wantOffset, wantN := ur.streamOptions(buf, opts)

	check := func(name string, gotFlags uint8, gotIOPrio uint16, gotOffset int64, gotN int) {
		t.Helper()
		if gotFlags != wantFlags || gotIOPrio != wantIOPrio || gotOffset != wantOffset || gotN != wantN {
			t.Fatalf("%s = (%d, %d, %d, %d), want (%d, %d, %d, %d)", name, gotFlags, gotIOPrio, gotOffset, gotN, wantFlags, wantIOPrio, wantOffset, wantN)
		}
	}

	flags, ioprio, offset, n := ur.readOptions(buf, opts)
	check("readOptions", flags, ioprio, offset, n)

	flags, ioprio, offset, n = ur.writeOptions(buf, opts)
	check("writeOptions", flags, ioprio, offset, n)

	flags, ioprio, offset, n = ur.sendOptions(buf, opts)
	check("sendOptions", flags, ioprio, offset, n)

	flags, ioprio, offset, n = ur.receiveOptions(buf, opts)
	check("receiveOptions", flags, ioprio, offset, n)
}

func TestStreamOptionsNilBufferStaysZeroLength(t *testing.T) {
	var ur Uring

	_, _, _, n := ur.receiveOptions(nil, nil)
	if n != 0 {
		t.Fatalf("receiveOptions(nil, nil) length = %d, want 0", n)
	}
}

func TestMsgOptionsSharedAcrossSendmsgRecvmsg(t *testing.T) {
	var ur Uring
	opts := []OpOptionFunc{
		WithFlags(5),
		WithIOPrio(9),
	}

	wantFlags, wantIOPrio := ur.msgOptions(opts)

	check := func(name string, gotFlags uint8, gotIOPrio uint16) {
		t.Helper()
		if gotFlags != wantFlags || gotIOPrio != wantIOPrio {
			t.Fatalf("%s = (%d, %d), want (%d, %d)", name, gotFlags, gotIOPrio, wantFlags, wantIOPrio)
		}
	}

	flags, ioprio := ur.sendmsgOptions(opts)
	check("sendmsgOptions", flags, ioprio)

	flags, ioprio = ur.recvmsgOptions(opts)
	check("recvmsgOptions", flags, ioprio)
}

func TestFlagsIOPrioHelpersDefaultAndOverride(t *testing.T) {
	var ur Uring
	opts := []OpOptionFunc{
		WithFlags(3),
		WithIOPrio(11),
	}

	tests := []struct {
		name string
		fn   func([]OpOptionFunc) (uint8, uint16)
	}{
		{"flagsIOPrioOptions", ur.flagsIOPrioOptions},
		{"msgOptions", ur.msgOptions},
		{"sendmsgOptions", ur.sendmsgOptions},
		{"recvmsgOptions", ur.recvmsgOptions},
		{"acceptOptions", ur.acceptOptions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, ioprio := tt.fn(nil)
			if flags != uringOpFlagsNone || ioprio != uringOpIOPrioNone {
				t.Fatalf("%s(nil) = (%d, %d), want (%d, %d)", tt.name, flags, ioprio, uringOpFlagsNone, uringOpIOPrioNone)
			}

			flags, ioprio = tt.fn(opts)
			if flags != 3 || ioprio != 11 {
				t.Fatalf("%s(opts) = (%d, %d), want (3, 11)", tt.name, flags, ioprio)
			}
		})
	}
}

func TestReadFixedOptionsDefaultAndClamp(t *testing.T) {
	ur := Uring{
		ioUring: &ioUring{
			bufs: [][]byte{make([]byte, 16)},
		},
	}

	flags, offset, n := ur.readFixedOptions(0, nil)
	if flags != uringOpFlagsNone || offset != 0 || n != 16 {
		t.Fatalf("readFixedOptions(nil) = (%d, %d, %d), want (%d, %d, %d)", flags, offset, n, uringOpFlagsNone, 0, 16)
	}

	flags, offset, n = ur.readFixedOptions(0, []OpOptionFunc{WithFlags(5), WithOffset(9), WithN(8)})
	if flags != 5 || offset != 9 || n != 8 {
		t.Fatalf("readFixedOptions(shrunk) = (%d, %d, %d), want (5, 9, 8)", flags, offset, n)
	}

	_, _, n = ur.readFixedOptions(0, []OpOptionFunc{WithN(32)})
	if n != 16 {
		t.Fatalf("readFixedOptions(oversized N) = %d, want 16", n)
	}
}

func TestWriteFixedOptionsDefaultAndOverride(t *testing.T) {
	var ur Uring

	flags, offset := ur.writeFixedOptions(nil)
	if flags != uringOpFlagsNone || offset != 0 {
		t.Fatalf("writeFixedOptions(nil) = (%d, %d), want (%d, %d)", flags, offset, uringOpFlagsNone, 0)
	}

	flags, offset = ur.writeFixedOptions([]OpOptionFunc{WithFlags(4), WithOffset(12)})
	if flags != 4 || offset != 12 {
		t.Fatalf("writeFixedOptions(opts) = (%d, %d), want (4, 12)", flags, offset)
	}
}

func TestTimeoutOptionHelpersDefaultAndOverride(t *testing.T) {
	var ur Uring

	flags, cnt, timeoutFlags := ur.timeoutOptions(nil)
	if flags != uringOpFlagsNone || cnt != 1 || timeoutFlags != 0 {
		t.Fatalf("timeoutOptions(nil) = (%d, %d, %d), want (%d, %d, %d)", flags, cnt, timeoutFlags, uringOpFlagsNone, 1, 0)
	}

	opts := []OpOptionFunc{
		WithFlags(6),
		WithCount(4),
		WithTimeoutAbsolute(),
		WithTimeoutMultishot(),
	}
	flags, cnt, timeoutFlags = ur.timeoutOptions(opts)
	wantTimeoutFlags := uint32(IORING_TIMEOUT_ABS | IORING_TIMEOUT_MULTISHOT)
	if flags != 6 || cnt != 4 || timeoutFlags != wantTimeoutFlags {
		t.Fatalf("timeoutOptions(opts) = (%d, %d, %d), want (6, 4, %d)", flags, cnt, timeoutFlags, wantTimeoutFlags)
	}

	flags, timeoutFlags = ur.linkTimeoutOptions(opts)
	if flags != 6 || timeoutFlags != wantTimeoutFlags {
		t.Fatalf("linkTimeoutOptions(opts) = (%d, %d), want (6, %d)", flags, timeoutFlags, wantTimeoutFlags)
	}

	flags, timeoutFlags = ur.timeoutUpdateOptions(opts)
	if flags != 6 || timeoutFlags != wantTimeoutFlags {
		t.Fatalf("timeoutUpdateOptions(opts) = (%d, %d), want (6, %d)", flags, timeoutFlags, wantTimeoutFlags)
	}
}

func TestSpliceAndTeeOptionHelpersDefaultAndOverride(t *testing.T) {
	var ur Uring

	tests := []struct {
		name string
		fn   func([]OpOptionFunc) (uint8, int, uint32)
	}{
		{"spliceOptions", ur.spliceOptions},
		{"teeOptions", ur.teeOptions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, n, spliceFlags := tt.fn(nil)
			if flags != uringOpFlagsNone || n != bufferSizeDefault || spliceFlags != 0 {
				t.Fatalf("%s(nil) = (%d, %d, %d), want (%d, %d, %d)", tt.name, flags, n, spliceFlags, uringOpFlagsNone, bufferSizeDefault, 0)
			}

			flags, n, spliceFlags = tt.fn([]OpOptionFunc{WithFlags(2), WithN(77), WithSpliceFlags(9)})
			if flags != 2 || n != 77 || spliceFlags != 9 {
				t.Fatalf("%s(opts) = (%d, %d, %d), want (2, 77, 9)", tt.name, flags, n, spliceFlags)
			}
		})
	}
}

func TestSocketListenAndSyncOptionHelpers(t *testing.T) {
	var ur Uring

	flags, fileIndex := ur.socketOptions(nil)
	if flags != uringOpFlagsNone || fileIndex != 0 {
		t.Fatalf("socketOptions(nil) = (%d, %d), want (%d, %d)", flags, fileIndex, uringOpFlagsNone, 0)
	}
	flags, fileIndex = ur.socketOptions([]OpOptionFunc{WithFlags(3), WithFileIndex(21)})
	if flags != 3 || fileIndex != 21 {
		t.Fatalf("socketOptions(opts) = (%d, %d), want (3, 21)", flags, fileIndex)
	}

	flags, backlog := ur.listenOptions(nil)
	if flags != uringOpFlagsNone || backlog != defaultBacklog {
		t.Fatalf("listenOptions(nil) = (%d, %d), want (%d, %d)", flags, backlog, uringOpFlagsNone, defaultBacklog)
	}
	flags, backlog = ur.listenOptions([]OpOptionFunc{WithFlags(4), WithBacklog(55)})
	if flags != 4 || backlog != 55 {
		t.Fatalf("listenOptions(opts) = (%d, %d), want (4, 55)", flags, backlog)
	}

	flags, fsyncFlags := ur.syncOptions(nil)
	if flags != uringOpFlagsNone || fsyncFlags != 0 {
		t.Fatalf("syncOptions(nil) = (%d, %d), want (%d, %d)", flags, fsyncFlags, uringOpFlagsNone, 0)
	}
	flags, fsyncFlags = ur.syncOptions([]OpOptionFunc{WithFlags(5), WithFsyncDataSync()})
	if flags != 5 || fsyncFlags != 1 {
		t.Fatalf("syncOptions(opts) = (%d, %d), want (5, 1)", flags, fsyncFlags)
	}
}

func TestReceiveWithBufferSelectOptionsUsesConfiguredGroupSource(t *testing.T) {
	fd := &mockPollFd{fd: 5}

	t.Run("simple buffers", func(t *testing.T) {
		buffers := newUringProvideBuffers(BufferSizeMedium, 4)
		buffers.setGIDOffset(9)
		ur := Uring{buffers: buffers}

		flags, ioprio, size, gid := ur.receiveWithBufferSelectOptions(fd, nil)
		if flags != uringOpFlagsNone || ioprio != uringOpIOPrioNone || size != bufferSizeDefault {
			t.Fatalf("receiveWithBufferSelectOptions(nil) = (%d, %d, %d, %d), want (%d, %d, %d, %d)", flags, ioprio, size, gid, uringOpFlagsNone, uringOpIOPrioNone, bufferSizeDefault, buffers.bufGroup(fd))
		}
		if gid != buffers.bufGroup(fd) {
			t.Fatalf("receiveWithBufferSelectOptions(nil) gid = %d, want %d", gid, buffers.bufGroup(fd))
		}
	})

	t.Run("buffer groups", func(t *testing.T) {
		groups := newUringBufferGroupsWithConfig(4, BufferGroupsConfig{
			PicoNum:   16,
			MediumNum: 16,
		})
		groups.setGIDOffset(7)
		ur := Uring{bufferGroups: groups}
		opts := []OpOptionFunc{
			WithFlags(4),
			WithIOPrio(12),
			WithReadBufferSize(BufferSizePico),
		}

		flags, ioprio, size, gid := ur.receiveWithBufferSelectOptions(fd, opts)
		wantGID := groups.bufGroupBySize(fd, BufferSizePico)
		if flags != 4 || ioprio != 12 || size != BufferSizePico || gid != wantGID {
			t.Fatalf("receiveWithBufferSelectOptions(opts) = (%d, %d, %d, %d), want (4, 12, %d, %d)", flags, ioprio, size, gid, BufferSizePico, wantGID)
		}
	})
}

func TestGetBufferGidPanicsWithoutProviders(t *testing.T) {
	var ur Uring
	defer func() {
		if recover() == nil {
			t.Fatal("getBufferGid() did not panic without buffers or bufferGroups")
		}
	}()
	ur.getBufferGid(&mockPollFd{fd: 0}, BufferSizeMedium)
}
