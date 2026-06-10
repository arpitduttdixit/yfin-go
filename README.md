# yfin-go

A small, zero-dependency Go client for Yahoo Finance's unofficial JSON API,
focused on fundamental data: P/E, P/B, EPS, market cap, dividend yield, and
52-week change.

Yahoo's endpoints have required a session cookie + crumb token since 2023
(which is why older clients like `piquette/finance-go` are broken). This
client performs the handshake automatically, caches it in memory, and
refreshes once on a 401/403 before surfacing an error.

## Install

```
go get github.com/arpitduttdixit/yfin-go
```

## Usage

```go
client := yfin.New()

// Single symbol — passed to Yahoo as-is. NSE symbols use the ".NS"
// suffix (yfin.NSE("INFY") == "INFY.NS"), BSE uses ".BO".
f, err := client.Fundamentals("INFY.NS")
if err != nil {
    log.Fatal(err)
}
if f.TrailingPE != nil { // fields are nil when Yahoo doesn't report them
    fmt.Printf("INFY P/E: %.2f\n", *f.TrailingPE)
}

// Many symbols — sequential with a polite ~500ms delay between requests.
// Per-symbol errors are collected, not fatal.
results, errs := client.BatchFundamentals([]string{"INFY.NS", "TCS.NS"})
```

`Fundamentals` fields (all nilable — absence is normal, especially for
small caps):

| Field | Source module |
|---|---|
| `TrailingPE`, `ForwardPE`, `DividendYield` | summaryDetail |
| `PriceToBook`, `TrailingEPS`, `Week52Change` | defaultKeyStatistics |
| `MarketCap` | price |

## Live verification

The unit tests mock Yahoo with `httptest`; to verify against the real API:

```
go run ./cmd/yfintest                 # INFY.NS RELIANCE.NS TCS.NS HDFCBANK.NS
go run ./cmd/yfintest AAPL MSFT      # or any symbols
YFIN_LIVE=1 go test -run TestLive -v # live test
```

Note: sandboxed/datacenter environments are often blocked by Yahoo. If
running from a restricted network, the allowlist needs `fc.yahoo.com`,
`query1.finance.yahoo.com`, and `query2.finance.yahoo.com`.
