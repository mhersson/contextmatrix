package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeAPI_ListEmpty(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "test-project")
}

// TestKnowledgeAPI_ListJSONShape verifies the wire shape uses snake_case
// field names that the web UI consumes (project, repos, name, docs,
// human_edited, last_built_*). Without JSON tags on the service-layer
// types, Go's default marshaling produces capitalized fields and the
// frontend crashes with "can't access property length, t.repos is undefined".
func TestKnowledgeAPI_ListJSONShape(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.WriteKnowledgeDocs(ctx, service.WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "# A\n"},
		Source:     service.KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Snake_case keys the frontend expects.
	for _, key := range []string{`"project"`, `"repos"`, `"name"`, `"docs"`, `"human_edited"`, `"last_built_at"`, `"last_built_commit"`} {
		assert.Contains(t, bodyStr, key, "expected snake_case key %s in JSON body", key)
	}

	// Capitalized Go field names must NOT leak through (default marshaling).
	for _, key := range []string{`"Project"`, `"Repos"`, `"Name"`, `"Docs"`, `"HumanEdited"`, `"LastBuiltAt"`, `"LastBuiltCommit"`} {
		assert.NotContains(t, bodyStr, key, "unexpected capitalized key %s in JSON body", key)
	}
}

func TestKnowledgeAPI_GetDocAfterWrite(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.WriteKnowledgeDocs(ctx, service.WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "# A\n"},
		Source:     service.KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge/core/architecture.md")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got struct {
		Content string `json:"content"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "# A\n", got.Content)
}

func TestKnowledgeAPI_PutSetsHumanEdited(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	// Seed an initial doc so ReadKnowledgeDoc has something to read back.
	_, err := svc.WriteKnowledgeDocs(ctx, service.WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "# Original\n"},
		Source:     service.KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{"content": "# Hand-edited\n"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/architecture.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	doc, err := svc.ReadKnowledgeDoc(ctx, "test-project", "core", "architecture.md")
	require.NoError(t, err)
	assert.Equal(t, "# Hand-edited\n", doc.Content)
	assert.True(t, doc.Meta.HumanEdited)
}

func TestKnowledgeAPI_PutFallsBackToHumanWeb(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{"content":"x"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/architecture.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	// no X-Agent-ID — UI traffic doesn't supply one and gets the human:web default

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestKnowledgeAPI_PutRequiresCSRFHeader(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{"content": "x"}`)
	req, err := http.NewRequest(http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/architecture.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// intentionally omit X-Requested-With; use rawHTTPClient so the
	// test transport doesn't auto-inject the header.

	resp, err := rawHTTPClient().Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// configureProjectRepo PUTs an updated project config that sets the singular
// Repo field. EffectiveRepos() then surfaces a one-entry list with Name
// derived from the URL (the suffix after the last "/" or ":", with ".git"
// stripped). Used by tests that need a configured repo on the project.
func configureProjectRepo(t *testing.T, baseURL, project, repoURL string) {
	t.Helper()

	body, err := json.Marshal(updateProjectRequest{
		Repo:       repoURL,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
		baseURL+"/api/projects/"+project, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestKnowledgeAPI_ListIncludesConfiguredRepos covers the empty-state UX bug:
// a freshly-deployed project with a repo in .board.yaml but no KB built yet
// must surface that repo as a stub entry so the web UI can show a Refresh
// button. Without this, the sidebar is hidden entirely and the user has no
// way to trigger the first refresh.
func TestKnowledgeAPI_ListIncludesConfiguredRepos(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	configureProjectRepo(t, server.URL, "test-project", "git@github.com:o/core.git")

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got service.KnowledgeBaseSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

	assert.Equal(t, "test-project", got.Project)
	require.Len(t, got.Repos, 1, "configured repo must appear even with no docs built")
	assert.Equal(t, "core", got.Repos[0].Name)
	assert.Empty(t, got.Repos[0].Docs, "stub repo must have empty docs list")
	assert.Empty(t, got.Repos[0].LastBuiltCommit, "stub repo must have no built commit")
	assert.Nil(t, got.Repos[0].LastBuiltAt, "stub repo must omit last_built_at")
}

// TestKnowledgeAPI_ListMergesBuiltAndConfigured: when one repo has docs and a
// second repo is configured but unbuilt, both must appear in the response with
// their respective populated/stub state, sorted alphabetically.
func TestKnowledgeAPI_ListMergesBuiltAndConfigured(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	configureProjectRepo(t, server.URL, "test-project", "git@github.com:o/core.git")

	_, err := svc.WriteKnowledgeDocs(context.Background(), service.WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "edge",
		Docs:       map[string]string{"architecture.md": "# A\n"},
		Source:     service.KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got service.KnowledgeBaseSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

	require.Len(t, got.Repos, 2)

	core := got.Repos[0]
	edge := got.Repos[1]

	assert.Equal(t, "core", core.Name)
	assert.Empty(t, core.Docs)
	assert.Nil(t, core.LastBuiltAt)

	assert.Equal(t, "edge", edge.Name)
	require.Len(t, edge.Docs, 1)
	assert.Equal(t, "architecture.md", edge.Docs[0].Name)
	require.NotNil(t, edge.LastBuiltAt)
	assert.False(t, edge.LastBuiltAt.IsZero())
}

func TestKnowledgeAPI_ListReturns404ForUnknownProject(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/nope/knowledge")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestKnowledgeAPI_PutReturns400ForInvalidDocName(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{"content":"x"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/notavaliddoc.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestKnowledgeAPI_PutRejectsEmptyContent(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{"content":""}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/architecture.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestKnowledgeAPI_PutInvalidJSONReturnsAPIError(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{not json`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/architecture.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}

func TestKnowledgeAPI_PutWrongMethod(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// Use httptest.NewRequest + ServeHTTP directly to hit the mux without a
	// network round-trip. The mux returns 405 when a path is registered but
	// the requested method is not.
	mux := NewRouter(RouterConfig{Service: svc, Bus: bus})
	req := httptest.NewRequest(http.MethodPost,
		"/api/projects/test-project/knowledge/core/architecture.md",
		strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:alice")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	// stdlib mux returns 405 for a known path hit with the wrong method.
	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func TestKnowledgeAPI_PutPathTraversalRejected(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// Use httptest.NewRequest with a URL-encoded slash (%2F) in the doc segment.
	// httptest.NewRequest preserves RawPath, so the mux routes the request and
	// r.PathValue("doc") returns "../bad" (with a literal slash). The storage
	// layer then rejects it via validatePathComponent → ErrInvalidPath → 400.
	mux := NewRouter(RouterConfig{Service: svc, Bus: bus})
	req := httptest.NewRequest(http.MethodPut,
		"/api/projects/test-project/knowledge/core/..%2Fbad",
		strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:alice")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	// doc name containing "/" must be rejected with 400 (invalid path component).
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestKnowledgeAPI_PutHonorsXAgentID(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	// Seed an initial doc so the PUT has something to overwrite.
	_, err := svc.WriteKnowledgeDocs(ctx, service.WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "# Original\n"},
		Source:     service.KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewBufferString(`{"content":"# Human edited\n"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		server.URL+"/api/projects/test-project/knowledge/core/architecture.md", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:bob")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	// X-Agent-ID is optional for KB PUT (UI traffic gets the human:web fallback);
	// when supplied it is honored as the audit identity. Deeper audit-trail
	// verification (git log) is covered by the WriteKnowledgeDocs service-layer
	// tests.
	require.Equal(t, http.StatusOK, resp.StatusCode)

	doc, readErr := svc.ReadKnowledgeDoc(ctx, "test-project", "core", "architecture.md")
	require.NoError(t, readErr)
	assert.Equal(t, "# Human edited\n", doc.Content)
	assert.True(t, doc.Meta.HumanEdited)
}
