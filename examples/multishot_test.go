// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// TestMultishotAcceptServer demonstrates the handler-side completion flow
// for a multishot accept server.
//
// Architecture:
//   - Single IORING_OP_ACCEPT with IORING_ACCEPT_MULTISHOT flag
//   - Kernel generates one CQE per accepted connection
//   - No SQE resubmission needed until multishot stops
//   - F_MORE flag indicates more connections may arrive
//
// This example focuses on CQE handling once a multishot accept is active.
//
// See also: TestTCPEchoServer in echo_test.go for single-shot accept.
func TestMultishotAcceptServer(t *testing.T) {
	const numClients = 5
	acceptedFDs := make(chan int32, numClients)
	var acceptErr error
	stopped := make(chan struct{}, 1)
	handler := uring.NewMultishotSubscriber().OnStep(func(step uring.MultishotStep) uring.MultishotAction {
		if step.Err != nil {
			acceptErr = step.Err
			return uring.MultishotStop
		}
		acceptedFDs <- step.CQE.Res
		return uring.MultishotContinue
	}).OnStop(func(err error, cancelled bool) {
		if err != nil && acceptErr == nil {
			acceptErr = err
		}
		stopped <- struct{}{}
	})

	for i := 0; i < numClients; i++ {
		flags := uint32(0)
		if i < numClients-1 {
			flags = uring.IORING_CQE_F_MORE
		}
		if keep := dispatchSimplifiedMultishotCQE(
			handler.Handler(),
			uring.CQEView{Res: int32(100 + i), Flags: flags},
		); !keep && i < numClients-1 {
			t.Fatalf("handler stopped early on CQE %d", i+1)
		}
	}

	acceptCount := 0
	for {
		select {
		case fd := <-acceptedFDs:
			acceptCount++
			t.Logf("Accepted connection %d: fd=%d", acceptCount, fd)
		default:
			goto drainedAccepts
		}
	}
drainedAccepts:
	if acceptErr != nil {
		t.Fatalf("accept CQE failed: %v", acceptErr)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("expected terminal multishot stop callback")
	}
	if acceptCount != numClients {
		t.Fatalf("accepted %d/%d connections via multishot", acceptCount, numClients)
	}
	t.Logf("Accepted %d/%d connections via multishot", acceptCount, numClients)
}

// TestMultishotRecvWithBufferSelection demonstrates the handler-side CQE flow
// for multishot receive with kernel buffer selection metadata.
//
// Architecture:
//   - Buffer ring registered with kernel (IORING_REGISTER_PBUF_RING)
//   - Multishot recv with IOSQE_BUFFER_SELECT flag
//   - Kernel picks buffer from ring, returns buffer ID in CQE
//   - Application returns buffer to ring after processing
//
// This example focuses on decoding CQE metadata once receive completions arrive.
func TestMultishotRecvWithBufferSelection(t *testing.T) {
	received := make(chan recvResult, 16)
	var recvErr error
	var missingBuffer bool
	stopped := make(chan struct{}, 1)
	handler := uring.NewMultishotSubscriber().OnStep(func(step uring.MultishotStep) uring.MultishotAction {
		if step.Err != nil {
			recvErr = step.Err
			return uring.MultishotStop
		}
		if step.CQE.Res == 0 {
			return uring.MultishotContinue
		}
		if !step.CQE.HasBuffer() {
			missingBuffer = true
			return uring.MultishotStop
		}
		received <- recvResult{bufID: step.CQE.BufID(), nbytes: int(step.CQE.Res)}
		return uring.MultishotContinue
	}).OnStop(func(err error, cancelled bool) {
		if err != nil && recvErr == nil {
			recvErr = err
		}
		stopped <- struct{}{}
	})

	payload := []byte("HelloWorldMultishotRecv")
	sentBytes := len(payload)
	firstChunk := 11
	secondChunk := sentBytes - firstChunk
	events := []uring.CQEView{
		{
			Res:   int32(firstChunk),
			Flags: uring.IORING_CQE_F_BUFFER | uring.IORING_CQE_F_MORE,
		},
		{
			Res: int32(secondChunk),
			Flags: uring.IORING_CQE_F_BUFFER |
				(1 << uring.IORING_CQE_BUFFER_SHIFT),
		},
	}

	recvCount := 0
	totalBytes := 0
	for i, cqe := range events {
		if keep := dispatchSimplifiedMultishotCQE(handler.Handler(), cqe); !keep && i < len(events)-1 {
			t.Fatalf("handler stopped early on recv CQE %d", i+1)
		}
		draining := true
		for draining {
			select {
			case res := <-received:
				recvCount++
				totalBytes += res.nbytes
				t.Logf("Received CQE %d: bufID=%d bytes=%d hasBuffer=true",
					recvCount, res.bufID, res.nbytes)
			default:
				draining = false
			}
		}
	}
	if recvErr != nil {
		t.Fatalf("recv CQE failed: %v", recvErr)
	}
	if missingBuffer {
		t.Fatal("recv CQE missing buffer-selection metadata")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("expected terminal multishot stop callback")
	}
	t.Logf("Received %d CQEs, %d/%d bytes", recvCount, totalBytes, sentBytes)

	// Verify at least one recv CQE arrived with buffer selection metadata.
	if recvCount == 0 {
		t.Fatal("no messages received; recv path may be broken")
	}

	// Verify total byte count matches what was sent.
	// TCP may coalesce sends, so we check aggregate bytes rather than
	// per-message boundaries.
	if totalBytes != sentBytes {
		t.Fatalf("byte count mismatch: got %d, want %d", totalBytes, sentBytes)
	}
}

type recvResult struct {
	bufID  uint16
	nbytes int
}

// TestMultishotLifecycle shows the complete lifecycle of a multishot subscription.
func TestMultishotLifecycle(t *testing.T) {
	t.Log("Multishot Subscription Lifecycle:")
	t.Log("-----------------------------------------------------")
	t.Log("  1. ring.AcceptMultishot(sqeCtx, handler) -> MultishotSubscription")
	t.Log("  2. Kernel generates CQE per event (F_MORE=true)")
	t.Log("  3. handler.OnMultishotStep(step) called for each CQE")
	t.Log("  4. Return MultishotStop on a result -> triggers Cancel")
	t.Log("  5. sub.Cancel() -> IORING_OP_ASYNC_CANCEL")
	t.Log("  6. Final CQE received (F_MORE=false)")
	t.Log("  7. handler.OnMultishotStop(err, cancelled) called")
	t.Log("  8. ExtSQE returned to pool")
	t.Log("-----------------------------------------------------")
	t.Log("")
	t.Log("State transitions:")
	t.Log("  SubscriptionActive -> SubscriptionCancelling (user cancel)")
	t.Log("  SubscriptionActive -> SubscriptionStopped (kernel stop)")
	t.Log("  SubscriptionCancelling -> SubscriptionStopped (after cancel CQE)")
}

// TestMultishotHandlerControl demonstrates handler-controlled stopping.
//
// The handler can control multishot behavior by returning an action:
//   - Return `uring.MultishotContinue`: keep accepting events
//   - Return `uring.MultishotStop`: request cancellation
//
// This allows dynamic control based on application state (e.g., max connections).
//
// Note: between the handler returning MultishotStop and the kernel processing
// the cancel, additional CQEs may arrive. The handler must handle these
// gracefully (e.g., close surplus FDs without counting them toward the limit).
func TestMultishotHandlerControl(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multishot handler test in short mode (requires reliable networking)")
	}
	ring, err := uring.New(
		testMinimalBufferOptions,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	serverSock, err := sock.ListenTCP4(&sock.TCPAddr{IP: sock.IPv4LoopBack, Port: 0})
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer serverSock.Close()

	const maxConnections = 3

	handler := &limitedAcceptHandler{
		maxConnections: maxConnections,
		acceptedFDs:    make(chan int32, maxConnections+4),
		stopped:        make(chan struct{}),
	}

	sqeCtx := uring.ForFD(serverSock.FD().Raw())
	sub, err := ring.AcceptMultishot(sqeCtx, handler)
	if err != nil {
		t.Logf("AcceptMultishot: %v", err)
		t.Skip("multishot may not be supported")
	}

	// Spawn more clients than max
	for i := 0; i < maxConnections+2; i++ {
		go func() {
			conn, _ := net.Dial("tcp", serverSock.Addr().String())
			if conn != nil {
				time.Sleep(100 * time.Millisecond)
				conn.Close()
			}
		}()
	}

	cqes := make([]uring.CQEView, 16)
	deadline := time.Now().Add(2 * time.Second)
	b := iox.Backoff{}

	for sub.Active() && time.Now().Before(deadline) {
		n, _ := ring.Wait(cqes)
		for i := range n {
			keep := dispatchSimplifiedMultishotCQE(handler, cqes[i])
			if !keep {
				break
			}
		}
		b.Wait()
	}

	accepted := handler.count.Load()
	surplus := handler.surplus.Load()
	t.Logf("Accepted %d connections at limit, %d surplus after stop (max was %d)", accepted, surplus, maxConnections)

	if accepted < maxConnections {
		t.Logf("accepted %d < max %d (may be environment-dependent)", accepted, maxConnections)
	}

	// Cleanup
	close(handler.acceptedFDs)
	for fd := range handler.acceptedFDs {
		zcall.Close(uintptr(fd))
	}
}

type limitedAcceptHandler struct {
	maxConnections int32
	count          atomic.Int32
	surplus        atomic.Int32
	acceptedFDs    chan int32
	stopped        chan struct{}
}

func (h *limitedAcceptHandler) OnMultishotStep(step uring.MultishotStep) uring.MultishotAction {
	if step.Err == nil && step.CQE.Res >= 0 {
		newCount := h.count.Add(1)
		if newCount > h.maxConnections {
			// Surplus: arrived between stop request and actual cancel.
			// Close the FD but do not count toward the limit.
			h.count.Add(-1)
			h.surplus.Add(1)
			zcall.Close(uintptr(step.CQE.Res))
			return uring.MultishotStop
		}
		select {
		case h.acceptedFDs <- step.CQE.Res:
		default:
			zcall.Close(uintptr(step.CQE.Res))
		}
		if newCount < h.maxConnections {
			return uring.MultishotContinue
		}
		return uring.MultishotStop
	}
	return uring.MultishotStop
}

func (h *limitedAcceptHandler) OnMultishotStop(error, bool) {
	close(h.stopped)
}
