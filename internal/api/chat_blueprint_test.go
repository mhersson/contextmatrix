package api

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/config"
)

const testArchitectKey = "architect-test-key-0123456789abcdef"

// fakeArchitect serves blueprint routes behind the architect's HMAC check,
// replay cache included, and records every request URI it receives.
type fakeArchitect struct {
	*httptest.Server

	mu   sync.Mutex
	uris []string
}

func newFakeArchitect(t *testing.T, routes map[string]http.HandlerFunc) *fakeArchitect {
	t.Helper()

	fa := &fakeArchitect{}
	replay := backend.NewSignatureCache()
	mux := http.NewServeMux()

	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}

	fa.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		fa.uris = append(fa.uris, r.URL.RequestURI())
		fa.mu.Unlock()

		sig := strings.TrimPrefix(r.Header.Get(protocol.SignatureHeader), "sha256=")
		if !protocol.VerifySignatureWithTimestamp(testArchitectKey, r.Method, r.URL.RequestURI(), sig,
			r.Header.Get(protocol.TimestampHeader), nil, time.Minute, replay) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"ok":false,"code":"unauthorized"}`)

			return
		}

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(fa.Close)

	return fa
}

func (fa *fakeArchitect) seen() []string {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	return append([]string(nil), fa.uris...)
}

func okJSON(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, `{}`)
}

// blueprintFixtureOpts wires an architect entry that points at upstreamURL;
// an empty URL leaves the instance without an architect client.
func blueprintFixtureOpts(upstreamURL string) fixtureOpts {
	opts := architectFixtureOpts()
	opts.architectBackendCfg.URL = upstreamURL
	opts.architectBackendCfg.APIKey = testArchitectKey
	opts.liveLinger = 50 * time.Millisecond
	opts.liveBackoff = 10 * time.Millisecond

	return opts
}

// seedBlueprint creates a cold architect session owned by owner; cold is
// also the state End leaves a blueprint in.
func seedBlueprint(t *testing.T, mgr *chat.Manager, owner string) chat.Session {
	t.Helper()

	sess, err := mgr.CreateSession(t.Context(), chat.CreateInput{
		Kind: chat.KindArchitect, Projects: []string{"alpha"}, CreatedBy: owner,
	})
	require.NoError(t, err)

	return sess
}

// blueprintPaths lists every blueprint proxy route for a session id.
func blueprintPaths(id string) []string {
	return []string{
		"/api/chats/" + id + "/blueprint",
		"/api/chats/" + id + "/blueprint/artifacts/need/1",
	}
}

func recorderErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var e APIError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &e), "body: %s", rec.Body.String())

	return e.Code
}

func TestBlueprintView_RelaysTheArchitectBodyUnchanged(t *testing.T) {
	const body = `{"id":"X","seq":7,"pipeline":[],"field_cm_does_not_know":{"a":[1,2]}}`

	fa := newFakeArchitect(t, map[string]http.HandlerFunc{
		"GET /blueprints/{id}": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, body)
		},
	})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	bp := seedBlueprint(t, mgr, "human:web")
	require.Equal(t, chat.StatusCold, bp.Status, "an ended blueprint stays readable")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, "/api/chats/"+bp.ID+"/blueprint", ""))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, body, rec.Body.String(), "bytes pass through, unknown fields included") //nolint:testifylint // byte identity is the contract, JSONEq would hide a re-encode
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), "CM never serves the architect's content type")

	seen := fa.seen()
	require.Len(t, seen, 1)
	assert.True(t, strings.HasPrefix(seen[0], "/blueprints/"+bp.ID+"?nonce="), seen[0])
}

func TestBlueprintArtifact_ForwardsStageAndVersion(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{
		"GET /blueprints/{id}/artifacts/{stage}/{version}": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"stage":"`+r.PathValue("stage")+`","version":`+r.PathValue("version")+`}`)
		},
	})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	bp := seedBlueprint(t, mgr, "human:web")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, "/api/chats/"+bp.ID+"/blueprint/artifacts/solution_design/02", ""))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"stage":"solution_design","version":2}`, rec.Body.String())

	seen := fa.seen()
	require.Len(t, seen, 1)
	assert.True(t, strings.HasPrefix(seen[0], "/blueprints/"+bp.ID+"/artifacts/solution_design/2?nonce="), seen[0])
}

func TestBlueprintArtifact_LargeBodyPassesThroughWhole(t *testing.T) {
	// About 3 MiB, past the 1 MiB cap on CM's other backend reads.
	body := `{"stage":"decomposition","version":1,"body":"` + strings.Repeat("card body ", 300*1024) + `"}`

	fa := newFakeArchitect(t, map[string]http.HandlerFunc{
		"GET /blueprints/{id}/artifacts/{stage}/{version}": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, body)
		},
	})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	bp := seedBlueprint(t, mgr, "human:web")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, "/api/chats/"+bp.ID+"/blueprint/artifacts/decomposition/1", ""))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, len(body), rec.Body.Len())
	assert.Equal(t, sha256.Sum256([]byte(body)), sha256.Sum256(rec.Body.Bytes()))
}

func TestBlueprintArtifact_MalformedSegmentsNeverReachTheArchitect(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{"/": okJSON})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	bp := seedBlueprint(t, mgr, "human:web")

	tests := []struct{ name, stage, version string }{
		{"encoded slash in the stage", "a%2Fb", "1"},
		{"stage with a dot", "need.v1", "1"},
		{"stage too long", strings.Repeat("s", 65), "1"},
		{"version zero", "need", "0"},
		{"negative version", "need", "-1"},
		{"version not a number", "need", "one"},
		{"version past the int range", "need", "99999999999999999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, jsonReq(t, http.MethodGet,
				"/api/chats/"+bp.ID+"/blueprint/artifacts/"+tt.stage+"/"+tt.version, ""))

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Equal(t, ErrCodeBadRequest, recorderErrorCode(t, rec))
		})
	}

	assert.Empty(t, fa.seen())
}

func TestBlueprintView_SameSecondReadsBothPassTheReplayCache(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{"GET /blueprints/{id}": okJSON})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	bp := seedBlueprint(t, mgr, "human:web")

	for i := range 3 {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, "/api/chats/"+bp.ID+"/blueprint", ""))
		require.Equal(t, http.StatusOK, rec.Code, "read %d: %s", i, rec.Body.String())
	}

	seen := fa.seen()
	require.Len(t, seen, 3)
	assert.NotEqual(t, seen[0], seen[1], "each read signs a distinct URI")
	assert.NotEqual(t, seen[1], seen[2], "each read signs a distinct URI")
}

func TestBlueprintProxy_UpstreamAnswers(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantCode int
		wantErr  string
	}{
		{"older architect without the routes", http.StatusNotFound, "404 page not found\n", http.StatusNotFound, ErrCodeBlueprintNotFound},
		{"unknown blueprint or artifact", http.StatusNotFound, `{"ok":false,"code":"not_found"}`, http.StatusNotFound, ErrCodeBlueprintNotFound},
		{"architect error", http.StatusInternalServerError, `{"ok":false,"code":"internal"}`, http.StatusBadGateway, ErrCodeBackendUnavailable},
		{"architect draining", http.StatusServiceUnavailable, `{"ok":false,"code":"draining"}`, http.StatusBadGateway, ErrCodeBackendUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fa := newFakeArchitect(t, map[string]http.HandlerFunc{
				"/": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, tt.body)
				},
			})
			mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
			bp := seedBlueprint(t, mgr, "human:web")

			for _, path := range blueprintPaths(bp.ID) {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, path, ""))

				assert.Equal(t, tt.wantCode, rec.Code, "%s: %s", path, rec.Body.String())
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), path)
				assert.Equal(t, tt.wantErr, recorderErrorCode(t, rec), path)
			}
		})
	}
}

func TestBlueprintProxy_UnreachableArchitectIsBadGateway(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()

	mux, mgr := newChatFixture(t, blueprintFixtureOpts(gone.URL))
	bp := seedBlueprint(t, mgr, "human:web")

	for _, path := range blueprintPaths(bp.ID) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, path, ""))

		assert.Equal(t, http.StatusBadGateway, rec.Code, path)
		assert.Equal(t, ErrCodeBackendUnavailable, recorderErrorCode(t, rec), path)
	}
}

func TestBlueprintProxy_RefusedSignatureIsBadGateway(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{"/": okJSON})
	opts := blueprintFixtureOpts(fa.URL)
	opts.architectBackendCfg.APIKey = "another-key-0123456789abcdef012345"
	mux, mgr := newChatFixture(t, opts)
	bp := seedBlueprint(t, mgr, "human:web")

	for _, path := range blueprintPaths(bp.ID) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, path, ""))

		assert.Equal(t, http.StatusBadGateway, rec.Code, path)
		assert.Equal(t, ErrCodeBackendUnavailable, recorderErrorCode(t, rec), path)
	}

	assert.Len(t, fa.seen(), len(blueprintPaths(bp.ID)), "every request reached the architect and was refused")
}

func TestBlueprintRead_HungArchitectIsBadGatewayAfterTheTimeout(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{
		"/": func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
	})
	opts := blueprintFixtureOpts(fa.URL)
	opts.architectReadTimeout = 50 * time.Millisecond
	mux, mgr := newChatFixture(t, opts)
	bp := seedBlueprint(t, mgr, "human:web")

	for _, path := range []string{"/api/chats/" + bp.ID + "/blueprint", "/api/chats/" + bp.ID + "/blueprint/artifacts/need/1"} {
		start := time.Now()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, path, ""))

		assert.Equal(t, http.StatusBadGateway, rec.Code, path)
		assert.Equal(t, ErrCodeBackendUnavailable, recorderErrorCode(t, rec), path)
		assert.Less(t, time.Since(start), 2*time.Second, path)
	}
}

func TestBlueprintProxy_ChatSessionHasNoBlueprint(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{"/": okJSON})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	plain := seedSession(t, mgr, "human:web")

	for _, path := range blueprintPaths(plain.ID) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, path, ""))

		assert.Equal(t, http.StatusNotFound, rec.Code, path)
		assert.Equal(t, ErrCodeBlueprintNotFound, recorderErrorCode(t, rec), path)
	}

	assert.Empty(t, fa.seen(), "a chat session never reaches the architect")
}

func TestBlueprintProxy_NoArchitectConfigured(t *testing.T) {
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(""))
	bp := seedBlueprint(t, mgr, "human:web")

	for _, path := range blueprintPaths(bp.ID) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, path, ""))

		assert.Equal(t, http.StatusConflict, rec.Code, path)
		assert.Equal(t, ErrCodeChatKindUnavailable, recorderErrorCode(t, rec), path)
	}
}

func TestBlueprintProxy_ForeignBlueprintLooksMissing(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{"/": okJSON})
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	alices := seedBlueprint(t, mgr, "human:alice")
	alicesChat := seedSession(t, mgr, "human:alice")
	bob := asUser(mux, "bob", false)

	missingPaths := blueprintPaths(unknownChatID)

	// A foreign chat session must also look missing: the kind gate runs after ownership.
	for _, foreignID := range []string{alices.ID, alicesChat.ID} {
		for i, path := range blueprintPaths(foreignID) {
			foreign := httptest.NewRecorder()
			bob.ServeHTTP(foreign, jsonReq(t, http.MethodGet, path, ""))

			missing := httptest.NewRecorder()
			bob.ServeHTTP(missing, jsonReq(t, http.MethodGet, missingPaths[i], ""))

			assert.Equal(t, http.StatusNotFound, foreign.Code, path)
			assert.Equal(t, missing.Body.String(), foreign.Body.String(), "%s: foreign 404 must match missing 404", path)
		}
	}

	assert.Empty(t, fa.seen(), "a foreign read never reaches the architect")

	rec := httptest.NewRecorder()
	asUser(mux, "alice", false).ServeHTTP(rec, jsonReq(t, http.MethodGet, "/api/chats/"+alices.ID+"/blueprint", ""))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestRouter_BlueprintRoutesFollowTheArchitectEntry(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{
		"GET /blueprints/{id}": func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"seq":3}`) },
	})
	opts := blueprintFixtureOpts(fa.URL)
	_, mgr := newChatFixture(t, opts)
	bp := seedBlueprint(t, mgr, "human:web")

	disabled := *opts.architectBackendCfg
	disabled.Enabled = new(bool)

	tests := []struct {
		name     string
		archCfg  *config.ArchitectBackendConfig
		wantCode int
		wantBody string
	}{
		{"enabled entry proxies to the architect", opts.architectBackendCfg, http.StatusOK, `{"seq":3}`},
		{"disabled entry leaves the kind unavailable", &disabled, http.StatusConflict, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(NewRouter(RouterConfig{
				ChatManager: mgr, ChatHub: chat.NewSSEHub(8), ArchitectBackendCfg: tt.archCfg,
			}))
			t.Cleanup(server.Close)

			resp, err := http.Get(server.URL + "/api/chats/" + bp.ID + "/blueprint")
			require.NoError(t, err)

			defer closeBody(t, resp.Body)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCode, resp.StatusCode, string(body))

			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, string(body))
			}
		})
	}
}

func TestRouter_ChatModelsWiresArchitectDefaultAndPricing(t *testing.T) {
	opts := blueprintFixtureOpts("http://architect:9094")
	_, mgr := newChatFixture(t, opts)

	disabled := *opts.architectBackendCfg
	disabled.Enabled = new(bool)

	tests := []struct {
		name        string
		archCfg     *config.ArchitectBackendConfig
		wantDefault string
	}{
		{"architect configured", opts.architectBackendCfg, opts.architectBackendCfg.DefaultModel},
		{"architect not configured", &disabled, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(NewRouter(RouterConfig{
				ChatManager: mgr, ChatHub: chat.NewSSEHub(8), ArchitectBackendCfg: tt.archCfg,
				ChatEndpointModels: func(context.Context) ([]EndpointModelView, error) {
					return []EndpointModelView{{ID: "priced"}, {ID: "free"}}, nil
				},
				ChatModelPriced: func(m string) bool { return m == "priced" },
			}))
			t.Cleanup(server.Close)

			resp, err := http.Get(server.URL + "/api/chats/models")
			require.NoError(t, err)

			defer closeBody(t, resp.Body)

			require.Equal(t, http.StatusOK, resp.StatusCode)

			var body modelsBody
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, tt.wantDefault, body.ArchitectDefault)
			require.Len(t, body.Models, 2)
			assert.False(t, body.Models[0].Unpriced)
			assert.True(t, body.Models[1].Unpriced)
		})
	}
}

// readSSEFrame reads one SSE frame, through its blank line, within two
// seconds; a stream that ends early returns what arrived.
func readSSEFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()

	got := make(chan string, 1)

	go func() {
		var b strings.Builder

		for {
			line, err := r.ReadString('\n')
			b.WriteString(line)

			if err != nil || line == "\n" {
				got <- b.String()

				return
			}
		}
	}()

	select {
	case s := <-got:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no SSE frame within 2s")

		return ""
	}
}

func TestBlueprintLive_NoBrowserFacingRoute(t *testing.T) {
	fa := newFakeArchitect(t, map[string]http.HandlerFunc{"/": okJSON})
	_, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	bp := seedBlueprint(t, mgr, "human:web")

	opts := blueprintFixtureOpts(fa.URL)
	srv := httptest.NewServer(NewRouter(RouterConfig{
		ChatManager: mgr, ChatHub: chat.NewSSEHub(8), ArchitectBackendCfg: opts.architectBackendCfg,
	}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/chats/" + bp.ID + "/blueprint/live")
	require.NoError(t, err)
	closeBody(t, resp.Body)

	assert.Empty(t, fa.seen(), "the architect's live stream is reached only through the chat SSE")
}
