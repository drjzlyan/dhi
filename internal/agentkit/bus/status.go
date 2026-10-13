package bus

import "strings"

// StatusPhrase turns a relayed stream line into the few plain words a
// typing indicator shows ("running tests", "editing files" — F-072).
// The raw line stays on the bus for the trace view.
func StatusPhrase(line string) string {
	l := strings.ToLower(strings.TrimSpace(line))
	head, rest, _ := strings.Cut(l, ":")
	rest = strings.TrimSpace(rest)
	tool := "" // a "tool: <Name> …" line is judged by the tool's name only
	if head == "tool" {
		if f := strings.Fields(rest); len(f) > 0 {
			tool = f[0]
		}
	}
	isCmd := head == "command" || head == "bash" || head == "run" ||
		containsAny(tool, "bash", "shell", "run", "exec")
	switch {
	case strings.HasPrefix(l, "retrying"):
		return "retrying"
	case strings.HasPrefix(l, "verify failed"):
		return "fixing a failing check"
	case strings.HasPrefix(l, "verified"), strings.HasPrefix(l, "could not verify"):
		return "checking the result"
	case strings.HasPrefix(l, "created worktree"):
		return "setting up a worktree"
	case strings.HasPrefix(l, "learned"), strings.HasPrefix(l, "reinforced"):
		return "noting a lesson"
	case isCmd && containsAny(rest, "test", "pytest", "vitest", "jest"):
		return "running tests"
	case isCmd && containsAny(rest, "build", "compile", "tsc", "vet", "lint", "fmt"):
		return "building"
	case isCmd && containsAny(rest, "git "):
		return "using git"
	case isCmd:
		return "running a command"
	case containsAny(tool, "todo", "plan"):
		return "planning"
	case head == "file_change", head == "write", head == "edit", head == "notebookedit",
		containsAny(tool, "edit", "write", "patch", "apply"):
		return "editing files"
	case head == "read", head == "glob", head == "grep",
		containsAny(tool, "read", "grep", "glob", "search", "list", "find"):
		return "reading code"
	case containsAny(tool, "web", "fetch", "browse"):
		return "looking something up"
	case head == "tool" || head == "tool failed" || strings.HasPrefix(head, "tool "):
		return "using tools"
	}
	return "thinking"
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}
