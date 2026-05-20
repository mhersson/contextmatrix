// Package runner provides an HTTP client for communicating with the
// contextmatrix-runner via signed webhooks.
package runner

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"
)

const (
	// DefaultMaxClockSkew is the maximum allowed age for webhook timestamps.
	// Payloads older than this are rejected to prevent replay attacks.
	DefaultMaxClockSkew = 5 * time.Minute

	// DefaultMaxFutureSkew is the maximum allowed future drift for webhook
	// timestamps. Tighter than the past window so a compromised signature
	// cannot be pre-issued for long before it is replayed.
	DefaultMaxFutureSkew = 30 * time.Second

	// timestampHeader carries the Unix timestamp used in HMAC computation.
	timestampHeader = "X-Webhook-Timestamp"
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
// DefaultMaxClockSkew*2 so every signature that could still pass the
// timestamp check is kept.
//
// Create instances with NewSignatureCache; the zero value is NOT valid.
type SignatureCache struct {
	mu      sync.Mutex
	entries map[signatureCacheKey]signatureCacheEntry
}

// maxSignatureCacheSize is the entry count threshold above which lazy
// eviction runs on the next insert. At one new entry per outbound runner
// call and a 5-minute retention window, normal load stays well below this.
const maxSignatureCacheSize = 1024

// NewSignatureCache returns an initialised, empty SignatureCache.
func NewSignatureCache() *SignatureCache {
	return &SignatureCache{
		entries: make(map[signatureCacheKey]signatureCacheEntry),
	}
}

// checkAndInsert looks up (timestamp, signature) in the cache.
// Returns true (duplicate — reject) when the pair is already present.
// Returns false (first-seen — accept) and inserts the entry otherwise.
// Lazy eviction runs when the map exceeds maxSignatureCacheSize.
func (c *SignatureCache) checkAndInsert(timestamp, signature string) bool {
	now := time.Now()
	key := signatureCacheKey{timestamp: timestamp, signature: signature}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		return true // duplicate
	}

	// Lazy eviction: prune entries older than the retention window.
	if len(c.entries) >= maxSignatureCacheSize {
		cutoff := now.Add(-(DefaultMaxClockSkew * 2))
		for k, v := range c.entries {
			if v.seenAt.Before(cutoff) {
				delete(c.entries, k)
			}
		}
	}

	c.entries[key] = signatureCacheEntry{seenAt: now}

	return false
}

// signPayloadWithTimestamp computes an HMAC-SHA256 signature bound to the
// HTTP method, request URI, timestamp, and body. The signed content is:
//
//	method + "\n" + uri + "\n" + timestamp + "." + body
//
// uri is the request-target form (path + "?" + raw query, or just path when
// no query is present) — the same value `r.URL.RequestURI()` returns on the
// receiving side.
//
// Including method and URI prevents a valid signature for one endpoint from
// being replayed against another endpoint with an identical body. Binding
// the query string also prevents two concurrent requests to the same path
// (e.g. GET /logs?project=A vs GET /logs?project=B) from producing
// identical signatures and colliding in the receiver's replay cache when
// issued in the same Unix second.
func signPayloadWithTimestamp(key, method, uri string, body []byte, ts string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(uri))
	mac.Write([]byte("\n"))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// SignRequestHeaders computes HMAC-SHA256 auth headers for an outbound request
// to the given method + URI (path + raw query). It signs the
// method/uri/timestamp/body tuple and returns the X-Signature-256 and
// X-Webhook-Timestamp header values to be set on the request. Use an empty
// body for GET requests.
func SignRequestHeaders(key, method, uri string, body []byte) (sigHeader, tsHeader string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, method, uri, body, ts)

	return "sha256=" + sig, ts
}

// VerifySignatureWithTimestamp checks the HMAC-SHA256 signature against the
// expected value computed over method/uri/timestamp/body, and rejects
// payloads with timestamps outside the allowed clock-skew window. uri must
// be the request-target form (`r.URL.RequestURI()`).
//
// The skew window is asymmetric: past timestamps up to maxSkew are accepted;
// future timestamps up to DefaultMaxFutureSkew are accepted. This limits
// the window during which a pre-issued signature could be held and replayed.
//
// If cache is non-nil, successfully verified signatures are checked against
// it and rejected if already present (replay protection). Duplicate
// (timestamp, signature) pairs return false even when the HMAC is valid.
func VerifySignatureWithTimestamp(key, method, uri, signature, timestamp string, body []byte, maxSkew time.Duration, cache *SignatureCache) bool {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	age := time.Since(time.Unix(ts, 0))
	if age < -DefaultMaxFutureSkew || age > maxSkew {
		return false
	}

	expected := signPayloadWithTimestamp(key, method, uri, body, timestamp)

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return false
	}

	if cache != nil && cache.checkAndInsert(timestamp, signature) {
		return false // replay detected
	}

	return true
}
