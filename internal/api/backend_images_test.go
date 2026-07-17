package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
)

// newImagesBackendServer serves a canned /images response for the given tags.
func newImagesBackendServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/images", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv
}

const imagesBody = `{"ok":true,"images":[{"tags":["contextmatrix-agent-worker:go-node"],"created":1750000000}]}`

func TestBackendImages_Agent(t *testing.T) {
	upstream := newImagesBackendServer(t, http.StatusOK, imagesBody)

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus,
		Backend: backend.NewClient(upstream.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"),
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/api/backends/agent/images")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var parsed struct {
		OK     bool `json:"ok"`
		Images []struct {
			Tags    []string `json:"tags"`
			Created int64    `json:"created"`
		} `json:"images"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	assert.True(t, parsed.OK)
	require.Len(t, parsed.Images, 1)
	assert.Equal(t, []string{"contextmatrix-agent-worker:go-node"}, parsed.Images[0].Tags)
	assert.Equal(t, int64(1750000000), parsed.Images[0].Created)
}

func TestBackendImages_Chat(t *testing.T) {
	upstream := newImagesBackendServer(t, http.StatusOK,
		`{"ok":true,"images":[{"tags":["contextmatrix-chat-worker:dev"]}]}`)

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus,
		ChatBackendCfg: &config.ChatBackendConfig{
			URL:    upstream.URL,
			APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
		},
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/api/backends/chat/images")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBackendImages_UnknownBackend404(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/api/backends/runner/images")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestBackendImages_NotConfigured503(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	t.Cleanup(server.Close)

	for _, name := range []string{"agent", "chat"} {
		resp := doGet(t, server.URL+"/api/backends/"+name+"/images")
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, name)
		closeBody(t, resp.Body)
	}
}

func TestBackendImages_UpstreamError502(t *testing.T) {
	upstream := newImagesBackendServer(t, http.StatusInternalServerError,
		`{"ok":false,"code":"internal","message":"boom"}`)

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus,
		Backend: backend.NewClient(upstream.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"),
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/api/backends/agent/images")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestImagesProbeCache_CachesSuccessForTTL(t *testing.T) {
	srv, calls := newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(imagesBody))
	})

	client := backend.NewClient(srv.URL, "test-key")

	var cache imagesProbeCache

	images, err := cache.get(context.Background(), client)
	require.NoError(t, err)
	require.Len(t, images, 1)

	_, err = cache.get(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load(), "second call within TTL should be served from cache")
}

// TestBackendImages_NonAdminForbidden_MultiMode mirrors
// TestProjectMutations_NonAdminForbidden_MultiMode's session harness: a
// logged-in non-admin must be rejected with 403 in multi mode, and an admin
// session must reach the real handler (200 if a backend is configured, 503
// if not - either proves the gate passed).
func TestBackendImages_NonAdminForbidden_MultiMode(t *testing.T) {
	server, admin, bob := projectsAuthTestServerWithNonAdmin(t)

	bobReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/backends/agent/images", nil)
	require.NoError(t, err)
	bobReq.Header.Set("X-Requested-With", "contextmatrix")
	bobReq.AddCookie(bob)

	bobResp, err := http.DefaultClient.Do(bobReq)
	require.NoError(t, err)
	closeBody(t, bobResp.Body)
	assert.Equal(t, http.StatusForbidden, bobResp.StatusCode)

	adminReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/backends/agent/images", nil)
	require.NoError(t, err)
	adminReq.Header.Set("X-Requested-With", "contextmatrix")
	adminReq.AddCookie(admin)

	adminResp, err := http.DefaultClient.Do(adminReq)
	require.NoError(t, err)
	closeBody(t, adminResp.Body)
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, adminResp.StatusCode)
}
