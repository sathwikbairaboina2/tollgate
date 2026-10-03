package gateway

import (
	"net/http"
	"testing"
)

const cacheOn = "cache:\n  enabled: true\n"

func TestCache_HitSkipsUpstreamAndBudget(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL())+cacheOn)
	first := h.post(helloBody)
	firstBody := readBody(t, first)
	spent, _ := h.gw.Usage("team-a")
	second := h.post(helloBody)
	secondBody := readBody(t, second)
	if first.Header.Get("X-Tollgate-Cache") != "miss" || second.Header.Get("X-Tollgate-Cache") != "hit" {
		t.Fatalf("cache headers %q then %q", first.Header.Get("X-Tollgate-Cache"), second.Header.Get("X-Tollgate-Cache"))
	}
	if firstBody != secondBody {
		t.Fatal("cached body differs from original")
	}
	if up.hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", up.hits.Load())
	}
	if after, _ := h.gw.Usage("team-a"); after != spent {
		t.Fatalf("cache hit changed spend: %+v -> %+v", spent, after)
	}
}

func TestCache_KeyIgnoresUserButNotMessages(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL())+cacheOn)
	readBody(t, h.post(`{"model":"chat-default","messages":[{"role":"user","content":"hi"}],"user":"a"}`))
	resp := h.post(`{"user":"b","messages":[{"role":"user","content":"hi"}],"model":"chat-default"}`)
	readBody(t, resp)
	if resp.Header.Get("X-Tollgate-Cache") != "hit" {
		t.Fatal("user field or key order caused a miss")
	}
	resp = h.post(`{"model":"chat-default","messages":[{"role":"user","content":"different"}]}`)
	readBody(t, resp)
	if resp.Header.Get("X-Tollgate-Cache") != "miss" {
		t.Fatal("different messages hit the cache")
	}
}

func TestCache_DoesNotStoreErrors(t *testing.T) {
	up := newUpstream(t, status(http.StatusBadRequest))
	h := newHarness(t, oneRoute("", up.baseURL())+cacheOn)
	readBody(t, h.post(helloBody))
	readBody(t, h.post(helloBody))
	if up.hits.Load() != 2 {
		t.Fatalf("error response was cached: hits = %d", up.hits.Load())
	}
}

func TestCache_HitsStillCountAgainstRateLimit(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("    rate_limit: { requests_per_second: 1, burst: 1 }\n", up.baseURL())+cacheOn)
	readBody(t, h.post(helloBody))
	resp := h.post(helloBody)
	readBody(t, resp)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("cached request bypassed rate limit: %d", resp.StatusCode)
	}
}

func TestCache_ScopedPerVirtualKey(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL())+"  - id: team-b\n    key: tg-other-key\n"+cacheOn)
	cacheHeader := func(auth string) string {
		resp := h.postKey(auth, helloBody)
		readBody(t, resp)
		return resp.Header.Get("X-Tollgate-Cache")
	}
	if got := cacheHeader("Bearer " + testKey); got != "miss" {
		t.Fatalf("key A first = %q, want miss", got)
	}
	if got := cacheHeader("Bearer " + testKey); got != "hit" {
		t.Fatalf("key A second = %q, want hit", got)
	}
	if got := cacheHeader("Bearer tg-other-key"); got != "miss" {
		t.Fatalf("key B saw key A's cached response: %q", got)
	}
	if up.hits.Load() != 2 {
		t.Fatalf("upstream hits = %d, want 2", up.hits.Load())
	}
}
