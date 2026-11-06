package lsp

import (
	"encoding/json"
	"testing"
)

func TestDidSaveCapabilities(t *testing.T) {
	tests := []struct {
		name            string
		caps            map[string]json.RawMessage
		wantSend        bool
		wantIncludeText bool
	}{
		{
			name:            "nil capabilities (test doubles benefit of doubt)",
			caps:            nil,
			wantSend:        true,
			wantIncludeText: false,
		},
		{
			name: "textDocumentSync omitted",
			caps: map[string]json.RawMessage{
				"hoverProvider": json.RawMessage(`true`),
			},
			wantSend:        false,
			wantIncludeText: false,
		},
		{
			name: "textDocumentSync is a number (old style)",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`1`),
			},
			wantSend:        false,
			wantIncludeText: false,
		},
		{
			name: "save omitted in textDocumentSync object",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`{"change": 1}`),
			},
			wantSend:        false,
			wantIncludeText: false,
		},
		{
			name: "save: true",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`{"save": true}`),
			},
			wantSend:        true,
			wantIncludeText: false,
		},
		{
			name: "save: false",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`{"save": false}`),
			},
			wantSend:        false,
			wantIncludeText: false,
		},
		{
			name: "save: {} (object without includeText)",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`{"save": {}}`),
			},
			wantSend:        true,
			wantIncludeText: false,
		},
		{
			name: "save: {includeText: true}",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`{"save": {"includeText": true}}`),
			},
			wantSend:        true,
			wantIncludeText: true,
		},
		{
			name: "save: {includeText: false}",
			caps: map[string]json.RawMessage{
				"textDocumentSync": json.RawMessage(`{"save": {"includeText": false}}`),
			},
			wantSend:        true,
			wantIncludeText: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{caps: tt.caps}
			send, includeText := c.SaveOptions()
			if send != tt.wantSend || includeText != tt.wantIncludeText {
				t.Errorf("SaveOptions() = (%v, %v), want (%v, %v)",
					send, includeText, tt.wantSend, tt.wantIncludeText)
			}
		})
	}
}
