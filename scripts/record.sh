#!/bin/sh
# record.sh — README recordings of real DHI flows (F-100), made with VHS.
#
# Builds dhi, seeds two dummy projects (a fresh one with no .dhi/ for
# onboarding, and the full demo for the view tours), sandboxes HOME and
# DHI's config, then plays each tape in scripts/tapes/. Nothing personal
# reaches a frame: git identity and prompts are demo values, GitHub tokens
# are unset, and the CLIs step is never opened.
#
#   scripts/record.sh               # every flow
#   scripts/record.sh onboarding    # one flow by name
#
#   DHI_REC_OUT=/some/dir   where GIF/MP4 land (default docs/assets/recordings)
#   DHI_REC_MAX_MB=6        per-GIF size budget (default 6)
#
# Needs VHS (brew install vhs — brings ttyd; ffmpeg too), tmux, go and git.
# DHI uses the tools on your PATH.
set -eu

for bin in vhs tmux go; do
	if ! command -v "$bin" >/dev/null 2>&1; then
		echo "record.sh needs $bin (VHS: brew install vhs)" >&2
		exit 1
	fi
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${DHI_REC_OUT:-"$ROOT/docs/assets/recordings"}
MAX_MB=${DHI_REC_MAX_MB:-6}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/dhi-rec.XXXXXX")
SOCK=dhi-rec-$$
trap 'tmux -L "$SOCK" kill-server 2>/dev/null || true; [ -n "${DHI_REC_KEEP:-}" ] || rm -rf "$WORK"' EXIT
DATA=${XDG_DATA_HOME:-$HOME/.local/share}
mkdir -p "$OUT" "$WORK/bin" "$WORK/tapes"
OUT=$(cd "$OUT" && pwd) # vhs runs in $WORK: outputs need an absolute path

echo "building dhi and seeding the demo projects…"
(cd "$ROOT" && go build -o "$WORK/bin/dhi" ./cmd/dhi)
(cd "$ROOT" && XDG_DATA_HOME="$DATA" go run ./scripts/demo "$WORK/acme")
(cd "$ROOT" && XDG_DATA_HOME="$DATA" go run ./scripts/demo -fresh "$WORK/shop")

# One sandboxed home per project, so onboarding starts from nothing and
# the tours start finished.
for flavor in fresh demo; do
	h="$WORK/home-$flavor"
	mkdir -p "$h" "$WORK/cfg-$flavor/dhi"
	printf '[user]\n\tname = Dana Rivera\n\temail = dana@example.com\n' >"$h/.gitconfig"
	printf "PROMPT='%%F{cyan}%%1~%%f $ '\n" >"$h/.zshrc"
	printf "PS1='\\\\W $ '\n" >"$h/.bashrc"
	cat >"$WORK/env-$flavor.sh" <<EOF
export HOME='$h' XDG_CONFIG_HOME='$WORK/cfg-$flavor' XDG_DATA_HOME='$DATA'
export PATH='$WORK/bin':"\$PATH" TERM=xterm-256color COLORTERM=truecolor
unset GH_TOKEN GITHUB_TOKEN
PS1='\\W \$ '
EOF
done
# The tours: every setup step already settled, so the start page shows
# the actions, not a checklist.
cat >"$WORK/cfg-demo/dhi/setup.json" <<'EOF'
{"finished": true, "steps": {"identity": "2026-10-20T00:00:00Z", "you": "2026-10-20T00:00:00Z",
 "instructions": "2026-10-20T00:00:00Z", "conventions": "2026-10-20T00:00:00Z", "cli": "2026-10-20T00:00:00Z"}}
EOF
cat >"$WORK/acme/.dhi/setup.json" <<'EOF'
{"finished": true, "steps": {"workspace": "2026-10-20T00:00:00Z", "project": "2026-10-20T00:00:00Z",
 "team": "2026-10-20T00:00:00Z", "integrations": "2026-10-20T00:00:00Z"}}
EOF

# A fresh demo workspace gets one-time repairs on its first launch: take
# them before recording so no tour opens on the repair card.
cat >"$WORK/tmux.conf" <<'EOF'
set -g default-terminal "tmux-256color"
set -g status off
EOF
tmux -L "$SOCK" -f "$WORK/tmux.conf" new-session -d -s s -x 150 -y 40 \
	". '$WORK/env-demo.sh'; cd '$WORK/acme' && dhi"
sleep 4
tmux -L "$SOCK" send-keys -t s Enter
sleep 1
tmux -L "$SOCK" send-keys -t s C-q
sleep 1
tmux -L "$SOCK" kill-server 2>/dev/null || true

WANT=${1:-}
status=0
for tape in "$ROOT"/scripts/tapes/*.tape; do
	name=$(basename "$tape" .tape)
	[ -z "$WANT" ] || [ "$WANT" = "$name" ] || continue
	sed -e "s|{{OUT}}|$OUT|g" -e "s|{{WORK}}|$WORK|g" "$tape" >"$WORK/tapes/$name.tape"
	echo "recording ${name}…"
	(cd "$WORK" && vhs "tapes/$name.tape" >/dev/null)
	size=$(wc -c <"$OUT/$name.gif" | tr -d ' ')
	mb=$((size / 1048576))
	echo "  $OUT/$name.gif (${mb} MB) and $name.mp4"
	if [ "$mb" -ge "$MAX_MB" ]; then
		echo "  over the ${MAX_MB} MB budget: shorten the tape or lower its framerate" >&2
		status=1
	fi
done
exit $status
