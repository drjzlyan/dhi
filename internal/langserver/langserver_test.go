package langserver

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestEveryBuiltinExtensionResolvesToItsLanguageAndLanguageID(t *testing.T) {
	r, warns := Resolve(nil)
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	for _, c := range []struct{ path, id, langID string }{
		{"/a/main.go", "go", "go"},
		{"x.ts", "typescript", "typescript"},
		{"x.TSX", "typescript", "typescriptreact"},
		{"x.jsx", "typescript", "javascriptreact"},
		{"x.mjs", "typescript", "javascript"},
		{"x.py", "python", "python"},
		{"deploy.sh", "bash", "shellscript"},
		{"ci.yml", "yaml", "yaml"},
		{"tsconfig.jsonc", "json", "jsonc"},
		{"package.json", "json", "json"},
	} {
		l, langID, ok := r.For(c.path)
		if !ok || l.ID != c.id || langID != c.langID {
			t.Errorf("%s → %q/%q ok=%v, want %q/%q", c.path, l.ID, langID, ok, c.id, c.langID)
		}
	}
	if _, _, ok := r.For("README.md"); ok {
		t.Error("markdown has no server")
	}
}

func TestBuiltinInstallPlansArePinnedAndNeverLatest(t *testing.T) {
	for _, l := range Builtin() {
		if l.Install.Method == MethodNPM {
			if len(l.Install.Packages) == 0 {
				t.Errorf("%s: npm method without packages", l.ID)
			}
			for _, p := range l.Install.Packages {
				if i := strings.LastIndex(p, "@"); i <= 0 || i == len(p)-1 {
					t.Errorf("%s: package %q has no pinned version", l.ID, p)
				}
			}
		}
		if l.Server == "" {
			t.Errorf("%s: no server", l.ID)
		}
	}
}

func TestOverridesReplaceDisableAndAddLanguages(t *testing.T) {
	r, warns := Resolve(map[string]Override{
		"python": {Command: "/opt/basedpyright-langserver", Args: []string{"--stdio", "-v"}, Formatter: []string{"ruff", "format", "-"}},
		"yaml":   {Enabled: ptr(false)},
		"rust":   {Command: "/home/me/.cargo/bin/rust-analyzer", Exts: []string{"rs"}},
		"zig":    {Command: "zls"}, // no exts: rejected
	})
	py, _, _ := r.For("a.py")
	if py.Server != "/opt/basedpyright-langserver" || len(py.Args) != 2 || py.Install.Method != MethodNone || len(py.Format) != 3 {
		t.Fatalf("python = %+v", py)
	}
	if _, _, ok := r.For("a.yaml"); ok {
		t.Error("disabled language still resolves")
	}
	rs, id, ok := r.For("lib.rs")
	if !ok || !rs.Custom || id != "rust" || rs.Server != "/home/me/.cargo/bin/rust-analyzer" {
		t.Fatalf("custom language = %+v %q %v", rs, id, ok)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "zig") {
		t.Fatalf("warnings = %v", warns)
	}
}

func TestChangingTheCommandDropsTheBuiltinsFlags(t *testing.T) {
	r, _ := Resolve(map[string]Override{"bash": {Command: "/x/other-bash-ls"}})
	l, _, _ := r.For("a.sh")
	if len(l.Args) != 0 {
		t.Fatalf("flags %v of a different server kept", l.Args)
	}
}

func TestACustomLanguageMayTakeOverABuiltinExtension(t *testing.T) {
	r, warns := Resolve(map[string]Override{"deno": {Command: "/x/deno", Args: []string{"lsp"}, Exts: []string{".ts"}}})
	l, _, _ := r.For("a.ts")
	if l.ID != "deno" || len(warns) != 0 {
		t.Fatalf("ts → %q, warns=%v", l.ID, warns)
	}
	if l2, _, _ := r.For("a.js"); l2.ID != "typescript" {
		t.Fatalf("js → %q", l2.ID)
	}
}
