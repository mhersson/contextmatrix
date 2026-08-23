package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// TestPatchCard_DependsOn drives PATCH depends_on through the real API router
// and card service: setting, clearing, leaving unchanged, and the two
// rejection paths (unknown dependency ID, self-reference) that
// runValidatorsAndDeps enforces on every non-skipValidators mutation. Set and
// clear also assert the "dependencies_updated" activity-log entry the
// service now writes on a real change; omit asserts no such entry is added.
func TestPatchCard_DependsOn(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	depTarget, err := svc.CreateCard(t.Context(), "test-project", service.CreateCardInput{
		Title:    "Dependency target",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	tests := []struct {
		name            string
		seedDependsOn   []string
		patchFn         func(selfID string) *[]string
		wantStatus      int
		wantDependsOnFn func(selfID string) []string
		wantLogEntry    bool
	}{
		{
			name:            "set a valid list",
			patchFn:         func(string) *[]string { return &[]string{depTarget.ID} },
			wantStatus:      http.StatusOK,
			wantDependsOnFn: func(string) []string { return []string{depTarget.ID} },
			wantLogEntry:    true,
		},
		{
			name:            "clear with empty list",
			seedDependsOn:   []string{depTarget.ID},
			patchFn:         func(string) *[]string { return &[]string{} },
			wantStatus:      http.StatusOK,
			wantDependsOnFn: func(string) []string { return nil },
			wantLogEntry:    true,
		},
		{
			name:            "omit leaves existing list unchanged",
			seedDependsOn:   []string{depTarget.ID},
			patchFn:         func(string) *[]string { return nil },
			wantStatus:      http.StatusOK,
			wantDependsOnFn: func(string) []string { return []string{depTarget.ID} },
			wantLogEntry:    false,
		},
		{
			name:       "unknown dependency ID",
			patchFn:    func(string) *[]string { return &[]string{"TEST-9999"} },
			wantStatus: http.StatusConflict,
		},
		{
			name:       "self-reference",
			patchFn:    func(selfID string) *[]string { return &[]string{selfID} },
			wantStatus: http.StatusConflict,
		},
		{
			name: "over the depends_on limit",
			patchFn: func(string) *[]string {
				list := make([]string, 51)
				for i := range list {
					list[i] = fmt.Sprintf("TEST-%d", i+1)
				}

				return &list
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, err := svc.CreateCard(t.Context(), "test-project", service.CreateCardInput{
				Title:     "Subject: " + tt.name,
				Type:      "task",
				Priority:  "medium",
				DependsOn: tt.seedDependsOn,
			})
			require.NoError(t, err)

			logCountBefore := len(mustGetCard(t, svc, card.ID).ActivityLog)

			body := patchCardRequest{DependsOn: tt.patchFn(card.ID)}

			jsonBody, err := json.Marshal(body)
			require.NoError(t, err)

			req, err := http.NewRequest(http.MethodPatch,
				server.URL+"/api/projects/test-project/cards/"+card.ID, bytes.NewReader(jsonBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer closeBody(t, resp.Body)

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.wantStatus != http.StatusOK {
				return
			}

			var got board.Card

			require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
			assertDependsOn(t, tt.wantDependsOnFn(card.ID), got.DependsOn)

			stored := mustGetCard(t, svc, card.ID)
			assertDependsOn(t, tt.wantDependsOnFn(card.ID), stored.DependsOn)

			wantLogCount := logCountBefore
			if tt.wantLogEntry {
				wantLogCount++
			}

			require.Len(t, stored.ActivityLog, wantLogCount)

			if tt.wantLogEntry {
				last := stored.ActivityLog[len(stored.ActivityLog)-1]
				assert.Equal(t, "dependencies_updated", last.Action)
			}
		})
	}
}

// mustGetCard fetches a card via the service, failing the test on error.
func mustGetCard(t *testing.T, svc *service.CardService, cardID string) *board.Card {
	t.Helper()

	card, err := svc.GetCard(t.Context(), "test-project", cardID)
	require.NoError(t, err)

	return card
}

// assertDependsOn compares depends_on lists, treating nil and a non-nil
// empty slice as equal. The "clear" case normalizes to a non-nil empty
// slice (matching the Labels convention), while a JSON round-trip through
// the wire's `omitempty` tag turns that same empty list back into nil - both
// represent "no dependencies" and neither is a regression.
func assertDependsOn(t *testing.T, want, got []string) {
	t.Helper()
	assert.ElementsMatch(t, want, got)
}
