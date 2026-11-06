package runtime

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/fsx"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-135: the prompt budget is in tokens but fitSections counts runes. The
// fixed runesPerToken guess is replaced, per CLI and model, by what the
// CLI's own usage reports: after a fresh (non-resumed) turn the real input
// size of the prompt (Run.Context) against the runes DHI sent.
//
// The CLI adds a fixed overhead of its own (its system prompt and tool
// definitions, often 10k+ tokens), so tokens = runes/ratio + overhead, not
// tokens = runes/ratio. A plain tokens/runes quotient would blame the
// overhead on the text and shrink every prompt. So the fit is a decayed
// least-squares line over the observations: its slope is the ratio, its
// intercept the overhead. A fit needs prompts of different sizes; until
// then the ratio stays at the default.

const (
	minRunesPerToken = 2.0
	maxRunesPerToken = 6.0
	// calDecay forgets old observations (a CLI or model update shifts
	// the ratio); calMinWeight and calMinSpread say when the fit is
	// trustworthy: enough recent turns, and prompt sizes that differ.
	calDecay     = 0.9
	calMinWeight = 3.0
	calMinSpread = 1500.0 // runes of standard deviation across the samples
	// calMinRunes skips turns too small to say anything about text.
	calMinRunes = 2000
)

// calSample holds the decayed sums of one CLI/model pair: weight, x=runes
// sent, y=real input tokens.
type calSample struct {
	N   float64 `json:"n"`
	Sx  float64 `json:"sx"`
	Sy  float64 `json:"sy"`
	Sxx float64 `json:"sxx"`
	Sxy float64 `json:"sxy"`
}

// ratio is the fitted runes per token, or ok=false without a usable fit.
func (c calSample) ratio() (float64, bool) {
	if c.N < calMinWeight {
		return 0, false
	}
	mx, my := c.Sx/c.N, c.Sy/c.N
	varX := c.Sxx/c.N - mx*mx
	if varX < calMinSpread*calMinSpread {
		return 0, false
	}
	slope := (c.Sxy/c.N - mx*my) / varX // tokens per rune
	if slope <= 0 {
		return 0, false
	}
	return math.Min(math.Max(1/slope, minRunesPerToken), maxRunesPerToken), true
}

// calibration is the smoothed state, kept in DHI's own folder
// (.dhi/calibration.json), not in a config file.
type calibration struct {
	mu   sync.Mutex
	path string
	m    map[string]calSample
	read bool
}

func newCalibration(wsRoot string) *calibration {
	return &calibration{path: filepath.Join(wsRoot, ".dhi", "calibration.json")}
}

func calKey(cli, model string) string { return cli + "/" + model }

// load reads the file once; the caller holds mu. A missing file is empty
// state; an unreadable one is an error, and the budget keeps the default.
func (c *calibration) load() error {
	if c.read {
		return nil
	}
	c.m = map[string]calSample{}
	b, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		c.read = true
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &c.m); err != nil {
		c.m = map[string]calSample{}
		return err
	}
	c.read = true
	return nil
}

// Ratio is the runes per token to budget with for a CLI and model: the
// fit when there is one, else the default of 4.
func (c *calibration) Ratio(cli, model string) float64 {
	if c == nil {
		return runesPerToken
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.load() != nil {
		return runesPerToken
	}
	if r, ok := c.m[calKey(cli, model)].ratio(); ok {
		return r
	}
	return runesPerToken
}

// Observe folds one turn in: runes DHI sent, the real input tokens the CLI
// reported for it. It returns a write error so the caller can name it.
func (c *calibration) Observe(cli, model string, runes, tokens int) error {
	if c == nil || runes < calMinRunes || tokens <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return err
	}
	k := calKey(cli, model)
	s := c.m[k]
	x, y := float64(runes), float64(tokens)
	s = calSample{N: s.N*calDecay + 1, Sx: s.Sx*calDecay + x, Sy: s.Sy*calDecay + y,
		Sxx: s.Sxx*calDecay + x*x, Sxy: s.Sxy*calDecay + x*y}
	c.m[k] = s
	b, err := json.MarshalIndent(c.m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	return fsx.WriteAtomic(c.path, b, 0o644)
}

// calibrate feeds one finished run to the fit. Only a fresh spawn says
// anything about the prompt: a resumed session's context also holds the
// whole earlier conversation. A CLI that reports no per-call context
// (Run.Context 0) contributes nothing; its ratio stays the default.
func (r *Runtime) calibrate(trigger bus.Message, e *entry, ep episode.Episode, run tasks.Run, fresh bool, sentRunes int) {
	if !fresh || run.Status != tasks.RunOK || run.Context <= 0 {
		return
	}
	model := e.m.CLIModel()
	if ep.Model != "" {
		model = ep.Model
	}
	if err := r.cal.Observe(e.cli.Name, model, sentRunes, run.Context); err != nil {
		r.notice(trigger, e.m.ID, "could not save the prompt-size calibration: "+err.Error())
	}
}
