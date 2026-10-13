// Package ledger totals a shop's sales and refunds.
package ledger

// Kind is what an entry records.
type Kind int

const (
	Sale Kind = iota
	Refund
)

// Entry is one ledger line, in cents.
type Entry struct {
	Kind  Kind
	Cents int64
}

// Total is the net amount in cents: sales minus refunds.
func Total(entries []Entry) int64 {
	var sum int32
	for _, e := range entries {
		sum += int32(e.Cents)
	}
	return int64(sum)
}
