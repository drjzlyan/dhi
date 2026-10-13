package bus

import "testing"

func TestStatusPhrase(t *testing.T) {
	for line, want := range map[string]string{
		"bash: go test ./...":                "running tests",
		"command: /bin/zsh -lc 'npm test'":   "running tests",
		"command: go build ./...":            "building",
		"bash: git status":                   "using git",
		"command: ls -la":                    "running a command",
		"file_change: app/money.go":          "editing files",
		"Edit: /w/app/money.go":              "editing files",
		"Read: /w/app/money.go":              "reading code",
		"tool: Grep TODO":                    "reading code",
		"tool: WebFetch https://go.dev":      "looking something up",
		"tool: TodoWrite":                    "planning",
		"retrying (attempt 1/3) in 30s…":     "retrying",
		"verify failed: go test (round 1)":   "fixing a failing check",
		"verified: go test ./... passed":     "checking the result",
		"Looking at the failing test first.": "thinking",
	} {
		if got := StatusPhrase(line); got != want {
			t.Errorf("StatusPhrase(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestChannelLabelAndKinds(t *testing.T) {
	if ChannelLabel("dm:scout") != "scout" || ChannelLabel("#general") != "#general" {
		t.Fatal("ChannelLabel")
	}
	if !IsDM("dm:x") || IsDM("#x") {
		t.Fatal("IsDM")
	}
	if Conversational(Message{Kind: KindStream}) || !Conversational(Message{Kind: KindAlert}) || !Conversational(Message{Kind: KindReply}) {
		t.Fatal("Conversational")
	}
}
