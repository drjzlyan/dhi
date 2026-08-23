package editor

import (
	"os"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/preview"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func openMarkdown(t *testing.T) *Model {
	t.Helper()
	ws, _ := setupWorkspace(t)

	vp, err := workspace.ParseVPath("beta/README.md")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ws.Resolve(vp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, []byte("# DHI Guide\n\n- [x] shipped\n- plain **bold** text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("test", ws)
	m.Resize(100, 30)
	return m
}

func TestPreviewToggleAndContent(t *testing.T) {
	m := openMarkdown(t)

	// navigate to README.md via finder
	feed(m, "/")
	typeKeys(m, "README")
	feed(m, "enter")

	if !preview.IsMarkdown(m.active().Path()) {
		t.Fatalf("fixture not markdown: %s", m.active().Path())
	}

	feed(m, "ctrl+g")
	v := plainView(m)
	if !strings.Contains(v, "preview") {
		t.Errorf("preview title missing:\n%s", v)
	}
	for _, want := range []string{"DHI Guide", "shipped", "bold"} {
		if !strings.Contains(v, want) {
			t.Errorf("rendered markdown missing %q:\n%s", want, v)
		}
	}

	feed(m, "ctrl+g") // back to buffer
	if !strings.Contains(plainView(m), "NORMAL") {
		t.Error("ctrl+g did not return to buffer view")
	}
}

func TestPreviewLiveUpdate(t *testing.T) {
	m := openMarkdown(t)
	feed(m, "/")
	typeKeys(m, "README")
	feed(m, "enter")
	feed(m, "ctrl+g")

	beforeHash := m.previewKey
	feed(m, "ctrl+g") // to buffer
	feed(m, "A")      // append at end of current line
	typeKeys(m, "-edited")
	feed(m, "esc")
	feed(m, "ctrl+g") // preview again

	v := ansi.Strip(m.View())
	if !strings.Contains(v, "-edited") {
		t.Errorf("preview did not reflect edit:\n%s", v)
	}
	if m.previewKey == beforeHash {
		t.Error("preview cache not invalidated on edit")
	}
}

func TestNonMarkdownHint(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter", "down", "down", "enter") // open app.go
	feed(m, "ctrl+g")
	if m.previewOn {
		t.Fatal("preview enabled for non-markdown")
	}
	if !strings.Contains(plainView(m), "not a markdown file") {
		t.Errorf("hint missing:\n%s", plainView(m))
	}
}

func TestPreviewGolden(t *testing.T) {
	m := openMarkdown(t)
	feed(m, "/", "R", "E", "A", "D", "M", "E")
	feed(m, "enter")
	feed(m, "ctrl+g")
	golden.Snapshot(t, "editor_preview_100x30", m.View())
}
