package runner

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSignPayload_Deterministic(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)

	sig1 := signPayload(key, body)
	sig2 := signPayload(key, body)
	assert.Equal(t, sig1, sig2)
	assert.NotEmpty(t, sig1)
}

func TestVerifySignature_Valid(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	sig := signPayload(key, body)

	assert.True(t, VerifySignature(key, sig, body))
}

func TestVerifySignature_WrongKey(t *testing.T) {
	body := []byte(`{"card_id":"TEST-001"}`)
	sig := signPayload("correct-key", body)

	assert.False(t, VerifySignature("wrong-key", sig, body))
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	sig := signPayload(key, body)

	tampered := []byte(`{"card_id":"TEST-002"}`)
	assert.False(t, VerifySignature(key, sig, tampered))
}

func TestVerifySignature_EmptyBody(t *testing.T) {
	key := "test-secret"
	body := []byte{}
	sig := signPayload(key, body)

	assert.True(t, VerifySignature(key, sig, body))
}

func TestVerifySignature_EmptyKey(t *testing.T) {
	body := []byte(`{"card_id":"TEST-001"}`)
	sig := signPayload("", body)

	// Empty key produces a valid HMAC — callers must guard against empty keys.
	assert.True(t, VerifySignature("", sig, body))
	assert.False(t, VerifySignature("any-key", sig, body))
}

func TestSignPayloadWithTimestamp_Deterministic(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := "1700000000"

	sig1 := signPayloadWithTimestamp(key, body, ts)
	sig2 := signPayloadWithTimestamp(key, body, ts)
	assert.Equal(t, sig1, sig2)
	assert.NotEmpty(t, sig1)
}

func TestSignPayloadWithTimestamp_DifferentFromPlain(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := "1700000000"

	plainSig := signPayload(key, body)
	tsSig := signPayloadWithTimestamp(key, body, ts)
	assert.NotEqual(t, plainSig, tsSig)
}

func TestVerifySignatureWithTimestamp_Valid(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, body, ts)

	assert.True(t, VerifySignatureWithTimestamp(key, sig, ts, body, DefaultMaxClockSkew))
}

func TestVerifySignatureWithTimestamp_Expired(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	// Timestamp from 10 minutes ago (outside 5-minute window).
	ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := signPayloadWithTimestamp(key, body, ts)

	assert.False(t, VerifySignatureWithTimestamp(key, sig, ts, body, DefaultMaxClockSkew))
}

func TestVerifySignatureWithTimestamp_FutureTimestamp(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	// Timestamp 10 minutes in the future (outside 5-minute window).
	ts := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	sig := signPayloadWithTimestamp(key, body, ts)

	assert.False(t, VerifySignatureWithTimestamp(key, sig, ts, body, DefaultMaxClockSkew))
}

func TestVerifySignatureWithTimestamp_InvalidTimestamp(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)

	assert.False(t, VerifySignatureWithTimestamp(key, "sig", "not-a-number", body, DefaultMaxClockSkew))
}

func TestVerifySignatureWithTimestamp_WrongKey(t *testing.T) {
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp("correct-key", body, ts)

	assert.False(t, VerifySignatureWithTimestamp("wrong-key", sig, ts, body, DefaultMaxClockSkew))
}

func TestVerifySignatureWithTimestamp_TamperedBody(t *testing.T) {
	key := "test-secret"
	body := []byte(`{"card_id":"TEST-001"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, body, ts)

	tampered := []byte(`{"card_id":"TEST-002"}`)
	assert.False(t, VerifySignatureWithTimestamp(key, sig, ts, tampered, DefaultMaxClockSkew))
}
