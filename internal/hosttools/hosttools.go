// Package hosttools finds the tools DHI uses on the user's own system and
// knows how the user's package manager installs a missing one (F-110,
// ADR-0044). DHI downloads nothing itself: a missing tool becomes the
// exact command for Homebrew, apt, dnf, pacman or zypper (or npm, go,
// rustup, uv where those are the norm), which runs only after the user
// says yes, in a terminal they can see.
package hosttools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/hostenv"
)

// Tool is one program DHI can use.
type Tool struct {
	Name    string   // id ("rg", "gopls")
	Bin     string   // executable name on PATH
	Purpose string   // what DHI uses it for, in the card
	Version []string // args that print a version
	// Recipes by installer: OS managers (brew, apt, dnf, pacman, zypper)
	// and language managers (npm, go, rustup, uv). Each is the package
	// argument(s) for that installer.
	Recipes map[string][]string
	Doc     string // where the vendor says to get it
	// Verify: found only when the version command succeeds (macOS ships a
	// /usr/bin/java stub that fails without a JDK).
	Verify bool
	OS     string // "" = every OS; "linux" = Linux only (bwrap)
}

// The installers DHI knows, OS managers first (their order is the
// preference when several are present).
var (
	osManagers   = []string{"brew", "apt", "dnf", "pacman", "zypper"}
	langManagers = []string{"npm", "go", "rustup", "uv"}
)

// Tools is what DHI uses.
var Tools = []Tool{
	{Name: "git", Bin: "git", Purpose: "version control", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"git"}, "apt": {"git"}, "dnf": {"git"}, "pacman": {"git"}, "zypper": {"git"}},
		Doc:     "https://git-scm.com/downloads"},
	{Name: "rg", Bin: "rg", Purpose: "searching the workspace", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"ripgrep"}, "apt": {"ripgrep"}, "dnf": {"ripgrep"}, "pacman": {"ripgrep"}, "zypper": {"ripgrep"}},
		Doc:     "https://github.com/BurntSushi/ripgrep#installation"},
	{Name: "gh", Bin: "gh", Purpose: "pull requests and reviews", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"gh"}, "apt": {"gh"}, "dnf": {"gh"}, "pacman": {"github-cli"}, "zypper": {"gh"}},
		Doc:     "https://cli.github.com"},
	{Name: "go", Bin: "go", Purpose: "Go builds and tests", Version: []string{"version"},
		Recipes: map[string][]string{"brew": {"go"}, "apt": {"golang-go"}, "dnf": {"golang"}, "pacman": {"go"}, "zypper": {"go"}},
		Doc:     "https://go.dev/dl/"},
	{Name: "node", Bin: "node", Purpose: "JavaScript tooling", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"node"}, "apt": {"nodejs", "npm"}, "dnf": {"nodejs"}, "pacman": {"nodejs", "npm"}, "zypper": {"nodejs"}},
		Doc:     "https://nodejs.org"},
	{Name: "python", Bin: "python3", Purpose: "Python tooling", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"python"}, "apt": {"python3"}, "dnf": {"python3"}, "pacman": {"python"}, "zypper": {"python3"}},
		Doc:     "https://www.python.org/downloads/"},
	{Name: "uv", Bin: "uv", Purpose: "Python packages and uvx MCP servers", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"uv"}, "pacman": {"uv"}},
		Doc:     "https://docs.astral.sh/uv/getting-started/installation/"},
	{Name: "rust", Bin: "cargo", Purpose: "Rust builds and tests", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"rust"}, "apt": {"cargo"}, "dnf": {"cargo"}, "pacman": {"rust"}, "zypper": {"cargo"}},
		Doc:     "https://rustup.rs"},
	// Homebrew's openjdk is keg-only (not on PATH), so macOS gets the docs.
	{Name: "jdk", Bin: "java", Purpose: "Java builds and tests", Version: []string{"-version"}, Verify: true,
		Recipes: map[string][]string{"apt": {"default-jdk"}, "dnf": {"java-latest-openjdk-devel"}, "pacman": {"jdk-openjdk"}},
		Doc:     "https://adoptium.net"},
	{Name: "maven", Bin: "mvn", Purpose: "Maven builds and tests", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"maven"}, "apt": {"maven"}, "dnf": {"maven"}, "pacman": {"maven"}, "zypper": {"maven"}},
		Doc:     "https://maven.apache.org/install.html"},
	{Name: "gradle", Bin: "gradle", Purpose: "Gradle builds and tests", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"gradle"}, "pacman": {"gradle"}},
		Doc:     "https://gradle.org/install/"},
	{Name: "dotnet", Bin: "dotnet", Purpose: ".NET builds and tests", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"dotnet"}},
		Doc:     "https://dotnet.microsoft.com/download"},
	{Name: "gopls", Bin: "gopls", Purpose: "Go language features", Version: []string{"version"},
		Recipes: map[string][]string{"brew": {"gopls"}, "pacman": {"gopls"}, "go": {"golang.org/x/tools/gopls@latest"}},
		Doc:     "https://go.dev/gopls"},
	{Name: "dlv", Bin: "dlv", Purpose: "debugging Go", Version: []string{"version"},
		Recipes: map[string][]string{"brew": {"delve"}, "pacman": {"delve"}, "go": {"github.com/go-delve/delve/cmd/dlv@latest"}},
		Doc:     "https://github.com/go-delve/delve"},
	{Name: "typescript-language-server", Bin: "typescript-language-server", Purpose: "TypeScript and JavaScript language features", Version: []string{"--version"},
		// typescript stays on 5.x: TypeScript 7 ships no tsserver and the
		// server refuses to start without one.
		Recipes: map[string][]string{"npm": {"typescript-language-server", "typescript@5"}},
		Doc:     "https://github.com/typescript-language-server/typescript-language-server"},
	{Name: "pyright", Bin: "pyright-langserver", Purpose: "Python language features",
		Recipes: map[string][]string{"brew": {"pyright"}, "npm": {"pyright"}},
		Doc:     "https://github.com/microsoft/pyright"},
	{Name: "bash-language-server", Bin: "bash-language-server", Purpose: "shell script language features", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"bash-language-server"}, "npm": {"bash-language-server"}},
		Doc:     "https://github.com/bash-lsp/bash-language-server"},
	{Name: "yaml-language-server", Bin: "yaml-language-server", Purpose: "YAML language features",
		Recipes: map[string][]string{"brew": {"yaml-language-server"}, "npm": {"yaml-language-server"}},
		Doc:     "https://github.com/redhat-developer/yaml-language-server"},
	{Name: "vscode-langservers-extracted", Bin: "vscode-json-language-server", Purpose: "JSON language features",
		Recipes: map[string][]string{"brew": {"vscode-langservers-extracted"}, "npm": {"vscode-langservers-extracted"}},
		Doc:     "https://github.com/hrsh7th/vscode-langservers-extracted"},
	{Name: "rust-analyzer", Bin: "rust-analyzer", Purpose: "Rust language features", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"rust-analyzer"}, "pacman": {"rust-analyzer"}, "rustup": {"rust-analyzer"}},
		Doc:     "https://rust-analyzer.github.io"},
	{Name: "jdtls", Bin: "jdtls", Purpose: "Java language features",
		Recipes: map[string][]string{"brew": {"jdtls"}},
		Doc:     "https://github.com/eclipse-jdtls/eclipse.jdt.ls"},
	{Name: "claude", Bin: "claude", Purpose: "the Claude Code agent", Version: []string{"--version"},
		Recipes: map[string][]string{"npm": {"@anthropic-ai/claude-code"}},
		Doc:     "https://docs.anthropic.com/en/docs/claude-code/setup"},
	{Name: "codex", Bin: "codex", Purpose: "the Codex agent", Version: []string{"--version"},
		Recipes: map[string][]string{"brew": {"codex"}, "npm": {"@openai/codex"}},
		Doc:     "https://github.com/openai/codex"},
	{Name: "opencode", Bin: "opencode", Purpose: "the opencode agent", Version: []string{"--version"},
		Recipes: map[string][]string{"npm": {"opencode-ai"}},
		Doc:     "https://opencode.ai/docs/"},
	{Name: "copilot", Bin: "copilot", Purpose: "the GitHub Copilot agent", Version: []string{"--version"},
		Recipes: map[string][]string{"npm": {"@github/copilot"}},
		Doc:     "https://docs.github.com/copilot/how-tos/set-up/install-copilot-cli"},
	{Name: "cursor-agent", Bin: "cursor-agent", Purpose: "the Cursor agent", Version: []string{"--version"},
		Doc: "https://cursor.com/cli"},
	{Name: "bwrap", Bin: "bwrap", Purpose: "running agents inside a boundary (Linux)", Version: []string{"--version"}, OS: "linux",
		Recipes: map[string][]string{"apt": {"bubblewrap"}, "dnf": {"bubblewrap"}, "pacman": {"bubblewrap"}, "zypper": {"bubblewrap"}},
		Doc:     "https://github.com/containers/bubblewrap"},
}

// Get finds a tool by name or binary.
func Get(name string) (Tool, bool) {
	for _, t := range Tools {
		if t.Name == name || t.Bin == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Seams swapped by tests.
var (
	look        = hostenv.LookPath
	extraDirs   = installDirs
	npmWritable = npmPrefixWritable
)

// Find returns the tool's path: on the user's PATH, or in a folder a
// language installer puts binaries in that PATH may lack (go install's
// GOBIN, cargo's bin), the way VS Code's Go extension also looks there.
func Find(t Tool) (string, bool) {
	p, _, ok := Locate(t)
	return p, ok
}

// Locate is Find that also says whether the tool is on PATH.
func Locate(t Tool) (path string, onPath, ok bool) {
	path, onPath, ok = locate(t)
	if ok && t.Verify && !runs(path, t.Version) {
		return "", false, false
	}
	return path, onPath, ok
}

func locate(t Tool) (string, bool, bool) {
	if p, err := look(t.Bin); err == nil {
		return p, true, true
	}
	for _, dir := range extraDirs() {
		p := filepath.Join(dir, t.Bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, false, true
		}
	}
	return "", false, false
}

// runs reports whether path args exits 0 within 3 s.
func runs(path string, args []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, args...).Run() == nil
}

// ForThisOS reports whether t applies on goos.
func (t Tool) ForThisOS(goos string) bool { return t.OS == "" || t.OS == goos }

// installDirs are where go install, cargo and pipx/uv put binaries.
func installDirs() []string {
	var dirs []string
	if d := hostenv.Get("GOBIN"); d != "" {
		dirs = append(dirs, d)
	}
	home, _ := os.UserHomeDir()
	if gp := hostenv.Get("GOPATH"); gp != "" {
		for _, d := range filepath.SplitList(gp) {
			dirs = append(dirs, filepath.Join(d, "bin"))
		}
	} else if home != "" {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".cargo", "bin"), filepath.Join(home, ".local", "bin"))
	}
	return dirs
}

var (
	npmOnce sync.Once
	npmOK   bool
)

// npmPrefixWritable reports whether `npm install -g` can write without
// sudo (Homebrew's or nvm's node: yes; a distro's /usr: no).
func npmPrefixWritable() bool {
	npmOnce.Do(func() {
		npm, err := look("npm")
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, npm, "prefix", "-g")
		cmd.Env = hostenv.Env()
		out, err := cmd.Output()
		if err != nil {
			return
		}
		dir := filepath.Join(strings.TrimSpace(string(out)), "lib")
		if _, err := os.Stat(dir); err != nil {
			dir = strings.TrimSpace(string(out))
		}
		if !writable(dir) {
			return
		}
		npmOK = true
	})
	return npmOK
}

// Version runs the tool's version command (first line, 3 s at most).
func Version(t Tool) string {
	p, ok := Find(t)
	if !ok || len(t.Version) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, t.Version...).CombinedOutput()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}

// Managers lists the installers present, OS managers first.
func Managers() []string {
	var out []string
	for _, m := range append(append([]string(nil), osManagers...), langManagers...) {
		bin := m
		if m == "apt" {
			bin = "apt-get"
		}
		if _, err := look(bin); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// OSManager is the OS package manager present ("" when none).
func OSManager() string {
	for _, m := range Managers() {
		for _, o := range osManagers {
			if m == o {
				return m
			}
		}
	}
	return ""
}

// Install is a command that installs a tool.
type Install struct {
	Argv []string // the command, exactly as it runs
	Via  string   // the installer ("brew", "npm", …)
	Sudo bool     // it asks for an administrator password
}

// String is the command as the user would type it.
func (i Install) String() string { return strings.Join(i.Argv, " ") }

// Recipe picks how to install t with what the user has: their OS
// manager when it carries the tool, else a language manager. ok is false
// when nothing present can install it (the card then shows Doc).
func Recipe(t Tool) (Install, bool) {
	return recipeWith(t, Managers())
}

func recipeWith(t Tool, have []string) (Install, bool) {
	for _, m := range have {
		if m == "npm" && !npmWritable() {
			continue // a root-owned global prefix (distro nodejs) would fail with EACCES
		}
		pkgs, ok := t.Recipes[m]
		if !ok {
			continue
		}
		switch m {
		case "brew":
			return Install{Argv: append([]string{"brew", "install"}, pkgs...), Via: m}, true
		case "apt":
			return Install{Argv: append([]string{"sudo", "apt-get", "install", "-y"}, pkgs...), Via: m, Sudo: true}, true
		case "dnf":
			return Install{Argv: append([]string{"sudo", "dnf", "install", "-y"}, pkgs...), Via: m, Sudo: true}, true
		case "pacman":
			return Install{Argv: append([]string{"sudo", "pacman", "-S", "--needed", "--noconfirm"}, pkgs...), Via: m, Sudo: true}, true
		case "zypper":
			return Install{Argv: append([]string{"sudo", "zypper", "--non-interactive", "install"}, pkgs...), Via: m, Sudo: true}, true
		case "npm":
			return Install{Argv: append([]string{"npm", "install", "-g"}, pkgs...), Via: m}, true
		case "go":
			return Install{Argv: append([]string{"go", "install"}, pkgs...), Via: m}, true
		case "rustup":
			return Install{Argv: append([]string{"rustup", "component", "add"}, pkgs...), Via: m}, true
		case "uv":
			return Install{Argv: append([]string{"uv", "tool", "install"}, pkgs...), Via: m}, true
		}
	}
	return Install{}, false
}

// Missing reports, for a tool, the line a card shows: the command when
// one can install it, else where the vendor documents it.
func Missing(t Tool) string {
	if in, ok := Recipe(t); ok {
		return in.String()
	}
	return t.Doc
}

// LegacyToolchainDir is where DHI kept its own toolchain before ADR-0044
// (Go, Node, git… downloaded per user). DHI no longer uses it; doctor
// names it and a repair deletes it after the user says yes.
func LegacyToolchainDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "dhi", "toolchain")
}

// DirSize sums the sizes of the regular files under dir.
func DirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, ierr := d.Info(); ierr == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}
