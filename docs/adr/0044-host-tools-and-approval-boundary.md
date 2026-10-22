# ADR-0044: Host tools, the user's configuration, and an approval boundary

Date: 2026-10-22 · Status: accepted · Supersedes: ADR-0005 (hermetic toolchain), ADR-0009 (hermetic
git binary), ADR-0036 (optional managed toolchains), the install parts of ADR-0011, ADR-0027 and
ADR-0029. Amends ADR-0012 §3–4 and ADR-0039. M36, F-109–F-114.

## Context

DHI downloaded and ran its own Go, Node, uv, ripgrep, git and gh, built gopls and dlv from source,
installed coding CLIs and language servers with its own npm, and ran its git with a managed global
config that ignored the user's. That cost a 157 MB first run, broke pushes to SSH remotes (go-git
ignores ssh config and credential helpers), and kept the user's own setup (nvm, Homebrew, includeIf,
credential helpers) out of the IDE. VS Code, JetBrains and Zed instead read the user's shell
environment, use the tools on their PATH and the user's git as configured, and offer to install
what is missing. AI harnesses such as Claude Code and Codex let agents work inside a boundary they
cross only with the user's approval.

## Decision

1. **The user's environment, read once.** At startup DHI reads the login shell's environment
   (`$SHELL -ilc`, with a timeout) and every child starts from it. Fallback: DHI's own environment,
   named in doctor.
2. **Host tools.** Every tool is found on that PATH. A missing tool raises an approval card with the
   exact install command for the user's package manager (Homebrew, apt, dnf, pacman, zypper; npm,
   go, rustup or uv where those are the norm). On yes it runs in a visible terminal tab. DHI never
   downloads tool binaries and never pipes a remote script.
3. **The user's configuration, read-only.** git runs as the user's git with their config, ssh-agent
   and credential helpers; gh with their login; CLIs with their own homes. DHI writes the user's
   git identity only when it is missing and the user answers the card (global, or only this
   workspace's repos).
4. **An approval boundary, never a wall for the IDE.** Boot never blocks on tools. Agents run inside
   the OS sandbox: workspace, their temp dir, their CLI's state dir and the standard package caches
   are writable; anything else outside raises a card, and a yes lets the agent cross. Credential
   folders are never handed to agents: DHI pushes for them after a push card, with the user's git.
   A missing sandbox helper pauses agents behind an install card; the editor, git, terminal and
   settings keep working.

## Consequences

- No first-run download; DHI starts in the editor at once.
- Behaviour follows the user's machine (versions differ between users); doctor reports what was
  found, and every capability names its missing tool with the command that installs it.
- The old `~/.local/share/dhi/toolchain` is left alone; a named repair offers to delete it.
- AGENTS.md rule 5 changes: the user's system, read-only; installs only through approved commands.
