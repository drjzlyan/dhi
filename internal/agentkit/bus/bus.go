// Package bus is DHI's message bus: channels (#name), direct messages,
// and threads, persisted as JSONL under .dhi/channels/ and replayed on
// open. The human posts as "you"; agents post under their manifest ids.
// @agent mentions in text are the trigger the runtime turns into LLM
// conversations.
package bus

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/jsonl"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Author is the human.
const Human = "you"

// Message is one immutable chat record.
type Message struct {
	ID      int64     `json:"id"`
	Channel string    `json:"channel"` // "#general" or "dm:<agent-id>"
	Thread  int64     `json:"thread,omitempty"`
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidChannel reports whether name is addressable: "#slug" for channels
// or "dm:<agent-id>" for direct messages. The slug form guards the
// on-disk layout against traversal.
func ValidChannel(name string) bool {
	switch {
	case strings.HasPrefix(name, "#"):
		return nameRe.MatchString(strings.TrimPrefix(name, "#"))
	case strings.HasPrefix(name, "dm:"):
		return nameRe.MatchString(strings.TrimPrefix(name, "dm:"))
	default:
		return false
	}
}

func fileFor(root, channel string) (string, error) {
	if !ValidChannel(channel) {
		return "", fmt.Errorf("bus: bad channel %q", channel)
	}
	var rel string
	if strings.HasPrefix(channel, "#") {
		rel = filepath.Join(workspace.DirChannels, "channels", strings.TrimPrefix(channel, "#")+".jsonl")
	} else {
		rel = filepath.Join(workspace.DirChannels, "dm", strings.TrimPrefix(channel, "dm:")+".jsonl")
	}
	return filepath.Join(root, rel), nil
}

// Bus is the loaded message store. It is safe for concurrent use; Post
// assigns IDs and timestamps, appends to disk, then fans out.
type Bus struct {
	root   string // workspace root
	mu     sync.Mutex
	nextID int64
	subs   map[string]map[int]chan Message
	subSeq int
}

// Open replays persisted history from ws's .dhi/channels/ tree (seeding
// the ID counter); a missing tree is an empty bus.
func Open(ws *workspace.Workspace) (*Bus, error) {
	b := &Bus{root: ws.Root, subs: map[string]map[int]chan Message{}}
	base := filepath.Join(ws.Root, workspace.DirChannels)
	load := func(dir, prefix string) error {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("bus: read %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			msgs, err := jsonl.ReadAll[Message](filepath.Join(dir, e.Name()))
			if err != nil {
				return err
			}
			b.mu.Lock()
			for _, m := range msgs {
				if m.ID > b.nextID {
					b.nextID = m.ID
				}
			}
			b.mu.Unlock()
		}
		return nil
	}
	if err := load(filepath.Join(base, "channels"), "#"); err != nil {
		return nil, err
	}
	return b, load(filepath.Join(base, "dm"), "dm:")
}

// History returns a snapshot of one channel (thread==0) or one thread,
// oldest first. Unknown channels read as empty.
func (b *Bus) History(channel string, thread int64) []Message {
	path, err := fileFor(b.root, channel)
	if err != nil {
		return nil
	}
	msgs, err := jsonl.ReadAll[Message](path)
	if err != nil {
		return nil
	}
	out := msgs[:0]
	for _, m := range msgs {
		if thread == 0 && m.Thread == 0 || thread != 0 && m.Thread == thread {
			out = append(out, m)
		}
	}
	return out
}

// Channels lists every channel holding at least one message, sorted.
func (b *Bus) Channels() []string {
	seen := map[string]bool{}
	scan := func(dir, prefix string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			seen[prefix+strings.TrimSuffix(e.Name(), ".jsonl")] = true
		}
	}
	base := filepath.Join(b.root, workspace.DirChannels)
	scan(filepath.Join(base, "channels"), "#")
	scan(filepath.Join(base, "dm"), "dm:")
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Subscribe receives every subsequently posted message for channel.
// Delivery is best-effort (a full subscriber buffer drops messages);
// call the returned cancel func when done.
func (b *Bus) Subscribe(channel string) (<-chan Message, func()) {
	ch := make(chan Message, 64)
	b.mu.Lock()
	b.subSeq++
	id := b.subSeq
	if b.subs[channel] == nil {
		b.subs[channel] = map[int]chan Message{}
	}
	b.subs[channel][id] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs[channel], id)
		b.mu.Unlock()
	}
}

// Post stamps, persists, and broadcasts one message. The returned value
// carries the assigned ID/timestamp.
func (b *Bus) Post(m Message) (Message, error) {
	if !ValidChannel(m.Channel) {
		return Message{}, fmt.Errorf("bus: bad channel %q", m.Channel)
	}
	if strings.TrimSpace(m.Text) == "" {
		return Message{}, fmt.Errorf("bus: empty text")
	}
	if m.Author == "" {
		return Message{}, fmt.Errorf("bus: empty author")
	}
	path, err := fileFor(b.root, m.Channel)
	if err != nil {
		return Message{}, err
	}
	b.mu.Lock()
	b.nextID++
	m.ID = b.nextID
	m.At = time.Now()
	b.mu.Unlock()

	if err := jsonl.Append(path, m); err != nil {
		return Message{}, fmt.Errorf("bus: persist: %w", err)
	}
	b.mu.Lock()
	for _, sub := range b.subs[m.Channel] {
		select {
		case sub <- m:
		default:
		}
	}
	b.mu.Unlock()
	return m, nil
}

// mentionRe finds "@agent" tokens (word-ish slugs).
var mentionRe = regexp.MustCompile(`(?:^|\s)@([a-z0-9][a-z0-9._-]*)`)

// Mentions extracts distinct agent ids mentioned in text, in order.
func Mentions(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// ThreadOf returns the thread root id for m (its own id at top level).
func ThreadOf(m Message) int64 {
	if m.Thread != 0 {
		return m.Thread
	}
	return m.ID
}
