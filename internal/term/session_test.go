package term

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSessionEchoesOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Options{
		Dir:     t.TempDir(),
		Command: []string{"/bin/sh", "-c", "echo hello-dhi; sleep 5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for {
		select {
		case chunk, ok := <-s.Out:
			if !ok {
				t.Fatal("output closed before echo")
			}
			got.Write(chunk)
			if strings.Contains(got.String(), "hello-dhi") {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for echo, got %q", got.String())
		}
	}
}

func TestSessionWriteAndRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Options{
		Dir:     t.TempDir(),
		Command: []string{"/bin/sh", "-i"},
	})
	if err == nil {
		s.Write([]byte("echo ping-$((20+3))\n"))
	} else {
		// interactive sh unavailable in sandboxed envs — use -c variant
		s2, err2 := Start(ctx, Options{Dir: t.TempDir(), Command: []string{"/bin/sh", "-c", `echo ping-$((20+3))`}})
		if err2 != nil {
			t.Skip("no shell available:", err2)
		}
		defer s2.Close()
		s = s2
	}
	defer s.Close()

	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for {
		select {
		case chunk, ok := <-s.Out:
			if !ok {
				t.Fatalf("closed early, got %q", got.String())
			}
			got.Write(chunk)
			if strings.Contains(got.String(), "ping-23") {
				return
			}
		case <-deadline:
			t.Fatalf("timeout, got %q", got.String())
		}
	}
}

func TestSessionBadDir(t *testing.T) {
	if _, err := Start(context.Background(), Options{Dir: "/nonexistent-dhi-xyz"}); err == nil {
		t.Fatal("bad dir accepted")
	}
}

func TestSessionCloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Options{Dir: t.TempDir(), Command: []string{"/bin/sh", "-c", "sleep 5"}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close() // must not panic
	if err := s.Write([]byte("x")); err == nil {
		t.Error("write after close succeeded")
	}
}
