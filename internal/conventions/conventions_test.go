package conventions

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultsValidate(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestExpandBranch(t *testing.T) {
	vars := map[string]string{"slug": "fix-login", "id": "7", "user": "Dhi Raj", "date": "20260101"}
	cases := []struct {
		pattern, want string
		wantErr       string
	}{
		{"task/{slug}", "task/fix-login", ""},
		{"{user}/{date}/{slug}", "Dhi-Raj/20260101/fix-login", ""},
		{"feature/{ticket}", "", "unknown placeholder {ticket}"},
		{"", "", "empty branch pattern"},
		{"task/{slug}/", "", "slash-bounded"},
		{"task//{slug}", "", "// or .."},
	}
	for _, c := range cases {
		got, err := ExpandBranch(c.pattern, vars)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%q: err = %v, want %q", c.pattern, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q = %q, %v; want %q", c.pattern, got, err, c.want)
		}
	}
}

func TestCheckCommit(t *testing.T) {
	conv := Commit{Format: FormatConventional, MaxSubject: 72}
	tick := Commit{Format: FormatTicket, TicketPattern: `[A-Z]+-\d+`, MaxSubject: 72}
	cases := []struct {
		name string
		c    Commit
		msg  string
		ok   bool
	}{
		{"free any", Commit{Format: FormatFree, MaxSubject: 72}, "whatever", true},
		{"empty", Commit{Format: FormatFree, MaxSubject: 72}, "  ", false},
		{"conv ok", conv, "feat(ui): add wizard", true},
		{"conv breaking", conv, "fix!: drop api", true},
		{"conv bad", conv, "Added a thing", false},
		{"ticket ok", tick, "PROJ-12: fix login", true},
		{"ticket bad", tick, "fix login", false},
		{"too long", Commit{Format: FormatFree, MaxSubject: 20}, strings.Repeat("x", 21), false},
	}
	for _, c := range cases {
		if err := c.c.CheckCommit(c.msg); (err == nil) != c.ok {
			t.Errorf("%s: err=%v ok=%v", c.name, err, c.ok)
		}
	}
}

func TestFinalizeCoAuthorOnce(t *testing.T) {
	c := Commit{CoAuthor: "Bot <bot@x.io>"}
	once := c.Finalize("feat: x")
	twice := c.Finalize(once)
	if once != twice || strings.Count(once, "Co-Authored-By") != 1 {
		t.Fatalf("not idempotent:\n%q\n%q", once, twice)
	}
	if got := (Commit{}).Finalize("a"); got != "a\n" {
		t.Fatalf("no co-author = %q", got)
	}
}

func TestValidateRejects(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Branch.Task = "x/{nope}" },
		func(c *Config) { c.Commit.Format = "wild" },
		func(c *Config) { c.Commit.TicketPattern = "(" },
		func(c *Config) { c.Commit.MaxSubject = 5 },
		func(c *Config) { c.Commit.CoAuthor = "no-email" },
		func(c *Config) { c.Copyright.Enabled = true },
	}
	for i, mut := range bad {
		c := Defaults()
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestCopyrightHeader(t *testing.T) {
	c := Copyright{Enabled: true, Holder: "Acme Inc", License: "MIT"}
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if got := c.HeaderFor("a.go", now); got != "// Copyright (c) 2026 Acme Inc\n// SPDX-License-Identifier: MIT\n" {
		t.Errorf("go header = %q", got)
	}
	if got := c.HeaderFor("a.md", now); got != "" {
		t.Errorf("unknown language = %q", got)
	}
	if got := (Copyright{}).HeaderFor("a.go", now); got != "" {
		t.Errorf("disabled = %q", got)
	}
	if got := c.HeaderFor("s.css", now); !strings.HasPrefix(got, "/*\n") || !strings.HasSuffix(got, " */\n") {
		t.Errorf("css = %q", got)
	}
}

func TestEnsureHeader(t *testing.T) {
	c := Copyright{Enabled: true, Holder: "Acme", Year: "2025"}
	now := time.Now()
	out := c.EnsureHeader("m.py", "#!/usr/bin/env python\nprint(1)\n", now)
	if !strings.HasPrefix(out, "#!/usr/bin/env python\n# Copyright (c) 2025 Acme\n") {
		t.Errorf("shebang not kept first: %q", out)
	}
	if again := c.EnsureHeader("m.py", out, now); again != out {
		t.Errorf("not idempotent: %q", again)
	}
	got := c.EnsureHeader("a.go", "package a\n", now)
	if !strings.HasPrefix(got, "// Copyright (c) 2025 Acme\n\npackage a") {
		t.Errorf("go = %q", got)
	}
}

func TestRender(t *testing.T) {
	if got := Render("{title} ({slug}) {typo}", map[string]string{"title": "T", "slug": "s"}); got != "T (s) {typo}" {
		t.Errorf("render = %q", got)
	}
}
