package kit

import "testing"

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		key             string
		takingText, raw bool
		want            string
		option          bool
	}{
		{"¡", false, false, "alt+1", true},
		{"º", false, false, "alt+0", true},
		{"¡", true, false, "¡", false}, // typing: Option+1 is text
		{"¡", false, true, "¡", false}, // terminal: passed through
		{"alt+b", true, false, "alt+left", false},
		{"alt+f", false, false, "alt+right", false},
		{"alt+b", false, true, "alt+b", false}, // readline gets ESC b
		{"alt+2", false, false, "alt+2", false},
		{"x", false, false, "x", false},
	}
	for _, c := range cases {
		got, opt := NormalizeKey(c.key, c.takingText, c.raw)
		if got != c.want || opt != c.option {
			t.Errorf("NormalizeKey(%q, text=%v, raw=%v) = %q,%v; want %q,%v",
				c.key, c.takingText, c.raw, got, opt, c.want, c.option)
		}
	}
}

func TestAltLabel(t *testing.T) {
	defer func() { MacKeys = false }()
	if got := AltLabel("alt+1-5 views"); got != "alt+1-5 views" {
		t.Fatalf("portable: %q", got)
	}
	MacKeys = true
	if got := AltLabel("alt+1-5 views"); got != "⌥1-5 views" {
		t.Fatalf("mac: %q", got)
	}
}
