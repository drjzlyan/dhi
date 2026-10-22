package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MarketTTL is how long a fetched marketplace catalog is reused.
const MarketTTL = 24 * time.Hour

// FetchMarketplace returns the catalog at src. A GitHub-hosted catalog is
// read as one file (raw.githubusercontent.com), never a full clone; other
// git hosts are cloned. Catalogs are cached under cacheDir for MarketTTL;
// refresh forces a new fetch.
func FetchMarketplace(ctx context.Context, src Source, client *http.Client, fetch Fetcher, cacheDir string, refresh bool) (*Marketplace, []string, error) {
	key := sha256.Sum256([]byte(src.String()))
	cache := filepath.Join(cacheDir, "marketplaces", hex.EncodeToString(key[:8])+".json")
	if !refresh {
		if fi, err := os.Stat(cache); err == nil && time.Since(fi.ModTime()) < MarketTTL {
			if data, err := os.ReadFile(cache); err == nil {
				return ParseMarketplace(data, src)
			}
		}
	}
	data, err := marketplaceBytes(ctx, src, client, fetch, cacheDir)
	if err != nil {
		if old, rerr := os.ReadFile(cache); rerr == nil { // offline: the last copy, named
			m, warns, perr := ParseMarketplace(old, src)
			return m, append(warns, "showing the cached catalog: "+err.Error()), perr
		}
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err == nil {
		_ = os.WriteFile(cache, data, 0o644)
	}
	return ParseMarketplace(data, src)
}

func marketplaceBytes(ctx context.Context, src Source, client *http.Client, fetch Fetcher, tmp string) ([]byte, error) {
	rel := strings.Trim(filepath.ToSlash(filepath.Join(src.Subdir, ".claude-plugin", "marketplace.json")), "/")
	if src.Path != "" {
		return os.ReadFile(filepath.Join(src.Path, filepath.FromSlash(rel)))
	}
	if owner, repo, ok := githubRepo(src.URL); ok && client != nil {
		ref := src.Ref
		if ref == "" {
			ref = "HEAD"
		}
		url := "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + ref + "/" + rel
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	}
	if fetch == nil {
		return nil, fmt.Errorf("no way to fetch %s", src)
	}
	dir, err := os.MkdirTemp(tmp, "dhi-market-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if _, err := fetch(ctx, src.URL, src.Ref, dir); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
}

// githubRepo splits https://github.com/<owner>/<repo>.git.
func githubRepo(url string) (owner, repo string, ok bool) {
	rest, found := strings.CutPrefix(url, "https://github.com/")
	if !found {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(rest, ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// Search lists a catalog's plugins matching query (name, description or
// category; empty matches all).
func (m *Marketplace) Search(query string) []PluginEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []PluginEntry
	for _, p := range m.Plugins {
		if q == "" || strings.Contains(strings.ToLower(p.Name+" "+p.Description+" "+p.Category), q) {
			out = append(out, p)
		}
	}
	return out
}

// Find returns the plugin named name.
func (m *Marketplace) Find(name string) (PluginEntry, bool) {
	for _, p := range m.Plugins {
		if p.Name == name {
			return p, true
		}
	}
	return PluginEntry{}, false
}
