// Package cache holds the response cache interface and its exact-match LRU implementation.
package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Cache stores response bodies by key. A semantic cache can implement the same interface.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, val []byte)
}

// Key hashes the scope (the virtual key id), the route and the request body, ignoring fields that
// do not change the answer. The scope keeps one tenant from reading, or probing for, another
// tenant's cached responses. encoding/json sorts map keys, so field order does not matter.
func Key(scope, route string, body map[string]any) string {
	c := make(map[string]any, len(body))
	for k, v := range body {
		switch k {
		case "stream", "stream_options", "user":
			continue
		}
		c[k] = v
	}
	b, _ := json.Marshal(c)
	h := sha256.New()
	h.Write([]byte(scope))
	h.Write([]byte{0})
	h.Write([]byte(route))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

type entry struct {
	key string
	val []byte
	exp time.Time
}

// LRU is a size-bounded, TTL-expiring cache. Safe for concurrent use.
type LRU struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	now   func() time.Time
	ll    *list.List
	items map[string]*list.Element
}

// NewLRU returns an empty cache holding at most maxEntries for ttl each.
func NewLRU(maxEntries int, ttl time.Duration, now func() time.Time) *LRU {
	if maxEntries < 1 {
		maxEntries = 1
	}
	if now == nil {
		now = time.Now
	}
	return &LRU{max: maxEntries, ttl: ttl, now: now, ll: list.New(), items: map[string]*list.Element{}}
}

// Get returns a live entry and marks it most recently used.
func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if !c.now().Before(e.exp) {
		c.ll.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return e.val, true
}

// Set stores val, evicting the least recently used entries beyond capacity.
func (c *LRU) Set(key string, val []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp := c.now().Add(c.ttl)
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		e.val, e.exp = val, exp
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&entry{key: key, val: val, exp: exp})
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.items, last.Value.(*entry).key)
	}
}

// Len returns the number of stored entries, including any not yet found to be expired.
func (c *LRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
