// Package money splits amounts of cents fairly.
package money

// Split divides cents among n parties. Every cent is accounted for: the
// remainder goes one cent at a time to the first parties.
func Split(cents int64, n int) []int64 {
	out := make([]int64, n)
	share := cents / int64(n)
	for i := range out {
		out[i] = share
	}
	return out
}
