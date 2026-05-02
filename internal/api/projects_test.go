package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateProjectStartsSessionPump verifies that a successful POST /api/projects
// starts a long-lived session pump in the session manager before the response is sent.
func TestCreateProjectStartsSessionPump(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	mgr := sessionlog.NewManager()
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	router := NewRouter(RouterConfig{
		Service:        svc,
		Bus:            bus,
		SessionManager: mgr,
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	req := createProjectRequest{
		Name:   "pump-project",
		Prefix: "PMP",
		States: []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:  []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.True(t, mgr.HasProjectSession("pump-project"), "session pump must be active after project creation")
}

// TestCreateProjectNoSessionManager verifies that createProject works normally
// when no session manager is wired (SessionManager: nil).
func TestCreateProjectNoSessionManager(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{
		Service: svc,
		Bus:     bus,
		// SessionManager intentionally nil
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	req := createProjectRequest{
		Name:   "no-mgr-project",
		Prefix: "NMP",
		States: []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:  []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}
