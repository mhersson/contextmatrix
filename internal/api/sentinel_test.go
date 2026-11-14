package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

type fakeSentinelStore struct {
	mu       sync.Mutex
	settings state.Settings
	by       string
	run      *state.Run
	heals    []state.Heal
	findings []state.Finding
	analyses []state.Heal
	pr       *state.StandingPR
	revision *state.Heal
	asked    string // the project LastRevision was asked for
}

func newFakeSentinelStore() *fakeSentinelStore {
	return &fakeSentinelStore{settings: state.DefaultSettings()}
}

func (f *fakeSentinelStore) SentinelSettings(context.Context) (state.Settings, time.Time, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.settings, time.Time{}, f.by, nil
}

func (f *fakeSentinelStore) PutSentinelSettings(_ context.Context, s state.Settings, by string) error {
	if err := s.Validate(); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.settings, f.by = s, by

	return nil
}

func (f *fakeSentinelStore) CardRunRow(context.Context, string, string) (*state.Run, error) {
	return f.run, nil
}

func (f *fakeSentinelStore) CardHeals(context.Context, string, string) ([]state.Heal, error) {
	return f.heals, nil
}

func (f *fakeSentinelStore) CardFindings(context.Context, string, string) ([]state.Finding, error) {
	return f.findings, nil
}

func (f *fakeSentinelStore) CardAnalyses(context.Context, string, string) ([]state.Heal, error) {
	return f.analyses, nil
}

func (f *fakeSentinelStore) StandingPR(context.Context, string) (*state.StandingPR, error) {
	return f.pr, nil
}

func (f *fakeSentinelStore) LastRevision(_ context.Context, project string) (*state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = project

	return f.revision, nil
}

func sentinelDo(t *testing.T, srv *httptest.Server, method, path string, body any, cookie *http.Cookie) (int, map[string]any) {
	t.Helper()

	resp, err := http.DefaultClient.Do(selectorRequest(t, method, srv.URL+path, body, cookie))
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var out map[string]any

	require.NoError(t, json.Unmarshal(raw, &out))

	return resp.StatusCode, out
}

func TestSentinelGetDefaults(t *testing.T) {
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: newFakeSentinelStore()}, false)

	code, body := sentinelDo(t, srv, http.MethodGet, "/api/sentinel", nil, nil)
	require.Equal(t, http.StatusOK, code)

	status := body["status"].(map[string]any)
	assert.Equal(t, false, status["enabled"])
	assert.Equal(t, sentinel.StateOff, status["state"])

	want := state.DefaultSettings()
	settings := body["settings"].(map[string]any)
	assert.InDelta(t, want.DailyCapUSD, settings["daily_cap_usd"], 1e-9)
	assert.InDelta(t, want.Headroom, settings["headroom"], 1e-9)
	assert.Equal(t, want.Healing, settings["healing"])
	assert.Equal(t, false, settings["analyse_every_run"])
	assert.InDelta(t, 1, settings["analyse_ceiling_usd"], 1e-9)
}

func TestSentinelPutRequiresAdminInMultiMode(t *testing.T) {
	on := state.DefaultSettings()
	on.Enabled = true

	t.Run("multi", func(t *testing.T) {
		store := newFakeSentinelStore()
		srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, true)

		bob := login(t, srv, "bob", "bob password1")
		code, _ := sentinelDo(t, srv, http.MethodPut, "/api/admin/sentinel", on, bob)
		assert.Equal(t, http.StatusForbidden, code)
		assert.False(t, store.settings.Enabled)

		root := login(t, srv, "root", "root password1")
		code, body := sentinelDo(t, srv, http.MethodPut, "/api/admin/sentinel", on, root)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "human:root", body["updated_by"])
		assert.True(t, store.settings.Enabled)
	})

	t.Run("none", func(t *testing.T) {
		store := newFakeSentinelStore()
		srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)

		code, body := sentinelDo(t, srv, http.MethodPut, "/api/admin/sentinel", on, nil)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "human:web", body["updated_by"])
		assert.True(t, store.settings.Enabled)
	})
}

func TestSentinelPutRejectsInvalid(t *testing.T) {
	store := newFakeSentinelStore()
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)

	bad := state.DefaultSettings()
	bad.Headroom = 1.5

	code, body := sentinelDo(t, srv, http.MethodPut, "/api/admin/sentinel", bad, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Equal(t, ErrCodeValidationError, body["code"])
	assert.Equal(t, state.DefaultSettings(), store.settings)
}

func TestSentinelPutRejectsAnalyseCeilingZero(t *testing.T) {
	store := newFakeSentinelStore()
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)

	bad := state.DefaultSettings()
	bad.AnalyseCeilingUSD = 0

	code, body := sentinelDo(t, srv, http.MethodPut, "/api/admin/sentinel", bad, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Equal(t, ErrCodeValidationError, body["code"])
	assert.Contains(t, body["error"], "analyse ceiling")
}

func TestSentinelPutPublishesAndNudges(t *testing.T) {
	bus := events.NewBus()
	ch, unsub := bus.Subscribe()

	defer unsub()

	ctl := sentinel.New(sentinel.Config{Store: nil})
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: newFakeSentinelStore(), Sentinel: ctl, Bus: bus}, false)

	code, _ := sentinelDo(t, srv, http.MethodPut, "/api/admin/sentinel", state.DefaultSettings(), nil)
	require.Equal(t, http.StatusOK, code)

	select {
	case ev := <-ch:
		assert.Equal(t, events.SentinelChanged, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("no sentinel.changed event")
	}
}

func TestSentinelGetWithoutControllerIsNotRunning(t *testing.T) {
	store := newFakeSentinelStore()
	store.settings.Enabled = true
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)

	code, body := sentinelDo(t, srv, http.MethodGet, "/api/sentinel", nil, nil)
	require.Equal(t, http.StatusOK, code)

	status := body["status"].(map[string]any)
	assert.Equal(t, sentinel.StateNotRunning, status["state"])
	assert.Equal(t, "no execution backend", status["reason"])
}

func TestCardSentinelRunView(t *testing.T) {
	store := newFakeSentinelStore()
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)
	path := "/api/projects/p/cards/p-001/sentinel"

	code, body := sentinelDo(t, srv, http.MethodGet, path, nil, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "run")
	assert.Nil(t, body["run"])

	started := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	stopped := started.Add(time.Hour)
	store.run = &state.Run{
		StartedAt: started, StoppedAt: &stopped, Attempts: 1, StopCause: "usage_limit", HumanStop: true,
		Launch: state.LaunchFacts{Origin: state.OriginREST, Autonomous: true},
	}

	_, body = sentinelDo(t, srv, http.MethodGet, path, nil, nil)
	run := body["run"].(map[string]any)
	assert.Equal(t, state.OriginREST, run["origin"])
	assert.Equal(t, true, run["autonomous"])
	assert.EqualValues(t, 1, run["attempts"])
	assert.EqualValues(t, state.MaxHealAttempts, run["max_attempts"])
	assert.Equal(t, "2026-11-13T10:00:00Z", run["started_at"])
	assert.Equal(t, "2026-11-13T11:00:00Z", run["stopped_at"])
	assert.Equal(t, "usage_limit", run["stop_cause"])
	assert.Equal(t, true, run["human_stop"])

	store.run = &state.Run{StartedAt: started}
	_, body = sentinelDo(t, srv, http.MethodGet, path, nil, nil)
	run = body["run"].(map[string]any)
	assert.NotContains(t, run, "stop_cause")
	assert.NotContains(t, run, "stopped_at")
}

func findingKinds(body map[string]any) []string {
	var kinds []string
	for _, f := range body["findings"].([]any) {
		kinds = append(kinds, f.(map[string]any)["kind"].(string))
	}

	return kinds
}

func TestCardSentinelViewHealsAndFindings(t *testing.T) {
	store := newFakeSentinelStore()
	at := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	store.heals = []state.Heal{{
		ID: 7, Kind: state.HealKindHeal, Cause: "gave_up", Stage: state.StageHealed, Reason: "fixed",
		Investigation: &protocol.InvestigateResult{Diagnosis: "the criteria were vague"},
		Change: &state.Change{
			SectionBefore: "old", SectionAfter: "new",
			VerifyBefore: &board.VerifyConfig{Command: "make a"}, VerifyAfter: &board.VerifyConfig{Command: "make b"},
			Retired: []string{"P-002"}, Fresh: true,
		},
		Dropped:  []string{"introduces gen-tool"},
		SpendUSD: 0.42, CreatedAt: at, UpdatedAt: at.Add(time.Minute),
	}, {ID: 6, Kind: state.HealKindForward, SourceCard: "P-009", CreatedAt: at, UpdatedAt: at}}
	store.findings = []state.Finding{{HealID: 7, Kind: protocol.FindingGuidance, Title: "t", Text: "x", Evidence: "e", CreatedAt: at}}
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)

	code, body := sentinelDo(t, srv, http.MethodGet, "/api/projects/p/cards/p-001/sentinel", nil, nil)
	require.Equal(t, http.StatusOK, code)

	heals := body["heals"].([]any)
	require.Len(t, heals, 2)

	h := heals[0].(map[string]any)
	assert.EqualValues(t, 7, h["id"])
	assert.Equal(t, "the criteria were vague", h["diagnosis"])
	assert.InDelta(t, 0.42, h["spend_usd"], 1e-9)
	assert.Equal(t, "2026-11-13T10:01:00Z", h["updated_at"])

	change := h["change"].(map[string]any)
	assert.Equal(t, "old", change["section_before"])
	assert.Equal(t, "new", change["section_after"])
	assert.Equal(t, []any{"P-002"}, change["retired"])
	assert.Equal(t, true, change["fresh"])
	assert.Equal(t, "make b", change["verify_after"].(map[string]any)["command"])

	assert.Equal(t, []any{"introduces gen-tool"}, h["dropped"])

	fwd := heals[1].(map[string]any)
	assert.Equal(t, "P-009", fwd["source_card"])
	assert.NotContains(t, fwd, "change")
	assert.Equal(t, []any{}, fwd["dropped"])

	f := body["findings"].([]any)[0].(map[string]any)
	assert.Equal(t, "guidance", f["kind"])
	assert.Equal(t, "e", f["evidence"])
}

func TestCardSentinelViewEmptyListsAreArrays(t *testing.T) {
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: newFakeSentinelStore()}, false)

	_, body := sentinelDo(t, srv, http.MethodGet, "/api/projects/p/cards/p-001/sentinel", nil, nil)
	assert.Equal(t, []any{}, body["heals"])
	assert.Equal(t, []any{}, body["findings"])
}

func TestCardSentinelViewHidesAdminNotesFromNonAdmins(t *testing.T) {
	newStore := func() *fakeSentinelStore {
		store := newFakeSentinelStore()
		store.findings = []state.Finding{
			{Kind: protocol.FindingProjectVerify, Title: "verify"},
			{Kind: protocol.FindingProjectVerify, Title: "rerouted", State: state.FindingNote, Reason: "re-routed: mentions CM"},
			{Kind: protocol.FindingHardCard, Title: "hard"},
		}

		return store
	}
	path := "/api/projects/p/cards/p-001/sentinel"

	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: newStore()}, true)

	_, body := sentinelDo(t, srv, http.MethodGet, path, nil, login(t, srv, "bob", "bob password1"))
	assert.Equal(t, []string{protocol.FindingHardCard}, findingKinds(body))

	_, body = sentinelDo(t, srv, http.MethodGet, path, nil, login(t, srv, "root", "root password1"))
	assert.Equal(t, []string{protocol.FindingProjectVerify, protocol.FindingProjectVerify, protocol.FindingHardCard}, findingKinds(body))

	none := newSelectorAdminServer(t, RouterConfig{SentinelStore: newStore()}, false)

	_, body = sentinelDo(t, none, http.MethodGet, path, nil, nil)
	assert.Equal(t, []string{protocol.FindingProjectVerify, protocol.FindingProjectVerify, protocol.FindingHardCard}, findingKinds(body))
}

func TestCardSentinelViewCarriesAnalysesOutcomesAndThePR(t *testing.T) {
	store := newFakeSentinelStore()
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	store.analyses = []state.Heal{{
		ID: 11, Kind: state.HealKindAnalysis, RunID: "run-1", Stage: state.StageAnalysed, Reason: "answered",
		SpendUSD: 0.31, CreatedAt: at,
	}}
	store.findings = []state.Finding{
		{ID: 5, Kind: protocol.FindingCode, Title: "t", State: state.FindingCarded, Reason: "new", Output: "ABC-40", CreatedAt: at},
		{ID: 6, Kind: protocol.FindingProjectVerify, Title: "v", State: state.FindingProposed, Output: "https://x/pull/1", CreatedAt: at},
	}
	store.pr = &state.StandingPR{Project: "p", URL: "https://x/pull/1", File: "AGENTS.md", State: "open", PushedAt: at}
	path := "/api/projects/p/cards/p-001/sentinel"

	check := func(t *testing.T, body map[string]any, wantFindings int) {
		t.Helper()

		a := body["analyses"].([]any)
		require.Len(t, a, 1)

		row := a[0].(map[string]any)
		assert.EqualValues(t, 11, row["id"])
		assert.Equal(t, "run-1", row["run_id"])
		assert.Equal(t, state.StageAnalysed, row["stage"])
		assert.Equal(t, "answered", row["reason"])
		assert.InDelta(t, 0.31, row["spend_usd"], 1e-9)
		assert.Equal(t, "2026-11-14T09:00:00Z", row["created_at"])

		require.Len(t, body["findings"], wantFindings)

		f := body["findings"].([]any)[0].(map[string]any)
		assert.EqualValues(t, 5, f["id"])
		assert.Equal(t, state.FindingCarded, f["state"])
		assert.Equal(t, "new", f["reason"])
		assert.Equal(t, "ABC-40", f["output"])

		pr := body["standing_pr"].(map[string]any)
		assert.Equal(t, "https://x/pull/1", pr["url"])
		assert.Equal(t, "open", pr["state"])
		assert.Equal(t, "AGENTS.md", pr["file"])
		assert.Equal(t, "2026-11-14T09:00:00Z", pr["pushed_at"])
	}

	none := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)
	_, body := sentinelDo(t, none, http.MethodGet, path, nil, nil)
	check(t, body, 2)

	multi := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, true)
	_, body = sentinelDo(t, multi, http.MethodGet, path, nil, login(t, multi, "bob", "bob password1"))
	check(t, body, 1)

	empty := newSelectorAdminServer(t, RouterConfig{SentinelStore: newFakeSentinelStore()}, false)
	_, body = sentinelDo(t, empty, http.MethodGet, path, nil, nil)
	assert.Equal(t, []any{}, body["analyses"])
	assert.Nil(t, body["standing_pr"])
	assert.Contains(t, body, "standing_pr")
}

func TestCardSentinelViewNamesARevisionThatDidNotGoOut(t *testing.T) {
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	row := func(stage, reason string) *state.Heal {
		return &state.Heal{ID: 3, Kind: state.HealKindRevision, Project: "p", Stage: stage, Reason: reason, UpdatedAt: at}
	}

	tests := []struct {
		name     string
		revision *state.Heal
		want     map[string]any // nil = null
	}{
		{"no revision", nil, nil},
		{"revised", row(state.StageRevised, ""), nil},
		{"still open", row(state.StageRevise, ""), nil},
		{
			"failed", row(state.StageRevisionFailed, "not sent: mint a pull request token: 403"),
			map[string]any{"stage": state.StageRevisionFailed, "reason": "not sent: mint a pull request token: 403", "updated_at": "2026-11-14T09:00:00Z"},
		},
		{
			"refused", row(state.StageRevisionRefused, "absent from the repo: x"),
			map[string]any{"stage": state.StageRevisionRefused, "reason": "absent from the repo: x", "updated_at": "2026-11-14T09:00:00Z"},
		},
		{
			"stale", row(state.StageRevisionStale, "stale"),
			map[string]any{"stage": state.StageRevisionStale, "reason": "stale", "updated_at": "2026-11-14T09:00:00Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeSentinelStore()
			store.revision = tt.revision
			srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: store}, false)

			_, body := sentinelDo(t, srv, http.MethodGet, "/api/projects/p/cards/p-001/sentinel", nil, nil)

			require.Contains(t, body, "guidance_revision")
			assert.Nil(t, body["standing_pr"], "set even with no PR on record")
			assert.Equal(t, "p", store.asked)

			if tt.want == nil {
				assert.Nil(t, body["guidance_revision"])
			} else {
				assert.Equal(t, tt.want, body["guidance_revision"])
			}
		})
	}
}

func TestSentinelStatusCarriesAnalysing(t *testing.T) {
	srv := newSelectorAdminServer(t, RouterConfig{SentinelStore: newFakeSentinelStore()}, false)

	_, body := sentinelDo(t, srv, http.MethodGet, "/api/sentinel", nil, nil)
	assert.EqualValues(t, 0, body["status"].(map[string]any)["analysing"])
}
