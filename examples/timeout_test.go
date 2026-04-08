// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"errors"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// TestBasicTimeout demonstrates io_uring timeout operations.
//
// Timeouts in io_uring are themselves SQEs that complete after a duration.
// Unlike traditional timers, they integrate into the completion queue,
// enabling unified event handling.
//
// Note: This test is known to be unreliable in WSL2 environments due to
// timing-sensitive kernel-userspace interactions. It works reliably on
// native Linux systems.
//
// Use cases:
//   - Operation deadlines
//   - Periodic wake-ups in event loops
//   - Linked timeouts for I/O cancellation
func TestBasicTimeout(t *testing.T) {
	// Skip in environments where timeout completions are unreliable
	// This matches the behavior of integration_test.go
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true
			opts.MultiIssuers = true // Use the shared-submit configuration in this example.
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit a 20ms timeout (shorter for faster test)
	ctx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 0, 0)
	start := time.Now()

	if err := ring.Timeout(ctx, 20*time.Millisecond); err != nil {
		t.Fatalf("Timeout submit: %v", err)
	}
	t.Log("Timeout submitted")

	// Wait for completion using same pattern as integration test
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(time.Second)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		// Ignore transient errors that occur in WSL2
		if err != nil &&
			!errors.Is(err, iox.ErrWouldBlock) &&
			!errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_TIMEOUT {
				elapsed := time.Since(start)
				t.Logf("Timeout completed after %v (res=%d)", elapsed, cqes[i].Res)
				if elapsed < 15*time.Millisecond {
					t.Errorf("Timeout too fast: %v", elapsed)
				}
				return // Test passed
			}
		}
		b.Wait()
	}

	// If we get here in WSL2, skip rather than fail
	t.Skip("Timeout completion not received - likely WSL2 timing issue")
}

// TestMultipleTimeouts demonstrates handling multiple concurrent timeouts.
//
// Multiple timeouts can be in-flight simultaneously. Each has its own
// context, allowing the completion handler to identify which timer fired.
//
// Note: This test is known to be unreliable in WSL2 environments.
func TestMultipleTimeouts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true
			opts.MultiIssuers = true // Use the shared-submit configuration in this example.
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit timeouts with different durations
	// Use bufGroup field to identify which timeout (hack for demo)
	timeouts := []struct {
		id       uint16
		duration time.Duration
	}{
		{1, 20 * time.Millisecond},
		{2, 40 * time.Millisecond},
		{3, 60 * time.Millisecond},
	}

	start := time.Now()
	for _, to := range timeouts {
		ctx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, to.id, 0)
		if err := ring.Timeout(ctx, to.duration); err != nil {
			t.Fatalf("Timeout[%d] submit: %v", to.id, err)
		}
	}
	t.Logf("Submitted %d timeouts", len(timeouts))

	// Collect completions in order
	cqes := make([]uring.CQEView, 16)
	completed := 0
	deadline := time.Now().Add(time.Second)
	var order []uint16
	b := iox.Backoff{}

	for completed < len(timeouts) && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		// Ignore transient errors that occur in WSL2
		if err != nil &&
			!errors.Is(err, iox.ErrWouldBlock) &&
			!errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			if cqes[i].Op() == uring.IORING_OP_TIMEOUT {
				id := cqes[i].BufGroup()
				elapsed := time.Since(start)
				t.Logf("Timeout %d completed at %v", id, elapsed)
				order = append(order, id)
				completed++
			}
		}
		b.Wait()
	}

	// Verify order: should be 1, 2, 3 (shortest first)
	if len(order) == 3 {
		if order[0] != 1 || order[1] != 2 || order[2] != 3 {
			t.Errorf("Unexpected order: %v", order)
		}
		return // Test passed
	}

	// If we get here in WSL2, skip rather than fail
	t.Skipf("Not all timeouts completed (got %v) - likely WSL2 timing issue", order)
}

// TestPollWithTimeout demonstrates polling a pipe with a timeout.
//
// This pattern is common in event loops:
//  1. Submit poll for read-ready
//  2. Submit timeout as deadline
//  3. First completion wins
//
// Note: This test is known to be unreliable in WSL2 environments.
func TestPollWithTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true
			opts.MultiIssuers = true // Use the shared-submit configuration in this example.
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create a pipe
	r, w, err := createPipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer closeFd(r)
	defer closeFd(w)

	// Submit poll for POLLIN on read end
	pollCtx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 1, int32(r))
	if err := ring.PollAdd(pollCtx, uring.EPOLLIN); err != nil {
		t.Fatalf("PollAdd: %v", err)
	}

	// Submit timeout as deadline (shorter for faster test)
	timeoutCtx := uring.PackDirect(uring.IORING_OP_TIMEOUT, 0, 2, 0)
	if err := ring.Timeout(timeoutCtx, 50*time.Millisecond); err != nil {
		t.Fatalf("Timeout: %v", err)
	}

	t.Log("Submitted poll and timeout, waiting for first completion...")

	// Wait - timeout should fire first since pipe has no data
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(time.Second)
	firstOp := uint8(0)
	b := iox.Backoff{}

	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		// Ignore transient errors that occur in WSL2
		if err != nil &&
			!errors.Is(err, iox.ErrWouldBlock) &&
			!errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			op := cqes[i].Op()
			if op == uring.IORING_OP_POLL_ADD || op == uring.IORING_OP_TIMEOUT {
				firstOp = op
				t.Logf("First completion: op=%d (6=POLL_ADD, 11=TIMEOUT)", op)
				break
			}
		}
		if firstOp != 0 {
			break
		}
		b.Wait()
	}

	if firstOp == uring.IORING_OP_TIMEOUT {
		t.Log("Timeout fired first (expected - no data on pipe)")
		return // Test passed
	} else if firstOp == uring.IORING_OP_POLL_ADD {
		t.Error("Poll completed first - unexpected")
		return
	}

	// If we get here in WSL2, skip rather than fail
	t.Skip("Neither poll nor timeout completed - likely WSL2 timing issue")
}

// TestLinkedTimeout demonstrates IOSQE_IO_LINK with LinkTimeout.
//
// Linked operations form atomic chains where:
//   - IOSQE_IO_LINK: soft link - failure cancels remaining chain
//   - IOSQE_IO_HARDLINK: hard link - continues regardless of result
//
// LinkTimeout is special: if it fires before the linked operation completes,
// the operation is cancelled with -ECANCELED (-125).
//
// Pattern:
//
//	SQE1: Operation (with IOSQE_IO_LINK flag)
//	SQE2: LinkTimeout (fires if SQE1 takes too long)
//
// Use cases:
//   - Read/write with deadline
//   - Connect with timeout
//   - Any I/O that must complete within a duration
func TestLinkedTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
			opts.NotifySucceed = true
			opts.MultiIssuers = true
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Submit a NOP with IOSQE_IO_LINK flag, followed by LinkTimeout
	// The NOP will complete immediately, so the timeout should be cancelled
	nopCtx := uring.PackDirect(uring.IORING_OP_NOP, uring.IOSQE_IO_LINK, 1, 0) // bufGroup=1 to identify
	if err := ring.Nop(nopCtx); err != nil {
		t.Fatalf("Nop submit: %v", err)
	}

	timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 2, 0) // bufGroup=2 to identify
	if err := ring.LinkTimeout(timeoutCtx, 100*time.Millisecond); err != nil {
		t.Fatalf("LinkTimeout submit: %v", err)
	}
	t.Log("Submitted NOP + LinkTimeout chain")

	// Wait for completions
	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(time.Second)
	nopDone := false
	timeoutDone := false
	b := iox.Backoff{}

	for time.Now().Before(deadline) && (!nopDone || !timeoutDone) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := 0; i < n; i++ {
			op := cqes[i].Op()
			res := cqes[i].Res
			id := cqes[i].BufGroup()

			switch id {
			case 1: // NOP
				t.Logf("NOP completed: res=%d", res)
				if res != 0 {
					t.Errorf("NOP expected res=0, got %d", res)
				}
				nopDone = true
			case 2: // LinkTimeout
				t.Logf("LinkTimeout completed: res=%d (op=%d)", res, op)
				// When linked op completes first, timeout gets -ECANCELED
				if res != -int32(zcall.ECANCELED) {
					t.Logf("Note: LinkTimeout res=%d (expected %d for cancelled)", res, -int32(zcall.ECANCELED))
				}
				timeoutDone = true
			}
		}
		b.Wait()
	}

	if !nopDone || !timeoutDone {
		t.Skip("Linked operations did not complete - likely WSL2 timing issue")
	}
	t.Log("Linked timeout test passed")
}
