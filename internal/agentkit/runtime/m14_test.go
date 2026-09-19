package runtime

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// stubEnvMulti writes several stub CLIs into one bin dir (the base
// stubEnv writes only claude).
func stubEnvMulti(t *testing.T, scripts map[string]string) (binDir, dumpDir string, cliEnv []string) {
	t.Helper()
	dir := t.TempDir()
	dump := t.TempDir()
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{
		"PATH=" + dir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"DUMPDIR=" + dump,
	}
	return dir, dump, env
}

func newHarnessMulti(t *testing.T, agentDoc string, scripts map[string]string) *harness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	binDir, dumpDir, cliEnv := stubEnvMulti(t, scripts)
	m, err := manifest.Parse("scout", []byte(agentDoc))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	rt, err := New(Config{
		WS: ws, Bus: b, Approvals: tools.NewApprovals(),
		Sandbox: sandbox.Noop{}, CLIs: stubRegistry(binDir), CLIEnv: cliEnv,
	}, []*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{ws: ws, bus: b, rt: rt, binDir: binDir, dumpDir: dumpDir}
}

// stdinStub echoes argv into the dump and pipes its stdin there too.
const stdinStub = `#!/bin/sh
: > "$DUMPDIR/cli-args.dump"
for a in "$@"; do
  printf '%s' "$a" | base64 >> "$DUMPDIR/cli-args.dump"
  printf '\n' >> "$DUMPDIR/cli-args.dump"
done
cat | base64 > "$DUMPDIR/stdin.dump"
printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}'
exit 0
`

// silentStub exits 0 with a valid empty stream (for refusal paths).
const silentStub = `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0}'
exit 0
`

func joinArgs(t *testing.T, h *harness) string {
	t.Helper()
	return strings.Join(readArgs(t, h), "\n")
}

// allFilesSearcher is a fake KB searcher: one hit per file under the
// roots (enough for the scorer to match index entries).
type allFilesSearcher struct{}

func (allFilesSearcher) Search(ctx context.Context, query string, roots []string) (<-chan search.Hit, error) {
	ch := make(chan search.Hit)
	go func() {
		defer close(ch)
		for _, root := range roots {
			_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					ch <- search.Hit{Path: p, Line: 1, Text: "hit"}
				}
				return nil
			})
		}
	}()
	return ch, nil
}

// TestMemoryAndKBReachTheSystemBlock pins M14 P1: journal tail, notes,
// and KB hits ride the assembled system block (visible in the argv).
func TestMemoryAndKBReachTheSystemBlock(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Memory = memory.Open(h.ws)
	kb, err := knowledge.Open(h.ws, knowledge.Auto, allFilesSearcher{})
	if err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Knowledge = kb
	if err := h.rt.cfg.Memory.Append("scout", "lesson", "always run gofmt before commit"); err != nil {
		t.Fatal(err)
	}
	if err := h.rt.cfg.Memory.WriteNotes("scout", "prefers short PRs"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := kb.Contribute(knowledge.Contribution{
		Title: "deploy runbook", Body: "deploy runs through scripts/ship.sh",
		Author: "you", Importance: 5,
	}); err != nil {
		t.Fatal(err)
	}

	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout deploy runbook status please"})
	h.rt.Handle(context.Background(), trig)
	waitReply(t, replies)

	joined := joinArgs(t, h)
	for _, want := range []string{
		"Your memory (persistent across turns)",
		"always run gofmt before commit",
		"prefers short PRs",
		// The KB section carries the matched snippet (the fake searcher
		// returns "hit" per entry file), not the entry body.
		"Relevant knowledge base entries:",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("system block missing %q:\n%s", want, joined)
		}
	}
}

// TestOversizedPromptRefusesNamed pins the ADR-0011 refusal: an adapter
// without a verified stdin path refuses an oversized prompt by name
// instead of dying on E2BIG.
func TestOversizedPromptRefusesNamed(t *testing.T) {
	doc := strings.Replace(baseDoc(), `runtime = "claude"`, `runtime = "opencode"`, 1)
	h := newHarnessMulti(t, doc, map[string]string{"claude": echoStub, "opencode": silentStub})
	big := strings.Repeat("x", 200<<10)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout " + big})
	err := h.rt.Turn(context.Background(), "scout", trig)
	if err == nil || !strings.Contains(err.Error(), "no verified stdin delivery") {
		t.Fatalf("err = %v, want the named stdin refusal", err)
	}
}

// TestOversizedPromptRidesStdin pins the claude stdin path: the blob
// reaches the process stdin in the shared tagged shape; argv stays slim.
func TestOversizedPromptRidesStdin(t *testing.T) {
	h := newHarnessMulti(t, baseDoc(), map[string]string{"claude": stdinStub})
	big := strings.Repeat("y", 200<<10)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout " + big})
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatalf("turn: %v", err)
	}
	for _, a := range readArgs(t, h) {
		if len(a) > 64<<10 {
			t.Fatalf("giant argv element leaked (%d bytes)", len(a))
		}
	}
	raw, err := os.ReadFile(filepath.Join(h.dumpDir, "stdin.dump"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	blob := string(decoded)
	if !strings.Contains(blob, "<dhi-system>") || !strings.Contains(blob, big) {
		t.Fatalf("stdin blob missing shape/content (len %d)", len(blob))
	}
}

// TestCliPromptMemoryDegradeNamesIt pins the named degrade: a KB
// search failure surfaces inside the block, never silently dropped.
func TestCliPromptMemoryDegradeNamesIt(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Knowledge = failingKB{}
	prompt, system := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"],
		bus.Message{Channel: "#general", Author: bus.Human, Text: "hello"})
	if !strings.Contains(system, "Knowledge base unavailable") {
		t.Fatalf("named KB degrade missing:\n%s", system)
	}
	if prompt == "" {
		t.Fatal("prompt lost")
	}
}

type failingKB struct{}

func (failingKB) Search(ctx context.Context, query string, limit int) ([]knowledge.Hit, error) {
	return nil, context.DeadlineExceeded
}
func (failingKB) Contribute(c knowledge.Contribution) (knowledge.Status, string, error) {
	return knowledge.StatusQueued, "", nil
}
func (failingKB) Pending() ([]knowledge.Queued, error) { return nil, nil }
func (failingKB) Approve(id string) (knowledge.Entry, error) {
	return knowledge.Entry{}, nil
}

// compile-time guard: the registry seam is what tests stub.
var _ = clirun.NewRegistry
