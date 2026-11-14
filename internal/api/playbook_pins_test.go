package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
)

func pinTestRouter(t *testing.T) http.Handler {
	t.Helper()

	svc, pbSvc, bus, cleanup := playbookTestSetup(t)
	t.Cleanup(cleanup)

	store, err := opsqlite.Open(t.TempDir() + "/ops.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	for _, title := range []string{"Alpha", "Beta"} {
		_, err = pbSvc.Create(t.Context(), service.CreatePlaybookInput{Title: title, AgentID: "human:a"})
		require.NoError(t, err)
	}

	return newTestRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc, PlaybookPins: store})
}

func pinReq(h http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, http.NoBody)
	req.Header.Set("X-Requested-With", "contextmatrix")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	return w
}

func pinnedIDs(t *testing.T, h http.Handler) []string {
	t.Helper()

	w := pinReq(h, http.MethodGet, "/api/playbooks")
	require.Equal(t, http.StatusOK, w.Code)

	var list []service.PlaybookSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))

	var ids []string

	for _, p := range list {
		if p.Pinned {
			ids = append(ids, p.ID)
		}
	}

	return ids
}

func TestPlaybookPins_ListMarksViewerPins(t *testing.T) {
	h := pinTestRouter(t)

	assert.Empty(t, pinnedIDs(t, h))
	assert.Equal(t, http.StatusNoContent, pinReq(h, http.MethodPut, "/api/playbooks/alpha/pin").Code)
	assert.Equal(t, http.StatusNoContent, pinReq(h, http.MethodPut, "/api/playbooks/alpha/pin").Code, "idempotent")
	assert.Equal(t, []string{"alpha"}, pinnedIDs(t, h))
	assert.Equal(t, http.StatusNoContent, pinReq(h, http.MethodDelete, "/api/playbooks/alpha/pin").Code)
	assert.Empty(t, pinnedIDs(t, h))
}

func TestPlaybookPins_MultiModeIsPerUser(t *testing.T) {
	h := pinTestRouter(t)
	alice, bob := asUser(h, "alice", false), asUser(h, "bob", false)

	require.Equal(t, http.StatusNoContent, pinReq(alice, http.MethodPut, "/api/playbooks/alpha/pin").Code)

	assert.Equal(t, []string{"alpha"}, pinnedIDs(t, alice))
	assert.Empty(t, pinnedIDs(t, bob))
}

func TestPlaybookPins_NoneModeIsShared(t *testing.T) {
	h := pinTestRouter(t)

	require.Equal(t, http.StatusNoContent, pinReq(h, http.MethodPut, "/api/playbooks/beta/pin").Code)

	// A second client in none mode carries no identity either, so it sees the same pins.
	assert.Equal(t, []string{"beta"}, pinnedIDs(t, h))
}

func TestPlaybookPins_UnknownPlaybook404(t *testing.T) {
	h := pinTestRouter(t)

	for _, m := range []string{http.MethodPut, http.MethodDelete} {
		w := pinReq(h, m, "/api/playbooks/nope/pin")
		assert.Equal(t, http.StatusNotFound, w.Code, m)
		assert.Contains(t, w.Body.String(), ErrCodePlaybookNotFound, m)
	}
}
