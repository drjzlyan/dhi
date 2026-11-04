# F-137 — Regex in the buffer `:s` command

Status: done · gap G-E5

`:s` / `:%s` replaced plain substrings only, so `\(..\)` groups, word edges, `&` and
`\1` did nothing, and only the cursor line or the whole buffer could be targeted.

## Decision
The pattern is vim's default (magic) syntax mapped onto Go RE2. `/` search keeps its own
smartcase substring matcher and highlighting; it does not share this code and is unchanged.

## Behaviour
- Form: `:[range]s/pat/rep/[flags]`, any non-alphanumeric separator (`s#a#b#`); `\/` escapes it.
- Range: none (cursor line), `%`, `n`, `n,m`, with `.`, `$`, `'<`, `'>`. `'<`/`'>` are the lines
  of the last visual selection (kept after it ends; type `'<,'>` after `:` in visual mode).
  Out-of-range or backwards ranges are named errors.
- Pattern translation: `\<` `\>` -> `\b`; `\(` `\)` `\|` `\+` `\?` `\=` `\{n,m}` -> RE2 operators;
  unescaped `( ) | + ? { }` are literal (as in vim); `. * ^ $ [..] [[:class:]] \s \d \w \t \n` pass through.
- Not supported, refused by name: backreferences in the pattern, `\v \V \c \C \zs \ze \@ \% \_`.
- Replacement: `&` and `\0`..`\9` (a group the pattern lacks is an error), `\&` literal, `\n`/`\r`
  line break (splits the line), `\t` tab, `\\` backslash.
- Flags: `g` (all matches per line), `i` (ignore case), `I` (case sensitive, default). `c` is
  refused ("the c (confirm) flag is not supported"); other flags are named errors.
- Invalid regex: `invalid pattern: ...` on the command line, buffer untouched, no undo entry.
- One undo step covers the whole substitution.

## Acceptance
- Regex forms, groups, `&`, `\n`, flags, separators: `TestRegexSubstituteForms`,
  `TestTranslateVimPattern`.
- Ranges incl. `'<,'>`: `TestRegexSubstituteRanges`.
- Errors leave the buffer clean: `TestRegexSubstituteErrorsLeaveBufferAlone`.
- Single undo: `TestRegexSubstituteOneUndoStep`; legacy cursor-line/`%` forms: `TestExSubstitute`.
