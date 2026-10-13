package doctor

import (
	"strings"
	"testing"
)

func TestSecretsNamesTokenLikeEnvOnly(t *testing.T) {
	c := Secrets([]string{"PATH=/bin", "GITHUB_TOKEN=ghp_x", "OPENAI_API_KEY=sk", "EMPTY_TOKEN=", "HOME=/h"})
	if c[0].Status != Warn || c[0].Detail[:len("GITHUB_TOKEN, OPENAI_API_KEY")] != "GITHUB_TOKEN, OPENAI_API_KEY" {
		t.Fatalf("check = %+v", c[0])
	}
	if strings.Contains(c[0].Detail, "ghp_x") {
		t.Fatal("a value leaked into doctor")
	}
	if Secrets([]string{"PATH=/bin"})[0].Status != OK {
		t.Fatal("clean env warned")
	}
}
