package syntax

import "strings"

// todoCommands maps each rebase todo command (and its one-letter form) to the
// class that colours it: what keeps a commit, what folds it away, what drops it.
var todoCommands = map[string]Class{
	"pick": Keyword, "p": Keyword, "reword": Keyword, "r": Keyword, "edit": Keyword, "e": Keyword,
	"squash": Func, "s": Func, "fixup": Func, "f": Func,
	"drop": Operator, "d": Operator,
	"exec": Tag, "x": Tag, "break": Tag, "b": Tag, "label": Tag, "l": Tag, "reset": Tag, "t": Tag,
	"merge": Tag, "m": Tag, "update-ref": Tag, "u": Tag,
}

// lexRebaseTodo colours a git-rebase-todo: the command, the commit hash and
// the # comment lines (F-165). Unknown first words stay plain, so a typo
// shows as uncoloured rather than fake-coloured.
func lexRebaseTodo(text string) []Line {
	if text == "" || len(text) > MaxSize {
		return nil
	}
	src := strings.Split(text, "\n")
	out := make([]Line, len(src))
	for i, ln := range src {
		trim := strings.TrimLeft(ln, " \t")
		lead := ln[:len(ln)-len(trim)]
		if strings.HasPrefix(trim, "#") {
			out[i] = Line{{Text: ln, Class: Comment}}
			continue
		}
		if trim == "" {
			if ln != "" {
				out[i] = Line{{Text: ln}}
			}
			continue
		}
		var line Line
		if lead != "" {
			line = append(line, Span{Text: lead})
		}
		word, rest, _ := strings.Cut(trim, " ")
		class, known := todoCommands[word]
		if !known {
			out[i] = append(line, Span{Text: trim})
			continue
		}
		line = append(line, Span{Text: word, Class: class})
		if rest == "" {
			out[i] = line
			continue
		}
		if class == Tag { // exec and friends: the rest is a command or a name
			out[i] = append(line, Span{Text: " " + rest})
			continue
		}
		hash, msg, _ := strings.Cut(rest, " ")
		line = append(line, Span{Text: " "}, Span{Text: hash, Class: Number})
		if msg != "" {
			line = append(line, Span{Text: " " + msg})
		}
		out[i] = line
	}
	return out
}
