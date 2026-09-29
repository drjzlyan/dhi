// Package tasks is DHI's task tracker (F-003 component 3): one TOML
// card per task under the reserved .dhi/tasks/ tree, kanban statuses,
// and ChangeSets — per-member linked worktrees created through an
// injectable attach seam so the store stays hermetic in tests and works
// pre-registry-flip (attaching reports a visible error until the
// hermetic git shim exists).
package tasks

import (
	"context"
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
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// SchemaVersion is the task-card schema this build understands. Schema 2
// adds labels/priority/epic/due (F-035 board depth); schema-1 cards load
// unchanged with those fields unset.
const SchemaVersion = 2

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

// Priority is a card's urgency (F-035). "" means unset (rendered as
// normal); the ordering is low → urgent.
type Priority string

// Priorities.
const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// Priorities is the canonical order for pickers.
var Priorities = []Priority{PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent}

// ValidPriority reports whether p is a defined priority ("" allowed).
func ValidPriority(p Priority) bool {
	if p == "" {
		return true
	}
	for _, v := range Priorities {
		if v == p {
			return true
		}
	}
	return false
}

// PriorityRank orders priorities for sorting (unset sorts as normal).
func PriorityRank(p Priority) int {
	switch p {
	case PriorityLow:
		return 0
	case PriorityUrgent:
		return 3
	case PriorityHigh:
		return 2
	case PriorityNormal:
		return 1
	default:
		return 1 // unset = normal
	}
}

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

// Bypass is one approved workflow exception: the step that was skipped
// (or cleared by exception) and the reason the human gave.
type Bypass struct {
	Step   string    `toml:"step"`
	Reason string    `toml:"reason"`
	At     time.Time `toml:"at"`
}

// Propagation decisions (F-032). A proposal is pending until a human
// accepts (creating a linked task) or declines (creating nothing).
const (
	PropPending  = "pending"
	PropAccepted = "accepted"
	PropDeclined = "declined"
)

// Propagation is one cross-project proposal: changing FromMember may
// affect ToMember through Kind. Decision is pending|accepted|declined;
// CreatedSlug names the accepted task, if any.
type Propagation struct {
	FromMember  string    `toml:"from_member"`
	ToMember    string    `toml:"to_member"`
	Kind        string    `toml:"kind"`
	Decision    string    `toml:"decision"`
	CreatedSlug string    `toml:"created_slug,omitempty"`
	At          time.Time `toml:"at"`
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

	// Board metadata (F-035): labels, priority, an epic/grouping name,
	// and a due date (YYYY-MM-DD; "" = none).
	Labels   []string
	Priority Priority
	Epic     string
	Due      string

	ThreadChannel string
	ThreadID      int64

	ChangeSets []ChangeSet
	Runs       []Run // turn history; append-only (F-013)

	// Workflow is the active feature workflow slug (F-031), and TestsPass
	// records that the declared test command last passed — the durable
	// signals the workflow gates read (tests-before-PR).
	Workflow  string
	TestsPass bool

	// Bypasses records explicit, approved workflow exceptions (F-031):
	// each names the step skipped and why. A bypass is never silent —
	// it exists only because a human approved it through the queue.
	Bypasses []Bypass

	// Propagations records cross-project proposals (F-032): when this
	// task touches member A and a declared edge A→B exists, a pending
	// proposal appears for B. Nothing is created until accepted.
	Propagations []Propagation

	PRNumber int    // GitHub PR created from this card's branch (0 = none)
	PRURL    string // PR URL once created

	CreatedAt time.Time
	UpdatedAt time.Time
}

// RunStatus classifies a recorded run.
type RunStatus string

// Run statuses.
const (
	RunOK      RunStatus = "ok"
	RunError   RunStatus = "error"
	RunTimeout RunStatus = "timeout"
)

// ValidRunStatus reports whether s is a recorded run status.
func ValidRunStatus(s RunStatus) bool {
	switch s {
	case RunOK, RunError, RunTimeout:
		return true
	}
	return false
}

// Run is one recorded execution of the task by an agent (F-013): the
// turn history that outlives the conversation thread so the card shows
// accumulated effort. Records are append-only — the runtime's finalize
// writes them once; editing or removing them is not supported.
type Run struct {
	ID       string    `toml:"id"` // "run-<stamp>"; generated by the caller
	Agent    string    `toml:"agent"`
	Runtime  string    `toml:"runtime"`         // "cli:<name>" (F-014 uniform schema; ADR-0013: always a CLI)
	Model    string    `toml:"model,omitempty"` // manifest model, when one is set
	Attempt  int       `toml:"attempt"`         // 0-based; >0 means a retry after failure (F-013 step 7)
	Started  time.Time `toml:"started"`
	Finished time.Time `toml:"finished"`
	Status   RunStatus `toml:"status"`
	Exit     int       `toml:"exit,omitempty"` // CLI process exit code (0 = success, 0 on timeout/cancel)
	Summary  string    `toml:"summary"`
	Error    string    `toml:"error,omitempty"` // failure detail on error/timeout runs

	// Transcript is the path to the persisted event JSONL under
	// .dhi/agents/<id>/runs/ (F-013 step 4); empty when the run could
	// not persist one (best-effort).
	Transcript string `toml:"transcript,omitempty"`

	// Token/cost accounting; Tokens* are -1 when unknown, never 0
	// (0 would claim a zero-token run). HasCost is the declared
	// cost:false marker: false = the CLI reported no US-dollar cost,
	// and cost_usd 0 must never be read as a price (F-014 §Part A).
	TokensIn  int     `toml:"tokens_in"`
	TokensOut int     `toml:"tokens_out"`
	CostUSD   float64 `toml:"cost_usd"`
	HasCost   bool    `toml:"cost"`
}

// file is the on-disk TOML shape.
type file struct {
	Schema        int           `toml:"schema"`
	Title         string        `toml:"title"`
	Status        Status        `toml:"status"`
	Assignee      string        `toml:"assignee"`
	Team          string        `toml:"team"`
	Labels        []string      `toml:"labels,omitempty"`
	Priority      Priority      `toml:"priority,omitempty"`
	Epic          string        `toml:"epic,omitempty"`
	Due           string        `toml:"due,omitempty"`
	ThreadChannel string        `toml:"thread_channel"`
	ThreadID      int64         `toml:"thread_id"`
	ChangeSets    []ChangeSet   `toml:"changeset"`
	Runs          []Run         `toml:"run"`
	Workflow      string        `toml:"workflow,omitempty"`
	TestsPass     bool          `toml:"tests_pass,omitempty"`
	Bypasses      []Bypass      `toml:"bypass,omitempty"`
	Propagations  []Propagation `toml:"propagation,omitempty"`
	PRNumber      int           `toml:"pr_number,omitempty"`
	PRURL         string        `toml:"pr_url,omitempty"`
	CreatedAt     time.Time     `toml:"created_at"`
	UpdatedAt     time.Time     `toml:"updated_at"`
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
	// identity resolves the user's git identity for commits (F-029); nil
	// refuses by name.
	identity gitcore.IdentityFunc
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
	if f.Schema != SchemaVersion && f.Schema != 1 {
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
	priority := Priority(strings.TrimSpace(string(f.Priority)))
	if !ValidPriority(priority) {
		return Task{}, fmt.Errorf("tasks: %s: bad priority %q", slug, f.Priority)
	}
	due := strings.TrimSpace(f.Due)
	if due != "" {
		if _, derr := time.Parse("2006-01-02", due); derr != nil {
			return Task{}, fmt.Errorf("tasks: %s: due %q must be YYYY-MM-DD", slug, due)
		}
	}
	for i, r := range f.Runs {
		if perr := validateRun(r); perr != nil {
			return Task{}, fmt.Errorf("tasks: %s: run %d: %w", slug, i, perr)
		}
	}
	for i, p := range f.Propagations {
		if p.FromMember == "" || p.ToMember == "" {
			return Task{}, fmt.Errorf("tasks: %s: propagation %d needs from/to members", slug, i)
		}
		switch p.Decision {
		case PropPending, PropAccepted, PropDeclined:
		default:
			return Task{}, fmt.Errorf("tasks: %s: propagation %d bad decision %q", slug, i, p.Decision)
		}
	}
	return Task{
		Slug:          slug,
		Title:         strings.TrimSpace(f.Title),
		Status:        st,
		Assignee:      strings.TrimSpace(f.Assignee),
		Team:          strings.TrimSpace(f.Team),
		Labels:        NormalizeLabels(f.Labels),
		Priority:      priority,
		Epic:          strings.TrimSpace(f.Epic),
		Due:           due,
		ThreadChannel: f.ThreadChannel,
		ThreadID:      f.ThreadID,
		ChangeSets:    f.ChangeSets,
		Runs:          f.Runs,
		Workflow:      f.Workflow,
		TestsPass:     f.TestsPass,
		Bypasses:      f.Bypasses,
		Propagations:  f.Propagations,
		PRNumber:      f.PRNumber,
		PRURL:         f.PRURL,
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

// SetWorkflow records the task's active feature workflow slug (F-031).
func (s *Store) SetWorkflow(slug, wf string) error {
	return s.mutate(slug, func(t *Task) { t.Workflow = wf })
}

// SetTestsPass records whether the task's declared test command passed
// (F-031 tests-before-PR gate).
func (s *Store) SetTestsPass(slug string, ok bool) error {
	return s.mutate(slug, func(t *Task) { t.TestsPass = ok })
}

// SetLabels replaces a card's labels (F-035).
func (s *Store) SetLabels(slug string, labels []string) error {
	clean := NormalizeLabels(labels)
	return s.mutate(slug, func(t *Task) { t.Labels = clean })
}

// SetPriority sets a card's priority ("" clears it).
func (s *Store) SetPriority(slug string, p Priority) error {
	p = Priority(strings.TrimSpace(string(p)))
	if !ValidPriority(p) {
		return fmt.Errorf("tasks: bad priority %q", p)
	}
	return s.mutate(slug, func(t *Task) { t.Priority = p })
}

// SetEpic sets a card's epic/grouping name ("" clears it).
func (s *Store) SetEpic(slug, epic string) error {
	return s.mutate(slug, func(t *Task) { t.Epic = strings.TrimSpace(epic) })
}

// SetDue sets a card's due date (YYYY-MM-DD; "" clears it).
func (s *Store) SetDue(slug, due string) error {
	due = strings.TrimSpace(due)
	if due != "" {
		if _, err := time.Parse("2006-01-02", due); err != nil {
			return fmt.Errorf("tasks: due %q must be YYYY-MM-DD", due)
		}
	}
	return s.mutate(slug, func(t *Task) { t.Due = due })
}

// NormalizeLabels trims, lowercases, dedupes and sorts labels so the
// filter and rendering are deterministic.
func NormalizeLabels(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// RecordBypass appends an approved workflow exception (F-031). A step
// and a reason are required — a silent bypass is not representable.
func (s *Store) RecordBypass(slug, step, reason string) error {
	step = strings.TrimSpace(step)
	reason = strings.TrimSpace(reason)
	if step == "" || reason == "" {
		return fmt.Errorf("tasks: bypass needs a step and a reason")
	}
	return s.mutate(slug, func(t *Task) {
		t.Bypasses = append(t.Bypasses, Bypass{Step: step, Reason: reason, At: time.Now().UTC()})
	})
}

// SeedPropagations records pending cross-project proposals (F-032) for
// edges from this task's changed members. An existing (from,to,kind)
// entry is left untouched, so re-seeding never duplicates or resurrects
// a decided proposal.
func (s *Store) SeedPropagations(slug string, seeds []Propagation) error {
	if len(seeds) == 0 {
		return nil
	}
	return s.mutate(slug, func(t *Task) {
		for _, seed := range seeds {
			if seed.FromMember == "" || seed.ToMember == "" {
				continue
			}
			dup := false
			for _, p := range t.Propagations {
				if p.FromMember == seed.FromMember && p.ToMember == seed.ToMember && p.Kind == seed.Kind {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			t.Propagations = append(t.Propagations, Propagation{
				FromMember: seed.FromMember, ToMember: seed.ToMember, Kind: seed.Kind,
				Decision: PropPending, At: time.Now().UTC(),
			})
		}
	})
}

// DecidePropagation resolves the pending proposal to toMember: accepted
// (naming the created task) or declined (creating nothing).
func (s *Store) DecidePropagation(slug, toMember, decision, createdSlug string) error {
	if decision != PropAccepted && decision != PropDeclined {
		return fmt.Errorf("tasks: decision must be %s or %s", PropAccepted, PropDeclined)
	}
	found := false
	err := s.mutate(slug, func(t *Task) {
		for i := range t.Propagations {
			p := &t.Propagations[i]
			if p.ToMember == toMember && p.Decision == PropPending {
				p.Decision = decision
				p.CreatedSlug = createdSlug
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("tasks: %s has no pending proposal to %q", slug, toMember)
	}
	return nil
}

// PendingPropagations returns the unresolved proposals on a task.
func (t Task) PendingPropagations() []Propagation {
	var out []Propagation
	for _, p := range t.Propagations {
		if p.Decision == PropPending {
			out = append(out, p)
		}
	}
	return out
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

// SetPR records the GitHub PR created from this card's branch.
func (s *Store) SetPR(slug string, number int, url string) error {
	if slug == "" {
		return fmt.Errorf("tasks: slug required")
	}
	return s.mutate(slug, func(t *Task) { t.PRNumber, t.PRURL = number, url })
}

// RecordChangeSet persists a changeset record for an externally-created
// worktree (e.g. the Reviewer binding its own worktree to a fix task).
// Re-recording the same member replaces the entry, like Attach.
func (s *Store) RecordChangeSet(slug string, cs ChangeSet) error {
	if cs.Member == "" || cs.Path == "" {
		return fmt.Errorf("tasks: changeset needs member and path")
	}
	return s.mutate(slug, func(t *Task) {
		for i := range t.ChangeSets {
			if t.ChangeSets[i].Member == cs.Member {
				t.ChangeSets[i] = cs
				return
			}
		}
		t.ChangeSets = append(t.ChangeSets, cs)
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

// validateRun enforces the F-014 run schema strictly, like every other
// decode in the repo: unknown status values refuse with the value named,
// and the round-trip-required fields (id, agent, started/finished) must
// be present. RecordRun uses the same rules for writes.
func validateRun(r Run) error {
	if r.ID == "" {
		return fmt.Errorf("run id required")
	}
	if r.Agent == "" {
		return fmt.Errorf("run %s: agent required", r.ID)
	}
	if r.Started.IsZero() || r.Finished.IsZero() {
		return fmt.Errorf("run %s: needs started and finished", r.ID)
	}
	if !ValidRunStatus(r.Status) {
		return fmt.Errorf("run %s: bad status %q", r.ID, r.Status)
	}
	if r.TokensIn < -1 || r.TokensOut < -1 {
		return fmt.Errorf("run %s: token counts must be -1 (unknown) or >= 0", r.ID)
	}
	return nil
}

// RecordRun appends one run record to the card (F-013). Records are
// append-only history: the runtime writes once on finalize. Required
// fields: a unique ID, the agent id, a started/finished window, and a
// valid status.
func (s *Store) RecordRun(slug string, r Run) error {
	if err := validateRun(r); err != nil {
		return fmt.Errorf("tasks: %w", err)
	}
	return s.mutate(slug, func(t *Task) {
		t.Runs = append(t.Runs, r)
	})
}

// FindByThread resolves the task bound to a conversation thread, if any.
func (s *Store) FindByThread(channel string, threadID int64) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.ThreadChannel == channel && t.ThreadID == threadID {
			return t, true
		}
	}
	return Task{}, false
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
		Labels: t.Labels, Priority: t.Priority, Epic: t.Epic, Due: t.Due,
		ThreadChannel: t.ThreadChannel, ThreadID: t.ThreadID,
		PRNumber: t.PRNumber, PRURL: t.PRURL,
		ChangeSets: t.ChangeSets,
		Runs:       t.Runs,
		Workflow:   t.Workflow, TestsPass: t.TestsPass,
		Bypasses: t.Bypasses, Propagations: t.Propagations,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
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

// SetIdentity installs the git identity resolver used by Commit (F-029).
// A nil resolver (the default) makes Commit refuse by name.
func (s *Store) SetIdentity(fn gitcore.IdentityFunc) {
	s.mu.Lock()
	s.identity = fn
	s.mu.Unlock()
}

// Commit stages all changes and creates a commit in EVERY changeset
// worktree (F-032: cross-project work is one piece of work). Each member
// commits on its own branch; a per-member failure is named, never
// silently skipped — the members that did commit stay committed.
// Commits are authored by the user's resolved git identity (F-029).
func (s *Store) Commit(slug, message string) error {
	if message == "" {
		return fmt.Errorf("tasks: commit message required")
	}
	s.mu.RLock()
	t, ok := s.tasks[slug]
	attach := s.attach
	identity := s.identity
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", slug)
	}
	if len(t.ChangeSets) == 0 {
		return fmt.Errorf("tasks: %s has no worktrees", slug)
	}
	if attach == nil {
		return fmt.Errorf("tasks: worktree seam unavailable")
	}
	if identity == nil {
		return fmt.Errorf("tasks: commit: %w", gitcore.ErrIdentityUnset)
	}
	id, err := identity(context.Background())
	if err != nil {
		return fmt.Errorf("tasks: commit: %w", err)
	}
	var failures []string
	for _, cs := range t.ChangeSets {
		absWorkdir := filepath.Join(s.ws.Root, cs.Path)
		repo, err := gitcore.Open(absWorkdir)
		if err != nil {
			failures = append(failures, cs.Member+": open repo: "+err.Error())
			continue
		}
		if err := repo.Stage("."); err != nil {
			failures = append(failures, cs.Member+": stage: "+err.Error())
			continue
		}
		if _, err := repo.Commit(gitcore.CommitOptions{Message: message, Author: id.Name, Email: id.Email}); err != nil {
			failures = append(failures, cs.Member+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("tasks: commit failed for %s", strings.Join(failures, "; "))
	}
	return nil
}

// PushBranch pushes every changeset's branch to origin, naming any member
// that fails rather than stopping silently.
func (s *Store) PushBranch(slug string) error {
	s.mu.RLock()
	t, ok := s.tasks[slug]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", slug)
	}
	if len(t.ChangeSets) == 0 {
		return fmt.Errorf("tasks: %s has no worktrees", slug)
	}
	var failures []string
	for _, cs := range t.ChangeSets {
		absWorkdir := filepath.Join(s.ws.Root, cs.Path)
		repo, err := gitcore.Open(absWorkdir)
		if err != nil {
			failures = append(failures, cs.Member+": open repo: "+err.Error())
			continue
		}
		refspec := "refs/heads/" + cs.Branch + ":refs/heads/" + cs.Branch
		if err := repo.Push(context.Background(), "", refspec, nil); err != nil {
			failures = append(failures, cs.Member+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("tasks: push failed for %s", strings.Join(failures, "; "))
	}
	return nil
}

// cardPathForTest exposes the on-disk path for tests only.
func (s *Store) cardPathForTest(slug string) string { return s.cardPath(slug) }
