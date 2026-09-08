package bootgate

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/boot"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/toolchain"
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
	m := New("test", blockedDecision(), nil)
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
	m := New("test", boot.Decision{}, nil)
	if !m.Finished() {
		t.Fatal("clean decision must release at once (no gate content)")
	}
}

func TestConfirmSkipReleasesDeclined(t *testing.T) {
	m := New("test", boot.Decision{Offer: []string{"rg", "gopls (built from source)"}}, nil)
	if m.phase != phaseConfirm {
		t.Fatalf("phase = %v", m.phase)
	}
	m.HandleKey("enter") // decline: capabilities refuse at use
	if !m.Finished() {
		t.Fatal("skip must release the shell")
	}
	m2 := New("test", boot.Decision{Offer: []string{"rg"}}, nil)
	m2.HandleKey("esc")
	if !m2.Finished() {
		t.Fatal("esc must also skip")
	}
	if m2.inner != nil {
		t.Fatal("skip must not start an install")
	}
}

func TestConfirmInstallDelegatesToBootstrap(t *testing.T) {
	m := New("test", boot.Decision{Offer: []string{"rg"}}, toolchain.New(t.TempDir()))
	m.Resize(80, 24)
	m.HandleKey("i")
	if m.phase != phaseInstalling || m.inner == nil {
		t.Fatalf("install phase not entered: %v", m.phase)
	}
	// afterInstall at this point: no gopls, no go shim → finishes with
	// a named refusal, no build scheduled
	if cmd := m.afterInstall(); cmd != nil {
		t.Error("missing go shim must not schedule a gopls build")
	}
	if m.phase != phaseDone || !strings.Contains(m.buildErr, "go toolchain") {
		t.Fatalf("phase=%v buildErr=%q", m.phase, m.buildErr)
	}
}

func TestBlockScreenGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", blockedDecision(), nil)
	m.Resize(70, 30)
	golden.Snapshot(t, "bootgate_blocked_70x30", m.View())
}

func TestConfirmScreenGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", boot.Decision{
		Offer:    []string{"rg", "uv", "node", "git", "gopls (built from source)"},
		Warnings: []string{"os sandbox disabled by config (explicit opt-out; path-jail + policy still apply)"},
	}, nil)
	m.Resize(70, 30)
	golden.Snapshot(t, "bootgate_confirm_70x30", m.View())
}

func TestBlockViewContent(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", blockedDecision(), nil)
	m.Resize(70, 30)
	v := m.View()
	for _, want := range []string{"boot blocked", "bwrap", "security.sandbox", "ctrl+q", "ADR-0011"} {
		if !strings.Contains(plain(v), want) {
			t.Errorf("block view missing %q:\n%s", want, plain(v))
		}
	}
}

func plain(s string) string { return s }

// TestConfirmInstallQueuesInitCommand pins the F-011 gate-start bug:
// the delegated bootstrap's Init must be drained by the shell right
// after the confirm key, or the install goroutine never starts.
func TestConfirmInstallQueuesInitCommand(t *testing.T) {
	m := New("test", boot.Decision{Offer: []string{"go"}}, toolchain.New(t.TempDir()))
	m.Resize(80, 24)
	if m.TakeCmd() != nil {
		t.Fatal("no command before confirm")
	}
	m.HandleKey("i")
	cmd := m.TakeCmd()
	if cmd == nil {
		t.Fatal("confirm key must queue the inner bootstrap Init")
	}
	if m.TakeCmd() != nil {
		t.Fatal("drain must be exactly once")
	}
	// skip/block/clean queue nothing
	for _, d := range []boot.Decision{
		{Offer: []string{"go"}},
		{Block: "x"},
		{},
	} {
		m2 := New("test", d, nil)
		m2.HandleKey("enter")
		if m2.TakeCmd() != nil {
			t.Errorf("decision %+v queued a command", d)
		}
	}
}
