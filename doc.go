// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package uring provides the kernel-boundary `io_uring` surface for Linux 6.18+.
// Its core Linux `io_uring` implementation was refactored from
// `code.hybscloud.com/sox` into this dedicated package. It prepares SQEs,
// decodes CQEs, transports submission context through `user_data`, and exposes
// kernel-boundary facts; dispatch, retry, and other orchestration policy stay in
// higher layers.
//
//	// Create TCP socket
//	socketCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
//	ring.TCP4Socket(socketCtx)
//
//	// Submit low-level multishot accept (one SQE, multiple CQEs)
//	acceptCtx := uring.PackDirect(uring.IORING_OP_ACCEPT, 0, 0, listenerFD)
//	ring.SubmitAcceptMultishot(acceptCtx)
//
//	// Start multishot receive with buffer selection
//	recvCtx := uring.ForFD(clientFD).WithBufGroup(bufGroupID)
//	sub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
//
// Completions return the kernel result together with the submission context.
//
//	cqes := make([]uring.CQEView, 64)
//	n, err := ring.Wait(cqes)  // Poll CQ, returns iox.ErrWouldBlock if empty
//
//	for i := range n {
//	    cqe := cqes[i]
//	    if cqe.Res < 0 {
//	        return fmt.Errorf("completion failed: op=%d fd=%d res=%d", cqe.Op(), cqe.FD(), cqe.Res)
//	    }
//	    fmt.Printf("completed op=%d on fd=%d with res=%d\n", cqe.Op(), cqe.FD(), cqe.Res)
//	}
//
// [Uring.SubmitAcceptMultishot], [Uring.SubmitReceiveMultishot], and
// [Uring.SubmitReceiveBundleMultishot] submit raw multishot SQEs and keep the
// kernel-boundary flow explicit. [Uring.AcceptMultishot] and
// [Uring.ReceiveMultishot] use the same kernel path and return a
// [MultishotSubscription] when caller code wants callback-driven retirement.
//
//	sqeCtx := uring.ForFD(listenerFD)
//	sub, err := ring.AcceptMultishot(sqeCtx, handler)
//
//	// Process CQEs - higher layers decide how to route decoded CQEs
//	for i := range n {
//	    dispatch(handler, cqes[i])
//	}
//
//	// Cancel when done
//	sub.Cancel()
//
// Listener setup advances with [DecodeListenerCQE], [PrepareListenerBind],
// [PrepareListenerListen], and [SetListenerReady]. [ListenerManager] is a thin
// convenience for the initial SOCKET submission and returns a [ListenerOp]. If
// [ListenerOp.Close] races a pending listener setup CQE, drain that CQE before
// the final Close that returns the pooled listener context.
//
//	pool := uring.NewContextPools(16)
//	manager := uring.NewListenerManager(ring, pool)
//
//	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
//	op, err := manager.ListenTCP4(addr, 128, handler)
//
//	// Caller decodes CQEs and chains bind→listen via Prepare helpers
//	// After LISTEN completes, start accepting:
//	acceptSub, err := op.AcceptMultishot(acceptHandler)
//
// Extended-mode raw `UserData` is caller-beware storage. Prefer scalar payloads
// there; if raw overlays or typed context views place Go pointers,
// interfaces, func values, maps, slices, strings, chans, or structs
// containing them in those bytes, caller code must keep the live roots
// outside `UserData`.
//
// [SQEContext] packs submission metadata into `user_data`.
//
// Direct mode layout (zero allocation, most common):
//
//	┌─────────┬─────────┬──────────────┬────────────────────────────┬────┐
//	│ Op (8b) │Flags(8b)│ BufGrp (16b) │        FD (30b)            │Mode│
//	└─────────┴─────────┴──────────────┴────────────────────────────┴────┘
//	  Bits 0-7  Bits 8-15  Bits 16-31     Bits 32-61              Bits 62-63
//
// Mode bits (62-63): 00=Direct, 01=Indirect (64B ptr), 10=Extended (128B ptr)
//
// Pack context for submission:
//
//	ctx := uring.PackDirect(
//	    uring.IORING_OP_RECV,   // Op: operation type
//	    0,                      // Flags: SQE flags
//	    bufferGroupID,          // BufGroup: for buffer selection
//	    clientFD,               // FD: target file descriptor
//	)
//
// If `IOSQE_FIXED_FILE` is set, the FD field stores the registered file index
// instead of a raw file descriptor.
//
// Or use the fluent builder:
//
//	ctx := uring.ForFD(clientFD).WithOp(uring.IORING_OP_RECV).WithBufGroup(groupID)
//
// # Handler Patterns
//
// Handler helpers provide convenience step/action adapters. They do not change
// the underlying CQE facts and are optional.
//
// Subscriber pattern (functional callbacks):
//
//	handler := uring.NewMultishotSubscriber().
//	    OnStep(func(step uring.MultishotStep) uring.MultishotAction {
//	        if step.Err == nil {
//	            return uring.MultishotContinue
//	        }
//	        return uring.MultishotStop
//	    }).
//	    OnStop(func(err error, cancelled bool) {
//	        log.Println("stopped", err, cancelled)
//	    })
//
// Noop embedding pattern (override only needed methods):
//
//	type myHandler struct {
//	    uring.NoopMultishotHandler
//	    connections int
//	}
//
//	func (h *myHandler) OnMultishotStep(step uring.MultishotStep) uring.MultishotAction {
//	    if step.Err == nil && step.CQE.Res >= 0 {
//	        h.connections++
//	        return uring.MultishotContinue
//	    }
//	    return h.NoopMultishotHandler.OnMultishotStep(step)
//	}
//
// Handlers either return `MultishotContinue` to keep a live subscription, or
// `MultishotStop` to request cancellation after the current step. The request
// is local until the cancel SQE is successfully enqueued.
//
// # Buffer Groups
//
// Buffer groups enable kernel-side buffer selection for receive operations.
// The kernel picks an available buffer from the group at completion time;
// userspace does not select or assign buffers per receive.
//
//	opts := uring.OptionsForBudget(256 * uring.MiB)
//	ring, _ := uring.New(func(opt *uring.Options) {
//	    *opt = opts
//	})
//
// # Supported Operations
//
// Socket creation:
//   - TCP: [Uring.TCP4Socket], [Uring.TCP6Socket], [Uring.TCP4SocketDirect], [Uring.TCP6SocketDirect]
//   - UDP: [Uring.UDP4Socket], [Uring.UDP6Socket], [Uring.UDP4SocketDirect], [Uring.UDP6SocketDirect]
//   - UDPLITE: [Uring.UDPLITE4Socket], [Uring.UDPLITE6Socket], [Uring.UDPLITE4SocketDirect], [Uring.UDPLITE6SocketDirect]
//   - SCTP: [Uring.SCTP4Socket], [Uring.SCTP6Socket], [Uring.SCTP4SocketDirect], [Uring.SCTP6SocketDirect]
//   - Unix: [Uring.UnixSocket], [Uring.UnixSocketDirect]
//   - Generic: [Uring.SocketRaw], [Uring.SocketDirect]
//
// Socket operations:
//   - [Uring.Bind], [Uring.Listen], [Uring.Accept], [Uring.AcceptDirect], [Uring.Connect], [Uring.Shutdown]
//   - [Uring.Receive], [Uring.Send], [Uring.RecvMsg], [Uring.SendMsg]
//   - [Uring.ReceiveBundle], [Uring.ReceiveZeroCopy]
//   - [Uring.Multicast], [Uring.MulticastZeroCopy]
//   - Raw multishot submits: [Uring.SubmitAcceptMultishot], [Uring.SubmitAcceptDirectMultishot], [Uring.SubmitReceiveMultishot], [Uring.SubmitReceiveBundleMultishot]
//   - Helper-backed multishot subscriptions: [Uring.AcceptMultishot], [Uring.ReceiveMultishot]
//
// File operations:
//   - [Uring.Read], [Uring.Write], [Uring.ReadV], [Uring.WriteV]
//   - [Uring.ReadFixed], [Uring.WriteFixed], [Uring.ReadvFixed], [Uring.WritevFixed] with registered buffers
//   - [Uring.OpenAt], [Uring.Close], [Uring.Sync], [Uring.Fallocate], [Uring.FTruncate]
//   - [Uring.Statx], [Uring.RenameAt], [Uring.UnlinkAt], [Uring.MkdirAt], [Uring.SymlinkAt], [Uring.LinkAt]
//   - [Uring.FGetXattr], [Uring.FSetXattr], [Uring.GetXattr], [Uring.SetXattr]
//   - [Uring.Splice], [Uring.Tee], [Uring.Pipe] for zero-copy data transfer
//   - [Uring.SyncFileRange], [Uring.FileAdvise]
//   - [Uring.Close] submits `IORING_OP_CLOSE` for the target file descriptor; it
//     does not tear down the ring itself
//
// Control operations:
//   - [Uring.Timeout], [Uring.TimeoutRemove], [Uring.TimeoutUpdate], [Uring.LinkTimeout]
//   - [Uring.AsyncCancel], [Uring.AsyncCancelFD], [Uring.AsyncCancelOpcode], [Uring.AsyncCancelAny], [Uring.AsyncCancelAll]
//   - [Uring.PollAdd], [Uring.PollRemove], [Uring.PollUpdate], [Uring.PollAddLevel], [Uring.PollAddMultishot], [Uring.PollAddMultishotLevel]
//   - [Uring.EpollWait], [Uring.FutexWait], [Uring.FutexWake], [Uring.FutexWaitV], [Uring.Waitid]
//   - [Uring.MsgRing], [Uring.MsgRingFD]
//   - [Uring.FixedFdInstall], [Uring.FilesUpdate]
//   - [Uring.UringCmd]
//   - [Uring.Nop]
//
// Registration:
//   - Files: [Uring.RegisterFiles], [Uring.RegisterFilesSparse], [Uring.RegisterFilesUpdate], [Uring.UnregisterFiles], [Uring.RegisteredFileCount]
//   - Buffers: [Uring.RegisterBufRingMMAP], [Uring.RegisterBufRingIncremental], [Uring.RegisterBufRingWithFlags], [Uring.RegisteredBuffer], [Uring.RegisteredBufferCount]
//   - Buffer cloning: [Uring.CloneBuffers], [Uring.CloneBuffersFromRegistered]
//   - Memory: [Uring.RegisterMemRegion]
//   - NAPI: [Uring.RegisterNAPI], [Uring.UnregisterNAPI], [Uring.NAPIAddStaticID], [Uring.NAPIDelStaticID]
//
// Ring management:
//   - [Uring.Start], [Uring.Stop], [Uring.Wait], [Uring.ResizeRings]
//   - [Uring.SQAvailable], [Uring.CQPending], [Uring.RingFD]
//   - [Uring.ExtSQE], [Uring.PutExtSQE], [Uring.IndirectSQE], [Uring.PutIndirectSQE], [Uring.SubmitExtended]
//
// Capability queries:
//   - [Uring.QueryOpcodes]
//
// Zero-copy receive (ZCRX):
//   - [Uring.RegisterZCRXIfq]
//
// # Performance
//
// The hot submit and reap paths are designed to remain zero-allocation.
// See the benchmark tests for current machine-specific numbers.
//
// # Ring Setup
//
// Create and start an io_uring instance:
//
//	ring, err := uring.New(func(opt *uring.Options) {
//	    opt.Entries = uring.EntriesMedium // 2048 entries
//	})
//	if err != nil {
//	    return err
//	}
//	if err := ring.Start(); err != nil {
//	    return err
//	}
//
// # Memory Barriers
//
// The package uses [dwcas.BarrierAcquire] and [dwcas.BarrierRelease] for SQ/CQ ring
// synchronization. On amd64 (TSO), these are compiler barriers. On arm64, they
// emit DMB ISHLD/ISHST instructions. User code does not manage these barriers.
//
// # Dependencies
//
//   - [code.hybscloud.com/zcall]: Zero-overhead syscalls
//   - [code.hybscloud.com/iox]: Non-blocking I/O semantics and error types
//   - [code.hybscloud.com/iofd]: File descriptor abstractions
//   - [code.hybscloud.com/iobuf]: Buffer pools and aligned memory
//   - [code.hybscloud.com/sock]: Socket types and address handling
//   - [code.hybscloud.com/dwcas]: Memory barriers for ring synchronization
//   - [code.hybscloud.com/spin]: Spin-wait primitives
//   - [code.hybscloud.com/framer]: Message framing for length-prefix encoding
package uring
