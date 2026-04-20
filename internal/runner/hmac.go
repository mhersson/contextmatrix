// Package runner provides an HTTP client for communicating with the
// contextmatrix-runner via signed webhooks.
package runner

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

const (
	// DefaultMaxClockSkew is the maximum allowed age for webhook timestamps.
	// Payloads older than this are rejected to prevent replay attacks.
	DefaultMaxClockSkew = 5 * time.Minute

	// timestampHeader carries the Unix timestamp used in HMAC computation.
	timestampHeader = "X-Webhook-Timestamp"
)

// signPayload computes an HMAC-SHA256 signature over body using key.
// Returns the hex-encoded signature string.
func signPayload(key string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// signPayloadWithTimestamp computes an HMAC-SHA256 signature over "timestamp.body"
// to bind the timestamp to the payload and prevent replay attacks.
func signPayloadWithTimestamp(key string, body []byte, ts string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// SignRequestHeaders computes HMAC-SHA256 auth headers for an outbound request.
// It signs "timestamp.body" and returns the X-Signature-256 and X-Webhook-Timestamp
// header values to be set on the request. Use an empty body for GET requests.
func SignRequestHeaders(key string, body []byte) (sigHeader, tsHeader string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, body, ts)

	return "sha256=" + sig, ts
}

// VerifySignature checks that signature matches the HMAC-SHA256 of body using key.
// The signature should be hex-encoded (as produced by signPayload).
func VerifySignature(key, signature string, body []byte) bool {
	expected := signPayload(key, body)

	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifySignatureWithTimestamp checks the HMAC-SHA256 signature and rejects
// payloads with timestamps outside the allowed clock-skew window.
func VerifySignatureWithTimestamp(key, signature, timestamp string, body []byte, maxSkew time.Duration) bool {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	age := time.Since(time.Unix(ts, 0))
	if age < -maxSkew || age > maxSkew {
		return false
	}

	expected := signPayloadWithTimestamp(key, body, timestamp)

	return hmac.Equal([]byte(expected), []byte(signature))
}
