package otel_test

import (
	wooltel "github.com/codefly-dev/core/wool/otel"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"testing"
)

func TestObservabilityConfiguration(t *testing.T) {
	valid := wooltel.Configuration{State: "enabled", Endpoint: "http://collector.example:4317", Protocol: "grpc", CollectorTier: "node-agent"}
	require.NoError(t, valid.Validate())
	for _, endpoint := range []string{"", "collector.example:4317", "ftp://collector.example", "https://user:secret@collector.example", "http://collector.example/v1/traces", "http://collector.example?secret=value", "http://collector.example:bad"} {
		c := valid
		c.Endpoint = endpoint
		require.Error(t, c.Validate(), endpoint)
	}
	for _, state := range []string{"", "unknown"} {
		c := valid
		c.State = state
		require.Error(t, c.Validate())
	}
	c := valid
	c.Protocol = "http/protobuf"
	require.Error(t, c.Validate())
	c = valid
	c.CollectorTier = ""
	require.Error(t, c.Validate())
}

func TestDisabledObservabilityDoesNotStartExporter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://stale.example:4317")
	before := otel.GetTracerProvider()
	c, err := wooltel.ReadConfiguration(func(key string) string {
		return map[string]string{"OBSERVABILITY_STATE": "disabled", "OBSERVABILITY_DISABLED_REASON": "no operations backend", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://stale.example:4317"}[key]
	})
	require.NoError(t, err)
	provider, err := wooltel.EnableConfigured(c)
	require.NoError(t, err)
	require.Nil(t, provider)
	require.Same(t, before, otel.GetTracerProvider())
	c.DisabledReason = ""
	_, err = wooltel.EnableConfigured(c)
	require.Error(t, err)
}
