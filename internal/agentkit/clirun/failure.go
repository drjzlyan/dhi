package clirun

import "strings"

// Failure names why a CLI run failed (F-065), so the runtime can react to
// the cause instead of resending the same prompt blind.
type Failure string

// Failure classes.
const (
	FailAuth      Failure = "auth"       // not logged in / bad key: retrying cannot help
	FailRateLimit Failure = "rate_limit" // 429 / overloaded / usage limit: back off longer
	FailOverflow  Failure = "overflow"   // prompt or context too long: retry compacted
	FailTimeout   Failure = "timeout"    // the turn's deadline is spent
	FailNoFinal   Failure = "no_final"   // the stream ended without a terminal line
	FailCrash     Failure = "crash"      // anything else
)

// Retryable reports whether another attempt can change the outcome.
func (f Failure) Retryable() bool { return f != FailAuth && f != FailTimeout }

// failurePatterns are matched lower-cased against the run error and the
// stream's error lines, first class wins. Order matters: overflow text
// ("too long") must not be mistaken for anything else, and auth beats a
// generic rate phrase.
var failurePatterns = []struct {
	class Failure
	subs  []string
}{
	{FailOverflow, []string{
		"prompt is too long", "context window", "context length", "maximum context",
		"too many tokens", "argv budget",
	}},
	{FailAuth, []string{
		"invalid api key", "/login", "not logged in", "unauthorized", " 401",
		"authentication", "codex login", "please log in",
	}},
	{FailRateLimit, []string{
		" 429", "rate limit", "rate_limit", "overloaded", " 529", "usage limit",
		"too many requests",
	}},
}

// Classify names the failure of one run from its timeout flag and its
// error text — the adapter's Finalize reason or the CLI's stderr tail.
// Stream error lines are deliberately ignored: they are mostly the
// agent's own failed tool commands (a `git push` printing "Authentication
// failed", test output mentioning 429), which say nothing about the CLI.
func Classify(timedOut bool, errText string) Failure {
	if timedOut {
		return FailTimeout
	}
	hay := " " + strings.ToLower(errText)
	for _, p := range failurePatterns {
		for _, s := range p.subs {
			if strings.Contains(hay, s) {
				return p.class
			}
		}
	}
	if strings.Contains(hay, "without a final event") {
		return FailNoFinal
	}
	return FailCrash
}
