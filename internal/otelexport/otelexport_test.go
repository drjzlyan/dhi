package otelexport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/tasks"
)

func TestFromEnv(t *testing.T) {
	if FromEnv(nil).Enabled() {
		t.Fatal("no env must mean off")
	}
	c := FromEnv([]string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://h:4318/", "OTEL_EXPORTER_OTLP_HEADERS=a=1, b=x%20y", "OTEL_SERVICE_NAME=me"})
	if c.URL != "http://h:4318/v1/traces" || c.Service != "me" || c.Headers["a"] != "1" || c.Headers["b"] != "x y" {
		t.Fatalf("%+v", c)
	}
	c = FromEnv([]string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://h:4318", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://t/x"})
	if c.URL != "http://t/x" || c.Service != "dhi" {
		t.Fatalf("%+v", c)
	}
	if w := (Config{URL: "https://u:p@h/v1/traces?token=s"}).Where(); strings.ContainsAny(w, "@?") {
		t.Fatalf("leaks credentials: %s", w)
	}
}

func run(ok bool) tasks.Run {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := tasks.Run{Agent: "forge", Runtime: "cli:claude", Model: "m", Attempt: 1, Started: t0, Finished: t0.Add(2 * time.Second),
		Status: tasks.RunOK, TokensIn: 10, TokensOut: 20, HasCost: true, CostUSD: 0.5,
		Summary: "SECRET-REPLY", Error: "SECRET-ERR"}
	if !ok {
		r.Status, r.StopReason = tasks.RunError, "loop"
	}
	return r
}

func TestExportsRunAsSpanWithoutText(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies, auth = append(bodies, string(b)), r.Header.Get("Authorization")
		mu.Unlock()
	}))
	defer srv.Close()
	e := New(Config{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer k"}, Service: "dhi"})
	e.Submit(run(true), 3)
	e.Submit(run(false), 0)
	e.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || auth != "Bearer k" {
		t.Fatalf("bodies=%d auth=%q", len(bodies), auth)
	}
	for _, b := range bodies {
		if strings.Contains(b, "SECRET") {
			t.Fatalf("text leaked: %s", b)
		}
	}
	var doc struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Name   string `json:"name"`
					Status struct{ Code int }
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(bodies[0]), &doc); err != nil {
		t.Fatal(err)
	}
	sp := doc.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if sp.Name != "agent.run forge" || sp.Status.Code != 1 {
		t.Fatalf("%+v", sp)
	}
	for _, want := range []string{"gen_ai.usage.input_tokens", "dhi.cost_usd", "dhi.tool_calls"} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(bodies[1], `"dhi.stop_reason"`) || !strings.Contains(bodies[1], `"code":2`) {
		t.Errorf("error run: %s", bodies[1])
	}
	if s := e.Stats(); s.Sent != 2 || s.Failed != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestFailureSurfacedOnceAndNeverBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	e := New(Config{URL: srv.URL})
	e.Submit(run(true), 0)
	e.Submit(run(true), 0)
	e.Close()
	s := e.Stats()
	if s.Failed != 2 || !strings.Contains(s.LastErr, "503") {
		t.Fatalf("%+v", s)
	}
}

func TestDropsWhenQueueFull(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	e := New(Config{URL: srv.URL})
	start := time.Now()
	for range QueueSize + 50 {
		e.Submit(run(true), 0)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Submit blocked")
	}
	if d := e.Stats().Dropped; d < 40 {
		t.Fatalf("dropped=%d", d)
	}
	close(block)
	e.Close()
}

func TestNilExporterSafe(t *testing.T) {
	var e *Exporter
	e.Submit(run(true), 0)
	e.Close()
	if New(Config{}) != nil {
		t.Fatal("disabled config must give nil")
	}
}
