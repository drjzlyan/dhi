# F-164 — Optional OpenTelemetry trace export

Status: done · gap G-C8

Teams that already run an OTel collector want agent runs in it. DHI must not grow a config format
for that (ADR-0043) nor a heavy SDK dependency.

## Decision
- Off by default; on only when the standard `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` or
  `OTEL_EXPORTER_OTLP_ENDPOINT` (+ `/v1/traces`) is in the user's login-shell environment.
  `OTEL_EXPORTER_OTLP_HEADERS` (or `_TRACES_HEADERS`) and `OTEL_SERVICE_NAME` (default `dhi`) are honoured.
- OTLP/HTTP JSON hand-rolled with `net/http` in `internal/otelexport`. The official Go SDK is not
  in `go.mod`; adding it would pull a large module tree for one POST, so no new dependency.
- Run granularity only. The run record holds no per-tool timing, so tool calls are a count
  attribute (`dhi.tool_calls`), not child spans.

## Behaviour
- One span `agent.run <agent>` per finished CLI run (`runtime.Config.OnRun`): agent, runtime, model,
  attempt, status, exit, stop reason, tokens in/out (when known), cost (when reported), duration.
- Metadata only: never prompt, reply, summary, error text, command or path.
- `Submit` never blocks: bounded queue (256), drop-on-full with a counter; one sender goroutine,
  5 s request timeout.
- `dhi doctor` row `telemetry/otel`: OK "off" or OK with the endpoint (credentials and query
  stripped) and sent/dropped counts; Warn once with the last failure or when spans were dropped.

## Acceptance
- Env parsing, endpoint suffix, header decoding, URL redaction (`TestFromEnv`).
- Receiver gets valid OTLP JSON, headers, no prompt/reply/error text (`TestExportsRunAsSpanWithoutText`).
- Collector failure is counted and reported once, not per run (`TestFailureSurfacedOnceAndNeverBlocks`).
- Full queue drops without blocking (`TestDropsWhenQueueFull`); nil exporter is safe (`TestNilExporterSafe`).
- A finished run reaches the hook (`TestOnRunHookSeesFinishedRun`); doctor row (`TestTelemetryRow`).
