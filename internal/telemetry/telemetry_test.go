package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewMetrics_RegistersOnIsolatedRegistries(t *testing.T) {
	for range 2 { // a second registry must not panic on duplicate registration
		reg := prometheus.NewRegistry()
		m := NewMetrics(reg)
		m.Requests.WithLabelValues("k", "m", "ok").Inc()
		m.Tokens.WithLabelValues("k", "m", "input").Add(3)
		m.CostUSD.WithLabelValues("k", "m").Add(0.5)
		m.CacheHits.WithLabelValues("k").Inc()
		m.Fallbacks.WithLabelValues("p", "server_error").Inc()
		m.Rejections.WithLabelValues("k", "budget_exceeded").Inc()
		m.UpstreamLatency.WithLabelValues("p").Observe(0.01)
		if got := testutil.ToFloat64(m.Tokens.WithLabelValues("k", "m", "input")); got != 3 {
			t.Fatalf("tokens = %v", got)
		}
		families, err := reg.Gather()
		if err != nil {
			t.Fatal(err)
		}
		if len(families) != 7 {
			t.Fatalf("gathered %d metric families, want 7", len(families))
		}
	}
}

func TestSetupTracing_NoopWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	tp, shutdown, err := SetupTracing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	_, span := tp.Tracer("t").Start(context.Background(), "x")
	if span.SpanContext().IsValid() {
		t.Fatal("expected a no-op tracer when no OTLP endpoint is configured")
	}
}

func TestSetupTracing_SDKWithEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	tp, shutdown, err := SetupTracing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Fatalf("provider type = %T, want *sdktrace.TracerProvider", tp)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = shutdown(ctx)
}
