package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/principal"
)

func TestFitSectionsShrinksLowestPriorityFirst(t *testing.T) {
	secs := []section{
		{name: "trigger", text: strings.Repeat("t", 500), prio: 0, floor: 500},
		{name: "task", text: strings.Repeat("k", 400), prio: 1, floor: 200},
		{name: "history", text: "OLD" + strings.Repeat("h", 2000) + "NEW", prio: 3, floor: 100, tail: true},
		{name: "knowledge", text: strings.Repeat("x", 1000), prio: 6},
	}
	out := fitSections(secs, 1500)
	total := 0
	for _, s := range out {
		total += runeLen(s.text)
	}
	if total > 1500 {
		t.Fatalf("over budget: %d", total)
	}
	if runeLen(out[0].text) != 500 || runeLen(out[1].text) != 400 {
		t.Fatalf("trigger/task shrank before lower priorities: %d %d", runeLen(out[0].text), runeLen(out[1].text))
	}
	if out[3].text != "" && runeLen(out[3].text) > 50 {
		t.Fatalf("knowledge should give way first: %d", runeLen(out[3].text))
	}
	if !strings.HasSuffix(out[2].text, "NEW") || strings.Contains(out[2].text, "OLD") {
		t.Fatalf("history must keep its newest end: %q…", out[2].text[:40])
	}
	// Floors hold even when the budget cannot be met.
	tight := fitSections([]section{{text: strings.Repeat("t", 300), floor: 300}, {text: strings.Repeat("x", 300), prio: 2, floor: 100}}, 10)
	if runeLen(tight[0].text) != 300 || runeLen(tight[1].text) < 100 {
		t.Fatalf("floors broken: %d %d", runeLen(tight[0].text), runeLen(tight[1].text))
	}
}

func TestShrinkRunesHeadAndTail(t *testing.T) {
	s := "HEAD" + strings.Repeat("m", 500) + "TAIL"
	got := shrinkRunes(s, 120, false)
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || !strings.Contains(got, "trimmed for the prompt budget") || runeLen(got) > 120 {
		t.Fatalf("shrunk = %q (%d)", got, runeLen(got))
	}
}

func TestPromptStaysInBudgetAndBriefsDroppedHistory(t *testing.T) {
	h := newHarness(t, baseDoc())
	for i := 0; i < 80; i++ {
		mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "decision " + strings.Repeat("detail ", 60) + ". more"})
	}
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: true, MaxIterations: 3, Memory: MemoryTuning{PromptTokens: 4000}} }
	prompt, system := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	if n := runeLen(prompt) + runeLen(system); n > 4000*runesPerToken {
		t.Fatalf("prompt %d runes over the %d budget", n, 4000*runesPerToken)
	}
	if !strings.Contains(prompt, "earlier messages omitted) In brief:") || !strings.Contains(prompt, "· human: decision") {
		t.Fatalf("dropped history not briefed:\n%s", prompt[:min(len(prompt), 1500)])
	}
	if !strings.HasPrefix(prompt, "<request>\ngo\n</request>") {
		t.Fatalf("the trigger must lead, untrimmed: %q", prompt[:40])
	}
	assertTagsBalanced(t, prompt)
	assertTagsBalanced(t, system)
}

// assertTagsBalanced checks every <tag> opens at a line start and closes
// once, in order, with nothing left open (F-099): truncation never cuts a
// tag because tags are added after the fit.
func assertTagsBalanced(t *testing.T, s string) {
	t.Helper()
	var open []string
	for _, l := range strings.Split(s, "\n") {
		if len(l) < 3 || l[0] != '<' || l[len(l)-1] != '>' || strings.ContainsAny(l[1:len(l)-1], " <>") {
			continue
		}
		if name, ok := strings.CutPrefix(l[1:len(l)-1], "/"); ok {
			if len(open) == 0 || open[len(open)-1] != name {
				t.Fatalf("</%s> closes nothing open (open %v)", name, open)
			}
			open = open[:len(open)-1]
			continue
		}
		open = append(open, l[1:len(l)-1])
	}
	if len(open) != 0 {
		t.Fatalf("tags left open: %v", open)
	}
}

// F-099: the system half's tag order is fixed (stable before volatile),
// so the prompt cache keeps its prefix turn to turn.
func TestSystemTagsKeepAStableOrder(t *testing.T) {
	h := newHarness(t, baseDoc())
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	_, system := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	last := -1
	for _, tag := range []string{"<identity>", "<operating_rules>", "<who_you_work_for>"} {
		i := strings.Index(system, tag)
		if i < 0 || i < last {
			t.Fatalf("%s missing or out of order:\n%s", tag, system)
		}
		last = i
	}
	assertTagsBalanced(t, system)
	// A tight budget trims sections, never their tags.
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: true, MaxIterations: 3, Memory: MemoryTuning{PromptTokens: 300}} }
	prompt, system := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	assertTagsBalanced(t, system)
	assertTagsBalanced(t, prompt)
}

func TestSystemPromptCarriesTheProjectBrief(t *testing.T) {
	h := newHarness(t, baseDoc())
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	_, system := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	if strings.Contains(system, "Project brief") {
		t.Fatal("no brief on disk, yet the prompt has one")
	}
	if err := brief.Save(h.rt.cfg.WS.Root, brief.Brief{Name: "shop", Dont: "never touch prod"}); err != nil {
		t.Fatal(err)
	}
	_, system = h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	i, j := strings.Index(system, "Project: shop"), strings.Index(system, "<operating_rules>")
	if i < 0 || !strings.Contains(system, "Never: never touch prod") || j < i {
		t.Fatalf("brief missing or after grounding:\n%s", system)
	}
}

func TestSystemPromptSpeaksForThePrincipal(t *testing.T) {
	h := newHarness(t, baseDoc())
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	_, system := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	if !strings.Contains(system, "You work for the user (developer)") || !strings.Contains(system, "no tool logs") {
		t.Fatalf("default voice missing:\n%s", system)
	}
	h.rt.cfg.Tuning = func() Tuning {
		return Tuning{MaxIterations: 3, Principal: principal.Profile{Name: "Ada", Role: "founder", Format: "bullets"}}
	}
	_, system = h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
	if !strings.Contains(system, "You work for Ada (founder)") || !strings.Contains(system, "short bullet points") {
		t.Fatalf("principal voice missing:\n%s", system)
	}
}
