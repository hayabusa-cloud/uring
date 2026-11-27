// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

func TestCQPendingTracksPartialDrain(t *testing.T) {
	for _, mode := range []struct {
		name   string
		shared bool
	}{{"single", false}, {"shared", true}} {
		t.Run(mode.name, func(t *testing.T) {
			if !mode.shared {
				runtime.LockOSThread()
				t.Cleanup(runtime.UnlockOSThread)
			}
			ring := newTestRing(t, func(opt *uring.Options) {
				opt.MultiIssuers = mode.shared
			})
			if got := ring.CQPending(); got != 0 {
				t.Fatalf("initial CQPending = %d, want 0", got)
			}

			const count = 3
			for range count {
				if err := ring.Nop(uring.PackDirect(uring.IORING_OP_NOP, 0, 0, 0)); err != nil {
					t.Fatal(err)
				}
			}
			checkWaitWithoutReaping(t, ring, ring.Wait, count)

			var cqes [1]uring.CQEView
			for remaining := count - 1; remaining >= 0; remaining-- {
				if n, err := ring.Wait(cqes[:]); err != nil || n != 1 {
					t.Fatalf("Wait = (%d, %v), want (1, nil)", n, err)
				}
				if cqes[0].Op() != uring.IORING_OP_NOP || cqes[0].Res != 0 {
					t.Fatalf("completion = %+v, want successful NOP", cqes[0])
				}
				if got := ring.CQPending(); got != remaining {
					t.Fatalf("CQPending = %d, want %d", got, remaining)
				}
			}
		})
	}
}

func TestEmptyWaitAfterStop(t *testing.T) {
	ring := newTestRing(t)
	if err := ring.Stop(); err != nil {
		t.Fatal(err)
	}
	if n, err := ring.Wait(nil); n != 0 || !errors.Is(err, uring.ErrClosed) {
		t.Errorf("Wait(nil) = (%d, %v), want (0, ErrClosed)", n, err)
	}
	if n, err := ring.WaitDirect(nil); n != 0 || !errors.Is(err, uring.ErrClosed) {
		t.Errorf("WaitDirect(nil) = (%d, %v), want (0, ErrClosed)", n, err)
	}
	if n, err := ring.WaitExtended(nil); n != 0 || !errors.Is(err, uring.ErrClosed) {
		t.Errorf("WaitExtended(nil) = (%d, %v), want (0, ErrClosed)", n, err)
	}
}

// Drive the submitted requests using only the wait method under test.
func checkWaitWithoutReaping[T any](t *testing.T, ring *uring.Uring, wait func([]T) (int, error), want int) {
	t.Helper()
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		if n, err := wait(nil); n != 0 || err != nil {
			t.Fatalf("wait(nil) = (%d, %v), want (0, nil)", n, err)
		}
		got := ring.CQPending()
		if got == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CQPending = %d, want %d after empty waits", got, want)
		}
		backoff.Wait()
	}

	var slot [1]T
	for _, dst := range [][]T{nil, slot[:0]} {
		if n, err := wait(dst); n != 0 || err != nil {
			t.Fatalf("empty wait with cap %d = (%d, %v), want (0, nil)", cap(dst), n, err)
		}
		if got := ring.CQPending(); got != want {
			t.Fatalf("CQPending after empty wait with cap %d = %d, want %d", cap(dst), got, want)
		}
	}
}
