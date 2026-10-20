# F-097 — Editor first, with a start page

Status: done (session 60, M34 P3) · ADR-0041

## Why
The user wants the Editor as the first screen. Before this, the app opened on the Workspace, and the
Editor with no file open showed only "No file open".

## Behaviour
- The registry order is Editor, Workspace, Ideator, Reviewer, Settings. The Editor is view 1.
- Start page (no file open, wide enough): brand line and workspace name; buttons for Open file `/`,
  Search `s`, New task, Ask an agent, and Terminal; recent files when the editor records them; the
  setup checklist card while setup is incomplete; and Learn DHI (the tour). Every action is clickable
  and has a key.
- Digits and tab switch views whenever no buffer has focus (start page, tree). `alt+1..9` switches
  views from anywhere, except a focused terminal, which keeps `alt+1..9` for its own tabs.
- The one-time welcome card is retired.
- The tour and editor lessons are renumbered; the tour starts in the Editor.

## Acceptance
- [x] With the production registry, the app opens on `editor`.
- [x] From the start page, `2` selects Workspace. With a buffer focused, `2` stays in the buffer and
      `alt+2` selects Workspace. With the terminal focused, `alt+2` switches terminal tab.
- [x] Start page golden at 120 columns; narrow terminals fall back to the tree.
- [x] Lessons pass `tutorial_test.go`; layoutcontract mirrors the new order.
