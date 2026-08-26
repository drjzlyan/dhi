package reviewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func ansiStrip(s string) string { return golden.Strip(s) }

const samplePatch = `diff --git a/main.go b/main.go
index 3e0f8b2..a1b2c3d 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@ package main
 func main() {
-	println("hi")
+	println("hello")
+	println("world")
 }
diff --git a/util/util.go b/util/util.go
new file mode 100644
--- /dev/null
+++ b/util/util.go
@@ -0,0 +1,2 @@
+package util
+
`

type seam struct {
	root      string
	discarded []string
}

func (s *seam) work(id, member, _ string) (string, error) {
	rel := filepath.Join(review.Dir, id, member)
	if err := os.MkdirAll(filepath.Join(s.root, rel), 0o755); err != nil {
		return "", err
	}
	return rel, nil
}
func (s *seam) discard(_, relPath string) error {
	s.discarded = append(s.discarded, relPath)
	return os.RemoveAll(filepath.Join(s.root, relPath))
}

// newSurface builds a fully-wired reviewer over a temp workspace whose
// member "api" is an ordinary directory; the worktree seam is faked and
// diffs come from the injected patch text, so no git binary runs here.
func newSurface(t *testing.T) (*Model, *workspace.Workspace, *review.Store, *seam) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	// local bare origin keeps push flows hermetic; gh flows fake the rest.
	bare := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	if r, err := git.PlainInit(filepath.Join(root, "api"), false); err == nil {
		_, _ = r.CreateRemote(&gitconfig.RemoteConfig{
			Name: "origin",
			URLs: []string{bare},
		})
		// one real commit so pushes have something to carry
		if wt, err := r.Worktree(); err == nil {
			p := filepath.Join(root, "api", "main.go")
			_ = os.WriteFile(p, []byte("package main\n"), 0o644)
			_, _ = wt.Add(".")
			_, _ = wt.Commit("base", &git.CommitOptions{
				Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
			})
		}
	}
	cfg := "schema = 1\n\n[members.api]\npath = \"api\"\n"
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := review.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	sm := &seam{root: root}
	st.SetWorktreeSeam(sm.work, sm.discard)
	svc := review.NewService(ws, st, nil, nil)
	svc.SetDiffForTest(func(context.Context, string, ...string) (string, error) {
		return samplePatch, nil
	})
	m := New("0.1.0", ws, Deps{Service: svc})
	m.Resize(100, 30)
	return m, ws, st, sm
}

func pumpCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	type boxed struct {
		msg tea.Msg
	}
	ch := make(chan boxed, 1)
	go func() { ch <- boxed{cmd()} }()
	select {
	case b := <-ch:
		return b.msg
	case <-time.After(3 * time.Second):
		t.Fatal("async operation timed out")
	}
	return nil
}

func startBranchReview(t *testing.T, m *Model, st *review.Store) review.Review {
	t.Helper()
	m.HandleKey("n") // modal
	if m.form.kind != fNewReview {
		t.Fatalf("modal kind = %v", m.form.kind)
	}
	m.form.fields[0].runes = []rune("api")
	m.form.fields[1].val = 0 // branch
	m.form.fields[2].runes = []rune("master")
	m.form.fields[3].runes = []rune("master")
	m.submitForm() // busy=true, goroutine running
	msg := pumpCmd(t, m.listen())
	_ = m.Update(msg) // returned listener is dropped: tests pump manually
	// start auto-opens the review, which chains a diff load — drain it too.
	if m.busy {
		msg2 := pumpCmd(t, m.listen())
		_ = m.Update(msg2)
	}
	if m.busy || m.opErr != "" {
		t.Fatalf("start failed: busy=%v err=%q", m.busy, m.opErr)
	}
	rs := st.List()
	if len(rs) != 1 {
		t.Fatalf("cards = %d", len(rs))
	}
	return rs[0]
}

func TestMetaAndNilWorkspace(t *testing.T) {
	m, _, _, _ := newSurface(t)
	if got := m.Meta(); got.ID != "reviewer" || got.Title != "Reviewer" {
		t.Fatalf("meta = %+v", got)
	}
	var _ surfaces.Surface = m

	bare := New("0.1.0", nil, Deps{})
	bare.Resize(90, 24)
	out := bare.View()
	if !strings.Contains(out, "not inside a DHI workspace") {
		t.Fatalf("hero missing:\n%s", out)
	}
	for _, k := range []string{"n", "enter", "x", "d", "j"} {
		if bare.HandleKey(k) {
			t.Fatalf("key %q consumed without workspace", k)
		}
	}
}

func TestDegradedWithoutService(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile),
		[]byte("schema = 1\n[members.x]\npath = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New("0.1.0", ws, Deps{})
	m.Resize(100, 30)
	out := m.View()
	if !strings.Contains(ansiStrip(out), "(review service unavailable)") {
		t.Fatalf("degraded row missing:\n%s", ansiStrip(out))
	}
	if m.HandleKey("n") {
		t.Error("new-review modal opened without service")
	}
}

func TestSectionCyclingAndEsc(t *testing.T) {
	m, _, _, _ := newSurface(t)
	if m.sec != secReviews {
		t.Fatalf("initial section = %v", m.sec)
	}
	m.HandleKey("[")
	if m.sec != secDiff {
		t.Fatalf("[ should wrap to DIFF, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secReviews {
		t.Fatalf("] did not wrap back: %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secFiles {
		t.Fatalf("] to FILES failed: %v", m.sec)
	}
	m.HandleKey("esc")
	if m.sec != secReviews {
		t.Fatalf("esc should return to REVIEWS, got %v", m.sec)
	}
}

func TestNewReviewFlowEndToEnd(t *testing.T) {
	m, ws, st, sm := newSurface(t)

	// validation: missing head
	m.HandleKey("n")
	m.form.fields[0].runes = []rune("api")
	m.form.fields[2].runes = []rune("master")
	m.submitForm()
	if m.form.err == "" {
		t.Fatal("empty head accepted")
	}

	r := startBranchReview(t, m, st)
	if r.ID != "api-branch-master" {
		t.Errorf("id = %q", r.ID)
	}
	if r.WorkRel == "" || !strings.HasPrefix(r.Channel, "#") {
		t.Errorf("rel=%q channel=%q", r.WorkRel, r.Channel)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, r.WorkRel)); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}

	// auto-opened: FILES section lists both files from the canned patch
	if m.sec != secFiles || m.openID != r.ID {
		t.Fatalf("sec=%v openID=%q", m.sec, m.openID)
	}
	if len(m.files) != 2 {
		t.Fatalf("files = %d, want 2 (err=%q)", len(m.files), m.opErr)
	}
	out := ansiStrip(m.View())
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "util/util.go") {
		t.Fatalf("file list incomplete:\n%s", out)
	}

	_ = sm
}

func TestDiffNavigationBothLayouts(t *testing.T) {
	m, _, st, _ := newSurface(t)
	r := startBranchReview(t, m, st)

	// into DIFF
	m.HandleKey("]")
	if m.sec != secDiff {
		t.Fatalf("section = %v", m.sec)
	}
	if len(m.diffRows()) == 0 {
		t.Fatal("no diff rows")
	}

	// cursor + file jumps
	first := m.cursor
	m.jumpFileDelta(1)
	if m.fileCur != 1 && m.cursor <= first {
		t.Errorf("jump to second file failed: cur=%d first=%d", m.fileCur, first)
	}
	m.jumpFileDelta(-1)
	if m.fileCur != 0 {
		t.Errorf("jump back failed: %d", m.fileCur)
	}

	// viewed marks flow through the store
	path := m.pathAtRow(m.cursor)
	if path == "" {
		t.Fatal("no path at row 0")
	}
	m.HandleKey("v")
	got, _ := st.Get(r.ID)
	if !got.Viewed[path] {
		t.Error("viewed mark not recorded")
	}
	m.HandleKey("v")
	got, _ = st.Get(r.ID)
	if got.Viewed[path] {
		t.Error("viewed mark not cleared")
	}

	// layout toggle swaps row model
	unifiedRows := len(m.diffRows())
	m.HandleKey("\\")
	sideRows := len(m.diffRows())
	if m.layout != layoutSideBySide || sideRows >= unifiedRows {
		t.Errorf("layout toggle wrong: unified=%d side=%d layout=%v",
			unifiedRows, sideRows, m.layout)
	}

	outU := ansiStrip(renderAt(m, layoutUnified))
	outS := ansiStrip(renderAt(m, layoutSideBySide))
	for _, want := range []string{"main.go", "@@ -1,4 +1,5 @@", "println(\"hello\")", "println(\"hi\")"} {
		if !strings.Contains(outU, want) {
			t.Errorf("unified view missing %q", want)
		}
	}
	if !strings.Contains(outS, "println(\"world\")") ||
		!strings.Contains(outS, "println(\"hi\")") {
		t.Errorf("side-by-side content wrong:\n%s", outS)
	}
}

func TestDiscardAndRemoveFlows(t *testing.T) {
	m, _, st, sm := newSurface(t)
	r := startBranchReview(t, m, st)

	m.HandleKey("esc") // back to REVIEWS
	if m.sec != secReviews {
		t.Fatalf("section = %v", m.sec)
	}
	// confirm modal swallows unrelated keys until decided
	m.HandleKey("x")
	if m.form.kind != fDiscardConfirm {
		t.Fatalf("kind = %v", m.form.kind)
	}
	for _, k := range []string{"a", "b", "j"} {
		if !m.HandleKey(k) {
			t.Fatalf("confirm modal dropped %q", k)
		}
	}
	m.HandleKey("esc")
	if m.form.kind != fNone {
		t.Fatal("esc did not close confirm")
	}

	m.HandleKey("x")
	m.HandleKey("enter")
	msg := pumpCmd(t, m.listen())
	_ = m.Update(msg)
	got, ok := st.Get(r.ID)
	if !ok || !got.Done {
		t.Fatalf("card after discard: %+v found=%v", got, ok)
	}
	if len(sm.discarded) != 1 || m.openID != "" {
		t.Fatalf("discarded=%v openID=%q", sm.discarded, m.openID)
	}

	// remove deletes the card outright
	m.cursors[secReviews] = 0
	m.HandleKey("d")
	m.HandleKey("enter")
	if _, ok := st.Get(r.ID); ok {
		t.Error("card survived removal")
	}
}

func TestModalOverlayRendersFields(t *testing.T) {
	m, _, _, _ := newSurface(t)
	m.HandleKey("n")
	out := ansiStrip(m.View())
	for _, want := range []string{"member ", "kind   ", "< branch >", "base   ", "head/# "} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q:\n%s", want, out)
		}
	}
	m.HandleKey("tab")
	m.HandleKey("right")
	if got := m.form.fields[1].toggleValue(); got != "worktree" {
		t.Errorf("toggle = %q", got)
	}
}

// renderAt forces a layout and renders the docked pane deterministically.
func renderAt(m *Model, l layoutMode) string {
	m.layout = l
	m.cursor, m.scroll = 0, 0
	r, _ := m.openReview()
	return m.renderDiff(80, 24, r.Viewed)
}
