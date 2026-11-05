package skills

import (
	"strings"
	"testing"
)

func parseSkill(t *testing.T, name, front string) Skill {
	t.Helper()
	s, err := Parse(name, []byte("---\nname: "+name+"\ndescription: d\n"+front+"---\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	return *s
}

func TestSlashDeclaration(t *testing.T) {
	for _, tc := range []struct {
		front string
		want  bool
	}{
		{"", false},
		{"user-invocable: true\n", true},
		{"user-invocable: false\n", false},
		{"disable-model-invocation: true\n", true},
		{"disable-model-invocation: true\nuser-invocable: false\n", false},
		{"user-invocable: \"true\"\n", true},
	} {
		if got := parseSkill(t, "x", tc.front).Slash(); got != tc.want {
			t.Errorf("%q: Slash() = %v want %v", tc.front, got, tc.want)
		}
	}
}

func TestSlashFieldsRoundTripThroughWrite(t *testing.T) {
	dir := t.TempDir()
	s := parseSkill(t, "deploy", "user-invocable: true\ndisable-model-invocation: true\n")
	if err := Write(dir, &s); err != nil {
		t.Fatal(err)
	}
	back, err := ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Slash() || !back.ModelDisabled {
		t.Fatalf("a Settings edit must keep the declaration: %+v", back)
	}
}

func TestInvocationBuiltinsWinAndOnlyDeclaredSkillsRun(t *testing.T) {
	list := []Skill{
		parseSkill(t, "deploy", "user-invocable: true\n"),
		parseSkill(t, "plan", "user-invocable: true\n"),
		parseSkill(t, "notes", ""),
	}
	if k, args, ok := Invocation("/deploy  to prod", list); !ok || k.Slug != "deploy" || args != "to prod" {
		t.Fatalf("deploy: %v %q %v", k.Slug, args, ok)
	}
	if k, _, ok := Invocation("@scout /deploy x", list); !ok || k.Slug != "deploy" {
		t.Fatalf("after a mention: %v %v", k.Slug, ok)
	}
	for _, in := range []string{"/plan x", "/notes x", "/unknown", "deploy", "/usr/bin/x"} {
		if _, _, ok := Invocation(in, list); ok {
			t.Errorf("%q must not run a skill", in)
		}
	}
	if n := ShadowNote(list); !strings.Contains(n, "built-in /plan wins") {
		t.Errorf("ShadowNote = %q", n)
	}
	got := Complete("p", list)
	if len(got) != 1 || got[0] != "plan" {
		t.Errorf("Complete(p) = %v: the built-in is offered once, the shadowed skill not at all", got)
	}
	if got := Complete("d", list); len(got) != 1 || got[0] != "deploy" {
		t.Errorf("Complete(d) = %v", got)
	}
}

func TestPromptCarriesTheActivationAndArgs(t *testing.T) {
	s := parseSkill(t, "deploy", "user-invocable: true\n")
	if p := Prompt(s, "staging"); !strings.Contains(p, `<skill_content name="deploy">`) || !strings.HasSuffix(p, "with: staging") {
		t.Fatalf("prompt = %q", p)
	}
	if p := Prompt(s, ""); !strings.Contains(p, "no arguments") {
		t.Fatalf("prompt = %q", p)
	}
}
