package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/service"
)

func knowledgeServer(t *testing.T, names ...string) (*httptest.Server, *service.CardService) {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	server := httptest.NewServer(newTestRouter(RouterConfig{Service: svc, Bus: bus, KnowledgeProviders: names}))
	t.Cleanup(server.Close)

	return server, svc
}

func TestUpdateProject_Knowledge_SetPreserveClear(t *testing.T) {
	server, _ := knowledgeServer(t, "intent")

	body := validUpdateProjectBody(nil)
	body.KnowledgeProvider = new("intent")
	body.KnowledgeScope = new("payments")

	resp := putProject(t, server.URL, nil, body)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg := decodeProject(t, resp)
	closeBody(t, resp.Body)
	assert.Equal(t, "intent", cfg.KnowledgeProvider)
	assert.Equal(t, "payments", cfg.KnowledgeScope)

	resp = putProject(t, server.URL, nil, validUpdateProjectBody(nil))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg = decodeProject(t, resp)
	closeBody(t, resp.Body)
	assert.Equal(t, "intent", cfg.KnowledgeProvider)
	assert.Equal(t, "payments", cfg.KnowledgeScope)

	body = validUpdateProjectBody(nil)
	body.KnowledgeProvider = new("")
	body.KnowledgeScope = new("")

	resp = putProject(t, server.URL, nil, body)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg = decodeProject(t, resp)
	closeBody(t, resp.Body)
	assert.Empty(t, cfg.KnowledgeProvider)
	assert.Empty(t, cfg.KnowledgeScope)
}

func errCode(t *testing.T, resp *http.Response) string {
	t.Helper()

	defer closeBody(t, resp.Body)

	var e struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&e))

	return e.Code
}

func TestUpdateProject_Knowledge_UnknownProviderRejected(t *testing.T) {
	server, svc := knowledgeServer(t, "intent")

	body := validUpdateProjectBody(nil)
	body.KnowledgeProvider = new("ghost")
	body.KnowledgeScope = new("p")

	resp := putProject(t, server.URL, nil, body)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Equal(t, ErrCodeValidationError, errCode(t, resp))

	cfg, err := svc.GetProject(context.Background(), "test-project")
	require.NoError(t, err)
	assert.Empty(t, cfg.KnowledgeProvider)
	assert.Empty(t, cfg.KnowledgeScope)
}

func TestUpdateProject_Knowledge_ScopeRequired(t *testing.T) {
	server, _ := knowledgeServer(t, "intent")

	body := validUpdateProjectBody(nil)
	body.KnowledgeProvider = new("intent")
	body.KnowledgeScope = new("")

	resp := putProject(t, server.URL, nil, body)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Equal(t, ErrCodeValidationError, errCode(t, resp))
}

func TestUpdateProject_Knowledge_StaleValuePreservedWhenOmitted(t *testing.T) {
	server, svc := knowledgeServer(t, "intent")

	seed := validUpdateProjectBody(nil)

	_, err := svc.UpdateProject(context.Background(), "test-project", service.UpdateProjectInput{
		States:            seed.States,
		Types:             seed.Types,
		Priorities:        seed.Priorities,
		Transitions:       seed.Transitions,
		KnowledgeProvider: new("ghost"),
		KnowledgeScope:    new("p"),
	})
	require.NoError(t, err)

	body := validUpdateProjectBody(nil)
	body.Repo = "https://github.com/org/other.git"

	resp := putProject(t, server.URL, nil, body)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg := decodeProject(t, resp)
	closeBody(t, resp.Body)

	assert.Equal(t, "ghost", cfg.KnowledgeProvider)
	assert.Equal(t, "p", cfg.KnowledgeScope)
}
