package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
)

// --- canonicalGitHost ---

// TestCanonicalGitHost pins the normalization rules git/curl apply when
// deciding two host spellings refer to the same host: case-insensitive,
// trailing FQDN dot ignored, ":port" stripped (including a bracketed IPv6
// literal's port). A bare (unbracketed) host with more than one colon is an
// IPv6 literal, never host:port, and must be left untouched.
func TestCanonicalGitHost(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "bare", input: "github.com", want: "github.com"},
		{name: "port suffix", input: "github.com:443", want: "github.com"},
		{name: "trailing dot", input: "github.com.", want: "github.com"},
		{name: "mixed case", input: "GitHub.COM", want: "github.com"},
		{name: "mixed case with port", input: "GitHub.COM:443", want: "github.com"},
		{name: "trailing dot with port", input: "github.com.:443", want: "github.com"},
		{name: "surrounding whitespace", input: "  github.com  ", want: "github.com"},
		{name: "bracketed IPv6 with port", input: "[::1]:443", want: "::1"},
		{name: "bracketed IPv6 without port", input: "[::1]", want: "::1"},
		{name: "bare IPv6 left alone, not mistaken for host:port", input: "::1", want: "::1"},
		{name: "IPv4 with port", input: "127.0.0.1:8443", want: "127.0.0.1"},
		{name: "empty", input: "", want: ""},
		{name: "whitespace only", input: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canonicalGitHost(tt.input))
		})
	}
}

// --- WorkerCredentialsToken / VerifyWorkerCredentialsToken ---

func TestWorkerCredentialsToken_Deterministic(t *testing.T) {
	tok1 := WorkerCredentialsToken("chat-key", "session-abc")
	tok2 := WorkerCredentialsToken("chat-key", "session-abc")
	assert.Equal(t, tok1, tok2, "same key+session must always mint the same token")
}

func TestWorkerCredentialsToken_SessionDistinct(t *testing.T) {
	tok1 := WorkerCredentialsToken("chat-key", "session-abc")
	tok2 := WorkerCredentialsToken("chat-key", "session-xyz")
	assert.NotEqual(t, tok1, tok2, "different sessions must mint different tokens")
}

func TestWorkerCredentialsToken_KeyDistinct(t *testing.T) {
	tok1 := WorkerCredentialsToken("chat-key-1", "session-abc")
	tok2 := WorkerCredentialsToken("chat-key-2", "session-abc")
	assert.NotEqual(t, tok1, tok2, "different keys must mint different tokens for the same session")
}

func TestWorkerCredentialsToken_FormShape(t *testing.T) {
	tok := WorkerCredentialsToken("chat-key", "session-abc")
	require.Contains(t, tok, ".")
	assert.Greater(t, len(tok), len("session-abc."), "token must carry a non-empty mac after the dot")
}

func TestVerifyWorkerCredentialsToken_RoundTrip(t *testing.T) {
	tok := WorkerCredentialsToken("chat-key", "session-abc")

	sessionID, ok := VerifyWorkerCredentialsToken("chat-key", tok)
	require.True(t, ok)
	assert.Equal(t, "session-abc", sessionID)
}

func TestVerifyWorkerCredentialsToken_WrongKeyRejected(t *testing.T) {
	tok := WorkerCredentialsToken("chat-key", "session-abc")

	_, ok := VerifyWorkerCredentialsToken("other-key", tok)
	assert.False(t, ok, "a token minted with a different key must not verify")
}

func TestVerifyWorkerCredentialsToken_TamperedMacRejected(t *testing.T) {
	tok := WorkerCredentialsToken("chat-key", "session-abc")

	tampered := tok[:len(tok)-1] + "x"
	if tampered == tok {
		tampered = tok[:len(tok)-1] + "y"
	}

	_, ok := VerifyWorkerCredentialsToken("chat-key", tampered)
	assert.False(t, ok, "flipping a byte in the mac must invalidate the token")
}

func TestVerifyWorkerCredentialsToken_TamperedSessionIDRejected(t *testing.T) {
	tok := WorkerCredentialsToken("chat-key", "session-abc")
	// Swap the embedded session id for a different one but keep the
	// original mac — the mac no longer matches the (different) session id.
	forged := WorkerCredentialsToken("chat-key", "session-xyz")

	sessionID, macPart, _ := cutFirstDot(t, forged)
	_, origMacPart, _ := cutFirstDot(t, tok)
	_ = macPart

	stitched := sessionID + "." + origMacPart

	_, ok := VerifyWorkerCredentialsToken("chat-key", stitched)
	assert.False(t, ok, "session id must not be swappable while keeping a foreign mac")
}

func TestVerifyWorkerCredentialsToken_GarbageRejected(t *testing.T) {
	_, ok := VerifyWorkerCredentialsToken("chat-key", "not-a-real-token-no-dot")
	assert.False(t, ok)
}

func TestVerifyWorkerCredentialsToken_EmptyRejected(t *testing.T) {
	_, ok := VerifyWorkerCredentialsToken("chat-key", "")
	assert.False(t, ok)
}

func TestVerifyWorkerCredentialsToken_EmptySessionIDRejected(t *testing.T) {
	_, ok := VerifyWorkerCredentialsToken("chat-key", ".abcdef")
	assert.False(t, ok)
}

func TestVerifyWorkerCredentialsToken_EmptyMacRejected(t *testing.T) {
	_, ok := VerifyWorkerCredentialsToken("chat-key", "session-abc.")
	assert.False(t, ok)
}

// cutFirstDot splits s on the first '.', mirroring VerifyWorkerCredentialsToken's
// own split so the tamper test can reassemble a forged token from two
// legitimately-minted tokens without depending on unexported internals.
func cutFirstDot(t *testing.T, s string) (before, after string, found bool) {
	t.Helper()

	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return s[:i], s[i+1:], true
		}
	}

	return s, "", false
}

// --- GET /api/worker/git-credentials ---
//
// Chat workers fetch per-repo git credentials on demand, authenticated by
// the deterministic per-session bearer minted at chat-start
// (ChatStartPayload.GitCredentialsToken). See docs/api-reference.md § Worker
// Endpoints for the full contract.

// stubSessionLiveness is a chatSessionLiveness test double. gotSessionID
// captures the id passed to the most recent SessionLiveness call so tests
// can pin that the endpoint checks liveness for the session embedded in the
// bearer token, not some other input.
type stubSessionLiveness struct {
	live         bool
	err          error
	gotSessionID string
}

func (s *stubSessionLiveness) SessionLiveness(_ context.Context, sessionID string) (bool, error) {
	s.gotSessionID = sessionID

	return s.live, s.err
}

// countingTokenProvider wraps a githubauth.TokenGenerator and counts
// GenerateToken calls, so fail-closed tests can assert the instance
// credential was NEVER minted, not merely that the response wasn't 200.
type countingTokenProvider struct {
	inner githubauth.TokenGenerator
	calls atomic.Int32
}

func (c *countingTokenProvider) GenerateToken(ctx context.Context) (string, time.Time, error) {
	c.calls.Add(1)

	return c.inner.GenerateToken(ctx)
}

// doWorkerGitCredentialsRequest drives h.getGitCredentials directly (no
// router/mux) with the given bearer token and host/path query params. An
// empty token omits the Authorization header entirely; empty host/path
// values are omitted from the query string.
func doWorkerGitCredentialsRequest(t *testing.T, h *workerCredentialsHandlers, token, host, path string) *httptest.ResponseRecorder {
	t.Helper()

	target := "/api/worker/git-credentials"

	q := url.Values{}
	if host != "" {
		q.Set("host", host)
	}

	if path != "" {
		q.Set("path", path)
	}

	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}

	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.getGitCredentials(rec, req)

	return rec
}

func noProjects(context.Context) ([]board.ProjectConfig, error) { return nil, nil }

func TestGetWorkerGitCredentials_MatchedProject_UsesProjectProvider(t *testing.T) {
	projectAProvider := &fakeTokenProvider{token: "project-a-token"}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{
				{Name: "alpha", Repo: "https://github.com/acme/alpha"},
				{Name: "beta", Repo: "https://github.com/acme/beta"},
			}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			require.Equal(t, "alpha", project)

			return projectAProvider, "", nil
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/alpha")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "x-access-token", body.Username)
	assert.Equal(t, "project-a-token", body.Token)
}

func TestGetWorkerGitCredentials_DifferentPathMatchesDifferentProject(t *testing.T) {
	projectBProvider := &fakeTokenProvider{token: "project-b-token"}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{
				{Name: "alpha", Repo: "https://github.com/acme/alpha"},
				{Name: "beta", Repo: "https://github.com/acme/beta"},
			}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			require.Equal(t, "beta", project)

			return projectBProvider, "", nil
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/beta")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "project-b-token", body.Token)
}

func TestGetWorkerGitCredentials_UnmatchedRepo_UsesInstanceProvider(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{
				{Name: "alpha", Repo: "https://github.com/acme/alpha"},
			}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			return nil, "", errors.New("must not be called: no project matches this repo")
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "someone/unrelated")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "instance-token", body.Token, "an unmatched repo gets the instance credential directly")
}

func TestGetWorkerGitCredentials_NilListProjects_UsesInstanceProvider(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		// listProjects intentionally nil — mirrors a router built with no
		// card service configured.
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "instance-token", body.Token)
}

// TestGetWorkerGitCredentials_MatchedProjectBrokenBinding_ConflictNeverInstance
// is the fail-closed guarantee: a matched project whose credential binding is
// broken rejects with 409 and never substitutes the (healthy, reachable)
// instance credential.
func TestGetWorkerGitCredentials_MatchedProjectBrokenBinding_ConflictNeverInstance(t *testing.T) {
	instanceProvider := &countingTokenProvider{inner: &fakeTokenProvider{token: "instance-token"}}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{
				{Name: "alpha", Repo: "https://github.com/acme/alpha", GitHubCredential: "broken-cred"},
			}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			require.Equal(t, "alpha", project)

			return nil, "", errors.New("credential pool: entry \"broken-cred\" not found")
		},
		instanceProvider: instanceProvider,
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/alpha")

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Zero(t, instanceProvider.calls.Load(),
		"instance credential must never be minted for a matched project's broken binding")

	var apiErr APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
}

func TestGetWorkerGitCredentials_ListProjectsFails_ConflictNeverInstance(t *testing.T) {
	instanceProvider := &countingTokenProvider{inner: &fakeTokenProvider{token: "instance-token"}}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return nil, errors.New("boards dir unreadable")
		},
		instanceProvider: instanceProvider,
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Zero(t, instanceProvider.calls.Load(),
		"instance credential must never be served when project resolution itself failed")
}

func TestGetWorkerGitCredentials_NoProviderAvailable_Conflict(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         &stubSessionLiveness{live: true},
		listProjects:     noProjects,
		instanceProvider: nil, // nothing configured at all
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestGetWorkerGitCredentials_ColdSession_Conflict(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         &stubSessionLiveness{live: false},
		listProjects:     noProjects,
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/alpha")

	assert.Equal(t, http.StatusConflict, rec.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBackendNotRunning, apiErr.Code)
}

func TestGetWorkerGitCredentials_UnknownSession_NotFound(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:   "chat-key",
		liveness:     &stubSessionLiveness{err: chat.ErrSessionNotFound},
		listProjects: noProjects,
	}

	token := WorkerCredentialsToken("chat-key", "sess-missing")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/alpha")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeChatNotFound, apiErr.Code)
}

func TestGetWorkerGitCredentials_LivenessStoreError_InternalError(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:   "chat-key",
		liveness:     &stubSessionLiveness{err: errors.New("store down")},
		listProjects: noProjects,
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/alpha")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInternalError, apiErr.Code)
}

func TestGetWorkerGitCredentials_MissingBearer_Unauthorized(t *testing.T) {
	h := &workerCredentialsHandlers{chatAPIKey: "chat-key"}

	rec := doWorkerGitCredentialsRequest(t, h, "", "github.com", "acme/alpha")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetWorkerGitCredentials_GarbageBearer_Unauthorized(t *testing.T) {
	h := &workerCredentialsHandlers{chatAPIKey: "chat-key"}

	rec := doWorkerGitCredentialsRequest(t, h, "garbage-no-dot", "github.com", "acme/alpha")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetWorkerGitCredentials_WrongKeyBearer_Unauthorized(t *testing.T) {
	h := &workerCredentialsHandlers{chatAPIKey: "chat-key"}

	token := WorkerCredentialsToken("a-different-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/alpha")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestGetWorkerGitCredentials_MissingHostOrPath_BadRequest pins the "exactly
// one empty" half of the contract: host and path are required as a pair, so
// only one of them present is still a malformed request. The "both empty"
// shape is a different contract (the repo-less gh shortcut) — see
// TestGetWorkerGitCredentials_EmptyHostAndPath_UsesInstanceProvider below.
func TestGetWorkerGitCredentials_MissingHostOrPath_BadRequest(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
	}
	token := WorkerCredentialsToken("chat-key", "sess-1")

	tests := []struct {
		name, host, path string
	}{
		{name: "missing host", host: "", path: "acme/alpha"},
		{name: "missing path", host: "github.com", path: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doWorkerGitCredentialsRequest(t, h, token, tt.host, tt.path)
			assert.Equal(t, http.StatusBadRequest, rec.Code)

			var apiErr APIError
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
			assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
		})
	}
}

// --- Empty (host, path) pair: repo-less gh (no origin remote) ---
//
// The chat worker's gh wrapper calls this endpoint with BOTH host and path
// empty when cwd has no origin remote (gh repo create, gh api /user).
// Before this contract existed, repo-less gh was covered by the
// instance-wide shared token, so the endpoint must serve the instance
// credential for the empty pair to preserve that capability — the same
// credential any unmatched repo already resolves to.

// TestGetWorkerGitCredentials_EmptyHostAndPath_UsesInstanceProvider pins the
// repo-less gh contract: an empty (host, path) pair skips project matching
// ENTIRELY — listProjects must not even be called, since a repo-less
// caller's credential must not depend on project listing succeeding — and
// resolves straight to the instance provider.
func TestGetWorkerGitCredentials_EmptyHostAndPath_UsesInstanceProvider(t *testing.T) {
	var listProjectsCalls atomic.Int32

	instanceProvider := &countingTokenProvider{inner: &fakeTokenProvider{token: "instance-token"}}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			listProjectsCalls.Add(1)

			return []board.ProjectConfig{{Name: "alpha", Repo: "https://github.com/acme/alpha"}}, nil
		},
		providerForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return nil, "", errors.New("must not be called: empty host/path must skip project matching")
		},
		instanceProvider: instanceProvider,
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "", "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "instance-token", body.Token)

	assert.Equal(t, int32(1), instanceProvider.calls.Load(), "instance provider must be consulted for the empty pair")
	assert.Zero(t, listProjectsCalls.Load(), "project resolution must be skipped entirely for the empty pair")
}

// TestGetWorkerGitCredentials_EmptyHostAndPath_ColdSession_Conflict pins that
// the instance-credential shortcut does not bypass the liveness gate: a cold
// session still 409s even though host/path are both empty.
func TestGetWorkerGitCredentials_EmptyHostAndPath_ColdSession_Conflict(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         &stubSessionLiveness{live: false},
		listProjects:     noProjects,
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "", "")

	assert.Equal(t, http.StatusConflict, rec.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBackendNotRunning, apiErr.Code)
}

// TestGetWorkerGitCredentials_EmptyHostAndPath_BadBearer_Unauthorized pins
// that the instance-credential shortcut does not bypass bearer auth.
func TestGetWorkerGitCredentials_EmptyHostAndPath_BadBearer_Unauthorized(t *testing.T) {
	h := &workerCredentialsHandlers{chatAPIKey: "chat-key"}

	rec := doWorkerGitCredentialsRequest(t, h, "garbage-no-dot", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetWorkerGitCredentials_MintFailure_BadGateway(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         &stubSessionLiveness{live: true},
		listProjects:     noProjects,
		instanceProvider: &fakeTokenProvider{err: errors.New("request token: github api returned status 401")},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	assert.Equal(t, http.StatusBadGateway, rec.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInternalError, apiErr.Code)
}

func TestGetWorkerGitCredentials_ExpiresAtOmittedWhenEmpty(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         &stubSessionLiveness{live: true},
		listProjects:     noProjects,
		instanceProvider: &fakeTokenProvider{token: "instance-token"}, // zero expiresAt
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	require.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))
	_, present := raw["expires_at"]
	assert.False(t, present, "zero-value expiry must be omitted, never formatted as 0001-01-01...")
}

func TestGetWorkerGitCredentials_ExpiresAtPresentWhenSet(t *testing.T) {
	expiry := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         &stubSessionLiveness{live: true},
		listProjects:     noProjects,
		instanceProvider: &fakeTokenProvider{token: "instance-token", expiresAt: expiry},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	require.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))
	assert.Equal(t, expiry.UTC().Format(time.RFC3339), raw["expires_at"])
}

func TestGetWorkerGitCredentials_LivenessCheckedForTokenSessionID(t *testing.T) {
	liveness := &stubSessionLiveness{live: true}
	h := &workerCredentialsHandlers{
		chatAPIKey:       "chat-key",
		liveness:         liveness,
		listProjects:     noProjects,
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-xyz-789")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/unrelated")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "sess-xyz-789", liveness.gotSessionID,
		"liveness must be checked for the session id embedded in the verified bearer")
}

// TestGetWorkerGitCredentials_RepoMatching_CaseInsensitiveAndGitSuffix pins the
// brief's matching rule: bare host compared case-insensitively, owner/repo
// path compared case-insensitively, with a .git suffix tolerated on either
// side (project repo URL, or the request's path param).
func TestGetWorkerGitCredentials_RepoMatching_CaseInsensitiveAndGitSuffix(t *testing.T) {
	tests := []struct {
		name        string
		projectRepo string
		reqHost     string
		reqPath     string
	}{
		{name: "exact https match", projectRepo: "https://github.com/acme/widgets", reqHost: "github.com", reqPath: "acme/widgets"},
		{name: "project side .git suffix", projectRepo: "https://github.com/acme/widgets.git", reqHost: "github.com", reqPath: "acme/widgets"},
		{name: "request side .git suffix", projectRepo: "https://github.com/acme/widgets", reqHost: "github.com", reqPath: "acme/widgets.git"},
		{name: "case-insensitive host", projectRepo: "https://GitHub.com/acme/widgets", reqHost: "github.com", reqPath: "acme/widgets"},
		{name: "case-insensitive path", projectRepo: "https://github.com/Acme/Widgets", reqHost: "github.com", reqPath: "acme/widgets"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchedProvider := &fakeTokenProvider{token: "matched-token"}

			h := &workerCredentialsHandlers{
				chatAPIKey: "chat-key",
				liveness:   &stubSessionLiveness{live: true},
				listProjects: func(context.Context) ([]board.ProjectConfig, error) {
					return []board.ProjectConfig{{Name: "widgets-project", Repo: tt.projectRepo}}, nil
				},
				providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
					require.Equal(t, "widgets-project", project)

					return matchedProvider, "", nil
				},
				instanceProvider: &fakeTokenProvider{token: "instance-token"},
			}

			token := WorkerCredentialsToken("chat-key", "sess-1")
			rec := doWorkerGitCredentialsRequest(t, h, token, tt.reqHost, tt.reqPath)

			require.Equal(t, http.StatusOK, rec.Code)

			var body workerGitCredentialsResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
			assert.Equal(t, "matched-token", body.Token,
				"must resolve to the matched project's provider, not the instance one")
		})
	}
}

// TestGetWorkerGitCredentials_NoPrefixGuessing_UsesInstanceProvider pins:
// only an EXACT owner/repo match selects a project — a request path that is
// merely a superset/prefix of a project's repo path must not match.
func TestGetWorkerGitCredentials_NoPrefixGuessing_UsesInstanceProvider(t *testing.T) {
	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{{Name: "widgets-project", Repo: "https://github.com/acme/widgets"}}, nil
		},
		providerForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return nil, "", errors.New("must not be called for a prefix-only match")
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/widgets-extra")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "instance-token", body.Token)
}

// TestGetWorkerGitCredentials_SkipsProjectsWithEmptyOrUnparseableRepo pins:
// projects with no repo configured, or a repo URL that doesn't parse to a
// (host, path), never match and never abort resolution for later projects.
func TestGetWorkerGitCredentials_SkipsProjectsWithEmptyOrUnparseableRepo(t *testing.T) {
	matchedProvider := &fakeTokenProvider{token: "matched-token"}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{
				{Name: "no-repo"},
				{Name: "garbage-repo", Repo: "::not-a-url::"},
				{Name: "real-project", Repo: "https://github.com/acme/real"},
			}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			require.Equal(t, "real-project", project)

			return matchedProvider, "", nil
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/real")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "matched-token", body.Token)
}

// --- Host canonicalization (git-equivalent host spellings must not defeat matching) ---
//
// Git/curl treat "github.com", "github.com:443", and "github.com." (trailing
// FQDN dot) as the same host. Before canonicalGitHost existed, the request's
// "host" query param was only trimmed, while the project side was compared
// via url.Hostname() (which already strips a port but not a trailing dot or
// case). A caller could therefore respell the host and *unmatch* a project,
// routing the request to the instance provider — bypassing the fail-closed
// broken-binding guarantee below and downgrading least-privilege for healthy
// bindings.

// TestGetWorkerGitCredentials_MatchedProjectBrokenBinding_HostRespelling_ConflictNeverInstance
// is a sibling of TestGetWorkerGitCredentials_MatchedProjectBrokenBinding_ConflictNeverInstance:
// same broken-binding project, but the request respells the host with a
// ":443" port suffix or a trailing FQDN dot. Both spellings must still match
// the project and fail closed (409, zero instance-provider calls) — not fall
// through to the instance credential.
func TestGetWorkerGitCredentials_MatchedProjectBrokenBinding_HostRespelling_ConflictNeverInstance(t *testing.T) {
	tests := []struct {
		name    string
		reqHost string
	}{
		{name: "port suffix", reqHost: "github.com:443"},
		{name: "trailing dot", reqHost: "github.com."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instanceProvider := &countingTokenProvider{inner: &fakeTokenProvider{token: "instance-token"}}

			h := &workerCredentialsHandlers{
				chatAPIKey: "chat-key",
				liveness:   &stubSessionLiveness{live: true},
				listProjects: func(context.Context) ([]board.ProjectConfig, error) {
					return []board.ProjectConfig{
						{Name: "alpha", Repo: "https://github.com/acme/alpha", GitHubCredential: "broken-cred"},
					}, nil
				},
				providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
					require.Equal(t, "alpha", project)

					return nil, "", errors.New("credential pool: entry \"broken-cred\" not found")
				},
				instanceProvider: instanceProvider,
			}

			token := WorkerCredentialsToken("chat-key", "sess-1")
			rec := doWorkerGitCredentialsRequest(t, h, token, tt.reqHost, "acme/alpha")

			assert.Equal(t, http.StatusConflict, rec.Code,
				"a respelled host must still match the project and fail closed, not fall through to the instance provider")
			assert.Zero(t, instanceProvider.calls.Load(),
				"instance credential must never be minted when a respelled host still refers to a matched project's broken binding")
		})
	}
}

// TestGetWorkerGitCredentials_HealthyBinding_HostRespelling_UsesProjectProvider
// is the healthy-binding counterpart: a respelled host must still resolve the
// PROJECT's provider, not silently fail over to the instance credential
// (which would be a least-privilege downgrade for a perfectly healthy
// binding).
func TestGetWorkerGitCredentials_HealthyBinding_HostRespelling_UsesProjectProvider(t *testing.T) {
	tests := []struct {
		name    string
		reqHost string
	}{
		{name: "port suffix", reqHost: "github.com:443"},
		{name: "trailing dot", reqHost: "github.com."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectProvider := &fakeTokenProvider{token: "project-token"}
			instanceProvider := &countingTokenProvider{inner: &fakeTokenProvider{token: "instance-token"}}

			h := &workerCredentialsHandlers{
				chatAPIKey: "chat-key",
				liveness:   &stubSessionLiveness{live: true},
				listProjects: func(context.Context) ([]board.ProjectConfig, error) {
					return []board.ProjectConfig{
						{Name: "alpha", Repo: "https://github.com/acme/alpha"},
					}, nil
				},
				providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
					require.Equal(t, "alpha", project)

					return projectProvider, "", nil
				},
				instanceProvider: instanceProvider,
			}

			token := WorkerCredentialsToken("chat-key", "sess-1")
			rec := doWorkerGitCredentialsRequest(t, h, token, tt.reqHost, "acme/alpha")

			require.Equal(t, http.StatusOK, rec.Code)

			var body workerGitCredentialsResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
			assert.Equal(t, "project-token", body.Token,
				"a respelled request host must still resolve the matched project's provider")
			assert.Zero(t, instanceProvider.calls.Load(),
				"instance provider must not be consulted when the canonicalized host matches a project")
		})
	}
}

// TestGetWorkerGitCredentials_ProjectRepoHostTrailingDotAndCase_StillMatches
// pins the other direction of the fix: canonicalization also applies to the
// PROJECT side, so an odd-cased or trailing-dot repo URL in .board.yaml still
// matches a plainly-spelled request host.
func TestGetWorkerGitCredentials_ProjectRepoHostTrailingDotAndCase_StillMatches(t *testing.T) {
	matchedProvider := &fakeTokenProvider{token: "matched-token"}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{{Name: "widgets-project", Repo: "https://GitHub.Com./acme/widgets"}}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			require.Equal(t, "widgets-project", project)

			return matchedProvider, "", nil
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/widgets")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "matched-token", body.Token,
		"a trailing-dot, mixed-case project repo host must still canonicalize to match a plain request host")
}

// TestGetWorkerGitCredentials_MatchesAnyOfProjectsMultipleRepos exercises the
// EffectiveRepos() multi-repo case: a project's non-primary repo must still
// resolve to that project, not fall through to the instance credential.
func TestGetWorkerGitCredentials_MatchesAnyOfProjectsMultipleRepos(t *testing.T) {
	matchedProvider := &fakeTokenProvider{token: "matched-token"}

	h := &workerCredentialsHandlers{
		chatAPIKey: "chat-key",
		liveness:   &stubSessionLiveness{live: true},
		listProjects: func(context.Context) ([]board.ProjectConfig, error) {
			return []board.ProjectConfig{
				{
					Name: "multi-repo-project",
					Repos: []board.Repo{
						{Name: "primary", URL: "https://github.com/acme/primary", Primary: true},
						{Name: "secondary", URL: "https://github.com/acme/secondary"},
					},
				},
			}, nil
		},
		providerForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			require.Equal(t, "multi-repo-project", project)

			return matchedProvider, "", nil
		},
		instanceProvider: &fakeTokenProvider{token: "instance-token"},
	}

	token := WorkerCredentialsToken("chat-key", "sess-1")
	// The SECOND (non-primary) repo must still resolve to this project.
	rec := doWorkerGitCredentialsRequest(t, h, token, "github.com", "acme/secondary")

	require.Equal(t, http.StatusOK, rec.Code)

	var body workerGitCredentialsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "matched-token", body.Token)
}
