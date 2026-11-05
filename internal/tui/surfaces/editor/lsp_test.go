package editor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/lsp"
)

// goplusFake is a minimal scripted language server over one pipe end.
// It echoes back the URIs it receives so fixtures stay path-agnostic.
type goplusFake struct {
	conn net.Conn
	rd   *bufio.Reader

	wmu      sync.Mutex // one frame at a time on the pipe
	mu       sync.Mutex
	lastOpen string // file URI seen in the last didOpen
	// declinePrepare makes prepareRename answer null (symbol not
	// renameable), for the F-009 validation path.
	declinePrepare bool
	// formatEdits is the textDocument/formatting reply; formatFail
	// answers with a JSON-RPC error instead (F-040).
	formatEdits []map[string]any
	formatFail  bool
	// events records didChange/formatting/didSave/didClose in arrival order;
	// lastChange is the text of the latest didChange (what the "server" believes).
	events     []string
	lastChange string
	// lastLang is the languageId of the latest didOpen (F-050); noFormat
	// makes the server omit documentFormattingProvider like pyright does.
	lastLang string
	noFormat bool
	// F-116: definition/references replies (nil = none) and an extra,
	// not-open file a rename also edits.
	locs        []map[string]any
	renameExtra string
	// wsSyms is the workspace/symbol reply (F-145).
	wsSyms []map[string]any
	// didSaveOpts controls server capabilities for save (F-132).
	// nil = default (no explicit capability); otherwise {}, {includeText: true}, etc.
	didSaveOpts any
	// didSaveEvents records (uri, hasText) pairs for each didSave received.
	didSaveEvents []map[string]any
	// didCloseUris records URIs for each didClose received.
	didCloseUris []string
	// F-158: codeAction replies per context.only ("" = any), resolve support,
	// the resolved action, and the executeCommand requests seen.
	// F-171: compItems replaces the default completion reply; initCaps is the
	// client capabilities of the initialize request.
	compItems    []map[string]any
	initCaps     string
	actionsFor   map[string][]map[string]any
	resolveCaps  bool
	resolved     map[string]any
	onlySeen     []string
	execCommands []string
	execEdit     map[string]any // sent as workspace/applyEdit while executing
	// F-173: extra server capabilities, the semantic token data, and the
	// requests seen ("semanticTokens", "prepareCallHierarchy", ...).
	extraCaps map[string]any
	semData   []int
	outerURI  string // the file call-hierarchy item "outer" lives in
	seen      []string
	// F-167: selectionRange capability, the reply chain (ranges, innermost
	// first), a forced error, and how many requests arrived.
	selCap   bool
	selChain []map[string]any
	selFail  bool
	selAsked int
	// F-169: inlayCap advertises inlayHintProvider; inlayHints is the reply
	// and inlayAsks counts the textDocument/inlayHint requests (their ranges
	// as "start-end" line pairs in inlayRanges).
	inlayCap    bool
	inlayHints  []map[string]any
	inlayAsks   int
	inlayRanges []string
	// F-175: outlineCap advertises documentSymbolProvider; outlineSyms (when
	// set) is the documentSymbol reply and outlineAsks counts the requests.
	outlineCap  bool
	outlineSyms []map[string]any
	outlineAsks int
	// F-176: foldCap advertises foldingRangeProvider; foldRanges is the reply,
	// foldFail answers with an error, foldAsks counts the requests.
	foldCap    bool
	foldRanges []map[string]any
	foldFail   bool
	foldAsks   int
}

func startFakeServer(t *testing.T) (*goplusFake, *lsp.Manager) {
	t.Helper()
	return startFakeServerFor(t, "go", false)
}

// startFakeServerFor injects the scripted server as the client of lang; with
// noFormat it reports capabilities that lack documentFormattingProvider.
func startFakeServerFor(t *testing.T, lang string, noFormat bool) (*goplusFake, *lsp.Manager) {
	return startFakeServerCfg(t, lang, noFormat, nil)
}

// startFakeServerCfg is startFakeServerFor with cfg run before the server
// answers initialize (capabilities are read then).
func startFakeServerCfg(t *testing.T, lang string, noFormat bool, cfg func(*goplusFake)) (*goplusFake, *lsp.Manager) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	f := &goplusFake{conn: serverConn, rd: bufio.NewReader(serverConn), noFormat: noFormat}
	if cfg != nil {
		cfg(f)
	}
	go f.serve()

	mgr := lsp.NewManager(nil, nil)
	c, err := lsp.New(context.Background(), clientConn, "/ws")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Inject(lang, c)
	t.Cleanup(func() { mgr.ShutdownAll() })
	return f, mgr
}

func (f *goplusFake) serve() {
	for {
		payload, err := readLSPFrame(f.rd)
		if err != nil {
			return
		}
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			f.mu.Lock()
			f.initCaps = string(msg.Params)
			f.mu.Unlock()
			res := map[string]any{"serverInfo": map[string]any{"name": "fake-gopls"}}
			if f.noFormat {
				res["capabilities"] = map[string]any{"hoverProvider": true}
			} else {
				f.mu.Lock()
				opts := f.didSaveOpts
				f.mu.Unlock()
				caps := map[string]any{}
				if opts != nil {
					caps["textDocumentSync"] = map[string]any{"save": opts}
				}
				if f.inlayCap {
					caps["inlayHintProvider"] = true
				}
				if f.outlineCap {
					caps["documentSymbolProvider"] = true
				}
				if f.foldCap {
					caps["foldingRangeProvider"] = true
				}
				if f.resolveCaps {
					caps["codeActionProvider"] = map[string]any{"resolveProvider": true}
				}
				if f.selCap {
					caps["selectionRangeProvider"] = true
				}
				for k, v := range f.extraCaps {
					caps[k] = v
				}
				if len(caps) > 0 {
					res["capabilities"] = caps
				}
			}
			f.reply(*msg.ID, res)

		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI        string `json:"uri"`
					LanguageID string `json:"languageId"`
				} `json:"textDocument"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.lastOpen = p.TextDocument.URI
			f.lastLang = p.TextDocument.LanguageID
			f.mu.Unlock()
			f.notify("textDocument/publishDiagnostics", map[string]any{
				"uri": p.TextDocument.URI,
				"diagnostics": []map[string]any{
					{"range": map[string]any{"start": map[string]any{"line": 0, "character": 0}}, "severity": 1, "message": "boom"},
				},
			})

		case "textDocument/inlayHint":
			var p struct {
				Range struct {
					Start, End struct{ Line int }
				} `json:"range"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.inlayAsks++
			f.inlayRanges = append(f.inlayRanges, fmt.Sprintf("%d-%d", p.Range.Start.Line, p.Range.End.Line))
			hints := f.inlayHints
			f.mu.Unlock()
			f.reply(*msg.ID, hints)

		case "textDocument/prepareCallHierarchy", "callHierarchy/incomingCalls", "callHierarchy/outgoingCalls", "textDocument/semanticTokens/full":
			f.serveF173(msg.ID, msg.Method, msg.Params)
		case "textDocument/foldingRange":
			f.mu.Lock()
			f.foldAsks++
			rs, fail := f.foldRanges, f.foldFail
			f.mu.Unlock()
			if fail {
				f.writeJSON(map[string]any{"jsonrpc": "2.0", "id": *msg.ID,
					"error": map[string]any{"code": -32603, "message": "folding exploded"}})
				break
			}
			f.reply(*msg.ID, rs)

		case "textDocument/completion":
			f.mu.Lock()
			custom := f.compItems
			f.mu.Unlock()
			if custom != nil {
				f.reply(*msg.ID, custom)
				break
			}
			f.reply(*msg.ID, []map[string]any{
				{"label": "Println", "detail": "func(a ...any) (n int, err error)"},
				{"label": "Printf"},
			})

		case "textDocument/hover":
			f.reply(*msg.ID, map[string]any{"contents": map[string]any{
				"kind": "markdown", "value": "```go\nfunc Helper()\n```\nhover doc line2",
			}})

		case "textDocument/prepareRename":
			f.mu.Lock()
			decline := f.declinePrepare
			f.mu.Unlock()
			if decline {
				f.reply(*msg.ID, nil)
				break
			}
			f.reply(*msg.ID, map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": 0, "character": 7},
			})

		case "textDocument/rename":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				NewName string `json:"newName"`
			}
			json.Unmarshal(msg.Params, &p)
			changes := map[string]any{
				p.TextDocument.URI: []map[string]any{
					{"range": map[string]any{
						"start": map[string]any{"line": 0, "character": 0},
						"end":   map[string]any{"line": 0, "character": 7},
					}, "newText": p.NewName},
				},
			}
			f.mu.Lock()
			if f.renameExtra != "" {
				changes[f.renameExtra] = []map[string]any{{"range": map[string]any{
					"start": map[string]any{"line": 0, "character": 8},
					"end":   map[string]any{"line": 0, "character": 12},
				}, "newText": p.NewName}}
			}
			f.mu.Unlock()
			f.reply(*msg.ID, map[string]any{"changes": changes})

		case "textDocument/definition", "textDocument/references":
			f.mu.Lock()
			locs := f.locs
			f.mu.Unlock()
			f.reply(*msg.ID, locs)

		case "textDocument/didChange":
			var p struct {
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.events = append(f.events, "didChange")
			if len(p.ContentChanges) > 0 {
				f.lastChange = p.ContentChanges[0].Text
			}
			f.mu.Unlock()

		case "textDocument/signatureHelp":
			// F-128: activeParameter = commas before the cursor on its line.
			var p struct {
				Position struct {
					Line, Character int
				} `json:"position"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			lines := strings.Split(f.lastChange, "\n")
			f.events = append(f.events, "signatureHelp")
			f.mu.Unlock()
			active := 0
			if p.Position.Line < len(lines) {
				r := []rune(lines[p.Position.Line])
				active = strings.Count(string(r[:min(p.Position.Character, len(r))]), ",")
			}
			f.reply(*msg.ID, map[string]any{
				"signatures": []map[string]any{{
					"label":         "func Add(a int, b int) int",
					"documentation": "Add sums two ints.\nmore",
					"parameters":    []map[string]any{{"label": "a int"}, {"label": []int{16, 21}}},
				}},
				"activeSignature": 0,
				"activeParameter": active,
			})

		case "textDocument/formatting":
			f.mu.Lock()
			f.events = append(f.events, "formatting")
			f.mu.Unlock()
			f.mu.Lock()
			edits, fail := f.formatEdits, f.formatFail
			f.mu.Unlock()
			if fail {
				f.writeJSON(map[string]any{"jsonrpc": "2.0", "id": *msg.ID,
					"error": map[string]any{"code": -32603, "message": "gofmt: syntax error"}})
				break
			}
			f.reply(*msg.ID, edits)

		case "textDocument/selectionRange": // F-167
			f.mu.Lock()
			chain, fail := f.selChain, f.selFail
			f.selAsked++
			f.mu.Unlock()
			if fail {
				f.writeJSON(map[string]any{"jsonrpc": "2.0", "id": *msg.ID,
					"error": map[string]any{"code": -32603, "message": "boom"}})
				break
			}
			var node any // innermost first; build the parent links
			for i := len(chain) - 1; i >= 0; i-- {
				n := map[string]any{"range": chain[i]}
				if node != nil {
					n["parent"] = node
				}
				node = n
			}
			f.reply(*msg.ID, []any{node})

		case "workspace/symbol":
			f.mu.Lock()
			syms := f.wsSyms
			f.mu.Unlock()
			f.reply(*msg.ID, syms)

		case "textDocument/documentSymbol":
			f.mu.Lock()
			f.outlineAsks++
			custom := f.outlineSyms
			f.mu.Unlock()
			if custom != nil {
				f.reply(*msg.ID, custom)
				break
			}
			sel := func(l int) map[string]any {
				return map[string]any{
					"start": map[string]any{"line": l, "character": 5},
					"end":   map[string]any{"line": l, "character": 9}}
			}
			f.reply(*msg.ID, []map[string]any{
				{"name": "main", "kind": 12, "range": sel(0), "selectionRange": sel(0)},
				{"name": "Config", "kind": 23, "range": sel(0), "selectionRange": sel(0),
					"children": []map[string]any{{"name": "Load", "kind": 6, "range": sel(1), "selectionRange": sel(1)}}},
			})

		case "textDocument/codeAction":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			json.Unmarshal(msg.Params, &p)
			var cp struct {
				Context struct {
					Only []string `json:"only"`
				} `json:"context"`
			}
			json.Unmarshal(msg.Params, &cp)
			only := strings.Join(cp.Context.Only, ",")
			f.mu.Lock()
			f.onlySeen = append(f.onlySeen, only)
			custom, has := f.actionsFor[only]
			if !has {
				custom, has = f.actionsFor[""]
			}
			f.mu.Unlock()
			if has {
				f.reply(*msg.ID, custom)
				break
			}
			f.reply(*msg.ID, []map[string]any{
				{"title": "quickfix boom", "edit": map[string]any{"changes": map[string]any{
					p.TextDocument.URI: []map[string]any{
						{"range": map[string]any{
							"start": map[string]any{"line": 1, "character": 0},
							"end":   map[string]any{"line": 1, "character": 0},
						}, "newText": "fixed\n"},
					},
				}}},
			})

		case "codeAction/resolve":
			f.mu.Lock()
			r := f.resolved
			f.events = append(f.events, "resolve")
			f.mu.Unlock()
			f.reply(*msg.ID, r)

		case "workspace/executeCommand":
			var p struct {
				Command string `json:"command"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.execCommands = append(f.execCommands, p.Command)
			ed := f.execEdit
			f.mu.Unlock()
			f.reply(*msg.ID, nil)
			if ed != nil { // async: the pipe is synchronous and the client answers the request
				go f.writeJSON(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "workspace/applyEdit", "params": map[string]any{"edit": ed}})
			}

		case "textDocument/didSave":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				Text *string `json:"text"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.events = append(f.events, "didSave")
			f.didSaveEvents = append(f.didSaveEvents, map[string]any{
				"uri":     p.TextDocument.URI,
				"hasText": p.Text != nil,
			})
			f.mu.Unlock()

		case "textDocument/didClose":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.events = append(f.events, "didClose")
			f.didCloseUris = append(f.didCloseUris, p.TextDocument.URI)
			f.mu.Unlock()

		default:
			if msg.ID != nil {
				f.reply(*msg.ID, nil)
			}
		}
	}
}

func (f *goplusFake) reply(id int64, result any) {
	f.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (f *goplusFake) notify(method string, params any) {
	f.writeJSON(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (f *goplusFake) writeJSON(v any) {
	data, _ := json.Marshal(v)
	f.wmu.Lock()
	defer f.wmu.Unlock()
	fmt.Fprintf(f.conn, "Content-Length: %d\r\n\r\n", len(data))
	f.conn.Write(data)
}

func readLSPFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			fmt.Sscanf(strings.TrimSpace(line[15:]), "%d", &length)
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("no content-length")
	}
	buf := make([]byte, length)
	readFull(r, buf)
	return buf, nil
}

func readFull(r *bufio.Reader, buf []byte) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return
		}
	}
}

func waitFor(t *testing.T, m *Model, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m.drainTerm()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s:\n%s", what, plainView(m))
}

func TestLSPWithFakeServer(t *testing.T) {
	srv, mgr := startFakeServer(t)

	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	// open main.go → didOpen → server echoes diagnostics for its URI
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")

	// completion popup in insert mode
	feed(m, "i")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "completions:") && strings.Contains(v, "Println")
	}, "completion popup")

	feed(m, "enter") // accept Println
	if !strings.Contains(m.active().Buffer().Text(), "Println") {
		t.Errorf("accepted label not inserted:\n%q", m.active().Buffer().Text())
	}
	_ = srv.lastOpen
}

func TestLSPSilentWithoutManager(t *testing.T) {
	m := newEditor(t) // no WithLSP
	feed(m, "enter", "down", "down", "enter")
	feed(m, "i")
	feed(m, "ctrl+space")
	m.drainTerm()
	if m.compOpen || strings.Contains(plainView(m), "completions:") {
		t.Error("completion fired without a manager")
	}
}

// request sends a server→client request to the editor's client.
func (f *goplusFake) request(id int64, method string, params any) {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	fmt.Fprintf(f.conn, "Content-Length: %d\r\n\r\n", len(data))
	f.conn.Write(data)
}

func openMainGoWithLSP(t *testing.T) (*Model, *goplusFake) {
	t.Helper()
	srv, mgr := startFakeServer(t)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter") // open main.go
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")
	return m, srv
}

func TestLSPHoverPopup(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "K")
	waitFor(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "hover:") && strings.Contains(v, "hover doc")
	}, "hover popup")
	feed(m, "j") // movement closes hover
	if m.hoverOpen {
		t.Error("hover should close on cursor movement")
	}
}

func TestLSPRenameFlow(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "g", "r")
	if !m.renameMode {
		t.Fatal("rename input did not open")
	}
	typeKeys(m, "zeta")
	feed(m, "enter")
	waitFor(t, m, func() bool {
		return strings.Contains(m.active().Buffer().Text(), "zeta main")
	}, "rename applied")
}

func TestLSPRenameDeclined(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.declinePrepare = true
	srv.mu.Unlock()
	before := m.active().Buffer().Text()
	feed(m, "g", "r")
	typeKeys(m, "zeta")
	feed(m, "enter")
	waitFor(t, m, func() bool {
		return strings.Contains(plainView(m), "not renameable")
	}, "declined rename note")
	if m.active().Buffer().Text() != before {
		t.Error("declined rename must not modify the buffer")
	}
}

func TestLSPCodeActions(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "g", "a")
	waitFor(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "code actions:") && strings.Contains(v, "quickfix boom")
	}, "action popup")
	feed(m, "enter")
	waitFor(t, m, func() bool {
		return strings.Contains(m.active().Buffer().Text(), "fixed")
	}, "action edit applied")
}

func TestLSPApplyEditPush(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	uri := "file://" + m.active().Path()
	srv.request(77, "workspace/applyEdit", map[string]any{
		"edit": map[string]any{"changes": map[string]any{
			uri: []map[string]any{
				{"range": map[string]any{
					"start": map[string]any{"line": 0, "character": 0},
					"end":   map[string]any{"line": 0, "character": 0},
				}, "newText": "// touched\n"},
			},
		}},
	})
	waitFor(t, m, func() bool {
		return strings.Contains(m.active().Buffer().Text(), "// touched")
	}, "server push applied")
}

func TestLSPDiagnosticsClear(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         "file://" + m.active().Path(),
		"diagnostics": []map[string]any{},
	})
	waitFor(t, m, func() bool { return !strings.Contains(plainView(m), "✗1") }, "chip cleared")
}

func TestLSPInertWithoutServer(t *testing.T) {
	m := newEditor(t) // no WithLSP: K/gr/ga must be no-ops
	feed(m, "enter", "down", "down", "enter")
	feed(m, "K", "g", "r", "g", "a")
	m.drainTerm()
	if m.hoverOpen || m.renameMode || m.actionOpen {
		t.Error("lsp flows opened without a manager")
	}
}

func TestWordAt(t *testing.T) {
	cases := []struct {
		line string
		col  int
		word string
	}{
		{"package main", 0, "package"},
		{"package main", 3, "package"},
		{"package main", 7, "package"}, // space after word still finds it
		{"package main", 11, "main"},
		{"ab cd", 2, "ab"},
		{"", 0, ""},
		{"  x", 2, "x"},
	}
	for _, c := range cases {
		w, _ := wordAt(c.line, c.col)
		if w != c.word {
			t.Errorf("wordAt(%q, %d) = %q, want %q", c.line, c.col, w, c.word)
		}
	}
}
