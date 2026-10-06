package otel

import (
	"fmt"
	"net/url"
	"strings"
)

// Configuration is the runtime observability handoff. A disabled collector is an
// explicit decision; missing configuration must never silently disable telemetry.
// Stdout is an explicit local tracing mode and starts no network exporter.
type Configuration struct {
	State          string
	Endpoint       string
	Protocol       string
	CollectorTier  string
	DisabledReason string
}

// ReadConfiguration reads the observability group through its owning framework.
// It does not mutate process environment or reimplement OTLP exporter options.
func ReadConfiguration(value func(string) string) (Configuration, error) {
	c := Configuration{
		State:          strings.TrimSpace(value("OBSERVABILITY_STATE")),
		Endpoint:       strings.TrimSpace(value("OTEL_EXPORTER_OTLP_ENDPOINT")),
		Protocol:       strings.TrimSpace(value("OTEL_EXPORTER_OTLP_PROTOCOL")),
		CollectorTier:  strings.TrimSpace(value("OBSERVABILITY_COLLECTOR_TIER")),
		DisabledReason: strings.TrimSpace(value("OBSERVABILITY_DISABLED_REASON")),
	}
	return c, c.Validate()
}

// Validate refuses ambiguous state and malformed URLs before the upstream
// exporter can fall back to its default endpoint. It never includes a URL in an
// error: userinfo and query parameters could carry credentials.
func (c Configuration) Validate() error {
	switch c.State {
	case "disabled":
		if c.DisabledReason == "" {
			return fmt.Errorf("disabled observability requires OBSERVABILITY_DISABLED_REASON")
		}
		return nil // Deliberate absence wins over stale lower-priority endpoint values.
	case "stdout":
		return nil
	case "enabled":
		if c.Protocol != "grpc" {
			return fmt.Errorf("enabled observability requires OTEL_EXPORTER_OTLP_PROTOCOL=grpc")
		}
		if c.CollectorTier == "" {
			return fmt.Errorf("enabled observability requires OBSERVABILITY_COLLECTOR_TIER")
		}
		endpoint, err := url.Parse(c.Endpoint)
		if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
			return fmt.Errorf("enabled observability requires an HTTP(S) OTLP/gRPC endpoint URL without credentials, query or path")
		}
		return nil
	default:
		return fmt.Errorf("OBSERVABILITY_STATE must be enabled, disabled or stdout")
	}
}

// EnableConfigured starts only the explicitly selected trace exporter. In the
// disabled state it returns nil without touching the global tracer or Wool.
func EnableConfigured(c Configuration) (*Provider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	switch c.State {
	case "disabled":
		return nil, nil
	case "stdout":
		return Enable(WithStdout())
	default:
		return Enable(WithEndpointURL(c.Endpoint))
	}
}
