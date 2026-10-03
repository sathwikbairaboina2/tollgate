// Command bench replays a fixed workload through Tollgate against an in-process fake upstream.
// It reports the latency the gateway adds over calling the upstream directly, and what the
// exact-match cache saves on the same workload. Every number it prints is measured.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/sathwikbairaboina2/tollgate/internal/budget"
	"github.com/sathwikbairaboina2/tollgate/internal/config"
	"github.com/sathwikbairaboina2/tollgate/internal/fakeupstream"
	"github.com/sathwikbairaboina2/tollgate/internal/gateway"
)

const benchKey = "tg-bench-key"

// Prices are gpt-4o-mini list prices, used only to turn tokens into dollars.
var price = config.Price{Input: 0.15, Output: 0.60}

type results struct {
	Timestamp        string  `json:"timestamp"`
	GoVersion        string  `json:"go_version"`
	OS               string  `json:"os"`
	Arch             string  `json:"arch"`
	NumCPU           int     `json:"num_cpu"`
	WorkloadFile     string  `json:"workload_file"`
	WorkloadSHA256   string  `json:"workload_sha256"`
	Requests         int     `json:"requests"`
	Rounds           int     `json:"rounds"`
	UpstreamDelay    string  `json:"upstream_delay"`
	DirectP50Ms      float64 `json:"direct_p50_ms"`
	DirectP99Ms      float64 `json:"direct_p99_ms"`
	GatewayP50Ms     float64 `json:"gateway_p50_ms"`
	GatewayP99Ms     float64 `json:"gateway_p99_ms"`
	AddedP50Ms       float64 `json:"added_p50_ms"`
	AddedP99Ms       float64 `json:"added_p99_ms"`
	CacheHits        int     `json:"cache_hits"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	CostNoCacheUSD   float64 `json:"cost_no_cache_usd"`
	CostWithCacheUSD float64 `json:"cost_with_cache_usd"`
	CostSavedPct     float64 `json:"cost_saved_pct"`
}

func main() {
	workload := flag.String("workload", "bench/workload.jsonl", "JSONL request set to replay")
	rounds := flag.Int("rounds", 5, "times to replay the workload in the latency phase")
	delay := flag.Duration("upstream-delay", 0, "artificial latency added by the fake upstream")
	out := flag.String("out", "", "write JSON results to this path")
	flag.Parse()

	reqs, sum := load(*workload)
	up := httptest.NewServer(fakeupstream.Handler(*delay))
	defer up.Close()
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 16}}

	res := results{
		Timestamp: time.Now().UTC().Format(time.RFC3339), GoVersion: runtime.Version(),
		OS: runtime.GOOS, Arch: runtime.GOARCH, NumCPU: runtime.NumCPU(),
		WorkloadFile: *workload, WorkloadSHA256: sum, Requests: len(reqs), Rounds: *rounds,
		UpstreamDelay: delay.String(),
	}
	latency(client, up.URL, reqs, *rounds, &res)
	cacheSavings(client, up.URL, reqs, &res)
	report(res)
	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

func load(path string) ([][]byte, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	h := sha256.Sum256(raw)
	var reqs [][]byte
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 {
			reqs = append(reqs, append([]byte(nil), line...))
		}
	}
	return reqs, hex.EncodeToString(h[:])
}

func newGateway(upstreamURL string, cacheOn bool) *httptest.Server {
	yml := fmt.Sprintf(`providers:
  - { name: fake, base_url: %s/v1 }
routes:
  - model: chat-default
    targets: [ { provider: fake, model: gpt-4o-mini } ]
pricing:
  gpt-4o-mini: { input: 0.15, output: 0.60 }
keys:
  - id: bench
    key: %s
    budget: { usd: 1000000, tokens: 1000000000000 }
    rate_limit: { requests_per_second: 1000000000, burst: 1000000000 }
    max_output_tokens: 1024
cache:
  enabled: %t
`, upstreamURL, benchKey, cacheOn)
	cfg, err := config.Parse([]byte(yml), os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	gw, err := gateway.New(cfg, gateway.Options{})
	if err != nil {
		log.Fatal(err)
	}
	return httptest.NewServer(gw.Handler())
}

func post(c *http.Client, url string, body []byte, auth bool) (*http.Response, []byte, time.Duration) {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer "+benchKey)
	}
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	d := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("POST %s: %d %s", url, resp.StatusCode, b)
	}
	return resp, b, d
}

// latency interleaves direct-to-upstream and through-gateway calls for the same request, with
// the cache off, so every gateway call does the full pipeline: auth, limits, reserve, forward, settle.
func latency(c *http.Client, upstreamURL string, reqs [][]byte, rounds int, res *results) {
	gw := newGateway(upstreamURL, false)
	defer gw.Close()
	direct, through := upstreamURL+"/v1/chat/completions", gw.URL+"/v1/chat/completions"
	for _, b := range reqs[:min(100, len(reqs))] { // warm-up: connections, allocations
		post(c, direct, b, false)
		post(c, through, b, true)
	}
	var d, g []time.Duration
	for range rounds {
		for _, b := range reqs {
			_, _, dd := post(c, direct, b, false)
			_, _, gd := post(c, through, b, true)
			d, g = append(d, dd), append(g, gd)
		}
	}
	slices.Sort(d)
	slices.Sort(g)
	res.DirectP50Ms, res.DirectP99Ms = ms(percentile(d, 0.50)), ms(percentile(d, 0.99))
	res.GatewayP50Ms, res.GatewayP99Ms = ms(percentile(g, 0.50)), ms(percentile(g, 0.99))
	res.AddedP50Ms, res.AddedP99Ms = res.GatewayP50Ms-res.DirectP50Ms, res.GatewayP99Ms-res.DirectP99Ms
}

// cacheSavings replays the workload once with the cache on. Cost without the cache prices
// every response; cost with the cache prices only misses, since hits never reach a provider.
func cacheSavings(c *http.Client, upstreamURL string, reqs [][]byte, res *results) {
	gw := newGateway(upstreamURL, true)
	defer gw.Close()
	var all, paid float64
	for _, b := range reqs {
		resp, body, _ := post(c, gw.URL+"/v1/chat/completions", b, true)
		var parsed struct {
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(body, &parsed)
		cost := budget.Cost(price, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
		all += cost
		if resp.Header.Get("X-Tollgate-Cache") == "hit" {
			res.CacheHits++
		} else {
			paid += cost
		}
	}
	res.CacheHitRate = float64(res.CacheHits) / float64(len(reqs))
	res.CostNoCacheUSD, res.CostWithCacheUSD = all, paid
	if all > 0 {
		res.CostSavedPct = 100 * (all - paid) / all
	}
}

func report(r results) {
	fmt.Printf("Tollgate bench: %d requests x %d rounds, upstream delay %s, %s %s/%s, %d CPUs\n\n",
		r.Requests, r.Rounds, r.UpstreamDelay, r.GoVersion, r.OS, r.Arch, r.NumCPU)
	fmt.Println("| metric | p50 | p99 |")
	fmt.Println("|---|---|---|")
	fmt.Printf("| direct to upstream (ms) | %.3f | %.3f |\n", r.DirectP50Ms, r.DirectP99Ms)
	fmt.Printf("| through Tollgate (ms) | %.3f | %.3f |\n", r.GatewayP50Ms, r.GatewayP99Ms)
	fmt.Printf("| **added by Tollgate (ms)** | **%.3f** | **%.3f** |\n\n", r.AddedP50Ms, r.AddedP99Ms)
	fmt.Printf("cache: %d/%d hits (%.1f%%), cost $%.6f -> $%.6f (%.1f%% saved)\n",
		r.CacheHits, r.Requests, 100*r.CacheHitRate, r.CostNoCacheUSD, r.CostWithCacheUSD, r.CostSavedPct)
}
