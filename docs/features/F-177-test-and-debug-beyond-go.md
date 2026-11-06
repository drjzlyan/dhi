# F-177 — Test runner and debugger beyond Go

Status: done · gap G-X1

`:test` ran only `go test -json`, and `:debug` only delve; both refused any other file. The wire
layer (DAP client, `Session`) was already language-neutral, so only the runner and the launch
layer were Go-specific.

## Decision
A per-language table in code, not a config file (ADR-0043): `testrun.Languages` and
`dap.Adapters`. Tools come from the user's PATH (ADR-0044); a missing one is named in the result
(`Report.MissingTool`, `dap.MissingToolError`) and the editor raises the existing install card
(ADR-0011, no guessing a substitute). Every runner normalises into `testrun.Report`/`Failure`, so
the overlay, jump, `f` (fix with an agent), `r` rerun and the F-148 restricted-mode gate are
unchanged and language-blind.

## Behaviour
- Python tools are looked up in this order: the member's `.venv/bin/`, then `venv/bin/` (`pytest`;
  `python` for `-m debugpy.adapter`), then the user's PATH (`pytest`; `python3`). The missing-tool
  message names that order. Windows layouts (`Scripts\\`) are not supported: the venv step is
  skipped there and the message says so. `node_modules/.bin` applies to vitest/jest only.
- Python tests: `pytest --junitxml=<temp> -o junit_family=xunit1 --tb=short`; the temp report is
  parsed (`ParseJUnit`) and removed. The jump lands on the traceback frame in the test's own
  file, else the first frame. `:test <pattern>` is `-k`.
- JavaScript/TypeScript tests: `vitest run --reporter=json` or `jest --json`, found in the
  project's `node_modules/.bin` first, then on PATH (`:test <pattern>` is `-t`). One parser serves
  both (`ParseJS`); a suite that fails to load is one failure for the file.
- Python debugging: `python3 -m debugpy.adapter` over stdio (`StartDebugpy`), launch body from the
  row (`pythonLaunch`): a test file (`test_*.py`, `*_test.py`) runs `pytest <file>` (`:debug
  <pattern>` adds `-k`), any other `.py` runs as a script. Breakpoints sent are those of the
  debugged language only.
- Missing tools: pytest, vitest (card for either JS runner), python3, and debugpy (found when
  `python3 -c "import debugpy.adapter"` succeeds; no install recipe, the card shows the vendor
  doc because pip into the user's interpreter is theirs to choose). Listed in `dhi doctor`.
- Not covered: a JS/TS debug adapter (vscode-js-debug is not a PATH tool and DHI downloads none).
  `:debug`/`:break` on those files say so.

## Acceptance
- JUnit parse, counts, jump frame, collection error: `TestParseJUnitNormalisesFailures`,
  `TestParseJUnitBareTestsuiteRoot`; fake pytest end to end: `TestRunPytestWithFakeRunner`,
  `TestRunPytestMissingNamesTheTool`, `TestRunPytestNoReportSurfacesStderr`.
- jest/vitest JSON: `TestParseJSNormalisesJestAndVitest`, `TestRunJSPrefersProjectBinThenNamesMissing`.
- Table: `TestForPathTable`, `TestAdapterTable`; launch bodies: `TestPythonLaunchConfigs`,
  `TestStartSendsRawLaunchBody`; adapter process: `TestStartDebugpyTalksOverStdio`,
  `TestStartDebugpyNamesWhatIsMissing`.
- Editor: `TestTestCommandRunsPytestAndJumps`, `TestTestCommandPytestMissingRaisesInstallCard`,
  `TestTestCommandUnsupportedFileIsNamed`, `TestRestrictedBlocksPythonTestsAndDebug`,
  `TestDebugPythonFileLaunchesDebugpyBody`, `TestDebugMissingAdapterToolRaisesInstallCard`,
  `TestDebugUnsupportedLanguageIsNamed`.
- Live, env-gated and skipped without the tools: `TestLivePytest`, `TestLiveDebugpy` (`DHI_LIVE_PY=1`).
