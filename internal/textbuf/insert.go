package textbuf

// keyInsert handles insert-mode keys.
func (e *Editor) keyInsert(key string) {
	switch key {
	case "esc", "ctrl+[":
		e.mode = ModeNormal
		// cursor steps left off the insert point like vim
		if e.buf.Cursor().Col > 0 {
			e.buf.cursor.Col--
			e.buf.wantCol = e.buf.Cursor().Col
		}
	case "enter":
		e.buf.BreakLine()
	case "backspace":
		e.buf.DeleteBackspace()
	case "delete":
		e.buf.DeleteForward()
	case "tab":
		e.buf.InsertString("\t")
	case "left":
		e.buf.move(motLeft, 1)
	case "right":
		e.buf.move(motRight, 1)
	case "up":
		e.buf.move(motUp, 1)
	case "down":
		e.buf.move(motDown, 1)
	default:
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			e.buf.InsertString(key)
		}
	}
}
