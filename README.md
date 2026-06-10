# yfin-go

A small, zero-dependency Go client for Yahoo Finance's unofficial JSON API.
It fetches fundamental data — trailing/forward P/E, P/B, EPS, market cap,
dividend yield, 52-week change — for any symbol Yahoo knows about, including
NSE (`INFY.NS`) and BSE (`INFY.BO`) listings.

Yahoo's endpoints have required a session cookie + crumb token since 2023;
this client performs that handshake transparently and caches the session,
refreshing it once automatically if Yahoo returns a 401/403.

## Usage

```go
package main

import (
    "fmt"

    yfin "github.com/arpitduttdixit/yfin-go"
)

func main() {
    c := yfin.New()

    f, err := c.Fundamentals("INFY.NS") // or yfin.NSE("INFY")
    if err != nil {
        panic(err)
    }
    if f.TrailingPE != nil {
        fmt.Printf("INFY trailing P/E: %.2f\n", *f.TrailingPE)
    }

    // Batch: sequential with a polite ~500ms delay; per-symbol errors are
    // collected, a bad symbol doesn't abort the batch.
    results, errs := c.BatchFundamentals([]string{"RELIANCE.NS", "TCS.NS"})
    _ = results
    _ = errs
}
```

All fields of `Fundamentals` are pointers — Yahoo frequently omits fields
(especially for small caps), and `nil` means "not reported", not an error.

## Verification

Unit tests mock the handshake and API with `httptest`:

```sh
go test ./...
```

Live tests against the real Yahoo API (needs network access to
`fc.yahoo.com` and `query1.finance.yahoo.com`):

```sh
YFIN_LIVE=1 go test ./...
```

There's also a live smoke-test command that prints a table of NSE large caps:

```sh
go run ./cmd/yfintest            # INFY, RELIANCE, TCS, HDFCBANK
go run ./cmd/yfintest WIPRO.NS   # or any symbols you pass
```

## Notes

- Stdlib only; no third-party dependencies.
- 10s timeout per request; no retries beyond the single session refresh.
- `Client` is safe for concurrent use, but prefer `BatchFundamentals` for
  multiple symbols — it spaces requests out to stay polite to Yahoo.
