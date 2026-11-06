package gitcore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Ref kinds on a graph row (F-162).
const (
	RefHead   = "head"   // HEAD, the checked-out commit
	RefBranch = "branch" // refs/heads/*
	RefRemote = "remote" // refs/remotes/*
	RefTag    = "tag"    // refs/tags/*
	RefOther  = "other"  // stash, notes, replace refs
)

// GraphRef is one decoration on a commit.
type GraphRef struct {
	Name    string // short name: main, origin/main, v1.0, HEAD
	Kind    string
	Current bool // the branch HEAD points at
}

// GraphRow is one line of `git log --graph`: a commit (Hash set) or a pure
// connector line between commits (Hash empty, only Graph).
type GraphRow struct {
	Graph   string // the lane art: "| * ", "|\ "
	Hash    string
	Short   string
	Parents []string
	Refs    []GraphRef
	When    string // relative commit time
	Author  string
	Subject string
}

// IsCommit reports whether the row is a commit rather than a connector.
func (r GraphRow) IsCommit() bool { return r.Hash != "" }

// graphFormat starts with a unit separator so the lane art (everything before
// it) is told apart from the fields, and connector lines have none.
const graphFormat = "%x1f%H%x1f%h%x1f%P%x1f%D%x1f%cr%x1f%an%x1f%s"

// GraphLog is the commit graph of every branch, newest first, at most limit
// commits, through the user's git (ADR-0044).
func GraphLog(ctx context.Context, r *Runner, dir string, limit int) ([]GraphRow, error) {
	args := []string{"log", "--graph", "--all", "--decorate=full", "--color=never",
		"--date=relative", "--format=" + graphFormat}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, _, err := r.Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return ParseGraph(out), nil
}

// ParseGraph turns `git log --graph` output in graphFormat into rows. Lines
// that do not parse as a commit are kept as connectors so the lanes stay intact.
func ParseGraph(out string) []GraphRow {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	var rows []GraphRow
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		art, rest, found := strings.Cut(line, "\x1f")
		if !found {
			rows = append(rows, GraphRow{Graph: strings.TrimRight(line, " ")})
			continue
		}
		f := strings.SplitN(rest, "\x1f", 7)
		if len(f) != 7 {
			rows = append(rows, GraphRow{Graph: strings.TrimRight(art, " ")})
			continue
		}
		rows = append(rows, GraphRow{
			Graph: art, Hash: f[0], Short: f[1], Parents: strings.Fields(f[2]),
			Refs: parseRefs(f[3]), When: f[4], Author: f[5], Subject: f[6],
		})
	}
	return rows
}

// parseRefs reads %D with --decorate=full: "HEAD -> refs/heads/main,
// tag: refs/tags/v1, refs/remotes/origin/main".
func parseRefs(d string) []GraphRef {
	var refs []GraphRef
	for _, part := range strings.Split(d, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if tgt, ok := strings.CutPrefix(part, "HEAD -> "); ok {
			refs = append(refs, GraphRef{Name: "HEAD", Kind: RefHead}, refOf(tgt, true))
			continue
		}
		refs = append(refs, refOf(part, false))
	}
	return refs
}

func refOf(full string, current bool) GraphRef {
	full = strings.TrimPrefix(full, "tag: ")
	switch {
	case full == "HEAD":
		return GraphRef{Name: "HEAD", Kind: RefHead}
	case strings.HasPrefix(full, "refs/heads/"):
		return GraphRef{Name: strings.TrimPrefix(full, "refs/heads/"), Kind: RefBranch, Current: current}
	case strings.HasPrefix(full, "refs/remotes/"):
		return GraphRef{Name: strings.TrimPrefix(full, "refs/remotes/"), Kind: RefRemote}
	case strings.HasPrefix(full, "refs/tags/"):
		return GraphRef{Name: strings.TrimPrefix(full, "refs/tags/"), Kind: RefTag}
	}
	return GraphRef{Name: full, Kind: RefOther}
}

// CherryPick applies one commit onto the current branch. On a conflict git
// stops mid-pick; InCherryPick then reports true and the work tree holds the
// markers (F-156).
func CherryPick(ctx context.Context, r *Runner, dir, hash string) error {
	_, _, err := r.Run(ctx, dir, "cherry-pick", hash)
	return err
}

// CherryPickAbort cancels a cherry-pick in progress and restores the tree.
func CherryPickAbort(ctx context.Context, r *Runner, dir string) error {
	_, _, err := r.Run(ctx, dir, "cherry-pick", "--abort")
	return err
}

// InCherryPick reports whether a cherry-pick is stopped in dir.
func InCherryPick(ctx context.Context, r *Runner, dir string) bool {
	_, _, err := r.Run(ctx, dir, "rev-parse", "-q", "--verify", "CHERRY_PICK_HEAD")
	return err == nil
}

// BranchAt creates branch name at hash without switching to it.
func BranchAt(ctx context.Context, r *Runner, dir, name, hash string) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q is not a branch name", name)
	}
	_, _, err := r.Run(ctx, dir, "branch", name, hash)
	return err
}
