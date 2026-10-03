package fakeupstream

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, srv *httptest.Server, path, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

const req = `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"How do I rotate an API key?"}]}`

func TestHandler_DeterministicUsage(t *testing.T) {
	srv := httptest.NewServer(Handler(0))
	defer srv.Close()
	_, a := post(t, srv, "/v1/chat/completions", req)
	_, b := post(t, srv, "/v1/chat/completions", req)
	if a != b {
		t.Fatalf("responses differ:\n%s\n%s", a, b)
	}
	var c struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(a), &c); err != nil {
		t.Fatal(err)
	}
	// "How do I rotate an API key?" is 27 chars: 27/4 + 4*1 = 10.
	if c.Model != "gpt-4o-mini" || c.Usage.PromptTokens != 10 || c.Usage.CompletionTokens < 32 || c.Usage.CompletionTokens >= 256 {
		t.Fatalf("parsed = %+v", c)
	}
}

func TestHandler_StreamsWithUsageWhenAsked(t *testing.T) {
	srv := httptest.NewServer(Handler(0))
	defer srv.Close()
	body := strings.Replace(req, `"messages"`, `"stream":true,"stream_options":{"include_usage":true},"messages"`, 1)
	resp, out := post(t, srv, "/v1/chat/completions", body)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(out, `"usage":{"prompt_tokens":10`) || !strings.HasSuffix(strings.TrimSpace(out), "data: [DONE]") {
		t.Fatalf("stream = %s", out)
	}
	noUsage := strings.Replace(req, `"messages"`, `"stream":true,"messages"`, 1)
	if _, out := post(t, srv, "/v1/chat/completions", noUsage); strings.Contains(out, `"usage"`) {
		t.Fatal("usage chunk sent without include_usage")
	}
}

func TestHandler_RejectsWrongPathAndBadJSON(t *testing.T) {
	srv := httptest.NewServer(Handler(0))
	defer srv.Close()
	if resp, _ := post(t, srv, "/v1/embeddings", req); resp.StatusCode != http.StatusNotFound {
		t.Errorf("wrong path: %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "/v1/chat/completions", "nope"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad json: %d", resp.StatusCode)
	}
}
