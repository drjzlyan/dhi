package bootgate

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/boot"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func blockedDecision() boot.Decision {
	return boot.Decision{
		Block: `sandbox: bwrap not found (install it, or set security.sandbox = "off" in settings to opt out explicitly)`,
		Fixes: []string{
			"install the OS sandbox helper (sandbox-exec on macOS is built in; bwrap on Linux)",
			`or set security.sandbox = "off" in settings to opt out explicitly`,
		},
	}
}

func TestBlockedNeverReleases(t *testing.T) {
	m := New("test", blockedDecision())
	if m.Finished() {
		t.Fatal("blocked boot must never release")
	}
	m.HandleKey("i")
	m.HandleKey("enter")
	m.HandleKey("esc")
	if m.Finished() {
		t.Fatal("keys must not release a blocked boot")
	}
	if cmd := m.Update(nil); cmd != nil {
		t.Fatal("blocked boot runs no commands")
	}
}

func TestCleanDecisionReleasesImmediately(t *testing.T) {
	m := New("test", boot.Decision{})
	if !m.Finished() {
		t.Fatal("clean decision must release at once (no gate content)")
	}
}

func TestBlockScreenGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", blockedDecision())
	m.Resize(70, 30)
	golden.Snapshot(t, "bootgate_blocked_70x30", m.View())
}

func TestBlockViewContent(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", blockedDecision())
	m.Resize(70, 30)
	v := m.View()
	for _, want := range []string{"boot blocked", "bwrap", "security.sandbox", "ctrl+q", "ADR-0011"} {
		if !strings.Contains(plain(v), want) {
			t.Errorf("block view missing %q:\n%s", want, plain(v))
		}
	}
}

func plain(s string) string { return s }

func TestWrapLineHasNoEmptyLines(t *testing.T) {
	for _, in := range []string{
		"one two three four five six seven eight nine ten",
		"averyveryverylongwordthatexceedsthewidthcompletely and more",
		"short",
	} {
		for _, l := range wrapLine(in, 12) {
			if l == "" {
				t.Fatalf("wrapLine(%q) emitted an empty line: %q", in, wrapLine(in, 12))
			}
			if len(l) > 12 {
				t.Fatalf("line %q exceeds width", l)
			}
		}
	}
}
