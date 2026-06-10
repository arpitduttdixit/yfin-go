// Command yfintest is the live verification tool for yfin-go: it fetches
// fundamentals for a handful of NSE large caps from Yahoo Finance and
// prints them as a table. Run it from a network that can reach
// fc.yahoo.com and query1.finance.yahoo.com.
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	yfin "github.com/arpitduttdixit/yfin-go"
)

func main() {
	symbols := []string{"INFY.NS", "RELIANCE.NS", "TCS.NS", "HDFCBANK.NS"}
	if len(os.Args) > 1 {
		symbols = os.Args[1:]
	}

	c := yfin.New()
	results, errs := c.BatchFundamentals(symbols)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SYMBOL\tTRAIL P/E\tFWD P/E\tP/B\tEPS\tMKT CAP\tDIV YLD\t52W CHG")
	for _, sym := range symbols {
		f, ok := results[sym]
		if !ok {
			fmt.Fprintf(w, "%s\tERROR: %v\n", sym, errs[sym])
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Symbol,
			fmtFloat(f.TrailingPE, "%.2f"),
			fmtFloat(f.ForwardPE, "%.2f"),
			fmtFloat(f.PriceToBook, "%.2f"),
			fmtFloat(f.TrailingEPS, "%.2f"),
			fmtMarketCap(f.MarketCap),
			fmtPercent(f.DividendYield),
			fmtPercent(f.Week52Change),
		)
	}
	w.Flush()

	if len(errs) > 0 {
		os.Exit(1)
	}
}

func fmtFloat(p *float64, format string) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf(format, *p)
}

func fmtPercent(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *p*100)
}

func fmtMarketCap(p *int64) string {
	if p == nil {
		return "-"
	}
	v := float64(*p)
	switch {
	case v >= 1e12:
		return fmt.Sprintf("%.2fT", v/1e12)
	case v >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	default:
		return fmt.Sprintf("%d", *p)
	}
}
