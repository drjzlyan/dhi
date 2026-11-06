package skills

import (
	"sort"
	"strings"
)

// G-X3: user-invocable skills as slash commands. The frontmatter is Claude
// Code's own (`user-invocable`, `disable-model-invocation`), so a skill
// written for it works here unchanged (ADR-0043). Claude Code lists every
// skill by default; DHI offers a skill only when it says so, because a
// workspace can hold dozens of reference skills that were never meant as
// commands and the composer's completion would drown in them.

// Builtins is every slash command a composer handles itself (images are an
// @image: mention, not a slash command). A skill with one of these names is
// never offered and never run: the built-in wins. A test keeps it equal to
// what the composers parse.
var Builtins = []string{"model", "fork", "new", "compact", "plan", "review", "ask", "yes", "no", "export"}

// Builtin reports whether name is a built-in slash command.
func Builtin(name string) bool {
	for _, b := range Builtins {
		if b == strings.ToLower(name) {
			return true
		}
	}
	return false
}

// Slash reports whether the skill declares itself a user command:
// `user-invocable: true`, or `disable-model-invocation: true` (only the
// user can run it) unless `user-invocable: false` says otherwise.
func (s Skill) Slash() bool {
	if s.UserInvocable != nil {
		return *s.UserInvocable
	}
	return s.ModelDisabled
}

// Shadowed reports that the skill would be a slash command but its name is
// a built-in, so the built-in answers.
func (s Skill) Shadowed() bool { return s.Slash() && Builtin(s.Slug) }

// Commands are the skills offered as /<slug>, sorted, built-ins excluded.
func Commands(list []Skill) []Skill {
	var out []Skill
	for _, s := range list {
		if s.Slash() && !Builtin(s.Slug) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// ShadowNote names the built-ins that win over a skill of the same name,
// for the completion line and the palette; "" when nothing is shadowed.
func ShadowNote(list []Skill) string {
	var names []string
	for _, s := range list {
		if s.Shadowed() {
			names = append(names, "/"+s.Slug)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "built-in " + strings.Join(names, " ") + " wins over the skill of that name"
}

// Head returns the leading "/word" of a message (after any @mentions) and
// the text after it; ok is false when the message is not a slash command.
func Head(text string) (name, args string, ok bool) {
	t := strings.TrimSpace(text)
	for strings.HasPrefix(t, "@") {
		_, rest, _ := strings.Cut(t, " ")
		t = strings.TrimSpace(rest)
	}
	if !strings.HasPrefix(t, "/") {
		return "", "", false
	}
	head, tail, _ := strings.Cut(t[1:], " ")
	head = strings.ToLower(strings.TrimSpace(head))
	if head == "" || !ValidName(head) {
		return "", "", false
	}
	return head, strings.TrimSpace(tail), true
}

// Invocation resolves a message against the skills: the skill it names and
// the arguments. found is false when the message is no slash command, names
// a built-in (the composer handles those), or names no user-invocable skill.
func Invocation(text string, list []Skill) (s Skill, args string, found bool) {
	name, args, ok := Head(text)
	if !ok || Builtin(name) {
		return Skill{}, "", false
	}
	for _, k := range list {
		if k.Slug == name && k.Slash() {
			return k, args, true
		}
	}
	return Skill{}, "", false
}

// Complete returns the slash words (no "/") that start with prefix:
// built-ins first, then the skill commands.
func Complete(prefix string, list []Skill) []string {
	prefix = strings.ToLower(prefix)
	var out []string
	for _, b := range Builtins {
		if strings.HasPrefix(b, prefix) {
			out = append(out, b)
		}
	}
	for _, s := range Commands(list) {
		if strings.HasPrefix(s.Slug, prefix) {
			out = append(out, s.Slug)
		}
	}
	return out
}

// CommonPrefix is the longest prefix every word shares.
func CommonPrefix(words []string) string {
	if len(words) == 0 {
		return ""
	}
	p := words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

// Prompt is the turn text for an invoked skill: its activation block
// (what skill_activate returns) followed by the user's arguments.
func Prompt(s Skill, args string) string {
	if args == "" {
		return Activation(s) + "\n\nThe user ran /" + s.Slug + " with no arguments."
	}
	return Activation(s) + "\n\nThe user ran /" + s.Slug + " with: " + args
}
