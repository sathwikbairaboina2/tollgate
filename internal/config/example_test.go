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
