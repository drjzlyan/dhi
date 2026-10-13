package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Snapshot is a deep copy of c as a generic tree, for SaveChanged to diff
// against after an edit (F-090). Config holds maps, so a struct copy
// would share them with the edited config.
func Snapshot(c Config) map[string]any {
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(c); err != nil {
		return map[string]any{}
	}
	m := map[string]any{}
	if _, err := toml.Decode(sb.String(), &m); err != nil {
		return map[string]any{}
	}
	delete(m, "schema")
	delete(m, "conventions") // its own files, its own writers (F-075)
	return m
}

// SaveChanged writes into the layer at path only the keys whose value
// differs between before (a Snapshot) and after, including removed map
// entries, keeping every other key in that file (F-090). Saving the whole
// merged config instead froze every user-level value into the workspace,
// so a later change "for every workspace" never reached it. It returns
// the changed dotted keys.
func SaveChanged(path string, before map[string]any, after Config) ([]string, error) {
	now := Snapshot(after)
	var set, del []string
	diffLeaves("", before, now, &set, &del)
	if len(set)+len(del) == 0 {
		return nil, nil
	}
	m, err := readLayer(path)
	if err != nil {
		return nil, err
	}
	for _, k := range set {
		setDotted(m, k, getDotted(now, k))
	}
	for _, k := range del {
		deleteDotted(m, k)
	}
	body, err := encodeLayer(m)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("settings: write %s: %w", path, err)
	}
	keys := append(set, del...)
	sort.Strings(keys)
	return keys, nil
}

// diffLeaves records the dotted leaves that differ: set for a new or
// changed value, del for one gone from b. Tables recurse; anything else
// (including arrays) is a leaf.
func diffLeaves(prefix string, a, b map[string]any, set, del *[]string) {
	for k, bv := range b {
		key := join(prefix, k)
		av, had := a[k]
		am, aIsMap := av.(map[string]any)
		bm, bIsMap := bv.(map[string]any)
		switch {
		case aIsMap && bIsMap:
			diffLeaves(key, am, bm, set, del)
		case bIsMap:
			diffLeaves(key, map[string]any{}, bm, set, del)
		case !had || !reflect.DeepEqual(av, bv):
			*set = append(*set, key)
		}
	}
	for k, av := range a {
		if _, ok := b[k]; ok {
			continue
		}
		if am, ok := av.(map[string]any); ok {
			diffLeaves(join(prefix, k), am, map[string]any{}, set, del)
			continue
		}
		*del = append(*del, join(prefix, k))
	}
}

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

func getDotted(m map[string]any, key string) any {
	var cur any = m
	for _, p := range strings.Split(key, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func deleteDotted(m map[string]any, key string) {
	parts := strings.Split(key, ".")
	cur := m
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, parts[len(parts)-1])
}

// LayerHas reports whether the layer file at path sets key itself — what
// Settings shows as "set for this workspace" (F-090).
func LayerHas(path, key string) bool {
	if path == "" {
		return false
	}
	m, err := readLayer(path)
	if err != nil {
		return false
	}
	return getDotted(m, key) != nil
}

// DuplicateKeys lists the workspace-layer leaves whose value equals the
// user layer's: what an older DHI copied there by saving the whole merged
// config (F-090). Removing them lets the user layer govern again.
func DuplicateKeys(userPath, wsPath string) ([]string, error) {
	u, err := readLayer(userPath)
	if err != nil {
		return nil, err
	}
	w, err := readLayer(wsPath)
	if err != nil {
		return nil, err
	}
	var dup []string
	sameLeaves("", u, w, &dup)
	sort.Strings(dup)
	return dup, nil
}

func sameLeaves(prefix string, u, w map[string]any, out *[]string) {
	for k, wv := range w {
		if k == "schema" && prefix == "" {
			continue
		}
		key := join(prefix, k)
		uv, ok := u[k]
		if !ok {
			continue
		}
		um, uIsMap := uv.(map[string]any)
		wm, wIsMap := wv.(map[string]any)
		switch {
		case uIsMap && wIsMap:
			sameLeaves(key, um, wm, out)
		case !uIsMap && !wIsMap && reflect.DeepEqual(uv, wv):
			*out = append(*out, key)
		}
	}
}

// RemoveKeys deletes keys from the layer at path, keeping the rest.
func RemoveKeys(path string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	m, err := readLayer(path)
	if err != nil {
		return err
	}
	for _, k := range keys {
		deleteDotted(m, k)
	}
	pruneEmpty(m)
	body, err := encodeLayer(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func pruneEmpty(m map[string]any) {
	for k, v := range m {
		if mm, ok := v.(map[string]any); ok {
			pruneEmpty(mm)
			if len(mm) == 0 {
				delete(m, k)
			}
		}
	}
}
