// Package telemetry owns Tollgate's Prometheus metrics and OpenTelemetry tracer setup.
package telemetry

import "github.com/prometheus/client_golang/prometheus"

// Metrics are the gateway's Prometheus collectors.
type Metrics struct {
	Requests        *prometheus.CounterVec
	Tokens          *prometheus.CounterVec
	CostUSD         *prometheus.CounterVec
	CacheHits       *prometheus.CounterVec
	Fallbacks       *prometheus.CounterVec
	Rejections      *prometheus.CounterVec
	UpstreamLatency *prometheus.HistogramVec
}

// NewMetrics creates the collectors and registers them on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tollgate_requests_total", Help: "Chat requests by key, public model and outcome.",
		}, []string{"key", "model", "outcome"}),
		Tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tollgate_tokens_total", Help: "Settled tokens by key, upstream model and direction.",
		}, []string{"key", "model", "direction"}),
		CostUSD: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tollgate_cost_usd_total", Help: "Settled spend in USD by key and upstream model.",
		}, []string{"key", "model"}),
		CacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tollgate_cache_hits_total", Help: "Responses served from the cache.",
		}, []string{"key"}),
		Fallbacks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tollgate_fallbacks_total", Help: "Upstream attempts abandoned for the next target.",
		}, []string{"provider", "reason"}),
		Rejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tollgate_rejections_total", Help: "Requests refused by a limit before forwarding.",
		}, []string{"key", "reason"}),
		UpstreamLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "tollgate_upstream_duration_seconds", Help: "Time to upstream response headers.",
			Buckets: prometheus.ExponentialBuckets(0.005, 2, 14),
		}, []string{"provider"}),
	}
	reg.MustRegister(m.Requests, m.Tokens, m.CostUSD, m.CacheHits, m.Fallbacks, m.Rejections, m.UpstreamLatency)
	return m
}
