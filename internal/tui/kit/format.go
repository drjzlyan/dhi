package kit

import "fmt"

// FormatBytes renders a byte count for people: "0 B", "512 B", "1.8 MB",
// "157.0 MB" (decimal units, one decimal above bytes). Negative counts
// render as unknown ("—").
func FormatBytes(n int64) string {
	switch {
	case n < 0:
		return "—"
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"kB", "MB", "GB", "TB"}
	v := float64(n) / 1000
	for _, u := range units {
		if v < 1000 || u == "TB" {
			return fmt.Sprintf("%.1f %s", v, u)
		}
		v /= 1000
	}
	return ""
}
