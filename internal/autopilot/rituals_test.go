package autopilot

import (
	"strings"
	"testing"
)

func TestRitualsAddedPausedOnce(t *testing.T) {
	rs := Rituals(map[string]string{"core": "scout", "web": ""}, []string{"core", "web"}, []string{"scout", "dev"})
	if len(rs) != 3 || rs[0].Slug != "standup-core" || rs[0].Agent != "scout" || rs[2].Slug != "review-dev" {
		t.Fatalf("rituals = %+v", rs)
	}
	if !strings.Contains(rs[0].Prompt, "#core") || !strings.Contains(rs[2].Prompt, "lesson_record") {
		t.Fatal("ritual prompts must name their channel and the learning tools")
	}
	s := withStore(t, nil)
	n, err := s.AddRituals(rs)
	if err != nil || n != 3 {
		t.Fatalf("added %d, %v", n, err)
	}
	for _, c := range s.List() {
		if c.Enabled {
			t.Fatalf("%s must start paused", c.Slug)
		}
	}
	if n, _ := s.AddRituals(rs); n != 0 {
		t.Fatal("rituals are added once")
	}
}
