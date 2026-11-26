# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go package for the kernel-facing `io_uring` boundary on Linux 6.18+.

Language: **English** | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Overview

`uring` is the kernel-facing boundary for Linux `io_uring`. It creates and starts rings, prepares SQEs, decodes CQEs, carries submission identity through `user_data`, and exposes buffer registration, multishot operations, and listener-setup primitives without becoming a scheduler.

The package keeps the boundary explicit: kernel mechanics and observable completion facts live here; policy and composition live in caller-side layers. Caller-side runtime code owns completion routing, route retirement, retry/backoff, handler and session routing, connection lifecycle, safety checks, and terminal resource release.

The primary surfaces are:

- `Uring`, the live ring handle and operation set
- `SQEContext`, the submission context carried in `user_data`
- `CQEView`, the copied completion descriptor returned by `Wait`
- buffer provisioning through registered buffers and multi-size buffer groups

## Installation

Building `uring` requires Go 1.27 or later. Running it against the kernel requires Linux 6.18 or later. Check the running kernel version first.

```bash
uname -r
```

`uring` assumes the 6.18+ baseline and carries no fallback branches for older kernels. Boot a supported kernel instead of expecting compatibility shims inside this package.

Debian 13's stable kernel track may still be below 6.18. See [Debian 13 kernel upgrade](#debian-13-kernel-upgrade) for the backports path to a kernel that meets the requirement.

```bash
go get code.hybscloud.com/uring
```

### Debian 13 kernel upgrade

Debian 13 ships kernel 6.12 in its stable track. The `trixie-backports` suite provides a Debian-packaged 6.18+ kernel. See [SETUP.md](./SETUP.md) for step-by-step instructions.

### Troubleshooting

Ring creation may return `ENOMEM`, `EPERM`, or `ENOSYS` depending on memlock limits, sysctl settings, or kernel support. [Docker's default seccomp profile](https://docs.docker.com/engine/security/seccomp/) blocks `io_uring` syscalls. See [SETUP.md](./SETUP.md) for diagnosis and resolution.

## Caller guide

Start with the [file I/O example](./examples/file_io_test.go#L30) to drive a ring directly from `Start` through `Stop`. For a specific operation, follow the [caller guide](./agents/INDEX.md). The [package instructions](./AGENTS.md) summarize ownership and verification requirements.

A batch can accept a prefix and return `iox.ErrWouldBlock` for the rest. On a matching CQE, `Res` reports the operation result and `IORING_CQE_F_MORE` signals a possible successor. Only the caller’s protocol can interpret that flag as `iox.ErrMore`. Some errors follow a kernel effect. For example, a `MsgRingFD` source CQE can report `-EOVERFLOW` after the target file was installed. [File-transfer recovery](./agents/boundary/resources.md#inter-ring-file-transfers)

## Ring lifecycle

`New` creates an unstarted ring and its context pools. With the default `MultiIssuers=false`, keep its owning goroutine locked to one OS thread from before `Start` through `Stop`. Call `Start` to register resources and enable submissions.

This example reads into a ring-owned registered buffer whose address stays stable until `Stop`. Consume the bytes after completion and before reuse. `iox.IsWouldBlock` identifies no progress; use caller-owned `iox.Backoff` to keep waits CPU-friendly.

This example exits on unrecoverable errors with `log.Fatal`. Services must retain and drain pending requests before teardown.

```go
runtime.LockOSThread()
defer runtime.UnlockOSThread()

ring, err := uring.New(func(o *uring.Options) {
	o.Entries = uring.EntriesMedium
	o.NotifySucceed = true
})
if err != nil {
	log.Fatal(err)
}

if err := ring.Start(); err != nil {
	log.Fatal(err)
}

fd := iofd.FD(file.Fd()) // Borrowed; file owns Close.
buf := ring.RegisteredBuffer(0)
ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
if err := ring.Read(ctx, buf); err != nil {
	log.Fatal(err)
}

var cqes [1]uring.CQEView // Only one request is outstanding.
var backoff iox.Backoff
for {
	n, err := ring.Wait(cqes[:])
	if err != nil && !iox.IsWouldBlock(err) {
		log.Fatal(err)
	}
	if n == 0 {
		backoff.Wait()
		continue
	}

	backoff.Reset()
	break
}

cqe := cqes[0]
if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
	log.Fatalf("unexpected completion: op=%d fd=%d", cqe.Op(), cqe.FD())
}
runtime.KeepAlive(file) // Keep the owner of the borrowed FD live through the read.
completionErr := cqe.Err()
if completionErr == nil {
	handle(buf[:int(cqe.Res)])
}
if err := ring.Stop(); err != nil {
	log.Fatal(err)
}
if completionErr != nil {
	log.Fatal(completionErr)
}
```

`Wait` flushes pending submissions, then reaps completions. On single-issuer rings it also issues the kernel enter that keeps deferred task work moving once the SQ drains; the caller must serialize `Wait`, `WaitDirect`, and `WaitExtended` with other submit-state operations. `iox.IsWouldBlock(err)` means no completion is currently observable.

`Start` and `Stop` form the ring lifecycle pair. `Stop` is idempotent and renders the ring permanently unusable; call it only after you have drained all in-flight operations, reaped outstanding CQEs, and quiesced live multishot subscriptions.

## Types and operations

| Type | Role |
|------|------|
| `Uring` | Ring setup, submission, completion reaping, and operation methods |
| `Options` | Ring entries, registered-buffer budget, buffer-group scale, and completion visibility |
| `SQEContext` | Packed submission context stored in `user_data` |
| `CQEView` | Copied completion descriptor with decoded accessors and mode-sensitive borrowed targets |
| `ListenerOp` | Handle to a listener creation operation with FD and accept helpers |
| `BundleIterator` | Iterates over buffers consumed in a bundle receive |
| `BufferRing` | Opaque explicitly registered provided-buffer ring; `Add` stages and `Advance` publishes |
| `IncrementalReceiver` | Manages incremental buffer-ring receives (`IOU_PBUF_RING_INC`) |
| `ZCTracker` | Tracks zero-copy send notification and fallback lifecycle |
| `ContextPools` | Pools for indirect and extended submission contexts |
| `ZCRXReceiver` | Zero-copy receive lifecycle over a NIC RX queue |
| `ZCRXConfig` | Configuration for a ZCRX receive instance |
| `ZCRXHandler` | Callback interface for ZCRX data, errors, and shutdown |
| `ZCRXBuffer` | Delivered zero-copy receive view with caller-managed refill release |

Operations:

| Area | Methods |
|------|---------|
| Socket | `TCP4Socket`, `TCP6Socket`, `UDP4Socket`, `UDP6Socket`, `UDPLITE4Socket`, `UDPLITE6Socket`, `SCTP4Socket`, `SCTP6Socket`, `UnixSocket`, `SocketRaw`, plus `*Direct` variants |
| Connection | `Bind`, `Listen`, `Accept`, `AcceptDirect`, `Connect`, `Shutdown` |
| Socket I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `SendMsgHeader`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
| Multishot | `AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, `SubmitReceiveBundleMultishot` |
| File I/O | `Read`, `Write`, `ReadV`, `WriteV`, `ReadFixed`, `WriteFixed`, `ReadvFixed`, `WritevFixed` |
| File mgmt | `OpenAt`, `Close`, `Sync`, `Fallocate`, `FTruncate`, `Statx`, `RenameAt`, `UnlinkAt`, `MkdirAt`, `SymlinkAt`, `LinkAt` |
| Xattr | `FGetXattr`, `FSetXattr`, `GetXattr`, `SetXattr` |
| Transfer | `Splice`, `Tee`, `Pipe`, `SyncFileRange`, `FileAdvise` |
| Timeout | `Timeout`, `TimeoutRemove`, `TimeoutUpdate`, `LinkTimeout` |
| Cancel | `AsyncCancel`, `AsyncCancelFD`, `AsyncCancelOpcode`, `AsyncCancelAny`, `AsyncCancelAll` |
| Poll | `PollAdd`, `PollRemove`, `PollUpdate`, `PollAddLevel`, `PollAddMultishot`, `PollAddMultishotLevel` |
| Async | `EpollWait`, `FutexWait`, `FutexWake`, `FutexWaitV`, `Waitid` |
| Ring msg | `MsgRing`, `MsgRingFD`, `FixedFdInstall`, `FilesUpdate` |
| Cmd | `UringCmd`, `UringCmdInline`, `UringCmd128`, `Nop`, `Nop128` |

`UringCmdInline` copies up to 80 bytes into an ordinary `IORING_OP_URING_CMD` SQE and requires `Options.SQE128`; it does not require support for the separate `IORING_OP_URING_CMD128` opcode.

`WithUringCmdFlags` writes the `uring_cmd_flags` union. For `IORING_URING_CMD_FIXED`, set the registered-buffer index with `sqeCtx.WithBufGroup`; for `IORING_URING_CMD_MULTISHOT`, also set `IOSQE_BUFFER_SELECT` and use the same context field as the provided-buffer group. The two flags are mutually exclusive.

`Nop128` and `UringCmd128` require a ring created with `Options.SQE128` and kernel support for the corresponding opcodes. Without both, they return `ErrNotSupported`.

`PollAddLevel` and `PollAddMultishotLevel` always return `ErrNotSupported`; Linux io_uring does not admit the level flag for `IORING_OP_POLL_ADD`.

`Uring.Close` submits `IORING_OP_CLOSE` for a target file descriptor. It is not a ring teardown method.

## Context transport

`Multicast` automatically selects ordinary or zero-copy sends by payload size and target count; `MulticastZeroCopy` uses lower thresholds to favor zero-copy. Both require a Direct context and return `(submitted, error)`: `submitted` counts the accepted target prefix. Resume with the remaining targets after backpressure. Inspect each CQE's opcode and flags because a resumed call can select a different operation. Ordinary `Multicast` sends follow `NotifySucceed`; selected zero-copy sends and both `MulticastZeroCopy` paths retain completion evidence and reject explicit `IOSQE_CQE_SKIP_SUCCESS`.

For a short stream send, retry only that send’s unsent bytes. Enable success CQEs when tracking ordinary-send byte counts. Keep shared bytes immutable until every accepted send releases them. An ordinary send releases on completion. `SEND_ZC` requires terminal data without `IORING_CQE_F_MORE`, or both the data CQE and its matching notification. [Multicast example](./examples/zerocopy_test.go)

In Direct mode, `SQEContext` packs the opcode, SQE flags, buffer-group ID, and file descriptor into 64 bits. Overlapping sends with identical fields carry the same `user_data`. To match results to individual sends, serialize those sends or submit them separately with distinct live contexts.

```go
sqeCtx := uring.ForFD(int32(fd)).
	WithOp(uring.IORING_OP_RECV).
	WithBufGroup(groupID)
```

The three context modes are:

| Mode | Representation | Typical use |
|------|----------------|-------------|
| Direct | Inline 64-bit payload | Common submit and reap path, zero allocation |
| Indirect | Pointer to `IndirectSQE` | Full SQE payload when 64 bits are not enough |
| Extended | Pointer to `ExtSQE` | Full SQE plus 64 bytes of user data |

For the common path, start with `ForFD` or `PackDirect` and attach only the bits you need to see again at completion time. `WithFlags` replaces the entire flag set, so compute unions before calling it.

When you need caller-owned metadata beyond the 64-bit direct layout, borrow an `ExtSQE`, write into its `UserData` through `Ctx*Of` or `ViewCtx*`, and pack it back into an `SQEContext`. Prefer scalar payloads. If a raw overlay or typed view stores Go pointers, interfaces, func values, slices, strings, maps, chans, or structs containing them, keep the live roots outside `UserData`; the GC does not trace those raw bytes.

```go
ext := ring.ExtSQE()
if ext == nil {
	return iox.ErrWouldBlock
}
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
ring.PutExtSQE(ext)
```

`NewContextPools` returns pools that are ready to use. Context-pool exhaustion is independent of submission-queue capacity. Return each context to its originating pool after all request completions and borrowed access finish. `Reset` clears both pools and invalidates outstanding borrows; call it only after stopping all pool access and finishing operations that use its contexts. Do not access or return old borrows after `Reset`.

### Completion dispatch with `CQEView`

`iox.ErrWouldBlock` from `Wait` means no CQE is ready. On success, dispatch all `n` CQEs, including those for failed requests. For a local request, `cqe.Context()` returns its submission token. Use `cqe.Err()` on an operation result. A zero-copy send notification can carry usage flags in `Res`. The CQE flags describe request and buffer lifetimes.

The sender and receiver reserve `messageID` for `MsgRing`. Every other ID on this example's ring belongs to a local request. Match `messageID` first and pass the sender's signed `Res`, including negative values, to the message handler.

```go
var cqes [64]uring.CQEView
n, err := ring.Wait(cqes[:])
if err != nil {
	return err
}

for i := range n {
	cqe := cqes[i]
	if cqe.Context().Raw() == uint64(messageID) {
		handleMessage(cqe.Res)
		continue
	}
	if !cqe.IsNotification() && cqe.Res < 0 {
		fmt.Printf("completion failed: op=%d fd=%d: %v\n", cqe.Op(), cqe.FD(), cqe.Err())
	}
	if cqe.HasBuffer() {
		fmt.Printf("buffer id=%d\n", cqe.BufID())
	}
	// Route every CQE, including failures, so its owner can release resources.
	dispatch(ring, cqe)
}
```

`CQEView` decodes the matching context mode on demand at completion time. Its scalar descriptor is copied; Indirect/Extended mode targets and borrowed buffers must not outlive their documented lifetimes.

## Buffer provisioning

`uring` has three practical buffer paths. Registered buffers are pinned during ring setup and used by fixed-buffer file I/O. Provided buffer rings let the kernel choose a receive buffer and report the selected buffer ID in the CQE. Bundle receives consume buffers in publication order, which can differ from numeric ID order after recycling. `Uring.BundleIterator` preserves that order. The standalone `NewBundleIterator` requires consecutive IDs modulo the ring size.

- fixed-size provided buffers through `ReadBufferSize` and `ReadBufferNum`
- multi-size buffer groups through `MultiSizeBuffer`
- registered fixed buffers through `LockedBufferMem`, `RegisteredBuffer`, `ReadFixed`, and `WriteFixed`

For most systems the configuration helpers are the easiest entry point:

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
runtime.LockOSThread()
defer runtime.UnlockOSThread()

ring, err := uring.New(func(o *uring.Options) {
	*o = opts
})
if err != nil {
	return err
}
defer ring.Stop()
```

Use `OptionsForBudget` to start from an explicit memory budget, or `BufferConfigForBudget` to inspect the tier layout chosen for a given budget:

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

Explicit incremental rings are initialized empty. After `Start`, choose a group ID not used by the configured provided-buffer groups, stage each backing slice, then publish the batch:

```go
const (
	groupID = 60000
	entries = 8
	bufSize = 4096
)

backing := make([]byte, entries*bufSize)
bufRing, err := ring.RegisterBufRingIncremental(entries, groupID)
if err != nil {
	return err
}
for i := range entries {
	buf := backing[i*bufSize : (i+1)*bufSize]
	if err := bufRing.Add(buf, uint16(i), i); err != nil {
		return err
	}
}
if err := bufRing.Advance(entries); err != nil {
	return err
}

pool := uring.NewContextPools(entries)
receiver := uring.NewIncrementalReceiver(ring, pool, groupID, bufSize, backing, entries)
```

`BufferRing.Add` stages entries; `BufferRing.Advance` publishes them and transfers ownership to the kernel. Keep `backing` reachable. Dispatch this receiver's CQEs once, serially in CQ order; it tracks offsets independently for each buffer ID.

`IORING_CQE_F_BUF_MORE` retains the selected buffer even after the request completes. Recycle only after a CQE reports `IORING_CQE_F_BUFFER` without `IORING_CQE_F_BUF_MORE` and borrowed access has ended. A zero or negative result can also return a buffer. `Uring.Stop` owns buffer-ring teardown.

`WriteFixed` submits bytes from a registered buffer by index. Keep the file open and the submitted bytes unchanged until completion. If the CQE determines when to reuse the buffer, enable `NotifySucceed` and leave `IOSQE_CQE_SKIP_SUCCESS` clear.

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.FD(file.Fd()) // Borrowed; file owns Close.
ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, 0).WithFD(fd)
if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
	return err
}
```

For socket receive with kernel buffer selection, pass `nil` as the receive buffer and request the size class you want. The completion reports which buffer was selected:

```go
recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 0)

if err := ring.Receive(recvCtx, &socketFD, nil, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
	return err
}

// Later, after Wait returns the matching CQE:
if cqe.HasBuffer() {
	fmt.Printf("kernel selected group=%d id=%d\n", cqe.BufGroup(), cqe.BufID())
}
```

A receive bundle can consume several provided buffers in one CQE. On a stream socket, as in this example, EOF or an error can return one slot with zero bytes. Finish reading the payload, then call `Recycle` exactly once across all copies of the iterator to return every consumed slot.

```go
if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
	return err
}

if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
	if it.TotalBytes() > 0 {
		for buf := range it.All() {
			handle(buf)
		}
	}
	if err := it.Recycle(ring); err != nil {
		return err
	}
}
```

Registered buffers require pinned memory. If large buffer registration fails, increase `RLIMIT_MEMLOCK` or use a smaller memory budget.

## Multishot and listener operations

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, and `SubmitReceiveBundleMultishot` each submit a multishot socket operation.

CQE routing stays with the caller. `ListenerOp.HandleCQE` advances SOCKET → BIND → LISTEN and invokes the setup callbacks. `DecodeListenerCQE`, `PrepareListenerBind`, `PrepareListenerListen`, and `SetListenerReady` remain available for manual advancement. See the [listener guide](agents/recipes/listener.md).

## Architecture implementation

The implementation sits at this boundary:

1. `New` builds a disabled kernel ring, constructs context pools, and selects a buffer strategy.
2. `Start` registers buffers and enables the ring for the 6.18+ baseline.
3. Operation methods express intent by writing SQEs.
4. `Wait` flushes submissions and returns copied CQE descriptors.
5. Caller-side runtime code decides scheduling, retries, parking, connection/session routing, route retirement, safety checks, and terminal resource policy.

This keeps `code.hybscloud.com/uring` focused on kernel-facing mechanics and preserves completion meaning across the boundary.

## Runtime boundary

Runtime layers above `code.hybscloud.com/uring` should use it as the kernel backend, not as a scheduler. The boundary is one-way: `code.hybscloud.com/uring` prepares SQEs, reaps CQEs, preserves `user_data`, exposes CQE `res` and flags, and reports ownership facts; caller-side runtime code correlates those observations with its own tokens, applies retry/backoff, routes handlers and sessions, batches submissions, retires routes, and releases terminal resources.

A runtime bridge can consume Extended-mode CQEs when abstract execution needs completion facts. A connection-scoped runtime can also poll raw Extended CQEs directly when it needs the CQE result, flags, buffer ID, and encoded token before reducing the event to handler callbacks.

Context and abstract-execution layers above this boundary do not change `code.hybscloud.com/uring`'s kernel-boundary role.

## Application-layer patterns

`code.hybscloud.com/uring` exposes kernel mechanics; scheduling, retry, connection tracking, and protocol interpretation belong in caller-side layers above it. The patterns below describe the boundary a caller-side runtime must preserve.

### Ring-owning event loop

Pin the default ring’s owner goroutine to its OS thread before `Start` and keep it pinned through `Stop`. Once requests are submitted, the owner calls `Wait` to reap their completions.

```go
cqes := make([]uring.CQEView, 64)
var backoff iox.Backoff
for {
	select {
	case <-stop:
		// The caller cancels and drains live requests before stopping the ring.
		return nil
	default:
	}

	n, err := ring.Wait(cqes)
	switch iox.Classify(err) {
	case iox.OutcomeOK:
		backoff.Reset()
		for i := range n {
			dispatch(ring, cqes[i])
		}
	case iox.OutcomeWouldBlock:
		backoff.Wait()
	case iox.OutcomeFailure:
		return err
	}
}
```

A successful `Wait` can include a CQE for a failed operation, so dispatch all `n` entries. If the owner also has submissions to retry, check them between polls, even when the CQ is empty. [Retrying a managed accept](agents/recipes/stream.md#rearm-a-managed-accept)

### Multishot subscription lifecycle

A multishot operation produces a stream of CQEs until the kernel sends a final one (without `IORING_CQE_F_MORE`). Caller-side code routes each CQE through the returned subscription in the same serialized completion loop before falling back to the rest of the dispatcher:

```go
accepting := true
handler := uring.NewMultishotSubscriber().
	OnStep(func(step uring.MultishotStep) uring.MultishotAction {
		if step.Err != nil {
			return uring.MultishotStop
		}
		connFD := iofd.FD(step.CQE.Res)
		if accepting {
			registerConnection(connFD)
		} else if err := connFD.Close(); err != nil {
			log.Print(err)
		}
		return uring.MultishotContinue
	}).
	OnStop(func(err error, cancelled bool) {
		if accepting && !cancelled {
			queueAcceptRetry(err)
		}
	})

sub, err := ring.AcceptMultishot(acceptCtx, handler.Handler())
if err != nil {
	return err
}

// Dispatch in the same serialized completion loop. If caller code stores
// copied CQEs beyond this loop, it must keep its own route state.
for i := range n {
	if sub.HandleCQE(cqes[i]) {
		continue
	}
	dispatch(ring, cqes[i])
}
```

`queueAcceptRetry` records the terminal result for the ring owner. During `OnStop`, the old subscription can still hold the last Extended context. After dispatch, the owner checks whether the result and listener state warrant another accept. If `AcceptMultishot` returns `iox.ErrWouldBlock`, the retry stays pending. [Rearming a managed accept](agents/recipes/stream.md#rearm-a-managed-accept)

Each `OnStep` callback observes one `MultishotStep`: return `MultishotContinue` to keep the stream live, or `MultishotStop` to request cancellation. If callbacks remain enabled until the terminal observation, `OnStop` runs at most once with the final error and a `cancelled` flag; on the step itself, `step.Cancelled` distinguishes the specific `-ECANCELED` kernel verdict from other failures at the boundary. These callbacks correspond to `MultishotHandler.OnMultishotStep` and `MultishotHandler.OnMultishotStop`; you can also implement that interface directly. `HandleCQE` is for immediate dispatch in the caller's serialized completion loop; if caller code stores copied CQEs beyond that loop, it must keep its own route state and reject observations for retired subscriptions. On default single-issuer rings (`MultiIssuers=false`), call `Cancel` / `Unsubscribe` from the OS-thread-pinned ring owner and serialize them with submit, `Wait`, `WaitDirect`, `WaitExtended`, `Stop`, and `ResizeRings`. `MultiIssuers=true` rings serialize cancel SQEs through the shared-submit path.

Route every queued cancellation CQE through the same subscription's `HandleCQE` or its extended handler, even after `SubscriptionStopped` or `OnStop`. Failed submission or a no-op creates no new cancellation CQE obligation. The subscription owns both contexts and returns the original to its pool only after original terminality, cancellation dispatch, and cancellation submission have finished. Do not return these contexts manually. The [receive example](./examples/multishot_test.go) demonstrates natural completion and handler-requested cancellation.

To stop taking connections, the loop owner sets `accepting = false` before calling `sub.Cancel()`. The step handler then closes late sockets. After `Unsubscribe`, the dispatcher owns each successful accept whose callback was suppressed. [Accepted sockets](agents/boundary/resources.md#accepted-sockets)

### Per-connection state with typed contexts

An Extended context can carry connection state through a send. Enable `NotifySucceed` and leave `IOSQE_CQE_SKIP_SUCCESS` clear so the caller receives a CQE before returning the context to its pool, including when `Send` succeeds.

```go
type ConnState struct {
	Addr    netip.AddrPort
	Created int64
}

ext := ring.ExtSQE()
if ext == nil {
	return iox.ErrWouldBlock
}
ctx := uring.Ctx1V1Of[ConnState](ext)
ctx.Ref1 = connState
ctx.Val1 = sequenceNumber

sqeCtx := uring.PackExtended(ext)
if err := ring.Send(sqeCtx, &fd, payload); err != nil {
	ring.PutExtSQE(ext)
	return err
}
```

After receiving the matching `SEND` CQE, copy the connection reference and sequence number before returning the context to the pool.

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

Keep live Go pointer roots reachable outside `UserData`. The GC does not trace those raw bytes. The sidecar root set attached to each `ExtSQE` slot handles this for internal multishot and listener protocols, but caller-side runtime code that places typed refs must keep them reachable independently.

### Deadline composition

`LinkTimeout` applies to the preceding SQE marked `IOSQE_IO_LINK`. On a started ring with a single owner, check capacity and queue the two SQEs together before any other submission or `Wait`. The return count below records how many requests the caller must still reap. Use a nonempty receive buffer and keep its address stable until the receive CQE.

```go
func queueReceiveWithTimeout(ring *uring.Uring, fd iofd.FD, buf []byte, d time.Duration) (int, error) {
	if ring.SQAvailable() < 2 {
		return 0, iox.ErrWouldBlock
	}
	recvCtx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_IO_LINK, 0, int32(fd))
	if err := ring.ReceiveFD(recvCtx, fd, buf); err != nil {
		return 0, err
	}
	timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 0, 0)
	if err := ring.LinkTimeout(timeoutCtx, d); err != nil {
		return 1, err
	}
	return 2, nil
}
```

On `iox.ErrWouldBlock` with zero admissions, drive the ring before retrying the pair. If only the receive was queued, flush it before submitting other work and retain `buf` through its completion. Once both are queued, reap both CQEs. Expiry attempts cancellation. The receive CQE gives the receive result. [Linked timeout example](./examples/timeout_linux_test.go#L259)

## TCP usage patterns

These are the shortest flows, meant to be read alongside the tests:

| Scenario    | Main APIs                                                        | Reference                                                                         |
|-------------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo server | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client      | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP echo server

`ListenerManager` submits the initial SOCKET request. Route each setup CQE to `listenerOp.HandleCQE` to invoke callbacks and submit BIND and LISTEN automatically. If it returns `iox.ErrWouldBlock`, drive the ring and retry the prepared SQE with `ring.SubmitExtended(uring.PackExtended(listenerOp.Ext()))`; do not replay the CQE. Start accepting after `OnListening` reports success.

```go
acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
if err != nil {
	return err
}
```

Keep `acceptSub` in the listener’s dispatcher. For each successful accept, the connection owner takes `iofd.FD(step.CQE.Res)` as `clientFD`. If receive admission returns `iox.ErrWouldBlock`, retain that FD and retry only the receive. Close it if you abandon the connection.

```go
recvCtx := uring.ForFD(int32(clientFD)).WithBufGroup(readGroup)
recvSub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
if err != nil {
	return err
}
```

Call `Cancel` when stopping a subscription, then keep dispatching until its terminal CQE and any submitted cancellation CQE have been consumed. Drain each admitted subscription before closing its descriptor or stopping the ring. [The listener example](./listener_example_test.go) shows the complete setup and shutdown; [multishot examples](./examples/multishot_test.go) and [echo examples](./examples/echo_test.go) cover the connection flow.

### TCP client

Create a socket, wait for the `IORING_OP_SOCKET` completion, then wrap the returned FD in an `iofd.FD` for `Connect`, `Send`, and `Receive`.

```go
clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
if err := ring.TCP4Socket(clientCtx); err != nil {
	return err
}

// After Wait returns the matching SOCKET CQE:
if err := socketCQE.Err(); err != nil {
	return err
}
clientFD := iofd.NewFD(int(socketCQE.Res))

connectCtx := uring.PackDirect(uring.IORING_OP_CONNECT, 0, 0, int32(clientFD))
if err := ring.Connect(connectCtx, remoteAddr); err != nil {
	return err
}

// Wait for a successful CONNECT CQE before sending.
sendCtx := uring.PackDirect(uring.IORING_OP_SEND, 0, 0, int32(clientFD))
if err := ring.Send(sendCtx, &clientFD, payload); err != nil {
	return err
}

// Check the SEND CQE and retry only any unsent suffix.
recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, int32(clientFD))
if err := ring.Receive(recvCtx, &clientFD, buf); err != nil {
	return err
}
// Wait for the RECV CQE before reading or reusing buf.
```

Enable `NotifySucceed` and leave `IOSQE_CQE_SKIP_SUCCESS` clear for the `Send` in this sequence. Use `Wait` to observe each matching CQE before the next dependent operation. Keep the payload valid through completion and retry only the unsent suffix of a short send. [TCP client](./examples/socket_test.go) · [Echo examples](./examples/echo_test.go)

## Zero-copy receive (ZCRX)

`ZCRXReceiver` drives zero-copy receive from a NIC hardware RX queue through `io_uring`.

`NewZCRXReceiver` is wired for rings with 32-byte CQEs (`IORING_SETUP_CQE32`). The current `Options` surface does not expose that setup flag, so this constructor returns `ErrNotSupported` for rings created through the standard `New` path. Until a CQE32 setup path is exposed, this section documents the receiver boundary contract rather than a runnable public setup recipe.

### Lifecycle

1. With a CQE32-enabled ring, create the receiver with `NewZCRXReceiver`. The constructor registers the ZCRX interface queue, maps the receive area, and initializes an empty refill ring. Linux supplies the initial receive buffers; the refill ring returns delivered buffers.
2. Call `Start` to submit the extended `RECV_ZC` operation.
3. On the CQE dispatch path, ZCRX completions route to the `ZCRXHandler`:
    - `OnData` delivers a `ZCRXBuffer` pointing into the NIC-mapped area. Call `Release` when done; successful refill publication returns the slot to the kernel. If it returns `iox.ErrWouldBlock`, retain the buffer and retry release later on the CQE path. A zero-length EOF buffer has no refill identity and releases without a refill entry. Return `false` to request a best-effort stop.
    - `OnError` delivers CQE errors. Return `false` to request a best-effort stop.
    - `OnStopped` fires once during terminal retirement, before the state reaches `Stopped`.
4. Call `Stop` to submit an async cancel. The receiver transitions through `Stopping` → `Retiring` → `Stopped`.
5. Poll `Stopped` until it returns `true`, stop the owning ring, then call `Close` to release the mapped area and the refill-ring mapping.

### State machine

```
Idle → Active → Stopping → Retiring → Stopped
```

`Stop` reverts to `Active` if cancel submission fails. `Close` is idempotent.

### Handler contract

- `OnData` and `OnError` are called serially from the CQE dispatch goroutine.
- `Release` is single-producer; call it only from the dispatch goroutine.
- `Stop` must not race with CQE dispatch. The caller is responsible for this serialization.

## Examples

The runnable tests in `examples/` teach setup → submission → completion → cleanup through direct public API calls. Start with contexts and file I/O, then follow the ownership rules in the socket and multishot examples.

- [context_test.go](./examples/context_test.go): Direct, Indirect, and Extended contexts; decoding completions and returning borrowed contexts
- [completion_test.go](./examples/completion_test.go): submission versus completion errors, request identity, and draining mixed-result batches
- [file_io_test.go](./examples/file_io_test.go): file reads and writes, chunked reads through EOF, and batched submissions
- [file_lifecycle_test.go](./examples/file_lifecycle_test.go): open, write, sync, and close a file through completions
- [vectored_io_test.go](./examples/vectored_io_test.go): scatter/gather reads and writes
- [fixed_buffers_test.go](./examples/fixed_buffers_test.go): registered buffers and fixed-buffer I/O
- [buffer_ring_test.go](./examples/buffer_ring_test.go): bundle receives, incremental receives, and buffer recycling
- [socket_test.go](./examples/socket_test.go): client socket creation, connection completion, and sending the remaining byte suffix
- [echo_test.go](./examples/echo_test.go): TCP echo, partial stream I/O, and UDP datagrams
- [multishot_test.go](./examples/multishot_test.go): repeated accept/receive completions and cancellation draining
- [zerocopy_test.go](./examples/zerocopy_test.go): zero-copy sends, automatic multicast selection, and separate result/release tracking
- [poll_test.go](./examples/poll_test.go): readiness observations and poll cancellation
- [timeout_linux_test.go](./examples/timeout_linux_test.go): expiry, cancellation, and linked timeouts
- [splice_tee_test.go](./examples/splice_tee_test.go): completion-driven splice and tee transfers

At the package root, [listener_example_test.go](./listener_example_test.go) demonstrates automatic SOCKET → BIND → LISTEN setup and multishot accept; [socket_integration_linux_test.go](./socket_integration_linux_test.go) covers TCP connect/send.

## Operational notes

- Enable `NotifySucceed` to retain successful write-like CQEs by default. Explicit `IOSQE_CQE_SKIP_SUCCESS` flags still apply.
- `ring.Features` reports actual SQ/CQ entry counts, SQE slot width, and the byte order used to interpret `user_data`.
- The default `MultiIssuers=false` requests `SINGLE_ISSUER` + `DEFER_TASKRUN` and avoids shared-submit synchronization. Lock the owner with `runtime.LockOSThread` before `Start`, remain pinned through `Stop`, and serialize submissions, waits, resize, and teardown. Select `MultiIssuers=true` (`COOP_TASKRUN`) for shared submission or OS-thread migration.
- `EpollWait` requires `timeout` to remain `0`; use `LinkTimeout` when you need a deadline.
- Return pooled contexts only after all request completions and borrowed access have finished.
- After `ListenerOp.Close`, `FD()` returns `-1`. A pending `BIND` or `LISTEN` keeps the socket open until its CQE is decoded. Dispatch any pending setup CQE with `HandleCQE`, then call `Close` again to return the borrowed `ExtSQE` to the pool. [Manual dispatch](agents/recipes/listener.md)

## Platform support

The kernel-backed implementation requires Go 1.27+ and Linux 6.18+. The Darwin build includes a userspace simulator for a subset of the shared API. CI compiles the Darwin targets and verifies kernel behavior on native Linux.

## License

MIT, see [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
