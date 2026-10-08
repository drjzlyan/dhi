package tutorial

import "sort"

// Action events (F-051). Surfaces report the real thing the user just did —
// a file opened, a buffer saved, a task created — and a step that awaits
// "do:<event>" advances on it instead of on a button press. Names live here,
// in the pure package, so a lesson can never wait for an event no surface
// emits: ValidAwait checks this table, and a test pins each name to its
// emitter.
const (
	EvEditorOpen   = "editor.open"   // a file was opened in the editor
	EvEditorInsert = "editor.insert" // a buffer entered insert mode
	EvEditorSave   = "editor.save"   // :w wrote a file
	EvEditorFormat = "editor.format" // :fmt formatted a buffer
	EvEditorPair   = "editor.pair"   // :pair started a pairing
	EvEditorTest   = "editor.test"   // :test started a run
	EvEditorBreak  = "editor.break"  // a breakpoint was set
	EvEditorDebug  = "editor.debug"  // a debug session started

	EvTaskCreated = "task.created" // a task was created on the board

	EvReviewOpened    = "review.opened"    // a review was started or opened
	EvReviewComment   = "review.comment"   // a draft comment was saved
	EvReviewSubmitted = "review.submitted" // a review was sent
)

// Events maps each action event to what it means (for docs and errors).
var Events = map[string]string{
	EvEditorOpen:      "a file is opened in the Editor",
	EvEditorInsert:    "a buffer enters insert mode",
	EvEditorSave:      "a file is saved with :w",
	EvEditorFormat:    "a buffer is formatted with :fmt",
	EvEditorPair:      "pairing with an employee starts (:pair)",
	EvEditorTest:      "a test run starts (:test)",
	EvEditorBreak:     "a breakpoint is set (:break)",
	EvEditorDebug:     "a debug session starts (:debug)",
	EvTaskCreated:     "a task is created on the board",
	EvReviewOpened:    "a review is started or opened",
	EvReviewComment:   "a draft comment is saved in the Reviewer",
	EvReviewSubmitted: "a review is sent",
}

// EventNames lists the events in order.
func EventNames() []string {
	out := make([]string, 0, len(Events))
	for k := range Events {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
