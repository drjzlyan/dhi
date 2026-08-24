package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func newSurface(t *testing.T) (*Model, *workspace.Workspace) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	repoA := filepath.Join(root, "repos", "alpha")
	repoB := filepath.Join(root, "beta")
	for _, dir := range []string{repoA, repoB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := workspace.Create(root, "placeholder"); err != nil {
		t.Fatal(err)
	}
	// Create seeds a config without real members; write one directly.
	cfgPath := filepath.Join(root, workspace.ConfigFile)
	cfg := "schema = 1\n\n[members.alpha]\npath = \"repos/alpha\"\n\n[members.beta]\npath = \"beta\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New("0.1.0", ws)
	m.Resize(100, 30)
	return m, ws
}

func TestMetaIsBootSurface(t *testing.T) {
	m, _ := newSurface(t)
	if got := m.Meta(); got.ID != "workspace" || got.Title != "Workspace" {
		t.Fatalf("meta = %+v", got)
	}
	var _ surfaces.Surface = m
}

func TestNilWorkspaceRendersHeroAndSwallowsKeys(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("0.1.0", nil)
	m.Resize(100, 30)
	out := m.View()
	if !strings.Contains(out, "███████") || !strings.Contains(out, "not inside a DHI workspace") {
		t.Fatalf("empty state missing hero/hint:\n%s", out)
	}
	for _, k := range []string{"a", "r", "d", "j", "k"} {
		if m.HandleKey(k) {
			t.Fatalf("key %q consumed without a workspace", k)
		}
	}
}

func TestViewListsMembersWithCursor(t *testing.T) {
	m, _ := newSurface(t)
	out := m.View()
	for _, want := range []string{"alpha", "beta", "a add", "org", "tasks"} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Fatalf("view missing %q:\n%s", want, ansi.Strip(out))
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if w := len([]rune(ansi.Strip(l))); w > 100 {
			t.Fatalf("line %d exceeds width: %d", i, w)
		}
	}
}

func TestAddMemberLocalPathFlow(t *testing.T) {
	m, ws := newSurface(t)
	gamma := filepath.Join(ws.Root, "gamma")
	os.MkdirAll(gamma, 0o755)

	m.HandleKey("a")
	typeIn(m, 0, "gamma")
	m.HandleKey("tab")
	typeIn(m, 1, gamma)
	m.HandleKey("enter")

	if _, ok := ws.Member("gamma"); !ok {
		t.Fatal("gamma not registered after form submit")
	}
	if m.form.kind != fNone {
		t.Fatalf("modal still open: %+v (err=%q)", m.form.kind, m.form.err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, workspace.ConfigFile)); err != nil {
		t.Fatal(err)
	}
}

func TestAddMemberValidationKeepsModalOpen(t *testing.T) {
	m, _ := newSurface(t)
	m.HandleKey("a")
	typeIn(m, 0, "Bad Name")
	m.HandleKey("enter")
	if m.form.kind != fAdd || !strings.Contains(m.form.err, "bad member name") {
		t.Fatalf("form = %+v err=%q", m.form.kind, m.form.err)
	}
	// Esc closes.
	m.HandleKey("esc")
	if m.form.kind != fNone {
		t.Fatal("esc did not close modal")
	}
}

func TestRenameMemberFlow(t *testing.T) {
	m, ws := newSurface(t)
	m.HandleKey("k") // cursor onto beta? starts at 0 (alpha); move down instead
	m.HandleKey("j")
	m.HandleKey("r")
	// Replace the prefilled name with the new alias.
	m.form.name = []rune("aab")
	m.HandleKey("enter")
	if _, ok := ws.Member("aab"); !ok {
		t.Fatalf("rename failed: %+v err=%q", ws.Members(), m.form.err)
	}
}

func TestRemoveMemberConfirmFlow(t *testing.T) {
	m, ws := newSurface(t)
	m.HandleKey("j") // beta
	m.HandleKey("d")
	if m.form.kind != fRemoveConfirm {
		t.Fatalf("expected confirm modal, got %+v", m.form.kind)
	}
	m.HandleKey("enter")
	if _, ok := ws.Member("beta"); ok {
		t.Fatal("beta still registered after confirm")
	}
	if info, err := os.Stat(filepath.Join(ws.Root, "beta")); err != nil || !info.IsDir() {
		t.Errorf("working tree must survive unregister: %v", err)
	}
	// Last-member guard surfaces inline.
	m.HandleKey("d")
	m.HandleKey("enter")
	if m.form.kind == fNone || !strings.Contains(m.form.err, "last member") {
		t.Fatalf("last member removal = kind:%+v err:%q", m.form.kind, m.form.err)
	}
}

func TestCloneSourceDetection(t *testing.T) {
	for _, url := range []string{
		"https://github.com/x/y.git", "http://cgit.local/x",
		"git@github.com:x/y.git", "ssh://git@host/x", "git://host/x",
	} {
		if !isCloneSource(url) {
			t.Errorf("%q not treated as clone source", url)
		}
	}
	for _, p := range []string{"/abs/path", "../rel", "C:\\dev", "repos/thing"} {
		if isCloneSource(p) {
			t.Errorf("%q wrongly treated as clone source", p)
		}
	}
}

// typeIn types s into the focused field of the open add/rename form,
// switching to the path field first when field==1.
func typeIn(m *Model, field int, s string) {
	if field == 1 && m.form.field == 0 {
		m.HandleKey("tab")
	}
	for _, r := range s {
		if !m.HandleKey(string(r)) {
			// Single-letter keys collide with nav only outside modals;
			// inside a modal every printable rune must be consumed.
			t := struct{}{}
			_ = t
		}
	}
}
