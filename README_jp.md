# gomcrest

[English README](README.md)

[gomc-rest](https://github.com/Moge800/gomc-rest)（三菱 PLC 向け MC プロトコル REST ゲートウェイ）の Go クライアントライブラリです。標準ライブラリのみで動き、外部依存はありません。

このライブラリ自体は PLC と直接通信しません。PLC への接続は gomc-rest の 1 本に集約されるので、クライアントは PLC の接続枠を消費しません。readonly・リモート操作の可否・トークン認証も gomc-rest 側で一元管理されます。PLC に直接つなぐ場合は [gomcprotocol](https://github.com/Moge800/gomcprotocol) を使ってください。

## インストール

```bash
go get github.com/moge800/gomcrest
```

Go 1.23 以降が必要です。

## 使い方

```go
c, err := gomcrest.New("http://192.168.0.10:8080", gomcrest.WithToken("s3cret"))
if err != nil {
	return err
}
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

words, err := c.ReadWords(ctx, "D100", 3)   // []uint16
bits, err := c.ReadBits(ctx, "M0", 8)       // []bool
wbits, err := c.ReadBits(ctx, "D100.3", 4)  // ワードデバイスのビットアクセス
dwords, err := c.ReadInt32s(ctx, "D200", 2) // []int32（連続 2 ワードで 1 値）

err = c.WriteWords(ctx, "D100", []uint16{10, 20, 30})
err = c.WriteBits(ctx, "M0", []bool{true, false})
```

全メソッドの第 1 引数は `context.Context` です。タイムアウトとキャンセルは呼び出し側で制御します。

### 型別の読み書き

| 読み出し | 書き込み | Go の型 | 対象 |
| --- | --- | --- | --- |
| `ReadWords` | `WriteWords` | `[]uint16` | ワード |
| `ReadInt16s` | `WriteInt16s` | `[]int16` | 符号付きワード |
| `ReadDWords` | `WriteDWords` | `[]uint32` | ダブルワード（連続 2 ワード） |
| `ReadInt32s` | `WriteInt32s` | `[]int32` | 符号付きダブルワード |
| `ReadBits` | `WriteBits` | `[]bool` | ビットデバイス（`M0`）、ワードのビット（`D100.1`） |

ビットデバイスに `ReadWords` を使うなど、型が合わない場合は `ErrBadResponse` になり、メッセージに使うべきメソッドが出ます。アドレスや範囲のチェックは gomc-rest が行います。

### デバイス種別が実行時に決まる場合

GUI のようにアドレスがユーザー入力で決まるツール向けに、汎用の `Read` / `Write` があります。

```go
v, err := c.Read(ctx, addr, 4, gomcrest.Format{SInt: true})
// ビットなら v.Bools、それ以外は v.Ints（int64）に入る

err = c.Write(ctx, addr, gomcrest.Values{Ints: []int64{-1, 2}}, gomcrest.Format{SInt: true})
```

`Write` の `Values` は `Ints` と `Bools` のどちらか一方だけを入れます。両方空・両方あり の場合は送信せずに `ErrBadRequest` を返します。

### まとめ読み・まとめ書き

```go
res, err := c.RandomRead(ctx, gomcrest.RandomReadRequest{
	Words:  []string{"D100", "D200"},
	DWords: []string{"D300"},
	Bits:   []string{"D100.1", "M0"},
})
// res.Words / res.DWords / res.Bits はリクエスト順

err = c.RandomWrite(ctx, gomcrest.RandomWriteRequest{
	Words: []gomcrest.WordItem{{Addr: "D100", Value: 10}},
	Bits:  []gomcrest.BitItem{{Addr: "M0", Value: true}},
})
```

各リストは最大 255 件で、`Words` と `Bits` 内のワードのビット（`D100.1` など）は合わせて 255 件までです。`Bits` に入れられるビットデバイス（`M0` など）は 16 件までで、1 件ごとに PLC への読み出しが 1 回増えます。M0〜M15 のような連続したビットは `ReadBits(ctx, "M0", 16)` で読み、`RandomRead` は離れたワード・ダブルワードに使ってください。サーバ側で複数回の PLC 読み出しになることがあるため、値が同じスキャンの状態だとは限りません。値の整合が必要なら、PLC のプログラムで連続した領域にまとめてから読んでください。`RandomWrite` は不可分（アトミック）ではありません。サーバはワードとダブルワード、ビットデバイス、ワードのビット（ワードごとに読み出し・変更・書き込み）を、別々の PLC 操作として順に書き込みます。途中で失敗しても、それより前の書き込みは反映されたままなので、再送する前に対象のアドレスを読み直してください。

### K 記法（gomc-rest v1.7.0 以降）

連続するビットデバイスを 1 つの整数として読み書きします（`K4M100` は M100〜M115 の 16 ビット）。K1〜K8 に対応しています。

```go
v, err := c.ReadK(ctx, "K4M100")     // uint32
s, err := c.ReadKInt(ctx, "K4M100")  // int32（幅で符号拡張）
err = c.WriteK(ctx, "K4M100", 1234)
```

### リモート操作

`RemoteRun` / `RemotePause` / `RemoteStop` / `RemoteLatchClear` / `RemoteReset` は PLC の運転状態を変えます。gomc-rest を `-enable-remote` 付き、かつ `-readonly` なしで起動している必要があり、そうでなければ `ErrForbidden` になります。

```go
err = c.RemoteRun(ctx, gomcrest.RunOptions{Clear: gomcrest.ClearNone})
```

書き込みと同じく、エラーが返っても PLC で実行されていないとは限りません。再送する前に[リトライ](#リトライ)を確認してください。

## エラー処理

エラーは `errors.Is` で判定します。

| エラー | 意味 |
| --- | --- |
| `ErrBadRequest` | パラメータ・アドレスの誤り |
| `ErrUnauthorized` | トークンの不足・不一致（401 はすべてこれ） |
| `ErrForbidden` | readonly、またはリモート操作が無効 |
| `ErrPLC` | PLC が終了コードを返した（`APIError.EndCode`） |
| `ErrPLCConnection` | **gomc-rest → PLC** に届かない |
| `ErrBusy` | サーバのキューが満杯で即拒否（PLC では未実行） |
| `ErrQueueClosed` | サーバが終了処理中（PLC では未実行） |
| `ErrRequestCanceled` | サーバ側でキャンセル |
| `ErrRequestTimeout` | サーバ側でタイムアウト |
| `ErrTransport` | **クライアント → gomc-rest** に届かない |
| `ErrBadResponse` | 想定外のレスポンス |

サーバが返したエラーは `*gomcrest.APIError` で、`Status`・`Code`・`Message`・`EndCode` を持ちます。リバースプロキシの HTML エラーページなど gomc-rest 以外の応答は `Code` が空になり、401 以外はどの番兵にも一致しません。番兵に一致するのは、HTTP ステータスも gomc-rest がそのコードで返すもの（各エラーの godoc に記載）である場合だけです。HTTP 500 で `"code":"busy"` のような不整合な応答は、`Code` は保持しますが `ErrBusy` にはならず、安全に再送できるエラーとしては扱いません。

呼び出し側の `ctx` のキャンセル・期限切れは `ErrTransport` ではなく、`context.Canceled` / `context.DeadlineExceeded` として返ります。

### リトライ

自動リトライはしません。判断は呼び出し側に任せます。

- `ErrBadRequest`・`ErrUnauthorized`・`ErrForbidden`・`ErrBusy`・`ErrQueueClosed` は、gomc-rest が PLC に触れる前に返すエラーなので、何も書き込まれておらず、何も実行されていません。`ErrBusy` の後は書き込みやリモート操作でも再送して安全です。サーバは `Retry-After: 1` を返すので、約 1 秒待ってから再送してください。
- 書き込みやリモート操作でそれ以外のエラーが出た場合は、PLC で実行されたか分かりません。`ErrRequestTimeout`・`ErrTransport`・`ErrBadResponse`（2xx なのに `{"ok":true}` が無い）に加え、リクエスト送信後に呼び出し側の `ctx` がキャンセル・期限切れになった場合（`context.Canceled` / `context.DeadlineExceeded`）も同じです。再送すると二重書き込みや操作の二重実行になることがあるので、値を読み直すか PLC の状態を確認してから判断してください。
- `RemoteReset` は、PLC が応答する前に接続を閉じることがあるため、リセットが実行されていても `ErrPLCConnection` などのエラーを返すことがあります。このようなエラーの後は `RemoteReset` や `RemoteLatchClear` を機械的に再送せず、PLC の状態を確認してください。
- `RandomWrite` は途中まで成功することもあります（[まとめ読み・まとめ書き](#まとめ読みまとめ書き)を参照）。

## タイムアウトと並行利用

- 既定の HTTP クライアントは 10 秒でタイムアウトします。`ctx` の期限がそれより長くても 10 秒で切れるので、長く待つ場合は `WithHTTPClient` で `Timeout` を変えてください。
- `*Client` は複数の goroutine から使えます。ただし gomc-rest は PLC へのリクエストを 1 本のキュー（既定 32）で直列に処理します。大量に並行で呼ぶより、少ない回数にまとめる方が速く、`ErrBusy` も起きにくくなります。連続した範囲は `ReadWords` / `ReadBits`、離れたワードは `RandomRead` を使ってください。

## 認証とネットワーク

- サーバが `-token` 付き（または `GOMCR_TOKEN` 環境変数付き）で起動されている場合は `WithToken` を渡します。全リクエストに `Authorization: Bearer <token>` が付きます。`/health` はサーバ側で認証が免除されています。
- リダイレクトは追いません。3xx はベース URL の設定ミスとして `*APIError` で返ります。
- gomc-rest は平文の HTTP で動くので、トークンは経路上で読めます。閉じたネットワークで使うか、HTTPS のリバースプロキシ配下に置いてください。パス付きのベース URL も使えます（`gomcrest.New("https://proxy.example/plc")`）。

## 対応する gomc-rest バージョン

gomc-rest `v1.4.0` 以降が対象です（`gomcrest.MinimumServerVersion`）。K 記法だけは `v1.7.0` 以降が必要です。

実行時に確かめる場合は `c.IsSupportedVersion(ctx)` を使います。ローカルビルドの `dev` は対応ありとして扱います。

## 対応 API

| メソッド | エンドポイント |
| --- | --- |
| `Health` | `GET /health` |
| `Version` / `IsSupportedVersion` | `GET /version` |
| `Info` | `GET /info` |
| `Metrics` | `GET /metrics` |
| `Read*` / `Read` / `ReadK*` | `GET /read` |
| `Write*` / `Write` / `WriteK*` | `POST /write` |
| `RandomRead` | `POST /random-read` |
| `RandomWrite` | `POST /random-write` |
| `RemoteRun` / `RemotePause` / `RemoteStop` / `RemoteLatchClear` / `RemoteReset` | `POST /remote/*` |

## 開発

```bash
gofmt -l .
go vet ./...
go test -race ./...
```

実機または GX Simulator につないだ gomc-rest に対する結合テストは、ビルドタグ `integration` で実行します。`GOMCR_TEST_URL` が未設定ならスキップされます。

```bash
GOMCR_TEST_URL=http://192.168.0.10:8080 go test -tags integration ./...
```

| 変数 | 用途 |
| --- | --- |
| `GOMCR_TEST_URL` | 接続先（必須） |
| `GOMCR_TEST_TOKEN` | トークン（任意） |
| `GOMCR_TEST_ALLOW_WRITE=1` | これが無い限り書き込みテストは実行しない |
| `GOMCR_TEST_WORD_ADDR` | テストに使ってよい空きワードの先頭。2 ワード使う。書き込みテストでは必須（読み出しテストの既定は `D9000`） |
| `GOMCR_TEST_BIT_ADDR` | テストに使ってよい空きビットデバイスの先頭。16 ビット使う。書き込みテストでは必須（読み出しテストの既定は `M9000`） |

書き込みテストは GX Simulator か切り離した試験用 PLC でだけ実行し、稼働中の設備には使わないでください。終了時に元の値へ書き戻しますが、値が変わっている間に PLC のプログラムが反応した結果は、書き戻しても取り消せません。リモート操作は PLC を止めてしまうため、結合テストの対象外です。

## ライセンス

Apache License 2.0。[LICENSE](LICENSE) と [NOTICE](NOTICE) を参照してください。
