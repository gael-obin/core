//go:build otel_integration

package otel_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codefly-dev/core/wool"
	wooltel "github.com/codefly-dev/core/wool/otel"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel"
)

// This test starts the official collector distribution, receives actual OTLP,
// and reads its file exporter. Select explicitly; missing tooling is a failure,
// never a skipped acceptance check. No mock exporter or receiver is involved.
func TestCollectorEndpointTransportResourcesAndCorrelation(t *testing.T) {
	binary := os.Getenv("OTELCOL_CONTRIB_BINARY")
	require.NotEmpty(t, binary, "set OTELCOL_CONTRIB_BINARY to the verified collector binary")
	for _, mode := range []string{"explicit-http", "explicit-https", "environment-precedence"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			ingest := freeAddress(t)
			health := freeAddress(t)
			output := filepath.Join(directory, "traces.json")
			tlsConfig := ""
			scheme := "http"
			for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_INSECURE", "OTEL_EXPORTER_OTLP_TRACES_INSECURE", "OTEL_EXPORTER_OTLP_CERTIFICATE", "OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE"} {
				t.Setenv(key, "")
			}
			t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "2000")
			t.Setenv("OTEL_SERVICE_NAME", "configured-service")
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=integration,service.version=test-build")
			if mode == "explicit-https" {
				cert, key := collectorCertificate(t, directory)
				tlsConfig = fmt.Sprintf("\n        tls:\n          cert_file: %q\n          key_file: %q", cert, key)
				t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", cert)
				scheme = "https"
			}
			configuration := fmt.Sprintf(`receivers:
  otlp:
    protocols:
      grpc:
        endpoint: %q%s
exporters:
  file:
    path: %q
extensions:
  health_check:
    endpoint: %q
service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [file]
`, ingest, tlsConfig, output, health)
			configPath := filepath.Join(directory, "collector.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(configuration), 0600))
			startCollector(t, binary, configPath, health, directory)
			previousTracer, previousWool := otel.GetTracerProvider(), wool.GetTelemetry()
			t.Cleanup(func() { otel.SetTracerProvider(previousTracer); wool.RegisterTelemetry(previousWool) })
			if mode == "environment-precedence" {
				// A real, unused address proves signal-specific selection wins over general.
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+freeAddress(t))
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://"+ingest)
			}
			var backend *wooltel.Provider
			var err error
			if mode == "environment-precedence" {
				backend, err = wooltel.Enable()
			} else {
				backend, err = wooltel.EnableConfigured(wooltel.Configuration{
					State: "enabled", Endpoint: scheme + "://" + ingest,
					Protocol: "grpc", CollectorTier: "node-agent",
				})
			}
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = backend.Shutdown(ctx)
			})
			sink := &capture{}
			provider := wool.New(context.Background(), &wool.Resource{Kind: "test", Unique: "wire-proof"}).WithLogger(sink).WithTelemetry(backend)
			ctx, span := otel.Tracer("integration").Start(context.Background(), "wire-proof")
			wool.Get(provider.Inject(ctx)).WithLogger(sink).Info("handling request")
			span.End()
			flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, backend.Shutdown(flush))
			record := sink.only(t)
			var received ptrace.ResourceSpans
			var receivedSpan ptrace.Span
			require.Eventually(t, func() bool {
				for _, traces := range collectorTraces(output) {
					resources := traces.ResourceSpans()
					for i := 0; i < resources.Len(); i++ {
						resource := resources.At(i)
						scopes := resource.ScopeSpans()
						for j := 0; j < scopes.Len(); j++ {
							spans := scopes.At(j).Spans()
							for k := 0; k < spans.Len(); k++ {
								span := spans.At(k)
								if span.Name() == "wire-proof" {
									received, receivedSpan = resource, span
									return true
								}
							}
						}
					}
				}
				return false
			}, 5*time.Second, 20*time.Millisecond, "collector never exported the correlated span")
			require.Equal(t, record.TraceID, receivedSpan.TraceID().String())
			require.Equal(t, record.SpanID, receivedSpan.SpanID().String())
			attributes := received.Resource().Attributes().AsRaw()
			require.Equal(t, "configured-service", attributes["service.name"])
			require.Equal(t, "integration", attributes["deployment.environment.name"])
			require.Equal(t, "test-build", attributes["service.version"])
		})
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

func startCollector(t *testing.T, binary, config, health, directory string) {
	t.Helper()
	logPath := filepath.Join(directory, "collector.log")
	output, err := os.Create(logPath)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := exec.CommandContext(ctx, binary, "--config", config)
	process.Stdout, process.Stderr = output, output
	require.NoError(t, process.Start())
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = output.Close()
		if t.Failed() {
			log, _ := os.ReadFile(logPath)
			t.Log(string(log))
		}
	})
	client := &http.Client{Timeout: 200 * time.Millisecond}
	require.Eventually(t, func() bool {
		response, err := client.Get("http://" + health)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 10*time.Second, 20*time.Millisecond, "collector health extension did not become ready")
}

func collectorTraces(path string) []ptrace.Traces {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	var requests []ptrace.Traces
	decoder := &ptrace.JSONUnmarshaler{}
	for scanner.Scan() {
		request, err := decoder.UnmarshalTraces(scanner.Bytes())
		if err == nil {
			requests = append(requests, request)
		}
	}
	return requests
}

func collectorCertificate(t *testing.T, directory string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "collector-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
	return certPath, keyPath
}
