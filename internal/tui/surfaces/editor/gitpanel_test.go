package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// makeAlphaRepo turns the alpha member into a real repo with one commit.
func makeAlphaRepo(t *testing.T, m *Model) {
	t.Helper()
	alpha := m.members[0].path
	r, err := git.PlainInit(alpha, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("app.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGitPanelStageAndCommit(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	// F-029: the panel commits as the user's resolved git identity.
	m.gitIdentity = func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	}

	feed(m, "enter", "down", "down", "enter") // open app.go in buffer
	if !strings.Contains(plainView(m), "NORMAL") {
		t.Fatal("buffer not open")
	}

	// modify + save so worktree differs from HEAD
	feed(m, "$")
	feed(m, "i")
	typeKeys(m, "// touched")
	feed(m, "esc")
	typeKeys(m, ":w")
	feed(m, "enter")

	feed(m, "ctrl+j") // open git panel
	if !m.gitOpen || !m.gitFocus {
		t.Fatal("panel did not open focused")
	}
	v := plainView(m)
	if !strings.Contains(v, "UNSTAGED") || !strings.Contains(v, "app.go") {
		t.Fatalf("status missing modified file:\n%s", v)
	}

	feed(m, "s") // stage cursor file
	if !strings.Contains(plainView(m), "STAGED") || !strings.Contains(plainView(m), "app.go") {
		t.Fatalf("staged section missing after s:\n%s", plainView(m))
	}
	if st := m.gitEntries; len(stagedPaths(st)) != 1 {
		t.Fatalf("index not updated: %+v", st)
	}

	feed(m, "c")
	if m.bufFocus && m.active() != nil && m.active().Mode() != textbuf.ModeNormal {
		t.Log("buffer state irrelevant here")
	}
	typeKeys(m, "panel commit")
	feed(m, "enter")

	log, err := m.gitRepo.Log(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Message != "panel commit" {
		t.Fatalf("log = %+v", log)
	}
	if log[0].Author != "Ada" {
		t.Fatalf("commit author = %q, want the resolved identity", log[0].Author)
	}
	for _, f := range m.gitEntries {
		if f.Staged {
			t.Errorf("entry still staged after commit: %+v", f)
		}
	}
}

func TestGitPanelCommitRefusesWithoutIdentity(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	m.loadGit()
	if m.gitRepo == nil {
		t.Fatal("no git repo loaded")
	}
	// No identity resolver installed: the commit path must refuse by
	// name, never invent an author (F-029).
	m.gitInput = []rune("x")
	m.gitInputMode = true
	m.handleCommitInput("enter")
	if !strings.Contains(m.gitErr, "user.name") {
		t.Fatalf("gitErr = %q, want an identity refusal", m.gitErr)
	}
}

func stagedPaths(entries []gitcore.FileStatus) []string {
	var out []string
	for _, e := range entries {
		if e.Staged {
			out = append(out, e.Path)
		}
	}
	return out
}

func TestGitPanelLogTab(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)

	feed(m, "ctrl+j")
	feed(m, "tab")
	v := plainView(m)
	if !strings.Contains(v, "initial") || m.gitTab != 1 {
		t.Fatalf("log view wrong:\n%s", v)
	}
	feed(m, "tab") // back to status
	if m.gitTab != 0 || !strings.Contains(plainView(m), "STAGED") {
		t.Error("tab did not return to status")
	}
}

func TestGitPanelNonRepoMessage(t *testing.T) {
	// beta member is not a repo; force selection there by opening its file
	m := newEditor(t)
	feed(m, "/", "R", "E", "A", "D", "M", "E")
	feed(m, "enter")
	feed(m, "ctrl+j")
	if !strings.Contains(plainView(m), "no git repository") {
		t.Fatalf("non-repo message missing:\n%s", plainView(m))
	}
}

func TestGitPanelEscBlurs(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	feed(m, "ctrl+j")
	feed(m, "esc")
	if m.gitFocus {
		t.Fatal("esc did not blur panel")
	}
	if !m.HandleKey("/") {
		t.Error("tree keys dead while panel merely blurred")
	}
	feed(m, "esc")
	feed(m, "ctrl+t") // unrelated drawer unaffected
	if !m.drawerOpen {
		t.Error("drawer broken by git panel usage")
	}
}

func TestCommitInputCancel(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	feed(m, "ctrl+j")
	feed(m, "c")
	typeKeys(m, "partial")
	feed(m, "esc")
	if m.gitInputMode {
		t.Fatal("esc did not cancel commit input")
	}
	if log, _ := m.gitRepo.Log(5); len(log) != 1 {
		t.Error("cancelled input committed anyway")
	}
	_ = os.Remove(filepath.Join(t.TempDir(), "unused"))
}

func TestGitPanelGolden(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)

	feed(m, "enter", "down", "down", "enter")
	feed(m, "$")
	feed(m, "i")
	typeKeys(m, "// touched")
	feed(m, "esc")
	typeKeys(m, ":w")
	feed(m, "enter")

	feed(m, "ctrl+j")
	awaitTreeGit(t, m)
	golden.Snapshot(t, "editor_git_status_100x30", m.View())

	feed(m, "tab")
	golden.Snapshot(t, "editor_git_log_100x30", m.View())
}

// go-git status hashes the worktree (seconds on a big repo): the tree reads
// it off the UI goroutine, keeps rendering meanwhile, and drops a read that
// a save overtook.
func TestTreeGitStatusLoadsOffTheUIGoroutine(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	m.invalidateTreeGit()
	m.refreshRows()
	alpha := m.members[0].name
	if g := m.treeGits[alpha]; g == nil || !g.loading {
		t.Fatalf("refreshRows should start a background read: %+v", g)
	}
	awaitTreeGit(t, m)
	if g := m.treeGits[alpha]; g.changed["src/main.go"] != '?' || g.branch == "" {
		t.Fatalf("status after the read: %+v", g)
	}

	m.invalidateTreeGit()
	m.refreshRows()       // read #1 starts
	m.invalidateTreeGit() // a save overtakes it
	select {
	case msg := <-m.termMsgs:
		m.Update(msg)
	case <-time.After(10 * time.Second):
		t.Fatal("read #1 never finished")
	}
	if g := m.treeGits[alpha]; !g.stale {
		t.Fatal("an overtaken read must not land as current")
	}
}

// The panel's status, log and conflict list are read off the UI goroutine:
// opening it returns at once and says it is reading; a read that a newer
// refresh overtook does not land.
func TestGitPanelReadsOffTheUIGoroutine(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	m.HandleKey("ctrl+j") // not feed: feed waits for the read
	if !m.gitLoading || m.gitEntries != nil {
		t.Fatalf("opening the panel should start a read: loading=%v entries=%v", m.gitLoading, m.gitEntries)
	}
	if v := plainView(m); !strings.Contains(v, "reading git status…") {
		t.Fatalf("panel while reading:\n%s", v)
	}
	settleGitPanel(m)
	if len(m.gitEntries) == 0 || len(m.gitLog) != 1 {
		t.Fatalf("after the read: entries=%v log=%v", m.gitEntries, m.gitLog)
	}

	m.refreshGitData() // read #1
	first := m.gitDataGen
	m.refreshGitData() // read #2 overtakes it
	for got := 0; got < 2; {
		select {
		case msg := <-m.termMsgs:
			if msg.kind != gitDataMsgDone {
				continue
			}
			got++
			if msg.gitData.gen == first {
				before := m.gitEntries
				m.Update(msg)
				if &m.gitEntries[0] != &before[0] {
					t.Fatal("an overtaken read landed")
				}
				continue
			}
			m.Update(msg)
		case <-time.After(10 * time.Second):
			t.Fatal("reads never finished")
		}
	}
	if m.gitLoading {
		t.Fatal("the newest read should have landed")
	}
}

// go-git reads the whole worktree before committing: the commit runs off
// the UI goroutine, a second one waits its turn, and the outcome refreshes
// the panel.
func TestGitPanelCommitsOffTheUIGoroutine(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	m.gitIdentity = func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	}
	alpha := m.members[0].path
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte("changed\n"), 0o644)
	m.loadGit()
	settleGitPanel(m)
	if err := m.gitRepo.Stage("app.go"); err != nil {
		t.Fatal(err)
	}
	m.gitInput, m.gitInputMode = []rune("tidy"), true
	m.handleCommitInput("enter")
	if !m.committing || m.gitMessage != "committing…" {
		t.Fatalf("enter should start the commit: committing=%v msg=%q", m.committing, m.gitMessage)
	}
	m.gitInput, m.gitInputMode = []rune("again"), true
	m.handleCommitInput("enter")
	if !strings.Contains(m.gitMessage, "already committing") {
		t.Fatalf("a second commit while one runs: %q", m.gitMessage)
	}
	settleGitPanel(m)
	if !strings.HasPrefix(m.gitMessage, "committed ") || m.gitErr != "" {
		t.Fatalf("after the commit: msg=%q err=%q", m.gitMessage, m.gitErr)
	}
	if len(m.gitLog) != 2 || m.gitLog[0].Message != "tidy" {
		t.Fatalf("log = %+v", m.gitLog)
	}
}

// go-git stages through a full status read (100ms+ here, seconds on a big
// repo): s returns at once, a second index write waits its turn, and the
// panel re-reads when the write lands.
func TestGitPanelStagesOffTheUIGoroutine(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	m.gitIdentity = func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	}
	_ = os.WriteFile(filepath.Join(m.members[0].path, "app.go"), []byte("changed\n"), 0o644)
	feed(m, "ctrl+j") // feed waits for the panel's first read
	m.HandleKey("s")
	if !m.gitWriting {
		t.Fatal("s should stage off the UI goroutine")
	}
	m.HandleKey("S")
	if !strings.Contains(m.gitMessage, "still busy") {
		t.Fatalf("a second write while one runs: %q", m.gitMessage)
	}
	m.gitInput, m.gitInputMode = []rune("x"), true
	m.handleCommitInput("enter")
	if m.committing || !strings.Contains(m.gitMessage, "still staging") {
		t.Fatalf("a commit beside a stage: committing=%v msg=%q", m.committing, m.gitMessage)
	}
	settleGitPanel(m)
	if got := stagedPaths(m.gitEntries); len(got) != 1 || got[0] != "app.go" {
		t.Fatalf("staged = %v", got)
	}
}
