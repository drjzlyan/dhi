package gitcore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNoSelectedChange is returned when a selection covers no + or - line.
var ErrNoSelectedChange = errors.New("no changed line selected")

// PartialPatch builds a patch for `git apply --cached` (F-151) from a
// file's patch header and ONE raw hunk (as split from `git diff`), keeping
// only the changes whose body line is in sel. Body lines count from 0 at
// the line after the @@ header.
//
// For staging (reverse=false, the hunk is work tree against index) an
// unselected '-' line becomes context (it stays in the index) and an
// unselected '+' line is dropped. For unstaging (reverse=true, the hunk is
// index against HEAD and the result is applied with -R) the roles swap: an
// unselected '+' stays as context and an unselected '-' is dropped. The
// @@ counts are recounted; a "\ No newline at end of file" marker follows
// the fate of the line above it.
func PartialPatch(head, hunk string, sel map[int]bool, reverse bool) (string, error) {
	lines := strings.Split(strings.TrimSuffix(hunk, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "@@") {
		return "", fmt.Errorf("not a hunk")
	}
	oldStart, rest, err := parseHunkHeader(lines[0])
	if err != nil {
		return "", err
	}
	var body []string
	var oldN, newN int
	changed := false
	emitted := false // whether the last non-marker line made it into the result
	for i, l := range lines[1:] {
		if l == "" { // a context line whose single space was trimmed
			l = " "
		}
		op, text := l[0], l[1:]
		switch op {
		case ' ':
			body = append(body, l)
			oldN++
			newN++
			emitted = true
		case '-', '+':
			// the side that stays when unselected: forward keeps '-' as
			// context, reverse keeps '+'
			stays := (op == '-') != reverse
			switch {
			case sel[i]:
				body = append(body, l)
				changed = true
				if op == '-' {
					oldN++
				} else {
					newN++
				}
				emitted = true
			case stays:
				body = append(body, " "+text)
				oldN++
				newN++
				emitted = true
			default:
				emitted = false
			}
		case '\\':
			if emitted {
				body = append(body, l)
			}
		default:
			return "", fmt.Errorf("unexpected hunk line %q", l)
		}
	}
	if !changed {
		return "", ErrNoSelectedChange
	}
	newStart := oldStart
	switch {
	case oldN == 0:
		newStart = oldStart + 1 // the start of an empty range is the line before
	case newN == 0:
		newStart = oldStart - 1
	}
	hdr := fmt.Sprintf("@@ -%s +%s @@%s", span(oldStart, oldN), span(newStart, newN), rest)
	return head + hdr + "\n" + strings.Join(body, "\n") + "\n", nil
}

func span(start, n int) string {
	if n == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d,%d", start, n)
}

// parseHunkHeader reads the old start and returns what follows the closing
// @@ (the optional section heading), including its leading space.
func parseHunkHeader(h string) (oldStart int, rest string, err error) {
	bad := fmt.Errorf("bad hunk header %q", h)
	fields := strings.SplitN(strings.TrimPrefix(h, "@@ "), " @@", 2)
	if len(fields) != 2 {
		return 0, "", bad
	}
	parts := strings.Fields(fields[0])
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "-") {
		return 0, "", bad
	}
	s, _, _ := strings.Cut(parts[0][1:], ",")
	oldStart, err = strconv.Atoi(s)
	if err != nil {
		return 0, "", bad
	}
	return oldStart, fields[1], nil
}

// ApplyPartial applies a PartialPatch to the index of the repo at dir with
// the user's git: `git apply --cached`, with --reverse to unstage. The
// patch rides stdin.
func (r *Runner) ApplyPartial(ctx context.Context, dir, patch string, reverse bool) error {
	args := []string{"apply", "--cached"}
	if reverse {
		args = append(args, "--reverse")
	}
	_, _, err := r.RunInput(ctx, dir, patch, append(args, "-")...)
	return err
}
