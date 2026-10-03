package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in: set TOLLGATE_IT_OLLAMA_URL (e.g. http://host.docker.internal:11434/v1) to run against a live Ollama.
// TOLLGATE_IT_OLLAMA_MODEL picks the model (default gemma4:12b).
func ollamaHarness(t *testing.T) *harness {
	t.Helper()
	url := os.Getenv("TOLLGATE_IT_OLLAMA_URL")
	if url == "" {
		t.Skip("TOLLGATE_IT_OLLAMA_URL not set")
	}
	model := os.Getenv("TOLLGATE_IT_OLLAMA_MODEL")
	if model == "" {
		model = "gemma4:12b"
	}
	cfg := fmt.Sprintf(`providers:
  - name: ollama
    base_url: %s
    timeout: 120s
routes:
  - model: chat-default
    targets:
      - { provider: ollama, model: %s }
pricing:
  %s: { input: 0, output: 0 }
keys:
  - id: team-a
    key: %s
    budget: { tokens: 100000 }
    max_output_tokens: 32
`, url, model, model, testKey)
	return newHarness(t, cfg)
}

const pongBody = `{"model":"chat-default","messages":[{"role":"user","content":"Reply with the single word: pong"}]`

func TestOllama_NonStreaming(t *testing.T) {
	h := ollamaHarness(t)
	start := time.Now()
	resp := h.post(pongBody + `}`)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, body)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("bad body %q: %v", body, err)
	}
	if len(out.Choices) == 0 || (out.Choices[0].Message.Content == "" && out.Choices[0].Message.Reasoning == "") {
		t.Fatalf("empty message: %s", body)
	}
	if out.Usage.CompletionTokens > 32 {
		t.Fatalf("completion_tokens = %d, want <= 32 (max_output_tokens clamp)", out.Usage.CompletionTokens)
	}
	waitFor(t, "settlement", func() bool {
		u, _ := h.gw.Usage("team-a")
		return u.HeldTokens == 0 && u.SpentTokens > 0
	})
	u, _ := h.gw.Usage("team-a")
	if u.SpentTokens != out.Usage.TotalTokens {
		t.Fatalf("spent = %d, upstream total = %d", u.SpentTokens, out.Usage.TotalTokens)
	}
	t.Logf("ok in %s: %d tokens spent, content %q", time.Since(start).Round(time.Millisecond), u.SpentTokens, out.Choices[0].Message.Content)
}

func TestOllama_Streaming(t *testing.T) {
	h := ollamaHarness(t)
	start := time.Now()
	resp := h.post(pongBody + `,"stream":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, readBody(t, resp))
	}
	lines := dataLines(t, resp.Body)
	resp.Body.Close()
	if len(lines) < 2 {
		t.Fatalf("got %d data chunks, want >= 2: %v", len(lines), lines)
	}
	if last := lines[len(lines)-1]; strings.TrimSpace(last) != "[DONE]" {
		t.Fatalf("last chunk = %q, want [DONE]", last)
	}
	waitFor(t, "stream settlement", func() bool {
		u, _ := h.gw.Usage("team-a")
		return u.HeldTokens == 0 && u.SpentTokens > 0
	})
	u, _ := h.gw.Usage("team-a")
	t.Logf("ok in %s: %d chunks, %d tokens spent", time.Since(start).Round(time.Millisecond), len(lines), u.SpentTokens)
}
