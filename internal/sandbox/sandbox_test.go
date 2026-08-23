package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJailContains(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	j, err := NewJail(root)
	if err != nil {
		t.Fatal(err)
	}

	if !j.Contains(root) {
		t.Error("root itself not contained")
	}
	inner := filepath.Join(root, "a", "b", "c.txt")
	if !j.Contains(inner) {
		t.Error("nested path not contained")
	}
	if j.Contains(outside) {
		t.Error("outside path contained")
	}
	sibling := root + "sibling"
	if j.Contains(filepath.Join(sibling, "x")) {
		t.Error("prefix-collision sibling contained")
	}
	if j.Contains("relative/path") {
		t.Error("relative path contained")
	}
	if got := j.Roots(); len(got) != 1 || !strings.HasSuffix(got[0], filepath.Base(root)) {
		t.Errorf("Roots = %v", got)
	}
}

func TestJailSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	j, err := NewJail(root)
	if err != nil {
		t.Fatal(err)
	}
	if j.Contains(filepath.Join(link, "secret.txt")) {
		t.Fatal("symlink escape allowed")
	}
}

func TestJailMissingPathResolvesExistingAncestor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "actual")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "real")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	ghost := filepath.Join(link, "not-yet-created.txt")

	j, err := NewJail(root)
	if err != nil {
		t.Fatal(err)
	}
	if !j.Contains(ghost) {
		t.Error("not-yet-existing file under internal symlink rejected")
	}
}

func TestNewJailRejectsRelative(t *testing.T) {
	if _, err := NewJail("rel/path"); err == nil {
		t.Fatal("relative root accepted")
	}
	if _, err := NewJail(); err == nil {
		t.Fatal("empty jail accepted")
	}
}

func TestPolicyFirstMatchWinsAndDefaultDeny(t *testing.T) {
	p := &Policy{Rules: []Rule{
		{Op: OpWrite, Path: "src/private/**", Effect: Deny},
		{Op: OpWrite, Path: "src/**", Effect: Allow},
		{Op: OpWrite, Effect: Ask},
		{Op: OpRead, Effect: Allow},
	}}

	cases := []struct {
		op   Op
		path string
		want Effect
	}{
		{OpRead, "anything/here.md", Allow},
		{OpWrite, "src/main.go", Allow},
		{OpWrite, "src/private/keys.env", Deny},
		{OpWrite, "docs/readme.md", Ask},
		{OpNet, "", Deny},
		{OpExec, "", Deny},
	}
	for _, c := range cases {
		got := p.Evaluate(c.op, c.path)
		if got.Effect != c.want {
			t.Errorf("Evaluate(%s, %q) = %s, want %s", c.op, c.path, got.Effect, c.want)
		}
	}
}

func TestMatchPathForms(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{"", "any/thing", true},
		{"**", "any/thing", true},
		{"/**", "/root", true},
		{"src/**", "src/a/b.go", true},
		{"src/**", "src", true},
		{"src/**", "other/x.go", false},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
		{"docs/*.md", "docs/a.md", true},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.rel); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

func TestParsePolicyValidates(t *testing.T) {
	good := `{"rules":[{"op":"read","effect":"allow"},{"op":"net","path":"","effect":"deny"}]}`
	if _, err := ParsePolicy([]byte(good)); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	for name, bad := range map[string]string{
		"unknown op":     `{"rules":[{"op":"fly","effect":"allow"}]}`,
		"unknown effect": `{"rules":[{"op":"read","effect":"maybe"}]}`,
		"bad pattern":    `{"rules":[{"op":"read","path":"a\\b","effect":"allow"}]}`,
	} {
		if _, err := ParsePolicy([]byte(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGuardDeniesOutsideJail(t *testing.T) {
	root := t.TempDir()
	j, err := NewJail(root)
	if err != nil {
		t.Fatal(err)
	}
	p := &Policy{Rules: []Rule{{Op: OpWrite, Effect: Allow}}}
	g := NewGuard(j, p)

	denied := g.Check(OpWrite, filepath.Join(t.TempDir(), "elsewhere"))
	if denied.Effect != Deny || !strings.Contains(denied.Reason, "outside") {
		t.Errorf("Check outside = %+v", denied)
	}
	allowed := g.Check(OpWrite, filepath.Join(root, "file.txt"))
	if allowed.Effect != Allow {
		t.Errorf("Check inside = %+v", allowed)
	}
}

func TestGuardExecUsesSandboxSeam(t *testing.T) {
	root := t.TempDir()
	j, _ := NewJail(root)
	allow := &Policy{Rules: []Rule{{Op: OpExec, Effect: Allow}}}
	deny := &Policy{}

	g := NewGuard(j, allow)
	wrapped, d, err := g.Exec([]string{"git", "status"})
	if err != nil || d.Effect != Allow || len(wrapped) != 2 {
		t.Errorf("Exec allow = %v, %+v, %v", wrapped, d, err)
	}

	g = NewGuard(j, deny)
	if _, _, err := g.Exec([]string{"rm"}); err == nil {
		t.Error("exec permitted under empty policy")
	}
}
