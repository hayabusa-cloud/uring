# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go で Linux 6.18+ の `io_uring` カーネル境界を扱うパッケージ。

言語: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | **日本語** | [Français](./README.fr.md)

## 概要

`uring` は Linux `io_uring` のカーネル境界を提供するワークスペースパッケージです。リングの生成・開始、SQE の組み立て、CQE
の解釈、`user_data` による発行元識別の伝達を担い、バッファ登録・マルチショット操作・リスナー構築の基本操作も備えています。

設計方針として、カーネル側の機構と完了結果の観測を API
境界に留め、スケジューリングやリトライなどの方針決定は上位層に委ねます。呼び出し側のランタイムコードが、完了の相関付け、再試行とバックオフ、ハンドラとセッションのルーティング、コネクションライフサイクル、終端時のリソース解放を担当します。

主な API は以下のとおりです。

- `Uring`: リングハンドルと操作メソッド群
- `SQEContext`: `user_data` で運ばれる発行元識別子
- `CQEView`: `Wait` が返す完了結果の借用ビュー
- 登録バッファおよびマルチサイズ buffer group によるバッファ供給

## インストール

`uring` の動作には Linux カーネル 6.18 以上が必要です。まず動作中のカーネルバージョンを確認してください。

```bash
uname -r
```

`uring` は 6.18+ のベースラインを前提とし、古いカーネル向けのフォールバック分岐を持ちません。そのため、古いカーネル向けの互換分岐ではなく、要件を満たすカーネルを起動してください。

Debian 13 の stable カーネルはこの要件を満たさない場合があります。6.18 以上の Debian
パッケージ版カーネルが必要な場合は、下記の [Debian 13 カーネル更新](#debian-13-カーネル更新) を参照してください。

```bash
go get code.hybscloud.com/uring
```

### Debian 13 カーネル更新

Debian 13 の安定版トラックが提供するカーネルは 6.12 です。`trixie-backports` スイートから Debian パッケージ済みの 6.18+
カーネルを導入できます。手順の詳細は [SETUP.md](./SETUP.md) を参照してください。

### トラブルシューティング

リング生成時に `ENOMEM`・`EPERM`・`ENOSYS` が返る場合、memlock 上限、sysctl 設定、またはカーネルのサポート状況が原因です。コンテナランタイムはデフォルトで
`io_uring` システムコールをブロックします。診断と解決手順は [SETUP.md](./SETUP.md) を参照してください。

## リングのライフサイクル

`New` は未開始状態のリングを返します。操作の発行に先立ち `Start` を呼び出してください。`New` がコンテキストプールを即座に構築し、
`Start` がリングリソースの登録と有効化を行います。次の例ではファイル read を発行し、対応する CQE を待ち、`iox.Classify`
によって `ErrWouldBlock` を失敗ではなく「進展なし」の意味として扱います。

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
        if cqe.Res < 0 {
            return fmt.Errorf("uring read failed: res=%d", cqe.Res)
        }
        handle(buf[:int(cqe.Res)])
        return nil
    }
}
```

`Wait` は未送信の SQE をフラッシュしてから完了を回収します。単一発行者リングでは、SQ が空になったあとも遅延タスクを進めるためのカーネル
enter も発行します。呼び出し側は `Wait`/`enter` と他の発行状態操作を直列化する必要があります。
`Wait` が返した `err` を `iox.Classify(err)` で分類して `iox.OutcomeWouldBlock` になった場合、それは現時点で
境界上に観測可能な完了がないことを示します。

`Start` と `Stop` がリングのライフサイクルを構成します。`Stop` は冪等ですが、呼び出すとリングは恒久的に使用不能になります。進行中の操作をすべて完了させ、未回収の
CQE を回収し、multishot サブスクリプションを停止してから呼び出してください。

## 型と操作

| 型                     | 役割                                              |
|-----------------------|-------------------------------------------------|
| `Uring`               | リングの初期化・発行・完了回収と操作メソッド                          |
| `Options`             | リングエントリ数、登録バッファ予算、buffer group スケール、完了の可視性      |
| `SQEContext`          | `user_data` に格納されるコンパクトな発行元識別子                  |
| `CQEView`             | デコード済みコンテキストアクセサを持つ完了レコードの借用ビュー                 |
| `ListenerOp`          | リスナー作成操作のハンドル。FD の保持と accept ヘルパーを提供            |
| `BundleIterator`      | バンドル受信で消費されたバッファの走査                             |
| `IncrementalReceiver` | インクリメンタル buffer-ring 受信の管理（`IOU_PBUF_RING_INC`） |
| `ZCTracker`           | ゼロコピー送信の 2-CQE ライフサイクルの追跡                       |
| `ContextPools`        | indirect・extended 発行コンテキストのプール                  |
| `ZCRXReceiver`        | NIC RX キューからのゼロコピー受信ライフサイクルの管理                  |
| `ZCRXConfig`          | ZCRX 受信インスタンスの設定                                |
| `ZCRXHandler`         | ZCRX のデータ・エラー・シャットダウンに対するコールバックインターフェース         |
| `ZCRXBuffer`          | 配送済みゼロコピー受信ビュー。解放時にカーネルが自動補充                    |

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
| コマンド     | `UringCmd`, `UringCmd128`, `Nop`, `Nop128`                                                                                                                          |

`Nop128` と `UringCmd128` は `Options.SQE128` 付きで作成したリングを必要とし、対応する opcode
がカーネルで利用可能でなければなりません。条件を満たさない場合は `ErrNotSupported` を返します。

`Uring.Close` は指定したファイルディスクリプタに `IORING_OP_CLOSE` を発行します。リング自体の破棄ではありません。

## コンテキスト伝達

`SQEContext` は `uring` の主要な識別トークンです。direct モードでは opcode・SQE flags・buffer group ID・ファイルディスクリプタを
64 ビット値ひとつに格納します。

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

通常は `ForFD` または `PackDirect` から開始し、完了時に参照したい情報だけを付加します。`WithFlags`
はフラグ集合全体を上書きするため、論理和は事前に計算してから渡してください。

direct の 64 ビット配置に収まらない独自メタデータが必要な場合は、`ExtSQE` を借用し、`Ctx*Of` や `ViewCtx*` で `UserData`
に書き込んだうえで `SQEContext` に再パックします。`UserData` にはスカラー値を格納するのが望ましく、Go
ポインタ、インターフェース値、関数値、スライス、文字列、マップ、チャネル、またはそれらを含む構造体を生のオーバーレイや型付きビュー経由で置く場合は、GC
が生バイトを走査しないため、参照元を `UserData` の外に保持する必要があります。

```go
ext := ring.ExtSQE()
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
```

`NewContextPools` はすぐに使えるプールを返します。`Reset` は、借用中のコンテキストをすべて返却し終えたあとにプールを再利用する場合にのみ呼び出してください。

### `CQEView` による完了ディスパッチ

`uring` は完了コンテキスト専用の型を持ちません。完了のディスパッチは `CQEView` を介して行い、発行時のトークンが必要であれば
`cqe.Context()` で取得します。

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

完了時、`CQEView` は対応するコンテキストモードをオンデマンドでデコードします。`CQEView`・`IndirectSQE`・`ExtSQE`
・借用バッファは、それぞれ定められたライフタイムを超えて保持しないでください。

## バッファ供給

`uring` には実用上 3 つのバッファ経路があります。登録バッファはリング開始時に固定され、固定バッファのファイル I/O
で使います。提供バッファリングではカーネルが受信バッファを選び、選択されたバッファ ID を CQE に返します。バンドル受信は
1 つの CQE で連続した論理バッファ範囲を消費し、その範囲を `BundleIterator` で公開します。

- `ReadBufferSize`・`ReadBufferNum` による固定サイズの提供バッファ
- `MultiSizeBuffer` によるマルチサイズバッファグループ
- `LockedBufferMem`・`RegisteredBuffer`・`ReadFixed`・`WriteFixed` による登録固定バッファ

多くの場合、設定ヘルパーから始めるのが簡単です。

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
```

メモリ予算を明示的に指定したい場合は `OptionsForBudget` を、予算に対して選択される階層構成を確認したい場合は
`BufferConfigForBudget` を使用してください。

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

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

ソケット受信でカーネルのバッファ選択を使う場合は、受信バッファとして `nil` を渡し、必要なサイズクラスを指定します。完了
CQE には選択されたバッファが記録されます。

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

バンドル受信は同じ提供バッファストレージを使いますが、1 つの CQE
で複数のバッファを消費することがあります。イテレータを処理したら、消費したスロットを回収してください。

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

登録バッファにはピン留めされたメモリが必要です。大きなバッファの登録に失敗する場合は `RLIMIT_MEMLOCK`
を引き上げるか、メモリ予算を小さくしてください。

## マルチショットとリスナー操作

`AcceptMultishot`・`ReceiveMultishot`・`SubmitAcceptMultishot`・`SubmitAcceptDirectMultishot`・`SubmitReceiveMultishot`・
`SubmitReceiveBundleMultishot` がマルチショットのソケット操作を発行します。

CQE のルーティング方針はパッケージ外に委ねています。リスナーの構築は `DecodeListenerCQE`・`PrepareListenerBind`・
`PrepareListenerListen`・`SetListenerReady` の順で進みますが、完了のディスパッチ方法や連鎖の停止条件は呼び出し側が決定します。

## 実装構造

実装の境界は以下のように分かれています。

1. `New`: 無効状態のカーネルリングを構築し、コンテキストプールを確保し、バッファ戦略を決定する。
2. `Start`: バッファを登録し、Linux 6.18+ ベースライン向けにリングを有効化する。
3. 操作メソッド: SQE を書き込んで操作の意図を発行する。
4. `Wait`: 未送信分をフラッシュし、借用された CQE 完了結果を返す。
5. 呼び出し側ランタイムコード: スケジューリング、リトライ、待機、コネクションとセッションのルーティング、終端時のリソース方針を担う。

この構造により、`uring` はカーネル境界の機構に専念しつつ、境界を越えた完了結果の意味を保持します。

## ランタイム境界

`uring` の上位にあるランタイム層は、これをスケジューラではなくカーネルバックエンドとして扱うべきです。理想的な境界は一方向です。
`uring` は SQE の準備、CQE の回収、`user_data` の保持、CQE `res` とフラグの公開、所有権事実の報告を担います。呼び出し側ランタイムコードは、それらの観測を自身の
トークンと相関付け、再試行とバックオフを適用し、ハンドラとセッションをルーティングし、発行をバッチ化し、終端リソースを解放します。

抽象実行が完了事実を必要とする場合、ランタイムブリッジは Extended モード CQE を消費できます。コネクション単位のランタイムは、CQE
の結果、フラグ、バッファ ID、エンコード済みトークンを必要とする場合、イベントをハンドラコールバックに還元する前に生の
Extended CQE
を直接ポーリングできます。

この境界の上にあるコンテキスト層と抽象実行層は、`uring` のカーネル境界としての役割を変更しません。

## アプリケーション層の設計パターン

`uring` はカーネルに面した機構を公開します。スケジューリング、リトライ、コネクション追跡、プロトコル解釈は上位の層が担います。以下のパターンは呼び出し側ランタイムが保持すべき境界を示します。

### リング所有イベントループ

シングルイシュアーモード（デフォルト）では、1 つの goroutine がすべての発行側操作を直列化します。一般的なループは保留中のワークを発行し、
`Wait` が観測可能な進展を返さないときは呼び出し側の `iox.Backoff` を適用し、完了を振り分けます。

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

`Send`、`Receive`、`AcceptMultishot`、`Wait` などのリングメソッドはすべてこの goroutine 上で実行します。他の goroutine
からの作業はチャネルまたはロックフリーキューを経由してループに渡します。リングメソッドを直接呼び出してはなりません。
`iox.Backoff` は呼び出し側が所有します。`Wait` が `iox.OutcomeWouldBlock` に分類された場合、または CQE を 1 件も回収できなかった場合は
`backoff.Wait()` を呼び、`n > 0` のバッチを回収できたら `backoff.Reset()` を呼びます。

### マルチショットサブスクリプションのライフサイクル

マルチショット操作は、カーネルが最終 CQE（`IORING_CQE_F_MORE` なし）を送るまで CQE
のストリームを生成します。呼び出し側ランタイムコードがサブスクリプションを追跡し、再発行を管理します。

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

`OnMultishotStep` は各完了を観察します。ストリームを維持するなら `MultishotContinue` を、キャンセルを要求するなら
`MultishotStop` を返します。`OnMultishotStop` は終端状態で一度だけ実行されます。クリーンアップと条件付き再サブスクリプションに使用します。

### 型付きコンテキストによるコネクション状態

拡張コンテキストは、グローバルルックアップテーブルを介さずに、発行 → 完了のラウンドトリップ全体にわたってコネクション単位の参照を伝搬します。

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

完了時に同じ型付きビューを通じて状態を復元します。

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

ライブな Go ポインタルートは `UserData` の外側で到達可能に保ってください。GC はそれらの生バイトをトレースしません。内部のマルチショットおよびリスナープロトコルでは各
`ExtSQE` スロットに付属するサイドカーのルートセットが処理しますが、型付き参照を配置するフレームワークコードは独自に到達可能性を維持する必要があります。

### デッドライン合成

`LinkTimeout` は `IOSQE_IO_LINK` チェーンを通じて直前の SQE にデッドラインを付与します。操作とタイムアウトが競合し、一方が完了すると他方はキャンセルされます。

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

フレームワーク層は両方の結果を処理します。受信成功はタイムアウトをキャンセルし、タイムアウト発火は受信をキャンセルします。いずれも
CQE を生成するため、ディスパッチループで観察する必要があります。

## TCP の使用パターン

テストと併せて読める最短のフローを以下に示します。

| シナリオ     | 主な API                                                           | 参照                                                                                |
|----------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo サーバ | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| クライアント   | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP Echo サーバ

socket → bind → listen の一連の手順をパッケージに任せるには `ListenerManager` を使います。リスナーの準備が完了したら、接続済み
FD に対して multishot accept と multishot receive を開始します。

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

`listener_example_test.go` はリスナーの構築と multishot accept、`examples/multishot_test.go` はハンドラ側の multishot
receive CQE の処理、`examples/echo_test.go` はループバック echo の一連の流れをそれぞれ示しています。

### TCP クライアント

ソケットを作成し、`IORING_OP_SOCKET` の完了を待ちます。返された FD を `iofd.FD` に変換したうえで `Connect`・`Send`・
`Receive` に渡します。

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

各発行のあとは「リングのライフサイクル」節で示した `Wait` ループで対応する完了を取得します。パッケージレベルの
`socket_integration_linux_test.go` に connect/send の一連の流れがあります。

## ゼロコピー受信（ZCRX）

`ZCRXReceiver` は `io_uring` を通じて NIC ハードウェア RX キューからゼロコピー受信を行う仕組みを管理します。

`NewZCRXReceiver` は 32 バイト CQE（`IORING_SETUP_CQE32`）で作成されたリング向けに用意されています。現在の `Options` はこの
設定フラグを公開していないため、標準の `New` で作成したリングに対してはこのコンストラクタは `ErrNotSupported`
を返します。CQE32 の設定経路が公開されるまでは、この節は実行可能な公開セットアップ手順ではなく、レシーバの境界契約を説明するものです。

### ライフサイクル

1. CQE32 対応リング上で `NewZCRXReceiver` を生成する。コンストラクタが ZCRX インターフェースキューの登録、補充
   領域のマッピング、補充リングの準備を行う。
2. `Start` を呼び出し、リング上で拡張 `RECV_ZC` 操作を発行する。
3. CQE のディスパッチ経路で ZCRX 完了が `ZCRXHandler` に届く。
    - `OnData`: NIC マップ領域を指す `ZCRXBuffer` を受け取る。処理後に `Release` を呼んでカーネルにスロットを返却する。ベストエフォートでの停止を要求するには
      `false` を返す。
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

パッケージレベルの `listener_example_test.go` はリスナー構築と multishot accept を、`socket_integration_linux_test.go` は
TCP クライアントの connect/send フローをそれぞれ示しています。

## 運用上の注意

- すべての成功操作に可視の CQE が必要な場合は `NotifySucceed` を有効にする。
- `ring.Features` は実際の SQ エントリ数・CQ エントリ数・SQE スロット幅・`user_data` 解釈時のバイト順を返す。
- 既定では `MultiIssuers` を無効のまま、単一発行者構成（`SINGLE_ISSUER` + `DEFER_TASKRUN`）を使用する。この構成では、呼び出し側が
  1 つの実行パスで発行状態操作（`submit`・`Wait`/`enter`・`Stop`・resize）を直列化する。複数 goroutine から並行して
  submit や wait 側 enter を行う必要がある場合にのみ `MultiIssuers` を有効にし、`COOP_TASKRUN` 構成に切り替える。
- `EpollWait` の `timeout` は `0` のままにすること。期限が必要な場合は `LinkTimeout` を使う。
- 借用中の完了ビューやプール由来のコンテキストは速やかに解放すること。
- `ListenerOp.Close` はリスナー FD を即座に閉じる。構築中の CQE が未回収の場合は、その CQE を回収してから再度 `Close`
  を呼び出し、借用中の `ExtSQE` をプールに返却する。

## 対応プラットフォーム

`uring` の実カーネルパスは Go 1.26+ / Linux 6.18+ を対象としています。実装ファイルとテストの大半は `//go:build linux`
で保護されています。Darwin 向けファイルは共有 API 面向けのコンパイルスタブのみを提供します。Linux 専用機能は引き続き
Linux 専用であり、上述の Linux 実行時ベースラインを変えません。

## ライセンス

MIT, [LICENSE](./LICENSE) を参照。

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
