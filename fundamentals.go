package yfin

import (
	"fmt"
	"net/url"
	"time"
)

// Fundamentals holds the valuation metrics for one symbol. Yahoo frequently
// omits fields (especially for small caps), so every metric is a pointer;
// nil means "not reported", never an error.
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

// rawValue is Yahoo's numeric encoding: {"raw": 24.5, "fmt": "24.50"}.
// Fields may also be {} or absent entirely, leaving Raw nil.
type rawValue struct {
	Raw *float64 `json:"raw"`
}

type quoteSummaryResponse struct {
	QuoteSummary struct {
		Result []struct {
			SummaryDetail *struct {
				TrailingPE    rawValue `json:"trailingPE"`
				ForwardPE     rawValue `json:"forwardPE"`
				DividendYield rawValue `json:"dividendYield"`
			} `json:"summaryDetail"`
			DefaultKeyStatistics *struct {
				PriceToBook  rawValue `json:"priceToBook"`
				TrailingEps  rawValue `json:"trailingEps"`
				Week52Change rawValue `json:"52WeekChange"`
			} `json:"defaultKeyStatistics"`
			Price *struct {
				MarketCap rawValue `json:"marketCap"`
			} `json:"price"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// Fundamentals fetches valuation metrics for one symbol. The symbol is
// passed to Yahoo as-is (e.g. "INFY.NS", "RELIANCE.NS", "AAPL").
func (c *Client) Fundamentals(symbol string) (*Fundamentals, error) {
	if symbol == "" {
		return nil, fmt.Errorf("yfin: symbol is required")
	}

	q := url.Values{}
	q.Set("modules", "summaryDetail,defaultKeyStatistics,price")

	var resp quoteSummaryResponse
	err := c.getJSON("/v10/finance/quoteSummary/"+url.PathEscape(symbol), q, &resp)
	if err != nil {
		return nil, err
	}
	if resp.QuoteSummary.Error != nil {
		return nil, &APIError{
			Code:        resp.QuoteSummary.Error.Code,
			Description: resp.QuoteSummary.Error.Description,
		}
	}
	if len(resp.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("yfin: no data for %q", symbol)
	}

	result := resp.QuoteSummary.Result[0]
	f := &Fundamentals{Symbol: symbol}
	if sd := result.SummaryDetail; sd != nil {
		f.TrailingPE = sd.TrailingPE.Raw
		f.ForwardPE = sd.ForwardPE.Raw
		f.DividendYield = sd.DividendYield.Raw
	}
	if ks := result.DefaultKeyStatistics; ks != nil {
		f.PriceToBook = ks.PriceToBook.Raw
		f.TrailingEPS = ks.TrailingEps.Raw
		f.Week52Change = ks.Week52Change.Raw
	}
	if p := result.Price; p != nil && p.MarketCap.Raw != nil {
		mc := int64(*p.MarketCap.Raw)
		f.MarketCap = &mc
	}
	return f, nil
}

// BatchFundamentals fetches many symbols sequentially with a polite delay
// (~500ms) between requests. Per-symbol errors are collected in the second
// return value rather than aborting the batch.
func (c *Client) BatchFundamentals(symbols []string) (map[string]*Fundamentals, map[string]error) {
	results := make(map[string]*Fundamentals)
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
