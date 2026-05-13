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
	"net"
	"os"
	"path/filepath"
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

	t.Skipf("polling direct-I/O file test requires a writable block-backed filesystem; no usable path found in current working directory or /var/tmp; probe errors: %v", probeErr)
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
				t.Errorf("write file: %v", errFromErrno(uintptr(-cqe.res)))
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
		if cqe == nil || err == iox.ErrWouldBlock {
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

func TestReadMultiShotEncodesSQE(t *testing.T) {
	ring := newWrapperTestRing(t)

	ctx := PackDirect(0, 0, 0, 15)
	if err := ring.ioUring.readMultiShot(ctx, 0, 4096, 7); err != nil {
		t.Fatalf("readMultiShot: %v", err)
	}

	sqe := lastSubmittedSQE(t, ring)
	if sqe.opcode != IORING_OP_READ_MULTISHOT {
		t.Fatalf("opcode = %d, want %d", sqe.opcode, IORING_OP_READ_MULTISHOT)
	}
	if sqe.fd != 15 {
		t.Fatalf("fd = %d, want 15", sqe.fd)
	}
	if sqe.len != 4096 {
		t.Fatalf("len = %d, want 4096", sqe.len)
	}
	if sqe.flags&IOSQE_BUFFER_SELECT == 0 {
		t.Fatal("IOSQE_BUFFER_SELECT not set")
	}
	if sqe.bufIndex != 7 {
		t.Fatalf("bufIndex = %d, want 7 (group)", sqe.bufIndex)
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

func TestCqAdvanceZeroIsNoop(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	headBefore := atomic.LoadUint32(ring.ioUring.cq.kHead)
	ring.ioUring.cqAdvance(0)
	headAfter := atomic.LoadUint32(ring.ioUring.cq.kHead)
	if headAfter != headBefore {
		t.Fatalf("cqAdvance(0) changed head: %d -> %d", headBefore, headAfter)
	}
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

func TestBufRingCQAdvanceZero(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	headBefore := atomic.LoadUint32(ring.ioUring.cq.kHead)
	br := &ioUringBufRing{}
	ring.ioUring.bufRingCQAdvance(br, 0)
	headAfter := atomic.LoadUint32(ring.ioUring.cq.kHead)
	if headAfter != headBefore {
		t.Fatalf("bufRingCQAdvance(0) changed CQ head: %d -> %d", headBefore, headAfter)
	}
	if br.tail != 0 {
		t.Fatalf("bufRingCQAdvance(0) changed tail: %d", br.tail)
	}
}

func TestCqRingAdvanceZero(t *testing.T) {
	ring := newStartedSharedTestRing(t)

	headBefore := atomic.LoadUint32(ring.ioUring.cq.kHead)
	ring.ioUring.cq.advance(0)
	headAfter := atomic.LoadUint32(ring.ioUring.cq.kHead)
	if headAfter != headBefore {
		t.Fatalf("cq.advance(0) changed head: %d -> %d", headBefore, headAfter)
	}
}

func TestCQReady(t *testing.T) {
	var head uint32
	var tail uint32
	ur := &ioUring{params: &ioUringParams{}, cq: ioUringCq{kHead: &head, kTail: &tail}}

	if ur.cqReady() {
		t.Fatal("empty CQ reported ready")
	}

	tail = 1
	if !ur.cqReady() {
		t.Fatal("visible CQE was not reported ready")
	}

	head = 1
	if ur.cqReady() {
		t.Fatal("reaped CQ reported ready")
	}
}

func TestObserveCQEmptyLockedNonIOPOLL(t *testing.T) {
	var head uint32
	var tail uint32
	ur := &ioUring{params: &ioUringParams{}, cq: ioUringCq{kHead: &head, kTail: &tail}}

	if err := ur.observeCQEmptyLocked(); !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("observeCQEmptyLocked() = %v, want %v", err, iox.ErrWouldBlock)
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
	if len(data) != uringCmd128DataMax {
		t.Fatalf("inlineCmdData128 len = %d, want %d", len(data), uringCmd128DataMax)
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
