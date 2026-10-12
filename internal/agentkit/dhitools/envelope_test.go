package dhitools

import "testing"

// F-067: the autonomy envelope lifts Ask only for work inside the bound
// task's worktree.
func TestEnvelopeCovers(t *testing.T) {
	wt := "/ws/.dhi/worktrees/fix-login"
	in := Deps{Envelope: wt, EnvelopeTask: "fix-login", Workdir: wt}
	off := Deps{Workdir: wt}
	elsewhere := Deps{Envelope: wt, EnvelopeTask: "fix-login", Workdir: "/ws"}
	slug := func(s string) any {
		return struct {
			Slug string `json:"slug"`
		}{s}
	}
	cases := []struct {
		name string
		d    Deps
		tool string
		dec  any
		want bool
	}{
		{"run in the worktree", in, "run", runPlan{argv: []string{"go", "test"}}, true},
		{"commit in the worktree", in, "git_commit", nil, true},
		{"run outside the worktree", elsewhere, "run", runPlan{}, false},
		{"write inside", in, "write", writePlan{abs: wt + "/api/x.go"}, true},
		{"write at the root of it", in, "write", writePlan{abs: wt}, true},
		{"write on the shared checkout", in, "write", writePlan{abs: "/ws/api/x.go"}, false},
		{"write escaping via ..", in, "write", writePlan{abs: wt + "/../other/x.go"}, false},
		{"sibling with a shared prefix", in, "write", writePlan{abs: wt + "-evil/x.go"}, false},
		{"patch inside", in, "patch", patchPlan{abs: wt + "/a.go"}, true},
		{"comment on the bound card", in, "task_comment", slug("fix-login"), true},
		{"status on another card", in, "task_status", slug("other"), false},
		{"push is never covered", in, "git_push", nil, false},
		{"pr is never covered", in, "pr_open", nil, false},
		{"channel post is not covered", in, "channel_post", nil, false},
		{"envelope off (chat turn)", off, "run", runPlan{}, false},
	}
	for _, c := range cases {
		if got := c.d.envelopeCovers(c.tool, c.dec); got != c.want {
			t.Errorf("%s: envelopeCovers = %v, want %v", c.name, got, c.want)
		}
	}
}
