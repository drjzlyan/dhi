package conventions

import "sync"

// Source yields the conventions in force right now. Consumers call it at the
// moment they need a rule (a commit, a branch name, a prompt), so an edit to
// the conventions file applies without restarting DHI (F-052). A nil Source
// means "no extra rules"; a Source may return nil likewise.
type Source func() *Config

// Static is a Source that never changes.
func Static(c Config) Source {
	cc := c
	return func() *Config { return &cc }
}

// Live re-reads the conventions when the files behind them change. It keeps
// the last good value when a reload fails (a half-typed edit must not drop
// the team's rules) and remembers the error for display.
type Live struct {
	mu          sync.Mutex
	cur         Config
	fp          string
	fingerprint func() string
	load        func() (Config, error)
	err         error
}

// NewLive starts from initial, which is taken to match the current
// fingerprint. fingerprint must change whenever any source file changes;
// load builds the full layered Config.
func NewLive(initial Config, fingerprint func() string, load func() (Config, error)) *Live {
	return &Live{cur: initial, fp: fingerprint(), fingerprint: fingerprint, load: load}
}

// Get returns the conventions in force, reloading first if the files changed.
func (l *Live) Get() Config {
	l.mu.Lock()
	defer l.mu.Unlock()
	if fp := l.fingerprint(); fp != l.fp {
		l.fp = fp // one attempt per edit, success or not
		if c, err := l.load(); err != nil {
			l.err = err
		} else {
			l.cur, l.err = c, nil
		}
	}
	return l.cur
}

// Err is the error of the latest failed reload ("" nil once a reload works).
func (l *Live) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Source adapts the Live value for consumers.
func (l *Live) Source() Source {
	return func() *Config {
		c := l.Get()
		return &c
	}
}
