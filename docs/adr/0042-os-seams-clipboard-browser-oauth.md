# ADR-0042: Clipboard and browser seams, and an OAuth client for MCP

Date: 2026-10-22 · Status: accepted · Narrow exception to ADR-0005 · Supersedes in part: ADR-0028
(static bearer only). M35, F-101, F-103, F-106.

## Context

DHI had no clipboard: OSC 52 was never sent and paste messages were dropped. macOS Terminal.app does
not implement OSC 52, so OSC 52 alone leaves its users with no copy. Official remote MCP servers
(Atlassian, Slack, Linear, Notion) authenticate with OAuth 2.1; `internal/mcp` sent only a static
bearer token. A browser login needs the OS to open a URL.

## Decision

1. **Clipboard.** `internal/clipboard.Write` always emits OSC 52 (`tea.SetClipboard`) and also calls
   `toolchain.Clipboard`, a named seam over the OS's own tool: `/usr/bin/pbcopy` on macOS,
   `wl-copy` or `xclip` on Linux when on PATH, nothing otherwise. Paste uses the terminal's
   bracketed paste; DHI never reads the clipboard on its own.
2. **Browser.** `toolchain.OpenURL` (`/usr/bin/open`, `xdg-open`). The URL is always shown and
   copied too, so a missing opener never blocks a login.
3. **OAuth client.** `internal/mcp` implements the MCP authorization spec with the standard library:
   protected-resource and authorization-server discovery, client ID from the server config
   (`oauth.clientId`) or dynamic registration, PKCE S256, loopback redirect (fixed port when the
   config names one), paste-back for remote sessions, refresh and revocation. DHI registers no OAuth
   apps of its own. Tokens are secrets in the credential store.

## Consequences

- Both seams are OS-provided tools at fixed paths or well-known names, behind `internal/toolchain`,
  and are optional: without them OSC 52 and the printed URL still work. No other shell-outs.
- A server whose official config offers neither a client ID nor dynamic registration (Zoom today)
  needs a token; DHI asks for it through the secret card instead of shipping an app.
