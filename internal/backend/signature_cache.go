// Package backend provides an HTTP client for communicating with the task
// backend via signed webhooks.
package backend

import (
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// signatureCacheKey is the map key used in SignatureCache. Combining the
// timestamp with the full hex signature (which already binds method/uri/body)
// gives a compact, collision-resistant key without additional hashing.
type signatureCacheKey struct {
	timestamp string
	signature string
}

// signatureCacheEntry stores the wall-clock time at which a signature was
// first successfully verified so the cache can evict stale entries lazily.
type signatureCacheEntry struct {
	seenAt time.Time
}

// SignatureCache is a bounded, in-memory cache of recently seen HMAC
// signatures used to detect replay attacks. Each entry is keyed by the
// (timestamp, signature) pair; because the signature already binds the
// method, URI, and body, no additional context is needed.
//
// Eviction is lazy: stale entries are pruned on every insert once the map
// grows beyond maxSignatureCacheSize. The retention window is
// protocol.DefaultMaxClockSkew*2 so every signature that could still pass
// the timestamp check is kept.
//
// Create instances with NewSignatureCache; the zero value is NOT valid.
type SignatureCache struct {
	mu      sync.Mutex
	entries map[signatureCacheKey]signatureCacheEntry
}

var _ protocol.ReplayCache = (*SignatureCache)(nil)

// maxSignatureCacheSize is the entry count threshold above which lazy
// eviction runs on the next insert. At one new entry per outbound backend
// call and a 5-minute retention window, normal load stays well below this.
const maxSignatureCacheSize = 1024

// NewSignatureCache returns an initialised, empty SignatureCache.
func NewSignatureCache() *SignatureCache {
	return &SignatureCache{
		entries: make(map[signatureCacheKey]signatureCacheEntry),
	}
}

// CheckAndInsert looks up (timestamp, signature) in the cache.
// Returns true (duplicate - reject) when the pair is already present.
// Returns false (first-seen - accept) and inserts the entry otherwise.
// Lazy eviction runs when the map exceeds maxSignatureCacheSize.
// It implements protocol.ReplayCache.
func (c *SignatureCache) CheckAndInsert(timestamp, signature string) bool {
	now := time.Now()
	key := signatureCacheKey{timestamp: timestamp, signature: signature}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		return true // duplicate
	}

	// Lazy eviction: prune entries older than the retention window.
	if len(c.entries) >= maxSignatureCacheSize {
		cutoff := now.Add(-(protocol.DefaultMaxClockSkew * 2))
		for k, v := range c.entries {
			if v.seenAt.Before(cutoff) {
				delete(c.entries, k)
			}
		}
	}

	c.entries[key] = signatureCacheEntry{seenAt: now}

	return false
}
