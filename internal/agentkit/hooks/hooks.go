// Package hooks runs the lifecycle hooks a vendor plugin ships (F-179,
// ADR-0047): the vendor's own JSON (hooks/hooks.json or the manifest's
// hooks), parsed into typed events, fired at the places DHI can see.
//
// Scope, stated plainly:
//   - Only `type: "command"` runs. Other types and events outside the five
//     below are kept in Set.Unsupported so the install card can list them.
//   - A hook is a shell command. It runs under the OS sandbox with the
//     calling agent's scopes, never wider, and only after the user turned
//     hooks on for that plugin.
//   - Exit code 2 blocks (Pre/UserPromptSubmit); every other failure and
//     every timeout is a named record, never a silent skip (ADR-0011).
//   - Hook output that reaches a prompt is fenced as untrusted data
//     (ADR-0046).
package hooks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Event is a lifecycle point.
type Event string

// The supported events, named as the vendor names them.
const (
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	UserPromptSubmit Event = "UserPromptSubmit"
	Stop             Event = "Stop"
	SessionStart     Event = "SessionStart"
)

// Events lists the supported events in firing order.
var Events = []Event{SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Stop}

func (e Event) supported() bool {
	for _, k := range Events {
		if k == e {
			return true
		}
	}
	return false
}

// IsTool reports whether the event wraps a tool call (it has a matcher).
func (e Event) IsTool() bool { return e == PreToolUse || e == PostToolUse }

// Timeouts: every hook has one. The vendor's `timeout` is in seconds.
const (
	DefaultTimeout = 30 * time.Second
	MaxTimeout     = 120 * time.Second
)

// Hook is one command bound to an event.
type Hook struct {
	Event   Event
	Matcher string // vendor pattern over tool names; "" or "*" = every tool
	Command string
	Timeout time.Duration // never zero
}

// Unsupported is a hook DHI lists but does not run.
type Unsupported struct {
	Event   string
	Type    string // the hook's type, "" when the entry has none
	Command string
	Why     string
}

// Set is one plugin's parsed hooks.
type Set struct {
	Hooks       []Hook
	Unsupported []Unsupported
	Warnings    []string
}

// Empty reports whether the set holds nothing to run or list.
func (s Set) Empty() bool { return len(s.Hooks) == 0 && len(s.Unsupported) == 0 }

// Plugin is an installed plugin whose hooks the user turned on.
type Plugin struct {
	Name string
	Dir  string // the copy under DHI's hooks folder; ${CLAUDE_PLUGIN_ROOT}
	Set  Set
}

type group struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string  `json:"type"`
		Command string  `json:"command"`
		Timeout float64 `json:"timeout"`
	} `json:"hooks"`
}

// Parse reads the vendor format: {"hooks":{"PreToolUse":[{"matcher":"Bash",
// "hooks":[{"type":"command","command":"…","timeout":10}]}]}}. The inner
// event map alone (a manifest's inline `hooks`) is accepted too.
func Parse(data []byte) (Set, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Set{}, fmt.Errorf("hooks: %w", err)
	}
	if inner, ok := top["hooks"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(inner, &m) == nil {
			top = m
		}
	}
	names := make([]string, 0, len(top))
	for k := range top {
		names = append(names, k)
	}
	sort.Strings(names)
	var set Set
	for _, name := range names {
		var groups []group
		if err := json.Unmarshal(top[name], &groups); err != nil {
			set.Warnings = append(set.Warnings, fmt.Sprintf("hooks: %s is not a list of hook groups (%v)", name, err))
			continue
		}
		ev := Event(name)
		for _, g := range groups {
			for _, h := range g.Hooks {
				u := Unsupported{Event: name, Type: h.Type, Command: h.Command}
				switch {
				case !ev.supported():
					u.Why = "DHI does not fire this event"
				case h.Type != "command":
					u.Why = "only command hooks run"
				case strings.TrimSpace(h.Command) == "":
					u.Why = "no command"
				case ev.IsTool() && !validMatcher(g.Matcher):
					u.Why = fmt.Sprintf("matcher %q is not a valid pattern", g.Matcher)
				}
				if u.Why != "" {
					set.Unsupported = append(set.Unsupported, u)
					continue
				}
				to := DefaultTimeout
				if h.Timeout > 0 {
					to = time.Duration(h.Timeout * float64(time.Second))
				}
				if to > MaxTimeout {
					set.Warnings = append(set.Warnings, fmt.Sprintf("hooks: %s timeout %s is above the %s limit; using the limit", name, to, MaxTimeout))
					to = MaxTimeout
				}
				m := g.Matcher
				if !ev.IsTool() {
					m = "" // a matcher means nothing on the other events
				}
				set.Hooks = append(set.Hooks, Hook{Event: ev, Matcher: m, Command: h.Command, Timeout: to})
			}
		}
	}
	return set, nil
}

func validMatcher(m string) bool {
	if m == "" || m == "*" {
		return true
	}
	_, err := regexp.Compile("^(?:" + m + ")$")
	return err == nil
}

// toolTable maps the vendor's tool names to the DHI tools that do the same
// job. A matcher naming a vendor tool matches the DHI tool beside it, so a
// plugin written for "Bash" guards DHI's `run`. Vendor tools with no DHI
// counterpart (WebFetch, Task, …) are absent: they never fire at DHI's
// boundary, only through a CLI's native hook input (see NativeSettings).
var toolTable = []struct {
	Vendor string
	DHI    []string
}{
	{"Bash", []string{"run"}},
	{"Write", []string{"write"}},
	{"Edit", []string{"patch", "editor_apply_edit"}},
	{"MultiEdit", []string{"patch"}},
	{"Read", []string{"read"}},
	{"Glob", []string{"glob", "list"}},
	{"Grep", []string{"workspace_search"}},
}

// ToolTable returns the table as vendor name → DHI tools, for display.
func ToolTable() map[string][]string {
	out := map[string][]string{}
	for _, r := range toolTable {
		out[r.Vendor] = append([]string(nil), r.DHI...)
	}
	return out
}

// VendorName is the name the vendor's tool has for a DHI tool, or the DHI
// name itself when there is none (bridged mcp__server__tool names pass
// through unchanged, as the vendor spells them).
func VendorName(tool string) string {
	for _, r := range toolTable {
		for _, d := range r.DHI {
			if d == tool {
				return r.Vendor
			}
		}
	}
	return tool
}

// names are the spellings a matcher may use for a DHI tool.
func names(tool string) []string {
	out := []string{tool}
	for _, r := range toolTable {
		for _, d := range r.DHI {
			if d == tool {
				out = append(out, r.Vendor)
			}
		}
	}
	return out
}

// MatchTool reports whether the matcher selects a call to the DHI tool.
func MatchTool(matcher, tool string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + matcher + ")$")
	if err != nil {
		return false
	}
	for _, n := range names(tool) {
		if re.MatchString(n) {
			return true
		}
	}
	return false
}

var simpleMatcher = regexp.MustCompile(`^[A-Za-z0-9_|]+$`)

// NativeMatch is how the vendor CLI itself matches a tool name: a plain
// name or a|b list is exact, anything else is an unanchored pattern.
func NativeMatch(matcher, name string) bool {
	switch {
	case matcher == "" || matcher == "*":
		return true
	case simpleMatcher.MatchString(matcher):
		for _, p := range strings.Split(matcher, "|") {
			if p == name {
				return true
			}
		}
		return false
	}
	re, err := regexp.Compile(matcher)
	return err == nil && re.MatchString(name)
}
