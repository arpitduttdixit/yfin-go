package yfin

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testCrumb = "AbC123xYz"

// yahooStub mocks the cookie handshake, crumb endpoint, and quoteSummary
// endpoint on a single httptest server.
type yahooStub struct {
	t *testing.T

	cookieHits atomic.Int64
	crumbHits  atomic.Int64
	quoteHits  atomic.Int64

	// reject401Once makes the first quoteSummary request fail with 401,
	// simulating an expired session.
	reject401Once atomic.Bool

	// fixtures maps symbol -> fixture filename served for it. Unknown
	// symbols get the not-found fixture with a 404 status.
	fixtures map[string]string
}

func (s *yahooStub) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		s.cookieHits.Add(1)
		s.checkUA(r)
		http.SetCookie(w, &http.Cookie{Name: "A3", Value: "session-cookie", Path: "/"})
		http.Error(w, "Not Found", http.StatusNotFound) // fc.yahoo.com 404s by design
	})

	mux.HandleFunc("/v1/test/getcrumb", func(w http.ResponseWriter, r *http.Request) {
		s.crumbHits.Add(1)
		s.checkUA(r)
		if c, err := r.Cookie("A3"); err != nil || c.Value != "session-cookie" {
			http.Error(w, "missing session cookie", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(testCrumb))
	})

	mux.HandleFunc("/v10/finance/quoteSummary/", func(w http.ResponseWriter, r *http.Request) {
		s.quoteHits.Add(1)
		s.checkUA(r)
		if s.reject401Once.CompareAndSwap(true, false) {
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		if c, err := r.Cookie("A3"); err != nil || c.Value != "session-cookie" {
			http.Error(w, "missing session cookie", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("crumb") != testCrumb {
			http.Error(w, "bad crumb", http.StatusUnauthorized)
			return
		}
		if got := r.URL.Query().Get("modules"); got != quoteModules {
			s.t.Errorf("modules query = %q, want %q", got, quoteModules)
		}
		symbol := strings.TrimPrefix(r.URL.Path, "/v10/finance/quoteSummary/")
		fixture, ok := s.fixtures[symbol]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fixture = "quotesummary_notfound.json"
		}
		body, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			s.t.Fatalf("read fixture %s: %v", fixture, err)
		}
		w.Write(body)
	})

	return mux
}

func (s *yahooStub) checkUA(r *http.Request) {
	if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "Mozilla") {
		s.t.Errorf("request %s sent non-browser User-Agent %q", r.URL.Path, ua)
	}
}

func newTestClient(t *testing.T, stub *yahooStub) *Client {
	t.Helper()
	stub.t = t
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{Jar: jar, Timeout: 5 * time.Second},
		cookieURL:  srv.URL + "/cookie",
		crumbURL:   srv.URL + "/v1/test/getcrumb",
		quoteURL:   srv.URL + "/v10/finance/quoteSummary",
		delay:      0,
	}
}

func TestFundamentals(t *testing.T) {
	stub := &yahooStub{fixtures: map[string]string{"INFY.NS": "quotesummary_infy.json"}}
	c := newTestClient(t, stub)

	f, err := c.Fundamentals("INFY.NS")
	if err != nil {
		t.Fatalf("Fundamentals: %v", err)
	}

	if f.Symbol != "INFY.NS" {
		t.Errorf("Symbol = %q, want INFY.NS", f.Symbol)
	}
	wantFloat(t, "TrailingPE", f.TrailingPE, 24.53)
	wantFloat(t, "ForwardPE", f.ForwardPE, 21.87)
	wantFloat(t, "DividendYield", f.DividendYield, 0.0285)
	wantFloat(t, "PriceToBook", f.PriceToBook, 7.12)
	wantFloat(t, "TrailingEPS", f.TrailingEPS, 63.39)
	wantFloat(t, "Week52Change", f.Week52Change, 0.1342)
	if f.MarketCap == nil || *f.MarketCap != 6457000000000 {
		t.Errorf("MarketCap = %v, want 6457000000000", f.MarketCap)
	}
}

func TestFundamentalsSparse(t *testing.T) {
	stub := &yahooStub{fixtures: map[string]string{"SMALLCAP.NS": "quotesummary_sparse.json"}}
	c := newTestClient(t, stub)

	f, err := c.Fundamentals("SMALLCAP.NS")
	if err != nil {
		t.Fatalf("Fundamentals: %v", err)
	}

	wantFloat(t, "PriceToBook", f.PriceToBook, 1.45)
	for name, p := range map[string]*float64{
		"TrailingPE":    f.TrailingPE,
		"ForwardPE":     f.ForwardPE,
		"DividendYield": f.DividendYield,
		"TrailingEPS":   f.TrailingEPS,
		"Week52Change":  f.Week52Change,
	} {
		if p != nil {
			t.Errorf("%s = %v, want nil for missing field", name, *p)
		}
	}
	if f.MarketCap != nil {
		t.Errorf("MarketCap = %v, want nil for missing field", *f.MarketCap)
	}
}

func TestFundamentalsNotFound(t *testing.T) {
	stub := &yahooStub{fixtures: map[string]string{}}
	c := newTestClient(t, stub)

	_, err := c.Fundamentals("NOSUCHSYM.NS")
	if err == nil {
		t.Fatal("Fundamentals: want error for unknown symbol, got nil")
	}
	if !strings.Contains(err.Error(), "Quote not found") {
		t.Errorf("error = %q, want Yahoo's not-found description in it", err)
	}
}

func TestCrumbCachedAcrossRequests(t *testing.T) {
	stub := &yahooStub{fixtures: map[string]string{"INFY.NS": "quotesummary_infy.json"}}
	c := newTestClient(t, stub)

	for i := 0; i < 3; i++ {
		if _, err := c.Fundamentals("INFY.NS"); err != nil {
			t.Fatalf("Fundamentals #%d: %v", i, err)
		}
	}
	if got := stub.cookieHits.Load(); got != 1 {
		t.Errorf("cookie handshake ran %d times, want 1", got)
	}
	if got := stub.crumbHits.Load(); got != 1 {
		t.Errorf("crumb fetch ran %d times, want 1", got)
	}
}

func TestCrumbRefreshOn401(t *testing.T) {
	stub := &yahooStub{fixtures: map[string]string{"INFY.NS": "quotesummary_infy.json"}}
	stub.reject401Once.Store(true)
	c := newTestClient(t, stub)

	f, err := c.Fundamentals("INFY.NS")
	if err != nil {
		t.Fatalf("Fundamentals after 401: %v", err)
	}
	wantFloat(t, "TrailingPE", f.TrailingPE, 24.53)

	if got := stub.crumbHits.Load(); got != 2 {
		t.Errorf("crumb fetched %d times, want 2 (initial + refresh)", got)
	}
	if got := stub.quoteHits.Load(); got != 2 {
		t.Errorf("quoteSummary hit %d times, want 2 (401 + retry)", got)
	}
}

func TestPersistent401Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cookie"):
			http.SetCookie(w, &http.Cookie{Name: "A3", Value: "x", Path: "/"})
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/v1/test/getcrumb"):
			w.Write([]byte(testCrumb))
		default:
			http.Error(w, "nope", http.StatusForbidden)
		}
	}))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	c := &Client{
		httpClient: &http.Client{Jar: jar, Timeout: 5 * time.Second},
		cookieURL:  srv.URL + "/cookie",
		crumbURL:   srv.URL + "/v1/test/getcrumb",
		quoteURL:   srv.URL + "/v10/finance/quoteSummary",
	}

	_, err := c.Fundamentals("INFY.NS")
	if err == nil {
		t.Fatal("want error after persistent 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %q, want HTTP 403 mentioned", err)
	}
}

func TestBatchFundamentals(t *testing.T) {
	stub := &yahooStub{fixtures: map[string]string{
		"INFY.NS":     "quotesummary_infy.json",
		"SMALLCAP.NS": "quotesummary_sparse.json",
	}}
	c := newTestClient(t, stub)

	results, errs := c.BatchFundamentals([]string{"INFY.NS", "BOGUS.NS", "SMALLCAP.NS"})

	if len(results) != 2 {
		t.Errorf("results has %d entries, want 2: %v", len(results), results)
	}
	if results["INFY.NS"] == nil || results["SMALLCAP.NS"] == nil {
		t.Errorf("missing expected symbols in results: %v", results)
	}
	if len(errs) != 1 || errs["BOGUS.NS"] == nil {
		t.Errorf("errs = %v, want exactly one error for BOGUS.NS", errs)
	}
}

func TestNSE(t *testing.T) {
	if got := NSE("INFY"); got != "INFY.NS" {
		t.Errorf("NSE(INFY) = %q, want INFY.NS", got)
	}
}

func wantFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}
