package hooks

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/mcp"
)

// Wrap fires PreToolUse and PostToolUse around every call h serves: DHI's
// own tools and the bridged third-party ones alike. It sits outside the
// secret redaction, so a hook never sees a granted secret's value.
func Wrap(h mcp.Handler, r *Runner) mcp.Handler {
	if r == nil || (!r.Active(PreToolUse) && !r.Active(PostToolUse)) {
		return h
	}
	return hooked{Handler: h, r: r}
}

type hooked struct {
	mcp.Handler
	r *Runner
}

func (h hooked) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	pre := h.r.Fire(ctx, Input{Event: PreToolUse, Tool: name, ToolInput: args})
	if pre.Blocked {
		return "tool " + name + " blocked by plugin hook " + pre.By + ":\n" + pre.Reason, true, nil
	}
	out, isErr, err := h.Handler.CallTool(ctx, name, args)
	if err != nil {
		return out, isErr, err
	}
	post := h.r.Fire(ctx, Input{Event: PostToolUse, Tool: name, ToolInput: args, ToolResponse: out})
	if post.Context != "" {
		out += "\n" + post.Context
	}
	return out, isErr, nil
}

// NativeSettings renders the tool hooks (Pre/PostToolUse) of plugins in the
// vendor CLI's own settings JSON, for a CLI that takes a settings file per
// run (claude: --settings <file>). The vendor format is the CLI's format,
// so groups pass through; each command first exports CLAUDE_PLUGIN_ROOT
// for its plugin. It returns the body and how many hooks it carries (0 =
// write nothing). The boundary events stay with DHI so their output can be
// fenced.
func NativeSettings(plugins []Plugin) (string, int) {
	type entry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type grp struct {
		Matcher string  `json:"matcher,omitempty"`
		Hooks   []entry `json:"hooks"`
	}
	by := map[Event][]grp{}
	n := 0
	for _, p := range plugins {
		for _, hk := range p.Set.Hooks {
			if !hk.Event.IsTool() {
				continue
			}
			cmd := "export CLAUDE_PLUGIN_ROOT=" + shQuote(p.Dir) + " DHI_PLUGIN_ROOT=" + shQuote(p.Dir) + "; " + hk.Command
			secs := int(hk.Timeout.Seconds())
			if secs < 1 {
				secs = 1
			}
			by[hk.Event] = append(by[hk.Event], grp{Matcher: hk.Matcher, Hooks: []entry{{"command", cmd, secs}}})
			n++
		}
	}
	if n == 0 {
		return "", 0
	}
	keys := make([]string, 0, len(by))
	for ev := range by {
		keys = append(keys, string(ev))
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, k := range keys {
		out[k] = by[Event(k)]
	}
	b, _ := json.MarshalIndent(map[string]any{"hooks": out}, "", "  ")
	return string(b) + "\n", n
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
