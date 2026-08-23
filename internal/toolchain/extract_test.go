package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarEntry is one in-memory tar/zip member for fixtures.
type tarEntry struct {
	name    string // slash-separated archive path
	content string
	link    string // non-empty ⇒ symlink to this target
	exec    bool
}

// buildTarGz renders entries into an in-memory tar.gz; write errors are
// impossible for in-memory buffers.
func buildTarGz(entries []tarEntry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	for _, e := range entries {
		mode := int64(0o644)
		if e.exec {
			mode = 0o755
		}
		var hdr *tar.Header
		switch {
		case e.link != "":
			hdr = &tar.Header{Name: e.name, Typeflag: tar.TypeSymlink, Linkname: e.link, Mode: 0o777}
		case strings.HasSuffix(e.name, "/"):
			hdr = &tar.Header{Name: e.name, Typeflag: tar.TypeDir, Mode: 0o755}
		default:
			hdr = &tar.Header{
				Name: e.name, Typeflag: tar.TypeReg, Mode: mode,
				Size: int64(len(e.content)),
			}
		}
		must(tw.WriteHeader(hdr))
		if hdr.Typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.content))
			must(err)
		}
	}
	must(tw.Close())
	must(gz.Close())
	return buf.Bytes()
}

func buildZip(entries []tarEntry) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	for _, e := range entries {
		var method zip.FileHeader
		switch {
		case e.link != "":
			method = zip.FileHeader{Name: e.name, Method: zip.Deflate}
			method.SetMode(0o777 | os.ModeSymlink)
		case strings.HasSuffix(e.name, "/"):
			method = zip.FileHeader{Name: e.name, Method: zip.Store}
			method.SetMode(0o755 | os.ModeDir)
		default:
			method = zip.FileHeader{Name: e.name, Method: zip.Deflate}
			if e.exec {
				method.SetMode(0o755)
			} else {
				method.SetMode(0o644)
			}
		}
		w, err := zw.CreateHeader(&method)
		must(err)
		data := e.content
		if e.link != "" {
			data = e.link
		}
		_, err = w.Write([]byte(data))
		must(err)
	}
	must(zw.Close())
	return buf.Bytes()
}

func writeArchive(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractTarGzStripAndModes(t *testing.T) {
	data := buildTarGz([]tarEntry{
		{name: "rg-14.1.0/"},
		{name: "rg-14.1.0/bin/rg", content: "#!/bin/sh\necho rg\n", exec: true},
		{name: "rg-14.1.0/man/rg.1", content: "man page"},
	})
	dest := filepath.Join(t.TempDir(), "root")
	if err := Extract(writeArchive(t, data), FormatTarGz, 1, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	bin, err := os.ReadFile(filepath.Join(dest, "bin", "rg"))
	if err != nil {
		t.Fatalf("stripped layout missing bin/rg: %v", err)
	}
	if !strings.HasPrefix(string(bin), "#!/bin/sh") {
		t.Errorf("bin/rg content = %q", bin)
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "rg"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("exec bit lost: %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(dest, "man", "rg.1")); err != nil {
		t.Errorf("man page missing: %v", err)
	}
}

func TestExtractZipLayoutAndSymlink(t *testing.T) {
	data := buildZip([]tarEntry{
		{name: "node/bin/node", content: "ELF...", exec: true},
		{name: "node/bin/corepack", link: "node"},
	})
	dest := filepath.Join(t.TempDir(), "root")
	if err := Extract(writeArchive(t, data), FormatZip, 0, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "node", "bin", "node")); err != nil || string(got) != "ELF..." {
		t.Fatalf("node content = %q, %v", got, err)
	}
	link := filepath.Join(dest, "node", "bin", "corepack")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("symlink not recreated: %v", err)
	}
	if target != "node" {
		t.Errorf("symlink target = %q", target)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	for _, fmt := range []Format{FormatTarGz, FormatZip} {
		var data []byte
		if fmt == FormatTarGz {
			data = buildTarGz([]tarEntry{{name: "../../evil.txt", content: "pwn"}})
		} else {
			data = buildZip([]tarEntry{{name: "../../evil.txt", content: "pwn"}})
		}
		outside := t.TempDir()
		dest := filepath.Join(outside, "dest")
		err := Extract(writeArchive(t, data), fmt, 0, dest)
		if err == nil {
			t.Fatalf("%s: traversal accepted", fmt)
		}
		if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); statErr == nil {
			t.Fatalf("%s: file escaped jail", fmt)
		}
	}
}

func TestExtractRejectsAbsoluteSymlink(t *testing.T) {
	data := buildTarGz([]tarEntry{{name: "bad/link", link: "/etc/passwd"}})
	dest := filepath.Join(t.TempDir(), "root")
	if err := Extract(writeArchive(t, data), FormatTarGz, 0, dest); err == nil {
		t.Fatal("absolute symlink target accepted")
	}
}
