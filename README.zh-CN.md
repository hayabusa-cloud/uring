# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go `io_uring` 内核接口包，面向 Linux 6.18+。

语言: [English](./README.md) | **简体中文** | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## 概述

`uring` 是 Linux `io_uring` 的内核侧边界：负责创建并启动 ring、填充 SQE、解码 CQE，借助 `user_data` 传递提交标识，并提供缓冲区注册、multishot 操作及监听器初始化等原语，本身并不充当调度器。

设计原则是明确划分边界：内核侧的机制与可观测的完成事实留在本 API 层面，调度策略与组合逻辑由调用方层负责。调用方运行时代码负责完成路由、路由退役、重试与退避、处理器与会话路由、连接生命周期、安全检查以及终态资源释放。

核心类型：

- `Uring`：活跃 ring 的句柄及操作方法集
- `SQEContext`：通过 `user_data` 传递的提交标识
- `CQEView`：`Wait` 返回的已复制完成描述符
- 缓冲区供给：通过注册缓冲区与多尺寸缓冲区组实现

## 安装

`uring` 要求 Linux 内核 6.18 或更高版本。先确认当前内核版本：

```bash
uname -r
```

`uring` 假定运行环境满足 6.18+ 基线，且不为旧内核保留任何回退分支。请直接启动满足要求的内核，而不是期待此包内部提供兼容分支。

Debian 13 的稳定源中内核版本可能仍低于 6.18。需要升级时请参见下文 [Debian 13 内核升级](#debian-13-内核升级) 一节。

```bash
go get code.hybscloud.com/uring
```

### Debian 13 内核升级

Debian 13 稳定版默认提供的内核为 6.12，`trixie-backports` 软件源提供经过 Debian 打包的 6.18+ 内核。详细步骤见 [SETUP.md](./SETUP.md)。

### 常见问题排查

Ring 创建可能返回 `ENOMEM`、`EPERM` 或 `ENOSYS`，分别对应 memlock 上限、sysctl 配置或内核支持等问题。[Docker 的默认 seccomp 配置](https://docs.docker.com/engine/security/seccomp/) 会阻止 `io_uring` 系统调用。诊断与解决方法见 [SETUP.md](./SETUP.md)。

## 调用方指南

使用 `uring` 构建或审查运行时、协议栈或服务时，请从 [`AGENTS.md`](./AGENTS.md) 开始阅读。[调用方指南](./agents/INDEX.md) 涵盖提交、完成处理、请求标识、缓冲区所有权、生命周期、包集成和验证。

`uring` 提供内核操作和完成结果。调用方负责轮询、调度、重试与退避、完成路由及其注销、协议状态与解析、取消和超时策略、安全检查以及服务生命周期。组合这些操作时，应保留 `ErrWouldBlock`、`ErrMore`、`IORING_CQE_F_MORE`、所有权转移和能力检查失败的含义。

## Ring 生命周期

`New` 返回尚未启动的 ring，并立即构建上下文池。在提交操作前调用 `Start`，以注册资源并启用 ring。下面的示例将一次读取的结果写入 ring 所有的注册内存，其地址在 `Stop` 前保持稳定。完成后先处理读取的数据，再复用缓冲区。`iox.IsWouldBlock` 表示当前没有进展；调用方使用 `iox.Backoff` 等待，兼顾 CPU 效率。

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

fd := iofd.FD(file.Fd()) // 借用的 FD；file 保留关闭责任。
buf := ring.RegisteredBuffer(0)
ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
if err := ring.Read(ctx, buf); err != nil {
	return err
}

var cqes [1]uring.CQEView // Only one request is outstanding.
var backoff iox.Backoff
for {
	n, err := ring.Wait(cqes[:])
	if err != nil && !iox.IsWouldBlock(err) {
		return err
	}
	if n == 0 {
		backoff.Wait()
		continue
	}

	backoff.Reset()
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
		return fmt.Errorf("unexpected completion: op=%d fd=%d", cqe.Op(), cqe.FD())
	}
	if err := cqe.Err(); err != nil {
		return fmt.Errorf("uring read failed: %w", err)
	}
	handle(buf[:int(cqe.Res)])
	return nil
}
```

`Wait` 先刷新待提交的 SQE，再回收完成事件。对于单一提交者 ring，它还会执行 kernel enter，使 SQ 排空后的延迟任务继续推进；调用方必须将 `Wait`、`WaitDirect`、`WaitExtended` 与其他提交状态操作串行执行。`iox.IsWouldBlock(err)` 表示当前没有可观察的完成事件。

`Start` 与 `Stop` 是 ring 生命周期的配对操作。`Stop` 幂等但不可逆，调用后 ring 将永久不可用；调用 `Stop` 前必须确保所有进行中的操作已完成、未处理的 CQE 已回收、活跃的 multishot 订阅已终止。

## 类型与操作

| 类型                    | 作用                               |
|-----------------------|----------------------------------|
| `Uring`               | Ring 的初始化、提交、完成回收及操作方法           |
| `Options`             | Ring 条目数、注册缓冲区预算、缓冲区组配置及完成事件可见性  |
| `SQEContext`          | 存储于 `user_data` 的紧凑提交标识          |
| `CQEView`             | 已复制完成描述符，提供解码访问器；模式相关目标仍为借用 |
| `ListenerOp`          | 监听器创建操作的句柄，持有 FD 并提供 accept 辅助方法 |
| `BundleIterator`      | 遍历 bundle 接收中消耗的缓冲区              |
| `BufferRing`          | 显式注册的不透明提供缓冲区环；`Add` 暂存，`Advance` 发布 |
| `IncrementalReceiver` | 管理增量缓冲区环接收（`IOU_PBUF_RING_INC`）  |
| `ZCTracker`           | 跟踪零拷贝发送的通知与回退生命周期              |
| `ContextPools`        | Indirect 与 extended 提交上下文的对象池    |
| `ZCRXReceiver`        | NIC RX 队列零拷贝接收的生命周期管理            |
| `ZCRXConfig`          | ZCRX 接收实例的配置                     |
| `ZCRXHandler`         | ZCRX 数据、错误及关闭的回调接口               |
| `ZCRXBuffer`          | 已交付的零拷贝接收视图，由调用方通过回填队列归还        |

操作一览：

| 领域 | 方法 |
|------|------|
| 套接字 | `TCP4Socket`, `TCP6Socket`, `UDP4Socket`, `UDP6Socket`, `UDPLITE4Socket`, `UDPLITE6Socket`, `SCTP4Socket`, `SCTP6Socket`, `UnixSocket`, `SocketRaw`，以及 `*Direct` 变体 |
| 连接 | `Bind`, `Listen`, `Accept`, `AcceptDirect`, `Connect`, `Shutdown` |
| 套接字 I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
| Multishot | `AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, `SubmitReceiveBundleMultishot` |
| 文件 I/O | `Read`, `Write`, `ReadV`, `WriteV`, `ReadFixed`, `WriteFixed`, `ReadvFixed`, `WritevFixed` |
| 文件管理 | `OpenAt`, `Close`, `Sync`, `Fallocate`, `FTruncate`, `Statx`, `RenameAt`, `UnlinkAt`, `MkdirAt`, `SymlinkAt`, `LinkAt` |
| 扩展属性 | `FGetXattr`, `FSetXattr`, `GetXattr`, `SetXattr` |
| 数据传输 | `Splice`, `Tee`, `Pipe`, `SyncFileRange`, `FileAdvise` |
| 超时 | `Timeout`, `TimeoutRemove`, `TimeoutUpdate`, `LinkTimeout` |
| 取消 | `AsyncCancel`, `AsyncCancelFD`, `AsyncCancelOpcode`, `AsyncCancelAny`, `AsyncCancelAll` |
| 轮询 | `PollAdd`, `PollRemove`, `PollUpdate`, `PollAddLevel`, `PollAddMultishot`, `PollAddMultishotLevel` |
| 异步 | `EpollWait`, `FutexWait`, `FutexWake`, `FutexWaitV`, `Waitid` |
| 环消息 | `MsgRing`, `MsgRingFD`, `FixedFdInstall`, `FilesUpdate` |
| 命令 | `UringCmd`, `UringCmdInline`, `UringCmd128`, `Nop`, `Nop128` |

`UringCmdInline` 将最多 80 字节复制到普通 `IORING_OP_URING_CMD` SQE 中，并要求使用 `Options.SQE128`；它不要求内核支持独立的 `IORING_OP_URING_CMD128` opcode。

`WithUringCmdFlags` 用于设置 `uring_cmd_flags` union。使用 `IORING_URING_CMD_FIXED` 时，通过 `sqeCtx.WithBufGroup` 指定注册缓冲区索引；使用 `IORING_URING_CMD_MULTISHOT` 时，还必须设置 `IOSQE_BUFFER_SELECT`，并将同一 context field 用作 provided-buffer group。这两个 flag 不能同时使用。

`Nop128` 和 `UringCmd128` 需要以 `Options.SQE128` 创建的 ring，且内核须声明支持相应 opcode，否则返回 `ErrNotSupported`。

`PollAddLevel` 和 `PollAddMultishotLevel` 始终返回 `ErrNotSupported`；Linux io_uring 不允许 `IORING_OP_POLL_ADD` 使用 level 标志。

`Uring.Close` 提交的是针对目标文件描述符的 `IORING_OP_CLOSE` 操作，而非 ring 本身的销毁方法。

## 上下文传递

`Multicast` 根据负载大小和目标数量自动选择普通发送或零拷贝发送；`MulticastZeroCopy` 使用更低的阈值，优先选择零拷贝。两者都要求 Direct 上下文并返回 `(submitted, error)`，其中 `submitted` 表示已接受的目标前缀长度。遇到背压后，从剩余目标继续提交。恢复调用可能选择不同的操作，因此应检查每个 CQE 的操作码和标志。`Multicast` 的普通发送遵循 `NotifySucceed`；选中的零拷贝发送和 `MulticastZeroCopy` 的两条路径都会保留完成证据，并拒绝显式的 `IOSQE_CQE_SKIP_SUCCESS`。

已受理的流式发送仍可能只完成部分字节。跟踪普通发送的字节数时，应保留成功 CQE，并根据每个目标的数据结果只续传尚未发送的尾部。共享的零拷贝数据必须保持不变，直到所有已受理目标都满足释放条件。[multicast 示例](./examples/zerocopy_test.go)展示了如何分别跟踪字节进度和释放。

`SQEContext` 是本包的核心标识令牌。Direct 模式将 opcode、SQE flags、buffer-group ID 与文件描述符打包为一个 64 位值。

```go
sqeCtx := uring.ForFD(int32(fd)).
	WithOp(uring.IORING_OP_RECV).
	WithBufGroup(groupID)
```

上下文有三种模式：

| 模式 | 表示方式 | 典型用途 |
|------|----------|----------|
| Direct | 内联 64 位负载 | 常见提交与回收路径，零分配 |
| Indirect | 指向 `IndirectSQE` 的指针 | 64 位不足以表达完整 SQE 负载时 |
| Extended | 指向 `ExtSQE` 的指针 | 完整 SQE 加 64 字节用户数据 |

常见路径下，从 `ForFD` 或 `PackDirect` 开始，只填入完成时需要回溯的信息；`WithFlags` 会整体替换 flag 集合，调用前应先计算好联合值。

当 direct 的 64 位布局不足以携带所需元数据时，可从池中借用 `ExtSQE`，通过 `Ctx*Of` 或 `ViewCtx*` 写入 `UserData`，再打包为 `SQEContext`。这里应优先使用标量负载；若通过原始覆盖视图或类型化视图存放了 Go 指针、接口值、函数值、切片、字符串、映射、通道或含有这些成员的结构体，必须将活跃根保留在 `UserData` 之外，因为 GC 不会扫描这段原始字节。

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

`NewContextPools` 返回可立即使用的对象池。上下文池耗尽与提交队列容量无关。所有请求完成事件和借用访问处理完毕后，将上下文归还给原池。`Reset` 会清空两个池并使尚未归还的借用失效；调用前必须停止所有池访问，并完成使用这些上下文的操作。`Reset` 之后不得访问或归还旧借用。

### 通过 `CQEView` 分发完成事件

没有独立的完成上下文类型。使用 `cqe.Context()` 获取原始提交标识，使用 `cqe.Err()` 获取请求结果，并检查标志以判断缓冲区状态和生命周期。`Wait` 返回的错误描述回收过程，而非单个请求。必须分发返回批次中的每个 CQE，包括失败的请求；提前返回会丢弃已从 ring 中回收的完成记录。

```go
var cqes [64]uring.CQEView
n, err := ring.Wait(cqes[:])
if err != nil {
	return err
}
if n == 0 {
	return iox.ErrWouldBlock
}

for i := range n {
	cqe := cqes[i]
	// Res < 0 encodes -errno; flags describe buffers and request lifetime.
	if err := cqe.Err(); err != nil {
		fmt.Printf("completion failed: op=%d fd=%d: %v\n", cqe.Op(), cqe.FD(), err)
	}
	if cqe.HasBuffer() {
		fmt.Printf("buffer id=%d\n", cqe.BufID())
	}
	// Route every CQE, including failures, so its owner can release resources.
	dispatch(ring, cqe)
}
```

完成时 `CQEView` 按需解码相应的上下文模式。其标量描述符是副本；Indirect/Extended 模式目标及借用缓冲区均不得超出文档约定的生命周期。

## 缓冲区供给

`uring` 提供三类常用缓冲区路径：注册缓冲区在 ring 启动时固定并用于固定缓冲区文件 I/O；提供缓冲区环让内核选择接收缓冲区，并在 CQE 中返回缓冲区 ID；捆绑接收按发布顺序消耗缓冲区，回收后该顺序可能不同于 ID 的数值顺序。`Uring.BundleIterator` 保留实际顺序；独立的 `NewBundleIterator` 要求按环大小取模后连续的 ID 顺序发布。

- 通过 `ReadBufferSize` 与 `ReadBufferNum` 配置固定尺寸的提供缓冲区
- 通过 `MultiSizeBuffer` 启用多尺寸缓冲区组
- 通过 `LockedBufferMem`、`RegisteredBuffer`、`ReadFixed` 与 `WriteFixed` 使用注册固定缓冲区

多数场景下，直接使用配置辅助函数即可：

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
	*o = opts
})
```

需要从显式内存预算出发时使用 `OptionsForBudget`；需要查看某预算对应的分层布局时使用 `BufferConfigForBudget`：

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

显式增量 ring 初始化为空。`Start` 之后，选择一个未被已配置提供缓冲区组使用的组 ID，暂存每个后备切片，然后发布该批次：

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

`BufferRing.Add` 暂存条目；`BufferRing.Advance` 发布条目并将所有权转移给内核。保持 `backing` 可达。按 CQ 顺序串行处理此 receiver 的 CQE，每条仅处理一次；receiver 为每个缓冲区 ID 独立维护偏移量。

`IORING_CQE_F_BUF_MORE` 表示内核在请求完成后仍保留所选缓冲区。只有 CQE 包含 `IORING_CQE_F_BUFFER`、不含 `IORING_CQE_F_BUF_MORE`，且所有借用访问均已结束时，才能重新发布该缓冲区。零或负的结果也可能归还缓冲区。buffer-ring 的销毁由 `Uring.Stop` 负责。

Fixed-buffer I/O 通过索引使用注册缓冲区。返回的切片属于 ring 内存；在 fixed 操作完成前保持其有效：

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.FD(file.Fd()) // 借用的 FD；file 保留关闭责任。
ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, 0).WithFD(fd)
if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
	return err
}
```

Socket 接收若要使用内核缓冲区选择，传入 `nil` 作为接收缓冲区，并指定所需的尺寸级别。完成事件会报告内核选择的缓冲区：

```go
recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 0)

if err := ring.Receive(recvCtx, &socketFD, nil, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
	return err
}

// 稍后，在 Wait 返回匹配 CQE 后：
if cqe.HasBuffer() {
	fmt.Printf("kernel selected group=%d id=%d\n", cqe.BufGroup(), cqe.BufID())
}
```

捆绑接收使用同一套提供缓冲区存储，但一个 CQE 可能消耗多个缓冲区。处理迭代器后，将消耗的槽位回收。在迭代器的所有副本中，`Recycle` 只能调用一次；调用返回后，不得再读取已生成的缓冲区：

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

注册缓冲区需要锁定内存。若大规模注册失败，可提高 `RLIMIT_MEMLOCK` 或缩减预算。

## Multishot 与监听器操作

`AcceptMultishot`、`ReceiveMultishot`、`SubmitAcceptMultishot`、`SubmitAcceptDirectMultishot`、`SubmitReceiveMultishot` 和 `SubmitReceiveBundleMultishot` 用于提交 multishot socket 操作。

CQE 路由仍由调用方负责。`ListenerOp.HandleCQE` 推进 SOCKET → BIND → LISTEN，并调用各阶段的回调。`DecodeListenerCQE`、`PrepareListenerBind`、`PrepareListenerListen` 和 `SetListenerReady` 仍可用于手动推进。参见[监听器指南](agents/recipes/listener.md)。

## 架构实现

实现层次如下：

1. `New` 创建处于禁用状态的内核 ring，构造上下文池，选定缓冲区策略。
2. `Start` 注册缓冲区并启用 ring（固定 Linux 6.18+ 基线）。
3. 操作方法通过写入 SQE 表达提交意图。
4. `Wait` 刷新提交并返回已复制的 CQE 描述符。
5. 调用方运行时代码负责调度、重试、挂起、连接与会话路由、路由退役、安全检查以及终态资源策略。

如此分工使 `code.hybscloud.com/uring` 专注于内核侧机制，同时在边界上保持清晰的完成语义。

## 运行时边界

`code.hybscloud.com/uring` 之上的运行时层应将其用作内核后端，而不是调度器。边界是单向的：`code.hybscloud.com/uring` 准备 SQE、回收 CQE、保留 `user_data`、暴露 CQE `res` 与标志，并报告所有权事实；调用方运行时代码将这些观测与自身令牌关联，应用重试与退避，路由处理器与会话，批量提交，退役路由，并释放终态资源。

当抽象执行需要完成事实时，运行时桥接层可以消费 Extended 模式 CQE。连接级运行时也可以在需要 CQE 结果、标志、缓冲区 ID 与编码令牌时直接轮询原始 Extended CQE，然后再将事件归约为处理器回调。

位于该边界之上的上下文层与抽象执行层不会改变 `code.hybscloud.com/uring` 的内核边界职责。

## 应用层设计模式

`code.hybscloud.com/uring` 公开面向内核的机制；调度、重试、连接追踪和协议解析属于调用方上层职责。以下模式描述调用方运行时必须保持的边界。

### Ring 所有者事件循环

在默认的任务可迁移模式下，共享提交状态同步允许 goroutine 在不同 OS 线程之间迁移。ring 所有者循环仍可串行化全部提交状态操作：提交待处理工作，在 `Wait` 没有返回可观察进展时使用调用方持有的 `iox.Backoff`，然后分发完成事件。

```go
func runLoop(ring *uring.Uring, stop <-chan struct{}) error {
	cqes := make([]uring.CQEView, 64)
	var backoff iox.Backoff
	for {
		select {
		case <-stop:
			// The caller must still cancel and drain live requests before ring.Stop.
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

所有 ring 方法，包括 `Send`、`Receive`、`AcceptMultishot` 和 `Wait`，均在该 goroutine 上执行。来自其他 goroutine 的工作应通过调用方持有的工作邮箱（例如 `code.hybscloud.com/lfq`）进入循环，不可直接调用 ring 方法。`iox.Backoff` 由调用方持有：当 `Wait` 返回的错误被分类为 `iox.OutcomeWouldBlock`，或一次 `Wait` 没有回收到任何 CQE 时，调用 `backoff.Wait()`；回收到 `n > 0` 的 CQE 批次后，调用 `backoff.Reset()`。

### Multishot 订阅生命周期

Multishot 操作产生 CQE 流，直到内核发送最终 CQE（不含 `IORING_CQE_F_MORE`）。调用方运行时代码先把每个 CQE 交给返回的订阅，再进入其余分发器。

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

// 在同一个串行化完成循环中 dispatch。若调用方在该循环之后保留复制 CQE，
// 调用方必须维护自己的 route state。
for i := range n {
	if sub.HandleCQE(cqes[i]) {
		continue
	}
	dispatch(ring, cqes[i])
}
```

每一次 `OnStep` 回调观察一个 `MultishotStep`：返回 `MultishotContinue` 保持流活跃，或返回 `MultishotStop` 请求取消。如果回调一直保持启用直到终态观察，`OnStop` 至多以最终错误及 `cancelled` 标志触发一次；在步骤自身上，`step.Cancelled` 在边界处把内核的 `-ECANCELED` 判定与其他失败明确区分开。这两个回调分别对应 `MultishotHandler.OnMultishotStep` 和 `MultishotHandler.OnMultishotStop`；也可以直接实现该接口。`HandleCQE` 用于调用方串行化完成循环内的立即 dispatch；若调用方在该循环之后保留复制 CQE，则需维护自己的 route state，并拒绝已退役订阅的观察。在显式单提交者 ring（`MultiIssuers=false`）上，应从固定于 OS 线程的 ring 所有者调用 `Cancel` / `Unsubscribe`，并将其与提交、`Wait`、`WaitDirect`、`WaitExtended`、`Stop` 以及 `ResizeRings` 串行化。默认 `MultiIssuers` ring 通过共享提交路径串行化取消 SQE。

每个已入队的取消 CQE 都应交给同一订阅的 `HandleCQE` 或其扩展处理器，即使已到达 `SubscriptionStopped` 或触发 `OnStop`。提交失败或未执行新提交的调用不会产生新的取消 CQE 处理义务。订阅拥有两个上下文，只有在处理完原请求的终态 CQE、完成取消分派且取消提交过程结束后，才将原上下文归还池。不要手动归还这些上下文。[接收示例](./examples/multishot_test.go)展示了自然完成和由处理器请求的取消。

### 类型化上下文承载连接状态

扩展上下文可以在提交 → 完成的往返全程携带连接级引用，无需全局查找表：

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

完成时通过同一类型化视图恢复状态：

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

活跃的 Go 指针根必须在 `UserData` 之外保持可达，因为 GC 不会追踪这些原始字节。内部 multishot 和监听器协议由附属于每个 `ExtSQE` 槽位的旁路根集处理，但放置类型化引用的调用方运行时代码需自行维护可达性。

### 截止时间组合

`LinkTimeout` 通过 `IOSQE_IO_LINK` 链将截止时间附加到前一个 SQE 上。到期时会尝试取消关联操作，但该操作可能已经完成。必须检查两条 CQE 的结果；超时本身不能证明操作已被取消。

```go
recvCtx := uring.ForFD(int32(fd)).
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

连续提交关联的 SQE，并将这对提交与其他提交和等待调用串行化。保留操作的缓冲区和上下文，直到该操作自身完成；结束前必须处理两条 CQE。

## TCP 使用模式

以下是配合测试代码阅读的简化流程：

| 场景       | 主要 API                                                           | 参考                                                                                |
|----------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo 服务器 | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| 客户端      | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP Echo 服务器

`ListenerManager` 提交最初的 SOCKET 请求。将配置阶段的每个 CQE 交给 `listenerOp.HandleCQE`，即可自动调用回调并提交 BIND 和 LISTEN。如果返回 `iox.ErrWouldBlock`，应先驱动 ring，再通过 `ring.SubmitExtended(uring.PackExtended(listenerOp.Ext()))` 重试已准备好的 SQE；不要重复处理该 CQE。收到 `OnListening` 的成功通知后再开始接受连接。

```go
// OnListening has reported successful setup for listenerOp.
acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
if err != nil {
	return err
}

recvCtx := uring.ForFD(int32(clientFD)).WithBufGroup(readGroup)
recvSub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
if err != nil {
	return err
}
```

停止订阅时调用 `Cancel`，然后继续分发，直到处理完其终端 CQE，以及已提交的取消请求对应的 CQE。两个订阅都必须完成收尾，才能关闭其描述符或停止 ring。[监听器示例](./listener_example_test.go) 展示完整的创建与关闭流程；[multishot 示例](./examples/multishot_test.go) 和 [echo 示例](./examples/echo_test.go) 展示连接处理。

### TCP 客户端

先创建 socket 并等待 `IORING_OP_SOCKET` 完成，将返回的 FD 转为 `iofd.FD`，随后用于 `Connect`、`Send` 和 `Receive`。

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

每次提交后，使用 ring 生命周期一节中的 `Wait` 循环观察对应的完成事件，再执行依赖它的下一步操作。保持数据有效直到完成，短发送后只重试尚未发送的后缀。[socket_test.go](./examples/socket_test.go) 展示完整的客户端流程；[echo_test.go](./examples/echo_test.go) 进一步展示接收与回复。

## 零拷贝接收（ZCRX）

`ZCRXReceiver` 管理通过 `io_uring` 从 NIC 硬件 RX 队列进行的零拷贝接收。

`NewZCRXReceiver` 面向以 32 字节 CQE（`IORING_SETUP_CQE32`）创建的 ring。当前 `Options` 尚未暴露该设置标志，因此通过标准 `New` 创建的 ring 会使该构造器返回 `ErrNotSupported`。在 CQE32 设置路径公开前，本节仅记录接收器的边界契约，而非可直接执行的公开设置流程。

### 生命周期

1. 在支持 CQE32 的 ring 上调用 `NewZCRXReceiver` 创建接收器。构造器会注册 ZCRX 接口队列、映射接收区域并初始化空的补充 ring。Linux 提供初始接收缓冲区；补充 ring 用于归还已交付的缓冲区。
2. 调用 `Start`，在 ring 上提交扩展 `RECV_ZC` 操作。
3. CQE 分发时，ZCRX 完成事件路由至 `ZCRXHandler`：
   - `OnData` 交付指向 NIC 映射区域的 `ZCRXBuffer`。处理完毕后调用 `Release`，成功发布回填条目后才将槽位归还内核。若返回 `iox.ErrWouldBlock`，调用方必须保留缓冲区，稍后在 CQE 路径上重试释放。零长度 EOF 缓冲区没有回填标识，释放时不发布回填条目。返回 `false` 请求尽力停止。
   - `OnError` 交付 CQE 错误；返回 `false` 请求尽力停止。
   - `OnStopped` 在进入 `Stopped` 前的终态退出阶段被调用一次。
4. 调用 `Stop` 提交异步取消，接收器依次经历 `Stopping` → `Retiring` → `Stopped`。
5. 轮询 `Stopped` 直至返回 `true`，停止所属 ring，再调用 `Close` 释放映射区域与补充 ring。

### 状态机

```
Idle → Active → Stopping → Retiring → Stopped
```

取消提交失败时，`Stop` 回退至 `Active`。`Close` 幂等。

### 处理器契约

- `OnData` 与 `OnError` 在 CQE 分发 goroutine 中串行调用。
- `Release` 为单生产者操作，仅限在分发 goroutine 中调用。
- 调用 `Stop` 时必须确保与 CQE 分发不并发，这是调用方需保证的串行化约定。

## 示例

`examples/` 中的可运行测试通过直接调用公开 API，展示初始化 → 提交 → 完成 → 清理。建议从上下文和文件 I/O 开始，再学习套接字与 multishot 示例中的所有权规则。

- [context_test.go](./examples/context_test.go)：Direct、Indirect、Extended 上下文，完成解码与借用上下文归还
- [completion_test.go](./examples/completion_test.go)：区分提交错误与完成错误、关联请求标识，以及排空包含成功和失败结果的批次
- [file_io_test.go](./examples/file_io_test.go)：文件读写、分块读取至 EOF 与批量提交
- [file_lifecycle_test.go](./examples/file_lifecycle_test.go)：通过完成事件依次打开、写入、同步和关闭文件
- [vectored_io_test.go](./examples/vectored_io_test.go)：分散读取与聚集写入
- [fixed_buffers_test.go](./examples/fixed_buffers_test.go)：注册缓冲区与 fixed-buffer I/O
- [buffer_ring_test.go](./examples/buffer_ring_test.go)：bundle 接收、增量接收与缓冲区回收
- [socket_test.go](./examples/socket_test.go)：客户端套接字创建、连接完成，以及继续发送剩余字节
- [echo_test.go](./examples/echo_test.go)：TCP echo、流式 I/O 的部分完成与 UDP 数据报
- [multishot_test.go](./examples/multishot_test.go)：重复的 accept/receive 完成事件与取消后的排空
- [zerocopy_test.go](./examples/zerocopy_test.go)：零拷贝发送、自动 multicast 选择，以及独立的结果与释放跟踪
- [poll_test.go](./examples/poll_test.go)：就绪状态观察与 poll 取消
- [timeout_linux_test.go](./examples/timeout_linux_test.go)：到期、取消与链接超时
- [splice_tee_test.go](./examples/splice_tee_test.go)：由完成事件驱动的 splice 与 tee 传输

包根目录下的 [listener_example_test.go](./listener_example_test.go) 展示自动 SOCKET → BIND → LISTEN 初始化与 multishot accept；[socket_integration_linux_test.go](./socket_integration_linux_test.go) 展示 TCP connect/send。

## 运行注意事项

- 启用 `NotifySucceed` 可默认保留写入类操作成功时的 CQE。显式指定的 `IOSQE_CQE_SKIP_SUCCESS` 标志仍然有效。
- `ring.Features` 报告实际 SQ/CQ 条目数、SQE 槽宽以及本包解析 `user_data` 的字节序。
- 普通 Go 执行和并发提交状态调用应保留默认的任务可迁移 `MultiIssuers=true` 配置（`COOP_TASKRUN`）。仅当所有者在 `New` 前调用 `runtime.LockOSThread`、持续固定到 `Stop`，并串行化 `submit`、`Wait`、`WaitDirect`、`WaitExtended`、`Stop` 与 `ResizeRings` 时，才设置 `MultiIssuers=false`；该显式快速路径请求 `SINGLE_ISSUER` + `DEFER_TASKRUN`。
- `EpollWait` 要求 `timeout` 为 `0`；如需设置截止时间，使用 `LinkTimeout`。
- 所有请求完成处理和借用访问结束后，才能归还池化上下文。
- `ListenerOp.Close` 会立即关闭监听 FD。若仍有设置 CQE 待处理，需先回收该 CQE，再调用 `Close` 将借用的 `ExtSQE` 归还池中。

## 平台支持

`uring` 的真实内核路径面向 Go 1.26+ / Linux 6.18+。大部分实现文件与示例测试由 `//go:build linux` 约束；Darwin 文件仅为共享 API 表面提供编译桩，Linux 专属能力仍仅限 Linux，不会改变上述 Linux 运行时基线。

## 许可证

MIT，参见 [LICENSE](./LICENSE)。

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
