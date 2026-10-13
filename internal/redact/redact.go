// Package redact keeps secret values out of everything DHI writes or
// shows (F-088, ADR-0039): tool results an agent reads, bus posts,
// transcripts and traces. A value is replaced by ‹secret:NAME›, so the
// reader still knows which secret was there.
//
// It matches literal values only: a transformed value (base64, split
// across lines) is not caught. ADR-0039 records that limit.
package redact

import (
	"sort"
	"strings"
	"sync"
)

// MinLen is the shortest value redacted: anything shorter would erase
// ordinary words and numbers wherever they appear.
const MinLen = 6

// Redactor replaces known secret values. Its source is re-read only when
// stamp changes (the credential file's size and mtime), so new secrets
// are covered without a restart. The zero value and nil redact nothing.
type Redactor struct {
	src   func() map[string]string // name → value
	stamp func() string

	mu   sync.Mutex
	seen string
	repl *strings.Replacer
}

// New builds a Redactor over src; stamp may be nil (src is read once).
func New(src func() map[string]string, stamp func() string) *Redactor {
	return &Redactor{src: src, stamp: stamp}
}

// String returns s with every known value replaced.
func (r *Redactor) String(s string) string {
	if r == nil || r.src == nil || s == "" {
		return s
	}
	repl := r.replacer()
	if repl == nil {
		return s
	}
	return repl.Replace(s)
}

// Func is String as a plain func, for seams that take one (nil-safe).
func (r *Redactor) Func() func(string) string {
	if r == nil {
		return nil
	}
	return r.String
}

func (r *Redactor) replacer() *strings.Replacer {
	st := "once"
	if r.stamp != nil {
		st = r.stamp()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.repl != nil && st == r.seen {
		return r.repl
	}
	r.seen = st
	r.repl = build(r.src())
	return r.repl
}

// build orders values longest first: strings.Replacer tries patterns in
// argument order at each position, so a secret that contains another is
// replaced whole.
func build(values map[string]string) *strings.Replacer {
	type kv struct{ name, value string }
	var all []kv
	for n, v := range values {
		if len(v) >= MinLen {
			all = append(all, kv{n, v})
		}
	}
	if len(all) == 0 {
		return nil
	}
	sort.Slice(all, func(i, j int) bool {
		if len(all[i].value) != len(all[j].value) {
			return len(all[i].value) > len(all[j].value)
		}
		return all[i].name < all[j].name
	})
	pairs := make([]string, 0, len(all)*2)
	for _, e := range all {
		pairs = append(pairs, e.value, Mark(e.name))
	}
	return strings.NewReplacer(pairs...)
}

// Mark is what a redacted value reads as.
func Mark(name string) string { return "‹secret:" + name + "›" }
