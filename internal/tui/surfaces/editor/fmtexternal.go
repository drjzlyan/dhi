package editor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// formatTimeout bounds an external formatter run.
const formatTimeout = 10 * time.Second

// formatExternal pipes the buffer through the language's configured
// formatter (stdin → stdout) and applies a changed result as one undo
// group. The command is the user's explicit choice; it runs with the same
// hermetic environment as the terminal drawer, so a bare name is looked up
// in DHI's PATH, never the host's (ADR-0011).
func (m *Model) formatExternal(ed *textbuf.Editor, l langserver.Language) string {
	if len(m.termEnv) == 0 {
		return "format: no hermetic environment — finish the first-run install"
	}
	bin, ok := lookIn(m.termEnv, l.Format[0])
	if !ok {
		return "format: " + l.Format[0] + " not found in DHI's environment — use an absolute path in editor.languages." + l.ID + ".formatter"
	}
	buf := ed.Buffer()
	text := buf.Text()
	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()
	args := make([]string, 0, len(l.Format)-1)
	for _, a := range l.Format[1:] {
		args = append(args, strings.ReplaceAll(a, "{file}", ed.Path()))
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = m.termEnv
	cmd.Dir = filepath.Dir(ed.Path())
	cmd.Stdin = strings.NewReader(text)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.WaitDelay = 500 * time.Millisecond
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(errb.String())
		if ctx.Err() != nil {
			detail = "timed out after " + formatTimeout.String()
		} else if detail == "" {
			detail = err.Error()
		}
		if len(detail) > 120 {
			detail = detail[:120] + "…"
		}
		return "format failed: " + filepath.Base(bin) + ": " + detail
	}
	if out.Len() == 0 {
		return "format failed: " + filepath.Base(bin) + " printed nothing (kept your text)"
	}
	if out.String() == text {
		return ""
	}
	last := buf.LineCount() - 1
	whole := lsp.TextEdit{
		Range: lsp.Range{
			End: lsp.Position{Line: last, Character: len([]rune(buf.Line(last)))},
		},
		NewText: out.String(),
	}
	applyTextEdits(buf, []lsp.TextEdit{whole})
	m.lspSync()
	return ""
}

// lookIn resolves name against the PATH inside env (never the process's).
func lookIn(env []string, name string) (string, bool) {
	if filepath.IsAbs(name) {
		_, err := os.Stat(name)
		return name, err == nil
	}
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}
