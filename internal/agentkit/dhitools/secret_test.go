package dhitools

import (
	"context"
	"strings"
	"testing"
)

type envRunner struct {
	fakeRunner
	extra []string
}

func (e *envRunner) RunEnv(ctx context.Context, dir string, argv []string, allowNetwork bool, extra []string) (string, error) {
	e.extra = extra
	return e.Run(ctx, dir, argv, allowNetwork)
}

// F-089: granted secrets reach `run` as env, nowhere else.
func TestRunPassesGrantedSecrets(t *testing.T) {
	f, m := newFixture(t, "run")
	er := &envRunner{fakeRunner: fakeRunner{out: "ok\n"}}
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: er, Approvals: f.approvals,
		SecretEnv: func() []string { return []string{"NPM_TOKEN=npm_abc"} }}.Handler()
	if out, isErr := callAsync(h, "run", `{"program":"npm","args":["test"]}`, f)(t); isErr {
		t.Fatalf("run refused: %s", out)
	}
	if strings.Join(er.extra, ",") != "NPM_TOKEN=npm_abc" {
		t.Fatalf("env = %v", er.extra)
	}
}

// A runner that cannot add env refuses rather than run without them.
func TestRunRefusesSecretsWithoutAnEnvRunner(t *testing.T) {
	f, m := newFixture(t, "run")
	fr := &fakeRunner{out: "ok\n"}
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: fr, Approvals: f.approvals,
		SecretEnv: func() []string { return []string{"NPM_TOKEN=x"} }}.Handler()
	out, isErr := callAsync(h, "run", `{"program":"npm","args":["test"]}`, f)(t)
	if !isErr || fr.called || !strings.Contains(out, "cannot pass") {
		t.Fatalf("out=%q isErr=%v called=%v", out, isErr, fr.called)
	}
}

func TestSecretRequestValidatesTheName(t *testing.T) {
	f, m := newFixture(t)
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals,
		Secret: func(context.Context, string, string) (string, error) { return "granted", nil }}.Handler()
	if out, isErr := call(h, t, "secret_request", `{"name":"my key","reason":"x"}`); !isErr || !strings.Contains(out, "env var name") {
		t.Fatalf("bad name: %q %v", out, isErr)
	}
	if out, isErr := call(h, t, "secret_request", `{"name":"API_KEY","reason":"deploy"}`); isErr || out != "granted" {
		t.Fatalf("good name: %q %v", out, isErr)
	}
}
