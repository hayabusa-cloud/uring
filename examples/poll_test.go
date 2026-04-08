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

// TestPollAddBasic demonstrates basic poll operation with io_uring.
//
// Architecture:
//   - PollAdd (IORING_OP_POLL_ADD) monitors fd for events
//   - CQE fires when event occurs or poll is cancelled
//   - Single-shot by default; use IORING_POLL_ADD_MULTI for persistent
//
// Use case: Waiting for fd readability/writability without blocking.
func TestPollAddBasic(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pipe for testing
	readFD, writeFD, err := createPipe()
	if err != nil {
		t.Fatalf("createPipe: %v", err)
	}
	defer closeFd(readFD)
	defer closeFd(writeFD)

	t.Logf("Created pipe: read=%d, write=%d", readFD, writeFD)

	// Submit poll for POLLIN (read ready) on read end
	ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(readFD))
	if err := ring.PollAdd(ctx, uring.EPOLLIN); err != nil {
		t.Fatalf("PollAdd: %v", err)
	}
	t.Log("Submitted PollAdd for EPOLLIN")

	// Write data to trigger poll
	go func() {
		time.Sleep(50 * time.Millisecond)
		testData := []byte("trigger poll")
		zcall.Write(writeFD, testData)
		t.Log("Wrote data to trigger poll")
	}()

	// Wait for poll completion
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	var result int32
	for time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			result = cqes[0].Res
			t.Logf("Poll CQE: res=%d, flags=0x%x", result, cqes[0].Flags)
			break
		}
		b.Wait()
	}

	if result < 0 {
		t.Logf("Poll failed: %d (may fail in WSL2)", result)
		t.Skip("Poll not available or failed")
	}

	// result contains the triggered events
	if result&uring.EPOLLIN != 0 {
		t.Log("EPOLLIN triggered - data is ready to read")
	}
}

// TestPollCancelByFD demonstrates cancelling a poll operation by file descriptor.
//
// Architecture:
//   - AsyncCancelFD (IORING_OP_ASYNC_CANCEL with IORING_ASYNC_CANCEL_FD)
//     cancels an active operation matching the given fd
//   - Cancelled poll receives CQE with -ECANCELED
//   - AsyncCancel itself receives CQE with success (0)
//
// Note: PollRemove (IORING_OP_POLL_REMOVE) matches by user_data in the kernel
// addr field. With packed direct contexts, AsyncCancelFD is the correct
// mechanism for fd-based cancellation.
func TestPollCancelByFD(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesSmall
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create pipe
	readFD, writeFD, err := createPipe()
	if err != nil {
		t.Fatalf("createPipe: %v", err)
	}
	defer closeFd(readFD)
	defer closeFd(writeFD)

	// Submit poll (will wait forever since we don't write)
	pollCtx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, 0, int32(readFD))
	if err := ring.PollAdd(pollCtx, uring.EPOLLIN); err != nil {
		t.Fatalf("PollAdd: %v", err)
	}
	t.Log("Submitted PollAdd (will be cancelled)")

	// Give poll time to register with the kernel
	time.Sleep(10 * time.Millisecond)

	// Cancel by file descriptor
	cancelCtx := uring.PackDirect(uring.IORING_OP_ASYNC_CANCEL, 0, 0, int32(readFD))
	if err := ring.AsyncCancelFD(cancelCtx, false); err != nil {
		t.Fatalf("AsyncCancelFD: %v", err)
	}
	t.Log("Submitted AsyncCancelFD")

	// Wait for completions (expect 2: cancelled poll + cancel success)
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	completions := 0
	var sawCancel, sawCancelled bool

	for completions < 2 && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) && !errors.Is(err, uring.ErrExists) {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			op := cqes[i].Op()
			res := cqes[i].Res
			t.Logf("CQE %d: op=%d, res=%d", completions+1, op, res)
			if op == uring.IORING_OP_ASYNC_CANCEL && res >= 0 {
				sawCancel = true
			}
			if res == -int32(uring.ECANCELED) {
				sawCancelled = true
			}
			completions++
		}
		if completions < 2 {
			b.Wait()
		}
	}

	if completions < 2 {
		t.Errorf("expected 2 completions, got %d", completions)
	}
	if !sawCancel {
		t.Error("expected successful cancel CQE")
	}
	if !sawCancelled {
		t.Error("expected cancelled poll CQE with -ECANCELED")
	}
	t.Logf("Received %d completions (cancel=%v, cancelled=%v)", completions, sawCancel, sawCancelled)
}

// TestPollMultipleFDs demonstrates polling multiple file descriptors.
//
// Architecture:
//   - Submit multiple PollAdd operations
//   - Each poll is independent
//   - CQEs arrive as events trigger
//
// Use case: Event-driven server monitoring many connections.
func TestPollMultipleFDs(t *testing.T) {
	ring, err := uring.New(
		testMinimalBufferOptions,
		func(opts *uring.Options) {
			opts.Entries = uring.EntriesMedium
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Create 3 pipes
	const numPipes = 3
	var readFDs, writeFDs [numPipes]uintptr

	for i := range numPipes {
		r, w, err := createPipe()
		if err != nil {
			t.Fatalf("createPipe[%d]: %v", i, err)
		}
		readFDs[i] = r
		writeFDs[i] = w
		defer closeFd(r)
		defer closeFd(w)
	}

	t.Logf("Created %d pipes", numPipes)

	// Submit poll for each read end
	for i := range numPipes {
		ctx := uring.PackDirect(uring.IORING_OP_POLL_ADD, 0, uint16(i+1), int32(readFDs[i]))
		if err := ring.PollAdd(ctx, uring.EPOLLIN); err != nil {
			t.Fatalf("PollAdd[%d]: %v", i, err)
		}
	}
	t.Log("Submitted polls for all pipes")

	// Trigger polls in reverse order
	go func() {
		for i := numPipes - 1; i >= 0; i-- {
			time.Sleep(30 * time.Millisecond)
			msg := []byte{byte('A' + i)}
			zcall.Write(writeFDs[i], msg)
			t.Logf("Wrote to pipe %d", i)
		}
	}()

	// Collect completions
	cqes := make([]uring.CQEView, 8)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}
	triggered := make([]bool, numPipes)
	count := 0

	for count < numPipes && time.Now().Before(deadline) {
		n, err := ring.Wait(cqes)
		if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("Wait: %v", err)
		}
		for i := range n {
			if cqes[i].Res >= 0 {
				// userData (1,2,3) maps to pipe index (0,1,2)
				// Note: actual userData extraction depends on context mode
				count++
				t.Logf("Poll completed: res=0x%x", cqes[i].Res)
			}
		}
		if count < numPipes {
			b.Wait()
		}
	}

	_ = triggered // Would track which pipe triggered
	t.Logf("Received %d poll completions", count)
}

// TestPollEventTypes shows the different poll events available.
func TestPollEventTypes(t *testing.T) {
	// Epoll event values not exported by the uring package.
	const (
		EPOLLRDHUP = 0x2000
		EPOLLHUP   = 0x010
		EPOLLERR   = 0x008
		EPOLLPRI   = 0x002
	)

	t.Log("Poll Event Types:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Logf("  EPOLLIN      (0x%03x): Data available to read", uring.EPOLLIN)
	t.Logf("  EPOLLOUT     (0x%03x): Ready for writing", uring.EPOLLOUT)
	t.Logf("  EPOLLRDHUP   (0x%04x): Peer closed connection", EPOLLRDHUP)
	t.Logf("  EPOLLHUP     (0x%03x): Hang up (always monitored)", EPOLLHUP)
	t.Logf("  EPOLLERR     (0x%03x): Error condition (always monitored)", EPOLLERR)
	t.Logf("  EPOLLPRI     (0x%03x): Urgent data (OOB)", EPOLLPRI)
	t.Log("")
	t.Log("Common Combinations:")
	t.Log("  Read-ready:    EPOLLIN")
	t.Log("  Write-ready:   EPOLLOUT")
	t.Log("  Connection:    EPOLLIN | EPOLLRDHUP")
	t.Log("  Full monitor:  EPOLLIN | EPOLLOUT | EPOLLRDHUP")
	t.Log("-----------------------------------------------------")
}

// TestPollVsMultishotAccept compares poll with multishot accept.
func TestPollVsMultishotAccept(t *testing.T) {
	t.Log("Poll vs Multishot Accept:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Traditional Poll + Accept:")
	t.Log("  1. PollAdd(listen_fd, EPOLLIN)")
	t.Log("  2. Wait for CQE (connection ready)")
	t.Log("  3. Accept(listen_fd)")
	t.Log("  4. Handle connection")
	t.Log("  5. Goto 1 (re-poll)")
	t.Log("  → 2 operations per connection")
	t.Log("")
	t.Log("Multishot Accept:")
	t.Log("  1. AcceptMultishot(listen_fd)")
	t.Log("  2. CQE fires for each connection")
	t.Log("  3. Handle connection")
	t.Log("  4. Automatic re-arm (no re-submit)")
	t.Log("  → 1 operation for all connections")
	t.Log("")
	t.Log("Recommendation:")
	t.Log("  - High connection rate: Multishot Accept")
	t.Log("  - Mixed fd types: Poll + operation")
	t.Log("  - Legacy integration: Poll pattern")
	t.Log("-----------------------------------------------------")
}

// TestPollIntegrationPattern shows poll in a server loop.
func TestPollIntegrationPattern(t *testing.T) {
	t.Log("Poll Integration Pattern:")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("Event Loop Structure:")
	t.Log("  for {")
	t.Log("      // Submit polls for new fds")
	t.Log("      for fd := range newFDs {")
	t.Log("          ring.PollAdd(ctx, EPOLLIN)")
	t.Log("      }")
	t.Log("")
	t.Log("      // Wait for events")
	t.Log("      n, _ := ring.Wait(cqes)")
	t.Log("")
	t.Log("      // Handle completions")
	t.Log("      for i := range n {")
	t.Log("          fd := fdFromContext(cqes[i])")
	t.Log("          events := cqes[i].Res")
	t.Log("          ")
	t.Log("          if events & EPOLLIN {")
	t.Log("              ring.Recv(fd, buf)")
	t.Log("          }")
	t.Log("          if events & EPOLLRDHUP {")
	t.Log("              closeConnection(fd)")
	t.Log("          }")
	t.Log("      }")
	t.Log("  }")
	t.Log("-----------------------------------------------------")
}
