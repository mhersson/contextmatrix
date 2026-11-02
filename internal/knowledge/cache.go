package knowledge

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/clock"
)

// itemKey is order-insensitive in ids and does not modify the caller's slice.
func itemKey(provider, scope string, ids []string, related bool) string {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)

	parts := append([]string{provider, scope, strconv.FormatBool(related)}, sorted...)

	return strings.Join(parts, "\x00")
}

type cacheEntry struct {
	answer  Answer
	raw     json.RawMessage
	expires time.Time
}

type itemCache struct {
	clk  clock.Clock
	ttl  time.Duration
	size int

	mu      sync.Mutex
	entries map[string]cacheEntry
}

func newItemCache(clk clock.Clock, ttl time.Duration, size int) *itemCache {
	return &itemCache{clk: clk, ttl: ttl, size: size, entries: make(map[string]cacheEntry)}
}

// get returns a shallow copy; callers may add or drop top-level fields.
func (c *itemCache) get(key string) (Answer, json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dropExpired()

	e, ok := c.entries[key]
	if !ok {
		return nil, nil, false
	}

	return maps.Clone(e.answer), e.raw, true
}

func (c *itemCache) put(key string, a Answer, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dropExpired()

	if _, exists := c.entries[key]; !exists {
		for len(c.entries) >= c.size && len(c.entries) > 0 {
			c.evictEarliest()
		}
	}

	c.entries[key] = cacheEntry{answer: maps.Clone(a), raw: raw, expires: c.clk.Now().Add(c.ttl)}
}

func (c *itemCache) dropExpired() {
	now := c.clk.Now()
	for k, e := range c.entries {
		if !e.expires.After(now) {
			delete(c.entries, k)
		}
	}
}

func (c *itemCache) evictEarliest() {
	var (
		oldestKey string
		oldest    time.Time
		found     bool
	)

	for k, e := range c.entries {
		if !found || e.expires.Before(oldest) {
			oldestKey, oldest, found = k, e.expires, true
		}
	}

	delete(c.entries, oldestKey)
}
