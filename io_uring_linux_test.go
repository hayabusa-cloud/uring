// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/zcall"
)

func newTestIoUring(t *testing.T, options ...func(*ioUringParams)) *ioUring {
	t.Helper()

	ur, err := newIoUring(16, options...)
	if err != nil {
		t.Fatalf("new io-uring: %v", err)
	}
	t.Cleanup(func() {
		if err := ur.stop(); err != nil {
			t.Fatalf("stop io-uring: %v", err)
		}
	})
	return ur
}

func newPollingDirectFilePath(t *testing.T) string {
	t.Helper()

	var probeErr error
	for _, base := range []string{".", "/var/tmp"} {
		path, err := newDirectFilePathInDir(t, base)
		if err == nil {
			return path
		}
		probeErr = errors.Join(probeErr, fmt.Errorf("%s: %w", base, err))
	}

	t.Skipf("polling file test requires O_DIRECT support; no usable path found in current working directory or /var/tmp; probe errors: %v", probeErr)
	return ""
}

func newDirectFilePathInDir(t *testing.T, base string) (string, error) {
	t.Helper()

	info, err := os.Stat(base)
	if err != nil {
		return "", fmt.Errorf("stat base: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("base is not a directory")
	}

	dir, err := os.MkdirTemp(base, "uring-direct-*")
	if err != nil {
		return "", fmt.Errorf("create probe dir: %w", err)
	}

	probe := filepath.Join(dir, "probe_direct.txt")
	f, err := os.OpenFile(probe, os.O_RDWR|os.O_CREATE|zcall.O_DIRECT, 0660)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("open O_DIRECT probe: %w", err)
	}
	block := AlignedMemBlock()
	n, writeErr := f.Write(block)
	closeErr := f.Close()
	_ = os.Remove(probe)
	if writeErr != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write O_DIRECT probe: %w", writeErr)
	}
	if closeErr != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("close O_DIRECT probe: %w", closeErr)
	}
	if n != len(block) {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("short O_DIRECT probe write: wrote %d of %d bytes", n, len(block))
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return filepath.Join(dir, "test_f_direct.txt"), nil
}

func TestIOUring_BasicUsage(t *testing.T) {
	fr := func(t *testing.T, ur *ioUring, path string) {
		defer os.Remove(path)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|zcall.O_DIRECT, 0660)
		if err != nil {
			t.Errorf("open file: %v", err)
			return
		}
		defer f.Close()

		s := AlignedMemBlock()
		copy(s, "test0123456789")
		n, err := f.Write(s)
		if err != nil {
			t.Errorf("write file: %v", err)
			return
		}
		if n != len(s) {
			t.Errorf("short write file expected %d but written %d bytes", len(s), n)
			return
		}

		_, err = f.Seek(0, 0)
		if err != nil {
			t.Errorf("seek file: %v", err)
			return
		}

		payload := AlignedMemBlock()
		sqeCtx := PackDirect(0, uringOpFlagsNone, 0, int32(f.Fd()))
		err = ur.read(sqeCtx, uringOpIOPrioNone, payload, 0, len(payload))
		if err != nil {
			t.Errorf("submission readv: %v", err)
			return
		}

		err = ur.enter()
		if err != nil {
			t.Errorf("io_ring_enter: %v", err)
			return
		}

		dl := time.Now().Add(2 * time.Second)
		b := iox.Backoff{}
		for {
			if time.Now().After(dl) {
				t.Error("read file timeout")
				return
			}
			err := ur.poll(1)
			if err != nil {
				t.Errorf("io_ring_enter poll: %v", err)
				return
			}
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				b.Wait()
				continue
			}
			if err != nil {
				t.Errorf("wait completion: %v", err)
				return
			}
			if cqe.res < 0 {
				t.Errorf("read file: %v", errFromErrno(uintptr(-cqe.res)))
				return
			}
			if cqe.res != int32(len(payload)) {
				t.Errorf("read file: %v", io.ErrShortWrite)
				return
			}
			break
		}

		if !bytes.Equal(payload, s) {
			t.Error("file read write wrong")
			return
		}
	}

	fw := func(t *testing.T, ur *ioUring, path string) {
		defer os.Remove(path)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|zcall.O_DIRECT, 0660)
		if err != nil {
			t.Errorf("open file: %v", err)
			return
		}
		defer f.Close()

		s := "test0123456789"
		payload := AlignedMemBlock()
		copy(payload, s)
		sqeCtx := PackDirect(0, uringOpFlagsNone, 0, int32(f.Fd()))
		err = ur.write(sqeCtx, uringOpIOPrioNone, payload, 0, len(payload))
		if err != nil {
			t.Errorf("submission write: %v", err)
			return
		}

		err = ur.enter()
		if err != nil {
			t.Errorf("io_ring_enter: %v", err)
			return
		}

		dl := time.Now().Add(2 * time.Second)
		backoff := iox.Backoff{}
		for {
			if time.Now().After(dl) {
				t.Error("write file timeout")
				return
			}
			err := ur.poll(1)
			if err != nil {
				t.Errorf("io_ring_enter poll: %v", err)
				return
			}
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				backoff.Wait()
				continue
			}
			if err != nil {
				t.Errorf("wait completion: %v", err)
				return
			}
			if cqe.res < 0 {
				err := errFromErrno(uintptr(-cqe.res))
				if err == ErrNotSupported && ur.params.flags&zcall.IORING_SETUP_IOPOLL != 0 {
					// O_DIRECT support alone does not establish IOPOLL support.
					t.Skipf("file or device does not support IOPOLL: %v", err)
				}
				t.Errorf("write file: %v", err)
				return
			}
			if cqe.res != int32(len(payload)) {
				t.Errorf("write file: %v", io.ErrShortWrite)
				return
			}
			break
		}

		_, err = f.Seek(0, 0)
		if err != nil {
			t.Errorf("seek file: %v", err)
			return
		}

		b := AlignedMemBlock()
		_, err = f.Read(b)
		if err != nil && err != io.EOF {
			t.Errorf("read file: %v", err)
			return
		}

		if !bytes.Equal(payload[:len(s)], b[:len(s)]) {
			t.Error("file read write wrong")
			return
		}
	}

	udsr := func(t *testing.T, ur *ioUring) {
		so, err := newUnixSocketPair()
		if err != nil {
			t.Errorf("unix socket pair: %v", err)
			return
		}
		defer zcall.Close(uintptr(so[0]))
		defer zcall.Close(uintptr(so[1]))

		wb := []byte("test0123456789")
		_, errno := zcall.Sendto(uintptr(so[1]), wb, 0, nil, 0)
		if errno != 0 {
			t.Errorf("socket send: %v", errFromErrno(errno))
			return
		}

		rb := make([]byte, len(wb))
		sqeCtx := PackDirect(0, 0, 0, int32(so[0]))
		err = ur.receive(sqeCtx, 0, rb, 0, len(rb))
		if err != nil {
			t.Errorf("submit recv: %v", err)
			return
		}

		err = ur.enter()
		if err != nil {
			t.Errorf("io_uring enter: %v", err)
			return
		}

		dl := time.Now().Add(2 * time.Second)
		b := iox.Backoff{}
		for {
			if time.Now().After(dl) {
				t.Error("read socket timeout")
				return
			}
			err := ur.poll(1)
			if err != nil {
				t.Errorf("io_ring_enter poll: %v", err)
				return
			}
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				b.Wait()
				continue
			}
			if err != nil {
				t.Errorf("wait completion: %v", err)
				return
			}
			if cqe.res < 0 {
				t.Errorf("socket recv: %v", errFromErrno(uintptr(-cqe.res)))
				return
			}
			if cqe.res != int32(len(wb)) {
				t.Errorf("socket recv: %v", io.ErrShortWrite)
				return
			}
			break
		}

		if !bytes.Equal(wb, rb) {
			t.Errorf("socked recv expected %s but got %s", wb, rb)
			return
		}
		return

	}

	udsw := func(t *testing.T, ur *ioUring) {
		so, err := newUnixSocketPair()
		if err != nil {
			t.Errorf("unix socket pair: %v", err)
			return
		}
		defer zcall.Close(uintptr(so[0]))
		defer zcall.Close(uintptr(so[1]))

		wb := []byte("test0123456789")
		sqeCtx := PackDirect(0, uringOpFlagsNone, 0, int32(so[1]))
		err = ur.send(sqeCtx, uringOpIOPrioNone, wb, 0, len(wb))
		if err != nil {
			t.Errorf("submit send: %v", err)
			return
		}

		err = ur.enter()
		if err != nil {
			t.Errorf("io_uring enter: %v", err)
			return
		}

		dl := time.Now().Add(2 * time.Second)
		b := iox.Backoff{}
		for {
			if time.Now().After(dl) {
				t.Error("write socket timeout")
				return
			}
			err := ur.poll(1)
			if err != nil {
				t.Errorf("io_ring_enter poll: %v", err)
				return
			}
			cqe, err := ur.wait()
			if err == iox.ErrWouldBlock {
				b.Wait()
				continue
			}
			if err != nil {
				t.Errorf("wait completion: %v", err)
				return
			}
			if cqe.res < 0 {
				t.Errorf("socket send: %v", errFromErrno(uintptr(-cqe.res)))
				return
			}
			if cqe.res != int32(len(wb)) {
				t.Errorf("socket send: %v", io.ErrShortWrite)
				return
			}
			break
		}

		rb := make([]byte, len(wb))
		n, errno := zcall.Recvfrom(uintptr(so[0]), rb, MSG_WAITALL, nil, nil)
		if errno != 0 {
			t.Errorf("socket recv: %v", errFromErrno(errno))
			return
		}
		if int(n) != len(wb) {
			t.Errorf("socked recv: %v", io.ErrUnexpectedEOF)
			return
		}
		if !bytes.Equal(wb, rb) {
			t.Errorf("socked recv expected %s but got %s", wb, rb)
			return
		}

		return
	}

	t.Run("normal mode read file", func(t *testing.T) {
		ur := newTestIoUring(t)
		fr(t, ur, "test_f_direct.txt")
	})

	t.Run("normal mode write file", func(t *testing.T) {
		ur := newTestIoUring(t)
		fw(t, ur, "test_f_direct.txt")
	})

	t.Run("normal mode read socket", func(t *testing.T) {
		ur := newTestIoUring(t)
		udsr(t, ur)
	})

	t.Run("normal mode write socket", func(t *testing.T) {
		ur := newTestIoUring(t)
		udsw(t, ur)
	})

	t.Run("io poll mode write file", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringIoPollOptions)
		fw(t, ur, newPollingDirectFilePath(t))
	})

	t.Run("sq poll mode read file", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringSqPollOptions)
		fr(t, ur, newPollingDirectFilePath(t))
	})

	t.Run("sq poll mode write file", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringSqPollOptions)
		fw(t, ur, newPollingDirectFilePath(t))
	})

	t.Run("sq poll mode read socket", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringSqPollOptions)
		udsr(t, ur)
	})

	t.Run("sq poll mode write socket", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringSqPollOptions)
		udsw(t, ur)
	})

	t.Run("io sq poll mode write file", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringIoPollOptions, ioUringSqPollOptions)
		fw(t, ur, newPollingDirectFilePath(t))
	})
}

func TestIoUring_BufferSelect(t *testing.T) {
	ur := newTestIoUring(t)

	s := make([]byte, 16*BufferSizeMicro)
	matrix := sliceOfMicroArray(s, 0, 16)
	ptr := unsafe.Pointer(unsafe.SliceData(matrix))
	sqeCtx := PackDirect(0, 0, 0, 0)
	err := ur.provideBuffers(sqeCtx, len(matrix), 0, ptr, BufferSizeMicro, 0)
	if err != nil {
		t.Errorf("provide buffer: %v", err)
		return
	}
	so, err := newUnixSocketPair()
	if err != nil {
		t.Errorf("uds pair: %v", err)
		return
	}
	defer zcall.Close(uintptr(so[0]))
	defer zcall.Close(uintptr(so[1]))
	wBuf := [BufferSizeMicro]byte{}
	copy(wBuf[:], "hello sox!")
	writeCtx := PackDirect(0, uringOpFlagsNone, 0, int32(so[1]))
	err = ur.write(writeCtx, uringOpIOPrioNone, wBuf[:], 0, 10)
	if err != nil {
		t.Errorf("write fixed: %v", err)
		return
	}
	readCtx := PackDirect(0, IOSQE_BUFFER_SELECT, 0, int32(so[0]))
	err = ur.readWithBufferSelect(readCtx, 10, 0)
	if err != nil {
		t.Errorf("read with buffer select: %v", err)
		return
	}

	_ = ur.enter()
	cnt := 0
	for range 10 {
		time.Sleep(100 * time.Millisecond)
		cqe, err := ur.wait()
		if err == iox.ErrWouldBlock {
			continue
		}
		if err != nil {
			t.Errorf("io-uring wait cq: %v", err)
			continue
		}
		if cqe.res < 0 {
			t.Errorf("io-uring wait cq: %v", errFromErrno(uintptr(-cqe.res)))
			continue
		}
		sqeCtx := cqe.Context()
		op := sqeCtx.Op()
		if op != IORING_OP_PROVIDE_BUFFERS && op != IORING_OP_WRITE && op != IORING_OP_READ {
			t.Errorf("expected write or read op but got: %d", op)
			return
		}
		if op == IORING_OP_READ && cqe.res != 10 {
			t.Errorf("read %d bytes, expected 10", cqe.res)
			return
		}
		cnt++
	}
	if cnt != 3 {
		t.Errorf("expected 3 operations but got %d", cnt)
		return
	}
}

func TestIoUringBufRingHeadAndAvailable(t *testing.T) {
	ur := newTestIoUring(t)
	const groupID = 71

	br, backing, err := ur.registerBufRing(8, groupID)
	if err != nil {
		t.Fatalf("registerBufRing: %v", err)
	}
	t.Cleanup(func() {
		if err := ur.unregisterBufRing(groupID); err != nil {
			t.Fatalf("unregisterBufRing: %v", err)
		}
	})

	ur.bufRingInit(br)

	var head uint16 = 99
	if ret := ur.bufRingHead(groupID, &head); ret != 0 {
		t.Fatalf("bufRingHead(valid) = %d, want 0", ret)
	}
	if head != 0 {
		t.Fatalf("head = %d, want 0", head)
	}
	if got := ur.bufRingAvailable(br, groupID); got != 0 {
		t.Fatalf("bufRingAvailable(initial) = %d, want 0", got)
	}

	data := make([]byte, 32)
	ur.bufRingAdd(br, uintptr(unsafe.Pointer(unsafe.SliceData(data))), len(data), 0, 7, 0)
	ur.bufRingAdvance(br, 1)

	head = 99
	if ret := ur.bufRingHead(groupID, &head); ret != 0 {
		t.Fatalf("bufRingHead(after add) = %d, want 0", ret)
	}
	if head != 0 {
		t.Fatalf("head after add = %d, want 0", head)
	}
	if got := ur.bufRingAvailable(br, groupID); got != 1 {
		t.Fatalf("bufRingAvailable(after add) = %d, want 1", got)
	}

	_ = backing
	_ = data
}

func TestIoUringBufRingHeadAndAvailableInvalidGroup(t *testing.T) {
	ur := newTestIoUring(t)

	var head uint16 = 77
	if ret := ur.bufRingHead(0xffff, &head); ret >= 0 {
		t.Fatalf("bufRingHead(invalid) = %d, want negative errno", ret)
	}
	if head != 77 {
		t.Fatalf("head on failure = %d, want unchanged 77", head)
	}

	if got := ur.bufRingAvailable(&ioUringBufRing{tail: 1}, 0xffff); got >= 0 {
		t.Fatalf("bufRingAvailable(invalid) = %d, want negative errno", got)
	}
}

func TestMadviseEncodesSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	buf := make([]byte, 4096)
	ctx := PackDirect(0, 0, 0, 0)
	if err := ring.ioUring.madvise(ctx, buf, 4); err != nil {
		t.Fatalf("madvise: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_MADVISE {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_MADVISE)
	}
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf))))
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.len != 4096 {
		t.Fatalf("len = %d, want 4096", sqe.len)
	}
	if sqe.uflags != 4 {
		t.Fatalf("uflags = %d, want 4 (MADV_DONTNEED)", sqe.uflags)
	}
}

func TestEpollCtlEncodesSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	ctx := PackDirect(0, 0, 0, 7)
	if err := ring.ioUring.epollCtl(ctx, 1, 42, 0x001); err != nil {
		t.Fatalf("epollCtl: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_EPOLL_CTL {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_EPOLL_CTL)
	}
	if sqe.fd != 7 {
		t.Fatalf("fd = %d, want 7", sqe.fd)
	}
	if sqe.off != 42 {
		t.Fatalf("off = %d, want 42 (targetFd)", sqe.off)
	}
	if sqe.len != 1 {
		t.Fatalf("len = %d, want 1 (EPOLL_CTL_ADD)", sqe.len)
	}
	if sqe.addr == 0 {
		t.Fatal("addr = 0, want non-zero epoll event pointer")
	}
}

func TestRemoveBuffersEncodesSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	ctx := PackDirect(0, 0, 0, 0)
	if err := ring.ioUring.removeBuffers(ctx, 8, 3); err != nil {
		t.Fatalf("removeBuffers: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_REMOVE_BUFFERS {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_REMOVE_BUFFERS)
	}
	if sqe.fd != 8 {
		t.Fatalf("fd = %d, want 8 (num)", sqe.fd)
	}
	if sqe.bufIndex != 3 {
		t.Fatalf("bufIndex = %d, want 3 (group)", sqe.bufIndex)
	}
}

func TestSendWithOffsetEncodesSubrange(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("hello-send-offset")
	const offset = 4
	ctx := PackDirect(0, 0, 0, 5)
	if err := ring.ioUring.send(ctx, 0, data, offset, len(data)-offset); err != nil {
		t.Fatalf("send: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(data)))) + offset
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.len != uint32(len(data)-offset) {
		t.Fatalf("len = %d, want %d", sqe.len, len(data)-offset)
	}
}

func TestSendRejectsRangePastPayload(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("send-range")
	ctx := PackDirect(0, 0, 0, 5)
	if err := ring.ioUring.send(ctx, 0, data, 1, len(data)); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("send error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestReceiveWithOffsetEncodesSubrange(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("hello-receive-offset")
	const offset = 6
	ctx := PackDirect(0, 0, 0, 7)
	if err := ring.ioUring.receive(ctx, 0, data, offset, len(data)-offset); err != nil {
		t.Fatalf("receive: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(data)))) + offset
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.len != uint32(len(data)-offset) {
		t.Fatalf("len = %d, want %d", sqe.len, len(data)-offset)
	}
	if sqe.uflags != MSG_WAITALL {
		t.Fatalf("uflags = %d, want %d", sqe.uflags, MSG_WAITALL)
	}
}

func TestReceiveRejectsRangePastPayload(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("receive-range")
	ctx := PackDirect(0, 0, 0, 7)
	if err := ring.ioUring.receive(ctx, 0, data, 1, len(data)); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("receive error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestStreamSubmitRejectsOversizedLen32(t *testing.T) {
	payload := mappedTestSlice[byte](t, oversizedLen32(t))
	udpAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9996}

	tests := []struct {
		name string
		call func(*Uring) error
	}{
		{
			name: "Read",
			call: func(ring *Uring) error {
				return ring.Read(PackDirect(0, 0, 0, 3), payload)
			},
		},
		{
			name: "Write",
			call: func(ring *Uring) error {
				return ring.Write(PackDirect(0, 0, 0, 4), payload)
			},
		},
		{
			name: "Send",
			call: func(ring *Uring) error {
				return ring.Send(PackDirect(0, 0, 0, 5), &mockPollFd{fd: 5}, payload)
			},
		},
		{
			name: "Receive",
			call: func(ring *Uring) error {
				return ring.Receive(PackDirect(0, 0, 0, 6), &mockPollFd{fd: 6}, payload)
			},
		},
		{
			name: "sendZeroCopy",
			call: func(ring *Uring) error {
				return ring.ioUring.sendZeroCopy(PackDirect(0, 0, 0, 7), payload, 0, len(payload), 0)
			},
		},
		{
			name: "sendtoZeroCopy with addr",
			call: func(ring *Uring) error {
				return ring.ioUring.sendtoZeroCopy(PackDirect(0, 0, 0, 8), payload, 0, len(payload), udpAddr, 0)
			},
		},
		{
			name: "sendtoZeroCopy without addr",
			call: func(ring *Uring) error {
				return ring.ioUring.sendtoZeroCopy(PackDirect(0, 0, 0, 9), payload, 0, len(payload), nil, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperTestRing(t)
			tail := atomic.LoadUint32(ring.ioUring.sq.kTail)

			err := tt.call(ring)
			if !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrInvalidParam)
			}
			if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
				t.Fatalf("SQ tail = %d, want %d", got, tail)
			}
		})
	}
}

func TestFixedBufferSubmitRejectsOversizedLen32(t *testing.T) {
	payload := mappedTestSlice[byte](t, oversizedLen32(t))

	tests := []struct {
		name string
		call func(*Uring) error
	}{
		{
			name: "readFixed",
			call: func(ring *Uring) error {
				_, err := ring.ioUring.readFixed(PackDirect(0, 0, 0, 3), 0, 0, len(payload))
				return err
			},
		},
		{
			name: "writeFixed",
			call: func(ring *Uring) error {
				return ring.ioUring.writeFixed(PackDirect(0, 0, 0, 4), 0, 0, len(payload))
			},
		},
		{
			name: "sendFixed",
			call: func(ring *Uring) error {
				return ring.ioUring.sendFixed(PackDirect(0, 0, 0, 5), 0, 0, len(payload), 0)
			},
		},
		{
			name: "sendZeroCopyFixed",
			call: func(ring *Uring) error {
				return ring.ioUring.sendZeroCopyFixed(PackDirect(0, 0, 0, 6), 0, 0, len(payload), 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperTestRing(t)
			ring.ioUring.bufs = [][]byte{payload}
			tail := atomic.LoadUint32(ring.ioUring.sq.kTail)

			err := tt.call(ring)
			if !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrInvalidParam)
			}
			if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
				t.Fatalf("SQ tail = %d, want %d", got, tail)
			}
		})
	}
}

func TestBoundaryLen32RejectsOversizedInputs(t *testing.T) {
	payload := mappedTestSlice[byte](t, oversizedLen32(t))
	oversizedFDs := mappedTestSlice[int32](t, oversizedLen32(t))

	tests := []struct {
		name string
		call func(*Uring) error
	}{
		{
			name: "Madvise",
			call: func(ring *Uring) error {
				return ring.ioUring.madvise(PackDirect(0, 0, 0, 0), payload, 0)
			},
		},
		{
			name: "FSetXattr",
			call: func(ring *Uring) error {
				return ring.FSetXattr(ForFD(5), "user.payload", payload, 0)
			},
		},
		{
			name: "SetXattr",
			call: func(ring *Uring) error {
				return ring.SetXattr(PackDirect(0, 0, 0, 0), "/tmp/uring-xattr", "user.payload", payload, 0)
			},
		},
		{
			name: "FGetXattr",
			call: func(ring *Uring) error {
				return ring.FGetXattr(ForFD(6), "user.payload", payload)
			},
		},
		{
			name: "GetXattr",
			call: func(ring *Uring) error {
				return ring.GetXattr(PackDirect(0, 0, 0, 0), "/tmp/uring-xattr", "user.payload", payload)
			},
		},
		{
			name: "FilesUpdate",
			call: func(ring *Uring) error {
				return ring.FilesUpdate(PackDirect(0, 0, 0, 0), oversizedFDs, 0)
			},
		},
		{
			name: "UringCmd",
			call: func(ring *Uring) error {
				return ring.UringCmd(ForFD(7), 0x1234, payload)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			tail := atomic.LoadUint32(ring.ioUring.sq.kTail)

			err := tt.call(ring)
			if !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrInvalidParam)
			}
			if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
				t.Fatalf("SQ tail = %d, want %d", got, tail)
			}
		})
	}
}

func TestIovecSubmitRejectsTooManyIovecs(t *testing.T) {
	iovs := make([][]byte, maxIovecs+1)
	tests := []struct {
		name string
		call func(*Uring) error
	}{
		{name: "ReadV", call: func(ring *Uring) error {
			return ring.ReadV(PackDirect(0, 0, 0, 3), iovs)
		}},
		{name: "WriteV", call: func(ring *Uring) error {
			return ring.WriteV(PackDirect(0, 0, 0, 4), iovs)
		}},
		{name: "ReadvFixed", call: func(ring *Uring) error {
			return ring.ReadvFixed(PackDirect(0, 0, 0, 5), 0, 0, iovs)
		}},
		{name: "WritevFixed", call: func(ring *Uring) error {
			return ring.WritevFixed(PackDirect(0, 0, 0, 6), 0, 0, iovs)
		}},
		{name: "SendMsg", call: func(ring *Uring) error {
			return ring.SendMsg(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 7}, iovs, nil, nil)
		}},
		{name: "RecvMsg", call: func(ring *Uring) error {
			msg := &Msghdr{Iov: new(IoVec), Iovlen: maxIovecs + 1}
			return ring.RecvMsg(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 8}, msg)
		}},
		{name: "SendmsgZeroCopy", call: func(ring *Uring) error {
			return ring.ioUring.sendmsgZeroCopy(PackDirect(0, 0, 0, 9), 0, iovs, nil, nil, 0)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			ring.ioUring.bufs = [][]byte{{1}}
			tail := atomic.LoadUint32(ring.ioUring.sq.kTail)

			if err := tt.call(ring); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParam)
			}
			if got := atomic.LoadUint32(ring.ioUring.sq.kTail); got != tail {
				t.Fatalf("SQ tail = %d, want %d", got, tail)
			}
			idx := int(tail) % len(ring.ioUring.submit.keepAlive)
			keep := &ring.ioUring.submit.keepAlive[idx]
			if keep.active || keep.iovs != nil || keep.ext != nil {
				t.Fatal("invalid vector count reached iovec staging")
			}
		})
	}
}

func TestMessageSubmitAcceptsIovecDomainEndpoints(t *testing.T) {
	tests := []struct {
		name         string
		stagesIovecs bool
		call         func(*Uring, [][]byte) error
	}{
		{name: "SendMsg", stagesIovecs: true, call: func(ring *Uring, iovs [][]byte) error {
			return ring.SendMsg(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 7}, iovs, nil, nil)
		}},
		{name: "RecvMsg", call: func(ring *Uring, iovs [][]byte) error {
			vec := make([]IoVec, len(iovs))
			msg := &Msghdr{Iov: unsafe.SliceData(vec), Iovlen: uint64(len(vec))}
			return ring.RecvMsg(PackDirect(0, 0, 0, 0), &mockPollFd{fd: 8}, msg)
		}},
		{name: "SendmsgZeroCopy", stagesIovecs: true, call: func(ring *Uring, iovs [][]byte) error {
			return ring.ioUring.sendmsgZeroCopy(PackDirect(0, 0, 0, 9), 0, iovs, nil, nil, 0)
		}},
	}

	for _, n := range []int{0, maxIovecs} {
		var iovs [][]byte
		if n > 0 {
			iovs = make([][]byte, n)
		}
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/%d", tt.name, n), func(t *testing.T) {
				ring := newWrapperSyntheticSubmitRing(t, 8)

				if err := tt.call(ring, iovs); err != nil {
					t.Fatalf("error = %v", err)
				}
				keep := &ring.ioUring.submit.keepAlive[0]
				if !keep.active || keep.ext == nil {
					t.Fatal("message submit keepAlive is inactive")
				}
				if tt.stagesIovecs {
					if len(keep.iovs) != n {
						t.Fatalf("staged iovecs = %d, want %d", len(keep.iovs), n)
					}
					if keep.ext.msg.Iovlen != uint64(n) {
						t.Fatalf("staged message iovlen = %d, want %d", keep.ext.msg.Iovlen, n)
					}
					return
				}
				if keep.iovs != nil {
					t.Fatalf("RecvMsg staged %d package-owned iovecs, want none", len(keep.iovs))
				}
				if keep.ext.recvMsg == nil || keep.ext.recvMsg.Iovlen != uint64(n) {
					var got uint64
					if keep.ext.recvMsg != nil {
						got = keep.ext.recvMsg.Iovlen
					}
					t.Fatalf("caller message iovlen = %d, want %d", got, n)
				}
			})
		}
	}
}

func TestTimeoutCountDomain(t *testing.T) {
	maxCount := uint64(^uint32(0))
	valid := []int{0}
	if uint64(^uint(0)) > maxCount {
		valid = append(valid, int(maxCount))
	}
	for _, count := range valid {
		t.Run(fmt.Sprintf("accept/%d", count), func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)

			if err := ring.Timeout(PackDirect(0, 0, 0, 0), time.Second, WithCount(count)); err != nil {
				t.Fatalf("Timeout(count=%d): %v", count, err)
			}
			sqe := lastSubmittedSQE(t, ring)
			if sqe.off != uint64(count) {
				t.Fatalf("SQE off = %d, want %d", sqe.off, count)
			}
		})
	}

	invalid := []int{-1}
	if uint64(^uint(0)) > maxCount {
		invalid = append(invalid, int(maxCount+1))
	}
	for _, count := range invalid {
		t.Run(fmt.Sprintf("reject/%d", count), func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)
			keep := &ring.ioUring.submit.keepAlive[0]
			sentinel := Timespec{Sec: 7, Nsec: 11}
			keep.timespec = sentinel
			before := ring.ioUring.sqCount()

			if err := ring.Timeout(PackDirect(0, 0, 0, 0), time.Second, WithCount(count)); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("Timeout(count=%d) error = %v, want %v", count, err, ErrInvalidParam)
			}
			if got := ring.ioUring.sqCount(); got != before {
				t.Fatalf("SQ count = %d, want %d", got, before)
			}
			if keep.active || keep.timespec != sentinel {
				t.Fatalf("invalid count touched submit staging: active=%t timespec=%+v", keep.active, keep.timespec)
			}
		})
	}
}

func TestPollInputContract(t *testing.T) {
	maxUint32 := uint64(math.MaxUint32)
	validEvents := []struct {
		name   string
		events int
	}{
		{name: "zero", events: 0},
		{name: "epoll-in", events: EPOLLIN},
	}
	if uint64(^uint(0)) > maxUint32 {
		validEvents = append(validEvents, struct {
			name   string
			events int
		}{name: "uint32-max", events: int(maxUint32)})
	}

	for _, tt := range validEvents {
		t.Run("add/"+tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)
			if err := ring.PollAdd(PackDirect(0, 0, 0, 7), tt.events); err != nil {
				t.Fatalf("PollAdd(events=%d): %v", tt.events, err)
			}
			sqe := lastSubmittedSQE(t, ring)
			if sqe.opcode != IORING_OP_POLL_ADD || sqe.len != 0 || sqe.uflags != uint32(tt.events) {
				t.Fatalf("SQE = {opcode:%d len:%d uflags:%#x}, want {%d 0 %#x}",
					sqe.opcode, sqe.len, sqe.uflags, IORING_OP_POLL_ADD, uint32(tt.events))
			}
		})
	}

	t.Run("add/multishot", func(t *testing.T) {
		ring := newWrapperSyntheticSubmitRing(t, 2)
		if err := ring.PollAddMultishot(PackDirect(0, 0, 0, 7), EPOLLIN); err != nil {
			t.Fatalf("PollAddMultishot: %v", err)
		}
		sqe := lastSubmittedSQE(t, ring)
		if sqe.len != IORING_POLL_ADD_MULTI || sqe.uflags != EPOLLIN {
			t.Fatalf("SQE = {len:%d uflags:%#x}, want {%d %#x}",
				sqe.len, sqe.uflags, IORING_POLL_ADD_MULTI, EPOLLIN)
		}
	})

	invalidEvents := []struct {
		name   string
		events int
	}{{name: "negative", events: -1}}
	if uint64(^uint(0)) > maxUint32 {
		invalidEvents = append(invalidEvents, struct {
			name   string
			events int
		}{name: "above-uint32", events: int(maxUint32 + 1)})
	}
	for _, tt := range invalidEvents {
		t.Run("add/reject-"+tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)
			before := ring.ioUring.sqCount()
			if err := ring.PollAdd(PackDirect(0, 0, 0, 7), tt.events); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("PollAdd(events=%d) error = %v, want %v", tt.events, err, ErrInvalidParam)
			}
			if got := ring.ioUring.sqCount(); got != before {
				t.Fatalf("SQ count = %d, want %d", got, before)
			}
		})
	}

	levelCalls := []struct {
		name string
		call func(*Uring) error
	}{
		{name: "single", call: func(ring *Uring) error {
			return ring.PollAddLevel(PackDirect(0, 0, 0, 7), EPOLLIN)
		}},
		{name: "multishot", call: func(ring *Uring) error {
			return ring.PollAddMultishotLevel(PackDirect(0, 0, 0, 7), EPOLLIN)
		}},
	}
	for _, tt := range levelCalls {
		t.Run("level/reject-"+tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)
			before := ring.ioUring.sqCount()
			if err := tt.call(ring); !errors.Is(err, ErrNotSupported) {
				t.Fatalf("error = %v, want %v", err, ErrNotSupported)
			}
			if got := ring.ioUring.sqCount(); got != before {
				t.Fatalf("SQ count = %d, want %d", got, before)
			}
		})
	}

	type updateCase struct {
		name        string
		newUserData uint64
		newEvents   int
		flags       int
	}
	validUpdates := []updateCase{
		{name: "remove"},
		{name: "events", newEvents: EPOLLIN, flags: IORING_POLL_UPDATE_EVENTS},
		{name: "user-data", newUserData: 17, flags: IORING_POLL_UPDATE_USER_DATA},
		{
			name:        "combined",
			newUserData: 19,
			newEvents:   EPOLLOUT,
			flags: IORING_POLL_UPDATE_EVENTS |
				IORING_POLL_UPDATE_USER_DATA,
		},
	}
	if uint64(^uint(0)) > maxUint32 {
		validUpdates = append(validUpdates, updateCase{
			name: "uint32-max-events", newEvents: int(maxUint32), flags: IORING_POLL_UPDATE_EVENTS,
		})
	}
	for _, tt := range validUpdates {
		t.Run("update/"+tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)
			const oldUserData = uint64(11)
			if err := ring.PollUpdate(PackDirect(0, 0, 0, 7), oldUserData, tt.newUserData, tt.newEvents, tt.flags); err != nil {
				t.Fatalf("PollUpdate: %v", err)
			}
			sqe := lastSubmittedSQE(t, ring)
			if sqe.opcode != IORING_OP_POLL_REMOVE || sqe.addr != oldUserData ||
				sqe.off != tt.newUserData || sqe.len != uint32(tt.flags) ||
				sqe.uflags != uint32(tt.newEvents) {
				t.Fatalf("SQE = {opcode:%d addr:%d off:%d len:%d uflags:%#x}",
					sqe.opcode, sqe.addr, sqe.off, sqe.len, sqe.uflags)
			}
		})
	}

	invalidUpdates := []updateCase{
		{name: "negative-events", newEvents: -1, flags: IORING_POLL_UPDATE_EVENTS},
		{name: "negative-flags", flags: -1},
		{name: "unknown-flag", flags: IORING_POLL_ADD_LEVEL},
		{name: "multishot-only", flags: IORING_POLL_ADD_MULTI},
		{name: "multishot-events", newEvents: EPOLLIN, flags: IORING_POLL_UPDATE_EVENTS | IORING_POLL_ADD_MULTI},
		{name: "multishot-user-data", newUserData: 1, flags: IORING_POLL_UPDATE_USER_DATA | IORING_POLL_ADD_MULTI},
		{
			name:        "multishot-combined",
			newUserData: 1,
			newEvents:   EPOLLIN,
			flags: IORING_POLL_UPDATE_EVENTS |
				IORING_POLL_UPDATE_USER_DATA |
				IORING_POLL_ADD_MULTI,
		},
		{name: "user-data-without-flag", newUserData: 1},
		{name: "events-without-flag", newEvents: EPOLLIN},
	}
	if uint64(^uint(0)) > maxUint32 {
		invalidUpdates = append(invalidUpdates,
			updateCase{name: "events-above-uint32", newEvents: int(maxUint32 + 1), flags: IORING_POLL_UPDATE_EVENTS},
			updateCase{name: "flags-above-uint32", flags: int(maxUint32 + 1)},
		)
	}
	for _, tt := range invalidUpdates {
		t.Run("update/reject-"+tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 2)
			before := ring.ioUring.sqCount()
			if err := ring.PollUpdate(PackDirect(0, 0, 0, 7), 11, tt.newUserData, tt.newEvents, tt.flags); !errors.Is(err, ErrInvalidParam) {
				t.Fatalf("PollUpdate error = %v, want %v", err, ErrInvalidParam)
			}
			if got := ring.ioUring.sqCount(); got != before {
				t.Fatalf("SQ count = %d, want %d", got, before)
			}
		})
	}
}

func TestVectorSubmitAcceptsMaxIovecs(t *testing.T) {
	iovs := make([][]byte, maxIovecs)
	fixedBuf := []byte{1}
	fixedIovs := make([][]byte, maxIovecs)
	for i := range fixedIovs {
		fixedIovs[i] = fixedBuf
	}
	tests := []struct {
		name string
		call func(*Uring) error
	}{
		{name: "ReadV", call: func(ring *Uring) error {
			return ring.ReadV(PackDirect(0, 0, 0, 3), iovs)
		}},
		{name: "WriteV", call: func(ring *Uring) error {
			return ring.WriteV(PackDirect(0, 0, 0, 4), iovs)
		}},
		{name: "ReadvFixed", call: func(ring *Uring) error {
			return ring.ReadvFixed(PackDirect(0, 0, 0, 5), 0, 0, fixedIovs)
		}},
		{name: "WritevFixed", call: func(ring *Uring) error {
			return ring.WritevFixed(PackDirect(0, 0, 0, 6), 0, 0, fixedIovs)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := newWrapperSyntheticSubmitRing(t, 8)
			ring.ioUring.bufs = [][]byte{fixedBuf}

			if err := tt.call(ring); err != nil {
				t.Fatalf("error = %v", err)
			}
			sqe := lastSubmittedSQE(t, ring)
			if sqe.len != maxIovecs {
				t.Fatalf("SQE len = %d, want %d", sqe.len, maxIovecs)
			}
			keep := &ring.ioUring.submit.keepAlive[0]
			if !keep.active || len(keep.iovs) != maxIovecs {
				t.Fatalf("staged iovecs = %d, want %d", len(keep.iovs), maxIovecs)
			}
		})
	}
}

func TestSendZeroCopyEncodesSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("hello-zc")
	ctx := PackDirect(0, 0, 0, 5)
	if err := ring.ioUring.sendZeroCopy(ctx, data, 0, len(data), 0); err != nil {
		t.Fatalf("sendZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_SEND_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND_ZC)
	}
	if sqe.fd != 5 {
		t.Fatalf("fd = %d, want 5", sqe.fd)
	}
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(data))))
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.len != uint32(len(data)) {
		t.Fatalf("len = %d, want %d", sqe.len, len(data))
	}
}

func TestSendZeroCopyWithOffsetEncodesSubrange(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("hello-zc-offset")
	const offset = 3
	ctx := PackDirect(0, 0, 0, 5)
	if err := ring.ioUring.sendZeroCopy(ctx, data, offset, len(data)-offset, 0); err != nil {
		t.Fatalf("sendZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(data)))) + offset
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
	if sqe.len != uint32(len(data)-offset) {
		t.Fatalf("len = %d, want %d", sqe.len, len(data)-offset)
	}
}

func TestSendZeroCopyRejectsRangePastPayload(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("send-zc-range")
	ctx := PackDirect(0, 0, 0, 5)
	if err := ring.ioUring.sendZeroCopy(ctx, data, 1, len(data), 0); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("sendZeroCopy error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestSendtoZeroCopyWithAddrEncodesSQE(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	data := []byte("sendto-zc")
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}
	ctx := PackDirect(0, 0, 0, 10)
	if err := ring.ioUring.sendtoZeroCopy(ctx, data, 0, len(data), addr, 0); err != nil {
		t.Fatalf("sendtoZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_SEND_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND_ZC)
	}
	if sqe.fd != 10 {
		t.Fatalf("fd = %d, want 10", sqe.fd)
	}
	if sqe.off == 0 {
		t.Fatal("off = 0, want non-zero sockaddr pointer")
	}
	if sqe.spliceFdIn == 0 {
		t.Fatal("spliceFdIn = 0, want non-zero sockaddr length")
	}
}

func TestSendtoZeroCopyWithAddrAppliesOffset(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	data := []byte("sendto-zc-offset")
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9998}
	const offset = 2
	ctx := PackDirect(0, 0, 0, 10)
	if err := ring.ioUring.sendtoZeroCopy(ctx, data, offset, len(data)-offset, addr, 0); err != nil {
		t.Fatalf("sendtoZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	wantAddr := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(data)))) + offset
	if sqe.addr != wantAddr {
		t.Fatalf("addr = %#x, want %#x", sqe.addr, wantAddr)
	}
	if sqe.len != uint32(len(data)-offset) {
		t.Fatalf("len = %d, want %d", sqe.len, len(data)-offset)
	}
}

func TestSendtoZeroCopyWithAddrRejectsRangePastPayload(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	data := []byte("sendto-zc-range")
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9997}
	ctx := PackDirect(0, 0, 0, 10)
	if err := ring.ioUring.sendtoZeroCopy(ctx, data, 1, len(data), addr, 0); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("sendtoZeroCopy error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestSendtoZeroCopyWithoutAddrEncodesSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("sendto-zc-noaddr")
	ctx := PackDirect(0, 0, 0, 11)
	if err := ring.ioUring.sendtoZeroCopy(ctx, data, 0, len(data), nil, 0x40); err != nil {
		t.Fatalf("sendtoZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_SEND_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SEND_ZC)
	}
	if sqe.fd != 11 {
		t.Fatalf("fd = %d, want 11", sqe.fd)
	}
	if sqe.uflags != 0x40 {
		t.Fatalf("uflags = %#x, want 0x40", sqe.uflags)
	}
	if sqe.off != 0 {
		t.Fatalf("off = %d, want 0", sqe.off)
	}
}

func TestSendtoZeroCopyWithoutAddrRejectsRangePastPayload(t *testing.T) {
	ring := newWrapperTestRing(t)

	data := []byte("sendto-zc-noaddr-range")
	ctx := PackDirect(0, 0, 0, 11)
	if err := ring.ioUring.sendtoZeroCopy(ctx, data, 1, len(data), nil, 0); !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("sendtoZeroCopy error = %v, want %v", err, ErrInvalidParam)
	}
}

func TestNewIoUringMapsSharedRingAtMaxSize(t *testing.T) {
	ur := newTestIoUring(t)

	want := uintptr(max(ur.sq.ringSz, ur.cq.ringSz))
	if got := ur.ringMapSz(); got != want {
		t.Fatalf("ringMapSz = %d, want %d", got, want)
	}
	if ur.ringMapSz() < uintptr(ur.cq.ringSz) {
		t.Fatalf("ringMapSz = %d, cq ring size = %d", ur.ringMapSz(), ur.cq.ringSz)
	}
}

func TestSendmsgZeroCopyEncodesSQE(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	buffers := [][]byte{[]byte("msg-zc")}
	oob := []byte{1, 2, 3, 4}
	to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8888}
	sa := AddrToSockaddr(to)

	ctx := PackDirect(0, 0, 0, 12)
	if err := ring.ioUring.sendmsgZeroCopy(ctx, 0, buffers, oob, sa, 0); err != nil {
		t.Fatalf("sendmsgZeroCopy: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_SENDMSG_ZC {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_SENDMSG_ZC)
	}
	if sqe.fd != 12 {
		t.Fatalf("fd = %d, want 12", sqe.fd)
	}
	if sqe.addr == 0 {
		t.Fatal("addr = 0, want non-zero msghdr pointer")
	}
	if sqe.len != 1 {
		t.Fatalf("len = %d, want 1", sqe.len)
	}
}

func TestReadMultishotEncodesExactSQE(t *testing.T) {
	ring := newWrapperSyntheticSubmitRing(t, 8)

	ctx := PackDirect(0, 0, 0, 15)
	if err := ring.ioUring.readMultishot(ctx, 4096, 7); err != nil {
		t.Fatalf("readMultishot: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_READ_MULTISHOT {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_READ_MULTISHOT)
	}
	if sqe.fd != 15 {
		t.Fatalf("fd = %d, want 15", sqe.fd)
	}
	if sqe.off != 4096 {
		t.Fatalf("off = %d, want 4096", sqe.off)
	}
	if sqe.addr != 0 {
		t.Fatalf("addr = %#x, want 0", sqe.addr)
	}
	if sqe.len != 0 {
		t.Fatalf("len = %d, want 0", sqe.len)
	}
	if sqe.flags&IOSQE_BUFFER_SELECT == 0 {
		t.Fatal("IOSQE_BUFFER_SELECT not set")
	}
	if sqe.bufIndex != 7 {
		t.Fatalf("bufIndex = %d, want 7 (group)", sqe.bufIndex)
	}
	if sqe.ioprio != 0 || sqe.uflags != 0 || sqe.personality != 0 || sqe.spliceFdIn != 0 {
		t.Fatal("readMultishot encoded nonzero optional fields")
	}
}

func TestFeatureReturnsCorrectly(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	if !ring.ioUring.feature(1 << 0) {
		t.Error("feature(1<<0) = false, want true")
	}
	if ring.ioUring.feature(1 << 31) {
		t.Error("feature(1<<31) = true, want false")
	}
}

func TestUnregisterBuffersWithoutRegistration(t *testing.T) {
	ring := newStartedSharedTestRing(t)
	_ = ring.ioUring.unregisterBuffers()
}

func TestInlineCmdData128(t *testing.T) {
	ring := newWrapperTestRing(t)

	if err := ring.Nop(PackDirect(IORING_OP_NOP, 0, 0, 0)); err != nil {
		t.Fatalf("Nop: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	slot := submitSlot{sqe: sqe}
	if data := slot.inlineCmdData128(); data != nil {
		t.Fatal("inlineCmdData128 != nil for non-SQE128 ring")
	}
}

func TestNoCopyLockUnlock(t *testing.T) {
	var nc noCopy
	nc.Lock()
	nc.Unlock()
}

func TestUnregisterBufRingWithoutRegistration(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	if err := ring.ioUring.unregisterBufRing(9999); err == nil {
		t.Fatal("unregisterBufRing(9999) = nil, want error")
	}
}

func TestRegisterPollerOnClosedRing(t *testing.T) {
	ring, err := New(testMinimalBufferOptions, func(opt *Options) {
		opt.Entries = EntriesNano
		opt.MultiIssuers = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ring.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	_, err = ring.ioUring.registerPoller(nil)
	if err != ErrClosed {
		t.Fatalf("registerPoller on closed ring = %v, want %v", err, ErrClosed)
	}
}

func TestRingLayoutHelpers(t *testing.T) {
	if got, want := sqeBytesFromFlags(0), int(unsafe.Sizeof(ioUringSqe{})); got != want {
		t.Fatalf("sqeBytesFromFlags(0) = %d, want %d", got, want)
	}
	if got, want := sqeBytesFromFlags(IORING_SETUP_SQE128), int(unsafe.Sizeof(ioUringSqe128{})); got != want {
		t.Fatalf("sqeBytesFromFlags(SQE128) = %d, want %d", got, want)
	}
	if got, want := sqeStrideFromFlags(IORING_SETUP_SQE128), unsafe.Sizeof(ioUringSqe128{}); got != want {
		t.Fatalf("sqeStrideFromFlags(SQE128) = %d, want %d", got, want)
	}
	if got, want := cqeBytesFromFlags(0), int(unsafe.Sizeof(ioUringCqe{})); got != want {
		t.Fatalf("cqeBytesFromFlags(0) = %d, want %d", got, want)
	}
	if got, want := cqeBytesFromFlags(IORING_SETUP_CQE32), 2*int(unsafe.Sizeof(ioUringCqe{})); got != want {
		t.Fatalf("cqeBytesFromFlags(CQE32) = %d, want %d", got, want)
	}
	if got := cqeStrideFromFlags(IORING_SETUP_CQE32); got != 2 {
		t.Fatalf("cqeStrideFromFlags(CQE32) = %d, want 2", got)
	}

	ur := &ioUring{
		sq:  ioUringSq{ringSz: 64},
		cq:  ioUringCq{ringSz: 96},
		ops: []ioUringProbeOp{{op: IORING_OP_NOP}},
	}
	if got := ur.ringMapSz(); got != 96 {
		t.Fatalf("ringMapSz() = %d, want 96", got)
	}
	if !ur.supportsOpcode(IORING_OP_NOP) {
		t.Fatal("supportsOpcode did not find registered NOP")
	}
	if ur.supportsOpcode(IORING_OP_READ) {
		t.Fatal("supportsOpcode found unregistered READ")
	}

	var head uint32 = 1
	var tail uint32 = 4
	ur.sq.kHead = &head
	ur.sq.kTail = &tail
	if got := ur.sqCount(); got != 3 {
		t.Fatalf("sqCount() = %d, want 3", got)
	}
}

func TestSubmitSlotResetAndInlineCmdData(t *testing.T) {
	sqe := ioUringSqe{opcode: IORING_OP_NOP, fd: 7, len: 3}
	slot := submitSlot{sqe: &sqe}
	if data := slot.inlineCmdData128(); data != nil {
		t.Fatalf("inlineCmdData128() = %v, want nil for 64-byte SQE", data)
	}
	slot.reset()
	if sqe != (ioUringSqe{}) {
		t.Fatalf("reset 64-byte SQE = %+v, want zero", sqe)
	}

	sqe128 := ioUringSqe128{ioUringSqe: ioUringSqe{opcode: IORING_OP_URING_CMD}}
	slot = submitSlot{sqe: &sqe, sqe128: &sqe128}
	data := slot.inlineCmdData128()
	if len(data) != uringCmdInlineDataMax {
		t.Fatalf("inlineCmdData128 len = %d, want %d", len(data), uringCmdInlineDataMax)
	}
	data[0] = 0xaa
	data[len(data)-1] = 0xbb
	if sqe128.pad[0] == 0 || sqe128.extra[len(sqe128.extra)-1] != 0xbb {
		t.Fatal("inlineCmdData128 did not expose the inline command data region")
	}
	slot.reset()
	if sqe128 != (ioUringSqe128{}) {
		t.Fatalf("reset 128-byte SQE = %+v, want zero", sqe128)
	}
}

func newCQStrideTestRing() *ioUring {
	head := uint32(0)
	tail := uint32(2)
	mask := uint32(1)
	overflow := uint32(0)

	return &ioUring{
		cq: ioUringCq{
			kHead:     &head,
			kTail:     &tail,
			kRingMask: &mask,
			kOverflow: &overflow,
			cqes:      make([]ioUringCqe, 4),
			cqeStride: cqeStrideFromFlags(IORING_SETUP_CQE32),
		},
	}
}

func TestWaitBatchDoesNotNarrowDestinationLengthBeforeMinimum(t *testing.T) {
	t.Run("generic", func(t *testing.T) {
		ur := newCQStrideTestRing()
		*ur.cq.kTail = 1
		ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 1).Raw(), res: 11}
		cqes := mappedTestSlice[CQEView](t, oversizedLen32(t))

		n, err := ur.waitBatch(cqes)
		if err != nil || n != 1 || cqes[0].Res != 11 || *ur.cq.kHead != 1 {
			t.Fatalf("waitBatch = (n:%d err:%v res:%d head:%d), want (1 nil 11 1)", n, err, cqes[0].Res, *ur.cq.kHead)
		}
	})

	t.Run("direct", func(t *testing.T) {
		ur := newCQStrideTestRing()
		*ur.cq.kTail = 1
		ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 1).Raw(), res: 12}
		cqes := mappedTestSlice[DirectCQE](t, oversizedLen32(t))

		n, err := ur.waitBatchDirect(cqes)
		if err != nil || n != 1 || cqes[0].Res != 12 || *ur.cq.kHead != 1 {
			t.Fatalf("waitBatchDirect = (n:%d err:%v res:%d head:%d), want (1 nil 12 1)", n, err, cqes[0].Res, *ur.cq.kHead)
		}
	})

	t.Run("extended", func(t *testing.T) {
		ur := newCQStrideTestRing()
		*ur.cq.kTail = 1
		ext := &ExtSQE{}
		ur.cq.cqes[0] = ioUringCqe{userData: PackExtended(ext).Raw(), res: 13}
		cqes := mappedTestSlice[ExtCQE](t, oversizedLen32(t))

		n, err := ur.waitBatchExtended(cqes)
		if err != nil || n != 1 || cqes[0].Res != 13 || cqes[0].Ext != ext || *ur.cq.kHead != 1 {
			t.Fatalf("waitBatchExtended = (n:%d err:%v res:%d ext:%p head:%d), want (1 nil 13 %p 1)", n, err, cqes[0].Res, cqes[0].Ext, *ur.cq.kHead, ext)
		}
	})
}

func TestWaitUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 2).Raw(), res: 22}

	first, err := ur.wait()
	if err != nil {
		t.Fatalf("wait first: %v", err)
	}
	if got := first.res; got != 11 {
		t.Fatalf("first.res = %d, want 11", got)
	}

	second, err := ur.wait()
	if err != nil {
		t.Fatalf("wait second: %v", err)
	}
	if got := second.res; got != 22 {
		t.Fatalf("second.res = %d, want 22", got)
	}
	if got := SQEContextFromRaw(second.userData).FD(); got != 2 {
		t.Fatalf("second.userData FD = %d, want 2", got)
	}
}

func TestWaitSnapshotsBeforePublishingHead(t *testing.T) {
	ur := newCQStrideTestRing()
	*ur.cq.kTail = 1
	ur.cq.cqes[0] = ioUringCqe{
		userData: PackDirect(IORING_OP_NOP, 0, 0, 7).Raw(),
		res:      17,
		flags:    IORING_CQE_F_MORE,
	}

	cqe, err := ur.wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	// Model immediate kernel reuse after the userspace head publication.
	ur.cq.cqes[0] = ioUringCqe{res: 99}
	if cqe.res != 17 || cqe.flags != IORING_CQE_F_MORE || SQEContextFromRaw(cqe.userData).FD() != 7 {
		t.Fatalf("wait snapshot = {res:%d flags:%d fd:%d}, want {17 %d 7}",
			cqe.res, cqe.flags, SQEContextFromRaw(cqe.userData).FD(), IORING_CQE_F_MORE)
	}
}

func TestWaitBatchUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, 0, 0, 2).Raw(), res: 22}

	cqes := make([]CQEView, 2)
	n, err := ur.waitBatch(cqes)
	if err != nil {
		t.Fatalf("waitBatch: %v", err)
	}
	if n != 2 {
		t.Fatalf("waitBatch count = %d, want 2", n)
	}
	if got := cqes[1].Res; got != 22 {
		t.Fatalf("cqes[1].Res = %d, want 22", got)
	}
	if got := cqes[1].ctx.FD(); got != 2 {
		t.Fatalf("cqes[1].ctx.FD = %d, want 2", got)
	}
}

func TestWaitBatchDirectUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ur.cq.cqes[0] = ioUringCqe{userData: PackDirect(IORING_OP_NOP, IOSQE_IO_LINK, 3, 1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackDirect(IORING_OP_RECV, IOSQE_BUFFER_SELECT, 7, 2).Raw(), res: 22}

	cqes := make([]DirectCQE, 2)
	n, err := ur.waitBatchDirect(cqes)
	if err != nil {
		t.Fatalf("waitBatchDirect: %v", err)
	}
	if n != 2 {
		t.Fatalf("waitBatchDirect count = %d, want 2", n)
	}
	if got := cqes[1].Res; got != 22 {
		t.Fatalf("cqes[1].Res = %d, want 22", got)
	}
	if got := cqes[1].FD; got != 2 {
		t.Fatalf("cqes[1].FD = %d, want 2", got)
	}
	if got := cqes[1].BufGroup; got != 7 {
		t.Fatalf("cqes[1].BufGroup = %d, want 7", got)
	}
}

func TestWaitBatchExtendedUsesCQE32Stride(t *testing.T) {
	ur := newCQStrideTestRing()
	ext1 := &ExtSQE{}
	ext2 := &ExtSQE{}
	ur.cq.cqes[0] = ioUringCqe{userData: PackExtended(ext1).Raw(), res: 11}
	ur.cq.cqes[2] = ioUringCqe{userData: PackExtended(ext2).Raw(), res: 22}

	cqes := make([]ExtCQE, 2)
	n, err := ur.waitBatchExtended(cqes)
	if err != nil {
		t.Fatalf("waitBatchExtended: %v", err)
	}
	if n != 2 {
		t.Fatalf("waitBatchExtended count = %d, want 2", n)
	}
	if got := cqes[1].Res; got != 22 {
		t.Fatalf("cqes[1].Res = %d, want 22", got)
	}
	if cqes[1].Ext != ext2 {
		t.Fatalf("cqes[1].Ext = %p, want %p", cqes[1].Ext, ext2)
	}
}

func TestRegisterBuffersAcceptsNonPowerOfTwoCount(t *testing.T) {
	ur := newTestIoUring(t)
	const (
		bufferCount = 3
		bufferSize  = 1 << 12
	)
	buffers := make([]byte, bufferCount*bufferSize)
	if err := ur.registerBuffers(unsafe.Pointer(unsafe.SliceData(buffers)), bufferCount, bufferSize); err != nil {
		t.Fatalf("registerBuffers: %v", err)
	}
	if err := ur.unregisterBuffers(); err != nil {
		t.Fatalf("unregisterBuffers: %v", err)
	}
	runtime.KeepAlive(buffers)
}

func TestNewIoUringRejectsEntriesOutsideSetupDomain(t *testing.T) {
	for _, entries := range []int{-1, 0, EntriesHuge + 1} {
		ur, err := newIoUring(entries)
		if ur != nil {
			t.Fatalf("newIoUring(%d) = non-nil ring, want nil", entries)
		}
		if !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("newIoUring(%d) error = %v, want %v", entries, err, ErrInvalidParam)
		}
	}
}
