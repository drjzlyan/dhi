// Package settings implements DHI's configuration: a small typed schema,
// layered precedence (defaults < user < workspace), unknown-key detection
// for doctor, and live application (theme swap). Files are hand-editable
// TOML and fully round-trippable.
package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

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
}

type Terminal struct {
	Scrollback int `toml:"scrollback"`
}

// Config is the full typed schema; zero values never leak — Load starts
// from Defaults.
type Config struct {
	Schema   int      `toml:"schema"`
	Theme    string   `toml:"theme"`
	Editor   Editor   `toml:"editor"`
	Terminal Terminal `toml:"terminal"`
}

// Defaults returns the built-in baseline every layer merges onto.
func Defaults() Config {
	return Config{
		Schema:   SchemaVersion,
		Theme:    theme.Dark().Name,
		Editor:   Editor{TabWidth: 4, LineNumbers: true},
		Terminal: Terminal{Scrollback: 1000},
	}
}

// Known reports the accepted top-level keys (for doctor warnings).
func Known() []string {
	return []string{"schema", "theme", "editor", "terminal",
		"editor.tab_width", "editor.line_numbers", "terminal.scrollback"}
}

// Load merges defaults ← user ← workspace. Missing files are fine;
// malformed files surface as errors naming the offending path.
func Load(userPath, wsPath string) (Config, error) {
	cfg := Defaults()
	for _, path := range []string{userPath, wsPath} {
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
		layer.mergeInto(&cfg)
	}
	if cfg.Schema != SchemaVersion {
		return cfg, fmt.Errorf("settings: schema %d, want %d", cfg.Schema, SchemaVersion)
	}
	sanitize(&cfg)
	return cfg, nil
}

// fileLayer decodes one TOML document; pointers distinguish "unset"
// from false/zero so later layers only override what they set.
type fileLayer struct {
	Schema int    `toml:"schema"`
	Theme  string `toml:"theme"`
	Editor struct {
		TabWidth    int   `toml:"tab_width"`
		LineNumbers *bool `toml:"line_numbers"`
	} `toml:"editor"`
	Terminal struct {
		Scrollback int `toml:"scrollback"`
	} `toml:"terminal"`
}

func (f fileLayer) mergeInto(dst *Config) {
	if f.Schema != 0 {
		dst.Schema = f.Schema
	}
	if f.Theme != "" {
		dst.Theme = strings.TrimSpace(f.Theme)
	}
	if f.Editor.TabWidth != 0 {
		dst.Editor.TabWidth = f.Editor.TabWidth
	}
	if f.Editor.LineNumbers != nil {
		dst.Editor.LineNumbers = *f.Editor.LineNumbers
	}
	if f.Terminal.Scrollback != 0 {
		dst.Terminal.Scrollback = f.Terminal.Scrollback
	}
}

func sanitize(c *Config) {
	if c.Editor.TabWidth <= 0 || c.Editor.TabWidth > 16 {
		c.Editor.TabWidth = 4
	}
	if c.Terminal.Scrollback < 100 {
		c.Terminal.Scrollback = 1000
	}
	if !themeExists(c.Theme) {
		c.Theme = theme.Dark().Name
	}
}

// themeExists checks the theme registry (kept here to avoid an import
// cycle from theme → settings).
var themeRegistry = map[string]func() theme.Tokens{
	theme.Dark().Name:  theme.Dark,
	theme.Light().Name: theme.Light,
}

func themeExists(name string) bool {
	_, ok := themeRegistry[name]
	return ok
}

// Apply sets the live theme from c.Theme; unknown names keep current.
func (c Config) Apply() {
	if fn, ok := themeRegistry[c.Theme]; ok {
		theme.Current = fn()
	}
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
	for k, v := range raw {
		if sub, ok := v.(map[string]any); ok {
			for sk := range sub {
				dotted := k + "." + sk
				if !known[dotted] {
					out = append(out, dotted)
				}
			}
			continue
		}
		if !known[k] {
			out = append(out, k)
		}
	}
	sortStrings(out)
	return out, nil
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
