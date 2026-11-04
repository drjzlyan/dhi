package doctor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/otelexport"
)

func TestTelemetryRow(t *testing.T) {
	otelexport.SetCurrent(nil)
	c := Telemetry(nil)[0]
	if c.Name != "telemetry/otel" || c.Status != OK || !strings.HasPrefix(c.Detail, "off") {
		t.Fatalf("%+v", c)
	}
	c = Telemetry([]string{"OTEL_EXPORTER_OTLP_ENDPOINT=https://u:secret@host:4318?k=v"})[0]
	if c.Status != OK || !strings.Contains(c.Detail, "https://host:4318") || strings.Contains(c.Detail, "secret") {
		t.Fatalf("%+v", c)
	}
}
