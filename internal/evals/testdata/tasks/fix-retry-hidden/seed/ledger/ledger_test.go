package ledger

import "testing"

func TestTotalNetsRefunds(t *testing.T) {
	got := Total([]Entry{{Kind: Sale, Cents: 500}, {Kind: Refund, Cents: 200}})
	if got != 300 {
		t.Fatalf("Total = %d, want 300", got)
	}
}
