// Package gateway is the HTTP front door: it authenticates, enforces limits, routes,
// forwards with fallback and settles usage for every chat request.
package gateway

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/sathwikbairaboina2/tollgate/internal/budget"
	"github.com/sathwikbairaboina2/tollgate/internal/cache"
	"github.com/sathwikbairaboina2/tollgate/internal/config"
	"github.com/sathwikbairaboina2/tollgate/internal/provider"
	"github.com/sathwikbairaboina2/tollgate/internal/ratelimit"
	"github.com/sathwikbairaboina2/tollgate/internal/telemetry"
)

const (
	maxBodyBytes     = 10 << 20 // client request bodies
	maxUpstreamBytes = 50 << 20 // non-streaming upstream bodies
	maxSSELineBytes  = 1 << 20  // one SSE line
)

// Options are injectable dependencies. Zero values select production defaults.
type Options struct {
	Now      func() time.Time
	Tracer   trace.Tracer
	Registry *prometheus.Registry
}

// Gateway holds routing tables and per-key state. All state is in memory (ADR 0002).
type Gateway struct {
	keys    map[string]*keyState // by bearer token
	byID    map[string]*keyState
	routes  map[string][]target
	cache   cache.Cache // nil when disabled
	metrics *telemetry.Metrics
	reg     *prometheus.Registry
	tracer  trace.Tracer
	now     func() time.Time
}

type keyState struct {
	id     string
	maxOut int
	ledger *budget.Ledger
	bucket *ratelimit.Bucket // nil means unlimited
}

type target struct {
	provider *provider.Provider
	model    string
	price    config.Price
}

// New builds a gateway from a validated config.
func New(cfg *config.Config, opts Options) (*Gateway, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Tracer == nil {
		opts.Tracer = otel.Tracer("tollgate")
	}
	if opts.Registry == nil {
		opts.Registry = prometheus.NewRegistry()
	}
	g := &Gateway{
		keys:    map[string]*keyState{},
		byID:    map[string]*keyState{},
		routes:  map[string][]target{},
		metrics: telemetry.NewMetrics(opts.Registry),
		reg:     opts.Registry,
		tracer:  opts.Tracer,
		now:     opts.Now,
	}
	providers := map[string]*provider.Provider{}
	for _, p := range cfg.Providers {
		providers[p.Name] = provider.New(p)
	}
	for _, r := range cfg.Routes {
		for _, t := range r.Targets {
			p, ok := providers[t.Provider]
			if !ok {
				return nil, fmt.Errorf("route %q: unknown provider %q", r.Model, t.Provider)
			}
			g.routes[r.Model] = append(g.routes[r.Model], target{provider: p, model: t.Model, price: cfg.Pricing[t.Model]})
		}
	}
	for _, k := range cfg.Keys {
		ks := &keyState{id: k.ID, maxOut: k.MaxOutputTokens, ledger: budget.NewLedger(k.Budget)}
		if ks.maxOut <= 0 {
			ks.maxOut = config.DefaultMaxOutputTokens
		}
		if k.RateLimit.RequestsPerSecond > 0 {
			ks.bucket = ratelimit.NewBucket(k.RateLimit.RequestsPerSecond, k.RateLimit.Burst, opts.Now)
		}
		g.keys[k.Key] = ks
		g.byID[k.ID] = ks
	}
	if cfg.Cache.Enabled {
		g.cache = cache.NewLRU(cfg.Cache.MaxEntries, cfg.Cache.TTL.Duration, opts.Now)
	}
	return g, nil
}

// Handler returns the HTTP routes.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", g.handleChat)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.Handle("GET /metrics", promhttp.HandlerFor(g.reg, promhttp.HandlerOpts{}))
	return mux
}

// Usage returns the ledger snapshot for a key id.
func (g *Gateway) Usage(keyID string) (budget.Usage, bool) {
	ks, ok := g.byID[keyID]
	if !ok {
		return budget.Usage{}, false
	}
	return ks.ledger.Usage(), true
}
