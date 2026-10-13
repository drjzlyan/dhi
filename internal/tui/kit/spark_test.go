package kit

import "testing"

func TestSparkline(t *testing.T) {
	if got := Sparkline([]float64{0, 1, 2, 4, 8}); got != "▁▂▃▅█" {
		t.Fatalf("Sparkline = %q", got)
	}
	if got := Sparkline([]float64{0, 0}); got != "▁▁" {
		t.Fatalf("all zero = %q", got)
	}
	if got := Sparkline([]float64{0.01, 100}); got != "▂█" {
		t.Fatalf("tiny but non-zero must show: %q", got)
	}
}
