// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"bytes"
	"io"
	"os"
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

func TestIOUring_BasicUsage(t *testing.T) {
	fr := func(t *testing.T, ur *ioUring) {
		defer os.Remove("test_f_direct.txt")
		f, err := os.OpenFile("test_f_direct.txt", os.O_RDWR|os.O_CREATE|zcall.O_DIRECT, 0660)
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

	fw := func(t *testing.T, ur *ioUring) {
		defer os.Remove("test_f_direct.txt")
		f, err := os.OpenFile("test_f_direct.txt", os.O_RDWR|os.O_CREATE|zcall.O_DIRECT, 0660)
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
		fr(t, ur)
	})

	t.Run("normal mode write file", func(t *testing.T) {
		ur := newTestIoUring(t)
		fw(t, ur)
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
		fw(t, ur)
	})

	t.Run("sq poll mode read file", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringSqPollOptions)
		fr(t, ur)
	})

	t.Run("sq poll mode write file", func(t *testing.T) {
		ur := newTestIoUring(t, ioUringSqPollOptions)
		fw(t, ur)
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
		fw(t, ur)
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
