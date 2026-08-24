package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

type fakeCaller struct {
	content string
	isErr   bool
	err     error
	gotName string
	gotArgs json.RawMessage
}

func (f *fakeCaller) CallTool(_ context.Context, name string, args json.RawMessage) (string, bool, error) {
	f.gotName = name
	f.gotArgs = args
	return f.content, f.isErr, f.err
}

func TestRemoteToolsAllow(t *testing.T) {
	p := pol(sandbox.Rule{Op: sandbox.OpNet, Effect: sandbox.Allow})
	c := &fakeCaller{content: "42"}
	tool := RemoteTools("Docs", PolicyGate(p, nil, "a"), c,
		[]RemoteInfo{{Name: "lookup", Description: "find", Schema: json.RawMessage(`{"type":"object"}`)}})[0]

	if tool.Def().Name != "mcp__docs__lookup" {
		t.Errorf("ref = %q", tool.Def().Name)
	}
	out, err := tool.Exec(context.Background(), json.RawMessage(`{"q":"x"}`))
	if err != nil || out != "42" {
		t.Errorf("exec = %q err = %v", out, err)
	}
	if c.gotName != "lookup" || string(c.gotArgs) != `{"q":"x"}` {
		t.Errorf("forwarded name=%q args=%s", c.gotName, c.gotArgs)
	}
}

func TestRemoteToolsDenyAndErrorMapping(t *testing.T) {
	p := pol() // default deny
	tool := RemoteTools("S", PolicyGate(p, nil, "a"), &fakeCaller{content: "x"},
		[]RemoteInfo{{Name: "t"}})[0]
	_, err := tool.Exec(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "default deny") {
		t.Fatalf("err = %v, want policy deny", err)
	}

	// Server-side isError surfaces as an Exec error carrying content.
	ap := pol(sandbox.Rule{Op: sandbox.OpNet, Effect: sandbox.Allow})
	boom := RemoteTools("S", PolicyGate(ap, nil, "a"), &fakeCaller{content: "kaput", isErr: true},
		[]RemoteInfo{{Name: "t"}})[0]
	if _, err := boom.Exec(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "kaput") {
		t.Errorf("err = %v, want server error text", err)
	}

	// Caller transport error propagates.
	net := RemoteTools("S", PolicyGate(ap, nil, "a"), &fakeCaller{err: context.DeadlineExceeded},
		[]RemoteInfo{{Name: "t"}})[0]
	if _, err := net.Exec(context.Background(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestRemoteToolsAskFlow(t *testing.T) {
	p := pol(sandbox.Rule{Op: sandbox.OpNet, Effect: sandbox.Ask})
	ap := NewApprovals()
	signals := make(chan *Approval, 4)
	ap.OnRequest = func(a *Approval) { signals <- a }
	tool := RemoteTools("S", PolicyGate(p, ap, "agent"), &fakeCaller{content: "ok"},
		[]RemoteInfo{{Name: "t"}})[0]

	done := make(chan error, 1)
	go func() {
		_, err := tool.Exec(context.Background(), nil)
		done <- err
	}()
	select {
	case a := <-signals:
		if a.Op != sandbox.OpNet || a.Target != "mcp__s__t" {
			t.Fatalf("approval = %+v", a)
		}
		ap.Resolve(a.ID, true)
	case <-time.After(2 * time.Second):
		t.Fatal("no approval surfaced")
	}
	if err := <-done; err != nil {
		t.Fatalf("approved exec errored: %v", err)
	}
}

func TestMCPToolNameSanitized(t *testing.T) {
	got := MCPToolName("My Docs!", "Look.Up-2")
	if got != "mcp__my_docs__look_up_2" {
		t.Errorf("name = %q", got)
	}
}
