package runner

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
)

const (
	testMethodPOST = http.MethodPost
	testPath       = "/kill"
)

// TestProtocolSignatureMatchesHistoricalVector pins the protocol module's
// signer to the byte-exact output of the deleted in-repo implementation.
func TestProtocolSignatureMatchesHistoricalVector(t *testing.T) {
	got := protocol.SignPayloadWithTimestamp("test-key", "POST", "/webhook/trigger?x=1",
		[]byte(`{"card_id":"CM-001"}`), "1765432100")

	want := "804755e4c2979a938007b977be571a1193a0b1e5b6e0a9daef5b91374a938013"
	if got != want {
		t.Errorf("protocol signer drifted from the historical wire format:\n got %s\nwant %s", got, want)
	}
}

func TestVerifySignatureWithTimestamp_ReplayRejected(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := protocol.SignPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	cache := NewSignatureCache()

	// First verification: must succeed and insert into the cache.
	assert.True(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, cache))

	// Second verification with the same (ts, sig): must be rejected as replay.
	assert.False(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, cache))
}

func TestVerifySignatureWithTimestamp_NilCacheNoReplay(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := protocol.SignPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	// With a nil cache, the same signature is accepted repeatedly (no replay tracking).
	assert.True(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, nil))
	assert.True(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, nil))
}

func TestSignatureCache_IndependentInstances(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := protocol.SignPayloadWithTimestamp(key, testMethodPOST, testPath, body, ts)

	cache1 := NewSignatureCache()
	cache2 := NewSignatureCache()

	// Inserting into cache1 must not block cache2.
	assert.True(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, cache1))
	assert.True(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, cache2))

	// Each cache now has the entry; subsequent calls on the same instance reject.
	assert.False(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, cache1))
	assert.False(t, protocol.VerifySignatureWithTimestamp(key, testMethodPOST, testPath, sig, ts, body, protocol.DefaultMaxClockSkew, cache2))
}

func TestSignatureCache_CheckAndInsert(t *testing.T) {
	cache := NewSignatureCache()

	// First-seen: accepted (false) and inserted.
	assert.False(t, cache.CheckAndInsert("1700000000", "sig-a"))
	// Duplicate: rejected (true).
	assert.True(t, cache.CheckAndInsert("1700000000", "sig-a"))
	// Same signature under a different timestamp is a distinct key.
	assert.False(t, cache.CheckAndInsert("1700000001", "sig-a"))
	// Different signature under the original timestamp is a distinct key.
	assert.False(t, cache.CheckAndInsert("1700000000", "sig-b"))
}
