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
	beforeSave  func(*Editor) string
	afterSave   func(*Editor)
	saveNote    string
	lastSel     *Selected // selection captured when ":" left visual mode
	markLo      int       // '< and '> lines of the last visual selection (F-137)
	markHi      int
	markSet     bool

	cmdline    []rune  // content after ':', '/' or '?'
	cmdPrefix  rune    // ':' (ex command), '/' or '?' (search)
	lastSearch string  // the last search, repeated by n/N
	lastDir    int     // 1 after "/", -1 after "?"
	page       int     // lines on one screen (SetPageSize)
	yanked     *string // the last yank, until TakeYank (system clipboard)
	message    string  // transient status message ("", "3 fewer lines", errors)
	register   string
	registerLW bool // register holds linewise text

	path        string
	closeReq    bool
	closeForced bool
	aborted     bool // :cq — the close is a failure (F-165)
	reloaded    bool
	delegate    CommandDelegate

	indent string // what Tab inserts ("" = a tab) (F-119)
	noPair bool   // bracket/quote pairing off

	visualLine   bool // V: whole lines (F-119)
	visualBlock  bool // ctrl+v: a rectangle by display column (F-159)
	blockEOL     bool // "$" in a block: each line's end
	pendRepl     bool // r waiting for its character in block mode
	preGrouped   bool // an undo group is already open for the insert about to start
	blockIns     *blockInsert
	blockReg     []string // the last block yank/delete, one string per line
	blockRegText string   // register text it matched; a later yank breaks the match
	blockW       int
	tabW         int
	pendShift    string   // ">" or "<" waiting for its double (>>, <<)
	rec          []string // keys of the change being made, for "."
	recSeq       uint64   // buffer seq when it started
	lastChange   []string // the last change's keys
	replaying    bool
	hlOff        bool       // matches of the last search are not marked
	cfBlocks     []Conflict // parsed merge conflicts, valid while cfSeq == buf.seq+1 (F-156)
	cfSeq        uint64

	mc      *multi // secondary cursors (F-174); nil = the single-cursor path
	inMulti bool   // a key is running at each cursor: Key must not fan out again

	pendObj  rune   // 'i' or 'a' waiting for its object character (F-167)
	objLast  [2]Pos // anchor and cursor after the last visual text object
	objSel   bool
	selStack []selFrame // selections to shrink back to (F-167)
	selLast  [2]Pos     // anchor and cursor the last expand/shrink left
	selChain bool       // selLast is meaningful

	pendFind rune     // 'f', 'F', 't', 'T' waiting for target char, or 0 (F-133)
	lastFind struct { // last find motion for ; and , (F-133)
		kind rune // 'f', 'F', 't', 'T'
		ch   rune
	}
}

type op uint8

const (
	opNone op = iota
	opDelete
	opChange
	opYank
	opComment // gc{motion} (F-150)
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

// Aborted reports that the buffer was left with :cq, the editor's failure
// exit; a program waiting on the buffer (git, F-165) treats it as cancelled.
func (e *Editor) Aborted() bool { return e.aborted }
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
		p := e.cmdPrefix
		if p == 0 {
			p = ':'
		}
		return string(p) + string(e.cmdline)
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

// AwaitingChar reports whether the editor is waiting for a target character
// after f/F/t/T (F-133).
func (e *Editor) AwaitingChar() bool { return e.pendFind != 0 }

// WantsRawKey reports a half-typed command whose next key is data for the
// buffer (an f/F/t/T target, an i/a object character, a block r character):
// the surface must not read it as the start of one of its own chords.
func (e *Editor) WantsRawKey() bool {
	return e.pendFind != 0 || e.pendObj != 0 || e.pendRepl
}
