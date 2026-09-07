package sandbox

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSeatbeltWrapShape(t *testing.T) {
	s, err := NewSeatbelt("/usr/bin/sandbox-exec",
		[]string{"/ws/repo", "/ws/.dhi"}, []string{"/opt/dhi"})
	if err != nil {
		t.Fatalf("NewSeatbelt: %v", err)
	}
	got, err := s.Wrap([]string{"git", "status"})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if got[0] != "/usr/bin/sandbox-exec" || got[1] != "-p" {
		t.Fatalf("argv head = %v, want sandbox-exec -p", got[:2])
	}
	if got[3] != "--" || !reflect.DeepEqual(got[4:], []string{"git", "status"}) {
		t.Fatalf("argv = %v, want profile then -- git status", got)
	}
	if s.Name() != "seatbelt" {
		t.Fatalf("Name = %q", s.Name())
	}
}

func TestSeatbeltProfileScopes(t *testing.T) {
	s, err := NewSeatbelt("sandbox-exec",
		[]string{"/ws/repo"}, []string{"/opt/dhi"})
	if err != nil {
		t.Fatalf("NewSeatbelt: %v", err)
	}
	p := s.Profile()
	for _, want := range []string{
		"(deny default)",
		`(subpath "/ws/repo")`,
		`(subpath "/opt/dhi")`,
		"(allow file-write*",
		"(allow network*)",
		`(subpath "/usr/lib")`,
		`(subpath "/System")`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("profile missing %q:\n%s", want, p)
		}
	}
	// write scope must exist and must not cover ro roots: file-write*
	// line contains only rw subpaths.
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, "(allow file-write*") && strings.Contains(line, "/opt/dhi") {
			t.Errorf("file-write* covers ro root:\n%s", line)
		}
	}
}

func TestSeatbeltProfileEscapesQuotes(t *testing.T) {
	s, err := NewSeatbelt("sandbox-exec", []string{"/ws/od\"d"}, nil)
	if err != nil {
		t.Fatalf("NewSeatbelt: %v", err)
	}
	if !strings.Contains(s.Profile(), `"/ws/od\"d"`) {
		t.Fatalf("quote not escaped:\n%s", s.Profile())
	}
}

func TestSeatbeltRequiresRoots(t *testing.T) {
	if _, err := NewSeatbelt("sandbox-exec", nil, nil); err == nil {
		t.Fatal("expected error for empty rw roots")
	}
	if _, err := NewSeatbelt("", []string{"/ws"}, nil); err == nil {
		t.Fatal("expected error for empty binary")
	}
}

func TestBubblewrapWrapShape(t *testing.T) {
	b, err := NewBubblewrap("/usr/bin/bwrap",
		[]string{"/ws/repo", "/ws/.dhi"}, []string{"/opt/dhi"})
	if err != nil {
		t.Fatalf("NewBubblewrap: %v", err)
	}
	got, err := b.Wrap([]string{"rg", "pattern"})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if got[0] != "/usr/bin/bwrap" {
		t.Fatalf("argv[0] = %q", got[0])
	}
	if !reflect.DeepEqual(got[len(got)-2:], []string{"rg", "pattern"}) {
		t.Fatalf("original argv not preserved: %v", got)
	}
	joined := " " + strings.Join(got, " ") + " "
	for _, want := range []string{
		" --unshare-all ", " --share-net ", " --die-with-parent ",
		" --proc /proc ", " --tmpfs /tmp ",
		" --ro-bind /usr /usr ", " --ro-bind /etc /etc ",
		" --bind /ws/repo /ws/repo ", " --bind /ws/.dhi /ws/.dhi ",
		" --ro-bind /opt/dhi /opt/dhi ",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q:\n%s", want, joined)
		}
	}
	if b.Name() != "bubblewrap" {
		t.Fatalf("Name = %q", b.Name())
	}
}

func TestBubblewrapRequiresAbsoluteRoots(t *testing.T) {
	if _, err := NewBubblewrap("bwrap", []string{"relative/path"}, nil); err == nil {
		t.Fatal("expected error for relative root")
	}
	if _, err := NewBubblewrap("bwrap", nil, nil); err == nil {
		t.Fatal("expected error for empty rw roots")
	}
}

func TestDetectMatrix(t *testing.T) {
	ok := func(string) (string, error) { return "/bin/x", nil }
	miss := func(string) (string, error) { return "", errors.New("not found") }
	cases := []struct {
		goos string
		look LookPath
		want string
	}{
		{"darwin", ok, "seatbelt"},
		{"darwin", miss, "noop"},
		{"linux", ok, "bubblewrap"},
		{"linux", miss, "noop"},
		{"windows", ok, "noop"},
		{"plan9", miss, "noop"},
	}
	for _, c := range cases {
		if got := Detect(c.goos, c.look); got != c.want {
			t.Errorf("Detect(%q) = %q, want %q", c.goos, got, c.want)
		}
	}
}

func TestSelectMatrix(t *testing.T) {
	rw := []string{"/ws"}
	ok := func(string) (string, error) { return "/bin/x", nil }
	miss := func(string) (string, error) { return "", errors.New("not found") }

	// off always yields Noop (explicit opt-out).
	sb, err := Select("darwin", ok, "off", rw, nil)
	if err != nil || sb.Name() != "noop" {
		t.Fatalf("off: %v %v", sb.Name(), err)
	}
	// available platform adapters are built.
	sb, err = Select("darwin", ok, "auto", rw, nil)
	if err != nil || sb.Name() != "seatbelt" {
		t.Fatalf("darwin: %v %v", sb.Name(), err)
	}
	sb, err = Select("linux", ok, "auto", rw, nil)
	if err != nil || sb.Name() != "bubblewrap" {
		t.Fatalf("linux: %v %v", sb.Name(), err)
	}
	// missing helper is an ERROR naming the binary + fix (ADR-0011):
	// never a silent Noop.
	if sb, err := Select("darwin", miss, "auto", rw, nil); err == nil {
		t.Fatalf("missing helper must refuse, got %v", sb.Name())
	} else if !strings.Contains(err.Error(), "sandbox-exec") ||
		!strings.Contains(err.Error(), `security.sandbox = "off"`) {
		t.Errorf("error must name binary + opt-out: %v", err)
	}
	// unknown GOOS refuses.
	if sb, err := Select("plan9", ok, "auto", rw, nil); err == nil {
		t.Fatalf("plan9 must refuse, got %v", sb.Name())
	}
}

func mustJail(t *testing.T, _ string) *Jail {
	t.Helper()
	j, err := NewJail(t.TempDir())
	if err != nil {
		t.Fatalf("NewJail: %v", err)
	}
	return j
}

func TestGuardExecWrapsThroughAdapter(t *testing.T) {
	guard := &Guard{
		Jail:    mustJail(t, "unused"),
		Policy:  &Policy{Rules: []Rule{{Op: OpExec, Effect: Allow}}},
		Sandbox: nil,
	}
	seat, err := NewSeatbelt("sandbox-exec", guard.Jail.Roots(), nil)
	if err != nil {
		t.Fatalf("NewSeatbelt: %v", err)
	}
	guard.Sandbox = seat
	wrapped, _, err := guard.Exec([]string{"sh", "-c", "ls"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if wrapped[0] != "sandbox-exec" || !reflect.DeepEqual(wrapped[4:], []string{"sh", "-c", "ls"}) {
		t.Fatalf("wrapped = %v", wrapped)
	}
}
