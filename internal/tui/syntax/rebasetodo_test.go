package syntax

import "testing"

// F-165: the rebase todo gets command, hash and comment colours; the text is
// unchanged and a typo'd command stays plain.
func TestLexRebaseTodo(t *testing.T) {
	text := "pick abc1234 first\nreword def5678 second\nsquash 1112223 third\nfixup 4445556\ndrop 7778889 gone\nexec make test\nbogus 123 x\n\n# Rebase help\n"
	lines := Lex("/r/.git/rebase-merge/git-rebase-todo", text)
	if len(lines) != 10 {
		t.Fatalf("lines = %d", len(lines))
	}
	for i, want := range []string{"pick abc1234 first", "reword def5678 second", "squash 1112223 third", "fixup 4445556", "drop 7778889 gone", "exec make test", "bogus 123 x", "", "# Rebase help", ""} {
		if got := lines[i].Text(); got != want {
			t.Errorf("line %d = %q, want %q", i, got, want)
		}
	}
	class := func(l, span int) Class { return lines[l][span].Class }
	if class(0, 0) != Keyword || class(1, 0) != Keyword {
		t.Error("pick and reword are keywords")
	}
	if class(2, 0) != Func || class(3, 0) != Func {
		t.Error("squash and fixup are folded commits")
	}
	if class(4, 0) != Operator {
		t.Error("drop stands out")
	}
	if class(0, 2) != Number {
		t.Error("the hash is a number")
	}
	if class(5, 0) != Tag {
		t.Error("exec is a tag")
	}
	if class(6, 0) != Plain || len(lines[6]) != 1 {
		t.Errorf("unknown command must stay plain: %+v", lines[6])
	}
	if class(8, 0) != Comment {
		t.Error("# lines are comments")
	}
}

func TestOtherPathsAreNotTodos(t *testing.T) {
	if got := Lex("notes.txt", "pick abc x\n"); len(got) > 0 && len(got[0]) > 0 && got[0][0].Class == Keyword {
		t.Error("only git-rebase-todo is lexed as a todo")
	}
}
