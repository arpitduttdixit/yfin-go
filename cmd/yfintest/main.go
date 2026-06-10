// Command yfintest is the live verification tool for yfin-go: it fetches
// fundamentals for a handful of NSE large caps and prints a table.
// Note the dev sandbox may block Yahoo; run this locally to verify.
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

	client := yfin.New()
	results, errs := client.BatchFundamentals(symbols)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SYMBOL\tP/E\tFWD P/E\tP/B\tEPS\tMCAP\tDIV YLD\t52W CHG")
	for _, symbol := range symbols {
		f, ok := results[symbol]
		if !ok {
			fmt.Fprintf(w, "%s\tERROR: %v\n", symbol, errs[symbol])
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Symbol,
			num(f.TrailingPE, "%.2f"),
			num(f.ForwardPE, "%.2f"),
			num(f.PriceToBook, "%.2f"),
			num(f.TrailingEPS, "%.2f"),
			mcap(f.MarketCap),
			pct(f.DividendYield),
			pct(f.Week52Change),
		)
	}
	w.Flush()

	if len(errs) > 0 {
		os.Exit(1)
	}
}

func num(v *float64, format string) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf(format, *v)
}

func pct(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *v*100)
}

func mcap(v *int64) string {
	if v == nil {
		return "-"
	}
	switch {
	case *v >= 1e12:
		return fmt.Sprintf("%.2fT", float64(*v)/1e12)
	case *v >= 1e9:
		return fmt.Sprintf("%.2fB", float64(*v)/1e9)
	default:
		return fmt.Sprintf("%d", *v)
	}
}
