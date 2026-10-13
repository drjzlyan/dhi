package kit

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"github.com/drjzlyan/dhi/internal/tui/syntax"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Chat markdown (F-072): message bodies in channels, threads and task
// comments render as formatted text — fenced code as a shaded block with
// a language label and syntax colours, lists with hanging indents,
// quotes, headings, inline code, links. The renderer walks goldmark's
// AST and paints with theme tokens only, so every theme re-colours it
// and no row ever carries a terminal-default background hole.

var (
	mdOnce   sync.Once
	mdParser goldmark.Markdown

	mdCacheMu sync.Mutex
	mdCache   = map[mdKey]string{}
)

type mdKey struct {
	src   string
	width int
	theme string
}

const mdCacheMax = 1024

// mdHints are cheap tells that a message uses markdown; plain prose
// skips the renderer and keeps the transcript's own word wrap.
var mdHints = regexp.MustCompile("```|`[^`\n]+`|\\*\\*[^*\n]+\\*\\*|(?m)^\\s{0,3}(#{1,6} |[-*+] |\\d+[.)] |> |\\|.*\\|)|\\[[^\\]\n]+\\]\\([^)\n]+\\)|(?m)^~~~")

// LooksLikeMarkdown reports whether s carries markdown worth rendering.
func LooksLikeMarkdown(s string) bool { return mdHints.MatchString(s) }

// RenderMarkdown renders src to styled lines no wider than width cells,
// joined by "\n". Output is cached per (src, width, theme).
func RenderMarkdown(src string, width int) string {
	width = clamp(width, 8, 4096)
	key := mdKey{src, width, theme.Current.Name}
	mdCacheMu.Lock()
	if out, ok := mdCache[key]; ok {
		mdCacheMu.Unlock()
		return out
	}
	mdCacheMu.Unlock()

	out := strings.Join(renderMarkdownLines(src, width), "\n")

	mdCacheMu.Lock()
	if len(mdCache) >= mdCacheMax {
		mdCache = map[mdKey]string{}
	}
	mdCache[key] = out
	mdCacheMu.Unlock()
	return out
}

// ChatMarkdown is the transcript seam: markdown-looking text renders,
// plain prose returns "" so the transcript wraps it itself.
func ChatMarkdown(src string, width int) string {
	if !LooksLikeMarkdown(src) {
		return ""
	}
	return RenderMarkdown(src, width)
}

func parser() goldmark.Markdown {
	mdOnce.Do(func() {
		mdParser = goldmark.New(goldmark.WithExtensions(
			extension.Table, extension.Strikethrough, extension.TaskList, extension.Linkify))
	})
	return mdParser
}

func renderMarkdownLines(src string, width int) []string {
	source := []byte(src)
	doc := parser().Parser().Parse(text.NewReader(source))
	r := &mdRenderer{src: source}
	lines := r.blocks(doc, width)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

type mdRenderer struct {
	src []byte
}

// blocks renders n's block children separated by a blank line.
func (r *mdRenderer) blocks(n gast.Node, width int) []string {
	return r.blocksSep(n, width, true)
}

// blocksSep renders n's block children, separated by a blank line when
// sep is set (a tight list item keeps its paragraph and sub-list close).
func (r *mdRenderer) blocksSep(n gast.Node, width int, sep bool) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		bl := r.block(c, width)
		if len(bl) == 0 {
			continue
		}
		if sep && len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, bl...)
	}
	return out
}

func (r *mdRenderer) block(n gast.Node, width int) []string {
	switch n := n.(type) {
	case *gast.Paragraph, *gast.TextBlock:
		return wrapSegs(r.inlines(n, nil), width)
	case *gast.Heading:
		st := theme.TextStyle().Bold(true)
		if n.Level <= 2 {
			st = theme.AccentText().Bold(true)
		}
		return wrapSegs(r.inlines(n, &st), width)
	case *gast.FencedCodeBlock:
		lang := ""
		if n.Info != nil {
			lang = strings.Fields(string(n.Info.Segment.Value(r.src)) + " ")[0]
		}
		return codeBlock(r.rawLines(n), lang, width)
	case *gast.CodeBlock:
		return codeBlock(r.rawLines(n), "", width)
	case *gast.Blockquote:
		bar := theme.RuleText().Render("▌ ")
		var out []string
		for _, l := range r.blocks(n, width-2) {
			out = append(out, bar+Restyle(theme.TextDim().Italic(true), l))
		}
		return out
	case *gast.List:
		return r.list(n, width)
	case *gast.ThematicBreak:
		return []string{theme.RuleText().Render(strings.Repeat("─", width))}
	case *east.Table:
		return r.table(n, width)
	case *gast.HTMLBlock:
		var out []string
		for _, l := range r.rawLines(n) {
			out = append(out, WrapWords(l, width)...)
		}
		return styleAll(theme.TextDim(), out)
	}
	if n.HasChildren() {
		return r.blocks(n, width)
	}
	return nil
}

// list renders items with a marker column and hanging indent; tight
// lists keep items adjacent, loose ones separate them with a blank row.
func (r *mdRenderer) list(n *gast.List, width int) []string {
	var out []string
	num := n.Start
	if num == 0 {
		num = 1
	}
	markW := 2
	if n.IsOrdered() {
		count := 0
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			count++
		}
		markW = len(strconv.Itoa(num+count-1)) + 2
	}
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		mark := theme.AccentText().Render(padTo("•", markW))
		if n.IsOrdered() {
			mark = theme.AccentText().Render(padTo(strconv.Itoa(num)+".", markW))
			num++
		} else if hasCheckbox(item) {
			mark = strings.Repeat(" ", markW) // the [ ] box is the marker
		}
		body := r.blocksSep(item, width-markW, !n.IsTight)
		if !n.IsTight && len(out) > 0 {
			out = append(out, "")
		}
		if len(body) == 0 {
			body = []string{""}
		}
		indent := strings.Repeat(" ", markW)
		for i, l := range body {
			if i == 0 {
				out = append(out, mark+l)
			} else {
				out = append(out, indent+l)
			}
		}
	}
	return out
}

// table renders a GFM table with padded columns, a bold header and a
// rule under it; cells clip with an ellipsis when the table is too wide.
func (r *mdRenderer) table(n *east.Table, width int) []string {
	var rows [][]string
	header := -1
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		if _, ok := row.(*east.TableHeader); ok {
			header = len(rows)
		}
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, segsText(r.inlines(c, nil)))
		}
		rows = append(rows, cells)
	}
	cols := 0
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	if cols == 0 {
		return nil
	}
	widths := make([]int, cols)
	for _, row := range rows {
		for i, c := range row {
			widths[i] = max(widths[i], runeWidth(c))
		}
	}
	// Shrink the widest columns until the table fits (3-cell separators).
	for total(widths)+3*(cols-1) > width {
		wi := 0
		for i, w := range widths {
			if w > widths[wi] {
				wi = i
			}
		}
		if widths[wi] <= 3 {
			break
		}
		widths[wi]--
	}
	sep := theme.RuleText().Render(" │ ")
	var out []string
	for ri, row := range rows {
		var parts []string
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			cell = padTo(ClipEllipsis(cell, widths[i]+1), widths[i])
			if runeWidth(cell) > widths[i] {
				cell = clip(cell, widths[i])
			}
			st := theme.TextStyle()
			if ri == header {
				st = st.Bold(true)
			}
			parts = append(parts, st.Render(cell))
		}
		out = append(out, strings.Join(parts, sep))
		if ri == header {
			var rule []string
			for _, w := range widths {
				rule = append(rule, strings.Repeat("─", w))
			}
			out = append(out, theme.RuleText().Render(strings.Join(rule, "─┼─")))
		}
	}
	return out
}

// hasCheckbox reports a GFM task-list item.
func hasCheckbox(item gast.Node) bool {
	if fc := item.FirstChild(); fc != nil {
		_, ok := fc.FirstChild().(*east.TaskCheckBox)
		return ok
	}
	return false
}

func total(ws []int) int {
	t := 0
	for _, w := range ws {
		t += w
	}
	return t
}

func (r *mdRenderer) rawLines(n gast.Node) []string {
	var sb strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(r.src))
	}
	s := strings.TrimRight(sb.String(), "\n")
	return strings.Split(strings.ReplaceAll(s, "\t", "    "), "\n")
}

// codeBlock paints code on the code shade across the full width, with
// the language as a muted label on its first row. Long lines wrap hard
// (code is never reflowed on spaces) and keep their syntax colours.
func codeBlock(lines []string, lang string, width int) []string {
	bg := theme.InsetBg() // one step off every transcript background
	inner := max(width-2, 4)
	var out []string
	if lang != "" {
		label := theme.TextMuted().Render(lang)
		out = append(out, PaintRow(" "+strings.Repeat(" ", max(inner-runeWidth(lang), 0))+label, width, bg))
	}
	lexed := syntax.LexLang(lang, strings.Join(lines, "\n"))
	for i, l := range lines {
		var spans syntax.Line
		if lexed != nil && i < len(lexed) {
			spans = lexed[i]
		} else {
			spans = syntax.Line{{Text: l}}
		}
		for _, chunk := range splitSpans(spans, inner) {
			out = append(out, PaintRow(" "+renderSpans(chunk), width, bg))
		}
	}
	return out
}

// splitSpans cuts a lexed line into chunks of at most w cells.
func splitSpans(l syntax.Line, w int) []syntax.Line {
	var out []syntax.Line
	var cur syntax.Line
	cw := 0
	for _, sp := range l {
		var b strings.Builder
		for _, r := range sp.Text {
			rw := runewidth.RuneWidth(r)
			if cw+rw > w && cw > 0 {
				if b.Len() > 0 {
					cur = append(cur, syntax.Span{Text: b.String(), Class: sp.Class})
					b.Reset()
				}
				out = append(out, cur)
				cur, cw = nil, 0
			}
			b.WriteRune(r)
			cw += rw
		}
		if b.Len() > 0 {
			cur = append(cur, syntax.Span{Text: b.String(), Class: sp.Class})
		}
	}
	return append(out, cur)
}

func renderSpans(l syntax.Line) string {
	var sb strings.Builder
	for _, s := range l {
		if st, ok := s.Class.Style(); ok {
			sb.WriteString(st.Render(s.Text))
		} else {
			sb.WriteString(theme.TextStyle().Render(s.Text))
		}
	}
	return sb.String()
}

// ---- inline ----

// seg is a run of inline text in one style; nl marks a hard break.
type seg struct {
	text string
	st   lipgloss.Style
	nl   bool
}

// inlines flattens n's inline children into styled segments. base (nil
// = body text) is the style the children inherit.
func (r *mdRenderer) inlines(n gast.Node, base *lipgloss.Style) []seg {
	st := theme.TextStyle()
	if base != nil {
		st = *base
	}
	var out []seg
	r.walkInline(n, st, &out)
	return out
}

func (r *mdRenderer) walkInline(n gast.Node, st lipgloss.Style, out *[]seg) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *gast.Text:
			*out = append(*out, seg{text: string(c.Segment.Value(r.src)), st: st})
			if c.HardLineBreak() {
				*out = append(*out, seg{nl: true})
			} else if c.SoftLineBreak() {
				*out = append(*out, seg{text: " ", st: st})
			}
		case *gast.String:
			*out = append(*out, seg{text: string(c.Value), st: st})
		case *gast.CodeSpan:
			code := lipgloss.NewStyle().Foreground(theme.Current.Accent3).Background(theme.Current.BgInset)
			var b strings.Builder
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if tx, ok := t.(*gast.Text); ok {
					b.Write(tx.Segment.Value(r.src))
				}
			}
			*out = append(*out, seg{text: b.String(), st: code})
		case *gast.Emphasis:
			s := st.Italic(true)
			if c.Level >= 2 {
				s = st.Bold(true)
			}
			r.walkInline(c, s, out)
		case *east.Strikethrough:
			r.walkInline(c, st.Strikethrough(true), out)
		case *gast.Link:
			link := lipgloss.NewStyle().Foreground(theme.Current.Info).Underline(true)
			before := len(*out)
			r.walkInline(c, link, out)
			label := segsText((*out)[before:])
			if dest := string(c.Destination); dest != "" && dest != label {
				*out = append(*out, seg{text: " (" + dest + ")", st: theme.TextMuted()})
			}
		case *gast.AutoLink:
			link := lipgloss.NewStyle().Foreground(theme.Current.Info).Underline(true)
			*out = append(*out, seg{text: string(c.URL(r.src)), st: link})
		case *gast.Image:
			*out = append(*out, seg{text: "[image: " + segsText(r.inlines(c, nil)) + "]", st: theme.TextMuted()})
		case *east.TaskCheckBox:
			box := "[ ] "
			if c.IsChecked {
				box = "[x] "
			}
			*out = append(*out, seg{text: box, st: theme.AccentText()})
		case *gast.RawHTML:
			for i := 0; i < c.Segments.Len(); i++ {
				s := c.Segments.At(i)
				*out = append(*out, seg{text: string(s.Value(r.src)), st: theme.TextDim()})
			}
		default:
			r.walkInline(c, st, out)
		}
	}
}

func segsText(ss []seg) string {
	var b strings.Builder
	for _, s := range ss {
		if s.nl {
			b.WriteString(" ")
			continue
		}
		b.WriteString(s.text)
	}
	return b.String()
}

// word is one wrap unit: adjacent styled pieces with no space between.
type word []seg

func (w word) width() int {
	n := 0
	for _, s := range w {
		n += runeWidth(s.text)
	}
	return n
}

func (w word) render() string {
	var b strings.Builder
	for _, s := range w {
		b.WriteString(s.st.Render(s.text))
	}
	return b.String()
}

// wrapSegs word-wraps styled segments to width; words longer than the
// line hard-split by cell. A space keeps the style of the text before
// it (an inline code span never paints the gap after it).
func wrapSegs(ss []seg, width int) []string {
	var lines [][]word
	var line []word
	var cur word
	lineW := 0
	flushWord := func() {
		if len(cur) == 0 {
			return
		}
		ww := cur.width()
		gap := 0
		if len(line) > 0 {
			gap = 1
		}
		if lineW+gap+ww > width && len(line) > 0 {
			lines = append(lines, line)
			line, lineW, gap = nil, 0, 0
		}
		for ww > width { // hard-split a word longer than a line
			head, tail := splitWord(cur, width-lineW-gap)
			line = append(line, head)
			lines = append(lines, line)
			line, lineW, gap = nil, 0, 0
			cur = tail
			ww = cur.width()
		}
		line = append(line, cur)
		lineW += gap + ww
		cur = nil
	}
	for _, s := range ss {
		if s.nl {
			flushWord()
			lines = append(lines, line)
			line, lineW = nil, 0
			continue
		}
		parts := strings.Split(s.text, " ")
		for i, p := range parts {
			if i > 0 {
				flushWord()
			}
			if p != "" {
				cur = append(cur, seg{text: p, st: s.st})
			}
		}
	}
	flushWord()
	if len(line) > 0 || len(lines) == 0 {
		lines = append(lines, line)
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		var b strings.Builder
		for i, w := range l {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(w.render())
		}
		out = append(out, b.String())
	}
	return out
}

// splitWord cuts w after n cells (n >= 1).
func splitWord(w word, n int) (word, word) {
	n = max(n, 1)
	var head, tail word
	used := 0
	for _, s := range w {
		if used >= n {
			tail = append(tail, s)
			continue
		}
		var hb, tb strings.Builder
		for _, r := range s.text {
			rw := runewidth.RuneWidth(r)
			if used+rw > n || tb.Len() > 0 {
				tb.WriteRune(r)
				continue
			}
			hb.WriteRune(r)
			used += rw
		}
		if hb.Len() > 0 {
			head = append(head, seg{text: hb.String(), st: s.st})
		}
		if tb.Len() > 0 {
			tail = append(tail, seg{text: tb.String(), st: s.st})
		}
	}
	return head, tail
}

func styleAll(st lipgloss.Style, ls []string) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = st.Render(l)
	}
	return out
}
