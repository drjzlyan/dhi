// Package settings implements DHI's configuration: a small typed schema,
// layered precedence (defaults < user < workspace), unknown-key detection
// for doctor, and live application (theme swap). Files are hand-editable
// TOML and fully round-trippable.
package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// SchemaVersion is the config schema this build understands.
const SchemaVersion = 1

// DefaultUserPath is $XDG_CONFIG_HOME/dhi/config.toml (or ~/.config/...).
func DefaultUserPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("settings: locate home: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dhi", "config.toml"), nil
}

type Editor struct {
	TabWidth    int  `toml:"tab_width"`
	LineNumbers bool `toml:"line_numbers"`
	// Languages overrides (or adds to) the editor's built-in language
	// servers, keyed by language id (F-050).
	Languages map[string]LanguageConfig `toml:"languages,omitempty"`
}

// LanguageConfig is one [editor.languages.<id>] table. Every field is
// optional for a built-in language; a custom language needs Exts and
// Command. Command is an absolute path or a binary in DHI's toolchain —
// never resolved from the host PATH.
type LanguageConfig struct {
	Enabled    *bool    `toml:"enabled,omitempty"`
	Name       string   `toml:"name,omitempty"`
	Command    string   `toml:"command,omitempty"`
	Args       []string `toml:"args,omitempty"`
	Exts       []string `toml:"exts,omitempty"`
	LanguageID string   `toml:"language_id,omitempty"`
	Formatter  []string `toml:"formatter,omitempty"`
}

// LanguageOverrides converts the [editor.languages] tables for the editor's
// language table (F-050).
func (e Editor) LanguageOverrides() map[string]langserver.Override {
	if len(e.Languages) == 0 {
		return nil
	}
	out := make(map[string]langserver.Override, len(e.Languages))
	for id, c := range e.Languages {
		out[id] = langserver.Override{Enabled: c.Enabled, Name: c.Name, Command: c.Command,
			Args: c.Args, Exts: c.Exts, LanguageID: c.LanguageID, Formatter: c.Formatter}
	}
	return out
}

// languageKeys are the keys a [editor.languages.<id>] table accepts.
var languageKeys = map[string]bool{
	"enabled": true, "name": true, "command": true, "args": true,
	"exts": true, "language_id": true, "formatter": true,
}

type Terminal struct {
	Scrollback int `toml:"scrollback"`
}

// Security holds the hardening toggles (F-010). Sandbox selects the
// OS-sandbox mode: "auto" (use seatbelt/bubblewrap when the platform
// binary exists) or "off" (path-jail + policy only). Unknown values
// sanitize back to auto.
type Security struct {
	Sandbox string `toml:"sandbox"`
}

// Sandbox mode values.
const (
	SandboxAuto = "auto"
	SandboxOff  = "off"
)

// Config is the full typed schema; zero values never leak — Load starts
// from Defaults.
type Config struct {
	Schema        int    `toml:"schema"`
	Theme         string `toml:"theme"`
	ReducedMotion bool   `toml:"reduced_motion"`
	// Engine is the workspace default engine (ADR-0019), "cli:<name>".
	// Empty means agents must name an engine in their manifest; an agent
	// that omits `engine` inherits this one.
	Engine string `toml:"engine,omitempty"`
	// Scopes is the workspace capability layer (F-030 P2): scope→effect
	// overrides applied under team and agent scopes.
	Scopes   map[string]string `toml:"scopes,omitempty"`
	Editor   Editor            `toml:"editor"`
	Terminal Terminal          `toml:"terminal"`
	Security Security          `toml:"security"`
	// Conventions is the team-style layer (F-042): branch names, commit
	// format, copyright header, PR text. It lives in its own tracked file
	// (ConventionsPath), so the personal config never serialises it.
	Conventions conventions.Config `toml:"-"`
}

// Defaults returns the built-in baseline every layer merges onto.
func Defaults() Config {
	return Config{
		Schema:      SchemaVersion,
		Theme:       theme.Dark().Name,
		Editor:      Editor{TabWidth: 4, LineNumbers: true},
		Terminal:    Terminal{Scrollback: 1000},
		Security:    Security{Sandbox: SandboxAuto},
		Conventions: conventions.Defaults(),
	}
}

// Known reports the accepted top-level keys (for doctor warnings).
func Known() []string {
	return []string{"schema", "theme", "reduced_motion", "engine", "editor",
		"terminal", "security", "editor.tab_width", "editor.line_numbers",
		"terminal.scrollback", "security.sandbox",
		"scopes.read", "scopes.write", "scopes.exec", "scopes.network",
		"scopes.git", "scopes.push", "scopes.admin",
		"conventions.branch.task", "conventions.branch.review",
		"conventions.commit.format", "conventions.commit.ticket_pattern",
		"conventions.commit.max_subject", "conventions.commit.co_author",
		"conventions.commit.co_author_enabled",
		"conventions.copyright.enabled", "conventions.copyright.holder",
		"conventions.copyright.license", "conventions.copyright.year",
		"conventions.pr.title", "conventions.pr.body"}
}

// ConventionsFile is the name of the conventions layer that sits next to
// a config file. The workspace copy (.dhi/conventions.toml) is tracked —
// it is the team's shared contract; the user copy is personal.
const ConventionsFile = "conventions.toml"

// ConventionsPath returns the conventions file paired with a config path
// ("" stays "").
func ConventionsPath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), ConventionsFile)
}

// Load merges defaults ← user ← user conventions ← team conventions
// (workspace .dhi/conventions.toml, tracked) ← workspace config
// (.dhi/config.toml, personal). Missing files are fine; malformed files
// surface as errors naming the offending path.
func Load(userPath, wsPath string) (Config, error) {
	cfg := Defaults()
	for _, path := range []string{userPath, ConventionsPath(userPath),
		ConventionsPath(wsPath), wsPath} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return cfg, fmt.Errorf("settings: read %s: %w", path, err)
		}
		var layer fileLayer
		if _, err := toml.Decode(string(data), &layer); err != nil {
			return cfg, fmt.Errorf("settings: parse %s: %w", path, err)
		}
		// Strict mode (ADR-0011): unknown keys refuse instead of warn.
		if unknown, err := UnknownKeys(data); err != nil {
			return cfg, fmt.Errorf("settings: %s: %w", path, err)
		} else if len(unknown) > 0 {
			return cfg, fmt.Errorf("settings: unknown keys in %s: %s (fix or remove them)",
				path, strings.Join(unknown, ", "))
		}
		if filepath.Base(path) == ConventionsFile {
			if err := onlyConventions(data); err != nil {
				return cfg, fmt.Errorf("settings: %s: %w", path, err)
			}
		}
		layer.mergeInto(&cfg)
	}
	if err := validate(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// LiveConventions serves the layered conventions and re-reads them when any
// of the files behind them (user config, user conventions, team conventions,
// workspace config) changes, so a Settings edit — or a hand edit of the
// tracked .dhi/conventions.toml — governs the running crew without a restart
// (F-052). A broken edit keeps the previous rules; Err() names the problem.
func LiveConventions(userPath, wsPath string, initial conventions.Config) *conventions.Live {
	paths := []string{userPath, ConventionsPath(userPath), ConventionsPath(wsPath), wsPath}
	fingerprint := func() string {
		var b strings.Builder
		for _, p := range paths {
			if p == "" {
				continue
			}
			if st, err := os.Stat(p); err == nil {
				fmt.Fprintf(&b, "%s|%d|%d;", p, st.Size(), st.ModTime().UnixNano())
			}
		}
		return b.String()
	}
	return conventions.NewLive(initial, fingerprint, func() (conventions.Config, error) {
		c, err := Load(userPath, wsPath)
		return c.Conventions, err
	})
}

// validate rejects invalid merged values by name — no silent
// substitution (F-011). Every branch names the offending key + value.
func validate(c Config) error {
	if c.Schema != SchemaVersion {
		return fmt.Errorf("settings: schema %d, want %d", c.Schema, SchemaVersion)
	}
	if !themeExists(c.Theme) {
		return fmt.Errorf("settings: unknown theme %q (want one of: %s)",
			c.Theme, strings.Join(ThemeNames(), ", "))
	}
	if c.Editor.TabWidth <= 0 || c.Editor.TabWidth > 16 {
		return fmt.Errorf("settings: editor.tab_width %d out of range 1..16", c.Editor.TabWidth)
	}
	if c.Terminal.Scrollback < 100 {
		return fmt.Errorf("settings: terminal.scrollback %d below minimum 100", c.Terminal.Scrollback)
	}
	switch c.Security.Sandbox {
	case SandboxAuto, SandboxOff:
	default:
		return fmt.Errorf("settings: security.sandbox %q (want %q or %q)",
			c.Security.Sandbox, SandboxAuto, SandboxOff)
	}
	for name, eff := range c.Scopes {
		if _, err := scopes.ParseScope(name); err != nil {
			return fmt.Errorf("settings: scopes.%s: %w", name, err)
		}
		if _, err := scopes.ParseEffect(eff); err != nil {
			return fmt.Errorf("settings: scopes.%s: %w", name, err)
		}
	}
	if err := c.Conventions.Validate(); err != nil {
		return fmt.Errorf("settings: conventions.%w", err)
	}
	if c.Engine != "" {
		kind, name, ok := strings.Cut(strings.TrimSpace(c.Engine), ":")
		if !ok || kind != "cli" || !engineNameRe.MatchString(name) {
			return fmt.Errorf("settings: engine %q must be \"cli:<name>\" (lowercase [a-z0-9._-])", c.Engine)
		}
	}
	return nil
}

// engineNameRe matches a valid engine CLI name (registration is checked
// by the runtime and doctor, which own the adapter registry).
var engineNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// LoadBestEffort returns defaults merged with whatever parsed, plus the
// first error — for diagnostics (doctor) that must report ON a broken
// install rather than refuse to run. Production boot uses Load.
func LoadBestEffort(userPath, wsPath string) (Config, error) {
	cfg, err := Load(userPath, wsPath)
	if err == nil {
		return cfg, nil
	}
	return Defaults(), err
}

// fileLayer decodes one TOML document; pointers distinguish "unset"
// from false/zero so later layers only override what they set.
type fileLayer struct {
	Schema        int               `toml:"schema"`
	Theme         string            `toml:"theme"`
	ReducedMotion *bool             `toml:"reduced_motion"`
	Engine        string            `toml:"engine"`
	Scopes        map[string]string `toml:"scopes"`
	Editor        struct {
		TabWidth    int                       `toml:"tab_width"`
		LineNumbers *bool                     `toml:"line_numbers"`
		Languages   map[string]LanguageConfig `toml:"languages"`
	} `toml:"editor"`
	Terminal struct {
		Scrollback int `toml:"scrollback"`
	} `toml:"terminal"`
	Security struct {
		Sandbox string `toml:"sandbox"`
	} `toml:"security"`
	Conventions struct {
		Branch struct {
			Task   string `toml:"task"`
			Review string `toml:"review"`
		} `toml:"branch"`
		Commit struct {
			Format        string `toml:"format"`
			TicketPattern string `toml:"ticket_pattern"`
			MaxSubject    int    `toml:"max_subject"`
			CoAuthor      string `toml:"co_author"`
			// A pointer, so a layer can switch the trailer off explicitly.
			CoAuthorEnabled *bool `toml:"co_author_enabled"`
		} `toml:"commit"`
		Copyright struct {
			Enabled *bool  `toml:"enabled"`
			Holder  string `toml:"holder"`
			License string `toml:"license"`
			Year    string `toml:"year"`
		} `toml:"copyright"`
		PR struct {
			Title string `toml:"title"`
			Body  string `toml:"body"`
		} `toml:"pr"`
	} `toml:"conventions"`
}

// setStr overwrites dst with a trimmed non-empty layer value.
func setStr(dst *string, v string) {
	if v = strings.TrimSpace(v); v != "" {
		*dst = v
	}
}

func (f fileLayer) mergeInto(dst *Config) {
	if f.Schema != 0 {
		dst.Schema = f.Schema
	}
	if f.Theme != "" {
		dst.Theme = strings.TrimSpace(f.Theme)
	}
	if f.ReducedMotion != nil {
		dst.ReducedMotion = *f.ReducedMotion
	}
	if f.Engine != "" {
		dst.Engine = strings.TrimSpace(f.Engine)
	}
	if f.Scopes != nil {
		dst.Scopes = f.Scopes
	}
	if f.Editor.TabWidth != 0 {
		dst.Editor.TabWidth = f.Editor.TabWidth
	}
	if f.Editor.LineNumbers != nil {
		dst.Editor.LineNumbers = *f.Editor.LineNumbers
	}
	for id, lc := range f.Editor.Languages { // a later layer replaces a language wholesale
		if dst.Editor.Languages == nil {
			dst.Editor.Languages = map[string]LanguageConfig{}
		}
		dst.Editor.Languages[strings.ToLower(strings.TrimSpace(id))] = lc
	}
	if f.Terminal.Scrollback != 0 {
		dst.Terminal.Scrollback = f.Terminal.Scrollback
	}
	if f.Security.Sandbox != "" {
		dst.Security.Sandbox = strings.TrimSpace(f.Security.Sandbox)
	}
	cv, d := f.Conventions, &dst.Conventions
	setStr(&d.Branch.Task, cv.Branch.Task)
	setStr(&d.Branch.Review, cv.Branch.Review)
	setStr(&d.Commit.Format, cv.Commit.Format)
	setStr(&d.Commit.TicketPattern, cv.Commit.TicketPattern)
	setStr(&d.Commit.CoAuthor, cv.Commit.CoAuthor)
	if cv.Commit.CoAuthorEnabled != nil {
		d.Commit.CoAuthorEnabled = *cv.Commit.CoAuthorEnabled
	}
	if cv.Commit.MaxSubject != 0 {
		d.Commit.MaxSubject = cv.Commit.MaxSubject
	}
	if cv.Copyright.Enabled != nil {
		d.Copyright.Enabled = *cv.Copyright.Enabled
	}
	setStr(&d.Copyright.Holder, cv.Copyright.Holder)
	setStr(&d.Copyright.License, cv.Copyright.License)
	setStr(&d.Copyright.Year, cv.Copyright.Year)
	setStr(&d.PR.Title, cv.PR.Title)
	setStr(&d.PR.Body, cv.PR.Body)
}

// themeExists checks the theme registry (kept here to avoid an import
// cycle from theme → settings).
var themeRegistry = map[string]func() theme.Tokens{
	theme.Dark().Name:         theme.Dark,
	theme.Light().Name:        theme.Light,
	theme.HighContrast().Name: theme.HighContrast,
}

// ThemeNames lists the registered themes in a stable display order
// (default first), for pickers and error messages.
func ThemeNames() []string {
	return []string{theme.Dark().Name, theme.Light().Name, theme.HighContrast().Name}
}

func themeExists(name string) bool {
	_, ok := themeRegistry[name]
	return ok
}

// Apply sets the live theme from c.Theme (unknown names keep current)
// and the live reduced-motion switch from c.ReducedMotion (F-012);
// surfaces read theme.Motion, so a Settings toggle takes effect on the
// next frame.
func (c Config) Apply() {
	if fn, ok := themeRegistry[c.Theme]; ok {
		theme.Current = fn()
	}
	theme.SetMotion(!c.ReducedMotion)
}

// UnknownKeys parses data and returns top-level/dotted keys not in the
// schema — doctor surfaces these as warnings (F-006 acceptance).
func UnknownKeys(data []byte) ([]string, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, k := range Known() {
		known[k] = true
	}
	var out []string
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			dotted := k
			if prefix != "" {
				dotted = prefix + "." + k
			}
			if sub, ok := v.(map[string]any); ok {
				walk(dotted, sub)
				continue
			}
			if !known[dotted] && !knownLanguageKey(dotted) {
				out = append(out, dotted)
			}
		}
	}
	walk("", raw)
	sortStrings(out)
	return out, nil
}

// knownLanguageKey accepts editor.languages.<id>.<key> for the documented keys.
func knownLanguageKey(dotted string) bool {
	rest, ok := strings.CutPrefix(dotted, "editor.languages.")
	if !ok {
		return false
	}
	i := strings.LastIndex(rest, ".")
	return i > 0 && languageKeys[rest[i+1:]]
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Save writes cfg as TOML with a leading comment.
func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("settings: save: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("# DHI configuration (hand-editable)\n")
	if err := toml.NewEncoder(&sb).Encode(c); err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("settings: save: %w", err)
	}
	return nil
}

// onlyConventions rejects a conventions file carrying anything but
// [conventions.*] — it is a shared, tracked contract, not a second config.
func onlyConventions(data []byte) error {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return err
	}
	for k := range raw {
		if k != "conventions" {
			return fmt.Errorf("only [conventions.*] is allowed here, found %q", k)
		}
	}
	return nil
}

// SaveConventions writes the conventions table to path in full — the file
// is an explicit, readable contract, so defaults are written too.
func SaveConventions(path string, c conventions.Config) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("settings: conventions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("settings: save conventions: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("# DHI team conventions (tracked; hand-editable)\n")
	if err := toml.NewEncoder(&sb).Encode(map[string]any{"conventions": c}); err != nil {
		return fmt.Errorf("settings: encode conventions: %w", err)
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
