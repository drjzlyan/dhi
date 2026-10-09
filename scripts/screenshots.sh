#!/bin/sh
# screenshots.sh — regenerate the README screenshots in docs/assets/.
#
# Builds dhi, seeds a demo workspace (scripts/demo), drives the real TUI in
# a private tmux server at fixed sizes, captures each screen with colors,
# and renders PNGs with charmbracelet/freeze (run through `go run`, pinned;
# a dev tool, not a dependency of dhi).
#
# Needs: go, tmux, python3, and DHI's toolchain installed (run `dhi` once).
# Optional: macOS `sips` to downscale the PNGs for the README.
#
#   scripts/screenshots.sh            # all shots
#   scripts/screenshots.sh board      # one shot by name
set -eu

FREEZE=github.com/charmbracelet/freeze@v0.2.2
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT="$ROOT/docs/assets"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/dhi-shots.XXXXXX")
SOCK=dhi-shots-$$
trap 'tmux -L "$SOCK" kill-server 2>/dev/null || true; rm -rf "$WORK"' EXIT

mkdir -p "$OUT" "$WORK/home" "$WORK/cfg"
echo "building dhi and freeze…"
(cd "$ROOT" && go build -o "$WORK/dhi" ./cmd/dhi)
GOBIN="$WORK" go install "$FREEZE"
DATA=${XDG_DATA_HOME:-$HOME/.local/share}
(cd "$ROOT" && XDG_DATA_HOME="$DATA" go run ./scripts/demo "$WORK/acme")

cat >"$WORK/tmux.conf" <<'EOF'
set -g default-terminal "tmux-256color"
set -as terminal-features ",*:RGB"
set -g status off
EOF

# The drawer's shell starts in the sandbox home: give it a neutral prompt
# so no real user or host name lands in a screenshot.
printf "PROMPT='%%F{cyan}%%1~%%f $ '\n" >"$WORK/home/.zshrc"
printf "PS1='\\W $ '\n" >"$WORK/home/.bashrc"

# shot NAME COLSxROWS KEY... — launch dhi, send keys, capture, render.
shot() {
	name=$1 size=$2
	shift 2
	cols=${size%x*} rows=${size#*x}
	tmux -L "$SOCK" kill-server 2>/dev/null || true
	# oxtabs: the renderer must not use hard tabs (a capture would keep them).
	tmux -L "$SOCK" -f "$WORK/tmux.conf" new-session -d -s s -x "$cols" -y "$rows" \
		"stty oxtabs 2>/dev/null || stty tab3; cd '$WORK/acme' && TERM=xterm-256color COLORTERM=truecolor \
		HOME='$WORK/home' XDG_CONFIG_HOME='$WORK/cfg' XDG_DATA_HOME='$DATA' '$WORK/dhi'"
	sleep 3
	for k in "$@"; do
		tmux -L "$SOCK" send-keys -t s "$k"
		sleep 0.5
	done
	sleep 1
	# freeze ignores SGR 39/49 (default fg/bg): spell them as DHI's text
	# and canvas colors.
	tmux -L "$SOCK" capture-pane -e -p -t s |
		python3 -c 'import sys; sys.stdout.write(sys.stdin.read().replace("\x1b[39m", "\x1b[38;2;230;237;243m").replace("\x1b[49m", "\x1b[48;2;11;14;20m"))' \
			>"$WORK/$name.ansi"
	"$WORK/freeze" --language ansi --window --border.radius 10 --background "#0B0E14" \
		--padding 24 --margin 0 -o "$OUT/$name.png" "$WORK/$name.ansi" </dev/null >/dev/null
	if command -v sips >/dev/null 2>&1; then
		sips -Z 2400 "$OUT/$name.png" >/dev/null
	fi
	echo "  $OUT/$name.png"
}

WANT=${1:-}

echo "capturing…"
{ [ -z "$WANT" ] || [ "$WANT" = board ]; } && shot board 150x40
{ [ -z "$WANT" ] || [ "$WANT" = channels ]; } && shot channels 150x40 ']'
{ [ -z "$WANT" ] || [ "$WANT" = review ]; } && shot review 150x40 4 Enter Enter
{ [ -z "$WANT" ] || [ "$WANT" = editor ]; } && shot editor 150x40 2 Enter j j j Enter Escape j Enter C-w v
{ [ -z "$WANT" ] || [ "$WANT" = replace ]; } && shot replace 150x40 2 s C-r f u n c ' ' '(' '\' w + ')' Enter r f u n c ' ' '$' '{' 1 '}' V 2
{ [ -z "$WANT" ] || [ "$WANT" = terminal ]; } && shot terminal 150x40 2 Enter j j j Enter C-t g i t ' ' l o g ' ' - - o n e l i n e ' ' - 5 Enter
{ [ -z "$WANT" ] || [ "$WANT" = ideator ]; } && shot ideator 150x40 3
{ [ -z "$WANT" ] || [ "$WANT" = settings ]; } && shot settings 150x40 5
{ [ -z "$WANT" ] || [ "$WANT" = dialog ]; } && shot dialog 150x40 n c s v - e x p o r t Tab E x p o r t ' ' r e p o r t s ' ' a s ' ' C S V
{ [ -z "$WANT" ] || [ "$WANT" = inbox ]; } && shot inbox 150x40 '['
{ [ -z "$WANT" ] || [ "$WANT" = help ]; } && shot help 150x40 '?'
{ [ -z "$WANT" ] || [ "$WANT" = narrow ]; } && shot narrow 72x30
echo "done"
