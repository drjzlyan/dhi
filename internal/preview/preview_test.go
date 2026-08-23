package preview

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
)

func TestRenderHeadingsAndEmphasis(t *testing.T) {
	out, err := Render("# Hello\n\nSome **bold** and _italic_ text.\n", 60)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"Hello", "bold", "italic"} {
		if !strings.Contains(plain, want) {
			t.Errorf("output missing %q:\n%s", want, plain)
		}
	}
}

func TestRenderGFMTablesAndTasks(t *testing.T) {
	md := "| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n- [ ] todo\n"
	out, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"done", "todo", "1"} {
		if !strings.Contains(plain, want) {
			t.Errorf("GFM element missing %q:\n%s", want, plain)
		}
	}
}

func TestEmptyInput(t *testing.T) {
	out, err := Render("   \n", 40)
	if err != nil || out != "" {
		t.Errorf("empty render = %q, %v", out, err)
	}
}

func TestIsMarkdown(t *testing.T) {
	for path, want := range map[string]bool{
		"readme.md":      true,
		"README.MD":      true,
		"notes.markdown": true,
		"x/y/file.mdown": true,
		"main.go":        false,
		"md":             false,
		"file.md.bak":    false,
	} {
		if got := IsMarkdown(path); got != want {
			t.Errorf("IsMarkdown(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestDeterministicOutput(t *testing.T) {
	md := "# T\n\ntext\n"
	a, _ := Render(md, 50)
	b, _ := Render(md, 50)
	if a != b {
		t.Error("same input+width produced different output")
	}
}
