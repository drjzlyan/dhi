package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestDefaultsAreSane(t *testing.T) {
	cfg := Defaults()
	if cfg.Theme != theme.Dark().Name || cfg.Editor.TabWidth != 4 || !cfg.Editor.LineNumbers {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestPrecedenceUserOverDefaultsWorkspaceOverUser(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	ws := filepath.Join(dir, "ws.toml")

	os.WriteFile(user, []byte("theme = \"light-paper\"\n\n[editor]\ntab_width = 8\n"), 0o644)
	os.WriteFile(ws, []byte("[editor]\ntab_width = 2\n"), 0o644)

	cfg, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light-paper" {
		t.Errorf("user theme lost: %+v", cfg)
	}
	if cfg.Editor.TabWidth != 2 {
		t.Errorf("workspace tab_width did not override: %d", cfg.Editor.TabWidth)
	}
	if !cfg.Editor.LineNumbers { // unset in both layers → default survives
		t.Error("default line_numbers dropped")
	}
}

func TestBoolFalseOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	os.WriteFile(p, []byte("[editor]\nline_numbers = false\n"), 0o644)

	cfg, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor.LineNumbers {
		t.Error("explicit false overridden by default true")
	}
}

func TestMissingFilesAreFine(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b"))
	if err != nil || cfg.Theme != theme.Dark().Name {
		t.Errorf("cfg=%+v err=%v", cfg, err)
	}
}

func TestMalformedFileErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(p, []byte("not toml ]]"), 0o644)
	if _, err := Load(p, ""); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("err = %v", err)
	}
}

func TestUnknownKeysDetected(t *testing.T) {
	data := []byte("them = \"dark\"\n\n[editr]\ntab_width = 4\n\n[terminal]\nscrollback = 5\n")
	unknown, err := UnknownKeys(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 2 || unknown[0] != "editr.tab_width" || unknown[1] != "them" {
		t.Errorf("unknown = %v", unknown)
	}

	clean := []byte("[terminal]\nscrollback = 5\n")
	if u, _ := UnknownKeys(clean); len(u) != 0 {
		t.Errorf("false positives: %v", u)
	}
}

func TestRoundTripAndSanitize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")

	cfg := Defaults()
	cfg.Theme = "light-paper"
	cfg.Editor.TabWidth = 2
	cfg.Terminal.Scrollback = 250
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}

	back, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, cfg) {
		t.Errorf("round trip mismatch:\n%+v\n%+v", back, cfg)
	}

	// out-of-range values REFUSE with the offending key named (F-011):
	// no silent substitution.
	broken := Defaults()
	broken.Editor.TabWidth = 99
	broken.Security.Sandbox = "paranoid"
	if err := broken.Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, ""); err == nil {
		t.Fatal("expected error for out-of-range tab_width + unknown sandbox mode")
	} else if !strings.Contains(err.Error(), "editor.tab_width") {
		t.Errorf("error should name the key: %v", err)
	}

	// a broken layer must not poison a later-good one: bad user file,
	// no workspace file → error; good user file → fine.
	if _, err := Load(p, ""); err == nil {
		t.Fatal("bad user file must fail")
	}
	good := Defaults()
	if err := good.Save(p); err != nil {
		t.Fatal(err)
	}
	back2, err := Load(p, "")
	if err != nil || !reflect.DeepEqual(back2, good) {
		t.Fatalf("good file failed: %v %+v", err, back2)
	}
}

func TestStrictValidationNamesKeyAndValue(t *testing.T) {
	dir := t.TempDir()
	write := func(doc string) string {
		t.Helper()
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name, doc, wantSub string
	}{
		{"unknown theme", "theme = \"neon-night\"\n", `unknown theme "neon-night"`},
		{"tab width", "[editor]\ntab_width = 32\n", "editor.tab_width"},
		{"scrollback", "[terminal]\nscrollback = 5\n", "terminal.scrollback"},
		{"sandbox mode", "[security]\nsandbox = \"yolo\"\n", `security.sandbox "yolo"`},
		{"schema", "schema = 99\n", "schema"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(write(c.doc), "")
			if err == nil {
				t.Fatalf("expected refusal for %s", c.name)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q missing %q", err, c.wantSub)
			}
		})
	}
}

func TestUnknownKeysRefuseBoot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("[editor]\ntab_widht = 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p, "")
	if err == nil || !strings.Contains(err.Error(), "editor.tab_widht") {
		t.Fatalf("unknown key must refuse with the key named, got: %v", err)
	}
	// best-effort reader (doctor) returns defaults + the error.
	cfg, berr := LoadBestEffort(p, "")
	if berr == nil {
		t.Fatal("best-effort must still report the error")
	}
	if !reflect.DeepEqual(cfg, Defaults()) {
		t.Errorf("best-effort config = %+v, want defaults", cfg)
	}
}

func TestSecuritySandboxModes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")

	// off round-trips and is honored
	cfg := Defaults()
	cfg.Security.Sandbox = SandboxOff
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	back, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if back.Security.Sandbox != SandboxOff {
		t.Fatalf("sandbox mode = %q, want off", back.Security.Sandbox)
	}

	// unknown values REFUSE (strict, F-011) naming the value
	if err := os.WriteFile(p, []byte("[security]\nsandbox = \"paranoid\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, ""); err == nil || !strings.Contains(err.Error(), `"paranoid"`) {
		t.Fatalf("unknown sandbox mode must refuse with the value named, got: %v", err)
	}

	// security.sandbox is a known key (no doctor warning)
	unknown, err := UnknownKeys([]byte("[security]\nsandbox = \"auto\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknown keys: %v", unknown)
	}
}

func TestReducedMotionLayering(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	ws := filepath.Join(dir, "ws.toml")

	// default false survives when unset
	cfg, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReducedMotion {
		t.Error("default reduced_motion must be false")
	}

	// user layer enables it
	os.WriteFile(user, []byte("reduced_motion = true\n"), 0o644)
	cfg, err = Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReducedMotion {
		t.Errorf("user reduced_motion lost: %+v", cfg)
	}

	// workspace layer explicitly cancels it (explicit false is a set
	// value, not "unset")
	os.WriteFile(ws, []byte("reduced_motion = false\n"), 0o644)
	cfg, err = Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReducedMotion {
		t.Errorf("workspace explicit false did not override user true: %+v", cfg)
	}

	// round trip keeps the value
	p := filepath.Join(dir, "save.toml")
	cfg = Defaults()
	cfg.ReducedMotion = true
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	back, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !back.ReducedMotion {
		t.Error("save/load dropped reduced_motion = true")
	}

	// it is a known key (no doctor warning, no boot refusal)
	unknown, err := UnknownKeys([]byte("reduced_motion = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknown keys: %v", unknown)
	}
}

func TestReducedMotionAppliesLive(t *testing.T) {
	t.Cleanup(func() { theme.SetMotion(true) })

	cfg := Defaults()
	cfg.Apply()
	if !theme.Motion {
		t.Fatal("default config must leave motion on")
	}
	cfg.ReducedMotion = true
	cfg.Apply()
	if theme.Motion {
		t.Fatal("reduced_motion=true must disable theme.Motion")
	}
	cfg.ReducedMotion = false
	cfg.Apply()
	if !theme.Motion {
		t.Fatal("reduced_motion=false must restore theme.Motion")
	}
}

func TestApplySwapsLiveTheme(t *testing.T) {
	before := theme.Current
	cfg := Defaults()
	cfg.Theme = theme.Light().Name
	cfg.Apply()
	if theme.Current.Name != theme.Light().Name {
		t.Fatal("Apply did not swap live theme")
	}
	theme.Current = before // restore for other tests

	// unknown theme keeps current
	cfg.Theme = "nope"
	cfg.Apply()
	if theme.Current.Name != before.Name {
		t.Error("unknown theme changed the active one")
	}
}

func TestEngineSetting(t *testing.T) {
	dir := t.TempDir()
	write := func(doc string) string {
		t.Helper()
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := Load(write("schema = 1\nengine = \"cli:claude\"\n"), "")
	if err != nil || cfg.Engine != "cli:claude" {
		t.Fatalf("engine = %q err = %v", cfg.Engine, err)
	}
	// A typo'd key still refuses (strict, ADR-0011).
	if _, err := Load(write("schema = 1\nengin = \"cli:claude\"\n"), ""); err == nil {
		t.Fatal("unknown key must refuse")
	}
	// A malformed engine refuses naming the key.
	if _, err := Load(write("schema = 1\nengine = \"claude\"\n"), ""); err == nil ||
		!strings.Contains(err.Error(), "engine") {
		t.Fatalf("bad engine err = %v", err)
	}
}

func TestWorkspaceScopes(t *testing.T) {
	dir := t.TempDir()
	write := func(doc string) string {
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := Load(write("schema = 1\n[scopes]\nwrite = \"deny\"\npush = \"auto\"\n"), "")
	if err != nil || cfg.Scopes["write"] != "deny" || cfg.Scopes["push"] != "auto" {
		t.Fatalf("scopes = %v err = %v", cfg.Scopes, err)
	}
	// Unknown scope key refuses.
	if _, err := Load(write("schema = 1\n[scopes]\nroot = \"auto\"\n"), ""); err == nil {
		t.Fatal("bad scope key must refuse")
	}
	// Unknown effect refuses.
	if _, err := Load(write("schema = 1\n[scopes]\nwrite = \"maybe\"\n"), ""); err == nil {
		t.Fatal("bad effect must refuse")
	}
}

func TestConventionsDefaultsAndLayering(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	ws := filepath.Join(dir, "ws.toml")
	os.WriteFile(user, []byte("[conventions.commit]\nformat = \"conventional\"\nco_author = \"Bot <bot@x.io>\"\nco_author_enabled = true\n"), 0o644)
	os.WriteFile(ws, []byte("[conventions.branch]\ntask = \"{user}/{slug}\"\n[conventions.commit]\nformat = \"ticket\"\n"), 0o644)

	def, err := Load("", "")
	if err != nil || def.Conventions.Branch.Task != "task/{slug}" || def.Conventions.Commit.Format != "free" {
		t.Fatalf("defaults = %+v err=%v", def.Conventions, err)
	}
	cfg, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Conventions
	if c.Commit.Format != "ticket" || !c.Commit.CoAuthorEnabled ||
		c.Branch.Task != "{user}/{slug}" || c.Branch.Review != "review/{id}" {
		t.Fatalf("layering wrong: %+v", c)
	}
}

func TestConventionsRefuseBadValuesAndTypos(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"[conventions.commit]\nformat = \"wild\"\n":   "commit.format",
		"[conventions.branch]\ntask = \"a/{nope}\"\n": "unknown placeholder",
		"[conventions.commit]\nformat_x = \"free\"\n": "unknown keys",
		"[conventions.copyright]\nenabled = true\n":   "copyright.holder",
	}
	for body, want := range cases {
		p := filepath.Join(dir, "c.toml")
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := Load(p, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
	}
}

func TestConventionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ws", "config.toml")
	cfg := Defaults()
	cfg.Conventions.Commit.Format = "conventional"
	cfg.Conventions.Copyright = conventions.Copyright{Enabled: true, Holder: "Acme", License: "MIT"}
	if err := SaveConventions(ConventionsPath(p), cfg.Conventions); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); strings.Contains(string(data), "conventions") {
		t.Errorf("personal config must not carry conventions:\n%s", data)
	}
	back, err := Load("", p)
	if err != nil || back.Conventions.Commit.Format != "conventional" || !back.Conventions.Copyright.Enabled {
		t.Fatalf("round trip = %+v err=%v", back.Conventions, err)
	}
}

// The team layer must be able to switch a user-level co-author OFF, and a
// settings save must not freeze lower layers into the personal file.
func TestTeamConventionsOverrideUserCoAuthor(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user", "config.toml")
	ws := filepath.Join(dir, "ws", ".dhi", "config.toml")
	os.MkdirAll(filepath.Dir(user), 0o755)
	os.MkdirAll(filepath.Dir(ws), 0o755)
	os.WriteFile(ConventionsPath(user), []byte(
		"[conventions.commit]\nco_author = \"Me <me@x.io>\"\nco_author_enabled = true\n"), 0o644)
	cfg, err := Load(user, ws)
	if err != nil || !cfg.Conventions.Commit.CoAuthorEnabled {
		t.Fatalf("user layer = %+v err=%v", cfg.Conventions.Commit, err)
	}
	cfg.Conventions.Commit.CoAuthorEnabled = false
	if err := SaveConventions(ConventionsPath(ws), cfg.Conventions); err != nil {
		t.Fatal(err)
	}
	back, err := Load(user, ws)
	if err != nil || back.Conventions.Commit.CoAuthorEnabled {
		t.Fatalf("team layer did not switch co-author off: %+v err=%v", back.Conventions.Commit, err)
	}
}

func TestConventionsFileRejectsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "config.toml")
	os.WriteFile(ConventionsPath(ws), []byte("theme = \"light\"\n"), 0o644)
	if _, err := Load("", ws); err == nil || !strings.Contains(err.Error(), "only [conventions.*]") {
		t.Fatalf("err = %v", err)
	}
}

func TestEditorLanguagesLayerAndAreAcceptedKeys(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	ws := filepath.Join(dir, "ws.toml")
	userDoc := "[editor.languages.python]\ncommand = \"/opt/pyls\"\nargs = [\"--stdio\"]\n\n[editor.languages.yaml]\nenabled = false\n"
	wsDoc := "[editor.languages.python]\nformatter = [\"ruff\", \"format\", \"-\"]\n\n[editor.languages.rust]\ncommand = \"/x/rust-analyzer\"\nexts = [\".rs\"]\n"
	if err := os.WriteFile(user, []byte(userDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws, []byte(wsDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{userDoc, wsDoc} {
		if u, err := UnknownKeys([]byte(doc)); err != nil || len(u) != 0 {
			t.Fatalf("documented language keys flagged: %v %v", u, err)
		}
	}
	if u, _ := UnknownKeys([]byte("[editor.languages.python]\ncomand = \"x\"\n")); len(u) != 1 {
		t.Fatalf("a typo in a language table must be flagged, got %v", u)
	}

	cfg, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	py := cfg.Editor.Languages["python"]
	// The workspace table replaces the user's wholesale: no command left over.
	if py.Command != "" || len(py.Formatter) != 3 {
		t.Fatalf("python = %+v", py)
	}
	if y := cfg.Editor.Languages["yaml"]; y.Enabled == nil || *y.Enabled {
		t.Fatalf("yaml = %+v", y)
	}
	ov := cfg.Editor.LanguageOverrides()
	if ov["rust"].Command != "/x/rust-analyzer" || len(ov["rust"].Exts) != 1 {
		t.Fatalf("rust override = %+v", ov["rust"])
	}

	out := filepath.Join(dir, "saved.toml")
	if err := cfg.Save(out); err != nil {
		t.Fatal(err)
	}
	back, err := Load(out, "")
	if err != nil {
		t.Fatalf("saved config does not reload: %v", err)
	}
	if len(back.Editor.Languages) != 3 {
		t.Fatalf("languages after round trip = %+v", back.Editor.Languages)
	}
}
