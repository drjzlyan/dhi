// Package workflow implements DHI's layered feature workflows
// (ADR-0020, F-031). A workflow is an ordered list of steps, each bound
// to a DHI seam or left as guidance; standards tell an agent *what good
// code is*, a workflow tells it *how a feature is done here* (worktree →
// implement → test → commit → push → open PR → review). The steps that
// map to machinery DHI owns (worktree, commit, push, PR, review, the
// declared test command) are enforced at their seam; the rest are
// guidance. A workflow never pretends to verify what it cannot.
//
// Definitions live at .dhi/workflows/<slug>.toml (strict decode); the
// builtin `feature` workflow always exists. Resolution is layered —
// agent → team → workspace default → builtin — and reads fresh per call
// so edits apply without reloads. A malformed definition refuses by name
// (ADR-0011); it is never silently ignored.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the workflow schema this build understands.
const SchemaVersion = 1

// Dir is the workflow definitions directory under the workspace root.
const Dir = ".dhi/workflows"

// DefaultFile names the workspace default-workflow document.
const DefaultFile = ".dhi/workflows.toml"

type defaultDoc struct {
	Schema  int    `toml:"schema"`
	Default string `toml:"default"`
}

// WorkspaceDefault returns the workspace-level default workflow slug, or
// "" when unset. A malformed file refuses by name (ADR-0011).
func WorkspaceDefault(root string) (string, error) {
	path := filepath.Join(root, DefaultFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("workflow: read %s: %w", DefaultFile, err)
	}
	var d defaultDoc
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return "", fmt.Errorf("workflow: parse %s: %w", DefaultFile, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return "", fmt.Errorf("workflow: unknown key(s) in %s", DefaultFile)
	}
	if d.Schema != SchemaVersion {
		return "", fmt.Errorf("workflow: %s schema %d, want %d", DefaultFile, d.Schema, SchemaVersion)
	}
	slug := strings.TrimSpace(d.Default)
	if slug != "" && !slugRe.MatchString(slug) {
		return "", fmt.Errorf("workflow: %s default %q is not a slug", DefaultFile, slug)
	}
	return slug, nil
}

// Gates control what a step does when its condition is unmet.
const (
	GateWarn    = "warn"               // guidance only; never blocks
	GateBlock   = "block"              // refuses the bound seam by name
	GateApprove = "exception-approval" // routes through the approvals queue
)

// Seams a step may bind to. A step bound to SeamNone is guidance-only
// and may only be GateWarn (ADR-0020 §1).
const (
	SeamNone     = "none"
	SeamWorktree = "worktree"
	SeamCommit   = "git:commit"
	SeamPush     = "git:push"
	SeamPR       = "pr"
	SeamReview   = "review"
)

// RunPrefix marks a step bound to an allowlisted run command
// ("run:<cmd>"): e.g. run:test is the task's declared test command.
const RunPrefix = "run:"

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Step is one ordered stage of a workflow.
type Step struct {
	ID       string `toml:"id"`
	Title    string `toml:"title"`
	Guidance string `toml:"guidance"`
	Gate     string `toml:"gate"`
	Bind     string `toml:"bind"`
}

// Definition is one whole workflow.
type Definition struct {
	Schema int    `toml:"schema"`
	Slug   string `toml:"slug"`
	Title  string `toml:"title"`
	Steps  []Step `toml:"step"`
}

// BuiltinSlug is the shipped workflow every feature task defaults to.
const BuiltinSlug = "feature"

// Builtin returns the shipped `feature` workflow (ADR-0020 §3).
func Builtin() *Definition {
	return &Definition{
		Schema: SchemaVersion,
		Slug:   BuiltinSlug,
		Title:  "Feature workflow",
		Steps: []Step{
			{
				ID: "worktree_create", Title: "Work in a worktree",
				Guidance: "Create a git worktree on a fresh branch before changing anything; never work on the shared checkout.",
				Gate:     GateBlock, Bind: SeamWorktree,
			},
			{
				ID: "implement", Title: "Implement test-first",
				Guidance: "Write the test that captures the change first, then the smallest implementation that makes it pass. Keep the diff focused.",
				Gate:     GateWarn, Bind: SeamNone,
			},
			{
				ID: "test", Title: "Run the tests",
				Guidance: "Run the project's declared test command and make it pass before proposing the change.",
				Gate:     GateBlock, Bind: RunPrefix + "test",
			},
			{
				ID: "commit", Title: "Commit the change",
				Guidance: "Commit on the worktree branch with a message that explains the why, not just the what.",
				Gate:     GateWarn, Bind: SeamCommit,
			},
			{
				ID: "push", Title: "Push the branch",
				Guidance: "Push the branch to the remote; never force-push or rewrite shared history.",
				Gate:     GateWarn, Bind: SeamPush,
			},
			{
				ID: "open_pr", Title: "Open a pull request",
				Guidance: "Open a pull request describing the change against its base branch.",
				Gate:     GateWarn, Bind: SeamPR,
			},
			{
				ID: "review", Title: "Get review before merge",
				Guidance: "A human approves the merge; a bypass is an explicit, recorded decision.",
				Gate:     GateApprove, Bind: SeamReview,
			},
		},
	}
}

// IsBuiltin reports whether slug names the shipped workflow.
func IsBuiltin(slug string) bool { return slug == BuiltinSlug }

// Check validates every workflow definition on disk (ADR-0011): local
// files are strict-decoded, step ids unique, gates/binds coherent. A
// missing directory is healthy (the builtin still applies). The error
// names the offending file.
func Check(root string) error {
	_, err := Available(root)
	return err
}

// Available lists the workflow slugs known to this workspace: the
// builtin plus every well-formed .dhi/workflows/<slug>.toml. It returns
// an error (naming the file) if any local definition is malformed.
func Available(root string) ([]string, error) {
	set := map[string]bool{BuiltinSlug: true}
	dir := filepath.Join(root, Dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return sortedSlugs(set), nil
		}
		return nil, fmt.Errorf("workflow: read %s: %w", Dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".toml")
		if !slugRe.MatchString(slug) {
			return nil, fmt.Errorf("workflow: bad slug in %s/%s", Dir, e.Name())
		}
		if _, err := Load(root, slug); err != nil {
			return nil, err
		}
		set[slug] = true
	}
	return sortedSlugs(set), nil
}

// NewDefinition builds a minimal authoring template: a title and one
// guidance step. The step is GateWarn/SeamNone so the template validates
// and can be extended in the file.
func NewDefinition(slug, title string) *Definition {
	return &Definition{
		Schema: SchemaVersion,
		Slug:   slug,
		Title:  title,
		Steps: []Step{{
			ID: "implement", Title: "Implement",
			Guidance: "Describe what this workflow's implementation step should do.",
			Gate:     GateWarn, Bind: SeamNone,
		}},
	}
}

// Save validates and writes a local workflow definition atomically. It
// refuses a malformed definition, so the UI can never persist one.
func Save(root string, d *Definition) error {
	if d == nil || !slugRe.MatchString(d.Slug) {
		return fmt.Errorf("workflow: bad slug")
	}
	if err := validate(d, d.Slug); err != nil {
		return err
	}
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(d); err != nil {
		return fmt.Errorf("workflow: encode %s: %w", d.Slug, err)
	}
	return writeAtomic(filepath.Join(dir, d.Slug+".toml"), []byte(buf.String()))
}

// SaveDefault writes the workspace default-workflow document (empty slug
// clears it).
func SaveDefault(root, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug != "" && !slugRe.MatchString(slug) {
		return fmt.Errorf("workflow: default %q is not a slug", slug)
	}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(defaultDoc{Schema: SchemaVersion, Default: slug}); err != nil {
		return fmt.Errorf("workflow: encode %s: %w", DefaultFile, err)
	}
	return writeAtomic(filepath.Join(root, DefaultFile), []byte(buf.String()))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".workflow-*")
	if err != nil {
		return fmt.Errorf("workflow: write: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("workflow: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("workflow: write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("workflow: write: %w", err)
	}
	return nil
}

// Parse decodes and validates one workflow definition from raw TOML with
// the intended filename stem slug (used by packs before a file lands).
func Parse(slug string, data []byte) (*Definition, error) {
	if !slugRe.MatchString(slug) {
		return nil, fmt.Errorf("workflow: bad slug %q", slug)
	}
	var d Definition
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return nil, fmt.Errorf("workflow: parse %s: %w", slug, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("workflow: unknown key(s) in %s: %s", slug, strings.Join(keys, ", "))
	}
	if err := validate(&d, slug); err != nil {
		return nil, err
	}
	return &d, nil
}

// Load returns the workflow named slug: the builtin for BuiltinSlug, or
// the local definition. It refuses a malformed file by name and a
// missing slug with a named error (never a silent builtin fallback).
func Load(root, slug string) (*Definition, error) {
	if !slugRe.MatchString(slug) {
		return nil, fmt.Errorf("workflow: bad slug %q", slug)
	}
	path := filepath.Join(root, Dir, slug+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if IsBuiltin(slug) {
				return Builtin(), nil
			}
			return nil, fmt.Errorf("workflow: %q not found", slug)
		}
		return nil, fmt.Errorf("workflow: read %s: %w", path, err)
	}
	var d Definition
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return nil, fmt.Errorf("workflow: parse %s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("workflow: unknown key(s) in %s: %s", slug, strings.Join(keys, ", "))
	}
	if err := validate(&d, slug); err != nil {
		return nil, err
	}
	return &d, nil
}

// Resolve picks the active workflow slug from a precedence list — most
// specific first (agent → team → workspace default). The first slug that
// is available wins; an empty list, or one naming only unavailable
// slugs, falls back to the builtin `feature`. A malformed definition
// refuses (never silently downgraded).
func Resolve(root string, precedence []string) (string, error) {
	avail, err := Available(root)
	if err != nil {
		return "", err
	}
	known := map[string]bool{}
	for _, s := range avail {
		known[s] = true
	}
	for _, s := range precedence {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if known[s] {
			return s, nil
		}
		return "", fmt.Errorf("workflow: active workflow %q not found", s)
	}
	return BuiltinSlug, nil
}

func validate(d *Definition, slug string) error {
	if d.Schema != SchemaVersion {
		return fmt.Errorf("workflow: %s schema %d, want %d", slug, d.Schema, SchemaVersion)
	}
	if d.Slug != "" && d.Slug != slug {
		return fmt.Errorf("workflow: %s slug field %q disagrees with filename", slug, d.Slug)
	}
	if len(d.Steps) == 0 {
		return fmt.Errorf("workflow: %s has no steps", slug)
	}
	seen := map[string]bool{}
	for i, s := range d.Steps {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("workflow: %s step %d missing id", slug, i+1)
		}
		if seen[s.ID] {
			return fmt.Errorf("workflow: %s duplicate step id %q", slug, s.ID)
		}
		seen[s.ID] = true
		switch s.Gate {
		case GateWarn, GateBlock, GateApprove:
		default:
			return fmt.Errorf("workflow: %s step %q gate %q must be warn|block|exception-approval", slug, s.ID, s.Gate)
		}
		if !validBind(s.Bind) {
			return fmt.Errorf("workflow: %s step %q bind %q is not a known seam", slug, s.ID, s.Bind)
		}
		// A non-warn gate needs a seam to enforce against.
		if s.Gate != GateWarn && s.Bind == SeamNone {
			return fmt.Errorf("workflow: %s step %q: a %s gate needs a bound seam (bind = %q)", slug, s.ID, s.Gate, SeamNone)
		}
		if strings.HasPrefix(s.Bind, RunPrefix) && strings.TrimPrefix(s.Bind, RunPrefix) == "" {
			return fmt.Errorf("workflow: %s step %q: run: bind needs a command name", slug, s.ID)
		}
	}
	return nil
}

func validBind(b string) bool {
	switch b {
	case SeamNone, SeamWorktree, SeamCommit, SeamPush, SeamPR, SeamReview:
		return true
	}
	return strings.HasPrefix(b, RunPrefix) && strings.TrimPrefix(b, RunPrefix) != ""
}

func sortedSlugs(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
