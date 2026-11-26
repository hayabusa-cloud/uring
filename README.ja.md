# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go で Linux 6.18+ の `io_uring` カーネル境界を扱うパッケージ。

言語: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | **日本語** | [Français](./README.fr.md)

## 概要

`uring` は Linux `io_uring` へのカーネル境界を提供する Go パッケージです。リングの生成・開始、SQE の組み立て、CQE の解釈、`user_data` による発行元識別の伝達を担い、バッファ登録・マルチショット操作・リスナー構築の基本操作をスケジューラにしないまま公開します。

パッケージは境界を明示に保ちます：カーネル側の機構と完了結果の観測はこの API にとどまり、方針や組み立ては呼び出し側レイヤーが担うという役割分担です。呼び出し側のランタイムコードが、完了ルーティング、ルートの終了処理、再試行とバックオフ、ハンドラとセッションのルーティング、安全性チェック、コネクションライフサイクル、終端時のリソース解放を担当します。

主な API は以下のとおりです。

- `Uring`: リングハンドルと操作メソッド群
- `SQEContext` は `user_data` に格納する投入時のコンテキスト
- `CQEView`: `Wait` が返すコピー済み完了記述子
- 登録バッファおよびマルチサイズ buffer group によるバッファ供給

## インストール

`uring` のビルドには Go 1.27 以降、カーネルの `io_uring` を使うには Linux 6.18 以降が必要です。まず実行中のカーネルのバージョンを確認してください。

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

リング生成時に `ENOMEM`・`EPERM`・`ENOSYS` が返る場合、memlock 上限、sysctl 設定、またはカーネルのサポート状況が原因です。[Docker のデフォルト seccomp プロファイル](https://docs.docker.com/engine/security/seccomp/) は `io_uring` システムコールをブロックします。診断と解決手順は [SETUP.md](./SETUP.md) を参照してください。

## 呼び出し側向けガイド

[ファイル I/O の例](./examples/file_io_test.go#L30)では、`Start` から `Stop` までリングを直接操作する流れを示します。操作別の手順は[呼び出し側向けガイド](./agents/INDEX.md)、所有権と検証の要件は[パッケージの指針](./AGENTS.md)を参照してください。

一括投入では、一部の要求が受理された後、残りに対して `iox.ErrWouldBlock` が返ることがあります。対応する CQE の `Res` は操作結果を示し、`IORING_CQE_F_MORE` は同じ要求の CQE がさらに届く可能性を示します。このフラグを `iox.ErrMore` と解釈するかは呼び出し側のプロトコルで決まります。カーネルの操作がすでに反映された後でエラーが返ることもあります。例えば `MsgRingFD` では、転送先にファイルが登録された後、転送元の CQE に `-EOVERFLOW` が返る場合があります。[ファイル転送時の復旧](./agents/boundary/resources.md#inter-ring-file-transfers)

## リングのライフサイクル

`New` は未開始のリングとコンテキストプールを作成します。デフォルトの `MultiIssuers=false` では、所有者の goroutine を `Start` の前に一つの OS スレッドへ固定し、`Stop` が完了するまで固定を保ちます。`Start` を呼んでリソースを登録し、操作の発行を有効にします。

次の例はリングが所有する登録済みバッファへ読み込みます。そのアドレスは `Stop` まで変わりません。完了後、バッファを再利用する前にデータを処理します。`iox.IsWouldBlock` で進展がない状態を判定し、呼び出し側の `iox.Backoff` で CPU 負荷を抑えて待機します。

この例は回復不能なエラーが発生すると `log.Fatal` でプロセスを終了します。サービスでは未完了のリクエストを保持し、すべての完了を処理してからリングを破棄します。

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

fd := iofd.FD(file.Fd()) // 借用した FD。Close の責任は file が保持する。
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
runtime.KeepAlive(file) // 読み取りが完了するまで、FD を所有する file を生存させる。
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

`Wait` は保留中の SQE をフラッシュしてから完了を回収します。単一発行者リングでは、SQ が空になった後も遅延タスクを進めるため kernel enter を発行します。呼び出し側は `Wait`、`WaitDirect`、`WaitExtended` と他の発行状態操作を直列化してください。`iox.IsWouldBlock(err)` は、現時点で観測可能な完了がないことを示します。

`Start` と `Stop` がリングのライフサイクルを構成します。`Stop` は冪等ですが、呼び出すとリングは恒久的に使用不能になります。進行中の操作をすべて完了させ、未回収の CQE を回収し、multishot サブスクリプションを停止してから呼び出してください。

## 型と操作

| 型                     | 役割                                              |
|-----------------------|-------------------------------------------------|
| `Uring`               | リングの初期化・発行・完了回収と操作メソッド                          |
| `Options`             | リングエントリ数、登録バッファ予算、buffer group スケール、完了の可視性      |
| `SQEContext`          | `user_data` に格納する投入時のコンテキスト |
| `CQEView`             | デコード済みアクセサを持つコピー済み完了記述子。モード依存の参照先は借用       |
| `ListenerOp`          | リスナー作成操作のハンドル。FD の保持と accept ヘルパーを提供            |
| `BundleIterator`      | バンドル受信で消費されたバッファの走査                             |
| `BufferRing`          | 明示的に登録する不透明な提供バッファリング。`Add` で準備し、`Advance` で公開 |
| `IncrementalReceiver` | インクリメンタル buffer-ring 受信の管理（`IOU_PBUF_RING_INC`） |
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
| ソケット I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `SendMsgHeader`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy`                                                       |
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

`PollAddLevel` と `PollAddMultishotLevel` は常に `ErrNotSupported` を返します。Linux io_uring は `IORING_OP_POLL_ADD` でレベルフラグを受け付けません。

`Uring.Close` は指定したファイルディスクリプタに `IORING_OP_CLOSE` を発行します。リング自体の破棄ではありません。

## コンテキスト伝達

`Multicast` はペイロードサイズと宛先数に応じて通常送信とゼロコピー送信を自動選択します。`MulticastZeroCopy` はより低いしきい値でゼロコピーを優先します。どちらも Direct コンテキストを要求し、`(submitted, error)` を返します。`submitted` は先頭から受理された宛先数です。バックプレッシャー時は残りの宛先から再開します。再開時に選択される操作が変わる場合があるため、各 CQE のオペコードとフラグを確認してください。`Multicast` の通常送信は `NotifySucceed` に従います。選択されたゼロコピー送信と `MulticastZeroCopy` の両経路は完了の観測を維持し、明示的な `IOSQE_CQE_SKIP_SUCCESS` を拒否します。

ストリーム送信が一部しか完了しなかった場合は、その送信の未送信部分だけを再送してください。通常送信のバイト数を追跡するには、成功時の CQE を有効にします。共有データは、受理されたすべての送信が解放するまで変更できません。通常送信では完了時に解放されます。`SEND_ZC` では `IORING_CQE_F_MORE` のない最終データ CQE、またはデータ CQE と対応する通知の両方が必要です。[複数の宛先への送信例](./examples/zerocopy_test.go)

Direct モードの `SQEContext` は、オペコード、SQE フラグ、バッファグループ ID、ファイル記述子を 64 ビットに格納します。これらの値が同じ送信が同時に進行すると、`user_data` も同じになります。結果を送信ごとに対応付けるには、そのような送信を直列化するか、それぞれ異なる有効なコンテキストで個別に投入してください。

```go
sqeCtx := uring.ForFD(int32(fd)).
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

`Wait` は CQE の回収時に発生したエラーを返します。失敗したリクエストの分も含め、返された CQE をすべて処理してください。ローカルのリクエストでは、`cqe.Context()` で発行時のトークンを取得できます。操作結果の判定には `cqe.Err()` を使います。ゼロコピー送信の通知では、`Res` にゼロコピーの使用状況を示すフラグが入ることがあります。CQE のフラグでリクエストとバッファのライフタイムを判断します。

送信側と受信側は `messageID` を `MsgRing` 専用に予約します。この例のリングでは、それ以外の ID はすべてローカルのリクエストを表します。まず `messageID` を照合し、一致したら送信側が指定した符号付きの `Res` を、負の値も含めてそのままメッセージハンドラに渡します。

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

完了時、`CQEView` は対応するコンテキストモードをオンデマンドでデコードします。そのスカラー記述子はコピーされます。Indirect/Extended モードの参照先と借用バッファは、それぞれ定められたライフタイムを超えて保持しないでください。

## バッファ供給

`uring` には実用上 3 つのバッファ経路があります。登録バッファはリング開始時に固定され、固定バッファのファイル I/O で使います。提供バッファリングではカーネルが受信バッファを選び、選択されたバッファ ID を CQE に返します。バンドル受信は公開順にバッファを消費します。再利用後は ID の数値順と一致するとは限りません。`Uring.BundleIterator` は実際の順序を保持します。単独の `NewBundleIterator` には、リングサイズを法とする連続した ID 順の公開が必要です。

- `ReadBufferSize`・`ReadBufferNum` による固定サイズの提供バッファ
- `MultiSizeBuffer` によるマルチサイズバッファグループ
- `LockedBufferMem`・`RegisteredBuffer`・`ReadFixed`・`WriteFixed` による登録固定バッファ

多くのシステムでは、設定ヘルパーから始めるのが手っ取り早い入り口です。

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

`WriteFixed` は、インデックスで指定した登録済みバッファから書き込みます。完了まではファイルを開いたままにし、投入したバイト列を変更しないでください。CQE を見てバッファの再利用を判断する場合は、`NotifySucceed` を有効にし、`IOSQE_CQE_SKIP_SUCCESS` は指定しません。

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.FD(file.Fd()) // 借用した FD。Close の責任は file が保持する。
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

バンドル受信では、1 件の CQE で複数の提供バッファを消費することがあります。この例のようなストリームソケットでは、EOF や受信エラーでも、選択済みのスロットがデータ長 0 で返る場合があります。データを読み終えたら、イテレータのコピー全体で `Recycle` を 1 回だけ呼び、消費したスロットをすべて返します。

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

登録バッファにはピン留めされたメモリが必要です。大きなバッファの登録に失敗する場合は `RLIMIT_MEMLOCK` を引き上げるか、メモリ予算を小さくしてください。

## マルチショットとリスナー操作

`AcceptMultishot`、`ReceiveMultishot`、`SubmitAcceptMultishot`、`SubmitAcceptDirectMultishot`、`SubmitReceiveMultishot`、`SubmitReceiveBundleMultishot` がマルチショットのソケット操作を発行します。

CQE のルーティングは呼び出し側が担当します。`ListenerOp.HandleCQE` は SOCKET → BIND → LISTEN を進め、各段階のコールバックを呼び出します。手動で進める場合は、引き続き `DecodeListenerCQE`、`PrepareListenerBind`、`PrepareListenerListen`、`SetListenerReady` を利用できます。[リスナーガイド](agents/recipes/listener.md)を参照してください。

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

デフォルトのリングのイベントループは、`Start` の前に OS スレッドへ固定した goroutine で実行し、`Stop` まで固定を保ちます。ループはリング操作を直列化して保留中の処理を発行し、`Wait` が観測可能な進展を返さないときは呼び出し側の `iox.Backoff` を適用して、完了を振り分けます。

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

`OnStep` コールバックは 1 件の `MultishotStep` を観察します。ストリームを維持するなら `MultishotContinue` を、キャンセルを要求するなら `MultishotStop` を返します。コールバックが終端観察まで有効なままなら、`OnStop` は最終エラーと `cancelled` フラグを伴って高々一度だけ実行されます。ステップ自身の `step.Cancelled` は、境界上でカーネルが `-ECANCELED` を返した判定をその他の失敗と明示的に区別します。これらのコールバックはそれぞれ `MultishotHandler.OnMultishotStep` と `MultishotHandler.OnMultishotStop` に対応します。インタフェースを直接実装することもできます。`HandleCQE` は呼び出し側の直列化された完了ループ内で即時 dispatch するための補助です。コピー済み CQE をそのループの後も保持する場合、呼び出し側が独自の route state を保持し、すでに解放されたサブスクリプションの観察を拒否してください。デフォルトの単一発行者リング（`MultiIssuers=false`）では、OS スレッドに固定したリング所有者から `Cancel` / `Unsubscribe` を呼び、発行、`Wait`、`WaitDirect`、`WaitExtended`、`Stop`、`ResizeRings` と直列化してください。`MultiIssuers=true` のリングでは、共有発行パスがキャンセル SQE を直列化します。

投入済みのキャンセル CQE は、`SubscriptionStopped` または `OnStop` の後も、同じサブスクリプションの `HandleCQE` またはその拡張ハンドラへ渡してください。投入の失敗や何もしない呼び出しでは、新たなキャンセル CQE の処理義務は生じません。サブスクリプションは両コンテキストを所有し、元の終端 CQE の処理、キャンセルの dispatch、キャンセル投入処理がすべて終わってから、元のコンテキストをプールへ返します。これらのコンテキストを手動で返さないでください。[受信の例](./examples/multishot_test.go)は、自然な完了とハンドラからのキャンセルを示します。

### 型付きコンテキストによるコネクション状態

Extended コンテキストは、送信時の接続状態を完了まで引き継げます。この例では、成功した `Send` の CQE を受け取ってからコンテキストをプールに返すため、`NotifySucceed` を有効にし、`IOSQE_CQE_SKIP_SUCCESS` は指定しません。

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

対応する `SEND` の CQE を受け取ったら、接続状態への参照とシーケンス番号をコピーしてからコンテキストをプールに返します。

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

ライブな Go ポインタルートは `UserData` の外側で到達可能に保ってください。GC はそれらの生バイトをトレースしません。内部のマルチショットおよびリスナープロトコルでは各 `ExtSQE` スロットに付属するサイドカーのルートセットが処理しますが、型付き参照を配置する呼び出し側ランタイムコードは独自に到達可能性を維持する必要があります。

### デッドライン合成

`LinkTimeout` は、直前の `IOSQE_IO_LINK` 付き SQE に期限を設定します。起動済みのリングを単一の所有者が操作し、空き数の確認から 2 つの SQE の投入までの間に、他の投入や `Wait` を挟まないようにします。次の例の戻り値は、完了を回収する必要のある要求の数です。空でない受信バッファを渡し、受信 CQE を回収するまでそのアドレスを固定してください。

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

投入数が 0 で `iox.ErrWouldBlock` が返ったら、リングを駆動してから 2 件とも再試行します。受信だけがキューに入った場合は、他の要求を追加する前にその受信をカーネルに渡し、完了まで `buf` を保持します。2 件とも入った場合は両方の CQE を回収します。期限切れはキャンセルを試みるためのもので、受信の結果は受信自身の CQE で確認します。[リンクタイムアウトの例](./examples/timeout_linux_test.go#L259)

## TCP の使用パターン

テストと併せて読める最短のフローを以下に示します。

| シナリオ     | 主な API                                                           | 参照                                                                                |
|----------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Echo サーバ | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| クライアント   | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### TCP Echo サーバ

`ListenerManager` は最初の SOCKET リクエストを投入します。構築中の各 CQE を `listenerOp.HandleCQE` に渡すと、コールバックの呼び出しと BIND、LISTEN の投入が自動的に行われます。`iox.ErrWouldBlock` が返った場合はリングを駆動し、`ring.SubmitExtended(uring.PackExtended(listenerOp.Ext()))` で準備済み SQE の投入を再試行してください。同じ CQE を再処理してはいけません。`OnListening` が成功を通知した後に接続の受け付けを開始します。

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

サブスクリプションを停止するときは `Cancel` を呼び、終端 CQE と、キャンセルを投入した場合はその CQE を処理するまでディスパッチを続けてください。両方のサブスクリプションをドレインしてから FD を閉じ、リングを停止します。[リスナーの例](./listener_example_test.go) はセットアップから終了処理までを示し、[multishot の例](./examples/multishot_test.go) と [echo の例](./examples/echo_test.go) は接続処理を示します。

### TCP クライアント

ソケットを作成し、`IORING_OP_SOCKET` の完了を待ちます。返された FD を `iofd.FD` に変換したうえで `Connect`、`Send`、`Receive` に渡します。

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

この例の `Send` では `NotifySucceed` を有効にし、`IOSQE_CQE_SKIP_SUCCESS` は指定しません。結果に依存する次の操作へ進む前に、`Wait` で対応する CQE を確認します。送信完了までデータを保持し、部分送信の場合は未送信部分だけを再送します。[TCP クライアント](./examples/socket_test.go) · [エコーの例](./examples/echo_test.go)

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

`examples/` の実行可能なテストでは、公開 API を直接呼び出し、初期化 → 発行 → 完了 → 解放を示します。まずコンテキストとファイル I/O を学び、次にソケットと multishot の例で所有権の規則を確認してください。

- [context_test.go](./examples/context_test.go): Direct・Indirect・Extended コンテキスト、完了のデコード、借用コンテキストの返却
- [completion_test.go](./examples/completion_test.go): 発行エラーと完了エラーの区別、リクエストの識別、成功と失敗が混在するバッチの回収
- [file_io_test.go](./examples/file_io_test.go): ファイルの読み書き、EOF までのチャンク読み取り、一括発行
- [file_lifecycle_test.go](./examples/file_lifecycle_test.go): 完了を確認しながらファイルを開く、書き込む、同期する、閉じる
- [vectored_io_test.go](./examples/vectored_io_test.go): scatter/gather の読み書き
- [fixed_buffers_test.go](./examples/fixed_buffers_test.go): 登録バッファと fixed-buffer I/O
- [buffer_ring_test.go](./examples/buffer_ring_test.go): bundle 受信、増分受信、バッファの再利用
- [socket_test.go](./examples/socket_test.go): クライアントソケットの生成、接続完了、未送信部分の送信
- [echo_test.go](./examples/echo_test.go): TCP echo、ストリーム I/O の部分完了、UDP データグラム
- [multishot_test.go](./examples/multishot_test.go): accept/receive の繰り返し完了とキャンセル後の回収
- [zerocopy_test.go](./examples/zerocopy_test.go): ゼロコピー送信、multicast の自動選択、結果と解放の独立した追跡
- [poll_test.go](./examples/poll_test.go): readiness の観測と poll のキャンセル
- [timeout_linux_test.go](./examples/timeout_linux_test.go): 満了、キャンセル、リンクタイムアウト
- [splice_tee_test.go](./examples/splice_tee_test.go): 完了に基づく splice と tee の転送

パッケージ直下の [listener_example_test.go](./listener_example_test.go) は自動 SOCKET → BIND → LISTEN と multishot accept を、[socket_integration_linux_test.go](./socket_integration_linux_test.go) は TCP connect/send を示します。

## 運用上の注意

- `NotifySucceed` を有効にすると、書き込み系操作の成功 CQE がデフォルトで保持されます。明示した `IOSQE_CQE_SKIP_SUCCESS` フラグは引き続き適用されます。
- `ring.Features` は実際の SQ エントリ数・CQ エントリ数・SQE スロット幅・`user_data` 解釈時のバイト順を返す。
- デフォルトの `MultiIssuers=false` は `SINGLE_ISSUER` + `DEFER_TASKRUN` を要求し、共有発行の同期処理を省きます。所有者は `Start` の前に `runtime.LockOSThread` を呼び、`Stop` まで同じ OS スレッドに固定したまま、発行、待機、リサイズ、終了処理を直列化します。発行を共有する場合や OS スレッド間の移動を許す場合は、`MultiIssuers=true`（`COOP_TASKRUN`）を明示的に選択します。
- `EpollWait` の `timeout` は `0` のままにすること。期限が必要な場合は `LinkTimeout` を使う。
- リクエストの全完了処理と借用アクセスが終了してから、プールのコンテキストを返却すること。
- `ListenerOp.Close` はリスナー FD を即座に閉じる。構築中の CQE が未回収の場合は、その CQE を回収してから再度 `Close` を呼び出し、借用中の `ExtSQE` をプールに返却する。

## 対応プラットフォーム

カーネルを使う実装には Go 1.27 以降と Linux 6.18 以降が必要です。Darwin ビルドには共有 API の一部を実行するユーザー空間のシミュレーターが含まれます。CI では Darwin 向けにコンパイルし、カーネルの動作はネイティブ Linux 上で検証します。

## ライセンス

MIT, [LICENSE](./LICENSE) を参照。

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
