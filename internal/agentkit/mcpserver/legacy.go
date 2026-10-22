package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// LegacyDir held DHI's own per-server TOML cards before ADR-0043. Only
// the migration repair reads it.
const LegacyDir = ".dhi/mcp"

type legacyCard struct {
	Schema    int      `toml:"schema"`
	Name      string   `toml:"name"`
	Transport string   `toml:"transport"`
	Command   string   `toml:"command"`
	Args      []string `toml:"args"`
	URL       string   `toml:"url"`
	Env       []string `toml:"env"`
	AuthEnv   string   `toml:"auth_env"`
}

// ReadLegacy converts the workspace's old .dhi/mcp/<slug>.toml cards to
// standard entries: declared env names become ${NAME} references and an
// auth_env becomes an Authorization header. It returns the card files it
// read, so the repair can remove them once the JSON is written.
func ReadLegacy(root string) ([]Server, []string, error) {
	dir := filepath.Join(root, LegacyDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var out []Server
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		var c legacyCard
		if _, err := toml.DecodeFile(path, &c); err != nil {
			return nil, nil, fmt.Errorf("mcp: %s: %w", path, err)
		}
		s := Server{Slug: strings.TrimSuffix(e.Name(), ".toml"), Command: c.Command, Args: c.Args, URL: c.URL}
		switch c.Transport {
		case "http":
			s.Transport = HTTP
		default:
			s.Transport = Stdio
		}
		if len(c.Env) > 0 {
			s.Env = map[string]string{}
			for _, n := range c.Env {
				s.Env[n] = "${" + n + "}"
			}
		}
		if c.AuthEnv != "" {
			s.Headers = map[string]string{"Authorization": "Bearer ${" + c.AuthEnv + "}"}
		}
		out = append(out, s)
		files = append(files, path)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, files, nil
}
