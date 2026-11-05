package clirun

import (
	"slices"
	"strings"
	"testing"
)

func TestEveryAdapterWithoutImagesSaysWhy(t *testing.T) {
	for _, c := range allAdapters() {
		if !c.ImagesOK && c.ImagesWhy == "" {
			t.Errorf("%s: no ImagesOK and no ImagesWhy", c.Name)
		}
		if c.ImagesFlag && !c.ImagesOK {
			t.Errorf("%s: ImagesFlag without ImagesOK", c.Name)
		}
	}
}

func TestCodexImagesAreRepeatedFlagsBeforeAnyPositional(t *testing.T) {
	in := RunInput{Prompt: "look", Workdir: "/w", Images: []string{"/a/b.png", "/c d/e.jpg"}}
	args := Codex.BuildArgs(in)
	i := slices.Index(args, "--image")
	if i < 0 || args[i+1] != "/a/b.png" || !slices.Contains(args, "/c d/e.jpg") {
		t.Fatalf("args = %v", args)
	}
	if last := args[len(args)-1]; !strings.Contains(last, "look") {
		t.Fatalf("the prompt must stay the final positional: %v", args)
	}
	// Greedy --image: a flag must follow the last path.
	j := slices.Index(args, "/c d/e.jpg")
	if !strings.HasPrefix(args[j+1], "-") {
		t.Fatalf("a positional follows --image: %v", args)
	}
	in.Resume = "T1"
	if args := Codex.BuildArgs(in); !slices.Contains(args, "--image") || slices.Index(args, "resume") > slices.Index(args, "--image") {
		t.Fatalf("resume form: %v", args)
	}
	if args := Codex.BuildArgs(RunInput{Prompt: "p", Workdir: "/w"}); slices.Contains(args, "--image") {
		t.Fatalf("no images, no flag: %v", args)
	}
}

func TestOnlyClaudeAndCodexTakeImages(t *testing.T) {
	var ok []string
	for _, c := range allAdapters() {
		if c.ImagesOK {
			ok = append(ok, c.Name)
		}
	}
	slices.Sort(ok)
	if strings.Join(ok, ",") != "claude,codex" {
		t.Fatalf("ImagesOK = %v", ok)
	}
}
