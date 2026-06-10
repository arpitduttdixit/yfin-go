package yfin

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client pointed at srv for both the cookie
// handshake and data requests, with no batch delay.
func newTestClient(srv *httptest.Server) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second, Jar: jar},
		baseURL:    srv.URL,
		cookieURL:  srv.URL + "/cookie",
		delay:      0,
	}
}

// mockYahoo simulates the cookie+crumb handshake and the quoteSummary
// endpoint, serving fixture bytes per symbol.
type mockYahoo struct {
	t        *testing.T
	crumb    string
	fixtures map[string]string // symbol -> testdata file
	// statuses maps symbol -> HTTP status (default 200)
	statuses map[string]int

	crumbCalls atomic.Int64
	dataCalls  atomic.Int64
}

func (m *mockYahoo) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "A3", Value: "test-session"})
		// fc.yahoo.com responds 404; the client must tolerate that.
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/v1/test/getcrumb", func(w http.ResponseWriter, r *http.Request) {
		m.crumbCalls.Add(1)
		if c, err := r.Cookie("A3"); err != nil || c.Value != "test-session" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(m.crumb))
	})
	mux.HandleFunc("/v10/finance/quoteSummary/", func(w http.ResponseWriter, r *http.Request) {
		m.dataCalls.Add(1)
		if strings.Contains(r.UserAgent(), "Go-http-client") {
			m.t.Errorf("default Go user agent sent: %q", r.UserAgent())
		}
		if r.URL.Query().Get("crumb") != m.crumb {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"finance":{"error":{"code":"Unauthorized","description":"Invalid Crumb"}}}`))
			return
		}
		symbol := strings.TrimPrefix(r.URL.Path, "/v10/finance/quoteSummary/")
		if status, ok := m.statuses[symbol]; ok && status != http.StatusOK {
			w.WriteHeader(status)
		}
		fixture, ok := m.fixtures[symbol]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		data, err := os.ReadFile(fixture)
		if err != nil {
			m.t.Fatalf("read fixture: %v", err)
		}
		w.Write(data)
	})
	return mux
}

func TestFundamentals(t *testing.T) {
	mock := &mockYahoo{t: t, crumb: "abc123", fixtures: map[string]string{
		"INFY.NS": "testdata/quotesummary_infy.json",
	}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	c := newTestClient(srv)

	f, err := c.Fundamentals("INFY.NS")
	if err != nil {
		t.Fatalf("Fundamentals: %v", err)
	}
	if f.Symbol != "INFY.NS" {
		t.Errorf("Symbol = %q, want INFY.NS", f.Symbol)
	}
	checkFloat(t, "TrailingPE", f.TrailingPE, 24.53)
	checkFloat(t, "ForwardPE", f.ForwardPE, 21.87)
	checkFloat(t, "PriceToBook", f.PriceToBook, 7.12)
	checkFloat(t, "TrailingEPS", f.TrailingEPS, 63.39)
	checkFloat(t, "DividendYield", f.DividendYield, 0.0289)
	checkFloat(t, "Week52Change", f.Week52Change, -0.1234)
	if f.MarketCap == nil || *f.MarketCap != 6450000000000 {
		t.Errorf("MarketCap = %v, want 6450000000000", f.MarketCap)
	}
}

func TestFundamentalsSparseFields(t *testing.T) {
	mock := &mockYahoo{t: t, crumb: "abc123", fixtures: map[string]string{
		"SMALLCAP.NS": "testdata/quotesummary_sparse.json",
	}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	c := newTestClient(srv)

	f, err := c.Fundamentals("SMALLCAP.NS")
	if err != nil {
		t.Fatalf("missing fields must not be an error, got: %v", err)
	}
	if f.TrailingPE != nil || f.ForwardPE != nil || f.TrailingEPS != nil ||
		f.MarketCap != nil || f.DividendYield != nil || f.Week52Change != nil {
		t.Errorf("expected nil for absent fields, got %+v", f)
	}
	checkFloat(t, "PriceToBook", f.PriceToBook, 1.45)
}

func TestFundamentalsNotFound(t *testing.T) {
	mock := &mockYahoo{
		t: t, crumb: "abc123",
		fixtures: map[string]string{"NOSUCH.NS": "testdata/quotesummary_notfound.json"},
		statuses: map[string]int{"NOSUCH.NS": http.StatusNotFound},
	}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	c := newTestClient(srv)

	_, err := c.Fundamentals("NOSUCH.NS")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "Not Found" {
		t.Errorf("Code = %q, want \"Not Found\"", apiErr.Code)
	}
}

func TestCrumbRefreshOnUnauthorized(t *testing.T) {
	mock := &mockYahoo{t: t, crumb: "fresh", fixtures: map[string]string{
		"INFY.NS": "testdata/quotesummary_infy.json",
	}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	c := newTestClient(srv)
	c.crumb = "stale" // simulate an expired cached crumb

	f, err := c.Fundamentals("INFY.NS")
	if err != nil {
		t.Fatalf("expected refresh+retry to succeed, got: %v", err)
	}
	if f.TrailingPE == nil {
		t.Error("expected data after crumb refresh")
	}
	if got := mock.crumbCalls.Load(); got != 1 {
		t.Errorf("crumb endpoint called %d times, want 1", got)
	}
	if got := mock.dataCalls.Load(); got != 2 {
		t.Errorf("data endpoint called %d times, want 2 (fail + retry)", got)
	}
}

func TestCrumbIsCachedAcrossRequests(t *testing.T) {
	mock := &mockYahoo{t: t, crumb: "abc123", fixtures: map[string]string{
		"INFY.NS": "testdata/quotesummary_infy.json",
	}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	c := newTestClient(srv)

	for i := 0; i < 3; i++ {
		if _, err := c.Fundamentals("INFY.NS"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if got := mock.crumbCalls.Load(); got != 1 {
		t.Errorf("crumb endpoint called %d times, want 1", got)
	}
}

func TestBatchFundamentals(t *testing.T) {
	mock := &mockYahoo{t: t, crumb: "abc123", fixtures: map[string]string{
		"INFY.NS":     "testdata/quotesummary_infy.json",
		"SMALLCAP.NS": "testdata/quotesummary_sparse.json",
	}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	c := newTestClient(srv)

	results, errs := c.BatchFundamentals([]string{"INFY.NS", "NOSUCH.NS", "SMALLCAP.NS"})
	if len(results) != 2 {
		t.Errorf("got %d results, want 2: %v", len(results), results)
	}
	if len(errs) != 1 {
		t.Errorf("got %d errors, want 1: %v", len(errs), errs)
	}
	if _, ok := errs["NOSUCH.NS"]; !ok {
		t.Error("expected an error entry for NOSUCH.NS")
	}
	if results["INFY.NS"] == nil || results["SMALLCAP.NS"] == nil {
		t.Error("a failing symbol must not abort the rest of the batch")
	}
}

func TestNSE(t *testing.T) {
	if got := NSE("INFY"); got != "INFY.NS" {
		t.Errorf("NSE(INFY) = %q, want INFY.NS", got)
	}
}

// TestLive hits the real Yahoo API. Enable with YFIN_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("YFIN_LIVE") != "1" {
		t.Skip("set YFIN_LIVE=1 to run live tests")
	}
	c := New()
	f, err := c.Fundamentals("INFY.NS")
	if err != nil {
		t.Fatalf("live Fundamentals(INFY.NS): %v", err)
	}
	if f.TrailingPE == nil {
		t.Error("expected INFY.NS to report a trailing P/E")
	}
	t.Logf("INFY.NS: %+v", f)
}

func checkFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}
