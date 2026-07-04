//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Multi-mode scenario constants. Every password/secret below is obviously
// fake — the fixtures live only in this test's throwaway auth.db, and the
// credential secret is validated against a local fake GitHub (below), never
// the real service. None of these values are asserted on.
const (
	mmAdminUser  = "harness-admin"
	mmAdminPass  = "harness-admin-pw" // >= MinPasswordLength (10)
	mmSecondUser = "harness-bob"
	mmSecondPass = "harness-bob-pw-01" // >= MinPasswordLength (10)
	mmCredName   = "harness-cred"
	mmCredSecret = "fake-pat-not-a-real-secret"
)

// bootstrapLinkRe extracts the one-time token from the line CM logs on its
// first zero-user start:
//
//	msg="auth: bootstrap link" path=/auth/token/<token>
//
// The token is base64url (RawURLEncoding) so the charset is [A-Za-z0-9_-].
// The logged path is the public form (no /api prefix); the redeem endpoint is
// POST /api/auth/token/<token>.
var bootstrapLinkRe = regexp.MustCompile(`/auth/token/([A-Za-z0-9_-]+)`)

// TestMultiUserAdminSurface boots the real CM binary in auth.mode: multi and
// exercises the auth + admin HTTP surface end-to-end over real HTTP:
//
//  1. boot an admin-surface-only server (no task backend — the runner backend
//     is frozen under multi mode);
//  2. assert unauthenticated reads are rejected (401);
//  3. scrape the bootstrap link from the logs, redeem it to create the first
//     admin, then log in with a password (cookie jar);
//  4. create a second, non-admin user and prove the 401/403 contract;
//  5. create a credential (validated against a local fake GitHub) and bind it
//     to a project.
//
// It needs no Docker worker; TestMain still builds the binaries and the stub
// image, which is why it lives under the integration tag.
func TestMultiUserAdminSurface(t *testing.T) {
	mc := bootMultiAuth(t)

	// --- Step 2: unauthenticated requests are rejected before any handler. ---
	anon := newMMClient(t, mc.baseURL)

	if status, body := anon.do(t, http.MethodGet, "/api/projects", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("unauth GET /api/projects: want 401 got %d body=%s", status, body)
	}
	// The session guard runs before the admin role check, so an admin route
	// without a session is 401 (unauthenticated), not 403.
	if status, body := anon.do(t, http.MethodGet, "/api/admin/users", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("unauth GET /api/admin/users: want 401 got %d body=%s", status, body)
	}

	// --- Step 3: bootstrap the first admin, then log in. ---
	token := scrapeBootstrapToken(t, mc.cm)

	admin := newMMClient(t, mc.baseURL)

	// Inspect does not consume the token.
	var inspect struct {
		Purpose  string `json:"purpose"`
		Username string `json:"username"`
	}
	if status, body := admin.do(t, http.MethodGet, "/api/auth/token/"+token, nil, &inspect); status != http.StatusOK {
		t.Fatalf("inspect bootstrap token: want 200 got %d body=%s", status, body)
	}

	if inspect.Purpose != "bootstrap" {
		t.Fatalf("bootstrap token purpose: want bootstrap got %q", inspect.Purpose)
	}

	// Redeem creates the first admin and auto-logs-in (sets the cookie).
	redeem := map[string]any{"username": mmAdminUser, "display_name": "Harness Admin", "password": mmAdminPass}

	var session sessionResponse
	if status, body := admin.do(t, http.MethodPost, "/api/auth/token/"+token, redeem, &session); status != http.StatusOK {
		t.Fatalf("redeem bootstrap: want 200 got %d body=%s", status, body)
	}

	if session.Username != mmAdminUser || !session.IsAdmin {
		t.Fatalf("bootstrap redeem: want admin=%s is_admin=true got %+v", mmAdminUser, session)
	}

	// Redeeming a spent token is rejected (410 Gone).
	if status, body := admin.do(t, http.MethodGet, "/api/auth/token/"+token, nil, nil); status != http.StatusGone {
		t.Fatalf("inspect spent bootstrap token: want 410 got %d body=%s", status, body)
	}

	// Explicit password login proves the login endpoint works; it overwrites
	// the auto-login cookie with a fresh session on the same jar.
	var loginSession sessionResponse
	if status, body := admin.do(t, http.MethodPost, "/api/auth/login",
		map[string]any{"username": mmAdminUser, "password": mmAdminPass}, &loginSession); status != http.StatusOK {
		t.Fatalf("admin login: want 200 got %d body=%s", status, body)
	}

	if !loginSession.IsAdmin {
		t.Fatalf("admin login: is_admin false")
	}

	// A bad password is a uniform 401.
	if status, body := admin.do(t, http.MethodPost, "/api/auth/login",
		map[string]any{"username": mmAdminUser, "password": "wrong-password"}, nil); status != http.StatusUnauthorized {
		t.Fatalf("login with wrong password: want 401 got %d body=%s", status, body)
	}

	// The gated read that was 401 while logged-out now succeeds.
	if status, body := admin.do(t, http.MethodGet, "/api/projects", nil, nil); status != http.StatusOK {
		t.Fatalf("authed GET /api/projects: want 200 got %d body=%s", status, body)
	}

	if status, body := admin.do(t, http.MethodGet, "/api/auth/session", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /api/auth/session: want 200 got %d body=%s", status, body)
	}

	// --- Step 4/5: admin user surface + the 401/403 contract. ---
	var users []map[string]any
	if status, body := admin.do(t, http.MethodGet, "/api/admin/users", nil, &users); status != http.StatusOK {
		t.Fatalf("admin list users: want 200 got %d body=%s", status, body)
	}

	if !hasField(users, "username", mmAdminUser) {
		t.Fatalf("admin user %q not in list %v", mmAdminUser, users)
	}

	// Create a second, non-admin user; capture the invite token it returns.
	var created struct {
		Invite struct {
			Token string `json:"token"`
		} `json:"invite"`
	}

	createUser := map[string]any{"username": mmSecondUser, "display_name": "Harness Bob", "is_admin": false}
	if status, body := admin.do(t, http.MethodPost, "/api/admin/users", createUser, &created); status != http.StatusCreated {
		t.Fatalf("create user: want 201 got %d body=%s", status, body)
	}

	if created.Invite.Token == "" {
		t.Fatalf("create user: empty invite token")
	}

	// Bob redeems his invite on his own jar → authenticated, non-admin.
	bob := newMMClient(t, mc.baseURL)

	var bobSession sessionResponse
	if status, body := bob.do(t, http.MethodPost, "/api/auth/token/"+created.Invite.Token,
		map[string]any{"password": mmSecondPass}, &bobSession); status != http.StatusOK {
		t.Fatalf("bob redeem invite: want 200 got %d body=%s", status, body)
	}

	if bobSession.Username != mmSecondUser || bobSession.IsAdmin {
		t.Fatalf("bob redeem: want %s non-admin got %+v", mmSecondUser, bobSession)
	}

	// Bob is authenticated (200 on a gated read) but forbidden (403) on the
	// admin surface — the role gate, distinct from the session gate.
	if status, body := bob.do(t, http.MethodGet, "/api/projects", nil, nil); status != http.StatusOK {
		t.Fatalf("bob GET /api/projects: want 200 got %d body=%s", status, body)
	}

	if status, body := bob.do(t, http.MethodGet, "/api/admin/users", nil, nil); status != http.StatusForbidden {
		t.Fatalf("bob GET /api/admin/users: want 403 got %d body=%s", status, body)
	}

	if status, body := bob.do(t, http.MethodPost, "/api/admin/credentials",
		map[string]any{"name": "nope", "kind": "pat", "secret": "nope"}, nil); status != http.StatusForbidden {
		t.Fatalf("bob POST /api/admin/credentials: want 403 got %d body=%s", status, body)
	}

	// --- Step 4 (option a): credential create against a local fake GitHub. ---
	// POST /api/admin/credentials triggers a LIVE validation. We point the
	// per-credential api_base_url at a fake httptest server the harness owns,
	// so the PAT probe (GET <api_base_url>/rate_limit) succeeds without ever
	// reaching real GitHub, and complete a bind to the project.
	fake := startFakeGitHub(t)

	createCred := map[string]any{
		"name": mmCredName, "kind": "pat", "api_base_url": fake.URL, "secret": mmCredSecret,
	}

	var createdCred struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		CreatedBy string `json:"created_by"`
	}

	status, credBody := admin.do(t, http.MethodPost, "/api/admin/credentials", createCred, &createdCred)
	if status != http.StatusCreated {
		t.Fatalf("create credential: want 201 got %d body=%s", status, credBody)
	}

	if createdCred.Name != mmCredName || createdCred.Kind != "pat" {
		t.Fatalf("create credential 201 body: want name=%q kind=pat got %+v", mmCredName, createdCred)
	}

	// Secrets are write-only: the created response must never echo one.
	if strings.Contains(credBody, mmCredSecret) {
		t.Fatalf("create credential 201 body echoes the secret: %s", credBody)
	}

	if fake.hits() == 0 {
		t.Fatalf("fake GitHub never received the credential probe")
	}

	var creds []map[string]any
	if status, body := admin.do(t, http.MethodGet, "/api/admin/credentials", nil, &creds); status != http.StatusOK {
		t.Fatalf("list credentials: want 200 got %d body=%s", status, body)
	}

	if !hasField(creds, "name", mmCredName) {
		t.Fatalf("credential %q not in list %v", mmCredName, creds)
	}

	// Bind the credential to the project. PUT replaces the config wholesale, so
	// re-send the current states/types/priorities/transitions with the binding
	// added.
	var proj struct {
		Repo        string              `json:"repo"`
		States      []string            `json:"states"`
		Types       []string            `json:"types"`
		Priorities  []string            `json:"priorities"`
		Transitions map[string][]string `json:"transitions"`
	}
	if status, body := admin.do(t, http.MethodGet, "/api/projects/"+mc.project, nil, &proj); status != http.StatusOK {
		t.Fatalf("get project: want 200 got %d body=%s", status, body)
	}

	bindBody := map[string]any{
		"repo":              proj.Repo,
		"states":            proj.States,
		"types":             proj.Types,
		"priorities":        proj.Priorities,
		"transitions":       proj.Transitions,
		"github_credential": mmCredName,
	}

	var bound struct {
		GitHubCredential string `json:"github_credential"`
	}
	if status, body := admin.do(t, http.MethodPut, "/api/projects/"+mc.project, bindBody, &bound); status != http.StatusOK {
		t.Fatalf("bind credential: want 200 got %d body=%s", status, body)
	}

	if bound.GitHubCredential != mmCredName {
		t.Fatalf("bind credential: want %q got %q", mmCredName, bound.GitHubCredential)
	}
}

// sessionResponse mirrors the JSON shape returned by the auth endpoints
// (login / redeem / session).
type sessionResponse struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// multiCtx is the running multi-mode server under test.
type multiCtx struct {
	cm      *process
	baseURL string
	project string
}

// bootMultiAuth writes a multi-mode CM config (no task backend), boots the
// real binary, and returns a handle. It reuses the shared harness plumbing
// (initBoardsRepo, startCM, runLog) so only the config differs from the
// none-mode scenarios.
func bootMultiAuth(t *testing.T) *multiCtx {
	t.Helper()

	const project = "harness"

	tmpDir := t.TempDir()

	boardsDir := filepath.Join(tmpDir, "boards")
	if err := os.MkdirAll(boardsDir, 0o755); err != nil {
		t.Fatalf("mkdir boards: %v", err)
	}

	initBoardsRepo(t, boardsDir, project)

	port := freePort(t)
	cfgPath := writeMultiAuthCMConfig(t, tmpDir, boardsDir, port)

	rl, err := newRunLog("multiuser")
	if err != nil {
		t.Fatalf("runlog: %v", err)
	}

	start := time.Now()

	// Registered before startCM so its SIGTERM cleanup (registered inside
	// startCM) runs first in LIFO order — finalize then reads a stable cmSink.
	t.Cleanup(func() {
		status := "PASS"
		if t.Failed() {
			status = "FAIL"
		}

		rl.finalize("multiuser", status, time.Since(start), nil)
		t.Logf("scenario diagnostics: %s", rl.dir)
	})

	cm := startCM(t, cfgPath, port, rl)

	return &multiCtx{cm: cm, baseURL: fmt.Sprintf("http://127.0.0.1:%d", port), project: project}
}

// writeMultiAuthCMConfig writes an admin-surface-only CM config in auth.mode:
// multi. It declares no backends block — the runner backend is frozen under
// multi mode, and this scenario needs no task execution. auth.db and the
// master key file land in the scenario tmp dir (CM creates both on first
// start), keeping the run hermetic.
func writeMultiAuthCMConfig(t *testing.T, tmpDir, boardsDir string, port int) string {
	t.Helper()

	path := filepath.Join(tmpDir, "cm-config-multi.yaml")

	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
mcp_api_key: %q
boards:
  dir: %s
  git_auto_commit: true
op_store:
  db_path: %s
images:
  db_path: %s
auth:
  mode: multi
  db_path: %s
  master_key_file: %s
cors_origin: http://127.0.0.1:0
theme: everforest
github:
  auth_mode: pat
  pat:
    token: harness-not-used
`, port, randomHex(t, 16), boardsDir,
		filepath.Join(tmpDir, "ops.db"), filepath.Join(tmpDir, "images.db"),
		filepath.Join(tmpDir, "auth.db"), filepath.Join(tmpDir, "master.key"))

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write multi CM config: %v", err)
	}

	return path
}

// scrapeBootstrapToken pulls the one-time bootstrap token out of CM's captured
// logs. The link is logged during startup, before /healthz serves, so it is
// already present by the time startCM returns; the short poll guards against
// output buffering.
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

// mmClient is a cookie-jar HTTP client for the multi-mode surface. Each caller
// (admin, bob, anon) gets its own jar so their sessions stay independent. It
// sets the CSRF header on writes — the auth and admin routes are not
// CSRF-exempt.
type mmClient struct {
	baseURL string
	hc      *http.Client
}

func newMMClient(t *testing.T, baseURL string) *mmClient {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	return &mmClient{baseURL: baseURL, hc: &http.Client{Timeout: 10 * time.Second, Jar: jar}}
}

// do issues a request, decoding a JSON body into `into` on 2xx. It returns the
// status and the (truncated) raw body for assertion messages.
func (c *mmClient) do(t *testing.T, method, path string, body, into any) (int, string) {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s %s: %v", method, path, err)
		}
	}

	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	if err != nil {
		t.Fatalf("req %s %s: %v", method, path, err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		req.Header.Set("X-Requested-With", "contextmatrix")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if into != nil && resp.StatusCode < 400 && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("decode %s %s: %v body=%s", method, path, err, raw)
		}
	}

	return resp.StatusCode, string(raw)
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
