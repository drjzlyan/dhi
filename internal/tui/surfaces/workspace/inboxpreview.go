package workspace

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/inbox"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// inboxPreviewMinWidth is the inbox panel width at which the selected
// item's preview docks beside the list (F-055, the M24 wide-layout
// deferral) — about a 150-column terminal with the rail.
const inboxPreviewMinWidth = 120

// inboxKindTitle names each attention kind for the preview header.
var inboxKindTitle = map[inbox.ItemKind]string{
	inbox.Approval:     "Approval needed",
	inbox.Proposal:     "Session proposal",
	inbox.RunFailed:    "Run failed",
	inbox.Dependency:   "Cross-repo change",
	inbox.InReview:     "Ready for review",
	inbox.AgentMessage: "Message for you",
}

// inboxPreview renders the selected item in full: a title, its non-empty
// facts as aligned rows, the message or error text wrapped, and what
// enter does. Built from the item's fields, so every kind previews
// without a per-kind layout to keep in sync.
func inboxPreview(it inbox.Item, w int) []string {
	title := inboxKindTitle[it.Kind]
	if title == "" {
		title = string(it.Kind)
	}
	out := []string{theme.TabActive().Render(title), ""}
	fact := func(k, v string) {
		if v == "" || v == "0" {
			return
		}
		out = append(out, theme.TextDim().Render(fmt.Sprintf("%-9s", k))+" "+kit.ClipEllipsis(v, w-11))
	}
	fact("agent", it.Agent)
	if it.Op != "" {
		fact("wants", string(it.Op))
	}
	fact("target", it.Target)
	fact("channel", it.Channel)
	fact("task", it.TaskSlug)
	fact("title", it.TaskTitle)
	fact("assignee", it.Assignee)
	fact("review", it.ReviewID)
	fact("proposal", it.ProposalName)
	fact("from", it.ProposalCaller)
	if it.FromMember != "" {
		fact("change", it.FromMember+" → "+it.ToMember+" ("+it.DepKind+")")
	}
	if it.Run.ID != "" {
		fact("run", it.Run.ID)
		fact("error", it.Run.Error)
	}
	if text := strings.TrimSpace(it.Text); text != "" {
		out = append(out, "")
		for _, l := range kit.WrapWords(text, w-2) {
			out = append(out, theme.TextStyle().Render(l))
		}
	}
	act := "enter jump"
	switch it.Kind {
	case inbox.Approval:
		act = "enter review the request"
	case inbox.RunFailed:
		act = "enter open the run replay"
	case inbox.AgentMessage:
		act = "enter open the thread"
	}
	return append(out, "", theme.Hint().Render(act+" · z snooze"))
}
