package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/projreplace"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// The search results buffer (F-168): `:grep` and `o` in the results pane
// write matches into one editable buffer, one `path:` header per file and
// `  line: text` rows (`  line- text` for context). Editing rows and :w
// applies them back to the source lines after a confirm card.

const (
	// searchBufMaxMatches caps the matches one buffer lists; more are cut
	// with a note rather than building a buffer nobody can read.
	searchBufMaxMatches = 1000
	// searchBufContext is the context lines kept around a match (`-C`).
	searchBufContext = 2
	// searchBufMaxContext bounds `-C` so a typo cannot list whole files.
	searchBufMaxContext = 20
	// searchBufName is the buffer's file under .dhi/tmp.
	searchBufName = "SEARCH_RESULTS"
	// searchSep divides two groups of rows that are not adjacent.
	searchSep = "  --"
)

// grepArgs is one :grep invocation; the last is what :grep! repeats.
type grepArgs struct {
	pattern string
	glob    string
	ctx     int
	regex   bool // the pattern is a regex (:grep); false for a fixed-string search
}

// searchBuf is the live search buffer: what the search saw, so :w can tell
// an edited row from an untouched one and a changed source line from a
// current one.
type searchBuf struct {
	args  grepArgs
	rows  map[string]map[int]string // abs path → 1-based line → text as listed
	paths map[string]string         // header label → abs path
	re    *regexp.Regexp            // match highlight; nil when the pattern did not compile
}

// searchApply is the confirm card before :w writes rows back.
type searchApply struct {
	plan    []projreplace.Change
	ignored int // buffer lines that are not rows of the search
}

// grepDoneMsg carries a finished :grep back to the UI goroutine.
type grepDoneMsg struct {
	gen    int
	args   grepArgs
	hits   []search.Hit
	capped bool
	err    error
}

func (m *Model) searchBufPath() string {
	return filepath.Join(m.ws.Root, workspace.DHIDir, "tmp", searchBufName)
}

// isSearchBuf reports whether e is the search buffer's editor.
func (m *Model) isSearchBuf(e *textbuf.Editor) bool {
	return m.ws != nil && m.sbuf != nil && e != nil && e.Path() == m.searchBufPath()
}

// splitArgs reads shell-like words: spaces separate, quotes group.
func splitArgs(s string) ([]string, error) {
	var out []string
	var cur []rune
	var quote rune
	has := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur = append(cur, r)
			}
		case r == '"' || r == '\'':
			quote, has = r, true
		case r == ' ' || r == '\t':
			if has || len(cur) > 0 {
				out = append(out, string(cur))
				cur, has = nil, false
			}
		default:
			cur = append(cur, r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c in the pattern", quote)
	}
	if has || len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out, nil
}

// parseGrep reads `[-C n] <pattern> [path-glob]`.
func parseGrep(s string) (grepArgs, error) {
	g := grepArgs{ctx: searchBufContext, regex: true}
	words, err := splitArgs(s)
	if err != nil {
		return g, err
	}
	if len(words) > 0 && strings.HasPrefix(words[0], "-C") {
		num := strings.TrimPrefix(strings.TrimPrefix(words[0], "-C"), "=")
		words = words[1:]
		if num == "" && len(words) > 0 {
			num, words = words[0], words[1:]
		}
		n, err := strconv.Atoi(num)
		if err != nil || n < 0 || n > searchBufMaxContext {
			return g, fmt.Errorf("-C takes 0 to %d context lines", searchBufMaxContext)
		}
		g.ctx = n
	}
	switch len(words) {
	case 0:
		return g, nil
	case 1:
		g.pattern = words[0]
	case 2:
		g.pattern, g.glob = words[0], words[1]
	default:
		return g, fmt.Errorf("usage: :grep [-C n] <pattern> [path-glob] (quote a pattern with spaces)")
	}
	return g, nil
}

// grepCommand handles :grep and :grep! (F-168).
func (m *Model) grepCommand(cmd string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(cmd), "grep")
	if !ok {
		return "", false
	}
	force := false
	if r, bang := strings.CutPrefix(rest, "!"); bang {
		rest, force = r, true
	}
	if rest != "" && rest[0] != ' ' {
		return "", false // :grepfoo is not ours
	}
	g, err := parseGrep(rest)
	if err != nil {
		return err.Error(), true
	}
	if g.pattern == "" {
		switch {
		case force && m.grepLast != nil:
			g = *m.grepLast
		case force:
			return "nothing to refresh — :grep <pattern> [path-glob] first", true
		default:
			return "usage: :grep [-C n] <pattern> [path-glob] (:grep! refreshes the last)", true
		}
	}
	if !force && m.sbufDirty() {
		return "the search buffer has edits not written — :w applies them, :grep! discards them", true
	}
	return m.runGrep(g), true
}

// sbufDirty reports unwritten edits in the open search buffer.
func (m *Model) sbufDirty() bool {
	if m.sbuf == nil || m.ws == nil {
		return false
	}
	t := m.bufferFor(m.searchBufPath())
	return t != nil && t.ed != nil && t.ed.Buffer().Dirty()
}

// runGrep starts the ripgrep search; the buffer appears when it finishes.
func (m *Model) runGrep(g grepArgs) string {
	gs, ok := m.searcher.(search.GlobSearcher)
	if !ok {
		return "grep is not available here (no ripgrep searcher)"
	}
	if m.grepCancel != nil {
		m.grepCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.grepCancel = cancel
	m.grepGen++
	gen := m.grepGen
	roots := make([]string, len(m.members))
	for i, mem := range m.members {
		roots[i] = mem.path
	}
	ch, err := gs.SearchGlob(ctx, g.pattern, roots, g.glob)
	if err != nil {
		cancel()
		return m.searchFailure(err)
	}
	m.grepLast = &g
	m.queue(func() tea.Msg {
		defer cancel()
		msg := grepDoneMsg{gen: gen, args: g}
		for h := range ch {
			if len(msg.hits) >= searchBufMaxMatches {
				msg.capped = true
				break
			}
			msg.hits = append(msg.hits, h)
		}
		return msg
	})
	return "searching…"
}

// searchFailure words a failed search start; a missing ripgrep opens the
// install card like the results pane does.
func (m *Model) searchFailure(err error) string {
	var missing *search.MissingToolError
	if errors.As(err, &missing) && m.askInstall(missing.Tool) {
		return "search needs ripgrep (" + missing.Tool + ") — install it, then search again"
	}
	return err.Error()
}

func (m *Model) onGrepDone(msg grepDoneMsg) {
	if msg.gen != m.grepGen {
		return // a newer :grep replaced this one
	}
	m.grepCancel = nil
	if msg.err != nil {
		m.say(msg.err.Error())
		return
	}
	m.say(m.showSearchBuffer(msg.args, msg.hits, msg.capped))
}

// say puts a line on the active editor's message row.
func (m *Model) say(s string) {
	if e := m.active(); e != nil {
		e.SetMessage(s)
	}
}

// openResultsAsBuffer is `o` in the results pane: the shown hits, as a buffer.
func (m *Model) openResultsAsBuffer() {
	if m.resultsTitle != "" || m.searching || len(m.hits) == 0 {
		return // a list (references, problems) or an unfinished search
	}
	hits := make([]search.Hit, len(m.hits))
	for i, h := range m.hits {
		hits[i] = h.hit
	}
	capped := len(hits) > searchBufMaxMatches
	if capped {
		hits = hits[:searchBufMaxMatches]
	}
	g := grepArgs{pattern: m.lastQueryText, ctx: searchBufContext, regex: m.lastQueryRegex}
	m.mode = modeNav
	m.say(m.showSearchBuffer(g, hits, capped))
}

// buildSearchBuf lays hits out as buffer text. Context comes from the
// files on disk through read; the rows record what was listed.
func buildSearchBuf(hits []search.Hit, ctx int, label func(abs string) string, read func(abs string) ([]string, error)) (text string, sb *searchBuf, matches, files int) {
	sb = &searchBuf{rows: map[string]map[int]string{}, paths: map[string]string{}}
	byPath := map[string][]search.Hit{}
	var order []string
	for _, h := range hits {
		if _, ok := byPath[h.Path]; !ok {
			order = append(order, h.Path)
		}
		byPath[h.Path] = append(byPath[h.Path], h)
	}
	sort.Strings(order)
	var out []string
	for _, abs := range order {
		hs := byPath[abs]
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].Line < hs[j].Line })
		src, _ := read(abs) // unreadable: the matches still list, without context
		isMatch := map[int]string{}
		for _, h := range hs {
			if _, dup := isMatch[h.Line]; !dup {
				isMatch[h.Line] = h.Text
				matches++
			}
		}
		lines := make([]int, 0, len(isMatch))
		for l := range isMatch {
			lines = append(lines, l)
		}
		sort.Ints(lines)
		// Windows of ±ctx around every match, merged when they touch.
		type span struct{ lo, hi int }
		var spans []span
		for _, l := range lines {
			lo, hi := max(l-ctx, 1), l+ctx
			if len(src) > 0 {
				hi = min(hi, len(src))
			} else {
				lo, hi = l, l
			}
			if n := len(spans); n > 0 && lo <= spans[n-1].hi+1 {
				spans[n-1].hi = max(spans[n-1].hi, hi)
			} else {
				spans = append(spans, span{lo, hi})
			}
		}
		name := label(abs)
		sb.paths[name] = abs
		sb.rows[abs] = map[int]string{}
		files++
		out = append(out, name+":")
		for i, sp := range spans {
			if i > 0 {
				out = append(out, searchSep)
			}
			for l := sp.lo; l <= sp.hi; l++ {
				if t, ok := isMatch[l]; ok {
					sb.rows[abs][l] = t
					out = append(out, "  "+strconv.Itoa(l)+": "+t)
				} else if l-1 < len(src) {
					sb.rows[abs][l] = src[l-1]
					out = append(out, "  "+strconv.Itoa(l)+"- "+src[l-1])
				}
			}
		}
	}
	return strings.Join(out, "\n"), sb, matches, files
}

// readSourceLines reads a file as the search saw it, from disk.
func readSourceLines(abs string) ([]string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), nil
}

// showSearchBuffer writes the buffer file and shows it in a tab.
func (m *Model) showSearchBuffer(g grepArgs, hits []search.Hit, capped bool) string {
	if len(hits) == 0 {
		return "no matches for " + strconv.Quote(g.pattern)
	}
	label := func(abs string) string {
		if vp, err := m.ws.VPathFor(abs); err == nil {
			return vp.String()
		}
		return abs
	}
	body, sb, matches, files := buildSearchBuf(hits, g.ctx, label, readSourceLines)
	sb.args = g
	if re, err := projreplace.Compile(g.pattern, g.regex); err == nil {
		sb.re = re
	}
	head := fmt.Sprintf("# :grep %s", strconv.Quote(g.pattern))
	if g.glob != "" {
		head += " " + g.glob
	}
	head += fmt.Sprintf(" — %s in %s · context %d", matchCount(matches), kit.Count(files, "file"), g.ctx)
	notes := []string{head, "# enter · gd opens the row · edit rows, :w asks before writing them to the files · :grep! refreshes"}
	if capped {
		notes = append(notes, fmt.Sprintf("# showing the first %d matches — narrow the pattern or add a path-glob", searchBufMaxMatches))
	}
	text := strings.Join(notes, "\n") + "\n" + body + "\n"

	p := m.searchBufPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err.Error()
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return err.Error()
	}
	existed := m.bufferFor(p) != nil
	m.sbuf = sb
	m.mode = modeNav
	m.open(&node{kind: nodeFile, path: p, name: searchBufName})
	t := m.bufferFor(p)
	e := m.active()
	if t == nil || e == nil || e.Path() != p {
		return "could not open the search buffer"
	}
	if existed {
		if err := e.Reload(); err != nil {
			return err.Error()
		}
	}
	t.syntax = nil // the buffer colors itself
	e.Buffer().SetCursor(textbuf.Pos{Line: len(notes) + 1})
	msg := matchCount(matches) + " in " + kit.Count(files, "file")
	if capped {
		msg += fmt.Sprintf(" (first %d shown)", searchBufMaxMatches)
	}
	return msg
}

var rowRe = regexp.MustCompile(`^  (\d+)([:-]) ?(.*)$`)

// planSearchEdits compares the buffer with what was listed: every row
// whose text changed is a Change. ignored counts lines that belong to no
// listed row, which are never written anywhere.
func (sb *searchBuf) planSearchEdits(lines []string) (plan []projreplace.Change, ignored int) {
	var cur string
	seen := map[string]bool{}
	for _, ln := range lines {
		switch {
		case ln == "" || strings.HasPrefix(ln, "#") || ln == searchSep:
			continue
		case !strings.HasPrefix(ln, " "):
			name, ok := strings.CutSuffix(ln, ":")
			abs := sb.paths[name]
			if !ok || abs == "" {
				cur = ""
				ignored++
				continue
			}
			cur = abs
			continue
		}
		mm := rowRe.FindStringSubmatch(ln)
		if mm == nil || cur == "" {
			ignored++
			continue
		}
		n, _ := strconv.Atoi(mm[1])
		old, listed := sb.rows[cur][n]
		key := cur + ":" + mm[1]
		if !listed || seen[key] {
			ignored++
			continue
		}
		seen[key] = true
		if mm[3] != old {
			plan = append(plan, projreplace.Change{Path: cur, Line: n, Old: old, New: mm[3]})
		}
	}
	return plan, ignored
}

// searchBufSaved runs after :w on the search buffer: it never writes a
// source file itself, it opens the card that asks.
func (m *Model) searchBufSaved(e *textbuf.Editor) {
	if m.sbuf == nil {
		return
	}
	plan, ignored := m.sbuf.planSearchEdits(strings.Split(strings.TrimSuffix(e.Buffer().Text(), "\n"), "\n"))
	if len(plan) == 0 {
		msg := "no edited rows — nothing to apply"
		if ignored > 0 {
			msg += " (" + kit.Count(ignored, "line") + " outside the listed rows ignored)"
		}
		e.SetMessage(msg)
		return
	}
	m.sbApply = &searchApply{plan: plan, ignored: ignored}
}

func (m *Model) searchApplyKey(key string) bool {
	switch key {
	case "y", "enter":
		plan := m.sbApply.plan
		m.sbApply = nil
		res := projreplace.Apply(plan, editorFiles{m})
		stale := map[string]bool{}
		for _, c := range res.Stale {
			stale[c.Path+":"+strconv.Itoa(c.Line)] = true
		}
		files := editorFiles{m}
		for _, c := range plan {
			if stale[c.Path+":"+strconv.Itoa(c.Line)] {
				continue
			}
			// Only a line that now reads as written is no longer an edit
			// (a file that failed to write keeps its rows pending).
			if cur, err := files.Lines(c.Path); err == nil && c.Line-1 < len(cur) && cur[c.Line-1] == c.New {
				m.sbuf.rows[c.Path][c.Line] = c.New
			}
		}
		msg := "applied " + kit.Count(res.Lines, "line") + " in " + kit.Count(res.Files, "file")
		if n := len(res.Stale); n > 0 {
			c := res.Stale[0]
			msg += fmt.Sprintf(" · %d skipped, the source changed since the search (first: %s:%d)", n, m.relLabel(c.Path), c.Line)
		}
		if n := len(res.Errs); n > 0 {
			msg += fmt.Sprintf(" · %s failed: %v", kit.Count(n, "file"), res.Errs[0])
		}
		if t := m.bufferFor(m.searchBufPath()); t != nil {
			t.ed.SetMessage(msg)
		}
		return true
	case "n", "esc":
		m.sbApply = nil
		if t := m.bufferFor(m.searchBufPath()); t != nil {
			t.ed.SetMessage("nothing applied — the rows stay edited in the buffer")
		}
		return true
	}
	return true // the card owns the keyboard
}

func (m *Model) relLabel(abs string) string {
	if vp, err := m.ws.VPathFor(abs); err == nil {
		return vp.String()
	}
	return abs
}

func (m *Model) searchApplyView(base string) string {
	a := m.sbApply
	perFile := map[string]int{}
	var order []string
	for _, c := range a.plan {
		if _, ok := perFile[c.Path]; !ok {
			order = append(order, c.Path)
		}
		perFile[c.Path]++
	}
	lines := []string{theme.Hint().Render("write " + kit.Count(len(a.plan), "edited line") + " to " + kit.Count(len(order), "file") + "?"), ""}
	for _, p := range order {
		open := ""
		if m.bufferFor(p) != nil {
			open = theme.Hint().Render("  open buffer: changes in place, undo with u")
		}
		lines = append(lines, "  "+kit.ClipEllipsis(m.relLabel(p), 46)+"  "+kit.Count(perFile[p], "line")+open)
	}
	lines = append(lines, "", theme.Hint().Render("a line whose source changed since the search is skipped and reported"))
	if a.ignored > 0 {
		lines = append(lines, theme.Hint().Render(kit.Count(a.ignored, "line")+" outside the listed rows will be ignored"))
	}
	lines = append(lines, "", theme.Hint().Render("y apply · n cancel"))
	md := kit.Modal{Title: "apply search edits", Lines: lines, Width: 72}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}

// searchRowTarget reads the buffer line under the cursor as a source
// location: a row, or a file header (line 1).
func (m *Model) searchRowTarget(e *textbuf.Editor) (string, int, bool) {
	sb := m.sbuf
	c := e.Buffer().Cursor()
	ln := e.Buffer().Line(c.Line)
	if name, ok := strings.CutSuffix(ln, ":"); ok && !strings.HasPrefix(ln, " ") && sb.paths[name] != "" {
		return sb.paths[name], 1, true
	}
	if mm := rowRe.FindStringSubmatch(ln); mm != nil {
		n, _ := strconv.Atoi(mm[1])
		for i := c.Line; i >= 0; i-- { // the header above names the file
			if name, ok := strings.CutSuffix(e.Buffer().Line(i), ":"); ok && !strings.HasPrefix(e.Buffer().Line(i), " ") && sb.paths[name] != "" {
				return sb.paths[name], n, true
			}
		}
	}
	return "", 0, false
}

// openSearchRow is enter / gd on a row: the file, on that line.
func (m *Model) openSearchRow(e *textbuf.Editor) {
	abs, line, ok := m.searchRowTarget(e)
	if !ok {
		e.SetMessage("not a result row")
		return
	}
	m.open(&node{kind: nodeFile, path: abs, name: filepath.Base(abs)})
	if t := m.active(); t != nil && t.Path() == abs {
		col := 0
		if mm := rowRe.FindStringSubmatch(e.Buffer().Line(e.Buffer().Cursor().Line)); mm != nil && m.sbuf.re != nil && mm[2] == ":" {
			if loc := m.sbuf.re.FindStringIndex(mm[3]); loc != nil {
				col = len([]rune(mm[3][:loc[0]]))
			}
		}
		t.Buffer().SetCursor(textbuf.Pos{Line: line - 1, Col: col})
	}
}

// searchBufStyled colors one tab-expanded buffer line.
func (m *Model) searchBufStyled(e *textbuf.Editor, line string) (string, bool) {
	if !m.isSearchBuf(e) {
		return "", false
	}
	switch {
	case strings.HasPrefix(line, "#") || line == searchSep:
		return theme.Hint().Render(line), true
	case !strings.HasPrefix(line, " "):
		if name, ok := strings.CutSuffix(line, ":"); ok && m.sbuf.paths[name] != "" {
			return theme.TabActive().Render(line), true
		}
		return line, true
	}
	mm := rowRe.FindStringSubmatch(line)
	if mm == nil {
		return line, true
	}
	prefix := line[:len(line)-len(mm[3])]
	if mm[2] == "-" {
		return theme.Hint().Render(prefix) + theme.TextDim().Render(mm[3]), true
	}
	body := mm[3]
	if m.sbuf.re != nil {
		var b strings.Builder
		at := 0
		for _, loc := range m.sbuf.re.FindAllStringIndex(body, -1) {
			if loc[1] == loc[0] {
				continue
			}
			b.WriteString(body[at:loc[0]])
			b.WriteString(searchStyle.Foreground(theme.Current.Warning).Render(body[loc[0]:loc[1]]))
			at = loc[1]
		}
		b.WriteString(body[at:])
		body = b.String()
	}
	return theme.Hint().Render(prefix) + body, true
}

// matchCount renders "1 match" / "2 matches".
func matchCount(n int) string {
	if n == 1 {
		return "1 match"
	}
	return strconv.Itoa(n) + " matches"
}
