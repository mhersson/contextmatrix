package runner

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	testMethodPOST = http.MethodPost
	testPath       = "/kill"
)

func TestSignPayloadWithTimestamp_Deterministic(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := "1700000000"

	sig1 := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)
	sig2 := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)
	assert.Equal(t, sig1, sig2)
	assert.NotEmpty(t, sig1)
}

// TestSignPayloadWithTimestamp_DifferentPath is the regression guard for the
// /end-session ↔ /kill replay-cache collision: identical body + ts + method
// signed under two different paths MUST produce distinct signatures, or the
// runner's replay cache will reject the second call.
func TestSignPayloadWithTimestamp_DifferentPath(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001","project":"p"}`)
	ts := "1700000000"

	sigEndSession := signPayloadWithTimestamp(key, testMethodPOST, "/end-session", body, ts)
	sigKill := signPayloadWithTimestamp(key, testMethodPOST, "/kill", body, ts)
	assert.NotEqual(t, sigEndSession, sigKill)
}

func TestSignPayloadWithTimestamp_DifferentMethod(t *testing.T) {
	key := "test-secret"
	body := []byte{}
	ts := "1700000000"

	sigGet := signPayloadWithTimestamp(key, http.MethodGet, "/containers", body, ts)
	sigPost := signPayloadWithTimestamp(key, http.MethodPost, "/containers", body, ts)
	assert.NotEqual(t, sigGet, sigPost)
}

func TestVerifySignatureWithTimestamp_Valid(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	assert.True(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_Expired(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_FutureTimestamp(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_InvalidTimestamp(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)

	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, "sig", "not-a-number", body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_WrongKey(t *testing.T) {
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp("correct-key", testMethodPOST, testPath, body, ts)

	assert.False(t, VerifySignatureWithTimestamp("wrong-key", testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_TamperedBody(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	tampered := []byte(`{"card_id":"TEST-002"}`)
	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, tampered, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_TamperedPath(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, "/end-session", body, ts)

	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, "/kill", sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_TamperedMethod(t *testing.T) {
	key := "test-secret"
	body := []byte{}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, http.MethodGet, "/containers", body, ts)

	assert.False(t, VerifySignatureWithTimestamp(key, http.MethodPost, "/containers", sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestVerifySignatureWithTimestamp_ReplayRejected(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	cache := NewSignatureCache()

	// First verification: must succeed and insert into the cache.
	assert.True(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, cache))

	// Second verification with the same (ts, sig): must be rejected as replay.
	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, cache))
}

func TestVerifySignatureWithTimestamp_NilCacheNoReplay(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	// With a nil cache, the same signature is accepted repeatedly (no replay tracking).
	assert.True(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, nil))
	assert.True(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, nil))
}

func TestSignatureCache_IndependentInstances(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	cache1 := NewSignatureCache()
	cache2 := NewSignatureCache()

	// Inserting into cache1 must not block cache2.
	assert.True(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, cache1))
	assert.True(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, cache2))

	// Each cache now has the entry; subsequent calls on the same instance reject.
	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, cache1))
	assert.False(t, VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, DefaultMaxClockSkew, cache2))
}
