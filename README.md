# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go package for the kernel-facing `io_uring` boundary on Linux 6.18+.

Language: **English** | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Overview

`uring` is the kernel-facing boundary for Linux `io_uring`. It creates and starts rings, prepares SQEs, decodes CQEs, carries submission identity through `user_data`, and exposes buffer registration, multishot operations, and listener-setup primitives without becoming a scheduler.

The package keeps the boundary explicit: kernel mechanics and observable completion facts live here; policy and composition live in caller-side layers. Caller-side runtime code owns completion routing, route retirement, retry/backoff, handler and session routing, connection lifecycle, safety checks, and terminal resource release.

The primary surfaces are:

- `Uring`, the live ring handle and operation set
- `SQEContext`, the submission identity carried in `user_data`
- `CQEView`, the copied completion descriptor returned by `Wait`
- buffer provisioning through registered buffers and multi-size buffer groups

## Installation

`uring` requires Linux kernel 6.18 or later. Check the running kernel first:

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

Ring creation may return `ENOMEM`, `EPERM`, or `ENOSYS` depending on memlock limits, sysctl settings, or kernel support. Container runtimes block `io_uring` syscalls by default. See [SETUP.md](./SETUP.md) for diagnosis and resolution.

## Agentic coding around the `code.hybscloud.com/uring` boundary

Use the standalone guide entrypoint [`AGENTS.md`](./AGENTS.md) when generating or reviewing caller-side code around the `code.hybscloud.com/uring` boundary: runtimes, protocol stacks, services, and review work that needs a precise Linux `io_uring` boundary. Use [`agents/INDEX.md`](./agents/INDEX.md) as the topic index to preserve the kernel boundary: SQE/CQE mechanics, `user_data` identity, buffer ownership, completion outcomes, lifecycle rules, integration points, reference checks, and the workflow from analysis through formalization, reasoning, compilation into Go, and verification.

The rule is simple: `code.hybscloud.com/uring` exposes kernel facts; caller-side layers choose policy. Keep retry/backoff, poll cadence, scheduling, routing, route retirement, protocol state, parsing, completion routing, cancellation and timeout policy, safety checks, and service lifecycle outside the `code.hybscloud.com/uring` boundary, and keep `ErrWouldBlock`, `ErrMore`, `IORING_CQE_F_MORE`, ownership transfer, and capability failures visible.

## Ring lifecycle

`New` returns an unstarted ring and eagerly constructs the context pools. Call `Start` before submitting operations; it registers ring resources and enables the ring. The example below submits a read, waits for the matching CQE, and uses `iox.Classify` so `ErrWouldBlock` stays a semantic no-progress result rather than a failure.

```go
ring, err := uring.New(func(o *uring.Options) {
    o.Entries = uring.EntriesMedium
    o.NotifySucceed = true
})
if err != nil {
    return err
}

if err := ring.Start(); err != nil {
    return err
}
defer ring.Stop()

fd := iofd.NewFD(int(file.Fd()))
buf := make([]byte, 4096)
ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
if err := ring.Read(ctx, buf); err != nil {
    return err
}

cqes := make([]uring.CQEView, 64)
var backoff iox.Backoff

for {
    n, err := ring.Wait(cqes)
    switch iox.Classify(err) {
    case iox.OutcomeWouldBlock:
        backoff.Wait()
        continue
    case iox.OutcomeFailure:
        return err
    }
    if n == 0 {
        backoff.Wait()
        continue
    }

    backoff.Reset()
    for i := range n {
        cqe := cqes[i]
        if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
            continue
        }
        if err := cqe.Err(); err != nil {
            return fmt.Errorf("uring read failed: %w", err)
        }
        handle(buf[:int(cqe.Res)])
        return nil
    }
}
```

`Wait` flushes pending submissions, then reaps completions. On single-issuer rings it also issues the kernel enter that keeps deferred task work moving once the SQ drains; the caller must serialize `Wait`, `WaitDirect`, and `WaitExtended` with other submit-state operations. If `iox.Classify(err)` yields `iox.OutcomeWouldBlock`, no completion is currently observable at the boundary.

`Start` and `Stop` form the ring lifecycle pair. `Stop` is idempotent and renders the ring permanently unusable; call it only after you have drained all in-flight operations, reaped outstanding CQEs, and quiesced live multishot subscriptions.

## Types and operations

| Type | Role |
|------|------|
| `Uring` | Ring setup, submission, completion reaping, and operation methods |
| `Options` | Ring entries, registered-buffer budget, buffer-group scale, and completion visibility |
| `SQEContext` | Compact submission identity stored in `user_data` |
| `CQEView` | Copied completion descriptor with decoded accessors and mode-sensitive borrowed targets |
| `ListenerOp` | Handle to a listener creation operation with FD and accept helpers |
| `BundleIterator` | Iterates over buffers consumed in a bundle receive |
| `IncrementalReceiver` | Manages incremental buffer-ring receives (`IOU_PBUF_RING_INC`) |
| `BufferRing` | Opaque explicitly registered provided-buffer ring; `Add` stages and `Advance` publishes |
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
| Socket I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
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

`Uring.Close` submits `IORING_OP_CLOSE` for a target file descriptor. It is not a ring teardown method.

`PollAddLevel` and `PollAddMultishotLevel` always return `ErrNotSupported`; Linux io_uring does not admit the level flag for `IORING_OP_POLL_ADD`.

## Context transport

`Multicast` automatically selects ordinary or zero-copy sends by payload size and target count; `MulticastZeroCopy` uses lower thresholds to favor zero-copy. Both require a Direct context and return `(submitted, error)`: `submitted` counts the accepted target prefix. Resume with the remaining targets after backpressure. Inspect each CQE's opcode and flags because a resumed call can select a different operation. Ordinary `Multicast` sends follow `NotifySucceed`; selected zero-copy sends and both `MulticastZeroCopy` paths retain completion evidence and reject explicit `IOSQE_CQE_SKIP_SUCCESS`.

Each accepted stream send may complete with fewer bytes than requested. Keep success CQEs enabled when tracking ordinary-send byte counts, and use each target's data result to resume only its unsent suffix. Keep shared zero-copy bytes immutable until every accepted target reaches its release condition. The [multicast example](./examples/zerocopy_test.go) demonstrates both byte progress and release tracking.

`SQEContext` is the primary identity token. In direct mode it packs the opcode, SQE flags, buffer-group ID, and file descriptor into a single 64-bit value.

```go
sqeCtx := uring.ForFD(fd).
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

There is no separate completion-context type. All completion dispatch goes through `CQEView`; call `cqe.Context()` to recover the original submission token.

```go
cqes := make([]uring.CQEView, 64)

n, err := ring.Wait(cqes)
switch iox.Classify(err) {
case iox.OutcomeWouldBlock:
    return iox.ErrWouldBlock
case iox.OutcomeFailure:
    return err
}
if n == 0 {
    return iox.ErrWouldBlock
}

for i := 0; i < n; i++ {
    cqe := cqes[i]
    if err := cqe.Err(); err != nil {
        return fmt.Errorf("completion failed: op=%d fd=%d: %w", cqe.Op(), cqe.FD(), err)
    }

    switch cqe.Op() {
    case uring.IORING_OP_ACCEPT:
        fmt.Printf("accepted fd=%d\n", cqe.Res)
    case uring.IORING_OP_RECV:
        if cqe.HasBuffer() {
            fmt.Printf("buffer id=%d\n", cqe.BufID())
        }
        if cqe.Extended() {
            seq := uring.CtxV1Of(cqe.ExtSQE()).Val1
            fmt.Printf("request seq=%d\n", seq)
        }
    }
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
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
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

Fixed-buffer I/O uses a registered buffer by index. The returned slice is ring-owned memory; keep it live until the fixed operation completes:

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.NewFD(int(file.Fd()))
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

Bundle receives use the same provided-buffer storage but may consume more than one buffer in a single CQE. Process the iterator, then recycle the consumed slots. Call `Recycle` exactly once across all copies of an iterator; after it returns, do not read the yielded buffers:

```go
if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
    return err
}

if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
    for buf := range it.All() {
        handle(buf)
    }
    if err := it.Recycle(ring); err != nil {
        return err
    }
}
```

Registered buffers require pinned memory. If large buffer registration fails, increase `RLIMIT_MEMLOCK` or use a smaller memory budget.

## Multishot and listener operations

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, and `SubmitReceiveBundleMultishot` each submit a multishot socket operation.

CQE routing policy stays outside the package. Listener setup progresses through `DecodeListenerCQE`, `PrepareListenerBind`, `PrepareListenerListen`, and `SetListenerReady`; the caller decides how to dispatch completions and when to stop the chain.

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

In the default task-migratable mode, shared submission-state synchronization allows a goroutine to move between OS threads. A ring-owner loop may still serialize all submit-state operations: it submits pending work, applies caller-owned `iox.Backoff` when `Wait` reports no observable progress, and dispatches completions:

```go
func runLoop(ring *uring.Uring, stop <-chan struct{}) error {
    cqes := make([]uring.CQEView, 64)
    var backoff iox.Backoff
    for {
        select {
        case <-stop:
            return nil
        default:
        }

        n, err := ring.Wait(cqes)
        switch iox.Classify(err) {
        case iox.OutcomeWouldBlock:
            backoff.Wait()
            continue
        case iox.OutcomeFailure:
            return err
        }
        if n == 0 {
            backoff.Wait()
            continue
        }

        backoff.Reset()
        for i := range n {
            dispatch(ring, cqes[i])
        }
    }
}
```

All ring methods, including `Send`, `Receive`, `AcceptMultishot`, and `Wait`, run on this goroutine. Work from other goroutines should enter the loop through a caller-owned mailbox such as `code.hybscloud.com/lfq`, not by calling ring methods directly. `iox.Backoff` stays caller-owned: call `backoff.Wait()` on `iox.OutcomeWouldBlock` or when `Wait` returns no CQEs, and `backoff.Reset()` after any batch with `n > 0`.

### Multishot subscription lifecycle

A multishot operation produces a stream of CQEs until the kernel sends a final one (without `IORING_CQE_F_MORE`). Caller-side code routes each CQE through the returned subscription in the same serialized completion loop before falling back to the rest of the dispatcher:

```go
handler := uring.NewMultishotSubscriber().
    OnStep(func(step uring.MultishotStep) uring.MultishotAction {
        if step.Err != nil {
            return uring.MultishotStop
        }
        connFD := iofd.FD(step.CQE.Res)
        registerConnection(connFD)
        return uring.MultishotContinue
    }).
    OnStop(func(err error, cancelled bool) {
        if !cancelled {
            resubscribeAccept()
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

Each `OnStep` callback observes one `MultishotStep`: return `MultishotContinue` to keep the stream live, or `MultishotStop` to request cancellation. If callbacks remain enabled until the terminal observation, `OnStop` runs at most once with the final error and a `cancelled` flag; on the step itself, `step.Cancelled` distinguishes the specific `-ECANCELED` kernel verdict from other failures at the boundary. Both callbacks are the builder-side projection of the `MultishotHandler` interface (`OnMultishotStep` / `OnMultishotStop`); implement that interface directly when you want an explicit handler. `HandleCQE` is for immediate dispatch in the caller's serialized completion loop; if caller code stores copied CQEs beyond that loop, it must keep its own route state and reject observations for retired subscriptions. On explicit single-issuer rings (`MultiIssuers=false`), call `Cancel` / `Unsubscribe` from the OS-thread-pinned ring owner and serialize them with submit, `Wait`, `WaitDirect`, `WaitExtended`, `Stop`, and `ResizeRings`. Default `MultiIssuers` rings serialize cancel SQEs through the shared-submit path.

Route every queued cancellation CQE through the same subscription's `HandleCQE` or its extended handler, even after `SubscriptionStopped` or `OnStop`. Failed submission or a no-op creates no new cancellation CQE obligation. The subscription owns both contexts and returns the original to its pool only after original terminality, cancellation dispatch, and cancellation submission have finished. Do not return these contexts manually. The [receive example](./examples/multishot_test.go) demonstrates natural completion and handler-requested cancellation.

### Per-connection state with typed contexts

Extended contexts carry per-connection references through the submit → complete round-trip without a global lookup table:

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

At completion time, recover the state through the same typed view:

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

Keep live Go pointer roots reachable outside `UserData`. The GC does not trace those raw bytes. The sidecar root set attached to each `ExtSQE` slot handles this for internal multishot and listener protocols, but caller-side runtime code that places typed refs must keep them reachable independently.

### Deadline composition

`LinkTimeout` attaches a deadline to the preceding SQE through an `IOSQE_IO_LINK` chain. Expiry attempts to cancel the linked operation, which may already have completed. Inspect both CQE results; a timeout does not prove that the operation was cancelled.

```go
recvCtx := uring.ForFD(fd).
    WithOp(uring.IORING_OP_RECV).
    WithBufGroup(group)

if err := ring.Receive(recvCtx, &fd, nil, uring.WithFlags(uring.IOSQE_IO_LINK)); err != nil {
    return err
}

timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 0, 0)
if err := ring.LinkTimeout(timeoutCtx, 5*time.Second); err != nil {
    return err
}
```

Submit the linked SQEs contiguously, serializing the pair with other submit and wait calls. Keep the operation's buffers and context live until its own completion, and drain both CQEs before teardown.

## TCP usage patterns

These are the shortest flows, meant to be read alongside the tests:

| Scenario    | Main APIs                                                        | Reference                                                                         |
|-------------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo server | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client      | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP echo server

`ListenerManager` prepares the socket → bind → listen chain for you. The listener handler's bool-return callbacks are control-flow hooks: `true` advances to the next setup phase, `false` aborts before it. Once the listener is live, start multishot accept and multishot receive on the connection FDs.

```go
pool := uring.NewContextPools(32)
manager := uring.NewListenerManager(ring, pool)

listenerOp, err := manager.ListenTCP4(addr, 128, listenerHandler)
if err != nil {
    return err
}

acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
if err != nil {
    return err
}
defer acceptSub.Cancel()

recvCtx := uring.ForFD(clientFD).WithBufGroup(readGroup)
recvSub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
if err != nil {
    return err
}
defer recvSub.Cancel()
```

`listener_example_test.go` covers listener setup with multishot accept, `examples/multishot_test.go` covers handler-side multishot receive CQEs, and `examples/echo_test.go` covers the full loopback echo flow.

### TCP client

Create a socket, wait for the `IORING_OP_SOCKET` completion, then wrap the returned FD in an `iofd.FD` for `Connect`, `Send`, and `Receive`.

```go
clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
if err := ring.TCP4Socket(clientCtx); err != nil {
    return err
}

clientFD := iofd.NewFD(int(socketCQE.Res))

connectCtx := uring.PackDirect(uring.IORING_OP_CONNECT, 0, 0, int32(clientFD))
if err := ring.Connect(connectCtx, remoteAddr); err != nil {
    return err
}

sendCtx := uring.PackDirect(uring.IORING_OP_SEND, 0, 0, int32(clientFD))
if err := ring.Send(sendCtx, &clientFD, payload); err != nil {
    return err
}

recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, int32(clientFD))
if err := ring.Receive(recvCtx, &clientFD, buf); err != nil {
    return err
}
```

After each submit, reuse the `Wait` loop from the ring lifecycle section to observe the matching completion. `socket_integration_linux_test.go` at the package level covers the connect/send cycle.

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

The example tests in `uring/examples/` show the API in practice.

- `multishot_test.go`, multishot accept, multishot receive, and subscription stop behavior
- `file_io_test.go`, basic file reads, writes, and batching
- `fixed_buffers_test.go`, registered buffers and fixed-buffer I/O
- `vectored_io_test.go`, vectored read and write operations
- `splice_tee_test.go`, splice and tee zero-copy data transfer
- `zerocopy_test.go`, zero-copy send paths and completion tracking
- `poll_test.go`, poll-based readiness workflows
- `buffer_ring_test.go`, buffer ring provisioning and multi-size buffer groups
- `context_test.go`, direct, indirect, and extended `SQEContext` flows plus `CQEView` access
- `echo_test.go`, TCP echo server and UDP ping-pong flows
- `timeout_linux_test.go`, timeout and linked-timeout operations

The package-level `listener_example_test.go` covers listener creation with multishot accept, and `socket_integration_linux_test.go` covers the TCP client connect/send flow.

## Operational notes

- Enable `NotifySucceed` when you need a visible CQE for every successful operation.
- `ring.Features` reports actual SQ/CQ entry counts, SQE slot width, and the byte order used to interpret `user_data`.
- Keep the default `MultiIssuers=true` task-migratable configuration (`COOP_TASKRUN`) for ordinary Go execution and concurrent submit-state calls. Set `MultiIssuers=false` only when the owner calls `runtime.LockOSThread` before `New`, remains pinned through `Stop`, and serializes `submit`, `Wait`, `WaitDirect`, `WaitExtended`, `Stop`, and `ResizeRings`; that explicit fast path requests `SINGLE_ISSUER` + `DEFER_TASKRUN`.
- `EpollWait` requires `timeout` to remain `0`; use `LinkTimeout` when you need a deadline.
- Return borrowed completion targets and pooled contexts promptly.
- `ListenerOp.Close` closes the listener FD immediately. If a setup CQE is still pending, drain it first, then call `Close` again to return the borrowed `ExtSQE` to the pool.

## Platform support

`uring` targets Go 1.26+ and Linux 6.18+ on the real kernel-backed path. Most source files and example tests carry a `//go:build linux` guard. Darwin files provide compile stubs for the shared surface only; Linux-only capabilities remain Linux-only and do not change the Linux runtime baseline.

## License

MIT, see [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
