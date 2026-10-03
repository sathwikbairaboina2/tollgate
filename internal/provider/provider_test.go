package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sathwikbairaboina2/tollgate/internal/config"
)

func TestDo_PostsToChatCompletionsWithAuth(t *testing.T) {
	var gotPath, gotAuth, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotCT = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New(config.Provider{Name: "x", BaseURL: srv.URL + "/v1/", APIKey: "sk-test", Timeout: config.Duration{Duration: time.Second}})
	resp, err := p.Do(context.Background(), []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotCT != "application/json" || gotBody != `{"a":1}` {
		t.Errorf("content-type %q body %q", gotCT, gotBody)
	}
}

func TestDo_OmitsAuthWithoutKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()
	resp, err := New(config.Provider{Name: "ollama", BaseURL: srv.URL}).Do(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "" {
		t.Errorf("auth header sent without key: %q", gotAuth)
	}
}

func TestDo_ResponseHeaderTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	p := New(config.Provider{Name: "slow", BaseURL: srv.URL, Timeout: config.Duration{Duration: 50 * time.Millisecond}})
	if _, err := p.Do(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected header timeout error")
	}
}
