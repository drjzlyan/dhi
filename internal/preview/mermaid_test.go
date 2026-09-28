package preview

import (
	"strings"
	"testing"
)

func TestIsMermaid(t *testing.T) {
	for _, p := range []string{"diagram.mmd", "flow.MERMAID", "a/b.mmd"} {
		if !IsMermaid(p) {
			t.Errorf("IsMermaid(%q) = false", p)
		}
	}
	for _, p := range []string{"a.md", "a.txt", "a.png"} {
		if IsMermaid(p) {
			t.Errorf("IsMermaid(%q) = true", p)
		}
	}
}

func TestRenderMermaidFlowchart(t *testing.T) {
	src := "graph TD\n  A[Start] --> B{Choice}\n  B -->|yes| C[Done]\n  B -.-> A\n  %% a comment\n"
	out, err := RenderMermaid(src, 80)
	if err != nil {
		t.Fatalf("RenderMermaid: %v", err)
	}
	for _, want := range []string{"Start ──▶ Choice", "Choice ──▶ Done  (yes)", "Choice ┈▶ Start"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "comment") {
		t.Fatalf("comment leaked:\n%s", out)
	}
	// Pure: identical source yields identical output.
	out2, _ := RenderMermaid(src, 80)
	if out != out2 {
		t.Fatal("RenderMermaid not deterministic")
	}
}

func TestRenderMermaidSequence(t *testing.T) {
	src := "sequenceDiagram\n  participant Alice\n  Alice->>Bob: Hello\n  Bob-->>Alice: Hi\n"
	out, _ := RenderMermaid(src, 80)
	for _, want := range []string{"• Alice", "Alice ──▶ Bob : Hello", "Bob ──▶ Alice : Hi"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderMermaidUnknownAndEmpty(t *testing.T) {
	out, _ := RenderMermaid("mindmap\n  root\n  child\n", 80)
	if !strings.Contains(out, "• root") {
		t.Fatalf("unknown diagram not outlined:\n%s", out)
	}
	empty, err := RenderMermaid("   ", 80)
	if err != nil || empty != "" {
		t.Fatalf("empty = %q, %v", empty, err)
	}
	out, _ = RenderMermaid("graph TD\n", 80)
	if !strings.Contains(out, "empty diagram") {
		t.Fatalf("header-only diagram = %q", out)
	}
}
