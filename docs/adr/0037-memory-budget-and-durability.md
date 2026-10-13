# ADR-0037: Agent memory is budgeted, bounded and durable

Date: 2026-10-13 · Status: accepted · Extends: ADR-0007 (memory and KB as
plain files), ADR-0031 (episodes), ADR-0033 (lessons). F-078.

## Context

An agent's memory grew without bound and was fragile:
- journals, channel files, lesson files and episodes never shrank, and
  journals and channels were parsed whole for every prompt;
- one corrupt line silently wiped a channel's history from prompts, and
  one bad journal line disabled the journal for good;
- several prompt blocks had no budget (pinned lessons, persona, task and
  team blocks), and a notes file was cut from the end so new notes were
  invisible; dropped history was simply dropped;
- overflow was only detected after the CLI failed, and resumed sessions
  grew until they did;
- writes used fixed temp names with no fsync or cross-process locks;
- none of the limits could be configured.

The user asked for memory that does not overload an employee and is
efficient, resilient and hard to break.

## Decision

1. **Durability.** Whole-file stores (notes, lessons, episodes, the KB
   index) write atomically and durably (`fsx.WriteAtomic`: unique temp,
   fsync, rename, directory fsync). Read-modify-write stores lock across
   processes (`fsx.Locked`). Appends lock the log file.
2. **Corruption is contained.** Readers skip undecodable lines; the bus
   quarantines them at open (`jsonl.Repair` → `*.corrupt`, in place under
   the append lock, with a recovery copy until done). `dhi doctor` reports
   quarantined lines, interrupted repairs and very large logs.
3. **Bounded growth.** Journals keep `memory.journal_max` entries; older
   ones go to a monthly archive and a line each to a capped `digest.md`.
   Lessons follow an idempotent lifecycle: stale weak lessons retire,
   established ones later, the weakest past `lessons_max` retire,
   long-retired ones move to an archive, pinned lessons are capped.
   Episodes of done or removed cards are collected after 30 days.
   Prompt history and journals are read by tail (`jsonl.Tail`,
   `Bus.Recent`, which still recovers a thread's root). Channel files
   themselves are not rotated: the chat UI, unread scans and reflection
   read them whole, parsed once per file change (a cache in the bus).
4. **One prompt budget.** The whole prompt (system + prompt) is fitted to
   `memory.prompt_tokens` (estimated at 4 runes per token). Every block is
   a section with a priority and a floor; the lowest priority shrinks
   first (knowledge, then memory, lessons, history, rules, task). The
   human's message is never trimmed. Truncation keeps head and tail with a
   marker (history keeps its newest end). Compaction levels scale the
   budget, and a third level means a first-compaction overflow can retry.
5. **Condensed, not forgotten.** History beyond the window is replaced by
   an extractive brief of the dropped messages (who said what, one line
   each). No model call: it costs nothing and cannot invent.
6. **Sessions stay healthy.** A resumed CLI session whose last call
   carried more than `memory.session_tokens` (or a verify round on one)
   starts fresh with the episode instead of overflowing mid-task.
7. **Configurable.** `[memory]` in settings (prompt_tokens,
   session_tokens, journal_max, lessons_max, pinned_max, decay_days),
   editable in Settings CONFIG and read every turn. BRAIN shows an agent's
   digest, journal and notes (`m`). `memory_read_notes` pages.

## Consequences

- Prompts have a known ceiling; long-lived agents carry a brief of their
  past instead of an ever-longer transcript.
- Rotation and repair rewrite logs in place under a lock; a crash during
  a repair leaves a `.repair` copy that doctor names.
- The brief is extractive, so it can be verbose or miss nuance; a model
  summary can be added behind `agents.reflect_model` later.
- Not done: channel-file segmentation (channels keep stream lines for
  traces, so they grow fastest), a KB index cache, spend-ledger rotation.
