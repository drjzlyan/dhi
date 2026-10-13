// Package text formats short labels for a dashboard.
package text

// Truncate shortens s to at most n characters, ending with "…" when cut.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Percent is part/whole as a whole percentage, rounded to nearest.
func Percent(part, whole int) int {
	if whole == 0 {
		return 0
	}
	return part * 100 / whole
}

// Average is the mean of xs, rounded down; 0 for none.
func Average(xs []int32) int32 {
	if len(xs) == 0 {
		return 0
	}
	var sum int32
	for _, x := range xs {
		sum += x
	}
	return sum / int32(len(xs))
}
