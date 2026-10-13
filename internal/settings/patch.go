package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/conventions"
)

// Agent-proposable settings (F-070). An agent may ask for any of these;
// the human approves each change. Security posture (security.sandbox),
// keys and conventions are never proposable by an agent.
var proposable = map[string]string{
	"agents.max_turns": "int", "agents.resume": "bool", "agents.autonomy": "string",
	"agents.max_iterations": "int", "agents.stall_seconds": "int", "agents.reflect": "bool",
	"agents.reflect_model": "string",
	"budgets.task_usd":     "float", "budgets.task_tokens": "int",
	"budgets.agent_day_usd": "float", "budgets.workspace_day_usd": "float",
	"scopes.read": "string", "scopes.write": "string", "scopes.exec": "string", "scopes.network": "string",
	"scopes.git": "string", "scopes.push": "string", "scopes.admin": "string",
	"editor.tab_width": "int", "editor.line_numbers": "bool", "terminal.scrollback": "int",
	"worktrees.root": "string", "engine": "string",
}

// Proposable lists the keys an agent may propose, sorted.
func Proposable() []string {
	out := make([]string, 0, len(proposable))
	for k := range proposable {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// parseValue types raw for key.
func parseValue(key, raw string) (any, error) {
	typ, ok := proposable[key]
	if !ok {
		return nil, fmt.Errorf("settings: %q cannot be proposed (proposable: %s)", key, strings.Join(Proposable(), ", "))
	}
	raw = strings.TrimSpace(raw)
	switch typ {
	case "int":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("settings: %s wants an integer, got %q", key, raw)
		}
		return v, nil
	case "float":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("settings: %s wants a number, got %q", key, raw)
		}
		return v, nil
	case "bool":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("settings: %s wants true or false, got %q", key, raw)
		}
		return v, nil
	}
	return raw, nil
}

// readLayer decodes a layer file into a generic map (missing = empty).
func readLayer(path string) (map[string]any, error) {
	m := map[string]any{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings: read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(b), &m); err != nil {
		return nil, fmt.Errorf("settings: parse %s: %w", path, err)
	}
	return m, nil
}

// setDotted sets a dotted key in a nested map.
func setDotted(m map[string]any, key string, v any) {
	parts := strings.Split(key, ".")
	cur := m
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = v
}

func encodeLayer(m map[string]any) (string, error) {
	if _, ok := m["schema"]; !ok {
		m["schema"] = int64(SchemaVersion)
	}
	var b strings.Builder
	b.WriteString("# DHI configuration (hand-editable)\n")
	if err := toml.NewEncoder(&b).Encode(m); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Value renders the effective value of a dotted key in c ("" = unset).
func Value(c Config, key string) string {
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(c); err != nil {
		return ""
	}
	m := map[string]any{}
	if _, err := toml.Decode(sb.String(), &m); err != nil {
		return ""
	}
	var cur any = m
	for _, p := range strings.Split(key, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprint(cur)
}

// CheckChange validates setting key to raw on the layer at layerPath, as
// the full stack (userPath + workspace) would load it, and returns the
// current effective value. Nothing is written.
func CheckChange(userPath, wsPath, layerPath, key, raw string) (old string, err error) {
	v, err := parseValue(key, raw)
	if err != nil {
		return "", err
	}
	cur, err := Load(userPath, wsPath)
	if err != nil {
		return "", err
	}
	m, err := readLayer(layerPath)
	if err != nil {
		return "", err
	}
	setDotted(m, key, v)
	body, err := encodeLayer(m)
	if err != nil {
		return "", err
	}
	// A private directory, so Load's conventions file lookup beside the
	// layer finds nothing stray (a shared /tmp might hold one).
	dir, err := os.MkdirTemp("", "dhi-setting-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tmp := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", err
	}
	u, w := userPath, wsPath
	if filepath.Clean(layerPath) == filepath.Clean(userPath) {
		u = tmp
	} else {
		w = tmp
	}
	if _, err := Load(u, w); err != nil {
		return "", err
	}
	return Value(cur, key), nil
}

// SetLayerKey writes one key into the layer file at path, leaving every
// other key in that file as it was (a proposal changes one setting, not
// the whole layer).
func SetLayerKey(path, key, raw string) error {
	v, err := parseValue(key, raw)
	if err != nil {
		return err
	}
	m, err := readLayer(path)
	if err != nil {
		return err
	}
	setDotted(m, key, v)
	body, err := encodeLayer(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

// PatchConventions writes only the named conventions keys into the
// conventions layer at path (F-075), keeping every other key in that
// file: set maps dotted keys under [conventions] ("branch.task") to
// values; templates replaces whole named templates, and a nil entry
// deletes one. Two writers (Settings CONFIG rows and the CONVENTIONS
// section) can then share one file without either reverting the other.
func PatchConventions(path string, set map[string]any, templates map[string]*conventions.Template) error {
	m, err := readLayer(path)
	if err != nil {
		return err
	}
	for k, v := range set {
		setDotted(m, "conventions."+k, v)
	}
	for name, t := range templates {
		if t == nil {
			if cv, ok := m["conventions"].(map[string]any); ok {
				if tp, ok := cv["templates"].(map[string]any); ok {
					delete(tp, name)
				}
			}
			continue
		}
		var sb strings.Builder
		if err := toml.NewEncoder(&sb).Encode(t); err != nil {
			return fmt.Errorf("settings: encode template %s: %w", name, err)
		}
		tm := map[string]any{}
		if _, err := toml.Decode(sb.String(), &tm); err != nil {
			return fmt.Errorf("settings: template %s: %w", name, err)
		}
		setDotted(m, "conventions.templates."+name, tm)
	}
	var b strings.Builder
	b.WriteString("# DHI team conventions (tracked; hand-editable)\n")
	if err := toml.NewEncoder(&b).Encode(m); err != nil {
		return fmt.Errorf("settings: encode conventions: %w", err)
	}
	if err := onlyConventions([]byte(b.String())); err != nil {
		return fmt.Errorf("settings: %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// SaveProfile replaces the [profile] table in the layer at path (F-081),
// leaving every other key in the file as it was. The result must load.
func SaveProfile(path string, p Profile) error {
	m, err := readLayer(path)
	if err != nil {
		return err
	}
	prof := map[string]any{
		"writing": map[string]any{"tone": p.Writing.Tone, "length": p.Writing.Length,
			"emoji": p.Writing.Emoji, "language": p.Writing.Language},
		"reading": map[string]any{"order": p.Reading.Order, "format": p.Reading.Format, "detail": p.Reading.Detail},
	}
	if p.Role != "" {
		prof["role"] = p.Role
	}
	m["profile"] = prof
	out, err := encodeLayer(m)
	if err != nil {
		return err
	}
	if err := checkLoads(path, out); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// checkLoads refuses a layer body that would not load on its own.
func checkLoads(path, body string) error {
	dir, err := os.MkdirTemp("", "dhi-setting-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tmp := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	_, err = Load(tmp, "")
	return err
}
