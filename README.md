# yfin-go

A small, zero-dependency Go client for Yahoo Finance's unofficial JSON API,
focused on fundamental data: valuation (P/E, P/B, PEG, EPS, market cap),
financial health (ROE, margins, debt, cash flow), analyst coverage (rating,
price targets), trading context (beta, 52-week range), company profile (sector,
industry), and the earnings/dividend calendar.

All of this comes from a single `quoteSummary` request per symbol — the
requested modules are listed in `quoteSummaryModules` in `fundamentals.go`, so
adding more data is a matter of extending that constant and the response
structs, with no extra HTTP round-trip.

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
small caps and non-US listings):

| Group | Fields | Source module |
|---|---|---|
| Valuation | `TrailingPE`, `ForwardPE`, `PriceToBook`, `TrailingEPS`, `PegRatio`, `EnterpriseValue`, `EnterpriseToEbitda`, `BookValue`, `MarketCap`, `DividendYield`, `PayoutRatio`, `Week52Change` | summaryDetail / defaultKeyStatistics / price |
| Financial health | `ReturnOnEquity`, `ReturnOnAssets`, `ProfitMargins`, `OperatingMargins`, `GrossMargins`, `DebtToEquity`, `CurrentRatio`, `QuickRatio`, `TotalCash`, `TotalDebt`, `FreeCashflow`, `OperatingCashflow`, `RevenueGrowth`, `EarningsGrowth` | financialData |
| Analyst coverage | `RecommendationKey`, `RecommendationMean`, `NumberOfAnalystOpinions`, `CurrentPrice`, `TargetMeanPrice`, `TargetHighPrice`, `TargetLowPrice` | financialData |
| Trading context | `Beta`, `FiftyTwoWeekHigh`, `FiftyTwoWeekLow`, `FiftyDayAverage`, `TwoHundredDayAverage`, `AverageVolume`, `SharesOutstanding`, `FloatShares`, `SharesShort`, `ShortRatio`, `ShortPercentOfFloat` | summaryDetail / defaultKeyStatistics |
| Company profile | `Sector`, `Industry`, `Country`, `FullTimeEmployees` | assetProfile |
| Calendar | `NextEarningsDate`, `ExDividendDate`, `DividendDate` (`*time.Time`) | calendarEvents |

`MarketCap`, `EnterpriseValue`, `TotalCash`, `TotalDebt`, `FreeCashflow`,
`OperatingCashflow` and the share counts are `*int64`; ratios, margins and
yields are `*float64`; `RecommendationKey`, `Sector`, `Industry`, `Country` and
`FinancialCurrency` are `*string`; the calendar dates are `*time.Time`.

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
