// Package ideation models F-004 ideation sessions: a named chat with an
// invited agent set plus a read-only artifact folder. One TOML card per
// session lives under .dhi/sessions/<slug>.toml; agent-produced artifacts
// live under .dhi/sessions/<slug>/ and are addressed as vpaths
// ".dhi/sessions/<slug>/<rel-path>". Artifact status (draft → reviewed →
// approved/rejected) is recorded per file and keyed to a content hash so
// a fresh agent revision flips the artifact back to draft. Approval is
// human-only; the Ideator surface routes rejection notes back to the
// authoring agent over the session channel.
//
// The store follows the tasks/review blueprint: strict decode, atomic
// persist-before-commit, Subscribe pings, malformed cards surface as
// warnings instead of failing the load.
package ideation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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

// SchemaVersion is the session card schema this build understands.
const SchemaVersion = 1

// Dir is the reserved tree holding session cards and artifact folders.
const Dir = workspace.DirSessions

// slugRe constrains session ids (and thus channel names + folder names).
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ArtifactStatus is the review state of one artifact file.
type ArtifactStatus string

// Artifact lifecycle. Approved artifacts are final; a rejected artifact
// carries revision notes until the author replaces the file, which flips
// it back to draft.
const (
	StatusDraft    ArtifactStatus = "draft"
	StatusReviewed ArtifactStatus = "reviewed"
	StatusApproved ArtifactStatus = "approved"
	StatusRejected ArtifactStatus = "rejected"
)

// Valid reports whether s is one of the defined artifact statuses.
func (s ArtifactStatus) Valid() bool {
	switch s {
	case StatusDraft, StatusReviewed, StatusApproved, StatusRejected:
		return true
	}
	return false
}

// Artifact records one agent-produced file inside a session folder.
type Artifact struct {
	Path      string // slash-separated, relative to the session folder
	Author    string // agent id; "" = unknown/external
	Status    ArtifactStatus
	Notes     string // revision notes recorded on rejection
	Hash      string // short content hash at last scan
	UpdatedAt time.Time
}

// Session is one ideation session.
type Session struct {
	ID        string   // slug; names the card and the artifact folder
	Name      string   // display name
	Topic     string   // one-line topic for the session
	Agents    []string // invited agent ids, sorted
	Channel   string   // bus channel, "#ideation-<id>"
	CreatedAt time.Time
	UpdatedAt time.Time
	Artifacts []Artifact // sorted by Path
}

// change kinds fanned out to subscribers.
type ChangeKind string

const (
	SessionCreated ChangeKind = "created"
	SessionUpdated ChangeKind = "updated"
	SessionRemoved ChangeKind = "removed"
)

// Change announces one store mutation.
type Change struct {
	Kind ChangeKind
	ID   string
}

// file is the on-disk TOML shape of a session card.
type file struct {
	Schema    int         `toml:"schema"`
	Name      string      `toml:"name"`
	Topic     string      `toml:"topic"`
	Agents    []string    `toml:"agents"`
	Channel   string      `toml:"channel"`
	CreatedAt time.Time   `toml:"created_at"`
	UpdatedAt time.Time   `toml:"updated_at"`
	Artifacts []artifactF `toml:"artifact"`
}

type artifactF struct {
	Path      string         `toml:"path"`
	Author    string         `toml:"author"`
	Status    ArtifactStatus `toml:"status"`
	Notes     string         `toml:"notes,omitempty"`
	Hash      string         `toml:"hash"`
	UpdatedAt time.Time      `toml:"updated_at"`
}

// Store is the session registry. Safe for concurrent use.
type Store struct {
	ws    *workspace.Workspace
	mu    sync.RWMutex
	items map[string]Session
	order []string // sorted ids

	warns []string

	subs   map[int]chan Change
	subSeq int

	now func() time.Time
}

// Open loads every *.toml card under .dhi/sessions/. A missing dir is an
// empty store; malformed cards are skipped and recorded as warnings.
func Open(ws *workspace.Workspace) (*Store, error) {
	s := &Store{
		ws:    ws,
		items: map[string]Session{},
		subs:  map[int]chan Change{},
		now:   time.Now,
	}
	if ws == nil {
		return s, nil
	}
	entries, err := os.ReadDir(filepath.Join(ws.Root, Dir))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("ideation: open: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".toml")
		sess, err := parseCard(filepath.Join(ws.Root, Dir, e.Name()), id)
		if err != nil {
			s.warns = append(s.warns, fmt.Sprintf("session %s: %v", id, err))
			continue
		}
		s.items[id] = sess
		s.order = insertSorted(s.order, id)
	}
	return s, nil
}

// Warnings reports malformed cards found at Open.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warns...)
}

func parseCard(path, id string) (Session, error) {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return Session{}, fmt.Errorf("parse: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Session{}, fmt.Errorf("unknown keys: %v", undecoded)
	}
	if f.Schema != SchemaVersion {
		return Session{}, fmt.Errorf("schema %d (want %d)", f.Schema, SchemaVersion)
	}
	if !slugRe.MatchString(id) {
		return Session{}, fmt.Errorf("bad id %q", id)
	}
	if strings.TrimSpace(f.Name) == "" {
		return Session{}, fmt.Errorf("empty name")
	}
	if !bus.ValidChannel(f.Channel) {
		return Session{}, fmt.Errorf("bad channel %q", f.Channel)
	}
	seen := map[string]bool{}
	agents := []string{}
	for _, a := range f.Agents {
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		agents = append(agents, a)
	}
	sort.Strings(agents)

	artSeen := map[string]bool{}
	arts := []Artifact{}
	for _, af := range f.Artifacts {
		if af.Path == "" || artSeen[af.Path] {
			continue
		}
		if !af.Status.Valid() {
			return Session{}, fmt.Errorf("artifact %q: bad status %q", af.Path, af.Status)
		}
		artSeen[af.Path] = true
		arts = append(arts, Artifact{
			Path: af.Path, Author: af.Author, Status: af.Status,
			Notes: af.Notes, Hash: af.Hash, UpdatedAt: af.UpdatedAt,
		})
	}
	sort.Slice(arts, func(i, j int) bool { return arts[i].Path < arts[j].Path })

	return Session{
		ID: id, Name: f.Name, Topic: f.Topic, Agents: agents,
		Channel: f.Channel, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
		Artifacts: arts,
	}, nil
}

// Sessions lists all sessions sorted by id.
func (s *Store) Sessions() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.items[id])
	}
	return out
}

// Get fetches one session.
func (s *Store) Get(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.items[id]
	return sess, ok
}

// ChannelFor is the canonical channel name for a session id.
func ChannelFor(id string) string { return "#ideation-" + id }

// Create persists a new session. The id is derived from name; topic and
// agents are optional. The artifact folder is created empty so agents can
// write into it immediately.
func (s *Store) Create(name, topic string, agents []string) (Session, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Session{}, fmt.Errorf("ideation: name required")
	}
	id := Slugify(name)
	if !slugRe.MatchString(id) {
		return Session{}, fmt.Errorf("ideation: name %q produces unusable id %q", name, id)
	}
	s.mu.Lock()
	_, exists := s.items[id]
	s.mu.Unlock()
	if exists {
		return Session{}, fmt.Errorf("ideation: session %q already exists", id)
	}
	invited := dedupeSorted(agents)
	channel := ChannelFor(id)
	if !bus.ValidChannel(channel) {
		return Session{}, fmt.Errorf("ideation: bad channel %q", channel)
	}
	now := s.now()
	sess := Session{
		ID: id, Name: name, Topic: strings.TrimSpace(topic),
		Agents: invited, Channel: channel, CreatedAt: now, UpdatedAt: now,
	}
	if err := os.MkdirAll(s.dirFor(id), 0o755); err != nil {
		return Session{}, fmt.Errorf("ideation: mkdir: %w", err)
	}
	if err := writeCard(s.cardPath(id), sess); err != nil {
		return Session{}, err
	}
	s.commit(func() {
		s.items[id] = sess
		s.order = insertSorted(s.order, id)
	}, Change{Kind: SessionCreated, ID: id})
	return sess, nil
}

// Remove deletes a session card. The artifact folder is left on disk —
// ideated documents are the user's work, never garbage-collected here.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	if _, ok := s.items[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("ideation: unknown session %q", id)
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
	}, Change{Kind: SessionRemoved, ID: id})
	return nil
}

// SetTopic updates the session topic line.
func (s *Store) SetTopic(id, topic string) error {
	return s.mutate(id, func(sess *Session) { sess.Topic = strings.TrimSpace(topic) })
}

// SetAgents replaces the invited agent set.
func (s *Store) SetAgents(id string, agents []string) error {
	invited := dedupeSorted(agents)
	return s.mutate(id, func(sess *Session) { sess.Agents = invited })
}

// dirFor is the artifact folder for a session.
func (s *Store) dirFor(id string) string {
	return filepath.Join(s.ws.Root, Dir, id)
}

// DirFor exposes the artifact folder path (surface reads artifact files
// from here). Unknown sessions still resolve — callers validate first.
func (s *Store) DirFor(id string) string { return s.dirFor(id) }

// cardPath is the TOML card location.
func (s *Store) cardPath(id string) string {
	return filepath.Join(s.ws.Root, Dir, id+".toml")
}

// Scan walks the session's artifact folder and merges the filesystem with
// the recorded artifact registry:
//
//   - a new file becomes a draft artifact (author unknown until claimed);
//   - a file whose content hash changed flips back to draft, keeping its
//     author and notes (the author revised after rejection/review);
//   - a recorded file that vanished is dropped from the registry.
//
// The card file itself lives beside the folder, not inside it, so cards
// never show up as artifacts. Directories are walked recursively.
func (s *Store) Scan(id string) error {
	sess, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("ideation: unknown session %q", id)
	}
	root := s.dirFor(id)
	type found struct{ rel, hash string }
	var foundFiles []found
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable entries are skipped, not fatal
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil //nolint:nilerr // skip unmappable entries
		}
		hash, herr := shortHash(path)
		if herr != nil {
			return nil //nolint:nilerr // unreadable files are skipped, not fatal
		}
		foundFiles = append(foundFiles, found{rel: filepath.ToSlash(rel), hash: hash})
		return nil
	})

	recorded := map[string]Artifact{}
	for _, a := range sess.Artifacts {
		recorded[a.Path] = a
	}
	now := s.now()
	var next []Artifact
	for _, f := range foundFiles {
		prev, ok := recorded[f.rel]
		if !ok {
			next = append(next, Artifact{
				Path: f.rel, Status: StatusDraft, Hash: f.hash, UpdatedAt: now,
			})
			continue
		}
		delete(recorded, f.rel)
		if prev.Hash == f.hash && prev.Status.Valid() {
			prev.UpdatedAt = now
			next = append(next, prev)
			continue
		}
		// Content changed since the last scan: any decision is stale.
		prev.Hash = f.hash
		prev.Status = StatusDraft
		prev.UpdatedAt = now
		next = append(next, prev)
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Path < next[j].Path })
	return s.mutate(id, func(sess *Session) { sess.Artifacts = next })
}

// Artifact fetches one recorded artifact.
func (s *Store) Artifact(id, path string) (Artifact, bool) {
	sess, ok := s.Get(id)
	if !ok {
		return Artifact{}, false
	}
	for _, a := range sess.Artifacts {
		if a.Path == path {
			return a, true
		}
	}
	return Artifact{}, false
}

// ArtifactPath maps an artifact's rel path to its absolute file location.
func (s *Store) ArtifactPath(id, rel string) string {
	return filepath.Join(s.dirFor(id), filepath.FromSlash(rel))
}

// VPathFor returns the agent-facing vpath of an artifact.
func VPathFor(sess Session, rel string) string {
	return ReservedPrefix + sess.ID + "/" + rel
}

// ReservedPrefix is the reserved `.dhi` vpath prefix for artifact paths.
const ReservedPrefix = ".dhi/sessions/"

// MarkReviewed flips an artifact to reviewed. Only drafts and rejected
// artifacts move forward to reviewed.
func (s *Store) MarkReviewed(id, path string) error {
	return s.transition(id, path, StatusReviewed, "")
}

// Approve flips an artifact to approved — the human-only terminal state.
func (s *Store) Approve(id, path string) error {
	return s.transition(id, path, StatusApproved, "")
}

// Reject records revision notes on an artifact. Any status may be
// rejected; the notes route back to the authoring agent (surface's job).
func (s *Store) Reject(id, path, notes string) error {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return fmt.Errorf("ideation: rejection needs notes")
	}
	return s.transition(id, path, StatusRejected, notes)
}

// transition moves one artifact to status, keeping notes only for
// rejections. Terminal states (approved) refuse further transitions.
func (s *Store) transition(id, path string, status ArtifactStatus, notes string) error {
	return s.mutateArtifact(id, path, func(a *Artifact) error {
		if a.Status == StatusApproved {
			return fmt.Errorf("ideation: %s is approved and final", path)
		}
		a.Status = status
		if status == StatusRejected {
			a.Notes = notes
		}
		a.UpdatedAt = s.now()
		return nil
	})
}

// ClaimAuthor records which agent produced an artifact (used when an
// agent announces its output in chat and the file is still unclaimed).
func (s *Store) ClaimAuthor(id, path, agent string) error {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return fmt.Errorf("ideation: empty author")
	}
	return s.mutateArtifact(id, path, func(a *Artifact) error {
		if a.Author != "" && a.Author != agent {
			return fmt.Errorf("ideation: %s already authored by %s", path, a.Author)
		}
		a.Author = agent
		a.UpdatedAt = s.now()
		return nil
	})
}

// mutateArtifact loads → validates via apply → persists → commits.
func (s *Store) mutateArtifact(id, path string, apply func(*Artifact) error) error {
	s.mu.Lock()
	sess, ok := s.items[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("ideation: unknown session %q", id)
	}
	idx := -1
	for i := range sess.Artifacts {
		if sess.Artifacts[i].Path == path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("ideation: %s has no artifact %q", id, path)
	}
	a := sess.Artifacts[idx]
	if err := apply(&a); err != nil {
		return err
	}
	sess.Artifacts[idx] = a
	return s.mutate(id, func(in *Session) {
		for i := range in.Artifacts {
			if in.Artifacts[i].Path == path {
				in.Artifacts[i] = a
			}
		}
	})
}

// mutate loads → applies → persists → commits, keeping disk ahead of
// memory like the other registries.
func (s *Store) mutate(id string, apply func(*Session)) error {
	s.mu.Lock()
	sess, ok := s.items[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("ideation: unknown session %q", id)
	}
	apply(&sess)
	sess.UpdatedAt = s.now()
	if err := writeCard(s.cardPath(id), sess); err != nil {
		return err
	}
	s.commit(func() { s.items[id] = sess }, Change{Kind: SessionUpdated, ID: id})
	return nil
}

func writeCard(path string, sess Session) error {
	f := file{
		Schema: SchemaVersion, Name: sess.Name, Topic: sess.Topic,
		Agents: sess.Agents, Channel: sess.Channel,
		CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}
	for _, a := range sess.Artifacts {
		f.Artifacts = append(f.Artifacts, artifactF{
			Path: a.Path, Author: a.Author, Status: a.Status, Notes: a.Notes,
			Hash: a.Hash, UpdatedAt: a.UpdatedAt,
		})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ideation: write: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*.toml")
	if err != nil {
		return fmt.Errorf("ideation: write: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(f); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("ideation: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("ideation: write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("ideation: write: %w", err)
	}
	return nil
}

func insertSorted(sorted []string, v string) []string {
	i := sort.SearchStrings(sorted, v)
	return append(sorted[:i:i], append([]string{v}, sorted[i:]...)...)
}

func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Slugify derives a session id from a display name: lowercase; runs of
// characters outside [a-z0-9._-] collapse to '-', trimmed at both ends.
// Empty results become "session".
func Slugify(name string) string {
	var b strings.Builder
	lastDash := true // suppress leading dashes
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	id := strings.Trim(b.String(), "-")
	if len(id) > 48 {
		id = strings.Trim(id[:48], "-")
	}
	if id == "" {
		id = "session"
	}
	return id
}

// shortHash is the first 12 hex chars of the file's sha256.
func shortHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
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

// Subscribe receives subsequent store changes until cancel runs.
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
