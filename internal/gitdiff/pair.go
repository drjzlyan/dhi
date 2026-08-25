package gitdiff

// Row is one visual line of a side-by-side rendering: the old side on the
// left, the new side on the right. A nil cell renders as blank padding.
type Row struct {
	Left, Right *Line
}

// Pair aligns a hunk's lines into side-by-side rows. Context passes
// straight through; runs of deletions and additions are zipped line-wise
// (a GitHub-style replace block), with leftovers padded on their missing
// side so row counts stay balanced.
func Pair(h Hunk) []Row {
	var rows []Row
	i := 0
	for i < len(h.Lines) {
		l := h.Lines[i]
		switch l.Kind {
		case Ctx:
			ctx := l
			rows = append(rows, Row{Left: &ctx, Right: &ctx})
			i++
		case Add:
			j := i
			for j < len(h.Lines) && h.Lines[j].Kind == Add {
				j++
			}
			for _, a := range h.Lines[i:j] {
				add := a
				rows = append(rows, Row{Right: &add})
			}
			i = j
		case Del:
			j := i
			for j < len(h.Lines) && h.Lines[j].Kind == Del {
				j++
			}
			k := j
			for k < len(h.Lines) && h.Lines[k].Kind == Add {
				k++
			}
			dels, adds := h.Lines[i:j], h.Lines[j:k]
			for r := 0; r < max(len(dels), len(adds)); r++ {
				var row Row
				if r < len(dels) {
					row.Left = &dels[r]
				}
				if r < len(adds) {
					row.Right = &adds[r]
				}
				rows = append(rows, row)
			}
			i = k
		}
	}
	return rows
}

// NewLineAt returns the hunk containing the given new-side file line and
// that line within it, or nil when the line carries no change (pure
// context or out of range). Surfaces use this to anchor comments.
func NewLineAt(files []FileDiff, path string, newNo int) (*Line, *Hunk) {
	for fi := range files {
		f := &files[fi]
		if f.DisplayPath() != path {
			continue
		}
		for hi := range f.Hunks {
			h := &f.Hunks[hi]
			for li := range h.Lines {
				l := &h.Lines[li]
				if l.Kind != Ctx && l.Kind != Add {
					continue
				}
				if l.NewNo == newNo {
					return l, h
				}
			}
		}
	}
	return nil, nil
}
