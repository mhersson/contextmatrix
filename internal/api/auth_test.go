package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAuthTestServer builds a router in multi mode with one seeded admin
// ("root" / "root password1") and returns the server + the auth service +
// the store for direct manipulation.
func newAuthTestServer(t *testing.T) (*httptest.Server, *auth.Service, *authstore.Store) {
	t.Helper()

	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := auth.NewService(store, time.Hour)

	u, err := store.CreateUser(t.Context(), "root", "Root", true, time.Now())
	require.NoError(t, err)

	hash, err := auth.HashPassword("root password1")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), u.ID, hash, time.Now()))

	// Credential-pool wiring: a random 32-byte subkey and a success-stub
	// GitHub checker, so admin-credential HTTP tests never touch the network.
	// Additive only — no existing behavior/assertion depends on this.
	credKey := make([]byte, 32)
	_, err = rand.Read(credKey)
	require.NoError(t, err)
	svc.SetCredentialKey(credKey)
	svc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })

	// TaskSkillsDir gives us a cheap session-gated GET route
	// (GET /api/task-skills) without wiring the full card service.
	router := NewRouter(RouterConfig{
		AuthService:   svc,
		AuthMode:      "multi",
		TaskSkillsDir: t.TempDir(),
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server, svc, store
}

// login performs a login and returns the session cookie.
func login(t *testing.T, server *httptest.Server, username, password string) *http.Cookie {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login",
		jsonBody(t, map[string]string{"username": username, "password": password}))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	for _, c := range resp.Cookies() {
		if c.Name == "cm_session" {
			return c
		}
	}

	t.Fatal("no cm_session cookie in login response")

	return nil
}

func TestSessionGuard_Matrix(t *testing.T) {
	server, _, _ := newAuthTestServer(t)

	tests := []struct {
		name       string
		method     string
		path       string
		wantNoAuth int // status without a session
	}{
		{name: "gated api route", method: http.MethodGet, path: "/api/task-skills", wantNoAuth: http.StatusUnauthorized},
		{name: "browser runner logs gated", method: http.MethodGet, path: "/api/runner/logs", wantNoAuth: http.StatusUnauthorized},
		{name: "browser runner health gated", method: http.MethodGet, path: "/api/runner/health", wantNoAuth: http.StatusUnauthorized},
		{name: "healthz open", method: http.MethodGet, path: "/healthz", wantNoAuth: http.StatusOK},
		{name: "readyz open", method: http.MethodGet, path: "/readyz", wantNoAuth: http.StatusOK},
		{name: "auth session endpoint reachable (401 from handler)", method: http.MethodGet, path: "/api/auth/session", wantNoAuth: http.StatusUnauthorized},
		{name: "app config slim reachable", method: http.MethodGet, path: "/api/app/config", wantNoAuth: http.StatusOK},
		{name: "agent callback exempt (404 unregistered, not 401)", method: http.MethodGet, path: "/api/agent/task-skills-source", wantNoAuth: http.StatusNotFound},
		{name: "chat callback exempt (404 unregistered, not 401)", method: http.MethodGet, path: "/api/chat/task-skills-source", wantNoAuth: http.StatusNotFound},
		{name: "admin chats gated", method: http.MethodGet, path: "/api/admin/chats", wantNoAuth: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, server.URL+tt.path, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantNoAuth, resp.StatusCode)
		})
	}
}

func TestSessionGuard_WithSession(t *testing.T) {
	server, _, _ := newAuthTestServer(t)
	cookie := login(t, server, "root", "root password1")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/task-skills", nil)
	require.NoError(t, err)
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNoneMode_RouterUnchanged(t *testing.T) {
	// AuthService nil → no gate, no auth routes: today's behavior.
	router := NewRouter(RouterConfig{TaskSkillsDir: t.TempDir()})
	server := httptest.NewServer(router)

	defer server.Close()

	resp, err := http.Get(server.URL + "/api/task-skills")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "no session required in none mode")

	resp2, err := http.Get(server.URL + "/api/auth/session")
	require.NoError(t, err)

	defer resp2.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp2.StatusCode, "auth routes not registered in none mode")
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return bytes.NewReader(data)
}

// jsonDecode decodes a response body into v and closes it.
func jsonDecode(resp *http.Response, v any) error {
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(v)
}

// timeNow is a small shim so admin tests don't import "time" just to stamp
// CreateUser/SetPasswordHash calls.
func timeNow() time.Time {
	return time.Now()
}

// authHashForTest wraps auth.HashPassword for tests that seed a user's
// password directly via the store.
func authHashForTest(t *testing.T, password string) (string, error) {
	t.Helper()

	return auth.HashPassword(password)
}

func TestAuthJourney_LoginSessionLogout(t *testing.T) {
	server, _, _ := newAuthTestServer(t)

	// Wrong password: uniform 401.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login",
		jsonBody(t, map[string]string{"username": "root", "password": "wrong"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Right password: cookie + user payload.
	cookie := login(t, server, "root", "root password1")
	assert.True(t, cookie.HttpOnly)

	// Who am I.
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/auth/session", nil)
	req.AddCookie(cookie)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	var session struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		IsAdmin     bool   `json:"is_admin"`
	}

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&session))
	_ = resp.Body.Close()

	assert.Equal(t, "root", session.Username)
	assert.True(t, session.IsAdmin)

	// Logout kills it server-side.
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", nil)
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(cookie)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/auth/session", nil)
	req.AddCookie(cookie)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthJourney_RateLimit429(t *testing.T) {
	server, _, _ := newAuthTestServer(t)

	for range 3 {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login",
			jsonBody(t, map[string]string{"username": "root", "password": "wrong"}))
		req.Header.Set("X-Requested-With", "contextmatrix")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
	}

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login",
		jsonBody(t, map[string]string{"username": "root", "password": "root password1"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Retry-After"))
}

func TestAuthJourney_InviteRedemption(t *testing.T) {
	server, svc, store := newAuthTestServer(t)

	u, err := store.CreateUser(t.Context(), "carol", "Carol", false, time.Now())
	require.NoError(t, err)

	raw, err := svc.IssueInviteToken(t.Context(), u.ID)
	require.NoError(t, err)

	// Inspect (no session, no CSRF needed on GET).
	resp, err := http.Get(server.URL + "/api/auth/token/" + raw)
	require.NoError(t, err)

	var info struct {
		Purpose  string `json:"purpose"`
		Username string `json:"username"`
	}

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&info))
	_ = resp.Body.Close()

	assert.Equal(t, "invite", info.Purpose)
	assert.Equal(t, "carol", info.Username)

	// Redeem: sets password + auto-login cookie.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/token/"+raw,
		jsonBody(t, map[string]string{"password": "carols password1"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var gotCookie bool

	for _, c := range resp.Cookies() {
		if c.Name == "cm_session" && c.Value != "" {
			gotCookie = true
		}
	}

	assert.True(t, gotCookie, "redemption auto-logs-in")

	// Second redemption: 410.
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/auth/token/"+raw,
		jsonBody(t, map[string]string{"password": "carols password1"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusGone, resp2.StatusCode)

	// Unknown token: 404.
	resp3, err := http.Get(server.URL + "/api/auth/token/garbage")
	require.NoError(t, err)

	_ = resp3.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp3.StatusCode)
}

func TestAuthJourney_ChangePassword(t *testing.T) {
	server, _, _ := newAuthTestServer(t)
	cookie := login(t, server, "root", "root password1")

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/password",
		jsonBody(t, map[string]string{"current_password": "root password1", "new_password": "brand new password1"}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Old password no longer works; new one does.
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/auth/login",
		jsonBody(t, map[string]string{"username": "root", "password": "root password1"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	login(t, server, "root", "brand new password1")
}

func TestAppConfig_AuthModeGating(t *testing.T) {
	server, _, _ := newAuthTestServer(t)

	// Unauthenticated: slim shape.
	resp, err := http.Get(server.URL + "/api/app/config")
	require.NoError(t, err)

	var slim map[string]any

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&slim))
	_ = resp.Body.Close()

	assert.Equal(t, "multi", slim["auth_mode"])
	assert.Contains(t, slim, "theme")
	assert.NotContains(t, slim, "task_backend", "full payload requires a session")

	// Authenticated: full shape.
	cookie := login(t, server, "root", "root password1")

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/app/config", nil)
	req.AddCookie(cookie)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	var full map[string]any

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&full))
	_ = resp.Body.Close()

	assert.Contains(t, full, "task_backend")
	assert.Equal(t, "multi", full["auth_mode"])
}

func TestAppConfig_NoneModeUnchanged(t *testing.T) {
	router := NewRouter(RouterConfig{Theme: "everforest"})
	server := httptest.NewServer(router)

	defer server.Close()

	resp, err := http.Get(server.URL + "/api/app/config")
	require.NoError(t, err)

	var body map[string]any

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	_ = resp.Body.Close()

	assert.Equal(t, "none", body["auth_mode"])
	assert.Contains(t, body, "task_backend", "none mode serves the full payload as today")
}

func TestAuthJourney_BootstrapOverHTTP(t *testing.T) {
	// Fresh store with ZERO users — the bootstrap situation.
	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := auth.NewService(store, time.Hour)
	server := httptest.NewServer(NewRouter(RouterConfig{AuthService: svc, AuthMode: "multi"}))

	defer server.Close()

	raw, err := svc.IssueBootstrapToken(t.Context())
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/token/"+raw,
		jsonBody(t, map[string]string{"username": "Admin", "display_name": "The Admin", "password": "a strong password"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var user struct {
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
	}

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	assert.Equal(t, "admin", user.Username)
	assert.True(t, user.IsAdmin)
}

func TestObserve_RedactsTokenPaths(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&buf, nil))
	prev := slog.Default()

	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	server, _, _ := newAuthTestServer(t)

	resp, err := http.Get(server.URL + "/api/auth/token/super-secret-raw-token")
	require.NoError(t, err)

	_ = resp.Body.Close()

	logs := buf.String()
	assert.NotContains(t, logs, "super-secret-raw-token", "raw one-time tokens must not reach log lines")
	assert.Contains(t, logs, "/api/auth/token/[redacted]")
}
