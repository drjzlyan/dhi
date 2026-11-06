package editshim

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the shim, the way the DHI binary does.
func TestMain(m *testing.M) {
	if IsShim(os.Args[0]) {
		os.Exit(RunShim(os.Args[1:], os.Getenv, func(s string) { os.Stderr.WriteString(s + "\n") }))
	}
	os.Exit(m.Run())
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

// shimRun starts the editor command the way git does (through a shell) and
// answers the request with ok.
func shimRun(t *testing.T, ok bool) (path string, err error) {
	t.Helper()
	exe, _ := os.Executable()
	srv, serr := Start(exe)
	if serr != nil {
		t.Fatal(serr)
	}
	defer srv.Close()
	env := srv.Env()
	if envValue(env, "GIT_EDITOR") != envValue(env, "GIT_SEQUENCE_EDITOR") {
		t.Fatalf("editors differ: %v", env)
	}
	cmd := exec.Command("sh", "-c", envValue(env, "GIT_EDITOR")+` "$1"`, "sh", "todo.txt")
	cmd.Env = append(os.Environ(), env...)
	if serr := cmd.Start(); serr != nil {
		t.Fatal(serr)
	}
	select {
	case req := <-srv.Requests():
		path = req.Path
		req.Done(ok)
	case <-time.After(10 * time.Second):
		t.Fatal("no request reached the server")
	}
	return path, cmd.Wait()
}

func TestShimBlocksUntilDoneAndExitsZero(t *testing.T) {
	path, err := shimRun(t, true)
	if err != nil {
		t.Fatalf("shim failed: %v", err)
	}
	if !strings.HasSuffix(path, "/todo.txt") || path[0] != '/' {
		t.Fatalf("path not absolute: %q", path)
	}
}

func TestShimExitsNonZeroOnAbort(t *testing.T) {
	if _, err := shimRun(t, false); err == nil {
		t.Fatal("an aborted edit must make the program fail")
	}
}

func TestShimFailsWhenServerCloses(t *testing.T) {
	exe, _ := os.Executable()
	srv, err := Start(exe)
	if err != nil {
		t.Fatal(err)
	}
	env := srv.Env()
	cmd := exec.Command("sh", "-c", envValue(env, "GIT_EDITOR")+` x`)
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	req := <-srv.Requests()
	_ = req
	srv.Close() // DHI quits mid-edit: the socket goes away
	if err := cmd.Wait(); err == nil {
		t.Fatal("a shim whose DHI quit must fail")
	}
}

func TestShimWithoutDHIRefuses(t *testing.T) {
	var msg string
	if code := RunShim([]string{"f"}, func(string) string { return "" }, func(s string) { msg = s }); code != 2 || msg == "" {
		t.Fatalf("code=%d msg=%q", code, msg)
	}
}

func TestEnvQuotesPath(t *testing.T) {
	s := &Server{dir: "/tmp/it's here"}
	if got := envValue(s.Env(), "GIT_EDITOR"); got != `'/tmp/it'\''s here/dhi-edit'` {
		t.Fatalf("got %s", got)
	}
}

func TestIsShim(t *testing.T) {
	if !IsShim("/x/y/dhi-edit") || IsShim("/x/dhi") {
		t.Fatal("IsShim")
	}
}
