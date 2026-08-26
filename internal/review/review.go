// Package review is DHI's code-review service (F-005): one TOML card per
// review session under the reserved .dhi/reviews/ tree, GitHub-style
// comment threads anchored to diff lines, per-file viewed marks and
// pending-review batching. Review worktrees are created through an
// injectable seam so the store stays hermetic in tests; PR metadata and
// posting flow through a narrow gh interface.
package review

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

// SchemaVersion is the review-card schema this build understands.
const SchemaVersion = 1

// Dir is the reserved reviews tree under the workspace root.
const Dir = ".dhi/reviews"

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Kind enumerates review inputs.
type Kind string

// Review input kinds.
const (
	KindBranch   Kind = "branch"   // base...ref inside the member repo
	KindWorktree Kind = "worktree" // an existing checked-out branch/worktree
	KindPR       Kind = "pr"       // gh-backed pull request
)

// ValidKind reports whether k names an input kind.
func ValidKind(k Kind) bool {
	switch k {
	case KindBranch, KindWorktree, KindPR:
		return true
	}
	return false
}

// Status tracks submission state of a review session.
type Status string

// Review statuses.
const (
	Pending   Status = "pending"
	Submitted Status = "submitted"
)

// Target identifies what is being reviewed.
type Target struct {
	Kind     Kind
	Member   string
	Base     string // merge-base side: branch name or sha
	Head     string // branch name or resolved sha ("" until started for PRs)
	PRNumber int    // set when Kind == pr
}

// Side anchors a thread to one parent of the diff.
type Side string

// Thread sides.
const (
	SideNew Side = "new"
	SideOld Side = "old"
)

// Comment is one authored note in a thread. Pending comments form the
// unsubmitted batch; Submit flips them all at once.
type Comment struct {
	Author  string
	Text    string
	At      time.Time
	Pending bool
}

// Thread is a discussion rooted at one file line (Line 0 = file level).
// BusThread links the thread to its conversation root on the message bus
// so agent replies can be mirrored back (0 = not yet invited).
type Thread struct {
	ID        int64
	File      string // display path (new side)
	Line      int    // new-side 1-based line; 0 = whole file
	Side      Side
	Resolved  bool
	BusThread int64
	Comments  []Comment
}

// Review is one session card.
type Review struct {
	ID      string
	Title   string
	Target  Target
	Status  Status
	Viewed  map[string]bool // display path → reviewed
	Threads []Thread
	Channel string // bus channel carrying this review's conversation
	WorkRel string // review worktree path relative to workspace root ("")
	Done    bool   // worktree discarded
	Posted  bool   // comments posted to the PR via gh

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PendingCount counts draft comments across threads.
func (r Review) PendingCount() int {
	n := 0
	for _, t := range r.Threads {
		for _, c := range t.Comments {
			if c.Pending {
				n++
			}
		}
	}
	return n
}

// file is the on-disk TOML shape.
type file struct {
	Schema    int       `toml:"schema"`
	Title     string    `toml:"title"`
	Status    Status    `toml:"status"`
	Kind      Kind      `toml:"kind"`
	Member    string    `toml:"member"`
	Base      string    `toml:"base"`
	Head      string    `toml:"head"`
	PRNumber  int       `toml:"pr_number,omitempty"`
	Channel   string    `toml:"channel"`
	WorkRel   string    `toml:"worktree"`
	Posted    bool      `toml:"posted"`
	Viewed    []string  `toml:"viewed"`
	Done      bool      `toml:"done"`
	CreatedAt time.Time `toml:"created_at"`
	UpdatedAt time.Time `toml:"updated_at"`

	Threads []threadFile `toml:"thread"`
}

type threadFile struct {
	ID        int64         `toml:"id"`
	File      string        `toml:"file"`
	Line      int           `toml:"line"`
	Side      Side          `toml:"side"`
	Resolved  bool          `toml:"resolved"`
	BusThread int64         `toml:"bus_thread,omitempty"`
	Comments  []commentFile `toml:"comment"`
}

type commentFile struct {
	Author  string    `toml:"author"`
	Text    string    `toml:"text"`
	At      time.Time `toml:"at"`
	Pending bool      `toml:"pending"`
}

// WorktreeFn creates the dedicated review worktree for one member and
// returns its path relative to the workspace root. Production wires
// gitcore.Runner; tests fake it. A nil seam disables starting reviews.
type WorktreeFn func(reviewID, member, startpoint string) (relPath string, err error)

// DiscardFn removes a previously created review worktree.
type DiscardFn func(reviewID, relPath string) error

// Store is the loaded review set; safe for concurrent use.
type Store struct {
	ws *workspace.Workspace

	mu      sync.RWMutex
	items   map[string]Review
	order   []string
	warns   []string
	subs    map[int]chan Change
	subSeq  int
	work    WorktreeFn
	discard DiscardFn
	now     func() time.Time
}

// Change announces one committed review mutation.
type Change struct {
	Kind ChangeKind
	ID   string
}

type ChangeKind string

// Change kinds.
const (
	ReviewCreated ChangeKind = "created"
	ReviewUpdated ChangeKind = "updated"
	ReviewRemoved ChangeKind = "removed"
)

// Open loads every *.toml under .dhi/reviews/. Missing dir = empty store;
// malformed cards are skipped and reported via Warnings (doctor).
func Open(ws *workspace.Workspace) (*Store, error) {
	s := &Store{
		ws:    ws,
		items: map[string]Review{},
		subs:  map[int]chan Change{},
		now:   time.Now,
	}
	dir := s.dir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("review: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".toml")
		r, perr := parseCard(filepath.Join(dir, e.Name()), id)
		if perr != nil {
			s.warns = append(s.warns, perr.Error())
			continue
		}
		s.items[id] = r
		s.order = append(s.order, id)
	}
	sort.Strings(s.order)
	return s, nil
}

func parseCard(path, id string) (Review, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Review{}, fmt.Errorf("review: read %s: %w", id, err)
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Review{}, fmt.Errorf("review: %s: %w", id, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		return Review{}, fmt.Errorf("review: %s: unknown key(s): %s", id, strings.Join(keys, ", "))
	}
	if f.Schema != SchemaVersion {
		return Review{}, fmt.Errorf("review: %s: schema %d, want %d", id, f.Schema, SchemaVersion)
	}
	if !slugRe.MatchString(id) {
		return Review{}, fmt.Errorf("review: bad id %q", id)
	}
	if !ValidKind(f.Kind) {
		return Review{}, fmt.Errorf("review: %s: bad kind %q", id, f.Kind)
	}
	if f.Member == "" || f.Base == "" {
		return Review{}, fmt.Errorf("review: %s: member and base required", id)
	}
	st := Status(strings.TrimSpace(string(f.Status)))
	if st != Pending && st != Submitted {
		return Review{}, fmt.Errorf("review: %s: bad status %q", id, f.Status)
	}
	if f.Channel != "" && !bus.ValidChannel(f.Channel) {
		return Review{}, fmt.Errorf("review: %s: bad channel %q", id, f.Channel)
	}
	r := Review{
		ID: id, Title: strings.TrimSpace(f.Title),
		Target: Target{Kind: f.Kind, Member: f.Member, Base: f.Base, Head: f.Head, PRNumber: f.PRNumber},
		Status: st, Viewed: map[string]bool{}, Channel: f.Channel, WorkRel: f.WorkRel, Done: f.Done,
		Posted:    f.Posted,
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}
	for _, vf := range f.Viewed {
		r.Viewed[vf] = true
	}
	for _, tf := range f.Threads {
		t := Thread{ID: tf.ID, File: tf.File, Line: tf.Line, Side: tf.Side,
			Resolved: tf.Resolved, BusThread: tf.BusThread}
		for _, cf := range tf.Comments {
			t.Comments = append(t.Comments, Comment{Author: cf.Author, Text: cf.Text, At: cf.At, Pending: cf.Pending})
		}
		r.Threads = append(r.Threads, t)
	}
	return r, nil
}

// SetWorktreeSeam installs the worktree seams (cmd/dhi wiring).
func (s *Store) SetWorktreeSeam(work WorktreeFn, discard DiscardFn) {
	s.mu.Lock()
	s.work, s.discard = work, discard
	s.mu.Unlock()
}

// seams reads the installed worktree functions.
func (s *Store) seams() (WorktreeFn, DiscardFn) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.work, s.discard
}

// Warnings lists malformed cards found at Open.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warns...)
}

func (s *Store) dir() string { return filepath.Join(s.ws.Root, Dir) }

func (s *Store) cardPath(id string) string { return filepath.Join(s.dir(), id+".toml") }

// List returns every review, newest first.
func (s *Store) List() []Review {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Review, 0, len(s.items))
	for _, id := range s.order {
		out = append(out, s.items[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Get returns one review by id.
func (s *Store) Get(id string) (Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.items[id]
	return r, ok
}

// Create persists a fully-formed review card (service assembles it).
func (s *Store) Create(r Review) error {
	if !slugRe.MatchString(r.ID) {
		return fmt.Errorf("review: bad id %q (lowercase [a-z0-9._-])", r.ID)
	}
	s.mu.Lock()
	if _, exists := s.items[r.ID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("review: %q already exists", r.ID)
	}
	s.mu.Unlock()
	if err := writeCard(s.cardPath(r.ID), r); err != nil {
		return err
	}
	s.commit(func() {
		s.items[r.ID] = r
		s.order = insertSorted(s.order, r.ID)
	}, Change{Kind: ReviewCreated, ID: r.ID})
	return nil
}

// AddThread appends a discussion thread to the review and returns its id.
func (s *Store) AddThread(id string, t Thread) (int64, error) {
	var out int64
	err := s.mutate(id, func(r *Review) {
		out = r.nextThreadID()
		t.ID = out
		r.Threads = append(r.Threads, t)
	})
	return out, err
}

// hasThread reports whether the in-memory review carries threadID.
func hasThread(r Review, threadID int64) bool {
	for _, t := range r.Threads {
		if t.ID == threadID {
			return true
		}
	}
	return false
}

// AppendComment adds a note to an existing thread.
func (s *Store) AppendComment(id string, threadID int64, c Comment) error {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	if !hasThread(r, threadID) {
		return fmt.Errorf("review: %s: unknown thread %d", id, threadID)
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID == threadID {
				r.Threads[i].Comments = append(r.Threads[i].Comments, c)
				return
			}
		}
	})
}

// SetResolved toggles a thread's resolve state.
func (s *Store) SetResolved(id string, threadID int64, resolved bool) error {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	if !hasThread(r, threadID) {
		return fmt.Errorf("review: %s: unknown thread %d", id, threadID)
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID == threadID {
				r.Threads[i].Resolved = resolved
				return
			}
		}
	})
}

// SetBusThread records the bus conversation root for a thread so agent
// replies can be mirrored into it later.
func (s *Store) SetBusThread(id string, threadID, busRoot int64) error {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	if !hasThread(r, threadID) {
		return fmt.Errorf("review: %s: unknown thread %d", id, threadID)
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID == threadID {
				r.Threads[i].BusThread = busRoot
				return
			}
		}
	})
}

// DeleteComment removes one pending comment by index within its thread.
func (s *Store) DeleteComment(id string, threadID int64, idx int) error {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	if err := checkPending(r, threadID, idx); err != nil {
		return err
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID != threadID {
				continue
			}
			c := r.Threads[i].Comments
			r.Threads[i].Comments = append(c[:idx:idx], c[idx+1:]...)
			return
		}
	})
}

// EditComment rewrites one pending comment's text.
func (s *Store) EditComment(id string, threadID int64, idx int, text string) error {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	if err := checkPending(r, threadID, idx); err != nil {
		return err
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID == threadID {
				r.Threads[i].Comments[idx].Text = strings.TrimSpace(text)
				return
			}
		}
	})
}

// checkPending validates thread/comment indexes and that the target is
// still an unsubmitted draft.
func checkPending(r Review, threadID int64, idx int) error {
	for _, t := range r.Threads {
		if t.ID != threadID {
			continue
		}
		if idx < 0 || idx >= len(t.Comments) {
			return fmt.Errorf("review: %s: comment %d missing in thread %d", r.ID, idx, threadID)
		}
		if !t.Comments[idx].Pending {
			return fmt.Errorf("review: %s: comment %d in thread %d already submitted", r.ID, idx, threadID)
		}
		return nil
	}
	return fmt.Errorf("review: %s: unknown thread %d", r.ID, threadID)
}

// ToggleViewed flips a file's viewed mark.
func (s *Store) ToggleViewed(id, path string) error {
	return s.mutate(id, func(r *Review) {
		if r.Viewed == nil {
			r.Viewed = map[string]bool{}
		}
		r.Viewed[path] = !r.Viewed[path]
	})
}

// Submit flips pending comments to submitted and marks the review done.
func (s *Store) Submit(id string) error {
	return s.mutate(id, func(r *Review) {
		for ti := range r.Threads {
			for ci := range r.Threads[ti].Comments {
				r.Threads[ti].Comments[ci].Pending = false
			}
		}
		r.Status = Submitted
	})
}

// MarkPosted records that the review's comments were posted to the PR.
func (s *Store) MarkPosted(id string) error {
	return s.mutate(id, func(r *Review) { r.Posted = true })
}

// Remove deletes the card entirely. The recorded worktree is left on disk
// unless the caller discards it first — visible over silent deletion.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	if _, ok := s.items[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("review: unknown review %q", id)
	}
	order := make([]string, 0, len(s.order))
	for _, x := range s.order {
		if x != id {
			order = append(order, x)
		}
	}
	s.mu.Unlock()

	if err := os.Remove(s.cardPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.commit(func() {
		delete(s.items, id)
		s.order = order
	}, Change{Kind: ReviewRemoved, ID: id})
	return nil
}

func (r Review) nextThreadID() int64 {
	var max int64
	for _, t := range r.Threads {
		if t.ID > max {
			max = t.ID
		}
	}
	return max + 1
}

// mutate loads → applies → persists → commits, keeping disk ahead of
// memory like the other registries.
func (s *Store) mutate(id string, apply func(*Review)) error {
	s.mu.Lock()
	r, ok := s.items[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	apply(&r)
	r.UpdatedAt = s.now()
	if err := writeCard(s.cardPath(id), r); err != nil {
		return err
	}
	s.commit(func() { s.items[id] = r }, Change{Kind: ReviewUpdated, ID: id})
	return nil
}

func writeCard(path string, r Review) error {
	f := file{
		Schema: SchemaVersion, Title: r.Title, Status: r.Status,
		Kind: r.Target.Kind, Member: r.Target.Member, Base: r.Target.Base,
		Head: r.Target.Head, PRNumber: r.Target.PRNumber,
		Channel: r.Channel, WorkRel: r.WorkRel, Done: r.Done, Posted: r.Posted,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for p := range r.Viewed {
		if r.Viewed[p] {
			f.Viewed = append(f.Viewed, p)
		}
	}
	sort.Strings(f.Viewed)
	for _, t := range r.Threads {
		tf := threadFile{ID: t.ID, File: t.File, Line: t.Line, Side: t.Side,
			Resolved: t.Resolved, BusThread: t.BusThread}
		for _, c := range t.Comments {
			tf.Comments = append(tf.Comments, commentFile{Author: c.Author, Text: c.Text, At: c.At, Pending: c.Pending})
		}
		f.Threads = append(f.Threads, tf)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("review: write: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".review-*.toml")
	if err != nil {
		return fmt.Errorf("review: write: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(f); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("review: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("review: write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("review: write: %w", err)
	}
	return nil
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

// Subscribe receives subsequent review changes until cancel runs.
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
