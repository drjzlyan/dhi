// Package branding renders DHI's brand assets (logo, tagline, hero blocks).
// One source of truth reused by the Workspace landing view and the animated
// bootstrap surface (M1).
package branding

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Logo returns the gradient-rendered DHI block-logo lines.
func Logo() []string {
	rows := [6][2]string{
		{"██████╗ ", "██╗  ██╗██╗"},
		{"██╔══██╗", "██║  ██║██║"},
		{"██║  ██║", "███████║██║"},
		{"██║  ██║", "██╔══██║██║"},
		{"██████╔╝", "██║  ██║██║"},
		{"╚═════╝ ", "╚═╝  ╚═╝╚═╝"},
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		t := float64(i) / float64(len(rows)-1)
		st := lipgloss.NewStyle().
			Foreground(theme.Blend(theme.Current.Accent, theme.Current.Accent2, t)).
			Bold(true)
		out[i] = st.Render(r[0] + r[1])
	}
	return out
}

// Tagline returns the styled brand line under the logo.
func Tagline() string {
	return theme.TextDim().Render("the agentic workspace IDE")
}

// VersionLine returns the styled version chip line.
func VersionLine(v string) string {
	return theme.Hint().Render("v" + v + "  " + theme.GlyphBullet + "  foundation")
}

// HeroBlock composes logo + tagline + version as raw left-aligned lines so
// callers can embed them in larger centered compositions.
func HeroBlock(version string) string {
	lines := append([]string{}, Logo()...)
	return strings.Join(append(lines,
		"",
		Tagline(),
		VersionLine(version),
	), "\n")
}

// Hero renders the brand hero centered inside width×height cells.
func Hero(width, height int, version string) string {
	return kit.Center(HeroBlock(version), width, height)
}
