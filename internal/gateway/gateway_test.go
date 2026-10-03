package gateway

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChat_RejectsMissingOrUnknownKey(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL()))
	for _, auth := range []string{"", "Bearer nope", "Basic " + testKey, "Bearer "} {
		resp := h.postKey(auth, helloBody)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errCode(t, body) != "invalid_api_key" {
			t.Errorf("auth %q: %d %s", auth, resp.StatusCode, body)
		}
	}
	if up.hits.Load() != 0 {
		t.Fatalf("unauthenticated requests reached upstream %d times", up.hits.Load())
	}
}

func TestChat_RejectsBadRequests(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL()))
	for _, body := range []string{
		`not json`, `{}`, `{"model":"chat-default"}`, `{"model":"chat-default","messages":[]}`,
		`{"model":"chat-default","messages":[{}],"stream":"yes"}`,
	} {
		resp := h.post(body)
		got := readBody(t, resp)
		if resp.StatusCode != http.StatusBadRequest || errCode(t, got) != "invalid_request" {
			t.Errorf("%s: %d %s", body, resp.StatusCode, got)
		}
	}
	if up.hits.Load() != 0 {
		t.Fatal("bad requests reached upstream")
	}
}

func TestChat_UnknownModel(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL()))
	resp := h.post(`{"model":"nope","messages":[{"role":"user","content":"hi"}]}`)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound || errCode(t, body) != "model_not_found" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestChat_ProxiesNonStreaming(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL()))
	resp := h.post(helloBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if got := up.lastBody()["model"]; got != "upstream-0" {
		t.Errorf("upstream saw model %v, want upstream-0", got)
	}
	if resp.Header.Get("X-Tollgate-Provider") != "p0" || resp.Header.Get("X-Tollgate-Attempts") != "1" {
		t.Errorf("headers = %v", resp.Header)
	}
	if resp.Header.Get("X-Tollgate-Cache") != "" {
		t.Error("cache header set while cache disabled")
	}
}

func TestChat_CapsMaxTokens(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("    max_output_tokens: 100\n", up.baseURL()))
	for body, want := range map[string]float64{
		helloBody: 100,
		`{"model":"chat-default","messages":[{"role":"user","content":"hi"}],"max_tokens":5000}`: 100,
		`{"model":"chat-default","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`:   10,
	} {
		readBody(t, h.post(body))
		if got := up.lastBody()["max_tokens"]; got != want {
			t.Errorf("%s: upstream max_tokens = %v, want %v", body, got, want)
		}
	}
}

func TestChat_SettlesUsageFromUpstream(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL()))
	readBody(t, h.post(helloBody))
	u, _ := h.gw.Usage("team-a")
	if u.SpentTokens != 15 || u.SpentUSD != 15 || u.HeldTokens != 0 || u.HeldUSD != 0 {
		t.Fatalf("usage = %+v, want 15 tokens / $15 spent, nothing held", u)
	}
}

func TestChat_ChargesWorstCaseWhenUsageMissing(t *testing.T) {
	up := newUpstream(t, noUsageCompletion)
	h := newHarness(t, oneRoute("    max_output_tokens: 100\n", up.baseURL()))
	readBody(t, h.post(helloBody))
	u, _ := h.gw.Usage("team-a")
	if u.SpentTokens != 38+100 {
		t.Fatalf("spent = %d, want prompt bound 38 + cap 100", u.SpentTokens)
	}
}

func TestChat_EmitsGenAISpan(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("    max_output_tokens: 100\n", up.baseURL()))
	readBody(t, h.post(helloBody))
	waitFor(t, "span to end", func() bool { return len(h.spans.Ended()) == 1 })
	s := h.spans.Ended()[0]
	if s.Name() != "chat chat-default" {
		t.Errorf("span name = %q", s.Name())
	}
	a := spanAttrs(s)
	checks := map[string]string{
		"gen_ai.operation.name": "chat", "gen_ai.request.model": "chat-default",
		"gen_ai.provider.name": "p0", "gen_ai.response.model": "upstream-model", "tollgate.key_id": "team-a",
	}
	for k, want := range checks {
		if got := a[k].AsString(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if a["gen_ai.request.max_tokens"].AsInt64() != 100 || a["gen_ai.usage.input_tokens"].AsInt64() != 10 || a["gen_ai.usage.output_tokens"].AsInt64() != 5 {
		t.Errorf("numeric attrs = %v %v %v", a["gen_ai.request.max_tokens"], a["gen_ai.usage.input_tokens"], a["gen_ai.usage.output_tokens"])
	}
	if fr := a["gen_ai.response.finish_reasons"].AsStringSlice(); len(fr) != 1 || fr[0] != "stop" {
		t.Errorf("finish reasons = %v", fr)
	}
}

func TestMetricsAndHealthEndpoints(t *testing.T) {
	up := newUpstream(t, okCompletion(10, 5))
	h := newHarness(t, oneRoute("", up.baseURL()))
	readBody(t, h.post(helloBody))
	resp, err := http.Get(h.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{
		`tollgate_requests_total{key="team-a",model="chat-default",outcome="ok"} 1`,
		`tollgate_tokens_total{direction="input",key="team-a",model="upstream-0"} 10`,
		`tollgate_tokens_total{direction="output",key="team-a",model="upstream-0"} 5`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	resp, err = http.Get(h.srv.URL + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %v %v", err, resp)
	}
	resp.Body.Close()
}
