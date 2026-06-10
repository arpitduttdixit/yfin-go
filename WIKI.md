# yfin-go Wiki — How This Library Works, In Detail

This document explains everything about yfin-go: why it exists, how Yahoo
Finance's unofficial API works, what the cookie + crumb handshake is, what
each fundamental metric means, and how the code is put together. It is
written for someone who knows how to code but hasn't worked with the Yahoo
API or studied finance.

---

## Table of Contents

1. [Why This Library Exists](#1-why-this-library-exists)
2. [Yahoo Finance's Unofficial API — A Short History](#2-yahoo-finances-unofficial-api--a-short-history)
3. [The Cookie + Crumb Handshake](#3-the-cookie--crumb-handshake)
4. [The quoteSummary Endpoint](#4-the-quotesummary-endpoint)
5. [Yahoo's Number Encoding — raw and fmt](#5-yahoos-number-encoding--raw-and-fmt)
6. [The Fundamentals — What Each Field Means](#6-the-fundamentals--what-each-field-means)
7. [Symbols — NSE, BSE, and Yahoo Suffixes](#7-symbols--nse-bse-and-yahoo-suffixes)
8. [Client Design — Session Caching and Refresh](#8-client-design--session-caching-and-refresh)
9. [Why Every Field Is a Pointer](#9-why-every-field-is-a-pointer)
10. [BatchFundamentals — Being Polite to Yahoo](#10-batchfundamentals--being-polite-to-yahoo)
11. [Error Handling Philosophy](#11-error-handling-philosophy)
12. [Code Walkthrough — File by File](#12-code-walkthrough--file-by-file)
13. [Testing Strategy](#13-testing-strategy)
14. [The yfintest Command](#14-the-yfintest-command)
15. [Troubleshooting](#15-troubleshooting)
16. [Design Constraints and Non-Goals](#16-design-constraints-and-non-goals)
17. [Glossary — Quick Reference](#17-glossary--quick-reference)

---

## 1. Why This Library Exists

This library was built to feed **fundamental data** into
a market-analysis tool
whose signals are computed from price and volume history alone (its data
source, Zerodha Kite Connect, provides no fundamentals).

The problem with price-only signals: a momentum BUY can fire on a stock
that has already run up 40–50% and is now trading at an absurd valuation.
Price history can't tell you whether you're buying a reasonably priced
business or paying 90× earnings for hype. Fundamentals — P/E ratio, P/B
ratio, earnings per share — are the missing dimension.

Yahoo Finance has this data for nearly every listed company in the world,
including NSE/BSE stocks, for free. The catch is that there is **no
official API** — only the website's own internal JSON endpoints, which the
entire ecosystem of unofficial clients (Python's `yfinance` being the most
famous) quietly uses. yfin-go is a minimal Go client for those endpoints.

Why not use an existing Go library? The most popular one,
`piquette/finance-go`, had its last release in 2019 and calls
`/v6/finance/quote` without the crumb token that Yahoo started requiring
in 2023 — it is broken for everyone. Writing ~250 lines of stdlib-only Go
was simpler than maintaining a fork.

---

## 2. Yahoo Finance's Unofficial API — A Short History

- **Pre-2017:** Yahoo had a genuinely public API (the YQL/CSV download
  endpoints). It was killed in 2017 with no replacement.
- **2017–2023:** The finance.yahoo.com website itself fetches data from
  internal JSON endpoints on `query1.finance.yahoo.com` and
  `query2.finance.yahoo.com`. These remained publicly reachable with no
  authentication, and unofficial clients simply called them directly.
- **2023–present:** Yahoo added a light anti-bot gate: data endpoints now
  require a **session cookie** and a matching **crumb token** (a
  CSRF-style value tied to the cookie). Requests without them get
  `401 Unauthorized`. Every working client today performs the same
  cookie + crumb dance described in the next section.

Two practical consequences of being unofficial:

1. **It can change without notice.** Yahoo owes us nothing. The library is
   deliberately tiny so that when something breaks, there is very little
   to fix.
2. **Bot detection exists.** Yahoo rejects requests with default
   programmatic user agents (Go's default is `Go-http-client/2.0`), so
   every request must impersonate a real browser.

`query1` and `query2` are interchangeable mirrors; yfin-go uses `query1`.

---

## 3. The Cookie + Crumb Handshake

Before any data request works, the client must acquire two things: a
session cookie and a crumb. This is a three-step process:

### Step 1 — Get a cookie from fc.yahoo.com

```
GET https://fc.yahoo.com
```

This request returns a **404 Not Found — and that's expected**. The page
doesn't exist; we never look at the body. What matters is the response's
`Set-Cookie` headers, which establish a Yahoo session (a cookie named
`A3` or similar, scoped to `.yahoo.com` so it is sent to every Yahoo
subdomain). yfin-go uses Go's `net/http/cookiejar` so the cookie is
captured and replayed automatically — the code never touches it directly.

### Step 2 — Exchange the cookie for a crumb

```
GET https://query1.finance.yahoo.com/v1/test/getcrumb
```

Sent *with* the cookie from step 1, this returns a short opaque string in
the response body — the crumb, e.g. `Ts7tg1mZ2Xy`. Sent *without* a valid
cookie, it returns an empty body or an error page. The crumb is
effectively a CSRF token: it proves the caller holds the session cookie.

yfin-go sanity-checks the response: a crumb must be non-empty and must not
contain `<` (an HTML error page means the handshake failed).

### Step 3 — Use both on every data request

Every data request carries the cookie (via the jar) and appends
`&crumb=<crumb>` as a query parameter. Yahoo validates that the crumb
matches the cookie's session.

### Caching and expiry

The handshake costs two round trips, so the result is cached in memory for
the lifetime of the `Client`. Sessions eventually expire; when a data
request comes back `401` or `403`, yfin-go re-runs the handshake **once**
and retries the request **once**. If it still fails, the error is
returned to the caller — no retry loops, no backoff. (See
[§8](#8-client-design--session-caching-and-refresh) for why.)

Sequence for a cold client:

```
Client                          Yahoo
  │  GET fc.yahoo.com             │
  │ ─────────────────────────────▶│
  │ ◀───────────────────────────── 404 + Set-Cookie: A3=...
  │  GET /v1/test/getcrumb        │   (cookie attached by jar)
  │ ─────────────────────────────▶│
  │ ◀───────────────────────────── 200 "Ts7tg1mZ2Xy"
  │  GET /v10/finance/quoteSummary/INFY.NS?modules=...&crumb=Ts7tg1mZ2Xy
  │ ─────────────────────────────▶│
  │ ◀───────────────────────────── 200 {"quoteSummary": {...}}
```

Subsequent requests skip straight to the last step.

---

## 4. The quoteSummary Endpoint

All data comes from a single endpoint:

```
GET https://query1.finance.yahoo.com/v10/finance/quoteSummary/{symbol}
      ?modules=summaryDetail,defaultKeyStatistics,price
      &crumb=...
```

`quoteSummary` is Yahoo's kitchen-sink endpoint — the website's quote page
is rendered from it. It is organised into **modules** (there are dozens:
`earnings`, `balanceSheetHistory`, `recommendationTrend`, ...); you
request only the modules you want, comma-separated. yfin-go requests
exactly three:

| Module | What it contains | Fields we extract |
|---|---|---|
| `summaryDetail` | The stats box on the quote page | `trailingPE`, `forwardPE`, `dividendYield` |
| `defaultKeyStatistics` | The "Key Statistics" tab | `priceToBook`, `trailingEps`, `52WeekChange` |
| `price` | Live quote and identity info | `marketCap` |

The response shape:

```json
{
  "quoteSummary": {
    "result": [
      {
        "summaryDetail":        { "trailingPE": {"raw": 24.53, "fmt": "24.53"}, ... },
        "defaultKeyStatistics": { "priceToBook": {"raw": 7.12, "fmt": "7.12"}, ... },
        "price":                { "marketCap": {"raw": 6457000000000, "fmt": "6.46T"}, ... }
      }
    ],
    "error": null
  }
}
```

For an unknown symbol, Yahoo returns HTTP 404 with `result: null` and a
populated `error` object:

```json
{
  "quoteSummary": {
    "result": null,
    "error": { "code": "Not Found",
               "description": "Quote not found for ticker symbol: BOGUS.NS" }
  }
}
```

yfin-go deliberately lets a 404 body through to the parser so the error
message Yahoo wrote (`Quote not found for ticker symbol: ...`) ends up in
the Go error verbatim — far more useful than a bare "HTTP 404".

---

## 5. Yahoo's Number Encoding — raw and fmt

Every numeric value in a quoteSummary response is not a bare number but an
object with up to three representations:

```json
"marketCap": {
  "raw": 6457000000000,        ← the actual number; this is what we use
  "fmt": "6.46T",              ← human-formatted, for display
  "longFmt": "6,457,000,000,000"
}
```

yfin-go unmarshals only `raw`, via one tiny type:

```go
type rawValue struct {
    Raw *float64 `json:"raw"`
}
```

The crucial subtlety: **missing data appears in two different ways**.
Either the key is absent entirely, or — and this is the trap — the key is
present but maps to an **empty object** `{}`:

```json
"trailingPE": {},          ← company has negative earnings; no P/E exists
```

Because `Raw` is a `*float64`, both cases decode to `nil` without error.
A non-pointer `float64` would have silently turned "no data" into `0.0`,
which for a P/E ratio is a real (and very wrong) value.

`MarketCap` is exposed as `*int64` (a share count × price is conceptually
an integer of rupees/dollars), so the parser converts the `float64` raw
value after checking it exists.

---

## 6. The Fundamentals — What Each Field Means

The `Fundamentals` struct carries seven metrics. For each: what it is,
how to read it, and why the caller wants it.

### TrailingPE — Trailing Price-to-Earnings Ratio

```
trailing P/E = current share price / earnings per share over the last 12 months
```

"How many years of current profits you are paying for." A P/E of 24 means
the market price is 24× the company's annual earnings per share. Rough
intuition for Indian large caps: 15–25 is ordinary, 40+ is expensive
(justified only by high growth), and **no value at all means the company
lost money** — you can't divide by negative earnings, so Yahoo omits the
field. *Trailing* means it uses the last four reported quarters — real,
audited numbers.

This is the headline metric for the valuation guard: a momentum BUY
on a stock trading at 2× the market's average P/E deserves suspicion.

### ForwardPE — Forward Price-to-Earnings Ratio

Same formula, but the denominator is **analysts' forecast** of next year's
earnings rather than last year's actuals. Forward < trailing means
analysts expect earnings to grow. It's an estimate, so treat it as
opinion, not fact. Often missing for small caps that no analyst covers.

### PriceToBook — P/B Ratio

```
P/B = share price / book value per share
```

Book value is the company's net assets on its balance sheet (assets minus
liabilities). P/B near 1 means you're paying roughly what the company's
assets are worth on paper; 7 means the market values the business far
above its assets (normal for software/services companies whose value is
people and brands, not factories). Most meaningful for banks and
asset-heavy businesses, where book value is a real anchor.

### TrailingEPS — Earnings Per Share

```
EPS = net profit over last 12 months / number of shares outstanding
```

The company's profit expressed per share, in the stock's currency (₹ for
NSE listings). It is the denominator of the P/E ratio. Negative EPS =
loss-making company (and a missing TrailingPE, as noted above).

### MarketCap — Market Capitalisation

```
market cap = share price × shares outstanding
```

The total market value of the company, in the listing currency. INFY at
~₹6.5 trillion (`6457000000000`) is a mega cap. Useful for distinguishing
large caps (stable, well-covered, reliable fundamentals) from small caps
(volatile, sparse data — expect many nil fields).

### DividendYield — Dividend Yield

```
dividend yield = annual dividends per share / share price
```

The cash return you'd earn just from dividends. **Yahoo reports it as a
raw fraction**: `0.0285` means 2.85%. (Some Yahoo fields are
pre-multiplied percentages; this one is not — multiply by 100 for
display, as `cmd/yfintest` does.) Mature companies pay dividends; growth
companies usually don't, so nil/zero is common and not alarming.

### Week52Change — 52-Week Price Change

The stock's price change over the trailing year, also as a raw fraction
(`0.1342` = up 13.42%). Strictly this is *price* data, not a fundamental,
but it ships in `defaultKeyStatistics` for free and gives instant context:
a P/E of 60 on a stock already up 80% in a year tells a story.

---

## 7. Symbols — NSE, BSE, and Yahoo Suffixes

Yahoo identifies non-US listings with an exchange suffix:

| Exchange | Suffix | Example |
|---|---|---|
| NSE (National Stock Exchange, India) | `.NS` | `INFY.NS` |
| BSE (Bombay Stock Exchange) | `.BO` | `INFY.BO` |
| US listings (NYSE/NASDAQ) | none | `AAPL` |

yfin-go passes symbols through **as-is** — it does no mapping, validation,
or guessing. The one convenience offered is:

```go
yfin.NSE("INFY")  // returns "INFY.NS"
```

The same company can trade on both NSE and BSE with independent Yahoo
records; prefer `.NS` (higher volume, better data). Note that Infosys also
has a US listing under the bare symbol `INFY` (an ADR on NYSE) — forgetting
the suffix silently gets you the US listing in dollars, not the NSE one in
rupees.

---

## 8. Client Design — Session Caching and Refresh

```go
type Client struct {
    httpClient *http.Client   // owns the cookie jar + 10s timeout

    cookieURL string          // endpoint URLs as fields, not constants,
    crumbURL  string          //   so tests can point them at httptest
    quoteURL  string
    delay     time.Duration   // pause between batch requests

    mu    sync.Mutex          // guards crumb
    crumb string              // cached session token; "" = no session yet
}
```

Design decisions worth explaining:

**Lazy handshake.** `New()` makes no network calls. The handshake runs
inside the first `Fundamentals` call, via `ensureCrumb(refresh bool)`.
This keeps construction infallible (`New()` returns no error) and means a
client that's never used costs nothing.

**One mutex, one cached string.** `ensureCrumb` takes the lock, checks the
cache, and runs the handshake only if needed. The client is safe for
concurrent use; concurrent first-calls serialize on the handshake instead
of racing to perform it several times.

**Refresh-once on 401/403.** The retry policy lives in
`fetchQuoteSummary` and is deliberately rigid:

```
request → 401/403? → ensureCrumb(refresh=true) → request again → done
                                                       │
                                            still failing? return the error
```

Exactly one refresh, exactly one retry, no sleeping, no exponential
backoff. Rationale: a 401 after a *fresh* handshake isn't a stale session
— it's Yahoo blocking you (rate limit, IP reputation) or a protocol
change, and retrying harder makes both worse. The caller (a weekly
sync) owns persistence and staleness policy, so the library stays dumb.

**URLs as struct fields.** `cookieURL`/`crumbURL`/`quoteURL` default to
the real Yahoo endpoints in `New()` but are plain fields, so the test
suite constructs a `Client` pointed at an `httptest.Server`. This is the
entire dependency-injection story — no interfaces, no options pattern,
because nothing else ever needs swapping.

**Browser User-Agent on every request.** A single `get()` helper sets
`User-Agent: Mozilla/5.0 ... Chrome/124 ...` on the cookie request, the
crumb request, and every data request. Miss one and Yahoo 401s
intermittently in ways that are miserable to debug.

**10-second timeout.** Set once on the `http.Client`, covering dial, TLS,
headers, and body for every request. A hung Yahoo endpoint fails a weekly
sync request in 10s instead of hanging it forever.

---

## 9. Why Every Field Is a Pointer

```go
type Fundamentals struct {
    Symbol        string
    TrailingPE    *float64
    ForwardPE     *float64
    PriceToBook   *float64
    TrailingEPS   *float64
    MarketCap     *int64
    DividendYield *float64
    Week52Change  *float64
}
```

Missing fundamentals are **normal**, not exceptional:

- Loss-making companies have no P/E (division by negative earnings).
- Non-dividend payers have no yield.
- Small caps with no analyst coverage have no forward P/E.
- Recently listed companies have no 52-week change.

With plain `float64`, all of these would decode to `0` — and a P/E of 0,
a yield of 0, an EPS of 0 are all *plausible-looking real values*. The
pointer makes "absent" (`nil`) structurally different from "zero", and
forces callers to handle it:

```go
if f.TrailingPE != nil && *f.TrailingPE > 2*niftyAvgPE {
    // overvalued — act
}
// nil → not enough data → skip the rule, don't guess
```

This is the same graceful-degradation contract the caller uses elsewhere:
no data means *skip the rule*, never *fabricate a value*.

---

## 10. BatchFundamentals — Being Polite to Yahoo

```go
func (c *Client) BatchFundamentals(symbols []string) (map[string]*Fundamentals, map[string]error)
```

Three deliberate properties:

1. **Sequential, with a 500ms pause between requests.** No goroutine
   fan-out. Yahoo's endpoints are unofficial and rate-limited by
   IP reputation; hammering them in parallel is the fastest way to get
   the whole client blocked. ~2 requests/second keeps a 50-symbol
   portfolio sync under 30 seconds, which is plenty for a weekly job.
2. **Per-symbol errors don't abort the batch.** One delisted or mistyped
   symbol in a watchlist must not cost you the other 49 results. Failures
   are collected into the second return value, keyed by symbol.
3. **Two maps, not one struct.** `len(errs) == 0` is the all-good check;
   callers can log errors and use results independently.

The first request in a batch pays the handshake cost; the rest reuse the
cached session. The delay is skipped before the first request (no pointless
startup sleep).

---

## 11. Error Handling Philosophy

The line yfin-go draws:

| Situation | Treatment |
|---|---|
| Field missing from Yahoo's response | **Not an error** — nil field |
| Symbol unknown to Yahoo | **Error** (with Yahoo's description) |
| Network failure / timeout | **Error** (wrapped, with symbol) |
| 401/403 after one refresh | **Error** |
| Other non-200 HTTP status | **Error** (`HTTP <code>`) |
| Crumb endpoint returns HTML or empty | **Error** (handshake failed) |

Every error string is prefixed `yfin:` and includes the symbol where
relevant, e.g.:

```
yfin: quoteSummary BOGUS.NS: Not Found: Quote not found for ticker symbol: BOGUS.NS
yfin: crumb fetch: HTTP 403
```

Underlying errors are wrapped with `%w` so `errors.Is`/`errors.As` work
(e.g. detecting `context.DeadlineExceeded` through the wrap).

---

## 12. Code Walkthrough — File by File

```
yfin-go/
├── go.mod                      module github.com/arpitduttdixit/yfin-go; stdlib only
├── yfin.go                     the entire library (~290 lines)
├── yfin_test.go                unit tests against an httptest stub
├── yfin_live_test.go           live tests, gated behind YFIN_LIVE=1
├── testdata/
│   ├── quotesummary_infy.json      golden fixture: rich large-cap response
│   ├── quotesummary_sparse.json    golden fixture: small cap, most fields missing
│   └── quotesummary_notfound.json  golden fixture: Yahoo's unknown-symbol error
├── cmd/yfintest/main.go        live smoke-test CLI
├── README.md                   quick start
└── WIKI.md                     this document
```

`yfin.go`, top to bottom:

- **Constants** — the three Yahoo URLs, the browser User-Agent, the 10s
  timeout, the 500ms batch delay, and the module list
  (`summaryDetail,defaultKeyStatistics,price`).
- **`Fundamentals`** — the public data struct ([§9](#9-why-every-field-is-a-pointer)).
- **`Client` / `New()`** — sets up the `http.Client` with a fresh
  `cookiejar` and the default URLs ([§8](#8-client-design--session-caching-and-refresh)).
- **`NSE()`** — the suffix helper ([§7](#7-symbols--nse-bse-and-yahoo-suffixes)).
- **`Fundamentals(symbol)`** — fetch + parse; two lines of orchestration.
- **`BatchFundamentals(symbols)`** — the sequential loop ([§10](#10-batchfundamentals--being-polite-to-yahoo)).
- **`fetchQuoteSummary`** — the refresh-once-on-401/403 policy lives here.
- **`getQuoteSummary`** — builds the URL (path-escaped symbol,
  query-escaped crumb) and returns raw body + status.
- **`ensureCrumb(refresh)`** — the handshake ([§3](#3-the-cookie--crumb-handshake)),
  behind the mutex.
- **`get(url)`** — the one place request headers are set.
- **`rawValue` / `quoteSummaryResponse`** — the JSON decoding types,
  mirroring Yahoo's nesting exactly (note the `json:"52WeekChange"` tag —
  Yahoo's key really does start with a digit, which is why the Go field
  can't just match by name).
- **`parseFundamentals`** — Yahoo-error check, empty-result check, then a
  plain field-by-field copy into `Fundamentals`.

---

## 13. Testing Strategy

The dev environment may not be able to reach Yahoo at all (sandboxes,
CI). So the test suite has two completely separate layers:

### Unit tests (always run) — `yfin_test.go`

A `yahooStub` type implements all three endpoints on a single
`httptest.Server`:

- `/cookie` sets a fake session cookie and returns 404 — faithfully
  mimicking fc.yahoo.com's odd behaviour, including the 404.
- `/v1/test/getcrumb` returns a crumb **only if the session cookie is
  present** — so the tests prove the jar actually works end-to-end.
- `/v10/finance/quoteSummary/{symbol}` validates the cookie, the crumb
  query parameter, and the modules list, then serves a golden fixture
  from `testdata/`; unknown symbols get the not-found fixture with a 404,
  exactly like real Yahoo.

The stub counts hits per endpoint with atomics, which lets tests make
claims about *behaviour over time*, not just single responses:

| Test | What it proves |
|---|---|
| `TestFundamentals` | every field parsed correctly from the rich fixture |
| `TestFundamentalsSparse` | missing fields → nil, not error, not zero |
| `TestFundamentalsNotFound` | Yahoo's error description surfaces in the Go error |
| `TestCrumbCachedAcrossRequests` | 3 fetches → exactly 1 handshake |
| `TestCrumbRefreshOn401` | a 401 → exactly 2 crumb fetches and 2 data requests |
| `TestPersistent401Surfaces` | refresh didn't help → error returned, no infinite loop |
| `TestBatchFundamentals` | bad symbol doesn't poison the batch |
| `TestNSE` | the suffix helper |

The fixtures are **golden files**: realistic Yahoo JSON (including
`raw`/`fmt` wrappers, empty-object missing fields, and irrelevant noise
fields the decoder must ignore) checked into `testdata/`. If Yahoo changes
its response shape, the fix starts with updating a fixture from a real
captured response.

### Live tests (opt-in) — `yfin_live_test.go`

```sh
YFIN_LIVE=1 go test ./...
```

These hit real Yahoo and assert only stable facts (Infosys has a positive
market cap) rather than exact values. Gated behind the env var so normal
`go test ./...` is fast, deterministic, and offline-safe.

---

## 14. The yfintest Command

```sh
go run ./cmd/yfintest                      # INFY, RELIANCE, TCS, HDFCBANK
go run ./cmd/yfintest WIPRO.NS TATAMOTORS.NS   # any symbols
```

The live verification tool. It batch-fetches the symbols and prints an
aligned table (`text/tabwriter`):

```
SYMBOL       TRAIL P/E  FWD P/E  P/B   EPS    MKT CAP  DIV YLD  52W CHG
INFY.NS      24.53      21.87    7.12  63.39  6.46T    2.85%    13.42%
...
```

Nil fields print as `-`. Market cap is humanised (T/B/M); the fractional
yield and 52-week change are multiplied by 100 for display. Exits non-zero
if any symbol errored, so it can double as a connectivity check in
scripts.

This command exists because the development sandbox can't always reach
Yahoo — it's the documented way to verify the library from a normal
network.

---

## 15. Troubleshooting

**`yfin: crumb fetch: HTTP 403` immediately, every time**
The network between you and Yahoo is blocking the request. In a Claude
Code web sandbox this is the egress allowlist — it must include
`fc.yahoo.com`, `query1.finance.yahoo.com`, and `query2.finance.yahoo.com`.
(Check the response headers: the sandbox proxy says
`x-deny-reason: host_not_allowed`.) Corporate proxies produce the same
symptom.

**`yfin: crumb fetch: unexpected response "<html..."`**
The crumb endpoint returned an HTML page instead of a token — usually a
consent/captcha interstitial (common from EU IPs) or a block page. Try a
different network; there is no code fix.

**401s that a refresh doesn't cure**
Rate limiting or IP reputation. Slow down (the batch delay exists for
this), and don't run many clients in parallel from one IP.

**A field is nil for a symbol that "should" have it**
Check the symbol suffix first — `INFY` (US ADR) and `INFY.NS` (NSE) are
different records. Then check Yahoo's website for the same symbol: if the
website doesn't show the number, the API won't either. Loss-making
companies never have a trailing P/E.

**`Quote not found for ticker symbol: ...`**
Yahoo doesn't know the symbol. Typo, delisting, or missing exchange
suffix.

---

## 16. Design Constraints and Non-Goals

Hard constraints (from the spec):

- **Zero third-party dependencies.** Only `net/http`,
  `net/http/cookiejar`, `encoding/json`, `net/url`, `sync`, `time`, `io`,
  `strings`, `fmt`. No go.sum at all. An unofficial-API client is a
  liability to maintain; dependencies multiply that.
- **Timeouts on everything** (10s per request).
- **No retries** beyond the single crumb-refresh — persistence, caching,
  staleness, and scheduling are the caller's job (a weekly sync runs
  into SQLite and tolerates 30-day-old data).

Explicit non-goals — if you need these, that's a different library:

- Historical prices / OHLCV candles (the caller sources those elsewhere).
- Real-time quotes or streaming.
- Other quoteSummary modules (balance sheets, analyst ratings, ...). The
  decoder extracts exactly seven fields on purpose; adding a module means
  adding a struct and a fixture, not generalising the parser.
- Symbol search or validation.
- Disk caching.

---

## 17. Glossary — Quick Reference

| Term | Meaning |
|---|---|
| **Crumb** | Short anti-CSRF token Yahoo requires on data requests; obtained via `getcrumb`, valid only with its session cookie |
| **Cookie jar** | `net/http` component that stores cookies from responses and attaches them to later requests automatically |
| **quoteSummary** | Yahoo's internal endpoint serving all quote-page data, organised into modules |
| **Module** | A named section of quoteSummary data (`summaryDetail`, `price`, ...) selected via the `modules=` query param |
| **raw / fmt** | Yahoo's number encoding: `raw` is the machine value, `fmt` the display string; yfin-go reads only `raw` |
| **P/E ratio** | Price ÷ earnings per share; years of current profit you pay for. Trailing = last 12 months actual, forward = next year forecast |
| **P/B ratio** | Price ÷ book value per share; price vs. net assets on the balance sheet |
| **EPS** | Earnings per share — annual net profit ÷ shares outstanding |
| **Market cap** | Share price × shares outstanding; total company value |
| **Dividend yield** | Annual dividends ÷ price, as a fraction (0.0285 = 2.85%) |
| **52-week change** | Price change over the trailing year, as a fraction |
| **`.NS` / `.BO`** | Yahoo suffixes for NSE / BSE listings |
| **ADR** | A US-listed wrapper of a foreign stock — why bare `INFY` is not `INFY.NS` |
| **Golden fixture** | A captured, checked-in real API response used as test input |
| **Large cap / small cap** | Big company / small company by market cap; small caps have sparser Yahoo data |
