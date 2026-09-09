// Package autopilot models F-015 autopilots: one named, scheduled
// engagement of a rostered agent per card, stored as strict TOML under
// the reserved .dhi/autopilots/ tree. DHI has no daemon (ADR-0004/0005),
// so schedules are declarations evaluated while DHI is open:
// due-on-launch catch-up plus message-driven ticks for interval cards.
package autopilot

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
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Dir is the reserved autopilots tree under the workspace root.
const Dir = ".dhi/autopilots"

// SchemaVersion is the card schema revision.
const SchemaVersion = 1

// Kind enumerates the three schedule shapes (F-015 Part A).
type Kind string

// Schedule kinds.
const (
	KindInterval Kind = "interval"
	KindDaily    Kind = "daily"
	KindWeekly   Kind = "weekly"
)

// Schedule is one strict schedule declaration.
type Schedule struct {
	Kind  Kind
	Every time.Duration // KindInterval only
	HH    int           // KindDaily / KindWeekly
	MM    int
	DOW   time.Weekday // KindWeekly only

	raw string
}

// String returns the original declaration text.
func (s Schedule) String() string { return s.raw }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
	"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday,
	"sat": time.Saturday,
}

// ParseSchedule decodes one strict schedule string; invalid input is
// refused with the offending value named (ADR-0011). Shapes:
// "interval <dur>", "daily HH:MM", "weekly <dow> HH:MM".
func ParseSchedule(s string) (Schedule, error) {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return Schedule{}, fmt.Errorf("autopilot: empty schedule")
	}
	sh := Schedule{raw: s}
	switch parts[0] {
	case "interval":
		if len(parts) != 2 {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: want \"interval <dur>\"", s)
		}
		d, err := time.ParseDuration(parts[1])
		if err != nil || d <= 0 {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: bad interval %q (want e.g. \"30m\")", s, parts[1])
		}
		sh.Kind, sh.Every = KindInterval, d
	case "daily":
		if len(parts) != 2 {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: want \"daily HH:MM\"", s)
		}
		hh, mm, ok := parseClock(parts[1])
		if !ok {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: bad time %q (want HH:MM)", s, parts[1])
		}
		sh.Kind, sh.HH, sh.MM = KindDaily, hh, mm
	case "weekly":
		if len(parts) != 3 {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: want \"weekly <dow> HH:MM\"", s)
		}
		dow, ok := weekdays[strings.ToLower(parts[1])]
		if !ok {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: unknown day %q (want mon..sun)", s, parts[1])
		}
		hh, mm, ok := parseClock(parts[2])
		if !ok {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: bad time %q (want HH:MM)", s, parts[2])
		}
		sh.Kind, sh.DOW, sh.HH, sh.MM = KindWeekly, dow, hh, mm
	default:
		return Schedule{}, fmt.Errorf("autopilot: schedule %q: want \"interval <dur>\", \"daily HH:MM\", or \"weekly <dow> HH:MM\"", s)
	}
	if sh.Kind != KindInterval {
		if sh.HH < 0 || sh.HH > 23 || sh.MM < 0 || sh.MM > 59 {
			return Schedule{}, fmt.Errorf("autopilot: schedule %q: time %02d:%02d out of range", s, sh.HH, sh.MM)
		}
	}
	return sh, nil
}

func parseClock(s string) (hh, mm int, ok bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 || i != len(s)-3 {
		return 0, 0, false
	}
	hh, err1 := atoi(s[:i])
	mm, err2 := atoi(s[i+1:])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return hh, mm, false
	}
	return hh, mm, true
}

func atoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a digit")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// Card is one autopilot declaration plus its runtime-maintained state.
type Card struct {
	Slug     string
	Name     string
	Agent    string
	Prompt   string
	Schedule Schedule
	Enabled  bool
	LastRun  time.Time // zero = never ran
}

// file is the on-disk TOML shape.
type file struct {
	Schema   int       `toml:"schema"`
	Name     string    `toml:"name"`
	Agent    string    `toml:"agent"`
	Prompt   string    `toml:"prompt"`
	Schedule string    `toml:"schedule"`
	Enabled  bool      `toml:"enabled"`
	LastRun  time.Time `toml:"last_run"`
}

// Store is the loaded autopilot set; safe for concurrent use.
type Store struct {
	ws *workspace.Workspace

	mu     sync.RWMutex
	cards  map[string]Card
	order  []string // slugs sorted for deterministic listing
	warns  []string // malformed cards skipped at Open
	subs   map[int]chan Change
	subSeq int
	now    func() time.Time
}

// Change announces one committed autopilot mutation.
type Change struct {
	Kind ChangeKind
	Slug string
}

// ChangeKind names a committed mutation.
type ChangeKind string

// Change kinds.
const (
	CardCreated ChangeKind = "created"
	CardUpdated ChangeKind = "updated"
	CardRemoved ChangeKind = "removed"
)

// Open loads every *.toml under .dhi/autopilots/. Missing dir = empty
// store; malformed cards are skipped and reported via Warnings (doctor).
func Open(ws *workspace.Workspace) (*Store, error) {
	s := &Store{
		ws:    ws,
		cards: map[string]Card{},
		subs:  map[int]chan Change{},
		now:   time.Now,
	}
	entries, err := os.ReadDir(filepath.Join(ws.Root, Dir))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("autopilot: read %s: %w", Dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".toml")
		c, perr := parseCard(filepath.Join(ws.Root, Dir, e.Name()), slug)
		if perr != nil {
			s.warns = append(s.warns, perr.Error())
			continue
		}
		s.cards[slug] = c
		s.order = append(s.order, slug)
	}
	sort.Strings(s.order)
	return s, nil
}

func parseCard(path, slug string) (Card, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Card{}, fmt.Errorf("autopilot: read %s: %w", slug, err)
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Card{}, fmt.Errorf("autopilot: %s: %w", slug, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		return Card{}, fmt.Errorf("autopilot: %s: unknown key(s): %s", slug, strings.Join(keys, ", "))
	}
	dummy := Card{}
	if err := validate(slug, f, dummy); err != nil {
		return Card{}, err
	}
	sch, err := ParseSchedule(f.Schedule)
	if err != nil {
		return Card{}, fmt.Errorf("autopilot: %s: %w", slug, err)
	}
	return Card{
		Slug: slug, Name: strings.TrimSpace(f.Name), Agent: strings.TrimSpace(f.Agent),
		Prompt: f.Prompt, Schedule: sch, Enabled: f.Enabled, LastRun: f.LastRun,
	}, nil
}

func validate(slug string, f file, _ Card) error {
	if f.Schema != SchemaVersion {
		return fmt.Errorf("autopilot: %s: schema %d, want %d", slug, f.Schema, SchemaVersion)
	}
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("autopilot: bad slug %q", slug)
	}
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("autopilot: %s: name required", slug)
	}
	if strings.TrimSpace(f.Agent) == "" {
		return fmt.Errorf("autopilot: %s: agent required", slug)
	}
	if strings.TrimSpace(f.Prompt) == "" {
		return fmt.Errorf("autopilot: %s: prompt required", slug)
	}
	return nil
}

// Warnings lists malformed cards found at Open.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warns...)
}

func (s *Store) dir() string                 { return filepath.Join(s.ws.Root, Dir) }
func (s *Store) cardPath(slug string) string { return filepath.Join(s.dir(), slug+".toml") }

// List returns every card ordered by slug.
func (s *Store) List() []Card {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Card, 0, len(s.order))
	for _, slug := range s.order {
		out = append(out, s.cards[slug])
	}
	return out
}

// Get returns one card by slug.
func (s *Store) Get(slug string) (Card, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cards[slug]
	return c, ok
}

// Due returns the enabled cards whose schedule has come due at now, in
// slug order. Missed instants collapse to a single catch-up: any past
// scheduled instant with no run since is due exactly once (F-015).
func (s *Store) Due(now time.Time) []Card {
	var out []Card
	for _, c := range s.List() {
		if c.Enabled && c.Due(now) {
			out = append(out, c)
		}
	}
	return out
}

// Due reports whether c's schedule instant is passed and un-run since.
// Missed instants (DHI closed) collapse to a single catch-up.
func (c Card) Due(now time.Time) bool {
	switch c.Schedule.Kind {
	case KindInterval:
		if c.LastRun.IsZero() {
			return true
		}
		return !now.Before(c.LastRun.Add(c.Schedule.Every))
	case KindDaily:
		t := time.Date(now.Year(), now.Month(), now.Day(),
			c.Schedule.HH, c.Schedule.MM, 0, 0, now.Location())
		return !now.Before(t) && (c.LastRun.IsZero() || c.LastRun.Before(t))
	case KindWeekly:
		t := weekAt(now, c.Schedule)
		return !now.Before(t) && (c.LastRun.IsZero() || c.LastRun.Before(t))
	}
	return false
}

// weekAt returns the schedule instant in the Monday-anchored week that
// contains now (weekly only).
// Next returns the next instant at which card's schedule should be
// re-evaluated (the tick boundary), or false when it never re-arms.
func (c Card) Next(now time.Time) (time.Time, bool) {
	return nextInstant(now, c.Schedule, c.LastRun)
}

func weekAt(now time.Time, s Schedule) time.Time {
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	daysSinceMon := (int(now.Weekday()) + 6) % 7 // Mon=0
	monday := anchor.AddDate(0, 0, -daysSinceMon)
	daysFromMon := (int(s.DOW) + 6) % 7 // sun=6, mon=0, …, sat=5
	return time.Date(monday.Year(), monday.Month(), monday.Day(),
		s.HH, s.MM, 0, 0, now.Location()).AddDate(0, 0, daysFromMon)
}

// NextArm returns the earliest future instant at which the enabled
// cards should be re-evaluated (interval chain re-arm or local
// date/clock change for daily/weekly), and whether such a card exists.
func (s *Store) NextArm(now time.Time) (time.Time, bool) {
	var next time.Time
	found := false
	for _, c := range s.List() {
		if !c.Enabled {
			continue
		}
		t, ok := c.Next(now)
		if !ok {
			continue
		}
		if !found || t.Before(next) {
			next, found = t, true
		}
	}
	return next, found
}

// nextInstant computes when c should next trigger re-evaluation.
func nextInstant(now time.Time, s Schedule, lastRun time.Time) (time.Time, bool) {
	switch s.Kind {
	case KindInterval:
		if lastRun.IsZero() || !now.Before(lastRun.Add(s.Every)) {
			return now, true
		}
		return lastRun.Add(s.Every), true
	case KindDaily:
		t := time.Date(now.Year(), now.Month(), now.Day(),
			s.HH, s.MM, 0, 0, now.Location())
		if now.Before(t) {
			return t, true
		}
		return t.AddDate(0, 0, 1), true
	case KindWeekly:
		t := weekAt(now, s)
		if !now.Before(t) {
			t = t.AddDate(0, 0, 7)
		}
		return t, true
	}
	return time.Time{}, false
}

// MarkRan persists a run timestamp for slug (runtime-maintained, and
// the switch between "due" and "caught up"; persisted before visibility
// so a crash never double-runs a catch-up).
func (s *Store) MarkRan(slug string, ts time.Time) error {
	return s.mutate(slug, func(c *Card) { c.LastRun = ts })
}

// Create adds one card (replacing nothing; duplicate slugs refuse).
func (s *Store) Create(slug, name, agent, prompt string, sch Schedule) (Card, error) {
	if !slugRe.MatchString(slug) {
		return Card{}, fmt.Errorf("autopilot: bad slug %q (lowercase [a-z0-9._-])", slug)
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(agent) == "" || strings.TrimSpace(prompt) == "" {
		return Card{}, fmt.Errorf("autopilot: name, agent, and prompt required")
	}
	if _, exists := s.Get(slug); exists {
		return Card{}, fmt.Errorf("autopilot: %q already exists", slug)
	}
	c := Card{Slug: slug, Name: strings.TrimSpace(name), Agent: strings.TrimSpace(agent),
		Prompt: prompt, Schedule: sch, Enabled: true}
	if err := writeCard(s.cardPath(slug), c); err != nil {
		return Card{}, err
	}
	s.mu.Lock()
	s.cards[slug] = c
	s.order = insertSorted(s.order, slug)
	s.mu.Unlock()
	s.emit(Change{Kind: CardCreated, Slug: slug})
	return c, nil
}

// SetEnabled arms (true) or pauses (false) a card.
func (s *Store) SetEnabled(slug string, on bool) error {
	return s.mutate(slug, func(c *Card) { c.Enabled = on })
}

// Remove deletes a card after its caller confirms.
func (s *Store) Remove(slug string) error {
	s.mu.RLock()
	c, ok := s.cards[slug]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("autopilot: unknown autopilot %q", slug)
	}
	_ = c
	if err := os.Remove(s.cardPath(slug)); err != nil {
		return fmt.Errorf("autopilot: remove %s: %w", slug, err)
	}
	order := make([]string, 0, len(s.order))
	for _, s2 := range s.order {
		if s2 != slug {
			order = append(order, s2)
		}
	}
	s.mu.Lock()
	delete(s.cards, slug)
	s.order = order
	s.mu.Unlock()
	s.emit(Change{Kind: CardRemoved, Slug: slug})
	return nil
}

// mutate loads → applies → persists → commits, keeping disk ahead of
// memory like the other registries.
func (s *Store) mutate(slug string, apply func(*Card)) error {
	s.mu.RLock()
	c, ok := s.cards[slug]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("autopilot: unknown autopilot %q", slug)
	}
	apply(&c)
	if err := writeCard(s.cardPath(slug), c); err != nil {
		return err
	}
	s.mu.Lock()
	s.cards[slug] = c
	s.mu.Unlock()
	s.emit(Change{Kind: CardUpdated, Slug: slug})
	return nil
}

func writeCard(path string, c Card) error {
	f := file{
		Schema:   SchemaVersion,
		Name:     c.Name,
		Agent:    c.Agent,
		Prompt:   c.Prompt,
		Schedule: c.Schedule.String(),
		Enabled:  c.Enabled,
		LastRun:  c.LastRun,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("autopilot: write: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auto-*.toml")
	if err != nil {
		return fmt.Errorf("autopilot: write: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(f); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("autopilot: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("autopilot: write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("autopilot: write: %w", err)
	}
	return nil
}

func insertSorted(sorted []string, v string) []string {
	i := sort.SearchStrings(sorted, v)
	return append(sorted[:i:i], append([]string{v}, sorted[i:]...)...)
}

// emit fans out a committed change to subscribers (best-effort).
func (s *Store) emit(c Change) {
	s.mu.Lock()
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

// Subscribe receives subsequent autopilot changes until cancel runs.
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
