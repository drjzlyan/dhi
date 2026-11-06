package main

import (
	"github.com/drjzlyan/dhi/internal/hostenv"
	"github.com/drjzlyan/dhi/internal/otelexport"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// otelOnRun starts the optional OTLP trace exporter (F-164) when the
// standard OTEL_EXPORTER_OTLP_* variables are set, and returns the
// runtime hook; nil when export is off.
func otelOnRun() func(tasks.Run, int) {
	e := otelexport.New(otelexport.FromEnv(hostenv.Env()))
	if e == nil {
		return nil
	}
	otelexport.SetCurrent(e)
	return e.Submit
}
