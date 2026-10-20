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
#
# Review variants (F-093) — never overwrite the README assets by accident:
#   DHI_SHOT_THEME=dhi-dawn|high-contrast|dhi-dusk            theme to capture
#   DHI_SHOT_SIZE=120x36                                     override every size
#   DHI_SHOT_OUT=/some/dir                                   where PNGs land
#   DHI_SHOT_FIRSTRUN=1                                      keep the repair gate (first-run shots)
set -eu

FREEZE=github.com/charmbracelet/freeze@v0.2.2
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${DHI_SHOT_OUT:-"${TMPDIR:-/tmp}/dhi-shots"} # README uses recordings (F-100)
THEME=${DHI_SHOT_THEME:-}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/dhi-shots.XXXXXX")
SOCK=dhi-shots-$$
trap 'tmux -L "$SOCK" kill-server 2>/dev/null || true; rm -rf "$WORK"' EXIT

mkdir -p "$OUT" "$WORK/home" "$WORK/cfg/dhi"
# freeze ignores SGR 39/49 (default fg/bg): spell them as the theme's text
# and canvas colors.
FG="236;234;244" BG="13;14;22" CANVAS="#0D0E16"
case "$THEME" in
dhi-dawn | light-paper) FG="30;31;43" BG="247;244;236" CANVAS="#F7F4EC" ;;
high-contrast) FG="255;255;255" BG="0;0;0" CANVAS="#000000" ;;
esac
if [ -n "$THEME" ]; then
	printf 'theme = "%s"\n' "$THEME" >"$WORK/cfg/dhi/config.toml"
fi
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
	name=$1 size=${DHI_SHOT_SIZE:-$2}
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
	tmux -L "$SOCK" capture-pane -e -p -t s |
		FG="$FG" BG="$BG" python3 -c 'import os, sys; sys.stdout.write(sys.stdin.read().replace("\x1b[39m", "\x1b[38;2;" + os.environ["FG"] + "m").replace("\x1b[49m", "\x1b[48;2;" + os.environ["BG"] + "m"))' \
			>"$WORK/$name.ansi"
	"$WORK/freeze" --language ansi --window --border.radius 10 --background "$CANVAS" \
		--padding 24 --margin 0 -o "$OUT/$name.png" "$WORK/$name.ansi" </dev/null >/dev/null
	if command -v sips >/dev/null 2>&1; then
		sips -Z 2400 "$OUT/$name.png" >/dev/null
	fi
	echo "  $OUT/$name.png"
}

# A fresh demo workspace gets one-time repairs on its first launch (the
# repair gate): launch once,
# accept the repairs so every shot shows the app (setup no longer gates).
warmup() {
	tmux -L "$SOCK" -f "$WORK/tmux.conf" new-session -d -s s -x 120 -y 36 \
		"cd '$WORK/acme' && TERM=xterm-256color HOME='$WORK/home' XDG_CONFIG_HOME='$WORK/cfg' XDG_DATA_HOME='$DATA' '$WORK/dhi'"
	sleep 3
	tmux -L "$SOCK" send-keys -t s Enter
	sleep 1
	tmux -L "$SOCK" kill-server 2>/dev/null || true
}

WANT=${1:-}
[ -n "${DHI_SHOT_FIRSTRUN:-}" ] || warmup

echo "capturing…"
{ [ -z "$WANT" ] || [ "$WANT" = board ]; } && shot board 150x40 2
{ [ -z "$WANT" ] || [ "$WANT" = channels ]; } && shot channels 150x40 2 ']'
{ [ -z "$WANT" ] || [ "$WANT" = review ]; } && shot review 150x40 4 Enter Enter
{ [ -z "$WANT" ] || [ "$WANT" = editor ]; } && shot editor 150x40 1 Enter j j j Enter Escape j Enter C-w v
{ [ -z "$WANT" ] || [ "$WANT" = replace ]; } && shot replace 150x40 1 s C-r f u n c ' ' '(' '\' w + ')' Enter r f u n c ' ' '$' '{' 1 '}' V 2
{ [ -z "$WANT" ] || [ "$WANT" = terminal ]; } && shot terminal 150x40 1 Enter j j j Enter C-t g i t ' ' l o g ' ' - - o n e l i n e ' ' - 5 Enter
{ [ -z "$WANT" ] || [ "$WANT" = ideator ]; } && shot ideator 150x40 3
{ [ -z "$WANT" ] || [ "$WANT" = settings ]; } && shot settings 150x40 5
{ [ -z "$WANT" ] || [ "$WANT" = dialog ]; } && shot dialog 150x40 2 n c s v - e x p o r t Tab E x p o r t ' ' r e p o r t s ' ' a s ' ' C S V
{ [ -z "$WANT" ] || [ "$WANT" = inbox ]; } && shot inbox 150x40 2 '['
{ [ -z "$WANT" ] || [ "$WANT" = help ]; } && shot help 150x40 2 '?'
{ [ -z "$WANT" ] || [ "$WANT" = narrow ]; } && shot narrow 72x30 2
# The ship toast: move the in-review card to done (review shots only).
[ "$WANT" = ship ] && shot ship 150x40 2 l l s
# First-run screens: only with DHI_SHOT_FIRSTRUN=1 (no warmup).
[ "$WANT" = gate ] && shot gate 120x36
[ "$WANT" = start ] && shot start 120x36
[ "$WANT" = checklist ] && shot checklist 120x36 c
echo "done"
