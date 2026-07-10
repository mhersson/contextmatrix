//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"
)

// Shared multi-mode auth fixtures. Every password/secret below is obviously
// fake — the values live only in a throwaway auth.db and are never asserted on.
const (
	adminUser = "harness-admin"
	adminPass = "harness-admin-pw" // >= MinPasswordLength (10)
)

// bootstrapLinkRe extracts the one-time token from the line CM logs on its
// first zero-user start:
//
//	msg="auth: bootstrap link" path=/auth/token/<token>
//
// The token is base64url (RawURLEncoding): charset [A-Za-z0-9_-]. The logged
// path is the public form; the redeem endpoint is POST /api/auth/token/<token>.
var bootstrapLinkRe = regexp.MustCompile(`/auth/token/([A-Za-z0-9_-]+)`)

// sessionResponse mirrors the JSON returned by the auth endpoints
// (login / redeem / session).
type sessionResponse struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// scrapeBootstrapToken pulls the one-time bootstrap token out of CM's captured
// logs. The link is logged during startup, before /healthz serves, so it is
// present by the time startCM returns; the short poll guards output buffering.
func scrapeBootstrapToken(t *testing.T, cm *process) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := bootstrapLinkRe.FindStringSubmatch(cm.stderr.String()); len(m) == 2 {
			return m[1]
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("bootstrap link not found in CM logs:\n%s", tail(cm.stderr.String(), 40))

	return ""
}

// bootAdminSession scrapes the bootstrap token, redeems it to create the first
// admin, and returns an authenticated cookie-jar client. Scenario clients use
// this — in multi mode X-Agent-ID alone does not authenticate browser routes.
func bootAdminSession(t *testing.T, baseURL string, cm *process) *apiClient {
	t.Helper()

	token := scrapeBootstrapToken(t, cm)

	admin := newAPIClient(t, baseURL)

	redeem := map[string]any{"username": adminUser, "display_name": "Harness Admin", "password": adminPass}

	var session sessionResponse
	if status, body := admin.do(t, http.MethodPost, "/api/auth/token/"+token, redeem, &session); status != http.StatusOK {
		t.Fatalf("redeem bootstrap: want 200 got %d body=%s", status, body)
	}

	if session.Username != adminUser || !session.IsAdmin {
		t.Fatalf("bootstrap redeem: want admin=%s is_admin=true got %+v", adminUser, session)
	}

	return admin
}

// fakeGitHub is an httptest server standing in for the GitHub REST API. It
// answers the PAT probe (GET /rate_limit) with 200 so credential validation
// passes without a real network call, and counts probe hits.
type fakeGitHub struct {
	*httptest.Server
	mu   sync.Mutex
	hitN int
}

func (f *fakeGitHub) hits() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.hitN
}

func startFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()

	f := &fakeGitHub{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rate_limit" {
			f.mu.Lock()
			f.hitN++
			f.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"resources":{"core":{"limit":5000,"remaining":5000}}}`)

			return
		}

		http.NotFound(w, r)
	}))

	t.Cleanup(f.Close)

	return f
}

// hasField reports whether any object in list has field == want.
func hasField(list []map[string]any, field, want string) bool {
	for _, m := range list {
		if s, _ := m[field].(string); s == want {
			return true
		}
	}

	return false
}
