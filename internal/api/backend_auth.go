package api

import (
	"io"
	"net/http"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// extractBackendSignature performs shared header validation for backend HMAC
// authentication (task backend or chat backend). It checks that an API key is
// configured and that the X-Signature-256 / X-Webhook-Timestamp headers are
// present and well-formed, returning the trimmed signature hex and timestamp on
// success. On failure it writes the 403 response and returns ok=false.
//
// Body reading and the actual HMAC verification are left to the caller so GET
// (no body) and POST (body in the signed payload) handlers can share this
// prefix without duplicating the differing tails.
func extractBackendSignature(w http.ResponseWriter, r *http.Request, apiKey string) (sig, ts string, ok bool) {
	if apiKey == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "backend authentication not configured", "")

		return "", "", false
	}

	sigHeader := r.Header.Get(protocol.SignatureHeader)
	if sigHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Signature-256 header", "")

		return "", "", false
	}

	tsHeader := r.Header.Get(protocol.TimestampHeader)
	if tsHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Webhook-Timestamp header", "")

		return "", "", false
	}

	if !strings.HasPrefix(sigHeader, "sha256=") {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "malformed X-Signature-256 header: missing sha256= prefix", "")

		return "", "", false
	}

	return strings.TrimPrefix(sigHeader, "sha256="), tsHeader, true
}

// authenticateBackendGet verifies an HMAC-SHA256 signature over method + uri
// with an empty body on a backend-originated GET, using the supplied key and
// replay cache. Shared by the task-backend and chat callback handlers. Returns
// true on success; on failure it writes the 403 response and returns false.
func authenticateBackendGet(w http.ResponseWriter, r *http.Request, apiKey string, replayCache protocol.ReplayCache) bool {
	sig, ts, ok := extractBackendSignature(w, r, apiKey)
	if !ok {
		return false
	}

	if !protocol.VerifySignatureWithTimestamp(apiKey, r.Method, r.URL.RequestURI(), sig, ts, nil, protocol.DefaultMaxClockSkew, replayCache) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")

		return false
	}

	return true
}

// extractSignature is the task backend's signature-header check,
// keyed by its configured backend HMAC key.
func (h *backendHandlers) extractSignature(w http.ResponseWriter, r *http.Request) (sig, ts string, ok bool) {
	return extractBackendSignature(w, r, h.backendCfg.APIKey)
}

// authenticateGet verifies an HMAC-SHA256 signature over
// `timestamp + "." + ""` (empty body) on a backend-originated GET. Returns
// true on success; on failure it writes the 403 response and returns false.
func (h *backendHandlers) authenticateGet(w http.ResponseWriter, r *http.Request) bool {
	return authenticateBackendGet(w, r, h.backendCfg.APIKey, h.replayCache)
}

// authenticatePost reads the request body, verifies the HMAC-SHA256
// signature over method+path+body. Returns (body, true) on success; on
// failure it writes the 403/400 response and returns (nil, false).
func (h *backendHandlers) authenticatePost(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	sig, tsHeader, ok := h.extractSignature(w, r)
	if !ok {
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "failed to read request body", "")

		return nil, false
	}

	if !protocol.VerifySignatureWithTimestamp(h.backendCfg.APIKey, r.Method, r.URL.RequestURI(), sig, tsHeader, body, protocol.DefaultMaxClockSkew, h.replayCache) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")

		return nil, false
	}

	return body, true
}
