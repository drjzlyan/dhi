# F-078 — Top-class agent memory

Status: done (M30 P7) · ADR-0037

## Problem
Agent memory grew without bound, was parsed whole for every prompt,
silently lost history on one corrupt line, had unbudgeted prompt blocks,
dropped old history instead of condensing it, let resumed sessions grow
until they overflowed, wrote files without fsync or cross-process locks,
and exposed no limits. See ADR-0037.

## Behaviour
- **Durable stores:** atomic durable writes for notes, lessons, episodes
  and the KB index; cross-process locks; locked appends.
- **Corruption contained:** tolerant reads everywhere; the bus quarantines
  corrupt lines to `*.corrupt` at open; doctor reports quarantine, broken
  repairs and huge logs.
- **Bounded:** journal rotation into monthly archives plus a capped
  digest; lesson lifecycle (decay, active cap, pinned cap, archive);
  episode collection; prompt history and journals read by tail (channel
  files are cached per change for the UI, not rotated — see ADR-0037).
- **One budget per prompt:** prioritised sections with floors; the human's
  message never trimmed; head+tail truncation; three compaction levels.
- **Brief of dropped history** in place of "N messages omitted".
- **Session health:** fresh session past `memory.session_tokens`.
- **Configurable:** `[memory]` settings and Settings CONFIG rows; BRAIN
  `m` shows an agent's digest, journal and notes; `memory_read_notes`
  pages through large notes.

## Acceptance
- [x] fsx atomic write and lock (concurrent read-modify-write).
- [x] jsonl tolerant read, Tail across chunks, Repair quarantine, Compact.
- [x] Bus survives a corrupt line; Recent window.
- [x] Journal rotation and digest; lesson policy; episode collection.
- [x] Budget fitting (priorities, floors, tail mode); prompt within budget
      with a brief of dropped history; trigger untrimmed.
- [x] Settings validation; CONFIG rows; BRAIN memory view; notes paging;
      doctor memory checks.
