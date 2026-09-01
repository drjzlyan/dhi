# ADR-0010: Ideation sessions — reserved `.dhi` vpath alias + sessions schema

Date: 2026-09-01 · Status: accepted

## Context

F-004 (Ideator) needs agents to *produce* artifacts — markdown design
docs, plans, decision records — during ideation sessions. Artifacts are
workspace-level documents, not member-repo files, so they belong under
the reserved `.dhi/` tree (`.dhi/sessions/<session>/`). But the VPath
registry only resolves `<member>/<rel-path>`, and member names can never
start with `.` — agents had no way to address the dotdir through their
jailed tools.

## Decision

1. **Reserved pseudo-member**: the vpath member `.dhi` (workspace.
   `ReservedMember`) resolves to `<root>/.dhi/<rel-path>`. The dot-first
   naming rule guarantees it can never collide with a real member (a
   member named `dhi` remains distinct). Reverse mapping (`VPathFor`)
   maps the `.dhi` tree back to the pseudo-member.
2. **Jail, not bypass** (ADR-0006 unchanged): the runtime adds
   `<root>/.dhi` as a jail root. Policies stay deny-by-default and
   root-relative — an ideating agent opts in with a manifest rule like
   `{op:"write", path:"sessions/**", effect:"allow"}`. Because rules are
   root-relative, such a rule also matches `sessions/` dirs inside
   member repos; accepted, since both stay inside the jail and inside
   the user's own repos.
3. **Sessions schema**: one TOML card per session under
   `.dhi/sessions/<slug>.toml` (tasks/review blueprint: strict decode,
   atomic persist-before-commit, warnings for malformed cards); artifact
   files under `.dhi/sessions/<slug>/`; one implicit bus channel per
   session (`#ideation-<slug>`) so agent context windows work with the
   existing top-level history mechanism. Artifact status
   (draft → reviewed → approved/rejected) is recorded per file and keyed
   to a content hash: a fresh agent revision flips the artifact back to
   draft. Approval is human-only.

## Consequences

- Agents address artifacts as `.dhi/sessions/<session>/<file>` with the
  same `write`/`read` tools and the same approval gates as member files.
- The Ideator surface stays read-only for the human: it navigates,
  previews (glamour), and approves; it never edits.
- Rejection notes route back to the authoring agent over the session
  channel; authorship is claimed from agent chatter referencing artifact
  vpaths.
- Export (repo paths, MCP issue trackers) remains post-M6; the tools
  registry seam is unchanged.
