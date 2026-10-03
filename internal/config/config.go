// Package config loads and validates Tollgate's YAML configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultListen          = ":8787"
	DefaultTimeout         = 60 * time.Second
	DefaultMaxOutputTokens = 4096
	DefaultCacheEntries    = 10000
	DefaultCacheTTL        = 10 * time.Minute
)

// Duration is a time.Duration that unmarshals from strings such as "60s".
type Duration struct{ time.Duration }

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = v
	return nil
}

// Config is the whole gateway configuration.
type Config struct {
	Listen    string           `yaml:"listen"`
	Providers []Provider       `yaml:"providers"`
	Routes    []Route          `yaml:"routes"`
	Pricing   map[string]Price `yaml:"pricing"`
	Keys      []Key            `yaml:"keys"`
	Cache     Cache            `yaml:"cache"`
}

// Provider is an OpenAI-compatible upstream.
type Provider struct {
	Name      string   `yaml:"name"`
	BaseURL   string   `yaml:"base_url"`
	APIKeyEnv string   `yaml:"api_key_env"`
	Timeout   Duration `yaml:"timeout"`
	APIKey    string   `yaml:"-"` // resolved from APIKeyEnv at load time
}

// Route maps a public model name to an ordered list of upstream targets.
type Route struct {
	Model   string   `yaml:"model"`
	Targets []Target `yaml:"targets"`
}

// Target is one provider/model pair in a route.
type Target struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// Price is USD per one million tokens.
type Price struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

// Key is a virtual API key with its limits.
type Key struct {
	ID              string    `yaml:"id"`
	Key             string    `yaml:"key"`
	KeyEnv          string    `yaml:"key_env"`
	Budget          Budget    `yaml:"budget"`
	RateLimit       RateLimit `yaml:"rate_limit"`
	MaxOutputTokens int       `yaml:"max_output_tokens"`
}

// Budget caps lifetime spend for a key. Zero means unlimited.
type Budget struct {
	USD    float64 `yaml:"usd"`
	Tokens int64   `yaml:"tokens"`
}

// RateLimit is a token bucket. Zero RequestsPerSecond means unlimited.
type RateLimit struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

// Cache configures the exact-match response cache.
type Cache struct {
	Enabled    bool     `yaml:"enabled"`
	MaxEntries int      `yaml:"max_entries"`
	TTL        Duration `yaml:"ttl"`
}

// Load reads and parses the config file at path, resolving secrets from the environment.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data, os.Getenv)
}

// Parse decodes, defaults, resolves and validates a config document.
func Parse(data []byte, getenv func(string) string) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.resolveSecrets(getenv); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	for i := range c.Providers {
		if c.Providers[i].Timeout.Duration == 0 {
			c.Providers[i].Timeout.Duration = DefaultTimeout
		}
	}
	for i := range c.Keys {
		if c.Keys[i].MaxOutputTokens == 0 {
			c.Keys[i].MaxOutputTokens = DefaultMaxOutputTokens
		}
	}
	if c.Cache.MaxEntries == 0 {
		c.Cache.MaxEntries = DefaultCacheEntries
	}
	if c.Cache.TTL.Duration == 0 {
		c.Cache.TTL.Duration = DefaultCacheTTL
	}
}

func (c *Config) resolveSecrets(getenv func(string) string) error {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.APIKeyEnv == "" {
			continue
		}
		if p.APIKey = getenv(p.APIKeyEnv); p.APIKey == "" {
			return fmt.Errorf("provider %q: env var %s is empty", p.Name, p.APIKeyEnv)
		}
	}
	for i := range c.Keys {
		k := &c.Keys[i]
		if k.KeyEnv == "" {
			continue
		}
		if k.Key != "" {
			return fmt.Errorf("key %q: set key or key_env, not both", k.ID)
		}
		if k.Key = getenv(k.KeyEnv); k.Key == "" {
			return fmt.Errorf("key %q: env var %s is empty", k.ID, k.KeyEnv)
		}
	}
	return nil
}

func (c *Config) validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	providers := map[string]bool{}
	for _, p := range c.Providers {
		switch {
		case p.Name == "":
			add("provider: name is required")
		case providers[p.Name]:
			add("provider %q: duplicate name", p.Name)
		}
		providers[p.Name] = true
		if p.BaseURL == "" {
			add("provider %q: base_url is required", p.Name)
		}
	}

	usdBudget := false
	ids, values := map[string]bool{}, map[string]bool{}
	for _, k := range c.Keys {
		if k.ID == "" {
			add("key: id is required")
		} else if ids[k.ID] {
			add("key %q: duplicate id", k.ID)
		}
		ids[k.ID] = true
		if k.Key == "" {
			add("key %q: key or key_env is required", k.ID)
		} else if values[k.Key] {
			add("key %q: duplicate key value", k.ID)
		}
		values[k.Key] = true
		if k.Budget.USD < 0 || k.Budget.Tokens < 0 {
			add("key %q: budget must not be negative", k.ID)
		}
		if k.RateLimit.RequestsPerSecond < 0 || k.RateLimit.Burst < 0 {
			add("key %q: rate_limit must not be negative", k.ID)
		}
		if k.MaxOutputTokens < 0 {
			add("key %q: max_output_tokens must not be negative", k.ID)
		}
		if k.Budget.USD > 0 {
			usdBudget = true
		}
	}

	models := map[string]bool{}
	for _, r := range c.Routes {
		if r.Model == "" {
			add("route: model is required")
		} else if models[r.Model] {
			add("route %q: duplicate model", r.Model)
		}
		models[r.Model] = true
		if len(r.Targets) == 0 {
			add("route %q: at least one target is required", r.Model)
		}
		for _, t := range r.Targets {
			if !providers[t.Provider] {
				add("route %q: unknown provider %q", r.Model, t.Provider)
			}
			if t.Model == "" {
				add("route %q: target model is required", r.Model)
			}
			if _, priced := c.Pricing[t.Model]; usdBudget && !priced {
				add("route %q: model %q has no pricing but a key has a USD budget", r.Model, t.Model)
			}
		}
	}

	if len(c.Keys) == 0 {
		errs = append(errs, errors.New("at least one key is required"))
	}
	if len(c.Routes) == 0 {
		errs = append(errs, errors.New("at least one route is required"))
	}
	return errors.Join(errs...)
}
