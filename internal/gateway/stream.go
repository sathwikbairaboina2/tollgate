package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/sathwikbairaboina2/tollgate/internal/budget"
)

type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *upstreamUsage `json:"usage"`
}

// relayStream copies SSE events to the client as they arrive, watching for usage. If the client
// did not ask for usage, the usage-only chunk the gateway requested is dropped. Settlement runs
// after the loop, so a client disconnect still settles the reservation.
func (g *Gateway) relayStream(w http.ResponseWriter, span trace.Span, ks *keyState, req *chatRequest, t target, resp *http.Response, res *budget.Reservation, promptBound int64) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var (
		usage        *upstreamUsage
		model        string
		finish       []string
		contentBytes int64
		skipBlank    bool
	)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), maxSSELineBytes)
	for sc.Scan() {
		line := sc.Text()
		if line == "" && skipBlank {
			skipBlank = false // the blank line that ended a dropped event
			continue
		}
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			data = strings.TrimSpace(data)
			var c streamChunk
			if data != "[DONE]" && json.Unmarshal([]byte(data), &c) == nil {
				if c.Model != "" {
					model = c.Model
				}
				for _, ch := range c.Choices {
					contentBytes += int64(len(ch.Delta.Content))
					if ch.FinishReason != nil && *ch.FinishReason != "" {
						finish = append(finish, *ch.FinishReason)
					}
				}
				if c.Usage != nil {
					usage = c.Usage
					if !req.WantsUsage && len(c.Choices) == 0 {
						skipBlank = true
						continue
					}
				}
			}
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			break // client went away; stop reading and settle below
		}
		if line == "" && flusher != nil {
			flusher.Flush()
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	// Without reported usage, charge the prompt bound plus observed content bytes, an upper
	// bound on output tokens for byte-level tokenizers (ADR 0001), capped at max_tokens.
	in, out := promptBound, min(contentBytes, int64(req.MaxTokens))
	if usage != nil {
		in, out = usage.PromptTokens, usage.CompletionTokens
	}
	g.settle(span, ks, req, t, res, in, out, model, finish)
}
