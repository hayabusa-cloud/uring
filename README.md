# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go package for the kernel-facing `io_uring` boundary on Linux 6.18+.

Language: **English** | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Overview

`uring` is the kernel-facing boundary for Linux `io_uring`. It creates and starts rings, prepares SQEs, decodes CQEs,
carries submission identity through `user_data`, and exposes buffer registration, multishot operations, and
listener-setup primitives without turning them into a scheduler.

The package keeps the boundary explicit: kernel mechanics and observable completion facts live here; policy and
composition live above it. Caller-side runtime code owns completion correlation, retry/backoff, handler and session
routing, connection lifecycle, and terminal resource release.

The primary surfaces are:

- `Uring`, the live ring handle and operation set
- `SQEContext`, the submission identity carried in `user_data`
- `CQEView`, the borrowed completion view returned by `Wait`
- buffer provisioning through registered buffers and multi-size buffer groups

## Installation

`uring` requires Linux kernel 6.18 or later. Check the running kernel first:

```bash
uname -r
```

`uring` assumes the 6.18+ baseline and carries no fallback branches for older kernels. Boot a supported kernel instead
of expecting compatibility shims inside this package.

Debian 13's stable kernel track may still be below 6.18. See [Debian 13 kernel upgrade](#debian-13-kernel-upgrade) for
the backports path to a kernel that meets the requirement.

```bash
go get code.hybscloud.com/uring
```

### Debian 13 kernel upgrade

Debian 13 ships kernel 6.12 in its stable track. The `trixie-backports` suite provides a Debian-packaged 6.18+ kernel.
See [SETUP.md](./SETUP.md) for step-by-step instructions.

### Troubleshooting

Ring creation may return `ENOMEM`, `EPERM`, or `ENOSYS` depending on memlock limits, sysctl settings, or kernel support.
Container runtimes block `io_uring` syscalls by default. See [SETUP.md](./SETUP.md) for diagnosis and resolution.

## Ring lifecycle

`New` returns an unstarted ring and eagerly constructs the context pools. Call `Start` before submitting operations; it
registers ring resources and enables the ring. The example below submits a read, waits for the matching CQE, and uses
`iox.Classify` so `ErrWouldBlock` stays a semantic no-progress result rather than a failure.

```go
ring, err := uring.New(func(o *uring.Options) {
    o.Entries = uring.EntriesMedium
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

`Start` and `Stop` form the ring lifecycle pair. `Stop` is idempotent and renders the ring permanently unusable; call it
only after you have drained all in-flight operations, reaped outstanding CQEs, and quiesced live multishot
subscriptions.

## Types and operations

| Type | Role |
|------|------|
| `Uring` | Ring setup, submission, completion reaping, and operation methods |
| `Options` | Ring entries, registered-buffer budget, buffer-group scale, and completion visibility |
| `SQEContext` | Compact submission identity stored in `user_data` |
| `CQEView` | Borrowed completion record with decoded context accessors |
| `ListenerOp` | Handle to a listener creation operation with FD and accept helpers |
| `BundleIterator` | Iterates over buffers consumed in a bundle receive |
| `IncrementalReceiver` | Manages incremental buffer-ring receives (`IOU_PBUF_RING_INC`) |
| `ZCTracker` | Tracks the two-CQE zero-copy send lifecycle |
| `ContextPools` | Pools for indirect and extended submission contexts |
| `ZCRXReceiver` | Zero-copy receive lifecycle over a NIC RX queue |
| `ZCRXConfig` | Configuration for a ZCRX receive instance |
| `ZCRXHandler` | Callback interface for ZCRX data, errors, and shutdown |
| `ZCRXBuffer` | Delivered zero-copy receive view with kernel refill on release |

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
| Cmd | `UringCmd`, `UringCmd128`, `Nop`, `Nop128` |

`Nop128` and `UringCmd128` require a ring created with `Options.SQE128` and kernel support for the corresponding
opcodes. Without both, they return `ErrNotSupported`.

`Uring.Close` submits `IORING_OP_CLOSE` for a target file descriptor. It is not a ring teardown method.

## Context transport

`SQEContext` is the primary identity token. In direct mode it packs the opcode, SQE flags, buffer-group ID, and file
descriptor into a single 64-bit value.

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

For the common path, start with `ForFD` or `PackDirect` and attach only the bits you need to see again at completion
time. `WithFlags` replaces the entire flag set, so compute unions before calling it.

When you need caller-owned metadata beyond the 64-bit direct layout, borrow an `ExtSQE`, write into its `UserData`
through `Ctx*Of` or `ViewCtx*`, and pack it back into an `SQEContext`. Prefer scalar payloads. If a raw overlay or typed
view stores Go pointers, interfaces, func values, slices, strings, maps, chans, or structs containing them, keep the
live roots outside `UserData`; the GC does not trace those raw bytes.

```go
ext := ring.ExtSQE()
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
```

`NewContextPools` returns pools that are ready to use. Call `Reset` only once all borrowed contexts have been returned
and you want to reuse the pool set.

### Completion dispatch with `CQEView`

There is no separate completion-context type. All completion dispatch goes through `CQEView`; call `cqe.Context()` to
recover the original submission token.

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

`CQEView` decodes the matching context mode on demand at completion time. `CQEView`, `IndirectSQE`, `ExtSQE`, and
borrowed buffers must not outlive their documented lifetimes.

## Buffer provisioning

`uring` has three practical buffer paths. Registered buffers are pinned during ring setup and used by fixed-buffer file
I/O. Provided buffer rings let the kernel choose a receive buffer and report the selected buffer ID in the CQE. Bundle
receives consume a contiguous logical range of provided buffers and expose that range through `BundleIterator`.

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

Use `OptionsForBudget` to start from an explicit memory budget, or `BufferConfigForBudget` to inspect the tier layout
chosen for a given budget:

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

Fixed-buffer I/O uses a registered buffer by index. The returned slice is ring-owned memory; keep it live until the
fixed operation completes:

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.NewFD(int(file.Fd()))
ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, 0).WithFD(fd)
if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
    return err
}
```

For socket receive with kernel buffer selection, pass `nil` as the receive buffer and request the size class you want.
The completion reports which buffer was selected:

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

Bundle receives use the same provided-buffer storage but may consume more than one buffer in a single CQE. Process the
iterator, then recycle the consumed slots:

```go
if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
    return err
}

if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
    for buf := range it.All() {
        handle(buf)
    }
    it.Recycle(ring)
}
```

Registered buffers require pinned memory. If large buffer registration fails, increase `RLIMIT_MEMLOCK` or use a smaller memory budget.

## Multishot and listener operations

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`,
and `SubmitReceiveBundleMultishot` each submit a multishot socket operation.

CQE routing policy stays outside the package. Listener setup progresses through `DecodeListenerCQE`,
`PrepareListenerBind`, `PrepareListenerListen`, and `SetListenerReady`; the caller decides how to dispatch completions
and when to stop the chain.

## Architecture implementation

The implementation sits at this boundary:

1. `New` builds a disabled kernel ring, constructs context pools, and selects a buffer strategy.
2. `Start` registers buffers and enables the ring for the 6.18+ baseline.
3. Operation methods express intent by writing SQEs.
4. `Wait` flushes submissions and returns borrowed CQE views.
5. Caller-side runtime code decides scheduling, retries, parking, connection/session routing, and terminal resource
   policy.

This keeps `uring` focused on kernel-facing mechanics and preserves completion meaning across the boundary.

## Runtime boundary

Runtime layers above `uring` should use it as the kernel backend, not as a scheduler. The ideal seam is one-way: `uring`
prepares SQEs, reaps CQEs, preserves `user_data`, exposes CQE `res` and flags, and reports ownership facts; caller-side
runtime code correlates those observations with its own tokens, applies retry/backoff, routes handlers and sessions,
batches submissions, and releases terminal resources.

A runtime bridge can consume Extended-mode CQEs when abstract execution needs completion facts. A connection-scoped
runtime can also poll raw Extended CQEs directly when it needs the CQE result, flags, buffer ID, and encoded token
before reducing the event to handler callbacks.

Context and abstract-execution layers above this boundary do not change `uring`'s kernel-boundary role.

## Application-layer patterns

`uring` exposes kernel mechanics; scheduling, retry, connection tracking, and protocol interpretation belong in the
layers above it. The patterns below describe the boundary a caller-side runtime must preserve.

### Ring-owning event loop

In single-issuer mode (the default), one goroutine serializes all submit-state operations. A typical loop submits
pending work, applies caller-owned `iox.Backoff` when `Wait` reports no observable progress, and dispatches completions:

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

All ring methods, including `Send`, `Receive`, `AcceptMultishot`, and `Wait`, run on this goroutine. Work from other
goroutines enters the loop through a channel or a lock-free queue, not by calling ring methods directly. `iox.Backoff`
stays caller-owned: call `backoff.Wait()` on `iox.OutcomeWouldBlock` or when `Wait` returns no CQEs, and
`backoff.Reset()` after any batch with `n > 0`.

### Multishot subscription lifecycle

A multishot operation produces a stream of CQEs until the kernel sends a final one (without `IORING_CQE_F_MORE`).
Caller-side runtime code routes each CQE through the returned subscription before falling back to the rest of the dispatcher:

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

for i := range n {
    if sub.HandleCQE(cqes[i]) {
        continue
    }
    dispatch(ring, cqes[i])
}
```

`OnMultishotStep` observes each completion; return `MultishotContinue` to keep the stream or `MultishotStop` to request cancellation. `OnMultishotStop` runs once at the terminal state. Use it for cleanup and conditional resubscription. On default single-issuer rings, call `Cancel` / `Unsubscribe` from the ring owner or otherwise serialize them with submit, `Wait`, `WaitDirect`, `WaitExtended`, `Stop`, and resize operations. On `MultiIssuers` rings, the shared-submit path serializes their cancel SQEs.

### Per-connection state with typed contexts

Extended contexts carry per-connection references through the submit → complete round-trip without a global lookup
table:

```go
type ConnState struct {
    Addr    netip.AddrPort
    Created int64
}

ext := ring.ExtSQE()
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

Keep live Go pointer roots reachable outside `UserData`. The GC does not trace those raw bytes. The sidecar root set
attached to each `ExtSQE` slot handles this for internal multishot and listener protocols, but caller-side runtime code
that places typed refs must keep them reachable independently.

### Deadline composition

`LinkTimeout` attaches a deadline to the preceding SQE through an `IOSQE_IO_LINK` chain. The operation and the timeout
race: exactly one completes, and the other is cancelled.

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

The caller-side runtime handles both outcomes: a successful receive cancels the timeout, and a fired timeout cancels the
receive. Both produce CQEs that the dispatch loop must observe.

## TCP usage patterns

These are the shortest flows, meant to be read alongside the tests:

| Scenario    | Main APIs                                                        | Reference                                                                         |
|-------------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo server | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client      | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP echo server

`ListenerManager` prepares the socket → bind → listen chain for you. The listener handler's bool-return callbacks are
control-flow hooks: `true` advances to the next setup phase, `false` aborts before it. Once the listener is live, start
multishot accept and multishot receive on the connection FDs.

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

`listener_example_test.go` covers listener setup with multishot accept, `examples/multishot_test.go` covers handler-side
multishot receive CQEs, and `examples/echo_test.go` covers the full loopback echo flow.

### TCP client

Create a socket, wait for the `IORING_OP_SOCKET` completion, then wrap the returned FD in an `iofd.FD` for `Connect`,
`Send`, and `Receive`.

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

After each submit, reuse the `Wait` loop from the ring lifecycle section to observe the matching completion.
`socket_integration_linux_test.go` at the package level covers the connect/send cycle.

## Zero-copy receive (ZCRX)

`ZCRXReceiver` drives zero-copy receive from a NIC hardware RX queue through `io_uring`.

`NewZCRXReceiver` is wired for rings with 32-byte CQEs (`IORING_SETUP_CQE32`). The current `Options` surface does not
expose that setup flag, so rings created through the standard `New` path cause this constructor to return
`ErrNotSupported`. Until a CQE32 setup path is exposed, this section documents the receiver boundary contract rather
than a runnable public setup recipe.

### Lifecycle

1. With a CQE32-enabled ring, create the receiver with `NewZCRXReceiver`. The constructor registers the ZCRX interface
   queue, maps the refill area, and prepares the refill ring.
2. Call `Start` to submit the extended `RECV_ZC` operation.
3. On the CQE dispatch path, ZCRX completions route to the `ZCRXHandler`:
    - `OnData` delivers a `ZCRXBuffer` pointing into the NIC-mapped area. Call `Release` when done to return the slot to
      the kernel. Return `false` to request a best-effort stop.
   - `OnError` delivers CQE errors. Return `false` to request a best-effort stop.
   - `OnStopped` fires once during terminal retirement, before the state reaches `Stopped`.
4. Call `Stop` to submit an async cancel. The receiver transitions through `Stopping` → `Retiring` → `Stopped`.
5. Poll `Stopped` until it returns `true`, stop the owning ring, then call `Close` to release the mapped area and the
   refill-ring mapping.

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

The package-level `listener_example_test.go` covers listener creation with multishot accept, and
`socket_integration_linux_test.go` covers the TCP client connect/send flow.

## Operational notes

- Enable `NotifySucceed` when you need a visible CQE for every successful operation.
- `ring.Features` reports actual SQ/CQ entry counts, SQE slot width, and the byte order used to interpret `user_data`.
- Leave `MultiIssuers` unset for the default single-issuer configuration (`SINGLE_ISSUER` + `DEFER_TASKRUN`) when a single execution path serializes submit-state operations (`submit`, `Wait`, `WaitDirect`, `WaitExtended`, `Stop`, and resize). Set it only when multiple goroutines need concurrent submission or concurrent calls to `Wait`, `WaitDirect`, or `WaitExtended`; this switches the ring to the shared-submit `COOP_TASKRUN` configuration.
- `EpollWait` requires `timeout` to remain `0`; use `LinkTimeout` when you need a deadline.
- Release or discard borrowed completion views and pooled contexts promptly.
- `ListenerOp.Close` closes the listener FD immediately. If a setup CQE is still pending, drain it first, then call
  `Close` again to return the borrowed `ExtSQE` to the pool.

## Platform support

`uring` targets Go 1.26+ and Linux 6.18+ on the real kernel-backed path. Most source files and example tests carry a
`//go:build linux` guard. Darwin files provide compile stubs for the shared surface only; Linux-only capabilities remain
Linux-only and do not change the Linux runtime baseline.

## License

MIT, see [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
