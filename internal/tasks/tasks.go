// Package tasks is DHI's task tracker (F-003 component 3): one TOML
// card per task under the reserved .dhi/tasks/ tree, kanban statuses,
// and ChangeSets — per-member linked worktrees created through an
// injectable attach seam so the store stays hermetic in tests and works
// pre-registry-flip (attaching reports a visible error until the
// hermetic git shim exists).
package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// SchemaVersion is the task-card schema this build understands.
const SchemaVersion = 1

// Dir is the reserved tasks tree under the workspace root.
const Dir = ".dhi/tasks"

// Status enumerates kanban columns.
type Status string

// Kanban statuses, in flow order.
const (
	Backlog  Status = "backlog"
	Active   Status = "active"
	InReview Status = "in-review"
	Done     Status = "done"
)

// Statuses is the canonical column order for UIs.
var Statuses = []Status{Backlog, Active, InReview, Done}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidStatus reports whether s names a kanban column.
func ValidStatus(s Status) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// ChangeSet binds one member repo to the task via a linked worktree.
type ChangeSet struct {
	Member string `toml:"member"`
	Branch string `toml:"branch"`
	Path   string `toml:"path"` // worktree dir, relative to workspace root
}

// Task is one card. Thread binding points at the conversation whose
// progress messages accompany the work.
type Task struct {
	Slug     string
	Title    string
	Status   Status
	Assignee string // agent id or "you"; "" = unassigned
	Team     string // optional org team slug

	ThreadChannel string
	ThreadID      int64

	ChangeSets []ChangeSet

	CreatedAt time.Time
	UpdatedAt time.Time
}

// file is the on-disk TOML shape.
type file struct {
	Schema        int         `toml:"schema"`
	Title         string      `toml:"title"`
	Status        Status      `toml:"status"`
	Assignee      string      `toml:"assignee"`
	Team          string      `toml:"team"`
	ThreadChannel string      `toml:"thread_channel"`
	ThreadID      int64       `toml:"thread_id"`
	ChangeSets    []ChangeSet `toml:"changeset"`
	CreatedAt     time.Time   `toml:"created_at"`
	UpdatedAt     time.Time   `toml:"updated_at"`
}

// AttachFn creates one linked worktree and returns its path relative to
// the workspace root. Production wires gitcore.Runner; tests fake it.
// A nil seam disables attaching with a visible error.
type AttachFn func(taskSlug, member, branch, startpoint string) (relPath string, err error)

// DetachFn removes a previously attached worktree (metadata only).
type DetachFn func(taskSlug, relPath string) error

// Store is the loaded task set; safe for concurrent use.
type Store struct {
	ws *workspace.Workspace

	mu     sync.RWMutex
	tasks  map[string]Task
	order  []string // slugs sorted for deterministic listing
	warns  []string // malformed cards skipped at Open
	subs   map[int]chan Change
	subSeq int
	attach AttachFn
	detach DetachFn
	now    func() time.Time
}

// Change announces one committed task mutation.
type Change struct {
	Kind ChangeKind
	Slug string
}

type ChangeKind string

// Change kinds.
const (
	TaskCreated ChangeKind = "created"
	TaskUpdated ChangeKind = "updated"
	TaskRemoved ChangeKind = "removed"
)

// Open loads every *.toml under .dhi/tasks/. Missing dir = empty store;
// malformed files are skipped and reported via Warnings (doctor).
func Open(ws *workspace.Workspace) (*Store, error) {
	s := &Store{
		ws:    ws,
		tasks: map[string]Task{},
		subs:  map[int]chan Change{},
		now:   time.Now,
	}
	dir := s.dir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tasks: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".toml")
		t, perr := parseCard(filepath.Join(dir, e.Name()), slug)
		if perr != nil {
			s.warns = append(s.warns, perr.Error())
			continue
		}
		s.tasks[slug] = t
		s.order = append(s.order, slug)
	}
	sort.Strings(s.order)
	return s, nil
}

func parseCard(path, slug string) (Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: read %s: %w", slug, err)
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: %s: %w", slug, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		return Task{}, fmt.Errorf("tasks: %s: unknown key(s): %s", slug, strings.Join(keys, ", "))
	}
	if f.Schema != SchemaVersion {
		return Task{}, fmt.Errorf("tasks: %s: schema %d, want %d", slug, f.Schema, SchemaVersion)
	}
	if !slugRe.MatchString(slug) {
		return Task{}, fmt.Errorf("tasks: bad slug %q", slug)
	}
	if strings.TrimSpace(f.Title) == "" {
		return Task{}, fmt.Errorf("tasks: %s: title required", slug)
	}
	st := Status(strings.TrimSpace(string(f.Status)))
	if !ValidStatus(st) {
		return Task{}, fmt.Errorf("tasks: %s: bad status %q", slug, f.Status)
	}
	return Task{
		Slug:          slug,
		Title:         strings.TrimSpace(f.Title),
		Status:        st,
		Assignee:      strings.TrimSpace(f.Assignee),
		Team:          strings.TrimSpace(f.Team),
		ThreadChannel: f.ThreadChannel,
		ThreadID:      f.ThreadID,
		ChangeSets:    f.ChangeSets,
		CreatedAt:     f.CreatedAt,
		UpdatedAt:     f.UpdatedAt,
	}, nil
}

// SetAttach installs the worktree seams (cmd/dhi wiring).
func (s *Store) SetAttach(fn AttachFn, detach DetachFn) {
	s.mu.Lock()
	s.attach, s.detach = fn, detach
	s.mu.Unlock()
}

// Warnings lists malformed cards found at Open.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warns...)
}

func (s *Store) dir() string { return filepath.Join(s.ws.Root, Dir) }

func (s *Store) cardPath(slug string) string { return filepath.Join(s.dir(), slug+".toml") }

// List returns every task ordered by status flow then slug.
func (s *Store) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.tasks))
	for _, slug := range s.order {
		out = append(out, s.tasks[slug])
	}
	rank := map[Status]int{Backlog: 0, Active: 1, InReview: 2, Done: 3}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].Status], rank[out[j].Status]
		if ri != rj {
			return ri < rj
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// Get returns one task by slug.
func (s *Store) Get(slug string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[slug]
	return t, ok
}

// TasksOf lists open (not done) tasks assigned to id, status-ordered.
func (s *Store) TasksOf(id string) []Task {
	var out []Task
	for _, t := range s.List() {
		if t.Assignee == id && t.Status != Done {
			out = append(out, t)
		}
	}
	return out
}

// Create adds a card in the backlog column.
func (s *Store) Create(slug, title, assignee, team string) error {
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("tasks: bad slug %q (lowercase [a-z0-9._-])", slug)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("tasks: title required")
	}
	if assignee != "" && !validMember(assignee) {
		return fmt.Errorf("tasks: bad assignee %q", assignee)
	}

	s.mu.Lock()
	if _, exists := s.tasks[slug]; exists {
		s.mu.Unlock()
		return fmt.Errorf("tasks: %q already exists", slug)
	}
	t := Task{
		Slug: slug, Title: title, Status: Backlog,
		Assignee: assignee, Team: team,
		CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	s.mu.Unlock()

	if err := writeCard(s.cardPath(slug), t); err != nil {
		return err
	}
	s.commit(func() {
		s.tasks[slug] = t
		s.order = insertSorted(s.order, slug)
	}, Change{Kind: TaskCreated, Slug: slug})
	return nil
}

// SetStatus moves a card between kanban columns.
func (s *Store) SetStatus(slug string, st Status) error {
	if !ValidStatus(st) {
		return fmt.Errorf("tasks: bad status %q", st)
	}
	return s.mutate(slug, func(t *Task) { t.Status = st })
}

// Assign sets (or clears, "") the assignee.
func (s *Store) Assign(slug, who string) error {
	if who != "" && !validMember(who) {
		return fmt.Errorf("tasks: bad assignee %q", who)
	}
	return s.mutate(slug, func(t *Task) { t.Assignee = who })
}

// BindThread records the conversation that carries this task's progress.
func (s *Store) BindThread(slug, channel string, threadID int64) error {
	if channel != "" && !bus.ValidChannel(channel) {
		return fmt.Errorf("tasks: bad thread channel %q", channel)
	}
	return s.mutate(slug, func(t *Task) { t.ThreadChannel, t.ThreadID = channel, threadID })
}

// Attach creates a ChangeSet: worktree through the injected seam plus a
// persisted record. Re-attach of the same member updates branch/path.
func (s *Store) Attach(slug, member, branch, startpoint string) error {
	s.mu.RLock()
	_, ok := s.tasks[slug]
	fn := s.attach
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", slug)
	}
	if fn == nil {
		return fmt.Errorf("tasks: worktree seam unavailable (hermetic git not installed?)")
	}
	if member == "" || branch == "" {
		return fmt.Errorf("tasks: member and branch required")
	}
	rel, err := fn(slug, member, branch, startpoint)
	if err != nil {
		return fmt.Errorf("tasks: attach %s/%s: %w", slug, member, err)
	}
	cs := ChangeSet{Member: member, Branch: branch, Path: rel}
	return s.mutate(slug, func(t *Task) {
		replaced := false
		for i := range t.ChangeSets {
			if t.ChangeSets[i].Member == member {
				t.ChangeSets[i] = cs
				replaced = true
				break
			}
		}
		if !replaced {
			t.ChangeSets = append(t.ChangeSets, cs)
		}
	})
}

// Detach drops the changeset record and removes the worktree through
// the seam. The working-tree copy goes too — callers confirm first.
func (s *Store) Detach(slug, member string) error {
	s.mu.RLock()
	t, ok := s.tasks[slug]
	fn := s.detach
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", slug)
	}
	var target *ChangeSet
	for i := range t.ChangeSets {
		if t.ChangeSets[i].Member == member {
			target = &t.ChangeSets[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("tasks: %s has no changeset for %s", slug, member)
	}
	if fn != nil {
		if err := fn(slug, target.Path); err != nil {
			return fmt.Errorf("tasks: detach %s/%s: %w", slug, member, err)
		}
	}
	return s.mutate(slug, func(t *Task) {
		var kept []ChangeSet
		for _, cs := range t.ChangeSets {
			if cs.Member != member {
				kept = append(kept, cs)
			}
		}
		t.ChangeSets = kept
	})
}

// Remove deletes the card entirely. Worktrees recorded on it are left
// on disk unless the caller detaches them first — visible over silent
// deletion (ADR-0005 spirit).
func (s *Store) Remove(slug string) error {
	s.mu.Lock()
	if _, ok := s.tasks[slug]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("tasks: unknown task %q", slug)
	}
	order := make([]string, 0, len(s.order))
	for _, x := range s.order {
		if x != slug {
			order = append(order, x)
		}
	}
	s.mu.Unlock()

	if err := os.Remove(s.cardPath(slug)); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.commit(func() {
		delete(s.tasks, slug)
		s.order = order
	}, Change{Kind: TaskRemoved, Slug: slug})
	return nil
}

// mutate loads → applies → persists → commits, keeping disk ahead of
// memory like the other registries.
func (s *Store) mutate(slug string, apply func(*Task)) error {
	s.mu.Lock()
	t, ok := s.tasks[slug]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", slug)
	}
	apply(&t)
	t.UpdatedAt = s.now()
	if err := writeCard(s.cardPath(slug), t); err != nil {
		return err
	}
	s.commit(func() { s.tasks[slug] = t }, Change{Kind: TaskUpdated, Slug: slug})
	return nil
}

func writeCard(path string, t Task) error {
	f := file{
		Schema: SchemaVersion, Title: t.Title, Status: t.Status,
		Assignee: t.Assignee, Team: t.Team,
		ThreadChannel: t.ThreadChannel, ThreadID: t.ThreadID,
		ChangeSets: t.ChangeSets,
		CreatedAt:  t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("tasks: write: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".task-*.toml")
	if err != nil {
		return fmt.Errorf("tasks: write: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(f); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("tasks: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("tasks: write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("tasks: write: %w", err)
	}
	return nil
}

func validMember(who string) bool {
	return who == "you" || slugRe.MatchString(who)
}

func insertSorted(sorted []string, v string) []string {
	i := sort.SearchStrings(sorted, v)
	return append(sorted[:i:i], append([]string{v}, sorted[i:]...)...)
}

// commit applies persisted state under lock and fans out the change.
func (s *Store) commit(apply func(), c Change) {
	s.mu.Lock()
	apply()
	targets := make([]chan Change, 0, len(s.subs))
	for _, sub := range s.subs {
		targets = append(targets, sub)
	}
	s.mu.Unlock()
	for _, sub := range targets {
		select {
		case sub <- c:
		default:
		}
	}
}

// Subscribe receives subsequent task changes until cancel runs.
func (s *Store) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 8)
	s.mu.Lock()
	s.subSeq++
	id := s.subSeq
	s.subs[id] = ch
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

// cardPathForTest exposes the on-disk path for tests only.
func (s *Store) cardPathForTest(slug string) string { return s.cardPath(slug) }
