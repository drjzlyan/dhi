package bootgate

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
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
	// afterInstall at this point: no gopls, no go shim → a named refusal
	// that is SHOWN, and no build scheduled
	if cmd := m.afterInstall(); cmd != nil {
		t.Error("missing go shim must not schedule a build")
	}
	if m.phase != phaseBuildFailed || len(m.buildErrs) != 2 ||
		!strings.Contains(strings.Join(m.buildErrs, "|"), "gopls: needs the go toolchain") ||
		!strings.Contains(strings.Join(m.buildErrs, "|"), "dlv: needs the go toolchain") {
		t.Fatalf("phase=%v errs=%v", m.phase, m.buildErrs)
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

func firstRunDecision() boot.Decision {
	return boot.Decision{
		FirstRun:   true,
		Offer:      []string{"git", "go", "gh", "node", "rg", "uv"},
		OfferBytes: map[string]int64{"git": 2016187, "go": 68303667, "gh": 14212224, "node": 52234372, "rg": 1764284, "uv": 18518284},
		OfferTotal: 157048998,
	}
}

func TestFirstRunScreenGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", firstRunDecision(), nil)
	m.Resize(80, 30)
	golden.Snapshot(t, "bootgate_firstrun_80x30", m.View())
}

func TestFirstRunShowsSizesTotalAndWhere(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", firstRunDecision(), nil)
	m.Resize(80, 30)
	v := plain(m.View())
	for _, want := range []string{"Welcome to DHI", "68.3 MB", "1.8 MB", "total download", "157.0 MB",
		"~/.local/share/dhi/toolchain", "no sudo", "enter install"} {
		if !strings.Contains(v, want) {
			t.Errorf("first-run screen lacks %q:\n%s", want, v)
		}
	}
}

// First run asks once, and the primary key installs; the older
// missing-pieces offer keeps "enter = skip".
func TestFirstRunEnterInstallsEscSkips(t *testing.T) {
	m := New("test", firstRunDecision(), toolchain.New(t.TempDir()))
	m.Resize(80, 24)
	m.HandleKey("enter")
	if m.phase != phaseInstalling || m.TakeCmd() == nil {
		t.Fatalf("enter must start the install on first run (phase=%v)", m.phase)
	}

	skip := New("test", firstRunDecision(), toolchain.New(t.TempDir()))
	skip.HandleKey("esc")
	if skip.phase != phaseDone || skip.inner != nil {
		t.Fatal("esc must skip without installing")
	}

	old := New("test", boot.Decision{Offer: []string{"rg"}}, toolchain.New(t.TempDir()))
	old.HandleKey("enter")
	if old.phase != phaseDone || old.inner != nil {
		t.Fatal("non-first-run offers keep enter = skip")
	}
}

func TestConfirmHintWrapsInsteadOfClipping(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", firstRunDecision(), nil)
	m.Resize(60, 30)
	// Drop the panel borders, collapse the wrap, and the whole hint must be there.
	flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(ansi.Strip(m.View()))), " ")
	if !strings.Contains(flat, "enter install · esc skip (capabilities refuse until installed)") {
		t.Fatalf("hint clipped:\n%s", plain(m.View()))
	}
}

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

func TestFirstRunShowsTheRealInstallRoot(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	d := firstRunDecision()
	d.OfferRoot = "/srv/data/dhi/toolchain"
	m := New("test", d, nil)
	m.Resize(100, 30)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "/srv/data/dhi/toolchain") || strings.Contains(v, "~/.local/share") {
		t.Fatalf("install root not shown:\n%s", v)
	}
	if got := tildePath(filepath.Join(os.Getenv("HOME"), ".local", "share", "dhi")); got != "~/.local/share/dhi" {
		t.Fatalf("tildePath = %q", got)
	}
}

// ---- source-built tools (gopls, dlv) ----

// drive runs the queued build commands to completion like the shell would.
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 10 {
			t.Fatal("build queue did not settle")
		}
		cmd = m.Update(cmd())
	}
}

func readyForBuilds(t *testing.T, build func(context.Context, toolchain.BuildSpec) error) *Model {
	t.Helper()
	root := t.TempDir()
	mgr := toolchain.New(root)
	if err := os.MkdirAll(mgr.ShimDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mgr.ShimDir(), "go"), []byte("#!/bin/sh\n"), 0o755)
	m := New("test", boot.Decision{}, mgr)
	m.build = build
	return m
}

func TestBothSourceBuiltToolsAreBuiltInOrder(t *testing.T) {
	var built []string
	m := readyForBuilds(t, func(_ context.Context, s toolchain.BuildSpec) error {
		built = append(built, s.Name)
		return nil
	})
	cmd := m.afterInstall()
	if m.phase != phaseBuilding || !strings.Contains(plain(m.View()), "building gopls from source (1/2)") {
		t.Fatalf("phase=%v view=%q", m.phase, m.View())
	}
	drive(t, m, cmd)
	if strings.Join(built, ",") != "gopls,dlv" || m.phase != phaseDone {
		t.Fatalf("built=%v phase=%v", built, m.phase)
	}
}

func TestAnAlreadyBuiltToolIsNotRebuilt(t *testing.T) {
	var built []string
	m := readyForBuilds(t, func(_ context.Context, s toolchain.BuildSpec) error {
		built = append(built, s.Name)
		return nil
	})
	os.WriteFile(filepath.Join(m.mgr.ShimDir(), "gopls"), []byte("#!/bin/sh\n"), 0o755)
	drive(t, m, m.afterInstall())
	if strings.Join(built, ",") != "dlv" {
		t.Fatalf("built = %v, want only dlv", built)
	}
}

// A failed build used to release the shell with the error text thrown away.
func TestAFailedBuildIsShownAndWaitsForAKey(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := readyForBuilds(t, func(_ context.Context, s toolchain.BuildSpec) error {
		if s.Name == "gopls" {
			return errors.New("proxy: 503")
		}
		return nil
	})
	m.Resize(90, 30)
	drive(t, m, m.afterInstall())
	if m.Finished() || m.phase != phaseBuildFailed {
		t.Fatalf("phase=%v finished=%v; a failure must not silently release the shell", m.phase, m.Finished())
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "gopls: proxy: 503") || strings.Contains(v, "dlv:") {
		t.Fatalf("failure view:\n%s", v)
	}
	m.HandleKey("enter")
	if !m.Finished() {
		t.Fatal("enter should continue")
	}
}

func TestOfferNamesEverySourceBuiltTool(t *testing.T) {
	for _, spec := range toolchain.SourceBuilt() {
		if spec.Name == "" || spec.Version == "" || spec.Module == "" {
			t.Errorf("incomplete spec %+v", spec)
		}
	}
	if names := func() string {
		var n []string
		for _, s := range toolchain.SourceBuilt() {
			n = append(n, s.Name)
		}
		return strings.Join(n, ",")
	}(); names != "gopls,dlv" {
		t.Fatalf("source-built = %s", names)
	}
}
