package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxArchiveSize bounds decompressed output to defuse zip bombs; the
// registry is trusted supply chain, but defense in depth is cheap.
const maxArchiveSize = 4 << 30 // 4 GiB

// Extract unpacks the verified archive into destDir, dropping Strip
// leading path components from every entry. Any entry escaping destDir
// (absolute path or "..") aborts extraction. Symlinks are recreated;
// symlink targets must be relative.
func Extract(archivePath string, format Format, strip int, destDir string) error {
	if !format.Valid() {
		return fmt.Errorf("toolchain: unsupported format %q", format)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("toolchain: extract: %w", err)
	}
	destDir = filepath.Clean(destDir)
	switch format {
	case FormatTarGz:
		return extractTarGz(archivePath, strip, destDir)
	case FormatZip:
		return extractZip(archivePath, strip, destDir)
	}
	return fmt.Errorf("toolchain: unreachable format %q", format)
}

func extractTarGz(path string, strip int, destDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("toolchain: extract: %w", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("toolchain: extract %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("toolchain: extract %s: %w", filepath.Base(path), err)
		}
		err = writeEntry(hdr.Name, hdr.Typeflag == tar.TypeDir, strip, destDir,
			func(w io.Writer) error {
				n, err := io.Copy(w, io.LimitReader(tr, maxArchiveSize+1))
				if err != nil {
					return err
				}
				if n > maxArchiveSize {
					return fmt.Errorf("toolchain: entry %q exceeds size limit", hdr.Name)
				}
				return nil
			},
			fileMode(hdr.FileInfo().Mode()))
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeSymlink {
			if err := linkEntry(hdr.Name, hdr.Linkname, strip, destDir); err != nil {
				return err
			}
		}
	}
}

func extractZip(path string, strip int, destDir string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("toolchain: extract %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = zr.Close() }()
	for _, zf := range zr.File {
		isDir := zf.FileInfo().IsDir()
		err := func() error {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			return writeEntry(zf.Name, isDir, strip, destDir,
				func(w io.Writer) error {
					n, err := io.Copy(w, io.LimitReader(rc, maxArchiveSize+1))
					if err != nil {
						return err
					}
					if n > maxArchiveSize {
						return fmt.Errorf("toolchain: entry %q exceeds size limit", zf.Name)
					}
					return nil
				},
				fileMode(zf.FileInfo().Mode()))
		}()
		if err != nil {
			return err
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			target, rerr := readAllString(zf)
			if rerr != nil {
				return rerr
			}
			if lerr := linkEntry(zf.Name, strings.TrimSpace(target), strip, destDir); lerr != nil {
				return lerr
			}
		}
	}
	return nil
}

func fileMode(m os.FileMode) os.FileMode {
	if m&0o111 != 0 || m.IsDir() {
		return 0o755
	}
	return 0o644
}

func readAllString(zf *zip.File) (string, error) {
	rc, err := zf.Open()
	if err != nil {
		return "", fmt.Errorf("toolchain: symlink %q: %w", zf.Name, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, 4096))
	if err != nil {
		return "", fmt.Errorf("toolchain: symlink %q: %w", zf.Name, err)
	}
	return string(b), nil
}

// strippedRel maps an archive member name to a safe path below destDir.
func strippedRel(name string, isDir bool, strip int) (string, bool, error) {
	p := filepath.ToSlash(name)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "./")
	var parts []string
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", false, fmt.Errorf("toolchain: archive entry %q escapes destination", name)
		default:
			parts = append(parts, part)
		}
	}
	if strip > 0 {
		if len(parts) <= strip {
			if isDir && len(parts) == strip {
				return "", false, nil // the strip consumes this whole subtree marker
			}
			return "", false, fmt.Errorf("toolchain: archive entry %q lost to strip=%d", name, strip)
		}
		parts = parts[strip:]
	}
	if len(parts) == 0 {
		return "", false, nil
	}
	return filepath.Join(parts...), true, nil
}

func writeEntry(name string, isDir bool, strip int, destDir string, copy func(io.Writer) error, mode os.FileMode) error {
	rel, ok, err := strippedRel(name, isDir, strip)
	if err != nil || !ok {
		return err
	}
	dst := filepath.Join(destDir, rel)
	if !strings.HasPrefix(dst, destDir+string(os.PathSeparator)) {
		return fmt.Errorf("toolchain: archive entry %q escapes destination", name)
	}
	if isDir {
		return os.MkdirAll(dst, mode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("toolchain: extract: %w", err)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("toolchain: extract: %w", err)
	}
	if err := copy(f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("toolchain: extract: %w", err)
	}
	return nil
}

func linkEntry(name, target string, strip int, destDir string) error {
	if filepath.IsAbs(target) {
		return fmt.Errorf("toolchain: symlink %q has absolute target", name)
	}
	rel, ok, err := strippedRel(name, false, strip)
	if err != nil || !ok {
		if err == nil {
			err = fmt.Errorf("toolchain: symlink %q lost to strip", name)
		}
		return err
	}
	dst := filepath.Join(destDir, rel)
	_ = os.Remove(dst)
	if err := os.Symlink(filepath.FromSlash(target), dst); err != nil {
		return fmt.Errorf("toolchain: symlink %q: %w", rel, err)
	}
	return nil
}
