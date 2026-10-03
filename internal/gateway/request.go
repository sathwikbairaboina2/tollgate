package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// tokenFields are the OpenAI output-limit parameters, newest first.
var tokenFields = []string{"max_completion_tokens", "max_tokens"}

// chatRequest is a client request kept as a JSON map so unknown fields pass through untouched.
type chatRequest struct {
	body       map[string]any
	Model      string
	Stream     bool
	MaxTokens  int // 0 when the client sent no limit
	WantsUsage bool
}

func parseRequest(raw []byte) (*chatRequest, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil || body == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	r := &chatRequest{body: body}
	if r.Model, _ = body["model"].(string); r.Model == "" {
		return nil, errors.New("model is required")
	}
	if msgs, _ := body["messages"].([]any); len(msgs) == 0 {
		return nil, errors.New("messages must be a non-empty array")
	}
	if v, ok := body["stream"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("stream must be a boolean")
		}
		r.Stream = b
	}
	if so, ok := body["stream_options"].(map[string]any); ok {
		r.WantsUsage, _ = so["include_usage"].(bool)
	}
	for _, f := range tokenFields {
		v, ok := body[f]
		if !ok || v == nil {
			continue
		}
		num, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer", f)
		}
		n, err := num.Int64()
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s must be a non-negative integer", f)
		}
		if n > 0 && (r.MaxTokens == 0 || int(n) < r.MaxTokens) {
			r.MaxTokens = int(n)
		}
	}
	return r, nil
}

// capMaxTokens bounds the output so the worst-case cost is known before forwarding.
// Every token-limit field the client sent is rewritten; if none was sent, max_tokens is added.
func (r *chatRequest) capMaxTokens(limit int) {
	if r.MaxTokens <= 0 || r.MaxTokens > limit {
		r.MaxTokens = limit
	}
	set := false
	for _, f := range tokenFields {
		if _, ok := r.body[f]; ok {
			r.body[f] = r.MaxTokens
			set = true
		}
	}
	if !set {
		r.body["max_tokens"] = r.MaxTokens
	}
}

// bodyFor returns the upstream request body for a target model. For streams it asks the
// upstream to report usage so the reservation can be settled exactly.
func (r *chatRequest) bodyFor(model string) []byte {
	b := make(map[string]any, len(r.body)+1)
	for k, v := range r.body {
		b[k] = v
	}
	b["model"] = model
	if r.Stream {
		so := map[string]any{}
		if old, ok := r.body["stream_options"].(map[string]any); ok {
			for k, v := range old {
				so[k] = v
			}
		}
		so["include_usage"] = true
		b["stream_options"] = so
	}
	out, _ := json.Marshal(b)
	return out
}

// promptTokenBound is an upper bound on prompt tokens: byte-level BPE tokenizers emit at most
// one token per UTF-8 byte, so the JSON byte length of messages and tools bounds the count.
// See docs/adr/0001-reservation-budgets.md.
func promptTokenBound(body map[string]any) int64 {
	const perRequest = 8
	n := int64(perRequest)
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		b, _ := json.Marshal(m)
		n += int64(len(b))
	}
	if tools, ok := body["tools"]; ok {
		b, _ := json.Marshal(tools)
		n += int64(len(b))
	}
	return n
}
