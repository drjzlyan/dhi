// Package episode keeps an agent's working state across the turns of one
// piece of work (F-066, ADR-0031): the task card, or the conversation
// thread when there is no card. An episode records what the CLI session
// has already seen (so a resumed turn sends only what is new), the CLI
// session id to resume, and cumulative spend. It is DHI-owned and
// engine-neutral: a CLI without verified resume still gets the episode's
// compact summary instead of the full raw history.
package episode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// Episode is one agent's working state on one piece of work.
type Episode struct {
	Key   string `json:"key"`
	Agent string `json:"agent"`
	Goal  string `json:"goal"` // the opening request, trimmed

	// CLI is the adapter the session belongs to, Session its id and
	// Workdir the cwd it ran in: a session resumes only on the same CLI
	// in the same directory (CLIs key sessions by project dir).
	CLI     string `json:"cli,omitempty"`
	Session string `json:"session,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	// SeenID is the newest bus message the CLI session has seen; a
	// resumed turn replays only messages after it.
	SeenID int64 `json:"seen_id,omitempty"`

	Turns       int     `json:"turns"`
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	CacheRead   int     `json:"cache_read"`
	CacheWrite  int     `json:"cache_write"`
	CostUSD     float64 `json:"cost_usd"`
	LastStatus  string  `json:"last_status,omitempty"`
	LastSummary string  `json:"last_summary,omitempty"`

	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
}

// Key names the episode for a turn: the bound task card, else the
// conversation thread.
func Key(taskSlug, channel string, thread int64) string {
	if taskSlug != "" {
		return "task:" + taskSlug
	}
	return fmt.Sprintf("thread:%s:%d", channel, thread)
}

// Resumable reports whether the episode's CLI session can continue on
// cli in workdir.
func (e Episode) Resumable(cli, workdir string) bool {
	return e.Session != "" && e.CLI == cli && e.Workdir == workdir
}

// Fold records one finished run into the episode (pure). session is the
// id the run reported ("" keeps none: a fresh session must not inherit
// a stale id), seen the newest bus message the run's prompt carried.
func Fold(e Episode, run tasks.Run, cli, workdir, session string, seen int64) Episode {
	e.Turns++
	if run.TokensIn > 0 {
		e.TokensIn += run.TokensIn
	}
	if run.TokensOut > 0 {
		e.TokensOut += run.TokensOut
	}
	e.CacheRead += run.CacheRead
	e.CacheWrite += run.CacheWrite
	e.CostUSD += run.CostUSD
	e.LastStatus = string(run.Status)
	if run.Status == tasks.RunOK {
		e.LastSummary = clip(run.Summary, 400)
	} else {
		e.LastSummary = clip(run.Error, 400)
	}
	e.CLI, e.Workdir, e.Session = cli, workdir, session
	if seen > e.SeenID {
		e.SeenID = seen
	}
	e.Updated = run.Finished
	return e
}

// Render is the compact block a fresh (non-resumed) turn carries in
// place of a session: how far the work has come. Empty on a first turn.
func (e Episode) Render() string {
	if e.Turns == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nThis work so far: %d earlier turn", e.Turns)
	if e.Turns > 1 {
		b.WriteString("s")
	}
	if e.LastStatus != "" {
		fmt.Fprintf(&b, "; the last one ended %s", e.LastStatus)
		if e.LastSummary != "" {
			b.WriteString(": " + e.LastSummary)
		}
	}
	b.WriteString(".")
	return b.String()
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Store persists episodes as .dhi/episodes/<agent>/<key>.json.
type Store struct {
	root string
	mu   sync.Mutex
}

// Open returns the store under wsRoot.
func Open(wsRoot string) *Store {
	return &Store{root: filepath.Join(wsRoot, ".dhi", "episodes")}
}

var unsafeRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (s *Store) path(agent, key string) string {
	return filepath.Join(s.root, unsafeRe.ReplaceAllString(agent, "_"), unsafeRe.ReplaceAllString(key, "_")+".json")
}

// Get loads an episode; ok is false when none exists yet.
func (s *Store) Get(agent, key string) (Episode, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path(agent, key))
	if errors.Is(err, fs.ErrNotExist) {
		return Episode{}, false, nil
	}
	if err != nil {
		return Episode{}, false, fmt.Errorf("episode: %w", err)
	}
	var e Episode
	if err := json.Unmarshal(b, &e); err != nil {
		return Episode{}, false, fmt.Errorf("episode: %s: %w", s.path(agent, key), err)
	}
	return e, true, nil
}

// Put writes an episode atomically.
func (s *Store) Put(e Episode) error {
	if e.Agent == "" || e.Key == "" {
		return errors.New("episode: agent and key are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.path(e.Agent, e.Key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("episode: %w", err)
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("episode: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("episode: %w", err)
	}
	return os.Rename(tmp, p)
}

// List returns an agent's episodes, most recently updated first.
func (s *Store) List(agent string) ([]Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.root, unsafeRe.ReplaceAllString(agent, "_"))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("episode: %w", err)
	}
	var out []Episode
	for _, en := range entries {
		if en.IsDir() || filepath.Ext(en.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err != nil {
			return nil, fmt.Errorf("episode: %w", err)
		}
		var e Episode
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, fmt.Errorf("episode: %s: %w", en.Name(), err)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}
