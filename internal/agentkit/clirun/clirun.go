// Package clirun declares DHI's host agent CLI runtimes (ADR-0012,
// F-013): user-owned agent CLIs (Claude Code, Codex, …) that a rostered
// agent runs on. Every agent thinks through one of these — DHI no longer
// ships its own engine (ADR-0013). Each adapter declares its binary, the
// tested version, its headless invocation, its stream parser, its cost
// extraction, and the EXACT environment pass-through. The registry is
// static and strict: DHI never installs these CLIs, an unknown runtime
// name is a refusal, and a missing binary is a named doctor row — never
// a fallback.
package clirun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Stream event kinds emitted by every adapter's ParseStream.
const (
	EventProgress = "progress" // assistant text worth showing
	EventCommand  = "command"  // a tool command the CLI ran
	EventError    = "error"    // an error inside the stream
	EventFinal    = "final"    // terminal line; Detail carries the raw JSON
)

// StreamEvent is one neutral transcript event from any CLI's stream.
type StreamEvent struct {
	Kind   string // Event* constant
	Detail string // human-readable one-liner (command, error, raw final)
}

// Usage is what a finished run reports about itself. -1 tokens mean the
// CLI reported nothing (rendered "n/a", never fudged to 0).
type Usage struct {
	TokensIn  int     // -1 = unknown
	TokensOut int     // -1 = unknown
	CostUSD   float64 // 0 when HasCost is false
	HasCost   bool
}

// RunInput is everything an adapter needs to build one headless run.
type RunInput struct {
	Prompt  string // the task prompt (flattened history + trigger)
	System  string // grounding + standards block ("" = none)
	Model   string // "" = the CLI's default model
	Workdir string // run cwd (the task worktree); "" = inherit
}

// CLI is one adapter: the complete declaration of how DHI engages one
// host CLI (ADR-0012). All fields are required; EnvPass is the exact
// pass-through set (doctor reports it — nothing else crosses).
type CLI struct {
	Name      string // registry key = manifest runtime value
	Bin       string // binary name looked up on PATH
	Tested    string // the exact version this adapter was verified against
	EnvPass   []string
	StateRoot func() []string // per-CLI rw roots for the sandbox profile

	// BuildArgs renders the headless argv (without the binary).
	BuildArgs func(in RunInput) []string
	// ParseStream consumes the CLI's stdout and emits neutral events;
	// the terminal line is delivered as EventFinal (Detail = raw line),
	// then the channel closes. Malformed lines become EventError and
	// the stream continues.
	ParseStream func(r io.Reader) <-chan StreamEvent
	// Finalize turns the EventFinal payload into (summary, usage, err).
	Finalize func(final string) (string, Usage, error)
	// Version probes `path --version` and extracts the version token.
	Version func(ctx context.Context, path string) (string, error)
}

// Registry is the declared set of runtimes. Adapters are compiled in;
// Detect only reports availability (ADR-0012 §2).
type Registry struct {
	byName map[string]*CLI
	order  []string
	look   func(file string) (string, error)
}

// NewRegistry compiles the full adapter set. look is injectable for
// tests (production passes exec.LookPath).
func NewRegistry(look func(string) (string, error)) *Registry {
	if look == nil {
		look = exec.LookPath
	}
	r := &Registry{byName: map[string]*CLI{}, look: look}
	for _, c := range allAdapters() {
		r.byName[c.Name] = c
		r.order = append(r.order, c.Name)
	}
	sort.Strings(r.order)
	return r
}

// All returns every registered CLI, sorted by name.
func (r *Registry) All() []*CLI {
	out := make([]*CLI, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.byName[n])
	}
	return out
}

// Get returns one adapter by registry name.
func (r *Registry) Get(name string) (*CLI, bool) {
	c, ok := r.byName[name]
	return c, ok
}

// Path resolves the binary abs path for a registered CLI name (""
// + error when the name is unknown or the binary is missing on PATH).
func (r *Registry) Path(name string) (string, error) {
	c, ok := r.byName[name]
	if !ok {
		return "", fmt.Errorf("clirun: unknown runtime %q", name)
	}
	p, err := r.look(c.Bin)
	if err != nil {
		return "", fmt.Errorf("clirun: %s not found on PATH (install it; DHI never installs host CLIs)", c.Bin)
	}
	return p, nil
}

// Names lists the registered CLI runtime values (sorted, the full valid
// `runtime` value set).
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Detect probes every registered CLI on PATH: name → detected version
// ("" = not detected). A probe that fails or times out counts as not
// detected — the doctor row names it, nothing is guessed (ADR-0011).
func (r *Registry) Detect() map[string]string {
	out := map[string]string{}
	for _, c := range r.All() {
		path, err := r.look(c.Bin)
		if err != nil {
			out[c.Name] = ""
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		v, verr := c.Version(ctx, path)
		cancel()
		if verr != nil {
			out[c.Name] = ""
			continue
		}
		out[c.Name] = v
	}
	return out
}

// ValidRuntime reports whether name is an accepted manifest `runtime`
// value: one of the registered CLIs. Used by manifest validation so a
// typo is a parse error naming the valid set. There is no default any
// more — a manifest without a registered runtime is invalid (ADR-0013).
func (r *Registry) ValidRuntime(name string) bool {
	return ValidRuntimeStatic(name)
}

// ValidRuntimeStatic reports whether name is an accepted runtime value
// without constructing a registry (manifest.Parse uses it: single source
// of truth is the compiled adapter set).
func ValidRuntimeStatic(name string) bool {
	return IsCLIRuntime(name)
}

// IsCLIRuntime reports whether name is a registered host CLI runtime.
// Agents on a CLI runtime run via the adapter; secrets reach them only
// through its declared pass-through (ADR-0012 §4).
func IsCLIRuntime(name string) bool {
	for _, a := range allAdapters() {
		if a.Name == name {
			return true
		}
	}
	return false
}

// CLINames lists the registered CLI runtime names, sorted. Used for
// error text without building a registry.
func CLINames() []string {
	var names []string
	for _, a := range allAdapters() {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}

// ValidRuntimeNames is the error text for an unknown runtime value.
func (r *Registry) ValidRuntimeNames() string {
	return strings.Join(r.Names(), ", ")
}

// ---------------------------------------------------------------------------
// Stream helpers shared by adapters.
// ---------------------------------------------------------------------------

// lineStream splits r into trimmed non-empty lines (folds CRLF).
func lineStream(r io.Reader) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			ch <- line
		}
	}()
	return ch
}

// truncate caps s at n runes for one-line transcript rows.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// firstJSONLineError wraps a malformed terminal line.
var ErrMalformedFinal = errors.New("clirun: malformed final line")

// parseJSONLine decodes one JSON object line into a map.
func parseJSONLine(line string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMalformedFinal, truncate(line, 80))
	}
	return m, nil
}

// probeVersion runs `path flag` (context-bounded) and returns the
// combined output; adapters extract the version token.
func probeVersion(ctx context.Context, path, flag string) (string, error) {
	cmd := exec.CommandContext(ctx, path, flag)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("clirun: version probe timed out")
		}
		return "", fmt.Errorf("clirun: version probe: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
