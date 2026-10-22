package kit

import "testing"

func TestEditText(t *testing.T) {
	cases := []struct {
		value   string
		cur     int
		key     string
		want    string
		wantCur int
	}{
		{"hello world", 11, "alt+left", "hello world", 6},
		{"hello world", 11, "ctrl+left", "hello world", 6},
		{"hello world", 0, "alt+right", "hello world", 5},
		{"hello world", 11, "ctrl+w", "hello ", 6},
		{"hello world", 11, "alt+backspace", "hello ", 6},
		{"hello world", 0, "alt+d", " world", 0},
		{"hello world", 5, "ctrl+u", " world", 0},
		{"hello world", 5, "ctrl+k", "hello", 5},
		{"héllo", 2, "backspace", "hllo", 1}, // runes, not bytes
		{"a😀b", 2, "backspace", "ab", 1},
		{"ab", 1, "¡", "a¡b", 2},
		{"ab", 2, "paste:x\ny\n", "abx y", 5}, // single line: trailing newline dropped
		{"ab", 2, "paste:\tz", "ab z", 4},
		{"line1\nline2", 8, "ctrl+a", "line1\nline2", 6},
		{"line1\nline2", 8, "ctrl+e", "line1\nline2", 11},
		{"ab", 2, "ctrl+j", "ab", 2}, // not multiline: not handled
	}
	for _, c := range cases {
		got, cur, _ := EditText(c.value, c.cur, c.key, false)
		if got != c.want || cur != c.wantCur {
			t.Errorf("EditText(%q, %d, %q) = %q,%d; want %q,%d", c.value, c.cur, c.key, got, cur, c.want, c.wantCur)
		}
	}
	if v, cur, ok := EditText("ab", 1, "ctrl+j", true); !ok || v != "a\nb" || cur != 2 {
		t.Errorf("multiline ctrl+j = %q,%d,%v", v, cur, ok)
	}
	if v, _, _ := EditText("", 0, "paste:a\r\nb", true); v != "a\nb" {
		t.Errorf("multiline paste normalizes CRLF: %q", v)
	}
	if _, _, ok := EditText("x", 1, "enter", false); ok {
		t.Error("enter is the caller's key")
	}
}

func TestInputUndoRedo(t *testing.T) {
	in := NewInput("", "")
	for _, k := range []string{"a", "b", "c"} {
		in.HandleKey(k)
	}
	in.HandleKey("ctrl+w")
	if in.Value != "" {
		t.Fatalf("ctrl+w left %q", in.Value)
	}
	in.HandleKey("ctrl+z")
	if in.Value != "abc" {
		t.Fatalf("undo gave %q", in.Value)
	}
	in.HandleKey("ctrl+shift+z")
	if in.Value != "" {
		t.Fatalf("redo gave %q", in.Value)
	}
	in.Paste("tok\n")
	if in.Value != "tok" || in.WithCursor("|") != "tok|" {
		t.Fatalf("paste gave %q", in.Value)
	}
}
