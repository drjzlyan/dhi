// Package report renders ledger summaries.
package report

import (
	"fmt"

	"example.com/shop/ledger"
)

// Summary is the one-line net figure for a set of entries.
func Summary(entries []ledger.Entry) string {
	return fmt.Sprintf("net %d cents", ledger.Total(entries))
}
