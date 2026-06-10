// Package yfin is a small, zero-dependency client for Yahoo Finance's
// unofficial JSON API. It performs the cookie + crumb handshake that the
// endpoints have required since 2023 and exposes fundamental data
// (P/E, P/B, EPS, market cap, ...) for a symbol.
//
// NSE symbols are addressed on Yahoo as "SYMBOL.NS" (e.g. "INFY.NS"),
// BSE symbols as "SYMBOL.BO".
package yfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultCookieURL = "https://fc.yahoo.com"
	defaultCrumbURL  = "https://query1.finance.yahoo.com/v1/test/getcrumb"
	defaultQuoteURL  = "https://query1.finance.yahoo.com/v10/finance/quoteSummary"

	// Yahoo rejects default Go user agents, so impersonate a browser.
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	requestTimeout = 10 * time.Second
	batchDelay     = 500 * time.Millisecond

	quoteModules = "summaryDetail,defaultKeyStatistics,price"
)

// Fundamentals holds the fundamental data for one symbol. Yahoo frequently
// omits fields (especially for small caps), so every value is a pointer and
// nil simply means "not reported".
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

// Client talks to Yahoo Finance. It is safe for concurrent use; the
// session cookie and crumb token are fetched lazily on first request and
// cached for the lifetime of the client.
type Client struct {
	httpClient *http.Client

	cookieURL string
	crumbURL  string
	quoteURL  string
	delay     time.Duration // pause between BatchFundamentals requests

	mu    sync.Mutex
	crumb string
}

// New returns a Client ready for use. No network calls are made until the
// first data request.
func New() *Client {
	jar, _ := cookiejar.New(nil) // only errors on bad options; nil is valid
	return &Client{
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: requestTimeout,
		},
		cookieURL: defaultCookieURL,
		crumbURL:  defaultCrumbURL,
		quoteURL:  defaultQuoteURL,
		delay:     batchDelay,
	}
}

// NSE returns the Yahoo Finance symbol for an NSE-listed stock,
// e.g. NSE("INFY") == "INFY.NS".
func NSE(symbol string) string {
	return symbol + ".NS"
}

// Fundamentals fetches fundamental data for one symbol. The symbol is
// passed to Yahoo as-is (use "INFY.NS" style suffixes for Indian listings).
// Missing fields are nil, not errors; an unknown symbol is an error.
func (c *Client) Fundamentals(symbol string) (*Fundamentals, error) {
	body, err := c.fetchQuoteSummary(symbol)
	if err != nil {
		return nil, err
	}
	return parseFundamentals(symbol, body)
}

// BatchFundamentals fetches many symbols sequentially with a polite delay
// between requests. Per-symbol errors are collected in the returned error
// map rather than aborting the batch.
func (c *Client) BatchFundamentals(symbols []string) (map[string]*Fundamentals, map[string]error) {
	results := make(map[string]*Fundamentals, len(symbols))
	errs := make(map[string]error)
	for i, symbol := range symbols {
		if i > 0 && c.delay > 0 {
			time.Sleep(c.delay)
		}
		f, err := c.Fundamentals(symbol)
		if err != nil {
			errs[symbol] = err
			continue
		}
		results[symbol] = f
	}
	return results, errs
}

// fetchQuoteSummary performs the authenticated quoteSummary request,
// refreshing the cookie+crumb session once on a 401/403.
func (c *Client) fetchQuoteSummary(symbol string) ([]byte, error) {
	crumb, err := c.ensureCrumb(false)
	if err != nil {
		return nil, err
	}

	body, status, err := c.getQuoteSummary(symbol, crumb)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		// Session expired: refresh once and retry, then surface the error.
		crumb, err = c.ensureCrumb(true)
		if err != nil {
			return nil, err
		}
		body, status, err = c.getQuoteSummary(symbol, crumb)
		if err != nil {
			return nil, err
		}
	}

	// Yahoo reports "quote not found" as a 404 with a JSON error payload;
	// let the parser extract the message. Other non-200s are plain errors.
	if status != http.StatusOK && status != http.StatusNotFound {
		return nil, fmt.Errorf("yfin: quoteSummary %s: HTTP %d", symbol, status)
	}
	return body, nil
}

func (c *Client) getQuoteSummary(symbol, crumb string) (body []byte, status int, err error) {
	u := c.quoteURL + "/" + url.PathEscape(symbol) +
		"?modules=" + url.QueryEscape(quoteModules) +
		"&crumb=" + url.QueryEscape(crumb)
	resp, err := c.get(u)
	if err != nil {
		return nil, 0, fmt.Errorf("yfin: quoteSummary %s: %w", symbol, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("yfin: quoteSummary %s: %w", symbol, err)
	}
	return body, resp.StatusCode, nil
}

// ensureCrumb returns the cached crumb, performing the cookie + crumb
// handshake if none is cached or if refresh is true.
func (c *Client) ensureCrumb(refresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.crumb != "" && !refresh {
		return c.crumb, nil
	}

	// Step 1: hit fc.yahoo.com to receive session cookies. The endpoint
	// returns a 404 by design; only the Set-Cookie headers matter and the
	// cookie jar captures those.
	resp, err := c.get(c.cookieURL)
	if err != nil {
		return "", fmt.Errorf("yfin: cookie handshake: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Step 2: exchange the cookie for a crumb token.
	resp, err = c.get(c.crumbURL)
	if err != nil {
		return "", fmt.Errorf("yfin: crumb fetch: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("yfin: crumb fetch: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("yfin: crumb fetch: HTTP %d", resp.StatusCode)
	}
	crumb := strings.TrimSpace(string(body))
	if crumb == "" || strings.Contains(crumb, "<") {
		return "", fmt.Errorf("yfin: crumb fetch: unexpected response %q", truncate(crumb, 80))
	}
	c.crumb = crumb
	return crumb, nil
}

func (c *Client) get(u string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	return c.httpClient.Do(req)
}

// rawValue models Yahoo's numeric encoding: {"raw": 24.5, "fmt": "24.50"}.
// Missing fields appear either as absent keys or as empty objects {}.
type rawValue struct {
	Raw *float64 `json:"raw"`
}

type quoteSummaryResponse struct {
	QuoteSummary struct {
		Result []struct {
			SummaryDetail struct {
				TrailingPE    rawValue `json:"trailingPE"`
				ForwardPE     rawValue `json:"forwardPE"`
				DividendYield rawValue `json:"dividendYield"`
			} `json:"summaryDetail"`
			DefaultKeyStatistics struct {
				PriceToBook  rawValue `json:"priceToBook"`
				TrailingEPS  rawValue `json:"trailingEps"`
				Week52Change rawValue `json:"52WeekChange"`
			} `json:"defaultKeyStatistics"`
			Price struct {
				MarketCap rawValue `json:"marketCap"`
			} `json:"price"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

func parseFundamentals(symbol string, body []byte) (*Fundamentals, error) {
	var qs quoteSummaryResponse
	if err := json.Unmarshal(body, &qs); err != nil {
		return nil, fmt.Errorf("yfin: quoteSummary %s: decode: %w", symbol, err)
	}
	if e := qs.QuoteSummary.Error; e != nil {
		return nil, fmt.Errorf("yfin: quoteSummary %s: %s: %s", symbol, e.Code, e.Description)
	}
	if len(qs.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("yfin: quoteSummary %s: empty result", symbol)
	}

	r := qs.QuoteSummary.Result[0]
	f := &Fundamentals{
		Symbol:        symbol,
		TrailingPE:    r.SummaryDetail.TrailingPE.Raw,
		ForwardPE:     r.SummaryDetail.ForwardPE.Raw,
		DividendYield: r.SummaryDetail.DividendYield.Raw,
		PriceToBook:   r.DefaultKeyStatistics.PriceToBook.Raw,
		TrailingEPS:   r.DefaultKeyStatistics.TrailingEPS.Raw,
		Week52Change:  r.DefaultKeyStatistics.Week52Change.Raw,
	}
	if mc := r.Price.MarketCap.Raw; mc != nil {
		v := int64(*mc)
		f.MarketCap = &v
	}
	return f, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
