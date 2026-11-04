// Package otelexport sends agent runs as OpenTelemetry traces (F-164),
// off unless the user sets the standard OTEL_EXPORTER_OTLP_* variables.
// It speaks OTLP/HTTP JSON by hand: one span per run, metadata only (no
// prompt, reply, command or path text), so no SDK dependency is needed.
// Submit never blocks a run: a bounded queue drops when full and counts.
package otelexport

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// QueueSize bounds the pending spans; beyond it new ones are dropped.
const QueueSize = 256

// Config is the export target read from the standard environment.
type Config struct {
	URL     string            // full traces URL; "" = export off
	Headers map[string]string // OTEL_EXPORTER_OTLP_HEADERS
	Service string            // OTEL_SERVICE_NAME, default "dhi"
}

// Enabled reports whether export is configured.
func (c Config) Enabled() bool { return c.URL != "" }

// FromEnv reads KEY=VALUE pairs. The traces-specific endpoint is used
// as given; the generic one gets "/v1/traces" appended (OTLP/HTTP spec).
func FromEnv(environ []string) Config {
	get := func(k string) string {
		v := ""
		for _, kv := range environ {
			if n, val, ok := strings.Cut(kv, "="); ok && n == k {
				v = val
			}
		}
		return strings.TrimSpace(v)
	}
	c := Config{Service: get("OTEL_SERVICE_NAME")}
	if c.Service == "" {
		c.Service = "dhi"
	}
	switch {
	case get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "":
		c.URL = get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	case get("OTEL_EXPORTER_OTLP_ENDPOINT") != "":
		c.URL = strings.TrimRight(get("OTEL_EXPORTER_OTLP_ENDPOINT"), "/") + "/v1/traces"
	}
	if h := get("OTEL_EXPORTER_OTLP_TRACES_HEADERS"); h != "" {
		c.Headers = parseHeaders(h)
	} else if h := get("OTEL_EXPORTER_OTLP_HEADERS"); h != "" {
		c.Headers = parseHeaders(h)
	}
	return c
}

func parseHeaders(s string) map[string]string {
	out := map[string]string{}
	for _, p := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if u, err := url.PathUnescape(v); err == nil {
			v = u
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// Where is the endpoint with any credentials and query stripped, safe to
// show in doctor.
func (c Config) Where() string {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" {
		return "(unparseable endpoint)"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// Stats is the exporter's health for doctor.
type Stats struct {
	Sent    int64
	Dropped int64  // queue was full
	Failed  int64  // spans the collector did not accept
	LastErr string // the failure since the last success; "" = healthy
}

// Exporter is an async OTLP/HTTP JSON trace sender.
type Exporter struct {
	cfg    Config
	client *http.Client
	q      chan []byte
	done   chan struct{}

	sent, dropped, failed atomic.Int64
	mu                    sync.Mutex
	lastErr               string
}

// New starts an exporter; nil when cfg is not enabled.
func New(cfg Config) *Exporter {
	if !cfg.Enabled() {
		return nil
	}
	e := &Exporter{cfg: cfg, client: &http.Client{Timeout: 5 * time.Second},
		q: make(chan []byte, QueueSize), done: make(chan struct{})}
	go e.loop()
	return e
}

// Config returns the target.
func (e *Exporter) Config() Config { return e.cfg }

// Stats returns the counters.
func (e *Exporter) Stats() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Stats{Sent: e.sent.Load(), Dropped: e.dropped.Load(), Failed: e.failed.Load(), LastErr: e.lastErr}
}

// Submit queues one run; it never blocks. Nil-safe. toolCalls is the
// number of tool commands the run issued (a count, never their text).
func (e *Exporter) Submit(r tasks.Run, toolCalls int) {
	if e == nil {
		return
	}
	body, err := json.Marshal(e.payload(r, toolCalls))
	if err != nil {
		return
	}
	select {
	case e.q <- body:
	default:
		e.dropped.Add(1)
	}
}

// Close drains what is queued, then stops the sender. Nil-safe.
func (e *Exporter) Close() {
	if e == nil {
		return
	}
	close(e.q)
	<-e.done
}

func (e *Exporter) loop() {
	defer close(e.done)
	for body := range e.q {
		err := e.post(body)
		e.mu.Lock()
		if err != nil {
			e.failed.Add(1)
			if e.lastErr == "" { // surfaced once in doctor, not per run
				e.lastErr = err.Error()
			}
		} else {
			e.sent.Add(1)
			e.lastErr = ""
		}
		e.mu.Unlock()
	}
}

func (e *Exporter) post(body []byte) error {
	req, err := http.NewRequest(http.MethodPost, e.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("bad endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		// url.Error embeds the URL, which may carry a token.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("collector answered HTTP %d", resp.StatusCode)
	}
	return nil
}

type kv struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func str(k, v string) kv { return kv{k, map[string]string{"stringValue": v}} }
func num(k string, v int64) kv {
	return kv{k, map[string]string{"intValue": strconv.FormatInt(v, 10)}}
}
func dbl(k string, v float64) kv { return kv{k, map[string]float64{"doubleValue": v}} }

func nanos(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// payload builds the OTLP ExportTraceServiceRequest for one run. Only
// metadata: ids, names, counts, numbers.
func (e *Exporter) payload(r tasks.Run, toolCalls int) map[string]any {
	attrs := []kv{
		str("dhi.agent", r.Agent), str("dhi.runtime", r.Runtime),
		num("dhi.attempt", int64(r.Attempt)), str("dhi.status", string(r.Status)),
		num("dhi.exit", int64(r.Exit)), num("dhi.tool_calls", int64(toolCalls)),
	}
	if r.Model != "" {
		attrs = append(attrs, str("gen_ai.request.model", r.Model))
	}
	if r.StopReason != "" {
		attrs = append(attrs, str("dhi.stop_reason", r.StopReason))
	}
	if r.TokensIn >= 0 {
		attrs = append(attrs, num("gen_ai.usage.input_tokens", int64(r.TokensIn)))
	}
	if r.TokensOut >= 0 {
		attrs = append(attrs, num("gen_ai.usage.output_tokens", int64(r.TokensOut)))
	}
	if r.HasCost {
		attrs = append(attrs, dbl("dhi.cost_usd", r.CostUSD))
	}
	attrs = append(attrs, num("dhi.duration_ms", r.Finished.Sub(r.Started).Milliseconds()))
	code := 1 // STATUS_CODE_OK
	if r.Status != tasks.RunOK {
		code = 2 // STATUS_CODE_ERROR, no message: the error text stays local
	}
	span := map[string]any{
		"traceId": randHex(16), "spanId": randHex(8),
		"name": "agent.run " + r.Agent, "kind": 1,
		"startTimeUnixNano": nanos(r.Started), "endTimeUnixNano": nanos(r.Finished),
		"attributes": attrs,
		"status":     map[string]any{"code": code},
	}
	return map[string]any{"resourceSpans": []any{map[string]any{
		"resource": map[string]any{"attributes": []kv{str("service.name", e.cfg.Service)}},
		"scopeSpans": []any{map[string]any{
			"scope": map[string]string{"name": "dhi"},
			"spans": []any{span},
		}},
	}}}
}

var current atomic.Pointer[Exporter]

// SetCurrent registers the process's exporter so doctor can report its
// counters; nil clears it.
func SetCurrent(e *Exporter) { current.Store(e) }

// Current is the registered exporter, or nil when export is off.
func Current() *Exporter { return current.Load() }
