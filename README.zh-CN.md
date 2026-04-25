# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go `io_uring` 内核接口包，面向 Linux 6.18+。

语言: [English](./README.md) | **简体中文** | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## 概述

`uring` 负责与 Linux 内核的 `io_uring` 接口交互：创建并启动 ring、填充 SQE、解码 CQE，借助 `user_data`
传递提交标识，并提供缓冲区注册、multishot 操作及监听器初始化等原语。

设计原则是明确划分边界：内核侧的机制与可观测的完成事实留在 API 层面，调度策略与组合逻辑由上层负责。调用方运行时代码负责完成关联、retry/backoff、handler
与 session 路由、连接生命周期和终态资源释放。

核心类型：

- `Uring`：活跃 ring 的句柄及操作方法集
- `SQEContext`：通过 `user_data` 传递的提交标识
- `CQEView`：`Wait` 返回的借用式完成视图
- 缓冲区供给：通过注册缓冲区与多尺寸缓冲区组实现

## 安装

`uring` 要求 Linux 内核 6.18 或更高版本。先确认当前内核版本：

```bash
uname -r
```

Debian 13 的 stable 源中内核版本可能低于此要求。如需升级，请参见下方 Debian 13 内核升级一节。

```bash
go get code.hybscloud.com/uring
```

### Debian 13 内核升级

Debian 13 稳定版提供的内核为 6.12。`trixie-backports` suite 提供经过 Debian 打包的 6.18+
内核。详细步骤见 [SETUP.md](./SETUP.md)。

### 常见问题排查

Ring 创建可能返回 `ENOMEM`、`EPERM` 或 `ENOSYS`，原因分别涉及 memlock 上限、sysctl 配置或内核支持情况。容器运行时默认阻止
`io_uring` 系统调用。诊断与解决方法见 [SETUP.md](./SETUP.md)。

## Ring 生命周期

`New` 返回一个未启动的 ring，提交操作前须先调用 `Start`。`New` 会预先构建上下文池，`Start` 则负责注册资源并启用 ring。
`uring` 固定以 Linux 6.18+ 为基线，启动路径中不包含低版本回退逻辑。

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

cqes := make([]uring.CQEView, 64)
n, err := ring.Wait(cqes)
if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
    return err
}

for i := range n {
    cqe := cqes[i]
    if cqe.Res < 0 {
        return fmt.Errorf("completion failed: op=%d fd=%d res=%d", cqe.Op(), cqe.FD(), cqe.Res)
    }
    fmt.Printf("completed op=%d on fd=%d with res=%d\n", cqe.Op(), cqe.FD(), cqe.Res)
}
```

`Wait` 先刷新待提交项，再回收完成事件。在单提交者 ring 上，它还会在 SQ 排空后向内核发起 enter 调用以推进 deferred task
work；调用方需保证 `Wait`/`enter` 与 submit-state 操作的串行执行。`iox.ErrWouldBlock` 表示当前没有可回收的完成事件，该错误定义在
`code.hybscloud.com/iox` 中。

`Start` 与 `Stop` 是 ring 生命周期的配对操作。`Stop` 幂等但不可逆，调用后 ring 将永久不可用。调用 `Stop` 前，需确保所有
in-flight 操作已完成、未处理的 CQE 已回收、活跃的 multishot 订阅已终止。

## 类型与操作

| 类型                    | 作用                               |
|-----------------------|----------------------------------|
| `Uring`               | Ring 的初始化、提交、完成回收及操作方法           |
| `Options`             | Ring 条目数、注册缓冲区预算、缓冲区组配置及完成事件可见性  |
| `SQEContext`          | 存储于 `user_data` 的紧凑提交标识          |
| `CQEView`             | 借用式完成记录，提供上下文解码访问器               |
| `ListenerOp`          | 监听器创建操作的句柄，持有 FD 并提供 accept 辅助方法 |
| `BundleIterator`      | 遍历 bundle 接收中消耗的缓冲区              |
| `IncrementalReceiver` | 管理增量缓冲区环接收（`IOU_PBUF_RING_INC`）  |
| `ZCTracker`           | 跟踪零拷贝发送的双 CQE 生命周期               |
| `ContextPools`        | Indirect 与 extended 提交上下文的对象池    |
| `ZCRXReceiver`        | NIC RX 队列零拷贝接收的生命周期管理            |
| `ZCRXConfig`          | ZCRX 接收实例的配置                     |
| `ZCRXHandler`         | ZCRX 数据、错误及关闭的回调接口               |
| `ZCRXBuffer`          | 已交付的零拷贝接收视图，释放时内核自动回填            |

操作一览：

| 领域 | 方法 |
|------|------|
| 套接字 | `TCP4Socket`, `TCP6Socket`, `UDP4Socket`, `UDP6Socket`, `UDPLITE4Socket`, `UDPLITE6Socket`, `SCTP4Socket`, `SCTP6Socket`, `UnixSocket`, `SocketRaw`，以及 `*Direct` 变体 |
| 连接 | `Bind`, `Listen`, `Accept`, `AcceptDirect`, `Connect`, `Shutdown` |
| 套接字 I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
| 多次触发 | `AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, `SubmitReceiveBundleMultishot` |
| 文件 I/O | `Read`, `Write`, `ReadV`, `WriteV`, `ReadFixed`, `WriteFixed`, `ReadvFixed`, `WritevFixed` |
| 文件管理 | `OpenAt`, `Close`, `Sync`, `Fallocate`, `FTruncate`, `Statx`, `RenameAt`, `UnlinkAt`, `MkdirAt`, `SymlinkAt`, `LinkAt` |
| 扩展属性 | `FGetXattr`, `FSetXattr`, `GetXattr`, `SetXattr` |
| 数据转移 | `Splice`, `Tee`, `Pipe`, `SyncFileRange`, `FileAdvise` |
| 超时 | `Timeout`, `TimeoutRemove`, `TimeoutUpdate`, `LinkTimeout` |
| 取消 | `AsyncCancel`, `AsyncCancelFD`, `AsyncCancelOpcode`, `AsyncCancelAny`, `AsyncCancelAll` |
| 轮询 | `PollAdd`, `PollRemove`, `PollUpdate`, `PollAddLevel`, `PollAddMultishot`, `PollAddMultishotLevel` |
| 异步 | `EpollWait`, `FutexWait`, `FutexWake`, `FutexWaitV`, `Waitid` |
| 环消息 | `MsgRing`, `MsgRingFD`, `FixedFdInstall`, `FilesUpdate` |
| 命令 | `UringCmd`, `UringCmd128`, `Nop`, `Nop128` |

`Nop128` 和 `UringCmd128` 需要以 `Options.SQE128` 创建的 ring，且内核须声明支持相应 opcode，否则返回 `ErrNotSupported`。

`Uring.Close` 提交的是针对目标文件描述符的 `IORING_OP_CLOSE` 操作，而非 ring 本身的销毁方法。

## 上下文传递

`SQEContext` 是 `uring` 的核心标识令牌。Direct 模式将 opcode、SQE flags、buffer-group ID 和文件描述符打包为一个 64 位值。

```go
sqeCtx := uring.ForFD(fd).
    WithOp(uring.IORING_OP_RECV).
    WithBufGroup(groupID)
```

上下文有三种模式：

| 模式 | 表示方式 | 典型用途 |
|------|----------|----------|
| Direct | 内联 64 位负载 | 常见提交与回收路径，零分配 |
| Indirect | 指向 `IndirectSQE` 的指针 | 64 位不足以表达完整 SQE 负载时 |
| Extended | 指向 `ExtSQE` 的指针 | 完整 SQE 加 64 字节用户数据 |

常见路径下，从 `ForFD` 或 `PackDirect` 开始，只填入完成时需要回溯的信息。`WithFlags` 会整体替换 flag 集合，调用前应先计算好联合值。

当 direct 的 64 位布局不足以携带所需元数据时，可从池中借用 `ExtSQE`，通过 `Ctx*Of` 或 `ViewCtx*` 写入 `UserData`，再打包为
`SQEContext`。此处应优先使用标量 payload。若通过 raw overlay 或 typed view 存放了 Go 指针、interface、func
value、slice、string、map、chan 或含有这些成员的 struct，必须将 live root 保留在 `UserData` 之外，因为 GC 不会扫描这段原始字节。

```go
ext := ring.ExtSQE()
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
```

`NewContextPools` 返回开箱即用的对象池。仅在所有借出的上下文已归还、且需要复用该池时，才调用 `Reset`。

### 通过 `CQEView` 分发完成事件

`uring` 没有独立的 completion-context 类型，完成分发统一通过 `CQEView` 进行。需要原始提交令牌时调用 `cqe.Context()`。

```go
cqes := make([]uring.CQEView, 64)

n, err := ring.Wait(cqes)
if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
    return err
}

for i := 0; i < n; i++ {
    cqe := cqes[i]
    if cqe.Res < 0 {
        return fmt.Errorf("completion failed: op=%d fd=%d res=%d", cqe.Op(), cqe.FD(), cqe.Res)
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

完成时 `CQEView` 按需解码相应的上下文模式。`CQEView`、`IndirectSQE`、`ExtSQE` 及借用缓冲区均不得超出文档约定的生命周期。

## 缓冲区供给

`uring` 提供两种接收缓冲区策略：

- 通过 `ReadBufferSize` 与 `ReadBufferNum` 配置固定尺寸提供缓冲区
- 通过 `MultiSizeBuffer` 启用多尺寸缓冲区组

多数场景下，直接使用配置辅助函数即可：

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
```

需要从显式内存预算出发时使用 `OptionsForBudget`；需要查看某预算对应的分层布局时使用 `BufferConfigForBudget`。

注册缓冲区需要锁定内存。若大规模注册失败，可提高 `RLIMIT_MEMLOCK` 或缩减预算。

## Multishot 与监听器操作

`AcceptMultishot`、`ReceiveMultishot`、`SubmitAcceptMultishot`、`SubmitAcceptDirectMultishot`、`SubmitReceiveMultishot` 和
`SubmitReceiveBundleMultishot` 用于提交 multishot socket 操作。

CQE 路由策略由调用方自行实现，不在本包范围内。监听器的配置流程通过 `DecodeListenerCQE`、`PrepareListenerBind`、
`PrepareListenerListen` 和 `SetListenerReady` 逐步推进，由调用方决定完成事件的分发方式和停止时机。

## 架构实现

实现层次如下：

1. `New` 创建处于禁用状态的内核 ring，构造上下文池，选定缓冲区策略。
2. `Start` 注册缓冲区并启用 ring（固定 Linux 6.18+ 基线）。
3. 操作方法通过写入 SQE 表达提交意图。
4. `Wait` 刷新提交并返回借用式 CQE。
5. 调用方运行时代码负责调度、重试、挂起、连接/session 路由和终态资源策略。

如此分工使 `uring` 专注于内核侧机制，同时在边界上保持清晰的完成语义。

## 运行时边界

`uring` 之上的运行时层应将其用作内核后端，而不是调度器。理想边界是单向的：`uring` 准备 SQE、回收 CQE、保留 `user_data`、暴露
CQE `res` 与 flags，并报告所有权事实；调用方运行时代码将这些观测与自身 token 关联，应用 retry/backoff，路由 handler 与
session，批量提交，并释放终态资源。

当抽象执行需要完成事实时，运行时桥接层可以消费 Extended 模式 CQE。连接级运行时也可以在需要 CQE 结果、flags、buffer ID 和编码
token 时直接轮询 raw Extended CQE，然后再将事件归约为 handler callback。

位于该边界之上的上下文层与抽象执行层不会改变 `uring` 的内核边界职责。

## 应用层设计模式

`uring` 公开面向内核的机制；调度、重试、连接追踪和协议解析属于上层职责。以下模式描述调用方运行时必须保持的边界。

### Ring 所有者事件循环

在单发行者模式（默认）下，一个 goroutine 串行化所有 submit 侧操作。典型循环为：发行待处理工作，在 `Wait` 没有返回可观察进展时使用调用方持有的
`iox.Backoff`，然后分发完成事件。

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
        if errors.Is(err, iox.ErrWouldBlock) {
            backoff.Wait()
            continue
        }
        if err != nil {
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

所有 ring 方法，包括 `Send`、`Receive`、`AcceptMultishot` 和 `Wait`，均在该 goroutine 上执行。来自其他 goroutine 的工作通过
channel 或无锁队列进入循环，不可直接调用 ring 方法。`iox.Backoff` 由调用方持有：`Wait` 返回 `iox.ErrWouldBlock`，或一次
`Wait` 没有回收到任何 CQE 时，调用 `backoff.Wait()`；回收到 `n > 0` 的 CQE 批次后，调用 `backoff.Reset()`。

### Multishot 订阅生命周期

Multishot 操作产生 CQE 流，直到内核发送最终 CQE（不含 `IORING_CQE_F_MORE`）。调用方运行时代码负责追踪订阅状态并管理重新提交。

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

_, err := ring.AcceptMultishot(acceptCtx, handler.Handler())
```

`OnMultishotStep` 观察每次完成；返回 `MultishotContinue` 保持流，返回 `MultishotStop` 请求取消。`OnMultishotStop`
在终态执行一次，用于清理和按条件重新订阅。

### 类型化上下文承载连接状态

扩展上下文通过 submit → complete 往返全程携带连接级引用，无需全局查找表。

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

完成时通过同一类型化视图恢复状态：

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

活跃的 Go 指针根须在 `UserData` 之外保持可达。GC 不会追踪裸字节。内部 multishot 和 listener 协议由每个 `ExtSQE` 槽上的
sidecar 根集处理，但放置类型化 ref 的框架代码须自行维护可达性。

### 截止时间组合

`LinkTimeout` 通过 `IOSQE_IO_LINK` 链将截止时间附加到前一个 SQE。操作与超时竞争：一方完成，另一方被取消。

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

框架层处理两种结果：接收成功取消超时，超时触发取消接收。两者均产生 CQE，分发循环须观察处理。

## TCP 使用模式

以下是配合测试代码阅读的简化流程：

| 场景       | 主要 API                                                           | 参考                                                                                |
|----------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo 服务器 | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| 客户端      | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP Echo 服务器

使用 `ListenerManager` 可自动完成 socket → bind → listen 链路的准备工作，之后在活跃连接的 FD 上启动 multishot accept 和
multishot receive。

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

`listener_example_test.go` 演示监听器创建与 multishot accept；`examples/multishot_test.go` 演示 handler 侧的 multishot
receive CQE 处理流程；`examples/echo_test.go` 则给出更完整的 loopback echo 示例。

### TCP 客户端

先创建 socket 并等待 `IORING_OP_SOCKET` 完成，将返回的 FD 转为 `iofd.FD`，随后用于 `Connect`、`Send` 和 `Receive`。

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

每次提交后，可复用“Ring 生命周期”一节中的 `Wait` 循环来等待完成事件。包级 `socket_integration_linux_test.go` 覆盖了
connect/send 流程。

## 零拷贝接收（ZCRX）

`ZCRXReceiver` 管理通过 `io_uring` 从 NIC 硬件 RX 队列进行的零拷贝接收。

`NewZCRXReceiver` 面向以 32 字节 CQE（`IORING_SETUP_CQE32`）创建的 ring。当前 `Options` 尚未暴露该 setup flag，因此通过标准
`New` 创建的 ring 会使该构造器返回 `ErrNotSupported`。在 CQE32 setup 路径公开前，本节记录的是接收器边界契约，而不是可直接执行的公开设置流程。

### 生命周期

1. 在支持 CQE32 的 ring 上调用 `NewZCRXReceiver` 创建接收器。构造器会注册 ZCRX 接口队列、映射 refill 区域并准备 refill
   ring。
2. 调用 `Start`，在 ring 上提交扩展 `RECV_ZC` 操作。
3. CQE 分发时，ZCRX 完成事件路由至 `ZCRXHandler`：
   - `OnData` 交付指向 NIC 映射区域的 `ZCRXBuffer`。处理完毕后调用 `Release` 将槽位回填给内核。返回 `false` 请求尽力停止。
   - `OnError` 交付 CQE 错误。返回 `false` 请求尽力停止。
   - `OnStopped` 在进入 `Stopped` 前的终态退出阶段调用一次。
4. 调用 `Stop` 提交异步取消，接收器依次经历 `Stopping` → `Retiring` → `Stopped`。
5. 轮询 `Stopped` 直至返回 `true`，停止所属 ring，再调用 `Close` 释放映射区域和 refill ring。

### 状态机

```
Idle → Active → Stopping → Retiring → Stopped
```

取消提交失败时，`Stop` 回退至 `Active`。`Close` 幂等。

### Handler 契约

- `OnData` 与 `OnError` 在 CQE dispatch goroutine 中串行调用。
- `Release` 为单生产者操作，仅限在 dispatch goroutine 中调用。
- 调用 `Stop` 时须保证与 CQE dispatch 不并发，这是调用方侧的串行化约定。

## 示例

`uring/examples/` 下的示例测试覆盖了各主要 API 的用法：

- `multishot_test.go`，multishot accept、multishot receive 及订阅停止行为
- `file_io_test.go`，基本文件读写与批量提交
- `fixed_buffers_test.go`，注册缓冲区与固定缓冲区 I/O
- `vectored_io_test.go`，向量化读写操作
- `splice_tee_test.go`，splice 与 tee 零拷贝数据传输
- `zerocopy_test.go`，零拷贝发送路径与完成跟踪
- `poll_test.go`，基于 poll 的就绪通知
- `buffer_ring_test.go`，缓冲区环供给与多尺寸缓冲区组
- `context_test.go`，`SQEContext` 的 direct、indirect、extended 模式及 `CQEView` 访问
- `echo_test.go`，TCP echo 服务器与 UDP ping-pong 流程
- `timeout_linux_test.go`，超时与链式超时操作

包级 `listener_example_test.go` 演示监听器创建与 multishot accept，`socket_integration_linux_test.go` 演示 TCP 客户端
connect/send 流程。

## 注意事项

- 若需为每个成功操作生成可见的 CQE，启用 `NotifySucceed`。
- `ring.Features` 报告实际 SQ/CQ 条目数、SQE 槽宽以及本包解析 `user_data` 的字节序。
- 默认不启用 `MultiIssuers`，此时采用单提交者配置（`SINGLE_ISSUER` + `DEFER_TASKRUN`），由调用方的单一执行路径串行化
  submit-state 操作（`submit`、`Wait`/`enter`、`Stop` 及 resize）。仅当多个 goroutine 需并发提交或执行 wait 侧 enter 时才启用
  `MultiIssuers`，这会切换为共享提交的 `COOP_TASKRUN` 配置。
- `EpollWait` 要求 `timeout` 为 `0`；如需设置截止时间，使用 `LinkTimeout`。
- 借用式完成视图与池化上下文应及时释放。
- `ListenerOp.Close` 会立即关闭监听 FD。若仍有 setup CQE 待处理，需先回收该 CQE，再调用 `Close` 将借用的 `ExtSQE` 归还池中。

## 平台支持

`uring` 的真实内核路径目标为 Go 1.26+ / Linux 6.18+。大部分实现文件和示例测试由 `//go:build linux` 约束。Darwin 文件只为共享
API 表面提供编译 stub；Linux 专属能力仍然仅限 Linux，不改变上述 Linux 运行时基线。

## 许可证

MIT，参见 [LICENSE](./LICENSE)。

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
