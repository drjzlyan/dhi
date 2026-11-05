package runtime

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/hooks"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/trace"
)

// Plugin lifecycle hooks (F-179, ADR-0047). The user turns a plugin's hooks
// on in Settings; the runtime loads the enabled ones for each turn and
// fires them at DHI's tool boundary (hooks.Wrap in serveToolsFor) and at
// the run's edges (SessionStart, UserPromptSubmit, Stop). A CLI that takes
// hooks per run (claude --settings) also gets the tool hooks for its own
// native tools, through a DHI temp file.

// hookLog collects one turn's hook records for the saved transcript.
type hookLog struct {
	mu   sync.Mutex
	recs []trace.Record
}

func (l *hookLog) add(rec hooks.Record) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = append(l.recs, trace.Record{Kind: "hook", Detail: rec.String(), At: time.Now().UTC()})
}

// drain returns the records since the last drain: each attempt's saved
// transcript carries what happened before and during it.
func (l *hookLog) drain() []trace.Record {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.recs
	l.recs = nil
	return out
}

// joinHookCtx appends fenced hook output to the system block.
func joinHookCtx(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p != "" {
			out += "\n\n" + p
		}
	}
	return out
}

// hookRunner builds the turn's runner from the plugins the user enabled,
// or nil when none are. A plugin that cannot load is a named notice, not a
// silent skip. Hooks run in the agent's own sandbox with its scopes.
func (r *Runtime) hookRunner(e *entry, trigger bus.Message, set scopes.Set, log *hookLog) *hooks.Runner {
	if r.cfg.HooksLoad == nil {
		return nil
	}
	plugins, problems := r.cfg.HooksLoad()
	for _, p := range problems {
		r.notice(trigger, e.m.ID, "plugin hooks not run: "+p)
	}
	if len(plugins) == 0 {
		return nil
	}
	env := append([]string(nil), r.baseEnv()...)
	if dir := r.agentTmp(e.m.ID); dir != "" {
		env = setEnv(env, "TMPDIR="+dir)
	}
	return &hooks.Runner{
		Plugins: plugins,
		Scopes:  set,
		Exec:    hooks.SandboxExec(r.spawnSandbox(e)),
		Dir:     r.cliWorkdir(trigger),
		Env:     env,
		Session: trigger.Channel,
		Record: func(rec hooks.Record) {
			log.add(rec)
			// Every non-allow decision is said in the thread. Stop runs after the
			// last transcript is saved, so it is said there too.
			if rec.Kind != hooks.Allowed || rec.Event == hooks.Stop {
				r.notice(trigger, e.m.ID, rec.String())
			}
		},
	}
}

// nativeHooksFile writes the per-run settings file for a CLI that takes
// plugin hooks per run, and marks the runner as covered natively. The file
// is a DHI temp file; the user's CLI config is not touched. Returns "" and
// a no-op when the CLI has no such input or no tool hook applies.
func (r *Runtime) nativeHooksFile(e *entry, hk *hooks.Runner, trigger bus.Message) (string, func()) {
	if hk == nil || e.cli == nil || e.cli.HooksArgs == nil {
		return "", func() {}
	}
	body, n := hooks.NativeSettings(hk.Plugins)
	if n == 0 {
		return "", func() {}
	}
	if hk.Scopes.EffectFor(scopes.Exec) == scopes.Deny {
		return "", func() {} // the runner records the skip when a tool fires
	}
	f, err := os.CreateTemp("", "dhi-hooks-*.json")
	if err != nil {
		r.notice(trigger, e.m.ID, "plugin hooks for "+e.cli.Name+"'s own tools not wired this turn: "+err.Error())
		return "", func() {}
	}
	path := f.Name()
	_, werr := f.WriteString(body)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(path)
		r.notice(trigger, e.m.ID, "plugin hooks for "+e.cli.Name+"'s own tools not wired this turn: cannot write "+filepath.Base(path))
		return "", func() {}
	}
	hk.Native = true
	return path, func() { _ = os.Remove(path) }
}
