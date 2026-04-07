// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "testing"

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
