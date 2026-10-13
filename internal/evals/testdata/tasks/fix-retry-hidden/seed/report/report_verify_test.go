//go:build verify

package report

import (
	"testing"

	"example.com/shop/ledger"
)

// A quarter's sales run to billions of cents.
func TestSummaryLargeQuarter(t *testing.T) {
	es := []ledger.Entry{
		{Kind: ledger.Sale, Cents: 1_500_000_000},
		{Kind: ledger.Sale, Cents: 1_500_000_000},
		{Kind: ledger.Refund, Cents: 250_000_000},
	}
	if got, want := Summary(es), "net 2750000000 cents"; got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}
