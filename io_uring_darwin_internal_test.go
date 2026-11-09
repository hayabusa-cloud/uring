// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"io"
	"math"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestDarwinDoCloseUsesRegisteredFileSlot(t *testing.T) {
	slotFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(slotFile): %v", err)
	}
	sqeFile, err := os.Open(os.DevNull)
	if err != nil {
		_ = slotFile.Close()
		t.Fatalf("Open(sqeFile): %v", err)
	}

	ur := &ioUring{
		files: []int32{int32(slotFile.Fd())},
	}
	sqe := &ioUringSqe{
		fd:         int32(sqeFile.Fd()),
		spliceFdIn: 1,
	}

	if got := ur.doClose(sqe); got != 0 {
		_ = slotFile.Close()
		_ = sqeFile.Close()
		t.Fatalf("doClose() = %d, want 0", got)
	}
	if got := ur.files[0]; got != -1 {
		_ = slotFile.Close()
		_ = sqeFile.Close()
		t.Fatalf("files[0] = %d, want -1", got)
	}
	if err := sqeFile.Close(); err != nil {
		_ = slotFile.Close()
		t.Fatalf("sqeFile should stay open: %v", err)
	}
	if err := slotFile.Close(); err == nil {
		t.Fatal("slotFile.Close() succeeded after direct close, want closed descriptor")
	}
}

func TestDarwinDoCloseRejectsEmptyRegisteredFileSlot(t *testing.T) {
	sqeFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(sqeFile): %v", err)
	}

	ur := &ioUring{
		files: []int32{-1},
	}
	sqe := &ioUringSqe{
		fd:         int32(sqeFile.Fd()),
		spliceFdIn: 1,
	}

	if got := ur.doClose(sqe); got != -int32(syscall.EBADF) {
		_ = sqeFile.Close()
		t.Fatalf("doClose() = %d, want %d", got, -int32(syscall.EBADF))
	}
	if err := sqeFile.Close(); err != nil {
		t.Fatalf("sqeFile should stay open on EBADF: %v", err)
	}
}

func tooManyDarwinIovecs() [][]byte {
	return make([][]byte, maxIovecs+1)
}

func TestDarwinSendZeroCopyRejectsOversizedLen32(t *testing.T) {
	var ur ioUring
	payload := mappedTestSlice[byte](t, oversizedLen32(t))

	err := ur.sendZeroCopy(PackDirect(0, 0, 0, 0), payload, 0, len(payload), 0)
	if err != ErrInvalidParam {
		t.Fatalf("sendZeroCopy oversized len error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestDarwinSignedKernelCarrierDomainsRejectOutOfRange(t *testing.T) {
	var ur ioUring
	oversized := oversizedSignedInt32(t)
	payload := mappedTestSlice[byte](t, oversized)
	ctx := PackDirect(0, 0, 0, 7)

	tests := []struct {
		name string
		call func() error
	}{
		{name: "send length", call: func() error { return ur.send(ctx, 0, payload, 0, len(payload)) }},
		{name: "receive length", call: func() error { return ur.receive(ctx, 0, payload, 0, len(payload)) }},
		{name: "receive buffer-select length", call: func() error { return ur.receiveWithBufferSelect(ctx, 0, oversized, 0) }},
		{name: "send zero-copy length", call: func() error { return ur.sendZeroCopy(ctx, payload, 0, len(payload), 0) }},
		{name: "socket domain", call: func() error { return ur.socket(ctx, oversized, SOCK_STREAM, 0) }},
		{name: "socket type", call: func() error { return ur.socket(ctx, AF_INET, oversized, 0) }},
		{name: "socket protocol", call: func() error { return ur.socket(ctx, AF_INET, SOCK_STREAM, oversized) }},
		{name: "socket direct protocol", call: func() error { return ur.socketDirect(ctx, AF_INET, SOCK_STREAM, oversized, 0) }},
		{name: "listen backlog", call: func() error { return ur.listen(ctx, oversized) }},
		{name: "shutdown mode", call: func() error { return ur.shutdown(ctx, SHUT_RDWR+1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != ErrInvalidParam {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
			}
		})
	}
}

func TestDarwinFixedBufferRejectsOversizedLen32(t *testing.T) {
	payload := mappedTestSlice[byte](t, oversizedLen32(t))

	tests := []struct {
		name string
		call func(*ioUring) error
	}{
		{
			name: "readFixed",
			call: func(ur *ioUring) error {
				_, err := ur.readFixed(PackDirect(0, 0, 0, 0), 0, 0, len(payload))
				return err
			},
		},
		{
			name: "writeFixed",
			call: func(ur *ioUring) error {
				return ur.writeFixed(PackDirect(0, 0, 0, 0), 0, 0, len(payload))
			},
		},
		{
			name: "sendFixed",
			call: func(ur *ioUring) error {
				return ur.sendFixed(PackDirect(0, 0, 0, 0), 0, 0, len(payload), 0)
			},
		},
		{
			name: "sendZeroCopyFixed",
			call: func(ur *ioUring) error {
				return ur.sendZeroCopyFixed(PackDirect(0, 0, 0, 0), 0, 0, len(payload), 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ur := &ioUring{bufs: [][]byte{payload}}
			if err := tt.call(ur); err != ErrInvalidParam {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrInvalidParam)
			}
		})
	}
}

func TestDarwinReadWriteVRejectsInvalidIovs(t *testing.T) {
	var ur ioUring
	iovs := tooManyDarwinIovecs()

	if err := ur.readv(PackDirect(0, 0, 0, 0), nil); err != ErrInvalidParam {
		t.Fatalf("readv(nil) error = %v, want %v", err, ErrInvalidParam)
	}
	if err := ur.writev(PackDirect(0, 0, 0, 0), nil); err != ErrInvalidParam {
		t.Fatalf("writev(nil) error = %v, want %v", err, ErrInvalidParam)
	}
	if err := ur.readv(PackDirect(0, 0, 0, 0), iovs); err != ErrInvalidParam {
		t.Fatalf("readv oversized iovs error = %v, want %v", err, ErrInvalidParam)
	}
	if err := ur.writev(PackDirect(0, 0, 0, 0), iovs); err != ErrInvalidParam {
		t.Fatalf("writev oversized iovs error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestDarwinMessageIovecsRejectOversizedCount(t *testing.T) {
	var ur ioUring
	iovs := tooManyDarwinIovecs()
	ctx := PackDirect(0, 0, 0, 7)

	if err := ur.sendmsg(ctx, 0, iovs, nil, nil); err != ErrInvalidParam {
		t.Fatalf("sendmsg oversized iovs error = %v, want %v", err, ErrInvalidParam)
	}
	msg := &Msghdr{Iov: new(IoVec), Iovlen: maxIovecs + 1}
	if err := ur.recvmsg(ctx, 0, msg); err != ErrInvalidParam {
		t.Fatalf("recvmsg oversized Iovlen error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestDarwinTimeoutRejectsCountOutsideUint32(t *testing.T) {
	var ur ioUring
	ts := Timespec{Sec: 1}
	ctx := PackDirect(0, 0, 0, 7)

	for _, count := range []int{-1, oversizedLen32(t)} {
		if err := ur.timeout(ctx, count, &ts, 0); err != ErrInvalidParam {
			t.Fatalf("timeout(count=%d) error = %v, want %v", count, err, ErrInvalidParam)
		}
	}
}

func TestDarwinPollInputContractRejectsInvalidValues(t *testing.T) {
	var ur ioUring
	ctx := PackDirect(0, 0, 0, 7)
	oversized := oversizedLen32(t)

	tests := []struct {
		name string
		call func() error
	}{
		{name: "negative add events", call: func() error { return ur.pollAdd(ctx, 0, -1) }},
		{name: "oversized add events", call: func() error { return ur.pollAdd(ctx, 0, oversized) }},
		{name: "negative update events", call: func() error {
			return ur.pollUpdate(ctx, 1, 0, -1, IORING_POLL_UPDATE_EVENTS)
		}},
		{name: "oversized update events", call: func() error {
			return ur.pollUpdate(ctx, 1, 0, oversized, IORING_POLL_UPDATE_EVENTS)
		}},
		{name: "negative update flags", call: func() error { return ur.pollUpdate(ctx, 1, 0, 0, -1) }},
		{name: "oversized update flags", call: func() error { return ur.pollUpdate(ctx, 1, 0, 0, oversized) }},
		{name: "multishot only", call: func() error {
			return ur.pollUpdate(ctx, 1, 0, 0, IORING_POLL_ADD_MULTI)
		}},
		{name: "multishot events", call: func() error {
			return ur.pollUpdate(ctx, 1, 0, EPOLLIN, IORING_POLL_UPDATE_EVENTS|IORING_POLL_ADD_MULTI)
		}},
		{name: "multishot user data", call: func() error {
			return ur.pollUpdate(ctx, 1, 2, 0, IORING_POLL_UPDATE_USER_DATA|IORING_POLL_ADD_MULTI)
		}},
		{name: "multishot combined", call: func() error {
			return ur.pollUpdate(ctx, 1, 2, EPOLLIN,
				IORING_POLL_UPDATE_EVENTS|IORING_POLL_UPDATE_USER_DATA|IORING_POLL_ADD_MULTI)
		}},
		{name: "user data without flag", call: func() error { return ur.pollUpdate(ctx, 1, 2, 0, 0) }},
		{name: "events without flag", call: func() error { return ur.pollUpdate(ctx, 1, 0, EPOLLIN, 0) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != ErrInvalidParam {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
			}
		})
	}
}

func TestDarwinBoundaryLen32RejectsOversizedInputs(t *testing.T) {
	var ur ioUring
	payload := mappedTestSlice[byte](t, oversizedLen32(t))
	fds := mappedTestSlice[int32](t, oversizedLen32(t))

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "fsetxattr",
			call: func() error {
				return ur.fsetxattr(PackDirect(0, 0, 0, 0), "user.payload", payload, 0)
			},
		},
		{
			name: "setxattr",
			call: func() error {
				return ur.setxattr(PackDirect(0, 0, 0, 0), "/tmp/uring-xattr", "user.payload", payload, 0)
			},
		},
		{
			name: "fgetxattr",
			call: func() error {
				return ur.fgetxattr(PackDirect(0, 0, 0, 0), "user.payload", payload)
			},
		},
		{
			name: "getxattr",
			call: func() error {
				return ur.getxattr(PackDirect(0, 0, 0, 0), "/tmp/uring-xattr", "user.payload", payload)
			},
		},
		{
			name: "filesUpdate",
			call: func() error {
				return ur.filesUpdate(PackDirect(0, 0, 0, 0), fds, 0)
			},
		},
		{
			name: "uringCmd",
			call: func() error {
				return ur.uringCmd(PackDirect(0, 0, 0, 0), 0x1234, payload, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != ErrInvalidParam {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrInvalidParam)
			}
		})
	}
}

func TestDarwinSQEView(t *testing.T) {
	t.Run("ViewSQE from IndirectSQE", func(t *testing.T) {
		indirect := &IndirectSQE{}
		indirect.opcode = IORING_OP_RECV
		indirect.flags = IOSQE_BUFFER_SELECT | IOSQE_ASYNC
		indirect.ioprio = 42
		indirect.fd = 100
		indirect.off = 0x1234567890
		indirect.addr = 0xDEADBEEF
		indirect.len = 4096
		indirect.uflags = 0x8000
		indirect.bufIndex = 7
		indirect.personality = 3
		indirect.spliceFdIn = 50
		indirect.userData = PackDirect(IORING_OP_RECV, indirect.flags, indirect.bufIndex, indirect.fd).Raw()

		view := ViewSQE(indirect)

		if !view.Valid() {
			t.Error("expected Valid() to be true")
		}
		if view.Opcode() != IORING_OP_RECV {
			t.Errorf("Opcode() = %d, want %d", view.Opcode(), IORING_OP_RECV)
		}
		if view.Flags() != IOSQE_BUFFER_SELECT|IOSQE_ASYNC {
			t.Errorf("Flags() = %d, want %d", view.Flags(), IOSQE_BUFFER_SELECT|IOSQE_ASYNC)
		}
		if view.IoPrio() != 42 {
			t.Errorf("IoPrio() = %d, want 42", view.IoPrio())
		}
		if view.RawFD() != 100 {
			t.Errorf("RawFD() = %d, want 100", view.RawFD())
		}
		if view.Off() != 0x1234567890 {
			t.Errorf("Off() = %x, want 0x1234567890", view.Off())
		}
		if view.Addr() != 0xDEADBEEF {
			t.Errorf("Addr() = %x, want 0xDEADBEEF", view.Addr())
		}
		if view.Len() != 4096 {
			t.Errorf("Len() = %d, want 4096", view.Len())
		}
		if view.UFlags() != 0x8000 {
			t.Errorf("UFlags() = %x, want 0x8000", view.UFlags())
		}
		if view.UserData() != indirect.userData {
			t.Errorf("UserData() = %x, want %x", view.UserData(), indirect.userData)
		}
		if view.BufIndex() != 7 {
			t.Errorf("BufIndex() = %d, want 7", view.BufIndex())
		}
		if view.BufGroup() != 7 {
			t.Errorf("BufGroup() = %d, want 7", view.BufGroup())
		}
		if view.Personality() != 3 {
			t.Errorf("Personality() = %d, want 3", view.Personality())
		}
		if view.SpliceFDIn() != 50 {
			t.Errorf("SpliceFDIn() = %d, want 50", view.SpliceFDIn())
		}
		if view.FileIndex() != 50 {
			t.Errorf("FileIndex() = %d, want 50", view.FileIndex())
		}
		if !view.HasBufferSelect() {
			t.Error("expected HasBufferSelect() true")
		}
		if !view.HasAsync() {
			t.Error("expected HasAsync() true")
		}
	})

	t.Run("ViewExtSQE from ExtSQE", func(t *testing.T) {
		ext := &ExtSQE{}
		ext.SQE.opcode = IORING_OP_SEND
		ext.SQE.flags = IOSQE_CQE_SKIP_SUCCESS
		ext.SQE.fd = 200

		view := ViewExtSQE(ext)

		if !view.Valid() {
			t.Error("expected Valid() to be true")
		}
		if view.Opcode() != IORING_OP_SEND {
			t.Errorf("Opcode() = %d, want %d", view.Opcode(), IORING_OP_SEND)
		}
		if view.RawFD() != 200 {
			t.Errorf("RawFD() = %d, want 200", view.RawFD())
		}
		if !view.HasCQESkipSuccess() {
			t.Error("expected HasCQESkipSuccess() to be true")
		}
	})
}

func TestDarwinNewIoUringRejectsEntriesOutsideSetupDomain(t *testing.T) {
	for _, entries := range []int{-1, 0, EntriesHuge + 1} {
		ur, err := newIoUring(entries)
		if ur != nil {
			t.Fatalf("newIoUring(%d) = non-nil ring, want nil", entries)
		}
		if err != ErrInvalidParam {
			t.Fatalf("newIoUring(%d) error = %v, want %v", entries, err, ErrInvalidParam)
		}
	}
}

func TestDarwinNewUringProvideBuffersRoundsAndPartitionsCount(t *testing.T) {
	buffers := newUringProvideBuffers(3, 3)
	if buffers.size != 4 {
		t.Fatalf("size = %d, want 4", buffers.size)
	}
	if buffers.n != 4 {
		t.Fatalf("n = %d, want 4", buffers.n)
	}
	if buffers.gn != 1 || buffers.gMask != 0 {
		t.Fatalf("group geometry = (%d, %d), want (1, 0)", buffers.gn, buffers.gMask)
	}

	buffers = newUringProvideBuffers(3, providedBufferGroupEntries+1)
	if buffers.n != providedBufferGroupEntries {
		t.Fatalf("split n = %d, want %d", buffers.n, providedBufferGroupEntries)
	}
	if buffers.gn != 2 || buffers.gMask != 1 {
		t.Fatalf("split group geometry = (%d, %d), want (2, 1)", buffers.gn, buffers.gMask)
	}
	if err := buffers.register(nil, 0); err != nil {
		t.Fatalf("register split buffers: %v", err)
	}
	if got, want := len(buffers.mem), buffers.size*buffers.n*buffers.gn; got != want {
		t.Fatalf("split backing length = %d, want %d", got, want)
	}
	if got := buffers.buf(bufferGroupIndex(1), 0); len(got) != buffers.size {
		t.Fatalf("second group buffer length = %d, want %d", len(got), buffers.size)
	}
}

func TestDarwinStopJoinsWorkersBeforeClearingState(t *testing.T) {
	for iteration := range 32 {
		ur, err := newIoUring(1)
		if err != nil {
			t.Fatalf("newIoUring[%d]: %v", iteration, err)
		}
		ur.bufs = [][]byte{{1}}
		ur.files = []int32{-1}
		ur.ops = []ioUringProbeOp{{}}
		if err := ur.enable(); err != nil {
			t.Fatalf("enable[%d]: %v", iteration, err)
		}

		stopped := make(chan error, 1)
		go func() {
			stopped <- ur.stop()
		}()

		select {
		case err := <-stopped:
			if err != nil {
				t.Fatalf("stop[%d]: %v", iteration, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("stop[%d] did not join workers", iteration)
		}

		if ur.stopChan != nil {
			t.Fatalf("stopChan[%d] is non-nil after stop", iteration)
		}
		if ur.bufs != nil || ur.files != nil || ur.ops != nil {
			t.Fatalf("worker-visible state[%d] remains after stop", iteration)
		}
	}
}

func TestDarwinSubmissionRetainsTypedPayload(t *testing.T) {
	ur, err := newIoUring(2)
	if err != nil {
		t.Fatalf("newIoUring: %v", err)
	}
	t.Cleanup(func() { _ = ur.stop() })

	payload := []byte("payload-root")
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(payload)))) + 2
	if err := ur.send(PackDirect(0, 0, 0, 7), 0, payload, 2, 4); err != nil {
		t.Fatalf("send: %v", err)
	}
	payload = nil
	runtime.GC()

	submission := <-ur.sqChan
	if got := string(submission.payload); got != "yloa" {
		t.Fatalf("queued payload = %q, want %q", got, "yloa")
	}
	if submission.sqe.addr != wantAddr {
		t.Fatalf("queued addr = %#x, want %#x", submission.sqe.addr, wantAddr)
	}
}

func TestDarwinFixedFileSubmissionBoundary(t *testing.T) {
	t.Run("reject unresolved operation", func(t *testing.T) {
		ur := &ioUring{sqChan: make(chan *darwinSubmission, 1)}
		ctx := PackDirect(0, IOSQE_FIXED_FILE, 0, 0)

		if err := ur.send(ctx, 0, []byte{1}, 0, 1); err != ErrNotSupported {
			t.Fatalf("send fixed-file error = %v, want %v", err, ErrNotSupported)
		}
		if got := len(ur.sqChan); got != 0 {
			t.Fatalf("queued submissions = %d, want 0", got)
		}
	})

	t.Run("preserve close direct", func(t *testing.T) {
		ur := &ioUring{sqChan: make(chan *darwinSubmission, 1)}
		ctx := PackDirect(0, IOSQE_FIXED_FILE, 0, 3)

		if err := ur.close(ctx); err != nil {
			t.Fatalf("close fixed-file: %v", err)
		}
		submission := <-ur.sqChan
		if submission.sqe.flags&IOSQE_FIXED_FILE != 0 {
			t.Fatalf("queued flags = %#x, want fixed-file flag cleared", submission.sqe.flags)
		}
		if got, want := submission.sqe.spliceFdIn, int32(4); got != want {
			t.Fatalf("queued file slot encoding = %d, want %d", got, want)
		}
	})

	t.Run("preserve full-width close direct", func(t *testing.T) {
		ur := &ioUring{sqChan: make(chan *darwinSubmission, 1)}
		indirect := &IndirectSQE{}
		indirect.flags = IOSQE_FIXED_FILE
		indirect.fd = math.MaxInt32

		if err := ur.close(PackIndirect(indirect)); err != nil {
			t.Fatalf("close fixed-file: %v", err)
		}
		submission := <-ur.sqChan
		if got, want := submission.sqe.spliceFdIn, int32(math.MinInt32); got != want {
			t.Fatalf("queued file slot encoding = %d, want %d", got, want)
		}
		if got, want := decodeCloseFileIndex(submission.sqe.spliceFdIn), uint32(math.MaxInt32); got != want {
			t.Fatalf("decoded file index = %d, want %d", got, want)
		}
	})
}

func TestDarwinFixedSendRetainsRegisteredPayloadRange(t *testing.T) {
	backing := []byte("fixed-payload")
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(backing)))) + 2
	ur := &ioUring{
		bufs:   [][]byte{backing},
		sqChan: make(chan *darwinSubmission, 1),
	}

	if err := ur.sendFixed(PackDirect(0, 0, 0, 7), 0, 2, 4, 0); err != nil {
		t.Fatalf("sendFixed: %v", err)
	}
	ur.bufs = nil
	backing = nil
	runtime.GC()

	submission := <-ur.sqChan
	if got := string(submission.payload); got != "xed-" {
		t.Fatalf("queued payload = %q, want %q", got, "xed-")
	}
	if submission.sqe.addr != wantAddr {
		t.Fatalf("queued addr = %#x, want %#x", submission.sqe.addr, wantAddr)
	}
}

func TestDarwinFixedReadWriteRetainSelectedBuffer(t *testing.T) {
	backing := []byte("fixed-payload")
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(backing))))
	ur := &ioUring{
		bufs:   [][]byte{[]byte("other"), backing},
		sqChan: make(chan *darwinSubmission, 2),
	}
	ctx := PackDirect(0, 0, 0, 7)

	readBuf, err := ur.readFixed(ctx, 1, 3, 5)
	if err != nil {
		t.Fatalf("readFixed: %v", err)
	}
	if uint64(uintptr(unsafe.Pointer(unsafe.SliceData(readBuf)))) != wantAddr {
		t.Fatal("readFixed returned a buffer other than the selected registration")
	}
	if err := ur.writeFixed(ctx, 1, 4, 5); err != nil {
		t.Fatalf("writeFixed: %v", err)
	}

	ur.bufs = nil
	backing = nil
	readBuf = nil
	runtime.GC()

	for _, want := range []struct {
		opcode uint8
		offset uint64
	}{
		{opcode: IORING_OP_READ_FIXED, offset: 3},
		{opcode: IORING_OP_WRITE_FIXED, offset: 4},
	} {
		submission := <-ur.sqChan
		if submission.sqe.opcode != want.opcode {
			t.Fatalf("queued opcode = %d, want %d", submission.sqe.opcode, want.opcode)
		}
		if submission.sqe.bufIndex != 1 {
			t.Fatalf("queued bufIndex = %d, want 1", submission.sqe.bufIndex)
		}
		if submission.sqe.off != want.offset {
			t.Fatalf("queued offset = %d, want %d", submission.sqe.off, want.offset)
		}
		if submission.sqe.addr != wantAddr {
			t.Fatalf("queued addr = %#x, want %#x", submission.sqe.addr, wantAddr)
		}
		if got := string(submission.payload); got != "fixed" {
			t.Fatalf("queued payload = %q, want %q", got, "fixed")
		}
	}
}

func TestDarwinPackedExtendedMirrorIncludesFinalUserData(t *testing.T) {
	ur, err := newIoUring(2)
	if err != nil {
		t.Fatalf("newIoUring: %v", err)
	}
	t.Cleanup(func() { _ = ur.stop() })

	ext := &ExtSQE{}
	ctx := PackExtended(ext)
	if err := ur.nop(ctx); err != nil {
		t.Fatalf("nop: %v", err)
	}
	if got := ViewExtSQE(ext).UserData(); got != ctx.WithOp(IORING_OP_NOP).Raw() {
		t.Fatalf("mirrored userData = %#x, want %#x", got, ctx.WithOp(IORING_OP_NOP).Raw())
	}
}

func TestDarwinSubmitExtendedRejectsUnrootedBufferOpcode(t *testing.T) {
	ur, err := newIoUring(2)
	if err != nil {
		t.Fatalf("newIoUring: %v", err)
	}
	t.Cleanup(func() { _ = ur.stop() })

	ext := &ExtSQE{SQE: ioUringSqe{opcode: IORING_OP_READ, len: 1}}
	if err := ur.submitExtended(PackExtended(ext)); err != ErrNotSupported {
		t.Fatalf("submitExtended error = %v, want %v", err, ErrNotSupported)
	}
	if len(ur.sqChan) != 0 {
		t.Fatalf("queued submissions = %d, want 0", len(ur.sqChan))
	}
}

func TestDarwinReadWriteUsesPositionedOffsets(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "positioned-io")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	if _, err := file.WriteString("abcdef"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if _, err := file.Seek(5, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	ur := &ioUring{}
	readBuf := make([]byte, 2)
	if got := ur.doRead(&ioUringSqe{fd: int32(file.Fd()), off: 1}, readBuf); got != int32(len(readBuf)) {
		t.Fatalf("positioned read result = %d, want %d", got, len(readBuf))
	}
	if got := string(readBuf); got != "bc" {
		t.Fatalf("positioned read payload = %q, want %q", got, "bc")
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil {
		t.Fatalf("current offset after positioned read: %v", err)
	} else if offset != 5 {
		t.Fatalf("current offset after positioned read = %d, want 5", offset)
	}

	writeBuf := []byte("XY")
	if got := ur.doWrite(&ioUringSqe{fd: int32(file.Fd()), off: 2}, writeBuf); got != int32(len(writeBuf)) {
		t.Fatalf("positioned write result = %d, want %d", got, len(writeBuf))
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil {
		t.Fatalf("current offset after positioned write: %v", err)
	} else if offset != 5 {
		t.Fatalf("current offset after positioned write = %d, want 5", offset)
	}

	contents := make([]byte, 6)
	if _, err := file.ReadAt(contents, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if got := string(contents); got != "abXYef" {
		t.Fatalf("file contents = %q, want %q", got, "abXYef")
	}

	currentBuf := make([]byte, 1)
	if got := ur.doRead(&ioUringSqe{fd: int32(file.Fd()), off: math.MaxUint64}, currentBuf); got != 1 {
		t.Fatalf("current-position read result = %d, want 1", got)
	}
	if got := string(currentBuf); got != "f" {
		t.Fatalf("current-position read payload = %q, want %q", got, "f")
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil {
		t.Fatalf("current offset after current-position read: %v", err)
	} else if offset != 6 {
		t.Fatalf("current offset after current-position read = %d, want 6", offset)
	}

	currentWrite := []byte("!")
	if got := ur.doWrite(&ioUringSqe{fd: int32(file.Fd()), off: math.MaxUint64}, currentWrite); got != 1 {
		t.Fatalf("current-position write result = %d, want 1", got)
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil {
		t.Fatalf("current offset after current-position write: %v", err)
	} else if offset != 7 {
		t.Fatalf("current offset after current-position write = %d, want 7", offset)
	}

	contents = make([]byte, 7)
	if _, err := file.ReadAt(contents, 0); err != nil {
		t.Fatalf("ReadAt after current-position write: %v", err)
	}
	if got := string(contents); got != "abXYef!" {
		t.Fatalf("file contents after current-position write = %q, want %q", got, "abXYef!")
	}
}

func TestDarwinExecuteFixedReadWrite(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "fixed-io")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	if _, err := file.WriteString("abcdef"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	ur := &ioUring{}
	readBuf := make([]byte, 2)
	readCQE := ur.executeSQE(&darwinSubmission{
		sqe: ioUringSqe{
			opcode: IORING_OP_READ_FIXED,
			fd:     int32(file.Fd()),
			off:    1,
			len:    uint32(len(readBuf)),
		},
		payload: readBuf,
	})
	if readCQE == nil || readCQE.res != int32(len(readBuf)) {
		t.Fatalf("fixed read completion = %#v, want result %d", readCQE, len(readBuf))
	}
	if got := string(readBuf); got != "bc" {
		t.Fatalf("fixed read payload = %q, want %q", got, "bc")
	}

	writeBuf := []byte("XY")
	writeCQE := ur.executeSQE(&darwinSubmission{
		sqe: ioUringSqe{
			opcode: IORING_OP_WRITE_FIXED,
			fd:     int32(file.Fd()),
			off:    2,
			len:    uint32(len(writeBuf)),
		},
		payload: writeBuf,
	})
	if writeCQE == nil || writeCQE.res != int32(len(writeBuf)) {
		t.Fatalf("fixed write completion = %#v, want result %d", writeCQE, len(writeBuf))
	}

	contents := make([]byte, 6)
	if _, err := file.ReadAt(contents, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if got := string(contents); got != "abXYef" {
		t.Fatalf("file contents = %q, want %q", got, "abXYef")
	}
}

func TestDarwinReadWriteOffsetZeroFallsBackForPipes(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	ur := &ioUring{}
	payload := []byte("x")
	if got := ur.doWrite(&ioUringSqe{fd: int32(writer.Fd())}, payload); got != 1 {
		t.Fatalf("pipe write result = %d, want 1", got)
	}

	buf := make([]byte, 1)
	if got := ur.doRead(&ioUringSqe{fd: int32(reader.Fd())}, buf); got != 1 {
		t.Fatalf("pipe read result = %d, want 1", got)
	}
	if got := string(buf); got != "x" {
		t.Fatalf("pipe read payload = %q, want %q", got, "x")
	}
}

func TestDarwinReadWriteRejectsOffsetsOutsideInt64(t *testing.T) {
	invalidOffset := uint64(math.MaxInt64) + 1
	sqe := &ioUringSqe{fd: -1, off: invalidOffset}
	ur := &ioUring{}

	if got := ur.doRead(sqe, make([]byte, 1)); got != -int32(syscall.EINVAL) {
		t.Fatalf("read result = %d, want -EINVAL", got)
	}
	if got := ur.doWrite(sqe, []byte{0}); got != -int32(syscall.EINVAL) {
		t.Fatalf("write result = %d, want -EINVAL", got)
	}
}

func TestDarwinDoSendReportsPartialProgress(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer syscall.Close(fds[0])
	defer syscall.Close(fds[1])

	if err := syscall.SetNonblock(fds[0], true); err != nil {
		t.Fatalf("set nonblock: %v", err)
	}
	if err := syscall.SetsockoptInt(fds[0], syscall.SOL_SOCKET, syscall.SO_SNDBUF, 4096); err != nil {
		t.Fatalf("set send buffer: %v", err)
	}
	sendBufferSize, err := syscall.GetsockoptInt(fds[0], syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	if err != nil {
		t.Fatalf("get send buffer: %v", err)
	}

	payload := make([]byte, max(1<<20, sendBufferSize*4))
	res := (&ioUring{}).doSend(&ioUringSqe{fd: int32(fds[0])}, payload)
	if res <= 0 {
		t.Fatalf("doSend result = %d, want positive progress", res)
	}
	if int(res) >= len(payload) {
		t.Fatalf("doSend result = %d, want a short send below %d", res, len(payload))
	}
}
