package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLiveBubblewrap runs real bwrap (Linux, DHI_LIVE_BWRAP=1): the
// default-branch lock, credential masking, /dev/null and /tmp (M32).
// docker run --privileged … go test ./internal/sandbox -run LiveBubblewrap
func TestLiveBubblewrap(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("DHI_LIVE_BWRAP") != "1" {
		t.Skip("set DHI_LIVE_BWRAP=1 on Linux")
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not installed")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	cred := filepath.Join(cfg, "dhi", "credentials.toml")
	os.MkdirAll(filepath.Dir(cred), 0o700)
	os.WriteFile(cred, []byte("[credentials]\nTOKEN = \"sekrit\"\n"), 0o600)

	repo := t.TempDir()
	host := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(git, append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	host("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a"), []byte("1"), 0o644)
	host("add", "a")
	host("commit", "-qm", "init")
	host("pack-refs", "--all") // main only in packed-refs: the case bwrap could not guard

	refs := DefaultRefsForTest(repo)
	base, err := NewBubblewrap(bwrap, []string{repo, cfg}, []string{"/usr", "/etc"})
	if err != nil {
		t.Fatal(err)
	}
	sb, err := base.WithGuarded(refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := func(cmd string) (string, error) {
		argv, _ := sb.Wrap([]string{"/bin/sh", "-c", cmd})
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		return string(out), err
	}
	gitc := git + " -C " + repo + " -c user.name=t -c user.email=t@t "
	if out, err := run("echo 2 > " + repo + "/a && " + gitc + "commit -qam main"); err == nil {
		t.Fatalf("commit on main went through:\n%s", out)
	} else {
		t.Logf("main refused: %s", strings.TrimSpace(out))
	}
	if out, err := run(gitc + "checkout -qb task/x && " + gitc + "commit -qam task"); err != nil {
		t.Fatalf("task branch commit failed: %v\n%s", err, out)
	}
	if out, _ := run("cat " + cred); strings.Contains(out, "sekrit") {
		t.Fatal("credentials readable inside the sandbox")
	}
	if out, err := run("echo hi > /dev/null && mktemp -p /tmp && echo OK"); err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("/dev/null or /tmp: %v %s", err, out)
	}
}
