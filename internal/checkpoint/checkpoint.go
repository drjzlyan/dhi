// Package checkpoint snapshots the workspace's repos around an agent turn
// so the turn can be undone (F-120). Snapshots live in a private git
// directory per member under .dhi/checkpoints/<member>.git, used with
// --work-tree=<member>: the member's own .git (refs, stash, index) is
// never touched. Tracked and untracked files are captured; what the
// member's .gitignore, its info/exclude (copied in) and the user's global
// excludes ignore is not.
package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Git runs the user's git (gitcore.Runner satisfies it).
type Git interface {
	Run(ctx context.Context, dir string, args ...string) (string, string, error)
}

// Member is one repo of the workspace.
type Member struct{ Name, Path string }

// Point is one snapshot: a commit per member, by member name.
type Point struct {
	ID      int64             `json:"id"`
	At      time.Time         `json:"at"`
	Label   string            `json:"label"`
	Commits map[string]string `json:"commits"`
}

// Turn pairs the snapshots taken before and after one agent turn.
type Turn struct {
	Agent   string    `json:"agent"`
	Channel string    `json:"channel"`
	At      time.Time `json:"at"`
	Before  Point     `json:"before"`
	After   Point     `json:"after"`
}

// Change is one file a turn changed.
type Change struct {
	Member string
	Path   string // slash-separated, relative to the member
	Status string // A added, M modified, D deleted
}

// Store keeps the snapshots of one workspace.
type Store struct {
	dir     string // .dhi/checkpoints
	git     Git
	members func() []Member
	mu      sync.Mutex
	keep    int
}

// keepTurns is how many turns stay undoable; older snapshots are pruned.
const keepTurns = 30

// Open binds a store to the workspace root.
func Open(wsRoot string, git Git, members func() []Member) *Store {
	return &Store{dir: filepath.Join(wsRoot, ".dhi", "checkpoints"), git: git, members: members, keep: keepTurns}
}

func (s *Store) gitDir(m Member) string { return filepath.Join(s.dir, m.Name+".git") }

// run is git with the member's tree and the store's git dir.
func (s *Store) run(ctx context.Context, m Member, args ...string) (string, error) {
	full := append([]string{"--git-dir=" + s.gitDir(m), "--work-tree=" + m.Path,
		"-c", "core.autocrlf=false", "-c", "core.safecrlf=false"}, args...)
	out, _, err := s.git.Run(ctx, m.Path, full...)
	return strings.TrimSpace(out), err
}

func (s *Store) ensure(ctx context.Context, m Member) error {
	gd := s.gitDir(m)
	if _, err := os.Stat(filepath.Join(gd, "HEAD")); err != nil {
		if err := os.MkdirAll(gd, 0o755); err != nil {
			return err
		}
		if _, _, err := s.git.Run(ctx, s.dir, "init", "--bare", "-q", gd); err != nil {
			return err
		}
	}
	// The member's own exclusions apply here too.
	if data, err := os.ReadFile(filepath.Join(m.Path, ".git", "info", "exclude")); err == nil {
		_ = os.MkdirAll(filepath.Join(gd, "info"), 0o755)
		_ = os.WriteFile(filepath.Join(gd, "info", "exclude"), data, 0o644)
	}
	return nil
}

// Snapshot records every member's working tree now.
func (s *Store) Snapshot(ctx context.Context, label string) (Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := Point{ID: time.Now().UnixNano(), At: time.Now(), Label: label, Commits: map[string]string{}}
	for _, m := range s.members() {
		if err := s.ensure(ctx, m); err != nil {
			return p, fmt.Errorf("checkpoint %s: %w", m.Name, err)
		}
		if _, err := s.run(ctx, m, "add", "-A", "--", "."); err != nil {
			return p, fmt.Errorf("checkpoint %s: %w", m.Name, err)
		}
		tree, err := s.run(ctx, m, "write-tree")
		if err != nil {
			return p, fmt.Errorf("checkpoint %s: %w", m.Name, err)
		}
		commit, err := s.run(ctx, m, "-c", "user.name=dhi", "-c", "user.email=dhi@localhost",
			"commit-tree", tree, "-m", label)
		if err != nil {
			return p, fmt.Errorf("checkpoint %s: %w", m.Name, err)
		}
		if _, err := s.run(ctx, m, "update-ref", "refs/dhi/"+strconv.FormatInt(p.ID, 10), commit); err != nil {
			return p, fmt.Errorf("checkpoint %s: %w", m.Name, err)
		}
		p.Commits[m.Name] = commit
	}
	return p, nil
}

func (s *Store) member(name string) (Member, bool) {
	for _, m := range s.members() {
		if m.Name == name {
			return m, true
		}
	}
	return Member{}, false
}

// Diff lists the files that differ between two points.
func (s *Store) Diff(ctx context.Context, a, b Point) ([]Change, error) {
	var out []Change
	for name, ca := range a.Commits {
		cb, ok := b.Commits[name]
		if !ok || ca == cb {
			continue
		}
		m, ok := s.member(name)
		if !ok {
			continue
		}
		raw, err := s.run(ctx, m, "diff-tree", "-r", "--no-renames", "--name-status", ca, cb)
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(raw, "\n") {
			st, path, ok := strings.Cut(l, "\t")
			if !ok {
				continue
			}
			out = append(out, Change{Member: name, Path: path, Status: st[:1]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Member != out[j].Member {
			return out[i].Member < out[j].Member
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// blob is a file's content at a point (ok false when it did not exist).
func (s *Store) blob(ctx context.Context, p Point, c Change) ([]byte, bool, error) {
	m, ok := s.member(c.Member)
	if !ok {
		return nil, false, fmt.Errorf("member %s is gone", c.Member)
	}
	commit := p.Commits[c.Member]
	if _, err := s.run(ctx, m, "cat-file", "-e", commit+":"+c.Path); err != nil {
		return nil, false, nil
	}
	// Raw stdout (s.run would trim the file's own surrounding space).
	out, _, err := s.git.Run(ctx, m.Path, "--git-dir="+s.gitDir(m), "cat-file", "blob", commit+":"+c.Path)
	if err != nil {
		return nil, false, err
	}
	return []byte(out), true, nil
}

// Conflicts lists the changes whose file was edited again after the turn
// (the current content is neither the turn's result nor its start).
func (s *Store) Conflicts(ctx context.Context, t Turn, cs []Change) ([]Change, error) {
	var out []Change
	for _, c := range cs {
		after, existed, err := s.blob(ctx, t.After, c)
		if err != nil {
			return nil, err
		}
		m, _ := s.member(c.Member)
		cur, rerr := os.ReadFile(filepath.Join(m.Path, filepath.FromSlash(c.Path)))
		exists := rerr == nil
		if exists != existed || (exists && string(cur) != string(after)) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Restore puts the files of cs back the way they were at point p: a file
// that did not exist then is removed.
func (s *Store) Restore(ctx context.Context, p Point, cs []Change) error {
	var errs []error
	for _, c := range cs {
		m, ok := s.member(c.Member)
		if !ok {
			errs = append(errs, fmt.Errorf("member %s is gone", c.Member))
			continue
		}
		dst := filepath.Join(m.Path, filepath.FromSlash(c.Path))
		data, existed, err := s.blob(ctx, p, c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !existed {
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(dst); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.WriteFile(dst, data, mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ---- the turn log ----

func (s *Store) logPath() string { return filepath.Join(s.dir, "turns.json") }

// Turns lists recorded turns, newest first.
func (s *Store) Turns() []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnsLocked()
}

func (s *Store) turnsLocked() []Turn {
	data, err := os.ReadFile(s.logPath())
	if err != nil {
		return nil
	}
	var ts []Turn
	_ = json.Unmarshal(data, &ts)
	return ts
}

// Record adds a turn and prunes the oldest beyond the keep limit.
func (s *Store) Record(ctx context.Context, t Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := append([]Turn{t}, s.turnsLocked()...)
	var drop []Turn
	if len(ts) > s.keep {
		ts, drop = ts[:s.keep], ts[s.keep:]
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(ts, "", "  ")
	if err := os.WriteFile(s.logPath(), data, 0o644); err != nil {
		return err
	}
	for _, d := range drop {
		for _, p := range []Point{d.Before, d.After} {
			for name := range p.Commits {
				if m, ok := s.member(name); ok {
					_, _ = s.run(ctx, m, "update-ref", "-d", "refs/dhi/"+strconv.FormatInt(p.ID, 10))
				}
			}
		}
	}
	if len(drop) > 0 {
		for _, m := range s.members() {
			_, _ = s.run(ctx, m, "gc", "--auto", "--quiet")
		}
	}
	return nil
}

// Forget removes a turn from the log once it was undone.
func (s *Store) Forget(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.turnsLocked()
	for i, t := range ts {
		if t.At.Equal(at) {
			ts = append(ts[:i], ts[i+1:]...)
			break
		}
	}
	data, _ := json.MarshalIndent(ts, "", "  ")
	_ = os.WriteFile(s.logPath(), data, 0o644)
}
