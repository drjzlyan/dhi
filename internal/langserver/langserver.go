// Package langserver is the editor's language table (F-050): which server,
// arguments, formatter and indentation belong to a file, and how DHI
// provisions the server. It is pure data plus resolution — it starts
// nothing and imports nothing from the UI.
package langserver

import (
	"path/filepath"
	"sort"
	"strings"
)

// Method says how a server gets onto the machine.
type Method string

const (
	// MethodToolchain: the binary ships in DHI's hermetic toolchain (gopls).
	MethodToolchain Method = "toolchain"
	// MethodNPM: installed by DHI's own npm into <toolchain>/lsp/<id> after
	// the user confirms the exact packages (nothing installs on open).
	MethodNPM Method = "npm"
	// MethodNone: DHI does not provision it; the user sets `command`.
	MethodNone Method = "none"
)

// Install is the provisioning plan for one language's server. Package
// versions are pins that were installed and handshaken with the hermetic
// npm (F-050) — never "latest".
type Install struct {
	Method   Method
	Packages []string
	Note     string
}

// Language is one resolved entry.
type Language struct {
	ID      string
	Name    string
	Exts    []string          // lower-case, with the dot
	LangIDs map[string]string // ext → LSP languageId, when it differs from ID
	Server  string            // binary name (toolchain/managed) or absolute path
	Args    []string
	Install Install
	UseTabs bool
	Width   int      // indent width when spaces
	Format  []string // external formatter argv (stdin → stdout); empty = server formatting
	Custom  bool     // defined by the user, not built in
}

// LanguageID is the LSP languageId for a file extension.
func (l Language) LanguageID(ext string) string {
	if id, ok := l.LangIDs[strings.ToLower(ext)]; ok {
		return id
	}
	return l.ID
}

// Builtin returns the languages DHI knows out of the box.
func Builtin() []Language {
	return []Language{
		{ID: "go", Name: "Go", Exts: []string{".go"}, Server: "gopls",
			Install: Install{Method: MethodToolchain, Note: "gopls ships with the DHI toolchain (run the first-run install)"},
			UseTabs: true, Width: 4},
		{ID: "typescript", Name: "TypeScript / JavaScript",
			Exts: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
			LangIDs: map[string]string{".tsx": "typescriptreact", ".js": "javascript", ".jsx": "javascriptreact",
				".mjs": "javascript", ".cjs": "javascript"},
			Server: "typescript-language-server", Args: []string{"--stdio"},
			// typescript is pinned to 5.x: TypeScript 7 ships no tsserver and the
			// server refuses to start ("Could not find a valid TypeScript installation").
			Install: Install{Method: MethodNPM, Packages: []string{"typescript-language-server@6.0.1", "typescript@5.9.3"}},
			Width:   2},
		{ID: "python", Name: "Python", Exts: []string{".py", ".pyi"},
			Server: "pyright-langserver", Args: []string{"--stdio"},
			Install: Install{Method: MethodNPM, Packages: []string{"pyright@1.1.414"},
				Note: "pyright does not format; set `formatter` to use one (e.g. [\"ruff\", \"format\", \"-\"])"},
			Width: 4},
		{ID: "bash", Name: "Shell", Exts: []string{".sh", ".bash"},
			LangIDs: map[string]string{".sh": "shellscript", ".bash": "shellscript"},
			Server:  "bash-language-server", Args: []string{"start"},
			Install: Install{Method: MethodNPM, Packages: []string{"bash-language-server@5.8.1"}},
			Width:   2},
		{ID: "yaml", Name: "YAML", Exts: []string{".yaml", ".yml"},
			Server: "yaml-language-server", Args: []string{"--stdio"},
			Install: Install{Method: MethodNPM, Packages: []string{"yaml-language-server@1.24.0"}},
			Width:   2},
		{ID: "json", Name: "JSON", Exts: []string{".json", ".jsonc"},
			LangIDs: map[string]string{".jsonc": "jsonc"},
			Server:  "vscode-json-language-server", Args: []string{"--stdio"},
			Install: Install{Method: MethodNPM, Packages: []string{"vscode-langservers-extracted@4.10.0"}},
			Width:   2},
	}
}

// Override is one user [editor.languages.<id>] table, decoupled from the
// settings package so this one stays free of config concerns.
type Override struct {
	Enabled    *bool
	Name       string
	Command    string
	Args       []string
	Exts       []string
	LanguageID string
	Formatter  []string
}

// Registry resolves a path to its language.
type Registry struct {
	langs []Language
	byExt map[string]int
}

// Resolve applies overrides onto the built-ins. Problems are returned as
// warnings (a custom language without exts or a command, an extension two
// languages claim) — a bad override never breaks the editor, it is reported
// and ignored.
func Resolve(overrides map[string]Override) (Registry, []string) {
	var warns []string
	langs := Builtin()
	idx := map[string]int{}
	for i, l := range langs {
		idx[l.ID] = i
	}
	ids := make([]string, 0, len(overrides))
	for id := range overrides {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	disabled := map[string]bool{}
	for _, id := range ids {
		o := overrides[id]
		key := strings.ToLower(strings.TrimSpace(id))
		if key == "" {
			continue
		}
		if o.Enabled != nil && !*o.Enabled {
			disabled[key] = true
			continue
		}
		i, builtin := idx[key]
		var l Language
		if builtin {
			l = langs[i]
		} else {
			if strings.TrimSpace(o.Command) == "" || len(o.Exts) == 0 {
				warns = append(warns, "editor.languages."+key+": a new language needs both `command` and `exts` — ignored")
				continue
			}
			l = Language{ID: key, Name: key, Custom: true, Width: 4, Install: Install{Method: MethodNone}}
		}
		if o.Name != "" {
			l.Name = o.Name
		}
		if c := strings.TrimSpace(o.Command); c != "" {
			l.Server = c
			l.Install = Install{Method: MethodNone, Note: "uses your configured command"}
			if o.Args == nil && builtin {
				l.Args = nil // a different server: the built-in's flags would not apply
			}
		}
		if o.Args != nil {
			l.Args = append([]string(nil), o.Args...)
		}
		if len(o.Exts) > 0 {
			l.Exts = normExts(o.Exts)
			l.LangIDs = nil
		}
		if o.LanguageID != "" {
			l.LangIDs = nil
			l.ID = key
			if len(l.Exts) > 0 {
				l.LangIDs = map[string]string{}
				for _, e := range l.Exts {
					l.LangIDs[e] = o.LanguageID
				}
			}
		}
		if len(o.Formatter) > 0 {
			l.Format = append([]string(nil), o.Formatter...)
		}
		if builtin {
			langs[i] = l
		} else {
			langs = append(langs, l)
			idx[key] = len(langs) - 1
		}
	}
	r := Registry{byExt: map[string]int{}}
	for _, l := range langs {
		if disabled[l.ID] {
			continue
		}
		r.langs = append(r.langs, l)
	}
	// Custom languages claim extensions before built-ins, so a user can
	// re-route ".js" to their own server.
	order := make([]int, 0, len(r.langs))
	for i, l := range r.langs {
		if l.Custom {
			order = append(order, i)
		}
	}
	for i, l := range r.langs {
		if !l.Custom {
			order = append(order, i)
		}
	}
	for _, i := range order {
		for _, e := range r.langs[i].Exts {
			if j, taken := r.byExt[e]; taken && r.langs[j].ID != r.langs[i].ID {
				if !r.langs[j].Custom {
					warns = append(warns, "extension "+e+" is claimed by both "+r.langs[j].ID+" and "+r.langs[i].ID)
				}
				continue
			}
			r.byExt[e] = i
		}
	}
	return r, warns
}

func normExts(in []string) []string {
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}

// For returns the language of path and the LSP languageId to announce.
func (r Registry) For(path string) (Language, string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	i, ok := r.byExt[ext]
	if !ok {
		return Language{}, "", false
	}
	l := r.langs[i]
	return l, l.LanguageID(ext), true
}

// ByID returns a language by id.
func (r Registry) ByID(id string) (Language, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, l := range r.langs {
		if l.ID == id {
			return l, true
		}
	}
	return Language{}, false
}

// All lists the active languages in table order.
func (r Registry) All() []Language { return append([]Language(nil), r.langs...) }
