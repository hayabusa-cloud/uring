// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package uring provides the kernel-boundary `io_uring` surface for Linux 6.18+.
// `uring` assumes the 6.18+ baseline and carries no fallback branches for older
// kernels. Its core Linux `io_uring` implementation was refactored from
// `code.hybscloud.com/sox` into this dedicated package. It prepares SQEs,
// decodes CQEs, transports submission context through `user_data`, and exposes
// kernel-boundary facts. Dispatch, retry, completion routing, route retirement,
// and connection/session orchestration stay in caller-side runtime code outside
// this boundary.
//
// A typical caller starts the ring, submits an operation, and then treats the
// completion queue as the source of truth for kernel results. This example has
// one outstanding read and uses [iox.Backoff] when no completion is available.
// The file retains close ownership. The ring owns the registered buffer until
// Stop; consume its bytes after completion and before reuse or ring teardown.
//
//	ring, err := uring.New(func(opt *uring.Options) {
//	    opt.Entries = uring.EntriesMedium
//	    opt.NotifySucceed = true
//	})
//	if err != nil {
//	    return err
//	}
//	if err := ring.Start(); err != nil {
//	    return err
//	}
//	defer ring.Stop()
//
//	fd := iofd.FD(file.Fd()) // Borrowed; file owns Close.
//	buf := ring.RegisteredBuffer(0)
//	ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
//	if err := ring.Read(ctx, buf); err != nil {
//	    return err
//	}
//
//	var cqes [1]uring.CQEView // Only one request is outstanding.
//	var backoff iox.Backoff
//	for {
//	    n, err := ring.Wait(cqes[:])
//	    if err != nil && !iox.IsWouldBlock(err) {
//	        return err
//	    }
//	    if n == 0 {
//	        backoff.Wait()
//	        continue
//	    }
//
//	    backoff.Reset()
//	    cqe := cqes[0]
//	    if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
//	        return fmt.Errorf("unexpected completion: op=%d fd=%d", cqe.Op(), cqe.FD())
//	    }
//	    if err := cqe.Err(); err != nil {
//	        return fmt.Errorf("uring read failed: %w", err)
//	    }
//	    handle(buf[:int(cqe.Res)])
//	    return nil
//	}
//
// [Uring.SubmitAcceptMultishot], [Uring.SubmitReceiveMultishot], and
// [Uring.SubmitReceiveBundleMultishot] submit raw multishot SQEs and keep the
// kernel-boundary flow explicit. [Uring.AcceptMultishot] and
// [Uring.ReceiveMultishot] use the same kernel path and return a
// [MultishotSubscription] for caller-owned callback dispatch in the same
// serialized completion loop. If copied CQEs outlive that loop, caller code
// must pair them with its own route state and reject observations for retired
// subscriptions.
// Dispatch cancellation CQEs through the same subscription's HandleCQE or
// their extended handlers, even after State reports SubscriptionStopped.
// The original pooled context is retained until both the original terminal
// CQE and any cancellation CQE have been handled.
//
//	sqeCtx := uring.ForFD(listenerFD)
//	sub, err := ring.AcceptMultishot(sqeCtx, handler)
//	if err != nil {
//	    return err
//	}
//
//	// Process CQEs in the same serialized completion loop.
//	for i := range n {
//	    if sub.HandleCQE(cqes[i]) {
//	        continue
//	    }
//	    dispatch(ring, cqes[i])
//	}
//
//	// Cancel when done
//	// On explicit single-issuer rings, call Cancel from the OS-thread-pinned
//	// ring owner and serialize it with submit, Wait, WaitDirect,
//	// WaitExtended, Stop, and ResizeRings.
//	if err := sub.Cancel(); err != nil {
//	    return err
//	}
//	// Keep dispatching the original terminal CQE and any queued cancellation CQE.
//
// [ListenerManager] submits the initial SOCKET and returns a [ListenerOp].
// Route setup CQEs to [ListenerOp.HandleCQE] to invoke callbacks and submit
// the next stage automatically:
//
//	SOCKET -> OnSocketCreated -> BIND -> OnBound -> LISTEN -> OnListening
//
// OnListening establishes readiness to accept. If HandleCQE returns
// [iox.ErrWouldBlock], drive the ring and retry the prepared SQE with
// ring.SubmitExtended(PackExtended(op.Ext())); never replay the CQE or callback.
// For manual advancement, use [DecodeListenerCQE], [PrepareListenerBind],
// [PrepareListenerListen], and [SetListenerReady] instead. Serialize dispatch,
// retry and Close. If Close is called while a setup CQE is pending, drain it
// before the final Close that returns the pooled context.
//
// Extended-mode raw `UserData` is caller-beware storage. Prefer scalar payloads
// there; if raw overlays or typed context views place Go pointers,
// interfaces, func values, maps, slices, strings, chans, or structs
// containing them in those bytes, caller code must keep the live roots
// outside `UserData`.
//
// [SQEContext] packs submission metadata into `user_data`.
//
// Direct mode layout (inline context, zero allocation):
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
// # Multishot Subscription Lifecycle
//
// Each original CQE with IORING_CQE_F_MORE leaves the multishot request live.
// Handling its terminal CQE sets [SubscriptionStopped] and, if callbacks remain
// enabled, invokes OnMultishotStop. This ends the original request; a queued
// cancellation still has its own completion obligation.
//
// The subscription owns its submitted [ExtSQE] through original terminality,
// cancellation dispatch, and any concurrent cancellation submission. The package
// returns that context to its pool only after all three obligations end. Caller
// code must not return the subscription's contexts manually, and must dispatch
// queued cancellation CQEs even after the original stop callback.
//
// Inspect each result independently of MORE: a live request does not imply byte
// progress or success. Returning [MultishotStop] from a non-final callback makes
// one best-effort cancellation attempt; failed admission leaves the subscription
// active. Caller code owns any retry through [MultishotSubscription.Cancel].
//
// # Runtime Boundary
//
// Multicast automatically selects ordinary or zero-copy sends by payload size
// and target count. MulticastZeroCopy favors zero-copy with lower thresholds.
// Inspect each CQE's opcode and flags; retrying fewer targets can change selection.
// Both require Direct contexts and return an accepted target count separately
// from the error. Retry starts at the first unaccepted target. Each accepted
// stream send may still complete partially. Keep success CQEs enabled when
// tracking ordinary-send byte counts, and use each target's data result to
// decide which byte suffix remains. Keep shared bytes immutable until every
// zero-copy borrow ends. Zero-copy sends retain required completions
// independently of NotifySucceed; MulticastZeroCopy also retains completions
// when its heuristic selects ordinary sends.
//
// uring is a kernel boundary, not a scheduler. It owns SQE encoding, CQE
// observation, `user_data` identity, capability exposure, and kernel-facing
// lifetimes. Runtime policy, completion routing, route retirement, batching,
// retries, parking, protocol parsing, safety checks, and terminal resource
// release belong outside this package.
//
// # Buffer Groups
//
// Buffer groups enable kernel-side buffer selection for receive operations.
// The kernel selects a published buffer while processing the receive and
// reports its ID in the CQE. Selection can precede completion; userspace does
// not select or assign buffers per receive. The package exposes
// three practical buffer-management paths: registered fixed buffers for
// fixed-buffer file I/O, provided buffers selected by the kernel, and bundle
// receives over buffers in publication order. IDs need not remain numerically
// consecutive after recycling; Uring.BundleIterator retains the actual order.
// NewBundleIterator requires a caller-maintained consecutive-ID layout.
//
// [IncrementalReceiver] tracks a separate offset for each buffer ID in an
// IOU_PBUF_RING_INC group. Dispatch its CQEs once, serially in CQ order.
// IORING_CQE_F_BUF_MORE retains the selected buffer for further consumption,
// independently of request completion. A CQE with IORING_CQE_F_BUFFER and
// without IORING_CQE_F_BUF_MORE returns that buffer, even for a zero or negative
// result. Finish borrowed payload access before republishing the buffer.
//
//	opts := uring.OptionsForBudget(256 * uring.MiB)
//	ring, err := uring.New(func(opt *uring.Options) {
//	    *opt = opts
//	})
//	if err != nil {
//	    return err
//	}
//
//	cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
//	fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
//
// A registered fixed buffer is ring-owned memory addressed by index. Keep the
// buffer live until the fixed operation completes.
//
//	fd := iofd.FD(file.Fd()) // Borrowed; file owns Close.
//	buf := ring.RegisteredBuffer(0)
//	copy(buf, payload)
//
//	writeCtx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, 0).WithFD(fd)
//	if err := ring.WriteFixed(writeCtx, 0, len(payload)); err != nil {
//	    return err
//	}
//
// For socket receive with kernel buffer selection, pass nil as the receive
// buffer and request the desired read-buffer size class. The matching CQE
// reports which buffer group and buffer ID were consumed.
//
//	recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 0)
//	if err := ring.Receive(recvCtx, &socketFD, nil, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
//	    return err
//	}
//
//	if cqe.HasBuffer() {
//	    fmt.Printf("kernel selected group=%d id=%d\n", cqe.BufGroup(), cqe.BufID())
//	}
//
// Bundle receives may consume more than one provided buffer in one CQE.
// Process the iterator and then recycle the consumed slots. Call Recycle
// exactly once across all copies of an iterator; after it returns, do not read
// the yielded buffers.
//
//	if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
//	    return err
//	}
//
//	if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
//	    for buf := range it.All() {
//	        handle(buf)
//	    }
//	    if err := it.Recycle(ring); err != nil {
//	        return err
//	    }
//	}
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
//   - [Uring.ReadFixed], [Uring.WriteFixed], [Uring.ReadvFixed], [Uring.WritevFixed] with registered-buffer ranges
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
//   - [Uring.UringCmd], [Uring.UringCmdInline], [Uring.UringCmd128]
//   - [Uring.Nop], [Uring.Nop128]
//
// Registration:
//   - Files: [Uring.RegisterFiles], [Uring.RegisterFilesSparse], [Uring.RegisterFilesUpdate], [Uring.UnregisterFiles], [Uring.RegisteredFileCount]
//   - Buffers: [Uring.RegisterBufRingMMAP], [Uring.RegisterBufRingIncremental], [Uring.RegisterBufRingWithFlags], [BufferRing.Add], [BufferRing.Advance], [Uring.RegisteredBuffer], [Uring.RegisteredBufferCount]
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
//   - [Uring.QueryZCRX], [Uring.RegisterZCRXIfq]
//   - [NewZCRXReceiver] is wired for rings created with 32-byte CQEs. The
//     current [Options] surface does not expose `IORING_SETUP_CQE32`, so rings
//     created through the standard [New] path return [ErrNotSupported] from
//     this constructor. Until a CQE32 setup path is exposed, the receiver docs
//     describe the boundary contract rather than a runnable public setup recipe.
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
// Completion reads acquire the kernel's published tail through Go atomics.
// Shared consumers claim the CQ copy with CAS and [spin.Wait], copy CQEs, and
// publish the head before admitting the next consumer. Submission and buffer
// publication also use [dwcas.BarrierRelease].
// User code does not manage these barriers.
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
