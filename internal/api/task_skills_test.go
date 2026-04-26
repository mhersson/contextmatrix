package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Subdirectory with no SKILL.md — should be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "no-skill-md"), 0o755))

	// Subdirectory with malformed frontmatter — should be skipped.
	badDir := filepath.Join(dir, "bad-frontmatter")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("not a frontmatter doc\n"), 0o644))

	// Subdirectory with invalid name — should be skipped (path safety).
	invalidNameDir := filepath.Join(dir, "Bad Name With Spaces")
	require.NoError(t, os.MkdirAll(invalidNameDir, 0o755))

	// Regular file at the top — should be skipped.
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

	// Bump dir mtime explicitly — writeSkillFile may already do so via
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
