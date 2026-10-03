package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/sathwikbairaboina2/tollgate/internal/config"
)

const testKey = "tg-test-key"

// upstream is a fake OpenAI-compatible server that records hits and request bodies.
type upstream struct {
	*httptest.Server
	hits   atomic.Int64
	mu     sync.Mutex
	bodies []map[string]any
}

func newUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		u.mu.Lock()
		u.bodies = append(u.bodies, b)
		u.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		h(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) baseURL() string { return u.URL + "/v1" }

func (u *upstream) lastBody() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.bodies) == 0 {
		return nil
	}
	return u.bodies[len(u.bodies)-1]
}

// okCompletion answers like OpenAI with fixed usage. It 404s on the wrong path.
func okCompletion(prompt, completion int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"cmpl-1","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`, prompt, completion, prompt+completion)
	}
}

// noUsageCompletion answers without a usage block.
func noUsageCompletion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"cmpl-2","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		io.WriteString(w, `{"error":{"message":"boom"}}`)
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type harness struct {
	t     *testing.T
	gw    *Gateway
	srv   *httptest.Server
	spans *tracetest.SpanRecorder
	reg   *prometheus.Registry
	clock *fakeClock
}

func newHarness(t *testing.T, yamlCfg string) *harness {
	t.Helper()
	cfg, err := config.Parse([]byte(yamlCfg), func(string) string { return "" })
	if err != nil {
		t.Fatalf("config: %v\n%s", err, yamlCfg)
	}
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	reg := prometheus.NewRegistry()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	gw, err := New(cfg, Options{Now: clk.Now, Tracer: tp.Tracer("test"), Registry: reg})
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	srv := httptest.NewServer(gw.Handler())
	t.Cleanup(srv.Close)
	return &harness{t: t, gw: gw, srv: srv, spans: sr, reg: reg, clock: clk}
}

// oneRoute builds a config with key "team-a" and route "chat-default" over the given upstreams,
// in order. Target i is provider "p<i>" with model "upstream-<i>", priced at $1 per token
// so USD equals tokens. keyExtra holds extra key fields, indented by four spaces.
func oneRoute(keyExtra string, upstreamURLs ...string) string {
	var b strings.Builder
	b.WriteString("providers:\n")
	for i, u := range upstreamURLs {
		fmt.Fprintf(&b, "  - name: p%d\n    base_url: %s\n", i, u)
	}
	b.WriteString("routes:\n  - model: chat-default\n    targets:\n")
	for i := range upstreamURLs {
		fmt.Fprintf(&b, "      - { provider: p%d, model: upstream-%d }\n", i, i)
	}
	b.WriteString("pricing:\n")
	for i := range upstreamURLs {
		fmt.Fprintf(&b, "  upstream-%d: { input: 1000000, output: 1000000 }\n", i)
	}
	fmt.Fprintf(&b, "keys:\n  - id: team-a\n    key: %s\n", testKey)
	b.WriteString(keyExtra)
	return b.String()
}

func (h *harness) post(body string) *http.Response { return h.postKey("Bearer "+testKey, body) }

func (h *harness) postKey(auth, body string) *http.Response {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("post: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func errCode(t *testing.T, body string) string {
	t.Helper()
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("not an error body: %q", body)
	}
	return e.Error.Code
}

// waitFor polls cond for up to 2s. Spans end and streams settle after the response is written.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func spanAttrs(s sdktrace.ReadOnlySpan) map[string]attribute.Value {
	m := map[string]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[string(kv.Key)] = kv.Value
	}
	return m
}
