// F-033 Part B: mermaid artifacts render as a deterministic ASCII
// outline. A terminal-native product cannot rasterize a full diagram, so
// the canvas renders what the diagram *says* — its nodes and edges in
// order — rather than pretending a glyph layout. The outline is pure:
// same source yields identical lines, so goldens and the live preview
// (keyed on the artifact's content hash) stay stable. The structured
// node/edge editor is the recorded follow-on (F-033 Deferred).
package preview

import (
	"fmt"
	"strings"
)

// IsMermaid reports whether a path should get the diagram treatment.
func IsMermaid(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".mmd", ".mermaid"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// diagramKind names the leading mermaid directive.
type diagramKind string

const (
	kindGraph    diagramKind = "graph"
	kindSequence diagramKind = "sequence"
	kindOther    diagramKind = "diagram"
)

func classify(header string) diagramKind {
	h := strings.ToLower(strings.TrimSpace(header))
	switch {
	case strings.HasPrefix(h, "graph"), strings.HasPrefix(h, "flowchart"):
		return kindGraph
	case strings.HasPrefix(h, "sequencediagram"):
		return kindSequence
	default:
		return kindOther
	}
}

// RenderMermaid converts mermaid source into a styled-agnostic ASCII
// outline. width is accepted for symmetry with Render but the outline
// uses one line per edge, so it is only used to trim oversized labels.
func RenderMermaid(source string, width int) (string, error) {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(source) == "" {
		return "", nil
	}
	kind := kindOther
	start := 0
	for i, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			kind = classify(t)
			start = i + 1
			break
		}
	}
	limit := width
	if limit < 20 {
		limit = 20
	}
	var out []string
	switch kind {
	case kindGraph:
		out = graphOutline(lines[start:], limit)
	case kindSequence:
		out = sequenceOutline(lines[start:], limit)
	default:
		out = plainOutline(lines, limit)
	}
	if len(out) == 0 {
		return "(empty diagram)\n", nil
	}
	return strings.Join(out, "\n") + "\n", nil
}

// graphOutline renders flowchart edges as "from ──▶ to (label)" and
// standalone node declarations as a node line. Node labels declared on
// one line (A[Start]) resolve wherever the id is referenced.
func graphOutline(lines []string, limit int) []string {
	type edge struct {
		from, op, to, label string
		standalone          string
	}
	var edges []edge
	labels := map[string]string{}
	for _, raw := range lines {
		l := strings.TrimSpace(strings.TrimSuffix(raw, ";"))
		if l == "" || strings.HasPrefix(l, "%%") || hasDirective(l) {
			continue
		}
		left, op, right, ok := splitEdge(l)
		if !ok {
			recordNode(labels, l)
			edges = append(edges, edge{standalone: l})
			continue
		}
		recordNode(labels, left)
		recordNode(labels, right)
		edges = append(edges, edge{from: left, op: op, to: right, label: edgeLabel(l)})
	}
	resolve := func(tok string) string {
		if lbl, ok := labels[nodeID(tok)]; ok {
			return lbl
		}
		return labelOf(tok)
	}
	var out []string
	for _, e := range edges {
		if e.standalone != "" {
			out = append(out, "• "+clip(resolve(e.standalone), limit))
			continue
		}
		from := clip(resolve(e.from), limit/2)
		to := clip(resolve(e.to), limit/2)
		arrow := "──▶"
		switch e.op {
		case "---", "----":
			arrow = "───"
		case "-.->", "-.-":
			arrow = "┈▶"
		case "==>", "===>":
			arrow = "══▶"
		}
		if e.label != "" {
			out = append(out, fmt.Sprintf("%s %s %s  (%s)", from, arrow, to, clip(e.label, limit/3)))
		} else {
			out = append(out, fmt.Sprintf("%s %s %s", from, arrow, to))
		}
	}
	return out
}

// recordNode learns a node's display label from a declaration token.
func recordNode(labels map[string]string, tok string) {
	id := nodeID(tok)
	if id == "" {
		return
	}
	if _, ok := labels[id]; ok {
		return
	}
	if lbl := labelOf(tok); lbl != "" {
		labels[id] = lbl
	}
}

// nodeID is the leading identifier of a node token (`A` in `A[Start]`).
func nodeID(tok string) string {
	tok = strings.TrimSpace(tok)
	end := 0
	for _, r := range tok {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			end++
			continue
		}
		break
	}
	return strings.TrimSpace(tok[:end])
}

// sequenceOutline renders sequence participants and messages.
func sequenceOutline(lines []string, limit int) []string {
	var out []string
	for _, raw := range lines {
		l := strings.TrimSpace(strings.TrimSuffix(raw, ";"))
		if l == "" || strings.HasPrefix(l, "%%") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(l), "participant ") {
			out = append(out, "• "+clip(strings.TrimSpace(l[len("participant "):]), limit))
			continue
		}
		colon := strings.Index(l, ":")
		if colon < 0 {
			out = append(out, "• "+clip(labelOf(l), limit))
			continue
		}
		from, to, ok := splitSequenceArrow(strings.TrimSpace(l[:colon]))
		if !ok {
			out = append(out, "• "+clip(labelOf(l), limit))
			continue
		}
		msg := clip(strings.TrimSpace(l[colon+1:]), limit)
		out = append(out, fmt.Sprintf("%s ──▶ %s : %s", clip(from, limit/2), clip(to, limit/2), msg))
	}
	return out
}

// splitSequenceArrow splits "Alice->>Bob" (any mermaid arrow form) into
// its two operands.
func splitSequenceArrow(s string) (from, to string, ok bool) {
	for _, arrow := range []string{"-->>", "->>", "--x", "-x", "-->", "->", "--)", "-)"} {
		if i := strings.Index(s, arrow); i > 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(arrow):]), true
		}
	}
	return "", "", false
}

func plainOutline(lines []string, limit int) []string {
	var out []string
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "%%") {
			continue
		}
		out = append(out, "• "+clip(l, limit))
	}
	return out
}

func hasDirective(l string) bool {
	for _, d := range []string{"classDef", "class ", "style ", "linkStyle", "subgraph", "end"} {
		if strings.HasPrefix(l, d) {
			return true
		}
	}
	return false
}

// splitEdge finds the first edge operator and returns its operands. It
// understands `A[Label] -->|yes| B`, `A --- B`, `A -.-> B`, `A ==> B`.
func splitEdge(l string) (left, op, right string, ok bool) {
	for _, cand := range []string{"-.->", "-.-", "===>", "==>", "---->", "<-->", "-->", "---", "--"} {
		if i := strings.Index(l, cand); i > 0 {
			return strings.TrimSpace(l[:i]), cand, strings.TrimSpace(stripEdgeLabel(l[i+len(cand):])), true
		}
	}
	return "", "", "", false
}

// stripEdgeLabel removes a leading `|label|` from an edge's right side.
func stripEdgeLabel(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "|") {
		if j := strings.Index(s[1:], "|"); j >= 0 {
			return strings.TrimSpace(s[j+2:])
		}
	}
	return s
}

// edgeLabel pulls the `|label|` immediately after an edge operator.
func edgeLabel(l string) string {
	i := strings.Index(l, "|")
	if i < 0 {
		return ""
	}
	rest := l[i+1:]
	j := strings.Index(rest, "|")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// labelOf extracts a node's display label from `A[Label]`, `A{Label}`,
// `A(Label)`, or a bare token.
func labelOf(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return ""
	}
	for _, pair := range [][2]string{{"[", "]"}, {"{", "}"}, {"(", ")"}, {">", "]"}} {
		if i := strings.Index(tok, pair[0]); i >= 0 {
			if j := strings.LastIndex(tok, pair[1]); j > i {
				inner := strings.TrimSpace(tok[i+1 : j])
				if inner != "" {
					return strings.Trim(inner, `"`)
				}
			}
		}
	}
	return tok
}

func clip(s string, n int) string {
	if n <= 1 {
		n = 1
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
