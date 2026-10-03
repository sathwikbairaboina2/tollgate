package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const streamBody = `{"model":"chat-default","messages":[{"role":"user","content":"hi"}],"stream":true}`

// sseCompletion streams "Hel" + "lo". It sends a usage chunk only when the request asked
// for one (like OpenAI), unless neverUsage is set.
func sseCompletion(neverUsage bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		send := func(s string) { fmt.Fprintf(w, "data: %s\n\n", s); f.Flush() }
		send(`{"id":"c","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`)
		send(`{"id":"c","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`)
		send(`{"id":"c","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		if req.StreamOptions.IncludeUsage && !neverUsage {
			send(`{"id":"c","object":"chat.completion.chunk","model":"upstream-model","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`)
		}
		send("[DONE]")
	}
}

// dataLines returns the payloads of all "data:" lines in an SSE body.
func dataLines(t *testing.T, r io.Reader) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			out = append(out, d)
		}
	}
	return out
}

func contentOf(lines []string) (content string, sawUsage bool) {
	for _, l := range lines {
		var c streamChunk
		if json.Unmarshal([]byte(l), &c) != nil {
			continue
		}
		for _, ch := range c.Choices {
			content += ch.Delta.Content
		}
		if c.Usage != nil {
			sawUsage = true
		}
	}
	return content, sawUsage
}

func TestStream_RelaysChunksInOrderAndSettlesUsage(t *testing.T) {
	up := newUpstream(t, sseCompletion(false))
	h := newHarness(t, oneRoute("", up.baseURL()))
	resp := h.post(streamBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	lines := dataLines(t, resp.Body)
	content, sawUsage := contentOf(lines)
	if content != "Hello" {
		t.Errorf("content = %q", content)
	}
	if sawUsage {
		t.Error("injected usage chunk leaked to a client that did not ask for it")
	}
	if lines[len(lines)-1] != "[DONE]" {
		t.Errorf("last data line = %q", lines[len(lines)-1])
	}
	waitFor(t, "stream usage settled", func() bool {
		u, _ := h.gw.Usage("team-a")
		return u.SpentTokens == 9 && u.HeldTokens == 0
	})
}

func TestStream_InjectsIncludeUsageUpstream(t *testing.T) {
	up := newUpstream(t, sseCompletion(false))
	h := newHarness(t, oneRoute("", up.baseURL()))
	readBody(t, h.post(streamBody))
	so, _ := up.lastBody()["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Fatalf("upstream stream_options = %v", up.lastBody()["stream_options"])
	}
}

func TestStream_ForwardsUsageChunkWhenClientAsked(t *testing.T) {
	up := newUpstream(t, sseCompletion(false))
	h := newHarness(t, oneRoute("", up.baseURL()))
	resp := h.post(`{"model":"chat-default","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`)
	defer resp.Body.Close()
	if _, sawUsage := contentOf(dataLines(t, resp.Body)); !sawUsage {
		t.Fatal("client asked for usage but did not get the usage chunk")
	}
}

func TestStream_ChargesObservedBytesWhenUsageMissing(t *testing.T) {
	up := newUpstream(t, sseCompletion(true))
	h := newHarness(t, oneRoute("    max_output_tokens: 100\n", up.baseURL()))
	readBody(t, h.post(streamBody))
	waitFor(t, "fallback settlement", func() bool {
		u, _ := h.gw.Usage("team-a")
		return u.SpentTokens == 38+5 && u.HeldTokens == 0 // prompt bound + len("Hello")
	})
}

func TestStream_FallsBackBeforeFirstByte(t *testing.T) {
	primary := newUpstream(t, status(http.StatusServiceUnavailable))
	secondary := newUpstream(t, sseCompletion(false))
	h := newHarness(t, oneRoute("", primary.baseURL(), secondary.baseURL()))
	resp := h.post(streamBody)
	defer resp.Body.Close()
	if resp.Header.Get("X-Tollgate-Provider") != "p1" {
		t.Fatalf("provider = %q", resp.Header.Get("X-Tollgate-Provider"))
	}
	if content, _ := contentOf(dataLines(t, resp.Body)); content != "Hello" {
		t.Fatalf("content = %q", content)
	}
}

func TestStream_BypassesCache(t *testing.T) {
	up := newUpstream(t, sseCompletion(false))
	h := newHarness(t, oneRoute("", up.baseURL())+cacheOn)
	readBody(t, h.post(streamBody))
	readBody(t, h.post(streamBody))
	if up.hits.Load() != 2 {
		t.Fatalf("streaming request served from cache: hits = %d", up.hits.Load())
	}
}
