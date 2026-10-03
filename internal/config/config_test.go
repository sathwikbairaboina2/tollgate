package config

import (
	"strings"
	"testing"
	"time"
)

const valid = `
providers:
  - name: openai
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  - name: ollama
    base_url: http://localhost:11434/v1
    timeout: 120s
routes:
  - model: chat-default
    targets:
      - { provider: openai, model: gpt-4o-mini }
      - { provider: ollama, model: llama3.2 }
pricing:
  gpt-4o-mini: { input: 0.15, output: 0.60 }
  llama3.2: { input: 0, output: 0 }
keys:
  - id: team-a
    key_env: TEAM_A_KEY
    budget: { usd: 5, tokens: 1000 }
    rate_limit: { requests_per_second: 2, burst: 4 }
    max_output_tokens: 256
cache:
  enabled: true
`

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var goodEnv = env(map[string]string{"OPENAI_API_KEY": "sk-test", "TEAM_A_KEY": "tg-a"})

func TestParse_Valid(t *testing.T) {
	c, err := Parse([]byte(valid), goodEnv)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Providers[0].APIKey != "sk-test" {
		t.Errorf("provider key not resolved from env: %q", c.Providers[0].APIKey)
	}
	if c.Keys[0].Key != "tg-a" {
		t.Errorf("virtual key not resolved from env: %q", c.Keys[0].Key)
	}
	if c.Providers[1].Timeout.Duration != 120*time.Second {
		t.Errorf("timeout = %v", c.Providers[1].Timeout.Duration)
	}
	if got := c.Routes[0].Targets[1]; got.Provider != "ollama" || got.Model != "llama3.2" {
		t.Errorf("target = %+v", got)
	}
	if c.Keys[0].Budget.USD != 5 || c.Keys[0].Budget.Tokens != 1000 || c.Keys[0].RateLimit.Burst != 4 {
		t.Errorf("key limits = %+v", c.Keys[0])
	}
}

func TestParse_Defaults(t *testing.T) {
	c, err := Parse([]byte(valid), goodEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != DefaultListen {
		t.Errorf("listen = %q", c.Listen)
	}
	if c.Providers[0].Timeout.Duration != DefaultTimeout {
		t.Errorf("default timeout = %v", c.Providers[0].Timeout.Duration)
	}
	if c.Cache.MaxEntries != DefaultCacheEntries || c.Cache.TTL.Duration != DefaultCacheTTL {
		t.Errorf("cache defaults = %+v", c.Cache)
	}
	noCap := strings.Replace(valid, "    max_output_tokens: 256\n", "", 1)
	c, err = Parse([]byte(noCap), goodEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Keys[0].MaxOutputTokens != DefaultMaxOutputTokens {
		t.Errorf("default max_output_tokens = %d", c.Keys[0].MaxOutputTokens)
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]struct {
		yaml    string
		env     func(string) string
		wantErr string
	}{
		"unknown field":                  {strings.Replace(valid, "cache:", "cahce:", 1), goodEnv, "cahce"},
		"missing provider env":           {valid, env(map[string]string{"TEAM_A_KEY": "tg-a"}), "OPENAI_API_KEY"},
		"missing key env":                {valid, env(map[string]string{"OPENAI_API_KEY": "x"}), "TEAM_A_KEY"},
		"unknown provider in route":      {strings.Replace(valid, "{ provider: ollama,", "{ provider: nope,", 1), goodEnv, `unknown provider "nope"`},
		"unpriced model with usd budget": {strings.Replace(valid, "  llama3.2: { input: 0, output: 0 }\n", "", 1), goodEnv, `"llama3.2" has no pricing`},
		"duplicate key id":               {strings.Replace(valid, "cache:\n", "  - id: team-a\n    key: other\ncache:\n", 1), goodEnv, `duplicate id`},
		"duplicate key value":            {strings.Replace(valid, "cache:\n", "  - id: team-b\n    key: tg-a\ncache:\n", 1), goodEnv, `duplicate key value`},
		"negative budget":                {strings.Replace(valid, "usd: 5", "usd: -1", 1), goodEnv, "negative"},
		"bad duration":                   {strings.Replace(valid, "timeout: 120s", "timeout: soon", 1), goodEnv, "invalid duration"},
		"route without targets":          {strings.Replace(valid, "    targets:\n      - { provider: openai, model: gpt-4o-mini }\n      - { provider: ollama, model: llama3.2 }\n", "    targets: []\n", 1), goodEnv, "at least one target"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml), tc.env)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}
