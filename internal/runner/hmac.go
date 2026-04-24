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

// signPayloadWithTimestamp computes an HMAC-SHA256 signature bound to the
// HTTP method, request path, timestamp, and body. The signed content is:
//
//	method + "\n" + path + "\n" + timestamp + "." + body
//
// Including method and path prevents a valid signature for one endpoint from
// being replayed against another endpoint with an identical body — critical
// because /kill, /end-session, and /promote all carry the same
// {card_id, project} payload shape and would otherwise produce colliding
// signatures when issued back-to-back in the same Unix second.
func signPayloadWithTimestamp(key, method, path string, body []byte, ts string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// SignRequestHeaders computes HMAC-SHA256 auth headers for an outbound request
// to the given method + path. It signs the method/path/timestamp/body tuple
// and returns the X-Signature-256 and X-Webhook-Timestamp header values to be
// set on the request. Use an empty body for GET requests.
func SignRequestHeaders(key, method, path string, body []byte) (sigHeader, tsHeader string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayloadWithTimestamp(key, method, path, body, ts)

	return "sha256=" + sig, ts
}

// VerifySignatureWithTimestamp checks the HMAC-SHA256 signature against the
// expected value computed over method/path/timestamp/body, and rejects
// payloads with timestamps outside the allowed clock-skew window.
func VerifySignatureWithTimestamp(key, method, path, signature, timestamp string, body []byte, maxSkew time.Duration) bool {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	age := time.Since(time.Unix(ts, 0))
	if age < -maxSkew || age > maxSkew {
		return false
	}

	expected := signPayloadWithTimestamp(key, method, path, body, timestamp)

	return hmac.Equal([]byte(expected), []byte(signature))
}
