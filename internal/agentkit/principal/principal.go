// Package principal describes the person DHI's employees work for (F-081):
// who they are, how they write and how they like to read. It renders the
// one voice block every agent's system prompt carries, so replies read the
// way the user reads and work written in their name sounds like them.
package principal

import (
	"fmt"
	"strings"
)

// Choices for each style setting; the first is the default.
var (
	Tones    = []string{"plain", "formal", "casual"}
	Lengths  = []string{"brief", "normal", "detailed"}
	Orders   = []string{"summary-first", "steps-first"}
	Formats  = []string{"prose", "bullets"}
	Details  = []string{"normal", "brief", "detailed"}
	Roles    = []string{"developer", "tech lead", "founder", "engineering manager", "product manager", "student"}
	Language = "English"
)

// Profile is the principal. Zero fields take the defaults above.
type Profile struct {
	Name  string // git user.name; "" = "the user"
	Email string
	Role  string

	// Writing: how work written in their name should sound (commits, PR
	// descriptions, task comments).
	Tone     string
	Length   string
	Emoji    bool
	Language string

	// Reading: how replies to them should be shaped.
	Order  string
	Format string
	Detail string
}

func or(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

// Normalized fills every empty field with its default.
func (p Profile) Normalized() Profile {
	p.Role = or(p.Role, Roles[0])
	p.Tone = or(p.Tone, Tones[0])
	p.Length = or(p.Length, Lengths[1])
	p.Language = or(p.Language, Language)
	p.Order = or(p.Order, Orders[0])
	p.Format = or(p.Format, Formats[0])
	p.Detail = or(p.Detail, Details[0])
	return p
}

// Who names the principal for prompts.
func (p Profile) Who() string {
	if n := strings.TrimSpace(p.Name); n != "" {
		return n
	}
	return "the user"
}

// Voice is the system-prompt block: who the employee works for, how its
// final reply should read, and how to write in the principal's name. It
// replaces the fixed teammate-voice note (F-072), which it includes.
func (p Profile) Voice() string {
	p = p.Normalized()
	who := p.Who()
	var b strings.Builder
	fmt.Fprintf(&b, "You work for %s (%s). What you deliver is their work: it ships under their git identity "+
		"and they answer for it in review, so hold it to the standard they would.\n", who, p.Role)

	b.WriteString("Your final reply is posted to team chat (or the task's thread) for people to read. " +
		"Write it like a teammate: what you did and what is left, no tool logs, no command transcripts, no internal reasoning. " +
		"Put code, diffs and commands in fenced ``` blocks with a language tag. " +
		"Use channel_post only for a brief update a person needs before you finish.\n")

	fmt.Fprintf(&b, "%s reads replies best ", who)
	if p.Order == "steps-first" {
		b.WriteString("walking through what you did in order, ending with the result")
	} else {
		b.WriteString("with the outcome in the first sentence, then the details")
	}
	if p.Format == "bullets" {
		b.WriteString(", as short bullet points")
	} else {
		b.WriteString(", in short plain paragraphs")
	}
	switch p.Detail {
	case "brief":
		b.WriteString(", kept to a few lines.\n")
	case "detailed":
		b.WriteString(", with the reasoning and trade-offs spelled out.\n")
	default:
		b.WriteString(".\n")
	}

	fmt.Fprintf(&b, "When you write in %s's name (commit messages, PR descriptions, task comments): a %s tone, %s, %s, in %s.",
		who, p.Tone, lengthPhrase(p.Length), emojiPhrase(p.Emoji), p.Language)
	return b.String()
}

func lengthPhrase(l string) string {
	switch l {
	case "brief":
		return "as short as the change allows"
	case "detailed":
		return "explaining the why and the trade-offs"
	}
	return "a short subject and a body that explains why"
}

func emojiPhrase(on bool) string {
	if on {
		return "emoji welcome"
	}
	return "no emoji"
}

// Persona is the library persona that suits how the principal reads, for
// employees created after onboarding ("" = keep the per-role choice).
func (p Profile) Persona() string {
	switch p.Normalized().Detail {
	case "brief":
		return "terse"
	case "detailed":
		return "meticulous"
	}
	return ""
}
