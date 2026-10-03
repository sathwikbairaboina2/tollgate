package gateway

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// With max_output_tokens 100, helloBody reserves 38 (prompt bound) + 100 = 138 tokens.
const capped = "    max_output_tokens: 100\n"

func TestBudget_RejectsBeforeForwardingWhenReservationExceedsTokens(t *testing.T) {
	up := newUpstream(t, okCompletion(1, 1))
	h := newHarness(t, oneRoute(capped+"    budget: { tokens: 137 }\n", up.baseURL()))
	resp := h.post(helloBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusPaymentRequired || errCode(t, body) != "budget_exceeded" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if up.hits.Load() != 0 {
		t.Fatal("over-budget request reached upstream")
	}

	up2 := newUpstream(t, okCompletion(1, 1))
	h2 := newHarness(t, oneRoute(capped+"    budget: { tokens: 138 }\n", up2.baseURL()))
	if resp := h2.post(helloBody); resp.StatusCode != http.StatusOK {
		t.Fatalf("reservation exactly at budget rejected: %d", resp.StatusCode)
	}
}

func TestBudget_RejectsOnceSettledSpendLeavesNoRoom(t *testing.T) {
	up := newUpstream(t, okCompletion(100, 100))
	h := newHarness(t, oneRoute(capped+"    budget: { tokens: 300 }\n", up.baseURL()))
	if resp := h.post(helloBody); resp.StatusCode != http.StatusOK {
		t.Fatalf("first request: %d", resp.StatusCode)
	}
	resp := h.post(helloBody) // 200 spent + 138 reserved > 300
	if body := readBody(t, resp); resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("second request: %d %s", resp.StatusCode, body)
	}
	if up.hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", up.hits.Load())
	}
}

func TestBudget_EnforcesUSD(t *testing.T) {
	up := newUpstream(t, okCompletion(1, 1))
	h := newHarness(t, oneRoute(capped+"    budget: { usd: 137.5 }\n", up.baseURL())) // $1/token, needs $138
	resp := h.post(helloBody)
	if body := readBody(t, resp); resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if up.hits.Load() != 0 {
		t.Fatal("over-budget request reached upstream")
	}
}

func TestBudget_ReservesAgainstMostExpensiveTarget(t *testing.T) {
	cheap := newUpstream(t, okCompletion(1, 1))
	pricey := newUpstream(t, okCompletion(1, 1))
	cfg := fmt.Sprintf(`providers:
  - { name: cheap, base_url: %s }
  - { name: pricey, base_url: %s }
routes:
  - model: chat-default
    targets:
      - { provider: cheap, model: c }
      - { provider: pricey, model: p }
pricing:
  c: { input: 1000000, output: 1000000 }
  p: { input: 2000000, output: 2000000 }
keys:
  - id: team-a
    key: %s
    max_output_tokens: 100
    budget: { usd: 200 }
`, cheap.baseURL(), pricey.baseURL(), testKey)
	h := newHarness(t, cfg)
	resp := h.post(helloBody) // cheap would cost $138, but fallback could cost $276
	if body := readBody(t, resp); resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if cheap.hits.Load()+pricey.hits.Load() != 0 {
		t.Fatal("request reached an upstream")
	}
}

func TestBudget_ConcurrentRequestsNeverOverspend(t *testing.T) {
	release := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		okCompletion(1, 1)(w, r)
	})
	h := newHarness(t, oneRoute(capped+"    budget: { tokens: 414 }\n", up.baseURL())) // exactly 3 x 138

	var ok, rejected atomic.Int64
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := h.post(helloBody)
			readBody(t, resp)
			switch resp.StatusCode {
			case http.StatusOK:
				ok.Add(1)
			case http.StatusPaymentRequired:
				rejected.Add(1)
			}
		}()
	}
	waitFor(t, "3 admitted and 17 rejected", func() bool { return up.hits.Load() == 3 && rejected.Load() == 17 })
	close(release)
	wg.Wait()
	if ok.Load() != 3 || rejected.Load() != 17 {
		t.Fatalf("ok=%d rejected=%d, want 3/17", ok.Load(), rejected.Load())
	}
}

func TestRateLimit_RejectsBeyondBurstWithRetryAfter(t *testing.T) {
	up := newUpstream(t, okCompletion(1, 1))
	h := newHarness(t, oneRoute("    rate_limit: { requests_per_second: 1, burst: 2 }\n", up.baseURL()))
	for i := range 2 {
		if resp := h.post(helloBody); resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d within burst: %d", i, resp.StatusCode)
		}
	}
	resp := h.post(helloBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusTooManyRequests || errCode(t, body) != "rate_limited" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want 1", got)
	}
	if up.hits.Load() != 2 {
		t.Fatalf("upstream hits = %d, want 2", up.hits.Load())
	}
	h.clock.Advance(time.Second)
	if resp := h.post(helloBody); resp.StatusCode != http.StatusOK {
		t.Fatalf("after refill: %d", resp.StatusCode)
	}
}

func TestRateLimit_RetryAfterRoundsUp(t *testing.T) {
	up := newUpstream(t, okCompletion(1, 1))
	h := newHarness(t, oneRoute("    rate_limit: { requests_per_second: 0.4, burst: 1 }\n", up.baseURL()))
	readBody(t, h.post(helloBody))
	resp := h.post(helloBody)
	readBody(t, resp)
	if got := resp.Header.Get("Retry-After"); got != "3" { // 2.5s rounds up to 3
		t.Fatalf("Retry-After = %q, want 3", got)
	}
	if u, _ := h.gw.Usage("team-a"); u.SpentTokens != 2 {
		t.Fatalf("rate-limited request changed spend: %+v", u)
	}
}
