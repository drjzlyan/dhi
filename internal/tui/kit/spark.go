package kit

import "github.com/drjzlyan/dhi/internal/tui/theme"

// Sparkline renders values as one row of bar glyphs scaled to the max
// (all zero = the lowest bar). Deterministic, uncoloured: callers style.
func Sparkline(values []float64) string {
	bars := []rune(theme.SparkBars)
	hi := 0.0
	for _, v := range values {
		hi = max(hi, v)
	}
	out := make([]rune, len(values))
	for i, v := range values {
		lvl := 0
		if hi > 0 && v > 0 {
			lvl = min(int(v/hi*float64(len(bars)-1)+0.5), len(bars)-1)
			lvl = max(lvl, 1) // any spend is visible
		}
		out[i] = bars[lvl]
	}
	return string(out)
}
