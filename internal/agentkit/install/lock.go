package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// LockFile records, per scope, where each installed skill and server came
// from (source, commit, content digest, plugin) and the "all bots"
// policies (F-107). Third-party files are never edited to carry this.
const LockFile = "installed.toml"

// Lock is one scope's install record.
type Lock struct {
	Schema  int               `toml:"schema"`
	Skills  map[string]Record `toml:"skills,omitempty"`
	Servers map[string]Record `toml:"servers,omitempty"`
	// AllBots names skills and servers every agent gets, including
	// agents created later (F-107).
	AllBots AllBots `toml:"all_bots,omitempty"`
}

// AllBots is the "give it to every bot" policy.
type AllBots struct {
	Skills  []string `toml:"skills,omitempty"`
	Servers []string `toml:"servers,omitempty"`
}

// Record is one install.
type Record struct {
	Source  string `toml:"source"`
	Commit  string `toml:"commit,omitempty"`
	Digest  string `toml:"digest,omitempty"`
	Plugin  string `toml:"plugin,omitempty"`
	Scripts bool   `toml:"scripts,omitempty"` // the user allowed its scripts to run
}

// ReadLock reads path; a missing file is an empty lock.
func ReadLock(path string) (*Lock, error) {
	l := &Lock{Schema: 1, Skills: map[string]Record{}, Servers: map[string]Record{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(data), l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.Skills == nil {
		l.Skills = map[string]Record{}
	}
	if l.Servers == nil {
		l.Servers = map[string]Record{}
	}
	return l, nil
}

// Write saves l to path atomically.
func (l *Lock) Write(path string) error {
	l.Schema = 1
	sort.Strings(l.AllBots.Skills)
	sort.Strings(l.AllBots.Servers)
	var b strings.Builder
	b.WriteString("# What DHI installed here and where it came from (ADR-0043).\n")
	if err := toml.NewEncoder(&b).Encode(l); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SetAllBots turns the all-bots policy for a skill or server on or off.
func (l *Lock) SetAllBots(kind, name string, on bool) {
	list := &l.AllBots.Skills
	if kind == "server" {
		list = &l.AllBots.Servers
	}
	out := (*list)[:0]
	for _, n := range *list {
		if n != name {
			out = append(out, n)
		}
	}
	if on {
		out = append(out, name)
	}
	*list = out
}

// Digest hashes every file under dir (paths and contents), so an update
// that changes a skill is visible.
func Digest(dir string) (string, error) {
	h := sha256.New()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		_, _ = io.WriteString(h, filepath.ToSlash(rel)+"\x00")
		fh, err := os.Open(f)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, fh)
		_ = fh.Close()
		if err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
