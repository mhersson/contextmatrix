package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
)

// writeSkillFile creates dir/<name>/SKILL.md with a frontmatter
// description for use in lister tests.
func writeSkillFile(t *testing.T, dir, name, description string) {
	t.Helper()

	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))
}

func TestTaskSkillsLister_EmptyDir(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		l := newTaskSkillsLister("")

		skills, err := l.List(context.Background())
		require.NoError(t, err)
		assert.Nil(t, skills)
	})

	t.Run("nonexistent path", func(t *testing.T) {
		l := newTaskSkillsLister(filepath.Join(t.TempDir(), "missing"))

		skills, err := l.List(context.Background())
		require.NoError(t, err)
		assert.Nil(t, skills)
	})

	t.Run("empty existing dir", func(t *testing.T) {
		dir := t.TempDir()
		l := newTaskSkillsLister(dir)

		skills, err := l.List(context.Background())
		require.NoError(t, err)
		assert.Empty(t, skills)
	})
}

func TestTaskSkillsLister_ValidSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "go-development", "Use when implementing or modifying Go source files.")
	writeSkillFile(t, dir, "typescript-react", "Use when writing or updating React/TypeScript components.")
	writeSkillFile(t, dir, "code-review", "Use when reviewing changes for correctness or security issues.")

	l := newTaskSkillsLister(dir)

	skills, err := l.List(context.Background())
	require.NoError(t, err)
	require.Len(t, skills, 3)

	// Sorted ascending by name.
	assert.Equal(t, "code-review", skills[0].Name)
	assert.Equal(t, "go-development", skills[1].Name)
	assert.Equal(t, "typescript-react", skills[2].Name)
	assert.Equal(t, "Use when implementing or modifying Go source files.", skills[1].Description)
}

func TestTaskSkillsLister_SkipsBadEntries(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "valid-skill", "Use when valid.")

	// Subdirectory with no SKILL.md - should be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "no-skill-md"), 0o755))

	// Subdirectory with malformed frontmatter - should be skipped.
	badDir := filepath.Join(dir, "bad-frontmatter")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("not a frontmatter doc\n"), 0o644))

	// Subdirectory with invalid name - should be skipped (path safety).
	invalidNameDir := filepath.Join(dir, "Bad Name With Spaces")
	require.NoError(t, os.MkdirAll(invalidNameDir, 0o755))

	// Regular file at the top - should be skipped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0o644))

	l := newTaskSkillsLister(dir)

	skills, err := l.List(context.Background())
	require.NoError(t, err)
	require.Len(t, skills, 1)
	assert.Equal(t, "valid-skill", skills[0].Name)
}

func TestTaskSkillsLister_CachesByMtime(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "first", "Use when first.")

	l := newTaskSkillsLister(dir)

	skills, err := l.List(context.Background())
	require.NoError(t, err)
	require.Len(t, skills, 1)

	// Add another skill and update directory mtime so the lister picks it up.
	writeSkillFile(t, dir, "second", "Use when second.")

	now := skills[0].Name // touch to force refresh below
	_ = now

	// Bump dir mtime explicitly - writeSkillFile may already do so via
	// MkdirAll, but be defensive.
	require.NoError(t, os.Chtimes(dir, mustNow(), mustNow()))

	skills, err = l.List(context.Background())
	require.NoError(t, err)
	require.Len(t, skills, 2)
}

func TestParseSkillDescription(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		want     string
		wantErr  bool
		errMatch string
	}{
		{
			name:  "lf-delimited",
			input: "---\nname: foo\ndescription: Use when foo.\n---\n\n# Foo\n",
			want:  "Use when foo.",
		},
		{
			name:  "crlf-delimited",
			input: "---\r\nname: foo\r\ndescription: Use when foo.\r\n---\r\n\r\n# Foo\r\n",
			want:  "Use when foo.",
		},
		{
			name:  "with-utf8-bom",
			input: "\xef\xbb\xbf---\nname: foo\ndescription: Use when bom.\n---\n",
			want:  "Use when bom.",
		},
		{
			name:    "missing-delimiter",
			input:   "name: foo\ndescription: Use when missing.\n",
			wantErr: true,
		},
		{
			name:    "unterminated",
			input:   "---\nname: foo\ndescription: Use when missing.\n",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSkillDescription([]byte(tc.input))
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListTaskSkillsHandler(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "alpha", "Use when alpha.")
	writeSkillFile(t, dir, "bravo", "Use when bravo.")

	router := NewRouter(RouterConfig{TaskSkillsDir: dir})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/task-skills")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Skills []TaskSkillSummary `json:"skills"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	require.Len(t, body.Skills, 2)
	assert.Equal(t, "alpha", body.Skills[0].Name)
	assert.Equal(t, "Use when alpha.", body.Skills[0].Description)
	assert.Equal(t, "bravo", body.Skills[1].Name)
}

func TestListTaskSkillsHandler_Unconfigured(t *testing.T) {
	router := NewRouter(RouterConfig{TaskSkillsDir: ""})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/task-skills")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Skills []TaskSkillSummary `json:"skills"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Empty(t, body.Skills)
}

func TestValidateSkillsAgainstAvailable(t *testing.T) {
	available := map[string]struct{}{
		"go-development":   {},
		"typescript-react": {},
	}

	cases := []struct {
		name    string
		skills  []string
		wantErr bool
	}{
		{name: "empty", skills: nil, wantErr: false},
		{name: "all-known", skills: []string{"go-development"}, wantErr: false},
		{name: "all-known-multi", skills: []string{"go-development", "typescript-react"}, wantErr: false},
		{name: "one-unknown", skills: []string{"go-development", "missing"}, wantErr: true},
		{name: "all-unknown", skills: []string{"missing-a", "missing-b"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkillsAgainstAvailable(tc.skills, available)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateSkillsAgainstAvailable_NilAvailable(t *testing.T) {
	// When the lister returned no skills (e.g. dir unconfigured), validation
	// is skipped so admins running without a configured task-skills dir
	// don't get blocked from setting card.skills via the API.
	err := validateSkillsAgainstAvailable([]string{"anything"}, nil)
	require.NoError(t, err)
}

func TestValidateSkillsAgainstProjectDefault(t *testing.T) {
	defaults := []string{"go-development", "typescript-react"}

	cases := []struct {
		name           string
		skills         []string
		projectDefault *[]string
		wantErr        bool
	}{
		{name: "no-project-default", skills: []string{"any"}, projectDefault: nil, wantErr: false},
		{name: "subset", skills: []string{"go-development"}, projectDefault: &defaults, wantErr: false},
		{name: "exact-match", skills: defaults, projectDefault: &defaults, wantErr: false},
		{name: "outside-default", skills: []string{"go-development", "code-review"}, projectDefault: &defaults, wantErr: true},
		{name: "empty-skills", skills: nil, projectDefault: &defaults, wantErr: false},
		{name: "empty-default-non-empty-skills", skills: []string{"go-development"}, projectDefault: &[]string{}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkillsAgainstProjectDefault(tc.skills, tc.projectDefault)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

// mustNow returns time.Now without import noise.
func mustNow() (t1 time.Time) {
	return time.Now()
}

// --- Integration tests: project + card update with skills validation ---

func TestUpdateProject_DefaultSkills(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	skillsDir := t.TempDir()
	writeSkillFile(t, skillsDir, "go-development", "Use when Go.")
	writeSkillFile(t, skillsDir, "documentation", "Use when docs.")

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, TaskSkillsDir: skillsDir})

	server := httptest.NewServer(router)
	defer server.Close()

	defaultStates := []string{"todo", "in_progress", "done", "stalled", "not_planned"}
	defaultTypes := []string{"task", "bug", "feature"}
	defaultPrios := []string{"low", "medium", "high"}
	defaultTrans := map[string][]string{
		"todo":        {"in_progress"},
		"in_progress": {"done", "todo"},
		"done":        {"todo"},
		"stalled":     {"todo", "in_progress"},
		"not_planned": {"todo"},
	}

	t.Run("valid subset", func(t *testing.T) {
		req := updateProjectRequest{
			States:        defaultStates,
			Types:         defaultTypes,
			Priorities:    defaultPrios,
			Transitions:   defaultTrans,
			DefaultSkills: &[]string{"go-development", "documentation"},
		}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("unknown skill rejected", func(t *testing.T) {
		req := updateProjectRequest{
			States:        defaultStates,
			Types:         defaultTypes,
			Priorities:    defaultPrios,
			Transitions:   defaultTrans,
			DefaultSkills: &[]string{"go-development", "missing-skill"},
		}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Error, "missing-skill")
	})

	t.Run("explicit empty list", func(t *testing.T) {
		req := updateProjectRequest{
			States:        defaultStates,
			Types:         defaultTypes,
			Priorities:    defaultPrios,
			Transitions:   defaultTrans,
			DefaultSkills: &[]string{},
		}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestUpdateCard_SkillsValidation(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	skillsDir := t.TempDir()
	writeSkillFile(t, skillsDir, "go-development", "Use when Go.")
	writeSkillFile(t, skillsDir, "documentation", "Use when docs.")
	writeSkillFile(t, skillsDir, "code-review", "Use when reviewing.")

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, TaskSkillsDir: skillsDir})

	server := httptest.NewServer(router)
	defer server.Close()

	// Set project default to a 2-skill subset.
	defaults := []string{"go-development", "documentation"}
	defaultStates := []string{"todo", "in_progress", "done", "stalled", "not_planned"}
	defaultTypes := []string{"task", "bug", "feature"}
	defaultPrios := []string{"low", "medium", "high"}
	defaultTrans := map[string][]string{
		"todo":        {"in_progress"},
		"in_progress": {"done", "todo"},
		"done":        {"todo"},
		"stalled":     {"todo", "in_progress"},
		"not_planned": {"todo"},
	}

	projectReq := updateProjectRequest{
		States:        defaultStates,
		Types:         defaultTypes,
		Priorities:    defaultPrios,
		Transitions:   defaultTrans,
		DefaultSkills: &defaults,
	}
	pBody, _ := json.Marshal(projectReq)
	pReq, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project", bytes.NewReader(pBody))
	pReq.Header.Set("Content-Type", "application/json")
	pResp, err := http.DefaultClient.Do(pReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, pResp.StatusCode)
	closeBody(t, pResp.Body)

	// Create a card we can update.
	createBody, _ := json.Marshal(createCardRequest{Title: "Test", Type: "task", Priority: "medium"})
	createResp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var card map[string]any
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&card))
	closeBody(t, createResp.Body)

	cardID := card["id"].(string)

	t.Run("subset of project default ok", func(t *testing.T) {
		req := patchCardRequest{Skills: &[]string{"go-development"}}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+cardID, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("outside project default rejected", func(t *testing.T) {
		req := patchCardRequest{Skills: &[]string{"go-development", "code-review"}}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+cardID, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Error, "code-review")
	})

	t.Run("unknown skill rejected", func(t *testing.T) {
		req := patchCardRequest{Skills: &[]string{"missing-skill"}}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+cardID, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("explicit empty list ok", func(t *testing.T) {
		req := patchCardRequest{Skills: &[]string{}}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+cardID, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// --- taskSkillsSource derivation ---

func TestTaskSkillsSourceFromCheckout(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "https://example.test/skills.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init")

	url, ref := taskSkillsSource(dir, "")
	assert.Equal(t, "https://example.test/skills.git", url, "remote derived from origin")
	assert.NotEmpty(t, ref, "ref derived from HEAD")
}

func TestTaskSkillsSourceFallbackForNonGitDir(t *testing.T) {
	url, ref := taskSkillsSource(t.TempDir(), "https://configured.test/s.git")
	assert.Equal(t, "https://configured.test/s.git", url, "non-git dir falls back to configured remote")
	assert.Empty(t, ref, "no ref when not a checkout")
}

func TestTaskSkillsSourceEmptyWhenNothing(t *testing.T) {
	url, ref := taskSkillsSource(t.TempDir(), "")
	assert.Empty(t, url)
	assert.Empty(t, ref)
}

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// --- GET /api/agent/task-skills-source handler auth gate ---

// setupTaskSkillsSourceEndpoint creates a test server with the task-skills-source
// route mounted, mirroring setupAutonomousEndpoint.
func setupTaskSkillsSourceEndpoint(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)

	backendClient := backend.NewClient("http://localhost:9090", testBackendAPIKey)
	router := NewRouter(RouterConfig{
		Service:         svc,
		Bus:             bus,
		Backend:         backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: testBackendAPIKey},
	})

	server := httptest.NewServer(router)

	return server, func() {
		server.Close()
		cleanup()
	}
}

func TestGetTaskSkillsSource_HMAC_Valid(t *testing.T) {
	server, cleanup := setupTaskSkillsSourceEndpoint(t)
	defer cleanup()

	path := "/api/agent/task-skills-source"
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body taskSkillsSourceResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	// dir and fallback are both empty in this test fixture - both fields are empty strings.
	assert.Empty(t, body.GitRemoteURL)
	assert.Empty(t, body.Ref)
}

func TestGetTaskSkillsSource_HMAC_Unsigned(t *testing.T) {
	server, cleanup := setupTaskSkillsSourceEndpoint(t)
	defer cleanup()

	req, _ := http.NewRequest("GET", server.URL+"/api/agent/task-skills-source", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInvalidSignature, apiErr.Code)
}

// testChatBackendAPIKey is the dedicated chat backend HMAC key used by the chat
// task-skills-source endpoint tests. Distinct from testBackendAPIKey so a
// wrong-key request proves the endpoint verifies with the chat key, not the
// task backend's.
const testChatBackendAPIKey = "chat-backend-test-key-0123456789abcdef"

// setupChatTaskSkillsSourceEndpoint mounts the chat backend's task-skills-source
// route, authenticated by the dedicated chat backend key. No Backend is wired -
// the chat callback is independent of the active task backend.
func setupChatTaskSkillsSourceEndpoint(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)

	router := NewRouter(RouterConfig{
		Service:        svc,
		Bus:            bus,
		ChatBackendCfg: &config.ChatBackendConfig{APIKey: testChatBackendAPIKey},
	})

	server := httptest.NewServer(router)

	return server, func() {
		server.Close()
		cleanup()
	}
}

func TestGetChatTaskSkillsSource_HMAC_Valid(t *testing.T) {
	server, cleanup := setupChatTaskSkillsSourceEndpoint(t)
	defer cleanup()

	path := "/api/chat/task-skills-source"
	sig, ts := protocol.SignRequestHeaders(testChatBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body taskSkillsSourceResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	// dir and fallback are both empty in this fixture - both fields are empty.
	assert.Empty(t, body.GitRemoteURL)
	assert.Empty(t, body.Ref)
}

func TestGetChatTaskSkillsSource_HMAC_WrongKey(t *testing.T) {
	server, cleanup := setupChatTaskSkillsSourceEndpoint(t)
	defer cleanup()

	path := "/api/chat/task-skills-source"
	// Sign with a different key than the configured chat backend key.
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetChatTaskSkillsSource_HMAC_Unsigned(t *testing.T) {
	server, cleanup := setupChatTaskSkillsSourceEndpoint(t)
	defer cleanup()

	req, _ := http.NewRequest("GET", server.URL+"/api/chat/task-skills-source", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestChatTaskSkillsSource_NotRegisteredWithoutBackend verifies the route is
// absent when no chat backend entry is configured.
func TestChatTaskSkillsSource_NotRegisteredWithoutBackend(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/chat/task-skills-source"
	sig, ts := protocol.SignRequestHeaders(testChatBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- task-skills-source instance token (best-effort, no binding) ---
//
// task-skills is instance-scoped, not project-scoped, so it mints from
// RouterConfig.GitHubTokenProvider directly - never providerForProject.
// Unlike git-credentials (fail-closed on a broken project binding), a mint
// failure here is best-effort: there is no binding to be wrong about, so the
// response still succeeds with the token fields simply omitted.

// TestGetTaskSkillsSource_InstanceProvider_IncludesToken asserts the
// task-backend variant attaches token + token_expires_at when the instance provider mints
// successfully.
func TestGetTaskSkillsSource_InstanceProvider_IncludesToken(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	backendClient := backend.NewClient("http://localhost:9090", testBackendAPIKey)
	fakeExpiry := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	router := NewRouter(RouterConfig{
		Service:             svc,
		Bus:                 bus,
		Backend:             backendClient,
		AgentBackendCfg:     &config.AgentBackendConfig{APIKey: testBackendAPIKey},
		GitHubTokenProvider: &fakeTokenProvider{token: "ghs_instance", expiresAt: fakeExpiry},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/agent/task-skills-source"
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body taskSkillsSourceResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ghs_instance", body.Token)
	assert.Equal(t, fakeExpiry.UTC().Format(time.RFC3339), body.TokenExpiresAt)
}

// TestGetTaskSkillsSource_MintFailure_OmitsTokenBestEffort asserts the
// best-effort asymmetry: a mint failure never fails the request - the
// response is still 200, just without the token fields.
func TestGetTaskSkillsSource_MintFailure_OmitsTokenBestEffort(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	backendClient := backend.NewClient("http://localhost:9090", testBackendAPIKey)
	router := NewRouter(RouterConfig{
		Service:             svc,
		Bus:                 bus,
		Backend:             backendClient,
		AgentBackendCfg:     &config.AgentBackendConfig{APIKey: testBackendAPIKey},
		GitHubTokenProvider: &fakeTokenProvider{err: errors.New("github api returned status 401")},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/agent/task-skills-source"
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode, "mint failure must not fail the whole response")

	var body taskSkillsSourceResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Empty(t, body.Token)
	assert.Empty(t, body.TokenExpiresAt)
}

// TestGetChatTaskSkillsSource_InstanceProvider_IncludesToken mirrors the
// task-backend-variant token test for the dedicated chat backend callback.
func TestGetChatTaskSkillsSource_InstanceProvider_IncludesToken(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	fakeExpiry := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	router := NewRouter(RouterConfig{
		Service:             svc,
		Bus:                 bus,
		ChatBackendCfg:      &config.ChatBackendConfig{APIKey: testChatBackendAPIKey},
		GitHubTokenProvider: &fakeTokenProvider{token: "ghs_chat_instance", expiresAt: fakeExpiry},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/chat/task-skills-source"
	sig, ts := protocol.SignRequestHeaders(testChatBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body taskSkillsSourceResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ghs_chat_instance", body.Token)
	assert.Equal(t, fakeExpiry.UTC().Format(time.RFC3339), body.TokenExpiresAt)
}
