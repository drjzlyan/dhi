---
name: Code review
description: A repeatable pass over any diff — correctness first, style last
---
Review in this order, and say which step you skipped if you skip one:

1. Correctness: does the change do what the title claims? Trace the
   main path end to end before commenting on anything else.
2. Edge cases: empty inputs, error returns, concurrency, resource
   cleanup. Name the concrete input that breaks it.
3. Tests: is the new behavior covered? A fix without a test is a
   regression waiting to happen — say where the test belongs.
4. Readability: naming, dead code, copy-paste. Keep it brief.

Output one finding per line: `file:line — risk — smallest fix`.
End with a one-line verdict: approve, request changes, or needs
discussion (and why).
