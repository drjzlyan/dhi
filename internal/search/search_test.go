package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRipgrepJSON(t *testing.T) {
	match := `{"type":"match","data":{"path":{"text":"/ws/alpha/src/main.go"},"line_number":42,"lines":{"text":"func main() {\n"},"submatches":[{"match":{"start":9,"end":13}}]}}`
	hit, ok := parseRipgrepJSON([]byte(match))
	if !ok {
		t.Fatal("match event not parsed")
	}
	if hit.Path != "/ws/alpha/src/main.go" || hit.Line != 42 || hit.Column != 9 {
		t.Errorf("hit = %+v", hit)
	}
	if hit.Text != "func main() {" {
		t.Errorf("Text = %q", hit.Text)
	}

	for _, nonMatch := range []string{
		`{"type":"begin","data":{"path":{"text":"/x"}}}`,
		`{"type":"end","data":{}}`,
		`{"type":"summary","data":{}}`,
		`not json at all`,
	} {
		if _, ok := parseRipgrepJSON([]byte(nonMatch)); ok {
			t.Errorf("non-match event accepted: %s", nonMatch)
		}
	}
}

// writeScript builds an executable stand-in for rg that emits canned NDJSON.
func writeScript(t *testing.T, dir, name, output string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRipgrepSearchStreamsHits(t *testing.T) {
	bin := writeScript(t, t.TempDir(), "rg", strings.Join([]string{
		`{"type":"begin","data":{"path":{"text":"/a"}}}`,
		`{"type":"match","data":{"path":{"text":"/a/f.go"},"line_number":3,"lines":{"text":"needle here\n"},"submatches":[{"match":{"start":0,"end":6}}]}}`,
		`{"type":"match","data":{"path":{"text":"/a/g.go"},"line_number":9,"lines":{"text":"x needle x\n"},"submatches":[{"match":{"start":2,"end":8}}]}}`,
		`{"type":"summary","data":{}}`,
	}, "\n"))

	hits, err := Ripgrep{Bin: bin}.Search(context.Background(), "needle", []string{"/ws"})
	if err != nil {
		t.Fatal(err)
	}
	var got []Hit
	for h := range hits {
		got = append(got, h)
	}
	if len(got) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(got), got)
	}
	if got[1].Line != 9 || got[1].Column != 2 {
		t.Errorf("second hit = %+v", got[1])
	}
}

func TestRipgrepRejectsBadInput(t *testing.T) {
	r := Ripgrep{Bin: "/bin/true"}
	if _, err := r.Search(context.Background(), "", []string{"/ws"}); err == nil {
		t.Error("empty query accepted")
	}
	if _, err := r.Search(context.Background(), "q", nil); err == nil {
		t.Error("no roots accepted")
	}
	if _, err := r.Search(context.Background(), "q", []string{"/missing-root-xyz"}); err == nil {
		t.Error("missing binary accepted")
	}
}

func TestRipgrepHonoursContextCancel(t *testing.T) {
	inf := "#!/bin/sh\nsleep 30\n"
	path := filepath.Join(t.TempDir(), "rg")
	os.WriteFile(path, []byte(inf), 0o755)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	hits, err := Ripgrep{Bin: path}.Search(ctx, "q", []string{"/ws"})
	if err != nil {
		t.Fatal(err)
	}
	for range hits { // closes when ctx fires
	}
}
