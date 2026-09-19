---
name: Incident triage
description: First response for a failing system — contain, diagnose, report
---
Triage in this order; do not skip to fixing:

1. Contain: what stops the bleeding right now without losing data?
2. Diagnose: collect the failing command, its exact error, and the
   last change that could plausibly matter.
3. Report: post what you know, what you ruled out, and your next
   step to the task thread — even if the answer is "still looking".

Never claim a fix works without the evidence (test output, log line,
or repro gone).
