# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go で Linux 6.18+ の `io_uring` カーネル境界を扱うパッケージ。

言語: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | **日本語** | [Français](./README.fr.md)

## 概要

`uring` は Linux `io_uring` へのカーネル境界を提供する Go パッケージです。リングの生成・開始、SQE の組み立て、CQE の解釈、`user_data` による発行元識別の伝達を担い、バッファ登録・マルチショット操作・リスナー構築の基本操作をスケジューラにしないまま公開します。

パッケージは境界を明示に保ちます：カーネル側の機構と完了結果の観測はこの API にとどまり、方針や組み立ては呼び出し側レイヤーが担うという役割分担です。呼び出し側のランタイムコードが、完了ルーティング、ルートの終了処理、再試行とバックオフ、ハンドラとセッションのルーティング、安全性チェック、コネクションライフサイクル、終端時のリソース解放を担当します。

主な API は以下のとおりです。

- `Uring`: リングハンドルと操作メソッド群
- `SQEContext`: `user_data` で運ばれる発行元識別子
- `CQEView`: `Wait` が返すコピー済み完了記述子
- 登録バッファおよびマルチサイズ buffer group によるバッファ供給

## インストール

`uring` の動作には Linux カーネル 6.18 以上が必要です。まず動作中のカーネルバージョンを確認してください。

```bash
uname -r
```

`uring` は 6.18+ のベースラインを前提とし、古いカーネル向けのフォールバック分岐を持ちません。本パッケージ内に互換シムを期待するのではなく、要件を満たすカーネルを起動してください。

Debian 13 の stable カーネルは 6.18 未満となる場合があります。Debian パッケージ版の 6.18+ カーネルを入手する手順は、下記の [Debian 13 カーネル更新](#debian-13-カーネル更新) を参照してください。

```bash
go get code.hybscloud.com/uring
```

### Debian 13 カーネル更新

Debian 13 の安定版トラックが提供するカーネルは 6.12 です。`trixie-backports` スイートから Debian パッケージ済みの 6.18+ カーネルを導入できます。手順の詳細は [SETUP.md](./SETUP.md) を参照してください。

### トラブルシューティング

リング生成時に `ENOMEM`・`EPERM`・`ENOSYS` が返る場合、memlock 上限、sysctl 設定、またはカーネルのサポート状況が原因です。コンテナランタイムはデフォルトで `io_uring` システムコールをブロックします。診断と解決手順は [SETUP.md](./SETUP.md) を参照してください。

## `code.hybscloud.com/uring` 境界周辺のエージェント向けコーディング

`code.hybscloud.com/uring` 境界周辺の呼び出し側コード、つまりランタイム、プロトコルスタック、サービス、および正確な Linux `io_uring` 境界を必要とするレビュー作業を生成またはレビューするときは、独立したガイドの入口 [`AGENTS.md`](./AGENTS.md) を使用します。[`agents/INDEX.md`](./agents/INDEX.md) はトピック索引として使い、SQE/CQE の機構、`user_data` による識別、バッファ所有権、完了結果、ライフサイクル規則、統合境界、参照確認、そして要求分析から形式化、推論、Go へのコンパイル、検証までの流れを保ってください。

規則は単純です。`code.hybscloud.com/uring` はカーネルの事実を公開し、ポリシーは呼び出し側レイヤーが選びます。再試行/backoff、ポーリング周期、スケジューリング、ルーティング、ルートの終了処理、プロトコル状態、パース、完了ルーティング、キャンセルおよびタイムアウトポリシー、安全性チェック、サービスライフサイクルは `code.hybscloud.com/uring` の境界外に置き、`ErrWouldBlock`、`ErrMore`、`IORING_CQE_F_MORE`、所有権の移動、機能不足による失敗を可視のまま保ってください。

## リングのライフサイクル

`New` は未開始状態のリングを返し、コンテキストプールを即座に構築します。操作の発行に先立ち `Start` を呼び出し、リングリソースの登録と有効化を行います。次の例ではファイル read を発行し、対応する CQE を待ち、`iox.Classify` によって `ErrWouldBlock` を失敗ではなく「進展なし」の意味として扱います。

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

`Wait` は未送信の SQE をフラッシュしてから完了を回収します。単一発行者リングでは、SQ が空になったあとも遅延タスクを進めるためのカーネル enter も発行します。呼び出し側は `Wait`、`WaitDirect`、`WaitExtended` と他の発行状態操作を直列化する必要があります。`Wait` が返した `err` を `iox.Classify(err)` で分類して `iox.OutcomeWouldBlock` になった場合、それは現時点で境界上に観測可能な完了がないことを示します。

`Start` と `Stop` がリングのライフサイクルを構成します。`Stop` は冪等ですが、呼び出すとリングは恒久的に使用不能になります。進行中の操作をすべて完了させ、未回収の CQE を回収し、multishot サブスクリプションを停止してから呼び出してください。

## 型と操作

| 型                     | 役割                                              |
|-----------------------|-------------------------------------------------|
| `Uring`               | リングの初期化・発行・完了回収と操作メソッド                          |
| `Options`             | リングエントリ数、登録バッファ予算、buffer group スケール、完了の可視性      |
| `SQEContext`          | `user_data` に格納されるコンパクトな発行元識別子                  |
| `CQEView`             | デコード済みアクセサを持つコピー済み完了記述子。モード依存の参照先は借用       |
| `ListenerOp`          | リスナー作成操作のハンドル。FD の保持と accept ヘルパーを提供            |
| `BundleIterator`      | バンドル受信で消費されたバッファの走査                             |
| `IncrementalReceiver` | インクリメンタル buffer-ring 受信の管理（`IOU_PBUF_RING_INC`） |
| `BufferRing`          | 明示的に登録する不透明な提供バッファリング。`Add` で準備し、`Advance` で公開 |
| `ZCTracker`           | ゼロコピー送信の通知とフォールバックのライフサイクルを追跡              |
| `ContextPools`        | indirect・extended 発行コンテキストのプール                  |
| `ZCRXReceiver`        | NIC RX キューからのゼロコピー受信ライフサイクルの管理                  |
| `ZCRXConfig`          | ZCRX 受信インスタンスの設定                                |
| `ZCRXHandler`         | ZCRX のデータ・エラー・シャットダウンに対するコールバックインターフェース         |
| `ZCRXBuffer`          | 配送済みゼロコピー受信ビュー。呼び出し側が補充キュー経由で返却する              |

操作メソッド:

| 領域       | メソッド                                                                                                                                                                |
|----------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| ソケット     | `TCP4Socket`, `TCP6Socket`, `UDP4Socket`, `UDP6Socket`, `UDPLITE4Socket`, `UDPLITE6Socket`, `SCTP4Socket`, `SCTP6Socket`, `UnixSocket`, `SocketRaw` および `*Direct` 系 |
| 接続       | `Bind`, `Listen`, `Accept`, `AcceptDirect`, `Connect`, `Shutdown`                                                                                                   |
| ソケット I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy`                                                       |
| マルチショット  | `AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, `SubmitReceiveBundleMultishot`             |
| ファイル I/O | `Read`, `Write`, `ReadV`, `WriteV`, `ReadFixed`, `WriteFixed`, `ReadvFixed`, `WritevFixed`                                                                          |
| ファイル管理   | `OpenAt`, `Close`, `Sync`, `Fallocate`, `FTruncate`, `Statx`, `RenameAt`, `UnlinkAt`, `MkdirAt`, `SymlinkAt`, `LinkAt`                                              |
| 拡張属性     | `FGetXattr`, `FSetXattr`, `GetXattr`, `SetXattr`                                                                                                                    |
| データ転送    | `Splice`, `Tee`, `Pipe`, `SyncFileRange`, `FileAdvise`                                                                                                              |
| タイムアウト   | `Timeout`, `TimeoutRemove`, `TimeoutUpdate`, `LinkTimeout`                                                                                                          |
| キャンセル    | `AsyncCancel`, `AsyncCancelFD`, `AsyncCancelOpcode`, `AsyncCancelAny`, `AsyncCancelAll`                                                                             |
| ポーリング    | `PollAdd`, `PollRemove`, `PollUpdate`, `PollAddLevel`, `PollAddMultishot`, `PollAddMultishotLevel`                                                                  |
| 非同期      | `EpollWait`, `FutexWait`, `FutexWake`, `FutexWaitV`, `Waitid`                                                                                                       |
| リングメッセージ | `MsgRing`, `MsgRingFD`, `FixedFdInstall`, `FilesUpdate`                                                                                                             |
| コマンド     | `UringCmd`, `UringCmdInline`, `UringCmd128`, `Nop`, `Nop128`                                                                                                        |

`UringCmdInline` は最大 80 バイトを通常の `IORING_OP_URING_CMD` SQE 内へコピーし、`Options.SQE128` を必要とします。別個の `IORING_OP_URING_CMD128` opcode のサポートは必要ありません。

`WithUringCmdFlags` は `uring_cmd_flags` union を設定します。`IORING_URING_CMD_FIXED` では `sqeCtx.WithBufGroup` に登録済みバッファ index を指定します。`IORING_URING_CMD_MULTISHOT` では `IOSQE_BUFFER_SELECT` も指定し、同じ context field を provided-buffer group として使用します。この 2 つの flag は同時に指定できません。

`Nop128` と `UringCmd128` は `Options.SQE128` 付きで作成したリングと、対応する opcode のカーネルサポートの両方を必要とします。いずれかが欠ける場合は `ErrNotSupported` を返します。

`Uring.Close` は指定したファイルディスクリプタに `IORING_OP_CLOSE` を発行します。リング自体の破棄ではありません。

`PollAddLevel` と `PollAddMultishotLevel` は常に `ErrNotSupported` を返します。Linux io_uring は `IORING_OP_POLL_ADD` でレベルフラグを受け付けません。

## コンテキスト伝達

`Multicast` はペイロードサイズと宛先数に応じて通常送信とゼロコピー送信を自動選択します。`MulticastZeroCopy` はより低いしきい値でゼロコピーを優先します。どちらも Direct コンテキストを要求し、`(submitted, error)` を返します。`submitted` は先頭から受理された宛先数です。バックプレッシャー時は残りの宛先から再開します。再開時に選択される操作が変わる場合があるため、各 CQE のオペコードとフラグを確認してください。`Multicast` の通常送信は `NotifySucceed` に従います。選択されたゼロコピー送信と `MulticastZeroCopy` の両経路は完了の観測を維持し、明示的な `IOSQE_CQE_SKIP_SUCCESS` を拒否します。

受理されたストリーム送信でも、要求したバイト数より少ない転送で完了する場合があります。通常送信のバイト数を追跡する場合は成功 CQE を有効にし、宛先ごとのデータ結果から未送信の末尾部分だけを再送してください。共有するゼロコピーデータは、受理されたすべての宛先が解放条件に達するまで変更しないでください。[multicast の例](./examples/zerocopy_test.go)は、バイト単位の進捗と解放をそれぞれ追跡します。

`SQEContext` はこのパッケージの主要な識別トークンです。direct モードでは opcode、SQE flags、buffer group ID、ファイルディスクリプタを 64 ビット値ひとつに格納します。

```go
sqeCtx := uring.ForFD(fd).
    WithOp(uring.IORING_OP_RECV).
    WithBufGroup(groupID)
```

コンテキストには 3 つのモードがあります。

| モード      | 表現形式                 | 主な用途                     |
|----------|----------------------|--------------------------|
| Direct   | 64 ビットのインライン負荷       | 一般的な発行・回収経路。ゼロアロケーション    |
| Indirect | `IndirectSQE` へのポインタ | 64 ビットでは SQE 全体を表現できない場合 |
| Extended | `ExtSQE` へのポインタ      | 完全な SQE と 64 バイトのユーザーデータ |

通常は `ForFD` または `PackDirect` から開始し、完了時に参照したい情報だけを付加します。`WithFlags` はフラグ集合全体を上書きするため、論理和は事前に計算してから渡してください。

direct の 64 ビット配置に収まらない独自メタデータが必要な場合は、`ExtSQE` を借用し、`Ctx*Of` や `ViewCtx*` で `UserData` に書き込んだうえで `SQEContext` に再パックします。`UserData` にはスカラー値を格納するのが望ましく、Go ポインタ、インターフェース値、関数値、スライス、文字列、マップ、チャネル、またはそれらを含む構造体を生のオーバーレイや型付きビュー経由で置く場合は、GC がその生バイトを走査しないため、参照元を `UserData` の外に保持する必要があります。

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

`NewContextPools` はすぐに使えるプールを返します。コンテキストプールの枯渇とサブミッションキューの容量は独立しています。リクエストのすべての完了処理と借用参照へのアクセスを終えてから、コンテキストを借用元のプールへ返却してください。`Reset` は両方のプールをクリアし、未返却の借用を無効にします。プールへのすべてのアクセスを停止し、そのコンテキストを使う操作が完了してから呼び出してください。`Reset` 後に以前の借用へアクセスしたり、返却したりしてはいけません。

### `CQEView` による完了ディスパッチ

`uring` は完了コンテキスト専用の型を持ちません。完了のディスパッチは `CQEView` を介して行い、発行時のトークンが必要であれば `cqe.Context()` で取得します。

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

完了時、`CQEView` は対応するコンテキストモードをオンデマンドでデコードします。そのスカラー記述子はコピーされます。Indirect/Extended モードの参照先と借用バッファは、それぞれ定められたライフタイムを超えて保持しないでください。

## バッファ供給

`uring` には実用上 3 つのバッファ経路があります。登録バッファはリング開始時に固定され、固定バッファのファイル I/O で使います。提供バッファリングではカーネルが受信バッファを選び、選択されたバッファ ID を CQE に返します。バンドル受信は公開順にバッファを消費します。再利用後は ID の数値順と一致するとは限りません。`Uring.BundleIterator` は実際の順序を保持します。単独の `NewBundleIterator` には、リングサイズを法とする連続した ID 順の公開が必要です。

- `ReadBufferSize`・`ReadBufferNum` による固定サイズの提供バッファ
- `MultiSizeBuffer` によるマルチサイズバッファグループ
- `LockedBufferMem`・`RegisteredBuffer`・`ReadFixed`・`WriteFixed` による登録固定バッファ

多くのシステムでは、設定ヘルパーから始めるのが手っ取り早い入り口です。

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
```

メモリ予算を明示的に指定したい場合は `OptionsForBudget` を、予算に対して選択される階層構成を確認したい場合は `BufferConfigForBudget` を使用してください。

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

明示的なインクリメンタルリングは空の状態で初期化されます。`Start` の後、設定済みの提供バッファグループが使用していないグループ ID を選び、各バッキングスライスを準備してからバッチを公開します。

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

`BufferRing.Add` はエントリを準備し、`BufferRing.Advance` はそれらを公開して所有権をカーネルへ移します。`backing` を到達可能な状態に保ってください。この receiver の CQE は、CQ の順序で一度ずつ直列に処理します。receiver はバッファ ID ごとに独立したオフセットを保持します。

`IORING_CQE_F_BUF_MORE` は、リクエスト完了後も選択されたバッファをカーネルが保持していることを示します。`IORING_CQE_F_BUFFER` があり、`IORING_CQE_F_BUF_MORE` がない CQE を受け取り、借用中のアクセスがすべて終了してからバッファを再公開してください。結果がゼロまたは負でもバッファが返却されることがあります。buffer-ring の破棄は `Uring.Stop` が担当します。

固定バッファ I/O は登録済みバッファをインデックスで参照します。返されるスライスはリング所有のメモリなので、固定操作が完了するまで有効に保ってください。

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.NewFD(int(file.Fd()))
ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, 0).WithFD(fd)
if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
    return err
}
```

ソケット受信でカーネルのバッファ選択を使う場合は、受信バッファとして `nil` を渡し、必要なサイズクラスを指定します。選択されたバッファは完了 CQE で報告されます。

```go
recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 0)

if err := ring.Receive(recvCtx, &socketFD, nil, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
    return err
}

// 後で、Wait が対応する CQE を返したあと:
if cqe.HasBuffer() {
    fmt.Printf("kernel selected group=%d id=%d\n", cqe.BufGroup(), cqe.BufID())
}
```

バンドル受信は同じ提供バッファストレージを使いますが、1 つの CQE で複数のバッファを消費することがあります。イテレータを処理したら、消費したスロットを回収してください。`Recycle` はイテレータのすべてのコピーを通じて正確に 1 回だけ呼び出し、返った後は生成されたバッファを読み取らないでください。

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

登録バッファにはピン留めされたメモリが必要です。大きなバッファの登録に失敗する場合は `RLIMIT_MEMLOCK` を引き上げるか、メモリ予算を小さくしてください。

## マルチショットとリスナー操作

`AcceptMultishot`、`ReceiveMultishot`、`SubmitAcceptMultishot`、`SubmitAcceptDirectMultishot`、`SubmitReceiveMultishot`、`SubmitReceiveBundleMultishot` がマルチショットのソケット操作を発行します。

CQE のルーティング方針はパッケージ外に委ねています。リスナーの構築は `DecodeListenerCQE`、`PrepareListenerBind`、`PrepareListenerListen`、`SetListenerReady` の順で進みますが、完了のディスパッチ方法や連鎖の停止条件は呼び出し側が決定します。

## 実装構造

実装の境界は以下のように分かれています。

1. `New`: 無効状態のカーネルリングを構築し、コンテキストプールを確保し、バッファ戦略を決定する。
2. `Start`: バッファを登録し、Linux 6.18+ ベースライン向けにリングを有効化する。
3. 操作メソッド: SQE を書き込んで操作の意図を発行する。
4. `Wait`: 未送信分をフラッシュし、コピー済み CQE 記述子を返す。
5. 呼び出し側ランタイムコード: スケジューリング、リトライ、待機、コネクションとセッションのルーティング、ルートの終了処理、安全性チェック、終端時のリソース方針を担う。

この構造により、`code.hybscloud.com/uring` はカーネル境界の機構に専念しつつ、境界を越えた完了結果の意味を保持します。

## ランタイム境界

`code.hybscloud.com/uring` の上位にあるランタイム層は、これをスケジューラではなくカーネルバックエンドとして扱うべきです。境界は一方向で、`code.hybscloud.com/uring` は SQE の準備、CQE の回収、`user_data` の保持、CQE `res` とフラグの公開、所有権事実の報告を担い、呼び出し側ランタイムコードはそれらの観測を自身のトークンと相関付け、再試行とバックオフを適用し、ハンドラとセッションをルーティングし、発行をバッチ化し、ルートを終了処理し、終端リソースを解放します。

抽象実行が完了事実を必要とする場合、ランタイムブリッジは Extended モード CQE を消費できます。コネクション単位のランタイムは、CQE の結果、フラグ、バッファ ID、エンコード済みトークンを必要とする場合、イベントをハンドラコールバックに還元する前に生の Extended CQE を直接ポーリングできます。

この境界の上にあるコンテキスト層と抽象実行層は、`code.hybscloud.com/uring` のカーネル境界としての役割を変更しません。

## アプリケーション層の設計パターン

`code.hybscloud.com/uring` はカーネルに面した機構を公開します。スケジューリング、リトライ、コネクション追跡、プロトコル解釈は呼び出し側の上位レイヤーが担います。以下のパターンは呼び出し側ランタイムが保持すべき境界を示します。

### リング所有イベントループ

デフォルトのタスク移動可能モードでは、共有された発行状態の同期により goroutine は OS スレッド間を移動できます。リング所有ループは引き続きすべての発行状態操作を直列化できます。一般的なループでは保留中のワークを発行し、`Wait` が観測可能な進展を返さないときは呼び出し側の `iox.Backoff` を適用し、完了を振り分けます。

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

`Send`、`Receive`、`AcceptMultishot`、`Wait` などのリングメソッドはすべてこの goroutine 上で実行します。他の goroutine からの作業は、`code.hybscloud.com/lfq` のように呼び出し側が所有する作業メールボックスを経由してループに渡します。リングメソッドを直接呼び出してはなりません。`iox.Backoff` は呼び出し側が所有します。`Wait` が `iox.OutcomeWouldBlock` に分類された場合、または CQE を 1 件も回収できなかった場合は `backoff.Wait()` を呼び、`n > 0` のバッチを回収できたら `backoff.Reset()` を呼びます。

### マルチショットサブスクリプションのライフサイクル

マルチショット操作は、カーネルが最終 CQE（`IORING_CQE_F_MORE` なし）を送るまで CQE のストリームを生成します。呼び出し側ランタイムコードは各 CQE を返されたサブスクリプションへ先にルーティングしてから残りのディスパッチャへ渡します。

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

// 同じ直列化された完了ループ内で dispatch します。コピーした CQE を
// このループの後も保持する場合、呼び出し側が独自の route state を保持してください。
for i := range n {
    if sub.HandleCQE(cqes[i]) {
        continue
    }
    dispatch(ring, cqes[i])
}
```

`OnStep` コールバックは 1 件の `MultishotStep` を観察します。ストリームを維持するなら `MultishotContinue` を、キャンセルを要求するなら `MultishotStop` を返します。コールバックが終端観察まで有効なままなら、`OnStop` は最終エラーと `cancelled` フラグを伴って高々一度だけ実行されます。ステップ自身の `step.Cancelled` は、境界上でカーネルが `-ECANCELED` を返した判定をその他の失敗と明示的に区別します。両コールバックは `MultishotHandler` インタフェース（`OnMultishotStep` / `OnMultishotStop`）のビルダー側射影です。明示的なハンドラを書く場合はこのインタフェースを直接実装してください。`HandleCQE` は呼び出し側の直列化された完了ループ内で即時 dispatch するための補助です。コピー済み CQE をそのループの後も保持する場合、呼び出し側が独自の route state を保持し、すでに解放されたサブスクリプションの観察を拒否してください。明示的な単一発行者リング（`MultiIssuers=false`）では、OS スレッドに固定したリング所有者から `Cancel` / `Unsubscribe` を呼び、発行、`Wait`、`WaitDirect`、`WaitExtended`、`Stop`、`ResizeRings` と直列化してください。デフォルトの `MultiIssuers` リングでは、共有発行パスがキャンセル SQE を直列化します。

投入済みのキャンセル CQE は、`SubscriptionStopped` または `OnStop` の後も、同じサブスクリプションの `HandleCQE` またはその拡張ハンドラへ渡してください。投入の失敗や何もしない呼び出しでは、新たなキャンセル CQE の処理義務は生じません。サブスクリプションは両コンテキストを所有し、元の終端 CQE の処理、キャンセルの dispatch、キャンセル投入処理がすべて終わってから、元のコンテキストをプールへ返します。これらのコンテキストを手動で返さないでください。[受信の例](./examples/multishot_test.go)は、自然な完了とハンドラからのキャンセルを示します。

### 型付きコンテキストによるコネクション状態

拡張コンテキストは、グローバルルックアップテーブルを介さずに、発行 → 完了のラウンドトリップ全体にわたってコネクション単位の参照を伝搬します。

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

完了時に同じ型付きビューを通じて状態を復元します。

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

ライブな Go ポインタルートは `UserData` の外側で到達可能に保ってください。GC はそれらの生バイトをトレースしません。内部のマルチショットおよびリスナープロトコルでは各 `ExtSQE` スロットに付属するサイドカーのルートセットが処理しますが、型付き参照を配置する呼び出し側ランタイムコードは独自に到達可能性を維持する必要があります。

### デッドライン合成

`LinkTimeout` は `IOSQE_IO_LINK` チェーンを通じて直前の SQE に期限を付与します。期限が切れるとリンク先の操作のキャンセルを試みますが、その操作はすでに完了している場合があります。両方の CQE の結果を確認してください。タイムアウトだけでは、操作がキャンセルされたとは判断できません。

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

リンクする SQE は連続して投入し、この一組の投入を他の投入・待機呼び出しと直列化してください。操作自身の完了までバッファとコンテキストを保持し、終了処理の前に両方の CQE を処理してください。

## TCP の使用パターン

テストと併せて読める最短のフローを以下に示します。

| シナリオ     | 主な API                                                           | 参照                                                                                |
|----------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo サーバ | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| クライアント   | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP Echo サーバ

socket → bind → listen の一連の手順をパッケージに任せるには `ListenerManager` を使います。リスナーハンドラの bool を返すコールバックは制御フローのフックです。`true` を返すと次のセットアップ段階へ進み、`false` を返すとその手前で中止します。リスナーが稼働したら、接続済み FD に対して multishot accept と multishot receive を開始します。

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

`listener_example_test.go` はリスナーの構築と multishot accept、`examples/multishot_test.go` はハンドラ側の multishot receive CQE の処理、`examples/echo_test.go` はループバック echo の一連の流れをそれぞれ示しています。

### TCP クライアント

ソケットを作成し、`IORING_OP_SOCKET` の完了を待ちます。返された FD を `iofd.FD` に変換したうえで `Connect`、`Send`、`Receive` に渡します。

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

各発行のあとは「リングのライフサイクル」節で示した `Wait` ループを使って対応する完了を取得します。パッケージレベルの `socket_integration_linux_test.go` が connect/send の一連の流れをカバーしています。

## ゼロコピー受信（ZCRX）

`ZCRXReceiver` は `io_uring` を通じて NIC ハードウェア RX キューからゼロコピー受信を行う仕組みを管理します。

`NewZCRXReceiver` は 32 バイト CQE（`IORING_SETUP_CQE32`）で作成されたリング向けに用意されています。現在の `Options` はこの設定フラグを公開していないため、標準の `New` で作成したリングに対してはこのコンストラクタは `ErrNotSupported` を返します。CQE32 の設定経路が公開されるまでは、この節は実行可能な公開セットアップ手順ではなく、レシーバの境界契約を説明するものです。

### ライフサイクル

1. CQE32 対応リング上で `NewZCRXReceiver` を生成する。コンストラクタが ZCRX インターフェースキューを登録し、受信領域をマッピングして、空の補充リングを初期化する。初期受信バッファは Linux が供給し、補充リングは配送済みバッファの返却に使う。
2. `Start` を呼び出し、リング上で拡張 `RECV_ZC` 操作を発行する。
3. CQE のディスパッチ経路で ZCRX 完了が `ZCRXHandler` に届く。
    - `OnData`: NIC マップ領域を指す `ZCRXBuffer` を受け取る。処理後に `Release` を呼び、補充エントリの公開に成功するとスロットがカーネルに返却される。`iox.ErrWouldBlock` が返った場合はバッファを保持し、後で CQE 経路上で解放を再試行する。長さゼロの EOF バッファには補充用の識別情報がなく、エントリを公開せずに解放される。ベストエフォートでの停止を要求するには `false` を返す。
    - `OnError`: CQE エラーを受け取る。ベストエフォートでの停止を要求するには `false` を返す。
    - `OnStopped`: 状態が `Stopped` に移行する直前に一度だけ呼ばれる。
4. `Stop` を呼び出して非同期キャンセルを発行する。レシーバは `Stopping` → `Retiring` → `Stopped` と遷移する。
5. `Stopped` が `true` を返すまでポーリングし、リングを停止してから `Close` でマップ領域と補充リングのマッピングを解放する。

### 状態機械

```
Idle → Active → Stopping → Retiring → Stopped
```

キャンセルの発行に失敗した場合、`Stop` は `Active` に復帰します。`Close` は冪等です。

### ハンドラの規約

- `OnData` と `OnError` は CQE ディスパッチ goroutine から逐次的に呼び出される。
- `Release` は単一プロデューサであり、ディスパッチ goroutine からのみ呼び出すこと。
- `Stop` は CQE ディスパッチと同時に実行されないことが保証されている状況でのみ呼び出すこと。これは呼び出し側が守るべき直列化の規約である。

## 使用例

`uring/examples/` にあるテストが現行 API の具体的な使い方を示しています。

- `multishot_test.go`: マルチショット accept・receive とサブスクリプション停止の挙動
- `file_io_test.go`: ファイルの read・write・バッチ処理
- `fixed_buffers_test.go`: 登録バッファと固定バッファ I/O
- `vectored_io_test.go`: ベクトル化 read/write
- `splice_tee_test.go`: splice・tee によるゼロコピーデータ転送
- `zerocopy_test.go`: ゼロコピー send と完了の追跡
- `poll_test.go`: poll による準備完了ワークフロー
- `buffer_ring_test.go`: バッファリング供給とマルチサイズバッファグループ
- `context_test.go`: direct・indirect・extended の各 `SQEContext` フローと `CQEView` アクセス
- `echo_test.go`: TCP echo サーバと UDP ping-pong
- `timeout_linux_test.go`: タイムアウトとリンクタイムアウト

パッケージレベルの `listener_example_test.go` はリスナー構築と multishot accept を、`socket_integration_linux_test.go` は TCP クライアントの connect/send フローをそれぞれ示しています。

## 運用上の注意

- すべての成功操作に可視の CQE が必要な場合は `NotifySucceed` を有効にする。
- `ring.Features` は実際の SQ エントリ数・CQ エントリ数・SQE スロット幅・`user_data` 解釈時のバイト順を返す。
- 通常の Go 実行および発行状態の並行呼び出しでは、デフォルトのタスク移動可能な `MultiIssuers=true` 構成（`COOP_TASKRUN`）を維持します。`MultiIssuers=false` は、所有者が `New` より前に `runtime.LockOSThread` を呼び、`Stop` まで固定されたまま、`submit`、`Wait`、`WaitDirect`、`WaitExtended`、`Stop`、`ResizeRings` を直列化する場合に限って使用します。この明示的な高速パスは `SINGLE_ISSUER` + `DEFER_TASKRUN` を要求します。
- `EpollWait` の `timeout` は `0` のままにすること。期限が必要な場合は `LinkTimeout` を使う。
- 借用中の完了参照先やプール由来のコンテキストは速やかに返却すること。
- `ListenerOp.Close` はリスナー FD を即座に閉じる。構築中の CQE が未回収の場合は、その CQE を回収してから再度 `Close` を呼び出し、借用中の `ExtSQE` をプールに返却する。

## 対応プラットフォーム

`uring` の実カーネルパスは Go 1.26+ / Linux 6.18+ を対象としています。実装ファイルとテストの大半は `//go:build linux` で保護されており、Darwin 向けファイルは共有 API 面向けのコンパイルスタブのみを提供します。Linux 専用機能は引き続き Linux 専用であり、上述の Linux 実行時ベースラインを変えません。

## ライセンス

MIT, [LICENSE](./LICENSE) を参照。

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
