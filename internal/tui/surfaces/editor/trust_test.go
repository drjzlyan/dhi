package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/dap"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/trust"
)

// trustedEditor opens alpha/app.go with a real trust store in a temp file
// (nothing trusted yet) and echo standing in for every tool.
func trustedEditor(t *testing.T) (*Model, *trust.Store, string) {
	t.Helper()
	ws, root := setupWorkspace(t)
	alpha := filepath.Join(root, "repos", "alpha")
	if err := os.WriteFile(filepath.Join(alpha, "Makefile"), []byte("deploy:\n\techo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := trust.New(filepath.Join(t.TempDir(), "trust.json"))
	mgr := lsp.NewManager(func(string) (string, bool) { return "/bin/echo", true }, nil)
	m := New("test", ws, WithTrust(st), WithHostTools(echoTools{}), WithTermEnv(os.Environ()), WithLSP(mgr))
	m.Resize(100, 30)
	m.open(&node{kind: nodeFile, path: filepath.Join(alpha, "app.go"), name: "app.go"})
	return m, st, alpha
}

func TestRestrictedBlocksTasksAndNamesTheWayOut(t *testing.T) {
	m, st, alpha := trustedEditor(t)
	m.trustCard = nil // opening the file already asked about the language server
	msg, _ := m.taskCommand("task deploy")
	if !strings.Contains(msg, "restricted: trust this folder to run make tasks") || len(m.terms) != 0 {
		t.Fatalf("task ran or stayed silent: %q terms=%d", msg, len(m.terms))
	}
	if err := st.Trust(alpha); err != nil {
		t.Fatal(err)
	}
	if msg, _ := m.taskCommand("task deploy"); msg != "" || len(m.terms) != 1 {
		t.Fatalf("trusted task: %q terms=%d", msg, len(m.terms))
	}
}

func TestRestrictedBlocksTestsAndDebug(t *testing.T) {
	m, _, _ := trustedEditor(t)
	started := false
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
		started = true
		return nil, os.ErrInvalid
	}
	if msg, _ := m.testCommand("test"); !strings.Contains(msg, "restricted: trust this folder to run tests") || m.testing != nil {
		t.Errorf("test: %q testing=%v", msg, m.testing)
	}
	if msg := m.startDebug(); !strings.Contains(msg, "restricted: trust this folder to debug") || started || m.dbg != nil {
		t.Errorf("debug: %q started=%v", msg, started)
	}
}

func TestRestrictedBlocksFormat(t *testing.T) {
	m, _, _ := trustedEditor(t)
	if msg := m.formatEditor(m.active()); !strings.Contains(msg, "restricted: trust this folder to format") {
		t.Errorf("format = %q", msg)
	}
}

func TestRestrictedHoldsTheLanguageServerAndAsksOnce(t *testing.T) {
	m, st, _ := trustedEditor(t)
	alpha := m.trustCard.dir
	if m.trustCard == nil || !strings.Contains(m.trustCard.what, "start ") {
		t.Fatalf("no trust card for the server: %+v", m.trustCard)
	}
	if len(m.lspStarting) != 0 {
		t.Fatalf("server started while restricted: %v", m.lspStarting)
	}
	if v := plainView(m); !strings.Contains(v, "trust this folder?") {
		t.Errorf("card not shown:\n%s", v)
	}
	feed(m, "n") // not now: restricted for the session, no second card
	if m.trustCard != nil || st.Trusted(alpha) {
		t.Fatalf("n trusted or kept the card: %v", m.trustCard)
	}
	m.lspOpenDoc(m.active().Path(), "package main\n")
	if m.trustCard != nil {
		t.Error("asked a second time after not now")
	}
	if !strings.Contains(m.active().Message(), "restricted: trust this folder to start") {
		t.Errorf("silent refusal: %q", m.active().Message())
	}
}

func TestTrustCardYesRecordsAndStartsTheServer(t *testing.T) {
	m, st, alpha := trustedEditor(t)
	feed(m, "y")
	if !st.Trusted(alpha) || m.trustCard != nil {
		t.Fatalf("y did not trust: card=%v", m.trustCard)
	}
	if !m.lspStarting["go"] {
		t.Errorf("held-back server not started: %v", m.lspStarting)
	}
}

func TestTrustUntrustCommands(t *testing.T) {
	m, st, alpha := trustedEditor(t)
	m.trustCard = nil
	if msg, ok := m.trustCommand("trust"); !ok || !strings.HasPrefix(msg, "trusted ") || !st.Trusted(alpha) {
		t.Fatalf("trust: %q ok=%v", msg, ok)
	}
	if msg, _ := m.trustCommand("untrust"); !strings.Contains(msg, "is restricted") || st.Trusted(alpha) {
		t.Fatalf("untrust: %q", msg)
	}
}

func TestTrustPaletteActionFlips(t *testing.T) {
	m, st, alpha := trustedEditor(t)
	m.trustCard = nil
	has := func(title string) bool {
		for _, c := range m.Commands() {
			if c.Title == title {
				return true
			}
		}
		return false
	}
	if !has("Trust this folder…") {
		t.Fatal("no trust action while restricted")
	}
	if err := st.Trust(alpha); err != nil {
		t.Fatal(err)
	}
	if !has("Stop trusting this folder (restricted mode)") {
		t.Fatal("no untrust action while trusted")
	}
}

func TestNoTrustStoreMeansUnrestricted(t *testing.T) {
	m, _ := taskEditor(t, &echoTools{})
	if m.restricted(m.installDir(), "anything") || m.trustCard != nil {
		t.Fatal("restricted without a trust store")
	}
}
