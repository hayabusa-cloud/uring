// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
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
			var backoff iox.Backoff
			deadline := time.Now().Add(3 * time.Second)
			for {
				// Flush submissions and deferred task work without consuming CQEs.
				if n, err := ring.Wait(nil); err != nil || n != 0 {
					t.Fatalf("Wait(nil) = (%d, %v), want (0, nil)", n, err)
				}
				if ring.CQPending() == count {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("CQPending = %d, want %d", ring.CQPending(), count)
				}
				backoff.Wait()
			}

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
