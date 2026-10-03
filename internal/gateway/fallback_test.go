package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestFallback_OnRetryableFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		handler http.HandlerFunc
		reason  string
	}{
		"5xx": {status(http.StatusServiceUnavailable), "server_error"},
		"429": {status(http.StatusTooManyRequests), "rate_limited"},
	} {
		t.Run(name, func(t *testing.T) {
			primary := newUpstream(t, tc.handler)
			secondary := newUpstream(t, okCompletion(10, 5))
			h := newHarness(t, oneRoute("", primary.baseURL(), secondary.baseURL()))
			resp := h.post(helloBody)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%d %s", resp.StatusCode, body)
			}
			if resp.Header.Get("X-Tollgate-Provider") != "p1" || resp.Header.Get("X-Tollgate-Attempts") != "2" {
				t.Errorf("headers = %v", resp.Header)
			}
			if primary.hits.Load() != 1 || secondary.hits.Load() != 1 {
				t.Errorf("hits primary=%d secondary=%d", primary.hits.Load(), secondary.hits.Load())
			}
			if secondary.lastBody()["model"] != "upstream-1" {
				t.Errorf("secondary saw model %v", secondary.lastBody()["model"])
			}
			if got := testutil.ToFloat64(h.gw.metrics.Fallbacks.WithLabelValues("p0", tc.reason)); got != 1 {
				t.Errorf("fallback metric = %v", got)
			}
		})
	}
}

func TestFallback_OnTransportError(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL + "/v1"
	dead.Close()
	secondary := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", deadURL, secondary.baseURL()))
	resp := h.post(helloBody)
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if got := testutil.ToFloat64(h.gw.metrics.Fallbacks.WithLabelValues("p0", "transport_error")); got != 1 {
		t.Errorf("fallback metric = %v", got)
	}
}

func TestFallback_NotOn4xx(t *testing.T) {
	primary := newUpstream(t, status(http.StatusBadRequest))
	secondary := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", primary.baseURL(), secondary.baseURL()))
	resp := h.post(helloBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "boom") {
		t.Fatalf("upstream 400 not passed through: %d %s", resp.StatusCode, body)
	}
	if secondary.hits.Load() != 0 {
		t.Fatal("a 400 triggered fallback")
	}
	if u, _ := h.gw.Usage("team-a"); u.SpentTokens != 0 || u.HeldTokens != 0 {
		t.Fatalf("usage after upstream 400 = %+v", u)
	}
}

func TestFallback_AllFail502ReleasesReservation(t *testing.T) {
	a := newUpstream(t, status(http.StatusInternalServerError))
	b := newUpstream(t, status(http.StatusBadGateway))
	h := newHarness(t, oneRoute("    budget: { tokens: 100000 }\n", a.baseURL(), b.baseURL()))
	resp := h.post(helloBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadGateway || errCode(t, body) != "upstream_unavailable" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if u, _ := h.gw.Usage("team-a"); u.SpentTokens != 0 || u.HeldTokens != 0 {
		t.Fatalf("usage after total failure = %+v", u)
	}
}
