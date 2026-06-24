package yfin

import (
	"fmt"
	"net/url"
	"time"
)

// Fundamentals holds the valuation, quality, analyst, and profile metrics for
// one symbol. Yahoo frequently omits fields (especially for small caps and
// non-US listings), so every metric is a pointer; nil means "not reported",
// never an error.
type Fundamentals struct {
	Symbol string

	// --- Valuation (summaryDetail / defaultKeyStatistics / price) ---
	TrailingPE         *float64
	ForwardPE          *float64
	PriceToBook        *float64
	TrailingEPS        *float64
	PegRatio           *float64
	EnterpriseValue    *int64
	EnterpriseToEbitda *float64
	BookValue          *float64
	MarketCap          *int64
	DividendYield      *float64
	PayoutRatio        *float64
	Week52Change       *float64

	// --- Quality & financial health (financialData) ---
	ReturnOnEquity    *float64
	ReturnOnAssets    *float64
	ProfitMargins     *float64
	OperatingMargins  *float64
	GrossMargins      *float64
	DebtToEquity      *float64
	CurrentRatio      *float64
	QuickRatio        *float64
	TotalCash         *int64
	TotalDebt         *int64
	FreeCashflow      *int64
	OperatingCashflow *int64
	RevenueGrowth     *float64
	EarningsGrowth    *float64

	// --- Analyst coverage (financialData) ---
	RecommendationKey       *string // "buy", "hold", "sell", ...
	RecommendationMean      *float64
	NumberOfAnalystOpinions *int64
	CurrentPrice            *float64
	TargetMeanPrice         *float64
	TargetHighPrice         *float64
	TargetLowPrice          *float64

	// --- Trading context (summaryDetail / defaultKeyStatistics) ---
	Beta                 *float64
	FiftyTwoWeekHigh     *float64
	FiftyTwoWeekLow      *float64
	FiftyDayAverage      *float64
	TwoHundredDayAverage *float64
	AverageVolume        *int64
	SharesOutstanding    *int64
	FloatShares          *int64
	SharesShort          *int64
	ShortRatio           *float64
	ShortPercentOfFloat  *float64

	// --- Company profile (assetProfile) ---
	Sector            *string
	Industry          *string
	Country           *string
	FullTimeEmployees *int64

	// --- Calendar (calendarEvents) ---
	NextEarningsDate *time.Time
	ExDividendDate   *time.Time
	DividendDate     *time.Time

	// FinancialCurrency is the currency the financial-health figures are
	// reported in (e.g. "INR"); may differ from the trading currency.
	FinancialCurrency *string
}

// rawValue is Yahoo's numeric encoding: {"raw": 24.5, "fmt": "24.50"}.
// Fields may also be {} or absent entirely, leaving Raw nil.
type rawValue struct {
	Raw *float64 `json:"raw"`
}

// intPtr returns the value truncated to int64, or nil when unreported. Used for
// share counts and monetary figures Yahoo encodes as floats.
func (rv rawValue) intPtr() *int64 {
	if rv.Raw == nil {
		return nil
	}
	v := int64(*rv.Raw)
	return &v
}

// timePtr interprets the value as a Unix timestamp (seconds), or nil.
func (rv rawValue) timePtr() *time.Time {
	if rv.Raw == nil {
		return nil
	}
	t := time.Unix(int64(*rv.Raw), 0).UTC()
	return &t
}

// strPtr returns a pointer to s, or nil when s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type quoteSummaryResponse struct {
	QuoteSummary struct {
		Result []struct {
			SummaryDetail *struct {
				TrailingPE           rawValue `json:"trailingPE"`
				ForwardPE            rawValue `json:"forwardPE"`
				DividendYield        rawValue `json:"dividendYield"`
				PayoutRatio          rawValue `json:"payoutRatio"`
				Beta                 rawValue `json:"beta"`
				FiftyTwoWeekHigh     rawValue `json:"fiftyTwoWeekHigh"`
				FiftyTwoWeekLow      rawValue `json:"fiftyTwoWeekLow"`
				FiftyDayAverage      rawValue `json:"fiftyDayAverage"`
				TwoHundredDayAverage rawValue `json:"twoHundredDayAverage"`
				AverageVolume        rawValue `json:"averageVolume"`
			} `json:"summaryDetail"`
			DefaultKeyStatistics *struct {
				PriceToBook         rawValue `json:"priceToBook"`
				TrailingEps         rawValue `json:"trailingEps"`
				Week52Change        rawValue `json:"52WeekChange"`
				PegRatio            rawValue `json:"pegRatio"`
				EnterpriseValue     rawValue `json:"enterpriseValue"`
				EnterpriseToEbitda  rawValue `json:"enterpriseToEbitda"`
				BookValue           rawValue `json:"bookValue"`
				SharesOutstanding   rawValue `json:"sharesOutstanding"`
				FloatShares         rawValue `json:"floatShares"`
				SharesShort         rawValue `json:"sharesShort"`
				ShortRatio          rawValue `json:"shortRatio"`
				ShortPercentOfFloat rawValue `json:"shortPercentOfFloat"`
			} `json:"defaultKeyStatistics"`
			Price *struct {
				MarketCap rawValue `json:"marketCap"`
			} `json:"price"`
			FinancialData *struct {
				CurrentPrice            rawValue `json:"currentPrice"`
				TargetHighPrice         rawValue `json:"targetHighPrice"`
				TargetLowPrice          rawValue `json:"targetLowPrice"`
				TargetMeanPrice         rawValue `json:"targetMeanPrice"`
				RecommendationMean      rawValue `json:"recommendationMean"`
				RecommendationKey       string   `json:"recommendationKey"`
				NumberOfAnalystOpinions rawValue `json:"numberOfAnalystOpinions"`
				TotalCash               rawValue `json:"totalCash"`
				TotalDebt               rawValue `json:"totalDebt"`
				QuickRatio              rawValue `json:"quickRatio"`
				CurrentRatio            rawValue `json:"currentRatio"`
				DebtToEquity            rawValue `json:"debtToEquity"`
				ReturnOnAssets          rawValue `json:"returnOnAssets"`
				ReturnOnEquity          rawValue `json:"returnOnEquity"`
				FreeCashflow            rawValue `json:"freeCashflow"`
				OperatingCashflow       rawValue `json:"operatingCashflow"`
				EarningsGrowth          rawValue `json:"earningsGrowth"`
				RevenueGrowth           rawValue `json:"revenueGrowth"`
				GrossMargins            rawValue `json:"grossMargins"`
				OperatingMargins        rawValue `json:"operatingMargins"`
				ProfitMargins           rawValue `json:"profitMargins"`
				FinancialCurrency       string   `json:"financialCurrency"`
			} `json:"financialData"`
			AssetProfile *struct {
				Sector            string `json:"sector"`
				Industry          string `json:"industry"`
				Country           string `json:"country"`
				FullTimeEmployees *int64 `json:"fullTimeEmployees"`
			} `json:"assetProfile"`
			CalendarEvents *struct {
				Earnings struct {
					EarningsDate []rawValue `json:"earningsDate"`
				} `json:"earnings"`
				ExDividendDate rawValue `json:"exDividendDate"`
				DividendDate   rawValue `json:"dividendDate"`
			} `json:"calendarEvents"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// quoteSummaryModules is the set of Yahoo quoteSummary modules requested in a
// single call. Adding a module here (and the matching struct fields above) is
// all it takes to surface more data — no extra HTTP round-trip per symbol.
const quoteSummaryModules = "summaryDetail,defaultKeyStatistics,price,financialData,assetProfile,calendarEvents"

// Fundamentals fetches valuation, quality, analyst, and profile metrics for one
// symbol. The symbol is passed to Yahoo as-is (e.g. "INFY.NS", "RELIANCE.NS",
// "AAPL").
func (c *Client) Fundamentals(symbol string) (*Fundamentals, error) {
	if symbol == "" {
		return nil, fmt.Errorf("yfin: symbol is required")
	}

	q := url.Values{}
	q.Set("modules", quoteSummaryModules)

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
		f.PayoutRatio = sd.PayoutRatio.Raw
		f.Beta = sd.Beta.Raw
		f.FiftyTwoWeekHigh = sd.FiftyTwoWeekHigh.Raw
		f.FiftyTwoWeekLow = sd.FiftyTwoWeekLow.Raw
		f.FiftyDayAverage = sd.FiftyDayAverage.Raw
		f.TwoHundredDayAverage = sd.TwoHundredDayAverage.Raw
		f.AverageVolume = sd.AverageVolume.intPtr()
	}
	if ks := result.DefaultKeyStatistics; ks != nil {
		f.PriceToBook = ks.PriceToBook.Raw
		f.TrailingEPS = ks.TrailingEps.Raw
		f.Week52Change = ks.Week52Change.Raw
		f.PegRatio = ks.PegRatio.Raw
		f.EnterpriseValue = ks.EnterpriseValue.intPtr()
		f.EnterpriseToEbitda = ks.EnterpriseToEbitda.Raw
		f.BookValue = ks.BookValue.Raw
		f.SharesOutstanding = ks.SharesOutstanding.intPtr()
		f.FloatShares = ks.FloatShares.intPtr()
		f.SharesShort = ks.SharesShort.intPtr()
		f.ShortRatio = ks.ShortRatio.Raw
		f.ShortPercentOfFloat = ks.ShortPercentOfFloat.Raw
	}
	if p := result.Price; p != nil {
		f.MarketCap = p.MarketCap.intPtr()
	}
	if fd := result.FinancialData; fd != nil {
		f.CurrentPrice = fd.CurrentPrice.Raw
		f.TargetHighPrice = fd.TargetHighPrice.Raw
		f.TargetLowPrice = fd.TargetLowPrice.Raw
		f.TargetMeanPrice = fd.TargetMeanPrice.Raw
		f.RecommendationMean = fd.RecommendationMean.Raw
		f.RecommendationKey = strPtr(fd.RecommendationKey)
		f.NumberOfAnalystOpinions = fd.NumberOfAnalystOpinions.intPtr()
		f.TotalCash = fd.TotalCash.intPtr()
		f.TotalDebt = fd.TotalDebt.intPtr()
		f.QuickRatio = fd.QuickRatio.Raw
		f.CurrentRatio = fd.CurrentRatio.Raw
		f.DebtToEquity = fd.DebtToEquity.Raw
		f.ReturnOnAssets = fd.ReturnOnAssets.Raw
		f.ReturnOnEquity = fd.ReturnOnEquity.Raw
		f.FreeCashflow = fd.FreeCashflow.intPtr()
		f.OperatingCashflow = fd.OperatingCashflow.intPtr()
		f.EarningsGrowth = fd.EarningsGrowth.Raw
		f.RevenueGrowth = fd.RevenueGrowth.Raw
		f.GrossMargins = fd.GrossMargins.Raw
		f.OperatingMargins = fd.OperatingMargins.Raw
		f.ProfitMargins = fd.ProfitMargins.Raw
		f.FinancialCurrency = strPtr(fd.FinancialCurrency)
	}
	if ap := result.AssetProfile; ap != nil {
		f.Sector = strPtr(ap.Sector)
		f.Industry = strPtr(ap.Industry)
		f.Country = strPtr(ap.Country)
		f.FullTimeEmployees = ap.FullTimeEmployees
	}
	if ce := result.CalendarEvents; ce != nil {
		if dates := ce.Earnings.EarningsDate; len(dates) > 0 {
			f.NextEarningsDate = dates[0].timePtr()
		}
		f.ExDividendDate = ce.ExDividendDate.timePtr()
		f.DividendDate = ce.DividendDate.timePtr()
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
