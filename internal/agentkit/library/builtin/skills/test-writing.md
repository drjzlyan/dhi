---
name: Test writing
description: How to add tests that fail honestly and pass meaningfully
---
Write the failing test first; run it and watch it fail for the reason
you expect. Then make it pass with the smallest change.

- Test behavior, not implementation: assert outcomes, not call counts.
- One concept per test; the test name states the behavior.
- Table-driven when cases share a shape; keep the table at the top.
- Never weaken an existing assertion to make a test pass — say why it
  changed or leave it red.
