package lsp

import "testing"

func TestUTF16Columns(t *testing.T) {
	line := "a😀b" // 😀 is two UTF-16 units
	for runeCol, u16 := range []int{0, 1, 3, 4} {
		if got := UTF16Col(line, runeCol); got != u16 {
			t.Errorf("UTF16Col(%d) = %d, want %d", runeCol, got, u16)
		}
		if got := RuneCol(line, u16); got != runeCol {
			t.Errorf("RuneCol(%d) = %d, want %d", u16, got, runeCol)
		}
	}
	if RuneCol("héllo", 99) != 5 || UTF16Col("héllo", 2) != 2 {
		t.Error("BMP text must count one unit per rune")
	}
}

func TestDecodeCompletion(t *testing.T) {
	items := decodeCompletion([]byte(`{"items":[
		{"label":"Println","insertText":"Println"},
		{"label":"Printf","textEdit":{"newText":"Printf(${1:format})","range":{"start":{"line":0,"character":4},"end":{"line":0,"character":7}}},"insertTextFormat":2},
		{"label":"x"}]}`))
	if len(items) != 3 || items[0].Insert != "Println" || items[1].Insert != "Printf(format)" ||
		items[1].Range == nil || items[1].Range.Start.Character != 4 || items[2].Insert != "x" {
		t.Fatalf("%+v", items)
	}
	if StripSnippet(`a${1:b${2:c}}$0\$`) != "abc$" {
		t.Fatal(StripSnippet(`a${1:b${2:c}}$0\$`))
	}
}
