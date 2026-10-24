package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/mhersson/contextmatrix/internal/githubauth"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
)

// WorkerCredentialsToken mints the deterministic per-session bearer a chat
// or architect worker presents to GET /api/worker/git-credentials. Form:
// "<sessionID>.<base64url(HMAC-SHA256(apiKey, sessionID))>", where apiKey is
// the owning backend's api_key.
//
// Deterministic by design: CM never persists the token anywhere. Instead it
// is reminted on demand from apiKey + sessionID (both already known to CM at
// verification time), so there is nothing to look up and nothing to leak
// from a token store. The start payload carries the token to the worker
// once (GitCredentialsToken); the worker treats it as an opaque bearer and
// never inspects it.
func WorkerCredentialsToken(apiKey, sessionID string) string {
	return sessionID + "." + workerCredentialsMAC(apiKey, sessionID)
}

// VerifyWorkerCredentialsToken splits token on the FIRST '.' (session ids are
// UUIDs/ULIDs and never contain a dot), recomputes the expected token from
// apiKey + the extracted session id, and constant-time-compares it
// against the presented token via hmac.Equal. Returns the session id and
// ok=true only when the whole reconstructed token matches byte-for-byte -
// a tampered mac OR a session id swapped in from a different token both fail
// this comparison, since the mac only matches the session id it was minted
// for.
func VerifyWorkerCredentialsToken(apiKey, token string) (sessionID string, ok bool) {
	sessionID, macPart, found := strings.Cut(token, ".")
	if !found || sessionID == "" || macPart == "" {
		return "", false
	}

	want := WorkerCredentialsToken(apiKey, sessionID)
	if !hmac.Equal([]byte(token), []byte(want)) {
		return "", false
	}

	return sessionID, true
}

// workerCredentialsMAC computes the base64url (no padding) HMAC-SHA256 of
// sessionID keyed by apiKey.
func workerCredentialsMAC(apiKey, sessionID string) string {
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(sessionID))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyToken checks token against every configured key and returns the
// session id and the kinds the token is valid for: the chat key vouches for
// chat sessions, the architect key for blueprints. Both kinds verify when the
// two entries share a key. ok is false when no key matches.
func (h *workerCredentialsHandlers) verifyToken(token string) (sessionID string, kinds map[chat.SessionKind]bool, ok bool) {
	kinds = map[chat.SessionKind]bool{}

	for kind, key := range map[chat.SessionKind]string{chat.KindChat: h.chatAPIKey, chat.KindArchitect: h.architectAPIKey} {
		// An empty key must never verify: WorkerCredentialsToken("", id) is forgeable.
		if key == "" {
			continue
		}

		sid, match := VerifyWorkerCredentialsToken(key, token)
		if !match {
			continue
		}

		sessionID = sid
		kinds[kind] = true
	}

	return sessionID, kinds, len(kinds) > 0
}

// chatSessionLiveness is the narrow slice of *chat.Manager the worker
// credentials endpoint needs: a session's kind and whether it is attached to
// its backend (a chat worker container, or an open architect session).
// Defined here, in the consuming package, per project convention.
type chatSessionLiveness interface {
	SessionLiveness(ctx context.Context, sessionID string) (kind chat.SessionKind, live bool, err error)
}

// workerGitCredentialsResponse is the success body of
// GET /api/worker/git-credentials. ExpiresAt is omitted (not zero-valued)
// when the provider reports no expiry (PAT-backed credentials).
type workerGitCredentialsResponse struct {
	Username  string `json:"username"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// workerCredentialsHandlers serves GET /api/worker/git-credentials: chat
// workers and the architect fetch per-repo git credentials on demand,
// authenticated by the deterministic per-session bearer minted at session
// start (GitCredentialsToken), independent of auth.mode. See
// docs/api-reference.md § Worker Endpoints for the full contract.
type workerCredentialsHandlers struct {
	// chatAPIKey is the chat backend's api_key, the secret chat-session
	// tokens are minted with. Empty when no chat backend is keyed.
	chatAPIKey string

	// architectAPIKey is the architect backend's api_key, the secret
	// blueprint-session tokens are minted with. Empty when no architect
	// backend is keyed. NewRouter registers the handler when either key is
	// non-empty.
	architectAPIKey string

	// liveness reports a session's kind and whether it is attached to its
	// backend. Narrow so tests can stub it without a real *chat.Manager.
	liveness chatSessionLiveness

	// listProjects enumerates every project so the handler can match the
	// request's (host, path) against each project's effective repo(s). nil
	// when no card service is configured - resolution then always falls
	// through to the instance provider (no projects to match against).
	listProjects func(ctx context.Context) ([]board.ProjectConfig, error)

	// providerForProject resolves the project-scoped git-token provider for
	// a matched project: the project's credential binding when set
	// (fail-closed on a broken one), else the instance provider. Same func
	// RouterConfig threads into backendHandlers.
	providerForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error)

	// instanceProvider mints the instance credential used when no project's
	// repo matches the request - the correct credential for non-project
	// repos, not a degraded fallback. Workers only ever get a token scoped to
	// the requested repo from it.
	instanceProvider githubauth.TokenGenerator

	// deniedRepoNames holds the lowercased bare names of the repos CM itself
	// pushes to (boards, task-skills). Bare names only: GitHub scopes an
	// installation token by name inside the installation's account, so a
	// request spelling a different owner or host would still mint the
	// denied repo's token.
	deniedRepoNames map[string]bool
}

// getGitCredentials handles GET /api/worker/git-credentials. Order of checks:
// bearer auth (401) → host/path presence (400 when exactly one is empty) →
// session lookup (404 unknown / 500 lookup error) → kind binding (401 on a
// mismatch) → liveness (409 cold) → repo shape and deny list (403) → project
// match + provider resolution (409 fail-closed on a broken binding) → token
// mint scoped to the requested repo (502) → 200.
func (h *workerCredentialsHandlers) getGitCredentials(w http.ResponseWriter, r *http.Request) {
	token, ok := extractBearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "missing or invalid bearer token", "")

		return
	}

	sessionID, kinds, ok := h.verifyToken(token)
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "missing or invalid bearer token", "")

		return
	}

	// host is canonicalized (case, trailing FQDN dot, ":port") so it compares
	// bare against a project's parsed repo host - see canonicalGitHost. Both
	// sides of that comparison must go through the same normalization, or a
	// caller could "unmatch" a project by respelling its host and fall
	// through to the instance provider.
	host := canonicalGitHost(r.URL.Query().Get("host"))
	path := normalizeWorkerRepoPath(r.URL.Query().Get("path"))

	// Exactly one of the two empty is a malformed request; both empty is
	// refused below, after the liveness gate.
	hostEmpty := host == ""
	pathEmpty := path == ""

	if hostEmpty != pathEmpty {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "host and path query params are required", "")

		return
	}

	kind, live, err := h.liveness.SessionLiveness(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, chat.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, ErrCodeChatNotFound, "chat session not found", "")

			return
		}

		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal error", "")

		return
	}

	// A token is bound to the kind of the key that minted it. A key holder
	// for one backend must not mint tokens for the other kind's session ids,
	// so a mismatch is the same 401 as a malformed token: no oracle.
	if !kinds[kind] {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "missing or invalid bearer token", "")

		return
	}

	if !live {
		writeError(w, http.StatusConflict, ErrCodeWorkerNotRunning, "chat session is not running", "")

		return
	}

	if hostEmpty {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "a repository is required", "")

		return
	}

	repoName, ok := repoNameFromPath(path)
	if !ok {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "repository path must be owner/repo", "")

		return
	}

	if h.deniedRepoNames[strings.ToLower(repoName)] {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "repository is not served to workers", "")

		return
	}

	provider, err := h.resolveProvider(r.Context(), host, path)
	if err != nil {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "project credential unavailable", sanitizeErrorDetails(err))

		return
	}

	if provider == nil {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "git credential unavailable", "")

		return
	}

	mintedToken, expiresAt, err := provider.GenerateRepoToken(r.Context(), repoName)
	if err != nil {
		writeError(w, http.StatusBadGateway, ErrCodeInternalError, "token mint failed", sanitizeErrorDetails(err))

		return
	}

	writeJSON(w, http.StatusOK, workerGitCredentialsResponse{
		Username:  "x-access-token",
		Token:     mintedToken,
		ExpiresAt: tokenExpiryString(expiresAt),
	})
}

// resolveProvider matches (host, path) against every project's effective
// repo(s) and resolves the corresponding credential provider. A match routes
// through providerForProject, which is itself fail-closed on a broken
// binding - this function never substitutes the instance provider for a
// matched project's error. No match returns instanceProvider directly: the
// correct credential for a non-project repo, not a fallback.
func (h *workerCredentialsHandlers) resolveProvider(ctx context.Context, host, path string) (githubauth.TokenGenerator, error) {
	project, err := h.matchProject(ctx, host, path)
	if err != nil {
		return nil, err
	}

	if project == nil {
		return h.instanceProvider, nil
	}

	if h.providerForProject == nil {
		return nil, nil
	}

	provider, _, err := h.providerForProject(ctx, project.Name)
	if err != nil {
		return nil, err // fail closed: matched project, broken binding
	}

	return provider, nil
}

// matchProject returns the first project whose effective repo(s) match
// (host, path), or nil (no error) when none do. Projects with no repo
// configured, or a repo URL that doesn't parse into a (host, path), are
// silently skipped rather than erroring.
func (h *workerCredentialsHandlers) matchProject(ctx context.Context, host, path string) (*board.ProjectConfig, error) {
	if h.listProjects == nil {
		return nil, nil
	}

	projects, err := h.listProjects(ctx)
	if err != nil {
		return nil, err
	}

	for i := range projects {
		for _, repo := range projects[i].EffectiveRepos() {
			if workerRepoMatches(host, path, repo.URL) {
				return &projects[i], nil
			}
		}
	}

	return nil, nil
}

// extractBearerToken reads the raw token out of an "Authorization: Bearer
// <token>" header. ok is false when the header is absent, lacks the exact
// "Bearer " prefix, or the token portion is empty.
func extractBearerToken(r *http.Request) (token string, ok bool) {
	const prefix = "Bearer "

	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}

	token = strings.TrimPrefix(h, prefix)
	if token == "" {
		return "", false
	}

	return token, true
}

// normalizeWorkerRepoPath trims surrounding slashes/whitespace and strips a
// trailing ".git" so the request's path query param tolerates the same
// shapes as a project's parsed repo URL.
func normalizeWorkerRepoPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")

	return path
}

// canonicalGitHost normalizes a bare host the same way git/curl treat
// git-equivalent host spellings, so both sides of a host comparison can be
// compared byte-for-byte instead of just case-insensitively: trims
// surrounding whitespace, lowercases, strips a single trailing "." (FQDN dot
// notation), and strips a ":port" suffix - including a bracketed IPv6
// literal's port, e.g. "[::1]:443" -> "::1". A bare (unbracketed) host with
// more than one colon is left untouched: that shape is an IPv6 literal, never
// host:port, and must not be truncated at the last colon. Takes no scheme -
// the input is always a bare host, never a URL.
func canonicalGitHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end >= 0 {
			return strings.TrimSuffix(s[1:end], ".")
		}

		return strings.TrimSuffix(s, ".") // malformed bracket - nothing better to do
	}

	if strings.Count(s, ":") == 1 {
		s = s[:strings.LastIndexByte(s, ':')]
	}

	return strings.TrimSuffix(s, ".")
}

// workerRepoHostPath extracts a bare host and an owner/repo-shaped path from
// a repo URL. Supports the common forms: https://host/owner/repo(.git),
// ssh://host/owner/repo(.git) (both via url.Parse), and the SCP-like
// git@host:owner/repo(.git) form. The returned host is already canonicalized
// via canonicalGitHost, so a trailing-dot or odd-cased repo URL still matches
// a plainly-spelled request host. Returns ok=false for empty or unparseable
// input - callers treat that as "skip this repo", not an error.
func workerRepoHostPath(rawURL string) (host, path string, ok bool) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", false
	}

	if !strings.Contains(rawURL, "://") {
		if _, after, ok0 := strings.Cut(rawURL, "@"); ok0 {
			rest := after
			if before, after, ok0 := strings.Cut(rest, ":"); ok0 {
				h := canonicalGitHost(before)
				p := normalizeWorkerRepoPath(after)

				if h == "" || p == "" {
					return "", "", false
				}

				return h, p, true
			}
		}
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "", "", false
	}

	p := normalizeWorkerRepoPath(u.Path)
	if p == "" {
		return "", "", false
	}

	return canonicalGitHost(u.Hostname()), p, true
}

// workerRepoMatches reports whether (host, path) - host already
// canonicalized and path already normalized by the caller - match repoURL.
// Host is compared bare (both sides went through canonicalGitHost); path is
// compared case-insensitively. An unparseable repoURL never matches.
func workerRepoMatches(host, path, repoURL string) bool {
	repoHost, repoPath, ok := workerRepoHostPath(repoURL)
	if !ok {
		return false
	}

	return repoHost == host && strings.EqualFold(repoPath, path)
}
