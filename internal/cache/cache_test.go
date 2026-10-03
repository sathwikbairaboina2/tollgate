package cache

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestLRU_GetSet(t *testing.T) {
	c := NewLRU(10, time.Minute, (&clock{t: time.Unix(0, 0)}).now)
	if _, ok := c.Get("a"); ok {
		t.Fatal("empty cache hit")
	}
	c.Set("a", []byte("1"))
	if v, ok := c.Get("a"); !ok || string(v) != "1" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
}

func TestLRU_ExpiresAfterTTL(t *testing.T) {
	clk := &clock{t: time.Unix(0, 0)}
	c := NewLRU(10, time.Minute, clk.now)
	c.Set("a", []byte("1"))
	clk.t = clk.t.Add(time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("entry served at TTL")
	}
	if c.Len() != 0 {
		t.Fatalf("expired entry not evicted, len = %d", c.Len())
	}
}

func TestLRU_EvictsLeastRecentlyUsed(t *testing.T) {
	c := NewLRU(2, time.Minute, (&clock{t: time.Unix(0, 0)}).now)
	c.Set("a", []byte("1"))
	c.Set("b", []byte("2"))
	c.Get("a") // a is now most recent
	c.Set("c", []byte("3"))
	if _, ok := c.Get("b"); ok {
		t.Fatal("least recently used entry b survived")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("recently used entry a evicted")
	}
}

func TestKey_IgnoresVolatileFieldsAndOrder(t *testing.T) {
	a := map[string]any{"model": "m", "messages": []any{"x"}, "user": "u1", "stream": false}
	b := map[string]any{"stream": true, "messages": []any{"x"}, "model": "m", "user": "u2", "stream_options": map[string]any{"include_usage": true}}
	if Key("r", a) != Key("r", b) {
		t.Fatal("keys differ for requests that differ only in user/stream fields")
	}
}

func TestKey_DistinguishesRouteAndContent(t *testing.T) {
	a := map[string]any{"model": "m", "messages": []any{"x"}}
	b := map[string]any{"model": "m", "messages": []any{"y"}}
	if Key("r", a) == Key("r", b) {
		t.Fatal("different messages share a key")
	}
	if Key("r1", a) == Key("r2", a) {
		t.Fatal("different routes share a key")
	}
}
