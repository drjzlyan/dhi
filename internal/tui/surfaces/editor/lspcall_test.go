package editor

import (
	"context"
	"strings"
	"testing"
)

func TestLSPCallRefusesWithoutClient(t *testing.T) {
	m := newEditor(t)
	if _, err := m.LSPCall(context.Background(), "hover", "/x.go", 0, 0, ""); err == nil ||
		!strings.Contains(err.Error(), "LSP unavailable") {
		t.Fatalf("err = %v, want LSP-unavailable refusal", err)
	}
	if _, err := m.LSPCall(context.Background(), "rename", "/x.go", 0, 0, "New"); err == nil {
		t.Fatal("rename without a client must refuse")
	}
}
