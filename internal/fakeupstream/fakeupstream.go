// Package fakeupstream is a deterministic OpenAI-compatible chat server for the benchmark and
// the docker-compose demo. Usage is derived from the prompt, so replays are repeatable.
package fakeupstream

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"
)

// Handler answers chat-completions requests after an optional artificial delay.
func Handler(delay time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model         string `json:"model"`
			Stream        bool   `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"invalid JSON","type":"invalid_request_error"}}`)
			return
		}
		chars, h := 0, fnv.New32a()
		for _, m := range req.Messages {
			chars += len(m.Content)
			h.Write([]byte(m.Content))
		}
		prompt := chars/4 + 4*len(req.Messages)
		completion := 32 + int(h.Sum32()%224)
		model := req.Model
		if model == "" {
			model = "fake"
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		if req.Stream {
			stream(w, model, prompt, completion, req.StreamOptions.IncludeUsage)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"fake","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`,
			model, prompt, completion, prompt+completion)
	})
}

func stream(w http.ResponseWriter, model string, prompt, completion int, usage bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	f, _ := w.(http.Flusher)
	send := func(data string) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		if f != nil {
			f.Flush()
		}
	}
	for _, part := range []string{"o", "k"} {
		send(fmt.Sprintf(`{"id":"fake","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, model, part))
	}
	send(fmt.Sprintf(`{"id":"fake","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, model))
	if usage {
		send(fmt.Sprintf(`{"id":"fake","object":"chat.completion.chunk","model":%q,"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`, model, prompt, completion, prompt+completion))
	}
	send("[DONE]")
}
