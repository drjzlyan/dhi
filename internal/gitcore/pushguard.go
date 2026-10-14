package gitcore

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PushGuardScript refuses a push that would update a remote's default
// branch (main, master, or what <remote>/HEAD names). Git runs it as a
// config-defined pre-push hook (git ≥ 2.47: hook.<name>.event/command),
// in addition to the repo's own hooks (F-086).
const PushGuardScript = `#!/bin/sh
# DHI: agents never push to a remote's default branch; that is for humans
# (or a reviewed PR). Installed by DHI; edits are overwritten.
remote="$1"
default=$(git symbolic-ref --short "refs/remotes/$remote/HEAD" 2>/dev/null | sed "s#^$remote/##")
while read -r local_ref local_sha remote_ref remote_sha; do
  case "$remote_ref" in
    refs/heads/main|refs/heads/master|"refs/heads/$default")
      echo "dhi: refusing to push to $remote_ref on $remote: the default branch is for humans; push your task branch and open a PR (pr_open)" >&2
      exit 1;;
  esac
done
exit 0
`

// WritePushGuard installs PushGuardScript at path (0755) and returns it.
func WritePushGuard(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(PushGuardScript), 0o755)
}

// PushGuardEnv adds the guard to env as a config-defined pre-push hook
// via GIT_CONFIG_COUNT/KEY/VALUE, after any entries already there.
func PushGuardEnv(env []string, script string) []string {
	n := 0
	out := make([]string, 0, len(env)+5)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			n, _ = strconv.Atoi(v)
			continue
		}
		out = append(out, kv)
	}
	add := func(k, v string) {
		out = append(out, "GIT_CONFIG_KEY_"+strconv.Itoa(n)+"="+k, "GIT_CONFIG_VALUE_"+strconv.Itoa(n)+"="+v)
		n++
	}
	add("hook.dhi-default-branch.event", "pre-push")
	add("hook.dhi-default-branch.command", script)
	return append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(n))
}
