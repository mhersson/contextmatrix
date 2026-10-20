package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
)

const oldHealthBody = `{"ok":true,"running_containers":0,"max_concurrent":2}`

func aboutBuild() protocol.BuildInfo {
	return protocol.BuildInfo{
		Commit:    strings.Repeat("c", 40),
		BuildTime: "2026-10-21T08:00:00Z",
		Changes: []protocol.Change{{
			Number: 516, Kind: protocol.ChangeKindFix, Title: "x", MergedAt: "2026-10-20T21:10:00+02:00",
		}},
	}
}

func healthServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	return newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

func getAboutRaw(t *testing.T, cfg RouterConfig) []byte {
	t.Helper()

	server := httptest.NewServer(NewRouter(cfg))
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/about", nil)
	require.NoError(t, err)
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return raw
}

func decodeAbout(t *testing.T, raw []byte) []map[string]json.RawMessage {
	t.Helper()

	var out struct {
		Components []map[string]json.RawMessage `json:"components"`
	}

	require.NoError(t, json.Unmarshal(raw, &out))

	return out.Components
}

func rawString(t *testing.T, m json.RawMessage) string {
	t.Helper()

	var s string

	require.NoError(t, json.Unmarshal(m, &s))

	return s
}

func TestGetAbout_ContextMatrixOnly(t *testing.T) {
	svc, bus := emptyTestSetup(t)

	raw := getAboutRaw(t, RouterConfig{Service: svc, Bus: bus, Build: aboutBuild()})

	var resp aboutResponse

	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Len(t, resp.Components, 1)
	assert.Equal(t, "contextmatrix", resp.Components[0].Name)
	assert.Equal(t, "running", resp.Components[0].Status)
	require.NotNil(t, resp.Components[0].Build)
	assert.Equal(t, strings.Repeat("c", 40), resp.Components[0].Build.Commit)
	assert.Len(t, resp.Components[0].Build.Changes, 1)
}

func TestGetAbout_NilChangesSentAsEmpty(t *testing.T) {
	svc, bus := emptyTestSetup(t)

	raw := getAboutRaw(t, RouterConfig{Service: svc, Bus: bus})

	assert.Contains(t, string(raw), `"changes":[]`)
}

func TestGetAbout_ReportsEachConfiguredBackend(t *testing.T) {
	svc, bus := emptyTestSetup(t)

	agentSrv, _ := healthServer(t, `{"ok":true,"running_containers":1,"max_concurrent":4,`+
		`"build":{"commit":"`+strings.Repeat("a", 40)+`","build_time":"2026-10-21T08:00:00Z","changes":[]},`+
		`"worker_image_revision":"`+strings.Repeat("b", 40)+`"}`)
	chatSrv, _ := healthServer(t, oldHealthBody)

	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	raw := getAboutRaw(t, RouterConfig{
		Service:             svc,
		Bus:                 bus,
		Build:               aboutBuild(),
		Backend:             backend.NewClient(agentSrv.URL, "k"),
		ChatBackendCfg:      &config.ChatBackendConfig{URL: chatSrv.URL, APIKey: "k"},
		ArchitectBackendCfg: &config.ArchitectBackendConfig{URL: deadURL, APIKey: "k"},
	})

	comps := decodeAbout(t, raw)
	require.Len(t, comps, 4)

	names := make([]string, 0, len(comps))
	for _, c := range comps {
		names = append(names, rawString(t, c["name"]))
	}

	assert.Equal(t, []string{"contextmatrix", "agent", "chat", "architect"}, names)

	agent := comps[1]
	assert.Equal(t, "connected", rawString(t, agent["status"]))
	assert.Contains(t, string(agent["build"]), strings.Repeat("a", 40))
	assert.Equal(t, strings.Repeat("b", 40), rawString(t, agent["worker_image_revision"]))

	chat := comps[2]
	assert.Equal(t, "connected", rawString(t, chat["status"]))
	assert.NotContains(t, chat, "build")

	arch := comps[3]
	assert.Equal(t, "unreachable", rawString(t, arch["status"]))
	assert.NotContains(t, arch, "build")
	assert.NotContains(t, string(raw), deadURL)
}

func TestGetAbout_DisabledBackendIsAbsent(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	chatSrv, calls := healthServer(t, oldHealthBody)
	archSrv, archCalls := healthServer(t, oldHealthBody)

	raw := getAboutRaw(t, RouterConfig{
		Service:             svc,
		Bus:                 bus,
		ChatBackendCfg:      &config.ChatBackendConfig{URL: chatSrv.URL, APIKey: "k", Enabled: new(false)},
		ArchitectBackendCfg: &config.ArchitectBackendConfig{URL: archSrv.URL},
	})

	comps := decodeAbout(t, raw)
	require.Len(t, comps, 1)
	assert.Equal(t, int32(0), calls.Load())
	assert.Equal(t, int32(0), archCalls.Load())
}

func TestGetAbout_HungBackendIsBoundedAndParallel(t *testing.T) {
	svc, bus := emptyTestSetup(t)

	release := make(chan struct{})
	agentSrv, _ := newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release

		_, _ = w.Write([]byte(oldHealthBody))
	})
	t.Cleanup(func() { close(release) })

	chatSrv, _ := healthServer(t, oldHealthBody)

	start := time.Now()
	raw := getAboutRaw(t, RouterConfig{
		Service:        svc,
		Bus:            bus,
		Backend:        backend.NewClient(agentSrv.URL, "k"),
		ChatBackendCfg: &config.ChatBackendConfig{URL: chatSrv.URL, APIKey: "k"},
	})

	assert.Less(t, time.Since(start), 5*time.Second)

	comps := decodeAbout(t, raw)
	require.Len(t, comps, 3)
	assert.Equal(t, "unreachable", rawString(t, comps[1]["status"]))
	assert.Equal(t, "connected", rawString(t, comps[2]["status"]))
}

func TestGetAbout_MultiModeNeedsSessionNotAdmin(t *testing.T) {
	server, _, bob := projectsAuthTestServerWithNonAdmin(t)

	do := func(cookie *http.Cookie) int {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/about", nil)
		require.NoError(t, err)
		req.Header.Set("X-Requested-With", "contextmatrix")

		if cookie != nil {
			req.AddCookie(cookie)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		return resp.StatusCode
	}

	assert.Equal(t, http.StatusUnauthorized, do(nil))
	assert.Equal(t, http.StatusOK, do(bob))
}
