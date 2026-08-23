package toolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Lockfile records what is actually installed under the toolchain root.
// It is written last, after every artifact is activated, and is the
// source of truth for `dhi doctor` integrity checks.
type Lockfile struct {
	Schema    int                   `json:"schema"`
	UpdatedAt time.Time             `json:"updated_at"`
	Tools     map[string]LockedTool `json:"tools"`
}

// LockedTool describes one activated tool version.
type LockedTool struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"` // archive digest from the manifest
	Path    string `json:"path"`   // tool dir, relative to the root
}

func (m *Manager) lockfilePath() string {
	return filepath.Join(m.root, "lock.json")
}

// ReadLockfile loads the installed-state record; a missing lockfile is
// not an error — it means nothing has been installed yet.
func (m *Manager) ReadLockfile() (*Lockfile, error) {
	data, err := os.ReadFile(m.lockfilePath())
	if os.IsNotExist(err) {
		return &Lockfile{Schema: SchemaVersion, Tools: map[string]LockedTool{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("toolchain: read lockfile: %w", err)
	}
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("toolchain: lockfile: %w", err)
	}
	if lf.Tools == nil {
		lf.Tools = map[string]LockedTool{}
	}
	return &lf, nil
}

// writeLockfile persists the lockfile atomically (temp file + rename).
func (m *Manager) writeLockfile(lf *Lockfile) error {
	lf.Schema = SchemaVersion
	lf.UpdatedAt = m.now()
	if lf.Tools == nil {
		lf.Tools = map[string]LockedTool{}
	}
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("toolchain: lockfile: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(m.root, ".lock-*.json")
	if err != nil {
		return fmt.Errorf("toolchain: lockfile: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("toolchain: lockfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("toolchain: lockfile: %w", err)
	}
	if err := os.Rename(name, m.lockfilePath()); err != nil {
		os.Remove(name)
		return fmt.Errorf("toolchain: lockfile: %w", err)
	}
	return nil
}
