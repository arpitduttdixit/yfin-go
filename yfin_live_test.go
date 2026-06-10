package yfin

import (
	"os"
	"testing"
)

// TestLiveFundamentals hits the real Yahoo Finance API. It only runs when
// YFIN_LIVE=1 is set, since sandboxes and CI usually block Yahoo.
func TestLiveFundamentals(t *testing.T) {
	if os.Getenv("YFIN_LIVE") != "1" {
		t.Skip("set YFIN_LIVE=1 to run live Yahoo Finance tests")
	}

	c := New()
	f, err := c.Fundamentals("INFY.NS")
	if err != nil {
		t.Fatalf("live Fundamentals(INFY.NS): %v", err)
	}
	if f.Symbol != "INFY.NS" {
		t.Errorf("Symbol = %q, want INFY.NS", f.Symbol)
	}
	// Infosys is a large cap: market cap and trailing P/E should always be
	// present and positive.
	if f.MarketCap == nil || *f.MarketCap <= 0 {
		t.Errorf("MarketCap = %v, want positive value", f.MarketCap)
	}
	if f.TrailingPE == nil || *f.TrailingPE <= 0 {
		t.Errorf("TrailingPE = %v, want positive value", f.TrailingPE)
	}
}

func TestLiveBatchFundamentals(t *testing.T) {
	if os.Getenv("YFIN_LIVE") != "1" {
		t.Skip("set YFIN_LIVE=1 to run live Yahoo Finance tests")
	}

	c := New()
	results, errs := c.BatchFundamentals([]string{"INFY.NS", "RELIANCE.NS"})
	for sym, err := range errs {
		t.Errorf("live batch %s: %v", sym, err)
	}
	if len(results) != 2 {
		t.Errorf("results has %d entries, want 2", len(results))
	}
}
