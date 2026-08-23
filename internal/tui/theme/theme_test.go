package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTokensAreComplete guards against accidentally shipping a theme with
// zero-valued colors or metrics, which would render invisible UI.
func TestTokensAreComplete(t *testing.T) {
	tk := Dark()
	if tk.Name == "" {
		t.Fatal("theme name is empty")
	}
	for name, c := range map[string]any{
		"Bg": tk.Bg, "BgPanel": tk.BgPanel, "BgElevated": tk.BgElevated,
		"BgSelection": tk.BgSelection, "Border": tk.Border, "BorderFocused": tk.BorderFocused,
		"Text": tk.Text, "TextDim": tk.TextDim, "TextMuted": tk.TextMuted,
		"Accent": tk.Accent, "Accent2": tk.Accent2,
		"Success": tk.Success, "Warning": tk.Warning, "Danger": tk.Danger,
	} {
		if c == nil {
			t.Errorf("token %s is nil", name)
		}
	}
	for name, v := range map[string]int{
		"PadX": tk.PadX, "HeightTab": tk.HeightTab, "HeightState": tk.HeightState,
	} {
		if v <= 0 {
			t.Errorf("metric %s = %d, want > 0", name, v)
		}
	}
}

// TestAccentsDistinct ensures the two brand accents are actually different so
// gradients and dual-accent compositions remain visible.
func TestAccentsDistinct(t *testing.T) {
	tk := Dark()
	if hex(tk.Accent) == hex(tk.Accent2) {
		t.Fatal("Accent and Accent2 are identical")
	}
}

func hex(c any) string {
	type rgba interface{ RGBA() (r, g, b, a uint32) }
	cv, ok := c.(rgba)
	if !ok {
		return "<nil>"
	}
	r, g, b, _ := cv.RGBA()
	const max = 0xFFFF
	to := func(v uint32) uint32 { return v * 0xFF / max }
	return strings.ToLower(fmt.Sprintf("#%02x%02x%02x", to(r), to(g), to(b)))
}

func itoa(i int) string { return strconv.Itoa(i) }

// TestNoRawColorsOutsideTheme enforces the branding invariant: lipgloss colors
// may only be constructed inside the theme package. Components must request
// styles through theme helpers.
func TestNoRawColorsOutsideTheme(t *testing.T) {
	raw := regexp.MustCompile(`lipgloss\.Color\(`)
	roots := []string{"../kit", "../app", "../surfaces"}
	var offenders []string
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, line := range strings.Split(string(data), "\n") {
				if raw.MatchString(line) {
					offenders = append(offenders, path+":"+itoa(i+1)+" "+strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("raw lipgloss.Color usage outside theme package:\n%s",
			strings.Join(offenders, "\n"))
	}
}
