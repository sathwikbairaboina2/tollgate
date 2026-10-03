package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

const helloBody = `{"model":"chat-default","messages":[{"role":"user","content":"hi"}]}`

func TestParseRequest_Valid(t *testing.T) {
	r, err := parseRequest([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Model != "m" || !r.Stream || !r.WantsUsage || r.MaxTokens != 50 {
		t.Fatalf("parsed = %+v", r)
	}
}

func TestParseRequest_TakesSmallerOfBothTokenFields(t *testing.T) {
	r, err := parseRequest([]byte(`{"model":"m","messages":[{}],"max_tokens":80,"max_completion_tokens":30}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.MaxTokens != 30 {
		t.Fatalf("MaxTokens = %d, want 30", r.MaxTokens)
	}
}

func TestParseRequest_Rejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":         `not json`,
		"array":            `[]`,
		"no model":         `{"messages":[{}]}`,
		"no messages":      `{"model":"m"}`,
		"empty messages":   `{"model":"m","messages":[]}`,
		"stream not bool":  `{"model":"m","messages":[{}],"stream":"yes"}`,
		"max_tokens text":  `{"model":"m","messages":[{}],"max_tokens":"ten"}`,
		"max_tokens float": `{"model":"m","messages":[{}],"max_tokens":1.5}`,
		"max_tokens neg":   `{"model":"m","messages":[{}],"max_tokens":-1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRequest([]byte(body)); err == nil {
				t.Fatalf("accepted %s", body)
			}
		})
	}
}

func TestCapMaxTokens(t *testing.T) {
	cases := []struct {
		name string
		body string
		want map[string]float64 // field -> value expected in the rewritten body
		none string             // field that must stay absent
	}{
		{"missing gets cap", `{"model":"m","messages":[{}]}`, map[string]float64{"max_tokens": 100}, "max_completion_tokens"},
		{"oversized clamped", `{"model":"m","messages":[{}],"max_tokens":5000}`, map[string]float64{"max_tokens": 100}, "max_completion_tokens"},
		{"small kept", `{"model":"m","messages":[{}],"max_tokens":10}`, map[string]float64{"max_tokens": 10}, "max_completion_tokens"},
		{"completion field clamped", `{"model":"m","messages":[{}],"max_completion_tokens":5000}`, map[string]float64{"max_completion_tokens": 100}, "max_tokens"},
		{"both fields aligned", `{"model":"m","messages":[{}],"max_tokens":5000,"max_completion_tokens":40}`, map[string]float64{"max_tokens": 40, "max_completion_tokens": 40}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseRequest([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			r.capMaxTokens(100)
			var out map[string]any
			if err := json.Unmarshal(r.bodyFor("up"), &out); err != nil {
				t.Fatal(err)
			}
			for f, v := range tc.want {
				if out[f] != v {
					t.Errorf("%s = %v, want %v", f, out[f], v)
				}
			}
			if _, ok := out[tc.none]; tc.none != "" && ok {
				t.Errorf("%s unexpectedly present", tc.none)
			}
		})
	}
}

func TestBodyFor_RewritesModelAndRequestsStreamUsage(t *testing.T) {
	r, _ := parseRequest([]byte(`{"model":"public","messages":[{}],"stream":true,"stream_options":{"foo":1}}`))
	var out map[string]any
	_ = json.Unmarshal(r.bodyFor("upstream-0"), &out)
	if out["model"] != "upstream-0" {
		t.Errorf("model = %v", out["model"])
	}
	so := out["stream_options"].(map[string]any)
	if so["include_usage"] != true || so["foo"] != float64(1) {
		t.Errorf("stream_options = %v", so)
	}
	if r.body["model"] != "public" {
		t.Error("bodyFor mutated the original request")
	}
}

func TestPromptTokenBound(t *testing.T) {
	r, _ := parseRequest([]byte(helloBody))
	// {"content":"hi","role":"user"} is 30 bytes, plus 8 bytes of per-request overhead.
	if got := promptTokenBound(r.body); got != 38 {
		t.Fatalf("bound = %d, want 38", got)
	}
}

func TestWriteError_OpenAIShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 402, "budget_exceeded", "nope")
	var body struct {
		Error struct{ Message, Type, Code string } `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 402 || body.Error.Code != "budget_exceeded" || body.Error.Type != "invalid_request_error" || body.Error.Message != "nope" {
		t.Fatalf("got %d %+v", rec.Code, body)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal("missing JSON content type")
	}
}
