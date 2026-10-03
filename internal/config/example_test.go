package config

import (
	"os"
	"testing"
)

func TestExampleConfigParses(t *testing.T) {
	data, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data, func(k string) string { return "set-" + k }); err != nil {
		t.Fatalf("config.example.yaml is invalid: %v", err)
	}
}

func TestComposeConfigParses(t *testing.T) {
	data, err := os.ReadFile("../../deploy/compose.tollgate.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data, func(k string) string { return "set-" + k }); err != nil {
		t.Fatalf("deploy/compose.tollgate.yaml is invalid: %v", err)
	}
}

func TestComposeConfigOllamaRoute(t *testing.T) {
	data, err := os.ReadFile("../../deploy/compose.tollgate.yaml")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"TOLLGATE_DEMO_KEY": "k1", "TOLLGATE_TINY_KEY": "k2"}
	cfg, err := Parse(data, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("deploy/compose.tollgate.yaml is invalid: %v", err)
	}
	var found bool
	for _, r := range cfg.Routes {
		if r.Model != "local-ollama" {
			continue
		}
		found = true
		if len(r.Targets) < 1 || r.Targets[0].Provider != "ollama" || r.Targets[0].Model != "gemma4:12b" {
			t.Fatalf("local-ollama first target = %+v, want ollama/gemma4:12b", r.Targets)
		}
	}
	if !found {
		t.Fatal("route local-ollama missing")
	}
	if _, ok := cfg.Pricing["gemma4:12b"]; !ok {
		t.Fatal("pricing for gemma4:12b missing")
	}
}
