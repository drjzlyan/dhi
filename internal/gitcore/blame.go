package gitcore

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// BlameLine is the commit that last changed one line (F-143).
type BlameLine struct {
	Hash        string // full
	Author      string
	When        time.Time
	Subject     string
	Text        string // the blamed line itself, as git read it from disk
	Uncommitted bool   // not in any commit yet
}

// Blame runs `git blame --porcelain` once for rel (relative to dir) and
// returns one entry per line of the file, in order.
func Blame(ctx context.Context, r *Runner, dir, rel string) ([]BlameLine, error) {
	out, _, err := r.Run(ctx, dir, "blame", "--porcelain", "--", rel)
	if err != nil {
		return nil, err
	}
	return ParseBlame(out), nil
}

// ParseBlame reads porcelain output. A commit's author, time and
// summary appear only on its first line; later lines of that commit
// carry just the header, so details are remembered per hash.
func ParseBlame(out string) []BlameLine {
	type info struct {
		author, subject string
		when            time.Time
	}
	seen := map[string]*info{}
	var (
		res  []BlameLine
		cur  *info
		hash string
	)
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(ln, "\t"):
			if cur == nil {
				continue
			}
			res = append(res, BlameLine{
				Hash: hash, Author: cur.author, When: cur.when, Subject: cur.subject,
				Text: ln[1:], Uncommitted: strings.Trim(hash, "0") == "",
			})
			cur = nil
		case cur != nil && strings.HasPrefix(ln, "author "):
			cur.author = strings.TrimPrefix(ln, "author ")
		case cur != nil && strings.HasPrefix(ln, "author-time "):
			if sec, err := strconv.ParseInt(strings.TrimPrefix(ln, "author-time "), 10, 64); err == nil {
				cur.when = time.Unix(sec, 0)
			}
		case cur != nil && strings.HasPrefix(ln, "summary "):
			cur.subject = strings.TrimPrefix(ln, "summary ")
		default:
			// A header is "<hash> <orig> <final> [<count>]".
			f := strings.Fields(ln)
			if len(f) >= 3 && isHexHash(f[0]) {
				hash = f[0]
				if seen[hash] == nil {
					seen[hash] = &info{}
				}
				cur = seen[hash]
			}
		}
	}
	return res
}

func isHexHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
