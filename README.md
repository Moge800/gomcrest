# gomcrest

[![Go Reference](https://pkg.go.dev/badge/github.com/moge800/gomcrest.svg)](https://pkg.go.dev/github.com/moge800/gomcrest)
[![CI](https://github.com/Moge800/gomcrest/actions/workflows/ci.yml/badge.svg)](https://github.com/Moge800/gomcrest/actions/workflows/ci.yml)

[日本語 README](README_jp.md)

A Go client library for [gomc-rest](https://github.com/Moge800/gomc-rest), an MC protocol REST gateway for Mitsubishi PLCs. Standard library only, no external dependencies.

This library does not talk to the PLC itself. All PLC connections go through gomc-rest, so clients do not use up the PLC's connection slots, and readonly mode, remote control and token auth are managed in one place on the server. To connect to a PLC directly, use [gomcprotocol](https://github.com/Moge800/gomcprotocol).

## Install

```bash
go get github.com/moge800/gomcrest
```

Requires Go 1.23 or later.

## Usage

```go
c, err := gomcrest.New("http://192.168.0.10:8080", gomcrest.WithToken("s3cret"))
if err != nil {
	return err
}
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

words, err := c.ReadWords(ctx, "D100", 3)   // []uint16
bits, err := c.ReadBits(ctx, "M0", 8)       // []bool
wbits, err := c.ReadBits(ctx, "D100.3", 4)  // bits of a word device
dwords, err := c.ReadInt32s(ctx, "D200", 2) // []int32 (two words per value)

err = c.WriteWords(ctx, "D100", []uint16{10, 20, 30})
err = c.WriteBits(ctx, "M0", []bool{true, false})
```

Every method takes a `context.Context` first; the caller controls timeouts and cancellation.

### Typed reads and writes

| Read | Write | Go type | Devices |
| --- | --- | --- | --- |
| `ReadWords` | `WriteWords` | `[]uint16` | words |
| `ReadInt16s` | `WriteInt16s` | `[]int16` | signed words |
| `ReadDWords` | `WriteDWords` | `[]uint32` | double words (two consecutive words) |
| `ReadInt32s` | `WriteInt32s` | `[]int32` | signed double words |
| `ReadBits` | `WriteBits` | `[]bool` | bit devices (`M0`), bits of a word (`D100.1`) |

A type mismatch, such as `ReadWords` on a bit device, returns `ErrBadResponse` with a hint naming the right method. gomc-rest checks addresses and ranges.

### Device type known only at run time

For tools where the address comes from user input, such as a GUI, use `Read` / `Write`.

```go
v, err := c.Read(ctx, addr, 4, gomcrest.Format{SInt: true})
// bits go to v.Bools, everything else to v.Ints (int64)

err = c.Write(ctx, addr, gomcrest.Values{Ints: []int64{-1, 2}}, gomcrest.Format{SInt: true})
```

Set exactly one of `Ints` and `Bools` in `Values`. Otherwise `Write` returns `ErrBadRequest` without sending anything.

### Batch reads and writes

```go
res, err := c.RandomRead(ctx, gomcrest.RandomReadRequest{
	Words:  []string{"D100", "D200"},
	DWords: []string{"D300"},
	Bits:   []string{"D100.1", "M0"},
})
// res.Words / res.DWords / res.Bits are in request order

err = c.RandomWrite(ctx, gomcrest.RandomWriteRequest{
	Words: []gomcrest.WordItem{{Addr: "D100", Value: 10}},
	Bits:  []gomcrest.BitItem{{Addr: "M0", Value: true}},
})
```

For `RandomRead`, each list holds up to 255 entries, and `Words` plus the bits of words in `Bits` (such as `D100.1`) also count together toward 255. `Bits` may hold at most 16 bit devices (such as `M0`), and each one costs a separate PLC read: read contiguous bits such as M0–M15 with `ReadBits(ctx, "M0", 16)` instead, and use `RandomRead` for scattered words and double words. Because the server may issue several PLC reads, the values are not guaranteed to come from one PLC scan; if they must be consistent, have the PLC program copy them into one contiguous block and read that.

`RandomWrite` takes up to 255 entries per list. It is not atomic: the server writes words and dwords, then bit devices, then bits of words (read-modify-write per word) as separate PLC operations. If one fails, the earlier ones stay written, so read the addresses back before resending.

### K notation (gomc-rest v1.7.0+)

Reads and writes consecutive bit devices as one integer (`K4M100` is the 16 bits M100–M115). K1–K8 are supported.

```go
v, err := c.ReadK(ctx, "K4M100")     // uint32
s, err := c.ReadKInt(ctx, "K4M100")  // int32, sign-extended from the width
err = c.WriteK(ctx, "K4M100", 1234)
```

### Remote control

`RemoteRun` / `RemotePause` / `RemoteStop` / `RemoteLatchClear` / `RemoteReset` change the PLC's run state. gomc-rest must run with `-enable-remote` and without `-readonly`; otherwise they return `ErrForbidden`.

```go
err = c.RemoteRun(ctx, gomcrest.RunOptions{Clear: gomcrest.ClearNone})
```

As with writes, an error does not always mean the PLC did nothing; see [Retries](#retries) before resending.

## Errors

Test errors with `errors.Is`.

| Error | Meaning |
| --- | --- |
| `ErrBadRequest` | bad parameter or address |
| `ErrUnauthorized` | missing or wrong token (any 401) |
| `ErrForbidden` | readonly, or remote control disabled |
| `ErrPLC` | error from the PLC; its MC end code, if any, is in `APIError.EndCode` |
| `ErrPLCConnection` | **gomc-rest → PLC** unreachable |
| `ErrBusy` | server queue full, rejected at once (not run on the PLC) |
| `ErrQueueClosed` | server shutting down (not run on the PLC) |
| `ErrRequestCanceled` | canceled on the server |
| `ErrRequestTimeout` | timed out on the server |
| `ErrTransport` | **client → gomc-rest** unreachable |
| `ErrBadResponse` | unexpected response |

Errors from the server are `*gomcrest.APIError` with `Status`, `Code`, `Message` and `EndCode`. A response that is not from gomc-rest, such as a reverse proxy's HTML error page, has an empty `Code` and matches no sentinel except for 401. A sentinel also needs the HTTP status that gomc-rest sends with that code (listed in each error's godoc): a garbled answer such as a 500 with `"code":"busy"` keeps its `Code` but is not `ErrBusy`, so it is not treated as safe to resend.

If the caller's `ctx` is canceled or expires, the error is `context.Canceled` / `context.DeadlineExceeded`, not `ErrTransport`.

### Retries

The library never retries; the caller decides.

- `ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrBusy` and `ErrQueueClosed` are returned before gomc-rest touches the PLC, so nothing was written or run. After `ErrBusy`, resending is safe even for writes and remote control; the server sends `Retry-After: 1`, so wait about a second.
- After any other error on a write or a remote control call, the PLC may or may not have run it. This includes `ErrRequestTimeout`, `ErrTransport`, `ErrBadResponse` (a 2xx without `{"ok":true}`), and `context.Canceled` / `context.DeadlineExceeded` from your `ctx` once the request was sent. Resending can write twice or repeat the operation; read the value back, or check the PLC's state, first.
- `RemoteReset` can return an error such as `ErrPLCConnection` even after the PLC has reset, because the PLC may close the connection before it answers. After such an error, check the PLC's state instead of resending `RemoteReset` or `RemoteLatchClear` automatically.
- `RandomWrite` can also succeed part-way (see [Batch reads and writes](#batch-reads-and-writes)).

## Timeouts and concurrency

- The default HTTP client times out after 10 seconds, even when `ctx` has a later deadline. To wait longer, pass `WithHTTPClient` with a different `Timeout`.
- A `*Client` is safe for concurrent use. gomc-rest, however, runs PLC requests one at a time through a queue (32 by default). Fewer, larger requests are faster than many concurrent calls and less likely to hit `ErrBusy`: `ReadWords` / `ReadBits` for contiguous ranges, `RandomRead` for scattered words.

## Auth and network

- If the server runs with `-token` (or the `GOMCR_TOKEN` environment variable), pass `WithToken`. Every request then carries `Authorization: Bearer <token>`. The server exempts `/health` from auth.
- Redirects are not followed. A 3xx means the base URL is wrong and is returned as `*APIError`.
- gomc-rest serves plain HTTP, so a token can be read on the network path. Use it on a closed network, or behind an HTTPS reverse proxy. A base URL with a path works (`gomcrest.New("https://proxy.example/plc")`).

## Supported gomc-rest versions

gomc-rest `v1.4.0` or later (`gomcrest.MinimumServerVersion`). K notation needs `v1.7.0` or later.

To check at run time, call `c.IsSupportedVersion(ctx)`. A local `dev` build counts as supported.

## API

| Method | Endpoint |
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

## Development

```bash
gofmt -l .
go vet ./...
go test -race ./...
```

`-race` needs cgo (a C compiler); without one, run `go test ./...`. CI runs the tests with `-race` on Go 1.23 and the latest stable Go.

Integration tests run against gomc-rest connected to a real PLC or GX Simulator, with the `integration` build tag. They are skipped unless `GOMCR_TEST_URL` is set.

```bash
GOMCR_TEST_URL=http://192.168.0.10:8080 go test -tags integration ./...
```

| Variable | Purpose |
| --- | --- |
| `GOMCR_TEST_URL` | server URL (required) |
| `GOMCR_TEST_TOKEN` | token (optional) |
| `GOMCR_TEST_ALLOW_WRITE=1` | write tests run only when this is set |
| `GOMCR_TEST_WORD_ADDR` | first of 2 free words the tests may use; required for write tests (read tests default to `D9000`) |
| `GOMCR_TEST_BIT_ADDR` | first of 16 free bits of a bit device the tests may use; required for write tests (read tests default to `M9000`) |

Run write tests only against GX Simulator or an isolated test PLC, never against equipment in operation. They restore the original values when they finish, but the PLC program can act on the changed values in the meantime, and restoring does not undo that. Remote control is not tested, because it would stop the PLC.

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
