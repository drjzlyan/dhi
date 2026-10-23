package hosttools

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/hostenv"
)

func TestRecipeUsesWhatTheUserHas(t *testing.T) {
	npmWritable = func() bool { return true }
	defer func() { npmWritable = npmPrefixWritable }()
	rg, _ := Get("rg")
	gopls, _ := Get("gopls")
	claude, _ := Get("claude")
	cursor, _ := Get("cursor-agent")
	cases := []struct {
		tool Tool
		have []string
		want string
		sudo bool
	}{
		{rg, []string{"brew", "npm"}, "brew install ripgrep", false},
		{rg, []string{"apt", "npm"}, "sudo apt-get install -y ripgrep", true},
		{rg, []string{"pacman"}, "sudo pacman -S --needed --noconfirm ripgrep", true},
		{gopls, []string{"apt", "go"}, "go install golang.org/x/tools/gopls@latest", false}, // apt has no gopls recipe
		{claude, []string{"brew", "npm"}, "npm install -g @anthropic-ai/claude-code", false},
	}
	for _, c := range cases {
		in, ok := recipeWith(c.tool, c.have)
		if !ok || in.String() != c.want || in.Sudo != c.sudo {
			t.Errorf("%s with %v = %q sudo=%v ok=%v; want %q", c.tool.Name, c.have, in.String(), in.Sudo, ok, c.want)
		}
	}
	if _, ok := recipeWith(cursor, []string{"brew", "npm"}); ok {
		t.Fatal("a vendor-script tool has no recipe: the card shows its docs")
	}
	if _, ok := recipeWith(rg, nil); ok {
		t.Fatal("no installer present: no recipe")
	}
	ts, _ := Get("typescript-language-server")
	if in, _ := recipeWith(ts, []string{"brew", "npm"}); in.String() != "npm install -g typescript-language-server typescript@5" {
		t.Fatalf("typescript stays on 5.x (7 has no tsserver): %q", in.String())
	}
}

// A distro node's global prefix is root-owned: npm -g would fail, so the
// card falls back to the next installer or the docs instead.
func TestRootOwnedNPMIsSkipped(t *testing.T) {
	npmWritable = func() bool { return false }
	defer func() { npmWritable = npmPrefixWritable }()
	claude, _ := Get("claude")
	if _, ok := recipeWith(claude, []string{"apt", "npm"}); ok {
		t.Fatal("npm with a root-owned prefix must not be offered")
	}
}

// go install puts gopls in ~/go/bin, often not on PATH: still found.
func TestFindLooksInInstallerBinDirs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "gopls"), []byte("#!/bin/sh\n"), 0o755)
	look = func(string) (string, error) { return "", errors.New("not found") }
	extraDirs = func() []string { return []string{dir} }
	defer func() { look, extraDirs = hostenv.LookPath, installDirs }()
	gopls, _ := Get("gopls")
	p, onPath, ok := Locate(gopls)
	if !ok || onPath || p != filepath.Join(dir, "gopls") {
		t.Fatalf("locate = %q onPath=%v ok=%v", p, onPath, ok)
	}
}

func TestManagersAndFindUseTheLoginPath(t *testing.T) {
	npmWritable = func() bool { return true }
	defer func() { npmWritable = npmPrefixWritable }()
	look = func(name string) (string, error) {
		if name == "brew" || name == "npm" || name == "rg" {
			return "/opt/homebrew/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	defer func() { look = hostenv.LookPath }()
	if got := Managers(); len(got) != 2 || got[0] != "brew" || got[1] != "npm" {
		t.Fatalf("managers = %v", got)
	}
	if OSManager() != "brew" {
		t.Fatal("brew is the OS manager")
	}
	rg, _ := Get("rg")
	if p, ok := Find(rg); !ok || p != "/opt/homebrew/bin/rg" {
		t.Fatalf("find = %q", p)
	}
	gopls, _ := Get("gopls")
	if Missing(gopls) != "brew install gopls" {
		t.Fatalf("missing = %q", Missing(gopls))
	}
}

func TestEveryToolHasADocOrRecipe(t *testing.T) {
	for _, tl := range Tools {
		if tl.Doc == "" && len(tl.Recipes) == 0 {
			t.Errorf("%s: no way to get it", tl.Name)
		}
		if tl.Bin == "" || tl.Purpose == "" {
			t.Errorf("%s: missing bin or purpose", tl.Name)
		}
	}
}

func TestRootRunsWithoutSudo(t *testing.T) {
	look = func(n string) (string, error) {
		if n == "apt-get" {
			return "/usr/bin/apt-get", nil
		}
		return "", errors.New("not found")
	}
	isRoot = func() bool { return true }
	defer func() { look, isRoot = hostenv.LookPath, func() bool { return os.Geteuid() == 0 } }()
	bw, _ := Get("bwrap")
	if in, ok := Recipe(bw); !ok || in.Sudo || in.String() != "apt-get install -y bubblewrap" {
		t.Fatalf("root recipe = %q sudo=%v", in.String(), in.Sudo)
	}
}
