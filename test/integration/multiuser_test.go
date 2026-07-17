//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Multi-mode fixtures specific to this test (the shared admin fixtures live in
// auth_test.go). Obviously fake; never asserted on.
const (
	mmSecondUser = "harness-bob"
	mmSecondPass = "harness-bob-pw-01" // >= MinPasswordLength (10)
	mmCredName   = "harness-cred"
	mmCredSecret = "fake-pat-not-a-real-secret"
)

// TestMultiUserAdminSurface boots the real CM binary in auth.mode: multi with
// no task backend and exercises the auth + admin HTTP surface end-to-end:
//
//  1. assert unauthenticated reads are rejected (401);
//  2. scrape the bootstrap link, inspect + redeem it to create the first admin,
//     then log in with a password (cookie jar);
//  3. create a second, non-admin user and prove the 401/403 contract;
//  4. create a credential (validated against a local fake GitHub) and bind it
//     to a project.
//
// It needs no Docker worker and no sibling repo - TestMain builds only CM.
func TestMultiUserAdminSurface(t *testing.T) {
	mc := bootMultiAuth(t)

	// --- Step 1: unauthenticated requests are rejected before any handler. ---
	anon := newAPIClient(t, mc.baseURL)

	if status, body := anon.do(t, http.MethodGet, "/api/projects", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("unauth GET /api/projects: want 401 got %d body=%s", status, body)
	}
	// The session guard runs before the admin role check, so an admin route
	// without a session is 401 (unauthenticated), not 403.
	if status, body := anon.do(t, http.MethodGet, "/api/admin/users", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("unauth GET /api/admin/users: want 401 got %d body=%s", status, body)
	}

	// --- Step 2: bootstrap the first admin, then log in. ---
	token := scrapeBootstrapToken(t, mc.cm)

	admin := newAPIClient(t, mc.baseURL)

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
	redeem := map[string]any{"username": adminUser, "display_name": "Harness Admin", "password": adminPass}

	var session sessionResponse
	if status, body := admin.do(t, http.MethodPost, "/api/auth/token/"+token, redeem, &session); status != http.StatusOK {
		t.Fatalf("redeem bootstrap: want 200 got %d body=%s", status, body)
	}

	if session.Username != adminUser || !session.IsAdmin {
		t.Fatalf("bootstrap redeem: want admin=%s is_admin=true got %+v", adminUser, session)
	}

	// Redeeming a spent token is rejected (410 Gone).
	if status, body := admin.do(t, http.MethodGet, "/api/auth/token/"+token, nil, nil); status != http.StatusGone {
		t.Fatalf("inspect spent bootstrap token: want 410 got %d body=%s", status, body)
	}

	// Explicit password login proves the login endpoint works; it overwrites
	// the auto-login cookie with a fresh session on the same jar.
	var loginSession sessionResponse
	if status, body := admin.do(t, http.MethodPost, "/api/auth/login",
		map[string]any{"username": adminUser, "password": adminPass}, &loginSession); status != http.StatusOK {
		t.Fatalf("admin login: want 200 got %d body=%s", status, body)
	}

	if !loginSession.IsAdmin {
		t.Fatalf("admin login: is_admin false")
	}

	// A bad password is a uniform 401.
	if status, body := admin.do(t, http.MethodPost, "/api/auth/login",
		map[string]any{"username": adminUser, "password": "wrong-password"}, nil); status != http.StatusUnauthorized {
		t.Fatalf("login with wrong password: want 401 got %d body=%s", status, body)
	}

	// The gated read that was 401 while logged-out now succeeds.
	if status, body := admin.do(t, http.MethodGet, "/api/projects", nil, nil); status != http.StatusOK {
		t.Fatalf("authed GET /api/projects: want 200 got %d body=%s", status, body)
	}

	if status, body := admin.do(t, http.MethodGet, "/api/auth/session", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /api/auth/session: want 200 got %d body=%s", status, body)
	}

	// --- Step 3: admin user surface + the 401/403 contract. ---
	var users []map[string]any
	if status, body := admin.do(t, http.MethodGet, "/api/admin/users", nil, &users); status != http.StatusOK {
		t.Fatalf("admin list users: want 200 got %d body=%s", status, body)
	}

	if !hasField(users, "username", adminUser) {
		t.Fatalf("admin user %q not in list %v", adminUser, users)
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
	bob := newAPIClient(t, mc.baseURL)

	var bobSession sessionResponse
	if status, body := bob.do(t, http.MethodPost, "/api/auth/token/"+created.Invite.Token,
		map[string]any{"password": mmSecondPass}, &bobSession); status != http.StatusOK {
		t.Fatalf("bob redeem invite: want 200 got %d body=%s", status, body)
	}

	if bobSession.Username != mmSecondUser || bobSession.IsAdmin {
		t.Fatalf("bob redeem: want %s non-admin got %+v", mmSecondUser, bobSession)
	}

	// Bob is authenticated (200 on a gated read) but forbidden (403) on the
	// admin surface - the role gate, distinct from the session gate.
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

	// --- Step 4: credential create against a local fake GitHub. ---
	// POST /api/admin/credentials triggers a LIVE validation. We point the
	// per-credential api_base_url at a fake httptest server the harness owns,
	// so the PAT probe (GET <api_base_url>/rate_limit) succeeds without ever
	// reaching real GitHub, and complete a bind to the project.
	fake := startFakeGitHub(t)

	createCred := map[string]any{
		"name": mmCredName, "kind": "pat", "api_base_url": fake.URL, "secret": mmCredSecret,
	}

	var createdCred struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
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

// multiCtx is the running multi-mode server under test.
type multiCtx struct {
	cm      *process
	baseURL string
	project string
}

// bootMultiAuth writes a multi-mode CM config with no task backend, boots the
// real binary, and returns a handle. It reuses the shared harness plumbing
// (newScenarioConfig, initBoardsRepo, startCM, runLog) so only the absence of
// backends distinguishes it from the scenario boots.
func bootMultiAuth(t *testing.T) *multiCtx {
	t.Helper()

	const project = "harness"

	sc := newScenarioConfig(t, "multiuser")
	initBoardsRepo(t, sc, project)

	cfgPath := sc.writeCMConfig(t, cmConfigOptions{})

	rl, err := newRunLog("multiuser")
	if err != nil {
		t.Fatalf("runlog: %v", err)
	}

	start := time.Now()

	// Registered before startCM so its SIGTERM cleanup (registered inside
	// startCM) runs first in LIFO order - finalize then reads a stable cmSink.
	t.Cleanup(func() {
		status := "PASS"
		if t.Failed() {
			status = "FAIL"
		}

		rl.finalize("multiuser", status, time.Since(start), nil)
		t.Logf("scenario diagnostics: %s", rl.dir)
	})

	cm := startCM(t, cfgPath, sc.cmPort, rl)

	return &multiCtx{cm: cm, baseURL: fmt.Sprintf("http://127.0.0.1:%d", sc.cmPort), project: project}
}
