package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCreateProject_BoardsRepo(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	post := func(name, repo string) *http.Response {
		t.Helper()

		payload := map[string]any{
			"name": name, "prefix": name, "boards_repo": repo,
			"states": []string{"todo", "in_progress", "done", "stalled", "not_planned"},
			"types":  []string{"task"}, "priorities": []string{"low"},
			"transitions": map[string][]string{
				"todo": {"in_progress"}, "in_progress": {"done", "todo"}, "done": {"todo"},
				"stalled": {"todo", "in_progress"}, "not_planned": {"todo"},
			},
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		req, _ := http.NewRequest("POST", server.URL+"/api/projects", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	resp := post("gamma", "boards")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.Equal(t, "boards", cfg.BoardsRepo)

	bad := post("delta", "nope")
	defer closeBody(t, bad.Body)

	assert.Equal(t, http.StatusBadRequest, bad.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(bad.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}
