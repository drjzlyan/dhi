package textbuf

// Mode is the active modal editing state.
type Mode uint8

// Editing modes.
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual
	ModeCommand
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeCommand:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// CommandDelegate handles ex commands the buffer core does not know
// (workspace-level concerns like buffer switching). Return true when
// the command was handled.
type CommandDelegate interface {
	ExecEx(requester *Editor, cmd string) bool
}

// Editor binds a Buffer to modal state: pending operator+count,
// visual anchor, command line, and the unnamed register.
type Editor struct {
	buf *Buffer

	mode        Mode
	pendingOp   op
	pendingCnt  int
	cntDigits   []rune
	visualStart Pos

	cmdline    []rune // content after ':'
	message    string // transient status message ("", "3 fewer lines", errors)
	register   string
	registerLW bool // register holds linewise text

	path        string
	closeReq    bool
	closeForced bool
	reloaded    bool
	delegate    CommandDelegate
}

type op uint8

const (
	opNone op = iota
	opDelete
	opChange
	opYank
)

// NewEditor wraps content in a modal editor.
func NewEditor(content string) *Editor {
	return &Editor{buf: New(content), mode: ModeNormal}
}

// OpenFile loads path into a modal editor.
func OpenFile(path string) (*Editor, error) {
	b, err := Open(path)
	if err != nil {
		return nil, err
	}
	e := &Editor{buf: b, mode: ModeNormal, path: path}
	return e, nil
}

// Buffer exposes the underlying buffer (rendering, tests).
func (e *Editor) Buffer() *Buffer { return e.buf }

// Mode reports the active mode.
func (e *Editor) Mode() Mode { return e.mode }

// Path returns the bound file path ("" when unbound).
func (e *Editor) Path() string { return e.path }

// SetPath binds a save target.
func (e *Editor) SetPath(p string) { e.path = p }

// RegisterText returns the unnamed register contents and whether they
// are linewise.
func (e *Editor) RegisterText() (string, bool) { return e.register, e.registerLW }

// Message returns the last status/error message for the command line.
func (e *Editor) Message() string { return e.message }

// CloseRequested reports a completed :q/:wq; CloseForced distinguishes
// :q!. The surface decides what closing means.
func (e *Editor) CloseRequested() bool { return e.closeReq }
func (e *Editor) CloseForced() bool    { return e.closeForced }
func (e *Editor) TakeClose() bool {
	c := e.closeReq
	e.closeReq = false
	e.closeForced = false
	return c
}

// Reloaded reports a completed :e (buffer reloaded from disk).
func (e *Editor) TakeReloaded() bool {
	r := e.reloaded
	e.reloaded = false
	return r
}

// CommandLine renders the status/command line area.
func (e *Editor) CommandLine() string {
	if e.mode == ModeCommand {
		return ":" + string(e.cmdline)
	}
	if e.message != "" {
		return e.message
	}
	return ""
}

// VisualStart is the anchor position where visual mode began.
func (e *Editor) VisualStart() Pos { return e.visualStart }

// SetCommandDelegate registers the workspace-level ex handler.
func (e *Editor) SetCommandDelegate(d CommandDelegate) { e.delegate = d }

// SetMessage sets the status-line text (used by delegates).
func (e *Editor) SetMessage(msg string) { e.message = msg }
