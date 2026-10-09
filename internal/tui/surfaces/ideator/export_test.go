package ideator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// TestExportArtifact pins F-059: an artifact leaves the canvas as a repo
// file (refusing to overwrite until confirmed), a task card with the text
// as its first comment, or a request to an agent to file it in the tracker.
func TestExportArtifact(t *testing.T) {
	m, ws, st, crew, b := newSurface(t)
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.tasks = ts
	sess, _ := st.Create("Launch", "", []string{"scout"})
	writeArtifact(t, m, sess.ID, "plan.md", "# Launch plan\n\nShip Thursday.\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secCanvas

	// Repo file.
	m.HandleKey("X")
	if m.form.kind != fExport || m.form.fields[1].text() != "api/docs/plan.md" {
		t.Fatalf("export form = %+v", m.form)
	}
	m.HandleKey("enter")
	dst := filepath.Join(ws.Root, "api", "docs", "plan.md")
	if data, err := os.ReadFile(dst); err != nil || !strings.Contains(string(data), "Ship Thursday") {
		t.Fatalf("exported file: %q %v", data, err)
	}
	m.HandleKey("X")
	m.HandleKey("enter")
	if !m.form.armed || !strings.Contains(m.form.err, "exists") {
		t.Fatalf("an existing file must ask first: %+v", m.form)
	}
	m.HandleKey("enter")
	if m.form.kind != fNone {
		t.Fatal("the second enter must overwrite")
	}

	// Task card.
	m.HandleKey("X")
	m.HandleKey("right")
	if got := m.form.fields[1].text(); got != "plan" {
		t.Fatalf("task default slug = %q", got)
	}
	m.HandleKey("enter")
	tk, ok := ts.Get("plan")
	if !ok || tk.Title != "Launch plan" || len(tk.Comments) != 1 || !strings.Contains(tk.Comments[0].Text, "Ship Thursday") {
		t.Fatalf("task = %+v", tk)
	}

	// Tracker via an agent.
	m.HandleKey("X")
	m.HandleKey("right")
	m.HandleKey("right")
	if got := m.form.fields[1].text(); got != "scout" {
		t.Fatalf("tracker default agent = %q", got)
	}
	m.HandleKey("enter")
	hist := b.History(sess.Channel, 0)
	if len(hist) == 0 || !strings.Contains(hist[len(hist)-1].Text, "@scout please file") {
		t.Fatalf("no filing request in %s: %+v", sess.Channel, hist)
	}
	if len(crew.handled) == 0 {
		t.Fatal("the agent turn was not requested")
	}
}
