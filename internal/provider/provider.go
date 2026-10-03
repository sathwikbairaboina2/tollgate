// Package provider talks to OpenAI-compatible chat-completions upstreams.
package provider

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/sathwikbairaboina2/tollgate/internal/config"
)

// Provider is one upstream. Safe for concurrent use.
type Provider struct {
	Name     string
	endpoint string
	apiKey   string
	client   *http.Client
}

// New builds a provider. Timeout bounds the wait for response headers, not the whole body,
// so long streams are not cut off.
func New(cfg config.Provider) *Provider {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = cfg.Timeout.Duration
	tr.MaxIdleConnsPerHost = 64
	return &Provider{
		Name:     cfg.Name,
		endpoint: strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions",
		apiKey:   cfg.APIKey,
		client:   &http.Client{Transport: tr},
	}
}

// Do POSTs body to the upstream's chat-completions endpoint. The caller closes the response body.
func (p *Provider) Do(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return p.client.Do(req)
}
