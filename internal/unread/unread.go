// Package unread is DHI's read-mark store (F-017): Slack-style read
// watermarks per channel/thread plus snoozes, persisted as a strict
// per-user JSON file under .dhi/. It answers one question — which agent
// messages addressed to the human are still unaddressed — and owns the
// attention predicate. Watermarks are monotonic; a missing file is the
// fresh-install state (seeded, never an error); a corrupted file is a
// named error, never a silent reset.
package unread

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// File is the per-user read-mark state, relative to the workspace root
// (gitignored runtime state, the marketplace.json precedent).
const File = ".dhi/unread.json"

const schemaVersion = 1

// ThreadScope composes the thread-scope key "<channel>#<threadID>";
// a plain channel name is the top-level scope.
func ThreadScope(channel string, threadID int64) string {
	return channel + "#" + strconv.FormatInt(threadID, 10)
}

// Snooze parks one attention item until Until (F-017 "remind me later").
type Snooze struct {
	Channel   string    `json:"channel"`
	MessageID int64     `json:"messageID"`
	Until     time.Time `json:"until"`
}

// Data is the on-disk state.
type Data struct {
	// Channels maps a scope to the largest message ID the human has
	// seen in that scope (monotonic; 0 = nothing seen).
	Channels map[string]int64 `json:"channels"`
	Snoozes  []Snooze         `json:"snoozes"`
}

// Decode is the strict parse (ADR-0011 / F-011): unknown keys refuse
// with the key named, a wrong schema refuses with the value named, and
// a bad number/timestamp refuses naming the offending field. It never
// falls back to a default — a broken file must surface, not reset.
func Decode(raw []byte) (Data, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Data{}, fmt.Errorf("unread: parse: %w", err)
	}
	for k := range probe {
		switch k {
		case "schema", "channels", "snoozes":
		default:
			return Data{}, fmt.Errorf("unread: unknown key %q", k)
		}
	}
	schema, ok := probe["schema"]
	if !ok {
		return Data{}, fmt.Errorf("unread: missing key %q", "schema")
	}
	var sv int
	if err := json.Unmarshal(schema, &sv); err != nil {
		return Data{}, fmt.Errorf("unread: schema %s is not a number", strings.TrimSpace(string(schema)))
	}
	if sv != schemaVersion {
		return Data{}, fmt.Errorf("unread: schema %d, want %d", sv, schemaVersion)
	}
	d := Data{Channels: map[string]int64{}}
	if rawCh, ok := probe["channels"]; ok {
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(rawCh, &entries); err != nil {
			return Data{}, fmt.Errorf("unread: channels %s is not an object", strings.TrimSpace(string(rawCh)))
		}
		for scope, v := range entries {
			var n json.Number
			if err := json.Unmarshal(v, &n); err != nil {
				return Data{}, fmt.Errorf("unread: channels[%s] = %s is not a number", scope, strings.TrimSpace(string(v)))
			}
			id, err := n.Int64()
			if err != nil {
				return Data{}, fmt.Errorf("unread: channels[%s] = %s is not an integer", scope, n)
			}
			d.Channels[scope] = id
		}
	}
	if rawSn, ok := probe["snoozes"]; ok {
		var rows []json.RawMessage
		if err := json.Unmarshal(rawSn, &rows); err != nil {
			return Data{}, fmt.Errorf("unread: snoozes %s is not an array", strings.TrimSpace(string(rawSn)))
		}
		for i, row := range rows {
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(row, &keys); err != nil {
				return Data{}, fmt.Errorf("unread: snoozes[%d] %s is not an object", i, strings.TrimSpace(string(row)))
			}
			for k := range keys {
				switch k {
				case "channel", "messageID", "until":
				default:
					return Data{}, fmt.Errorf("unread: snoozes[%d] has unknown key %q", i, k)
				}
			}
			var ch string
			if err := json.Unmarshal(keys["channel"], &ch); err != nil || ch == "" {
				return Data{}, fmt.Errorf("unread: snoozes[%d].channel %s is not a non-empty string", i, orNull(keys["channel"]))
			}
			id, err := strconv.ParseInt(string(orNum(keys["messageID"])), 10, 64)
			if err != nil {
				return Data{}, fmt.Errorf("unread: snoozes[%d].messageID %s is not an integer", i, orNull(keys["messageID"]))
			}
			var until string
			if err := json.Unmarshal(keys["until"], &until); err != nil {
				return Data{}, fmt.Errorf("unread: snoozes[%d].until %s is not a string", i, orNull(keys["until"]))
			}
			t, err := time.Parse(time.RFC3339, until)
			if err != nil {
				return Data{}, fmt.Errorf("unread: snoozes[%d].until %q is not RFC3339", i, until)
			}
			d.Snoozes = append(d.Snoozes, Snooze{Channel: ch, MessageID: id, Until: t})
		}
	}
	return d, nil
}

func orNull(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "null"
	}
	return s
}

func orNum(raw json.RawMessage) string {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return s
}

// Store is the live read-mark state: a loaded Data plus a change pump
// for event-driven UIs (the task/autopilot store pattern).
type Store struct {
	root   string
	mu     sync.Mutex
	data   Data
	notes  []string
	subs   map[int]chan struct{}
	subSeq int
}

// Open loads (or seeds and writes) the store for ws. Seeding a missing
// file marks every channel's CURRENT top-level history as read — only
// messages posted after the first open count — while unopened threads
// stay fully unread (their replies are not top-level).
func Open(ws *workspace.Workspace, b *bus.Bus) (*Store, error) {
	path := filepath.Join(ws.Root, File)
	s := &Store{root: ws.Root, subs: map[int]chan struct{}{}}
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		s.data = seed(b)
		s.notes = append(s.notes, "seeded fresh-install watermarks at current top-level max IDs")
		if err := s.write(); err != nil {
			return nil, err
		}
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("unread: read %s: %w", File, err)
	}
	d, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	d, notes := prune(d, b)
	s.notes = notes
	s.data = d
	if len(notes) > 0 {
		if err := s.write(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// seed builds fresh-install watermarks: top-level max ID per existing
// channel. Threads are absent on purpose (unopened = unread).
func seed(b *bus.Bus) Data {
	d := Data{Channels: map[string]int64{}}
	if b == nil {
		return d
	}
	for _, ch := range b.Channels() {
		top := b.History(ch, 0)
		if len(top) > 0 {
			d.Channels[ch] = top[len(top)-1].ID
		}
	}
	return d
}

// prune drops watermarks for channels that no longer exist on the bus
// and expired/dangling snoozes, naming each group in the notes.
func prune(d Data, b *bus.Bus) (Data, []string) {
	if b == nil {
		return d, nil
	}
	var notes []string
	channels := map[string]bool{}
	for _, ch := range b.Channels() {
		channels[ch] = true
	}
	var prunedWM []string
	for scope := range d.Channels {
		if channels[scope] || parentOf(scope, channels) != "" {
			continue
		}
		delete(d.Channels, scope)
		prunedWM = append(prunedWM, scope)
	}
	var kept []Snooze
	var expired, dangling int
	for _, sn := range d.Snoozes {
		switch {
		case !sn.Until.After(time.Now()):
			expired++
		case !channels[sn.Channel]:
			dangling++
		default:
			kept = append(kept, sn)
		}
	}
	d.Snoozes = kept
	if len(prunedWM) > 0 {
		sort.Strings(prunedWM)
		notes = append(notes, fmt.Sprintf("pruned %d watermark(s) for absent channel(s): %s",
			len(prunedWM), strings.Join(prunedWM, ", ")))
	}
	if expired > 0 {
		notes = append(notes, fmt.Sprintf("dropped %d expired snooze(s)", expired))
	}
	if dangling > 0 {
		notes = append(notes, fmt.Sprintf("dropped %d snooze(s) for absent channel(s)", dangling))
	}
	return d, notes
}

// parentOf reports the parent channel of a thread scope ("#g#12" →
// "#g") when the parent exists among channels; "" otherwise.
func parentOf(scope string, channels map[string]bool) string {
	for ch := range channels {
		if strings.HasPrefix(scope, ch+"#") {
			return ch
		}
	}
	return ""
}

// Notes lists what Open did on load (seeding, pruning) — for display
// and tests, never an error.
func (s *Store) Notes() []string { return s.notes }

// Subscribe receives a token on every store change; the token carries
// no payload — callers re-read Unread. The channel is created once and
// shared by all callers.
func (s *Store) Subscribe() (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subSeq++
	id := s.subSeq
	ch := make(chan struct{}, 1)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

func (s *Store) signal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// MarkRead advances the watermark for scope to upToID. Monotonic: an
// ID at or below the current watermark is a no-op (never rewinds).
func (s *Store) MarkRead(scope string, upToID int64) error {
	s.mu.Lock()
	if upToID <= s.data.Channels[scope] {
		s.mu.Unlock()
		return nil
	}
	s.data.Channels[scope] = upToID
	err := s.writeLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.signal()
	return nil
}

// Snooze parks (channel, msgID) until until, replacing any existing
// snooze for the same message.
func (s *Store) Snooze(channel string, msgID int64, until time.Time) error {
	s.mu.Lock()
	kept := s.data.Snoozes[:0:0]
	for _, sn := range s.data.Snoozes {
		if sn.Channel == channel && sn.MessageID == msgID {
			continue
		}
		kept = append(kept, sn)
	}
	s.data.Snoozes = append(kept, Snooze{Channel: channel, MessageID: msgID, Until: until})
	err := s.writeLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.signal()
	return nil
}

// Unsnooze clears the snooze for (channel, msgID); a no-op when absent.
func (s *Store) Unsnooze(channel string, msgID int64) error {
	s.mu.Lock()
	kept := s.data.Snoozes[:0:0]
	for _, sn := range s.data.Snoozes {
		if sn.Channel == channel && sn.MessageID == msgID {
			continue
		}
		kept = append(kept, sn)
	}
	s.data.Snoozes = kept
	err := s.writeLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.signal()
	return nil
}

// Unread computes the current attention set (F-017 §Part B): every
// agent message addressed to the human, past the applicable watermark,
// including snoozed items (flagged) so the UI can dim them. Pure over
// the bus snapshot; oldest first.
func (s *Store) Unread(b *bus.Bus, now time.Time) []Item {
	s.mu.Lock()
	d := s.data
	s.mu.Unlock()
	return Scan(d.Channels, d.Snoozes, b, now)
}

// Item is one unaddressed agent message. Snoozed is zero unless the
// message is under an active snooze (its expiry).
type Item struct {
	Msg     bus.Message
	Snoozed time.Time
}

// AddressedToHuman is the attention predicate: a message needs the
// human only when it is addressed to them — any agent message in a DM
// (dm:<id> is 1:1) or a channel message mentioning "you". The human's
// own messages and agent-to-agent chatter never do.
func AddressedToHuman(m bus.Message) bool {
	if m.Author == bus.Human {
		return false
	}
	if strings.HasPrefix(m.Channel, "dm:") {
		return true
	}
	for _, id := range bus.Mentions(m.Text) {
		if id == bus.Human {
			return true
		}
	}
	return false
}

// Scan is the pure unread computation over a bus snapshot: for every
// channel, top-level messages past the channel watermark, plus each
// thread's replies past the thread watermark when the thread was opened
// (unopened threads count fully). A top-level message whose OWN thread
// was opened also counts as read (you looked at it) — max of the two
// watermarks applies. Oldest first (bus IDs are chronological),
// snoozed items flagged.
func Scan(channels map[string]int64, snoozes []Snooze, b *bus.Bus, now time.Time) []Item {
	if b == nil {
		return nil
	}
	snoozed := map[int64]time.Time{}
	for _, sn := range snoozes {
		if sn.Until.After(now) {
			snoozed[sn.MessageID] = sn.Until
		}
	}
	var out []Item
	consider := func(m bus.Message, limit int64) {
		if m.ID <= limit || !AddressedToHuman(m) {
			return
		}
		item := Item{Msg: m}
		if until, ok := snoozed[m.ID]; ok {
			item.Snoozed = until
		}
		out = append(out, item)
	}
	for _, ch := range b.Channels() {
		top := b.History(ch, 0)
		limit := channels[ch]
		for _, m := range top {
			// Opening a message's thread reads that message too.
			if tw, ok := channels[ThreadScope(ch, m.ID)]; ok && tw > limit {
				limit = tw
			}
			consider(m, limit)
			threadLimit, opened := channels[ThreadScope(ch, m.ID)]
			if !opened {
				threadLimit = 0
			}
			for _, r := range b.History(ch, m.ID) {
				consider(r, threadLimit)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Msg.ID < out[j].Msg.ID })
	return out
}

// Counts groups the unread set per channel (snoozed included — the rail
// reports unseen messages; the inbox reports attention).
func Counts(channels map[string]int64, snoozes []Snooze, b *bus.Bus, now time.Time) map[string]int {
	out := map[string]int{}
	for _, it := range Scan(channels, snoozes, b, now) {
		out[it.Msg.Channel]++
	}
	return out
}

// Counts reports the current per-channel unread counts (rail markers).
func (s *Store) Counts(b *bus.Bus, now time.Time) map[string]int {
	s.mu.Lock()
	d := s.data
	s.mu.Unlock()
	return Counts(d.Channels, d.Snoozes, b, now)
}

func (s *Store) write() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked()
}

func (s *Store) writeLocked() error {
	body, err := json.MarshalIndent(fileFromData(s.data), "", "  ")
	if err != nil {
		return fmt.Errorf("unread: encode: %w", err)
	}
	body = append(body, '\n')
	final := filepath.Join(s.root, File)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("unread: write %s: %w", File, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("unread: rename %s: %w", File, err)
	}
	return nil
}

func fileFromData(d Data) fileOut {
	f := fileOut{Schema: schemaVersion, Channels: map[string]int64{}}
	for scope, id := range d.Channels {
		f.Channels[scope] = id
	}
	for _, sn := range d.Snoozes {
		f.Snoozes = append(f.Snoozes, snoozeOut{
			Channel:   sn.Channel,
			MessageID: sn.MessageID,
			Until:     sn.Until.UTC(),
		})
	}
	return f
}

type fileOut struct {
	Schema   int              `json:"schema"`
	Channels map[string]int64 `json:"channels"`
	Snoozes  []snoozeOut      `json:"snoozes"`
}

type snoozeOut struct {
	Channel   string    `json:"channel"`
	MessageID int64     `json:"messageID"`
	Until     time.Time `json:"until"`
}
