package toolchain

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rustSkip are components left out of the merged sysroot: hundreds of
// megabytes of HTML no build needs.
var rustSkip = map[string]bool{"rust-docs": true, "rust-docs-json": true}

// mergeRustComponents folds Rust's standalone installer layout — one
// directory per component (rustc/, cargo/, rust-std-<target>/, …) listed
// in a `components` file — into a single sysroot under root, as the
// archive's install.sh would, without running a shell (ADR-0005). rustc
// finds its standard library relative to its own binary, so the
// components must share one tree.
func mergeRustComponents(root string) error {
	f, err := os.Open(filepath.Join(root, "components"))
	if err != nil {
		return fmt.Errorf("toolchain: rust: no components list: %w", err)
	}
	var comps []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if c := strings.TrimSpace(sc.Text()); c != "" && !strings.ContainsAny(c, `/\`) && c != "." && c != ".." {
			comps = append(comps, c)
		}
	}
	_ = f.Close()
	if len(comps) == 0 {
		return fmt.Errorf("toolchain: rust: empty components list")
	}
	for _, c := range comps {
		src := filepath.Join(root, c)
		if rustSkip[c] {
			_ = os.RemoveAll(src)
			continue
		}
		if err := mergeTree(src, root); err != nil {
			return fmt.Errorf("toolchain: rust: merge %s: %w", c, err)
		}
		if err := os.RemoveAll(src); err != nil {
			return err
		}
	}
	return nil
}

// mergeTree moves every file under src into the same relative place
// under dst (dirs created; component metadata skipped).
func mergeTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return err
		}
		if rel == "manifest.in" {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		_ = os.Remove(target)
		return os.Rename(p, target)
	})
}
