package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// writeCall matches the calls that change the file system.
var writeCall = regexp.MustCompile(`\bos\.(WriteFile|Create|CreateTemp|OpenFile|Mkdir|MkdirAll|MkdirTemp|Rename|Remove|RemoveAll|Symlink|Chmod)\(`)

// TestWriteSitesAreReviewed is the F-113 audit: DHI changes nothing
// outside its own folders and the workspace without the user's yes. Every
// file that writes is listed in testdata/write_sites.txt with its count;
// a new write site fails here until someone checks where it writes and
// records it (DHI_UPDATE_WRITE_SITES=1 go test ./cmd/dhi -run WriteSites).
// A write outside DHI's folders and the workspace must go through an
// approval card (or be the user's own action, like a package install).
func TestWriteSitesAreReviewed(t *testing.T) {
	root := filepath.Join("..", "..")
	got := map[string]int{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".dhi", "testdata", "scripts", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "testutil") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if n := len(writeCall.FindAll(b, -1)); n > 0 {
			rel, _ := filepath.Rel(root, p)
			got[filepath.ToSlash(rel)] = n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "write_sites.txt")
	if os.Getenv("DHI_UPDATE_WRITE_SITES") == "1" {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("# Files that write to the file system, with their write-call counts (F-113).\n")
		b.WriteString("# Each was checked: writes go to DHI's folders, the workspace, temp dirs, or\n")
		b.WriteString("# outside only after an approval card or the user's own action.\n")
		for _, k := range keys {
			b.WriteString(k + " " + strconv.Itoa(got[k]) + "\n")
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, n, _ := strings.Cut(line, " ")
		want[name], _ = strconv.Atoi(n)
	}
	for k, n := range got {
		if n > want[k] {
			t.Errorf("%s: %d write call(s), %d reviewed — check where the new one writes (outside DHI's folders and the workspace it needs an approval card), then record it with DHI_UPDATE_WRITE_SITES=1", k, n, want[k])
		}
	}
}
