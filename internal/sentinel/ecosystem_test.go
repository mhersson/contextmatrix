package sentinel

import (
	"context"
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func held(id int64, project, title, text string) state.Finding {
	return state.Finding{
		ID: id, Project: project, CardID: "X-001", Kind: protocol.FindingEcosystem, Title: title, Text: text,
		Evidence: "ev", State: state.FindingHeld,
	}
}

func ecosystemEnv(t *testing.T, ecosystem, friction bool, findings ...state.Finding) *dispatchEnv {
	t.Helper()

	e := routeEnv(t, findings...)
	s := enabledSettings()
	s.Ecosystem, s.Friction = ecosystem, friction
	e.store.set(s)

	return e
}

const upstreamURL = "https://github.com/mhersson/contextmatrix"

func TestRouteHeldFindingsDraftsAnIssue(t *testing.T) {
	e := ecosystemEnv(t, true, true, held(1, "shop", "Verify hangs on SHOP-3", "the shop verify step never returns when the worker restarts"))
	e.cards.projects["shop"] = &board.ProjectConfig{Name: "shop", Prefix: "SHOP"}

	require.NoError(t, e.ctl.pass(context.Background()))

	f := e.store.finding(1)
	require.Equal(t, state.FindingDrafted, f.State)

	title, body := decode(t, f.Output)
	assert.NotContains(t, title+body, "shop")
	assert.NotContains(t, title+body, "SHOP")
}

func TestRouteHeldFindingsWaitsForTheSwitch(t *testing.T) {
	e := ecosystemEnv(t, false, true, held(1, "alpha", "Verify hangs", "the verify step never returns when the worker restarts"))

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, state.FindingHeld, e.store.finding(1).State)
	assert.Zero(t, e.store.outcomeWrites)

	s := enabledSettings()
	s.Ecosystem = true
	e.store.set(s)

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, state.FindingDrafted, e.store.finding(1).State)
}

func TestRouteHeldFindingsRoutesWithFrictionOff(t *testing.T) {
	e := ecosystemEnv(t, true, false, held(1, "alpha", "Verify hangs", "the verify step never returns when the worker restarts"))

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, state.FindingDrafted, e.store.finding(1).State)
}

func upstreamEnv(t *testing.T, findings ...state.Finding) *dispatchEnv {
	t.Helper()

	e := ecosystemEnv(t, true, true, findings...)
	e.cards.projects["cm"] = &board.ProjectConfig{Name: "cm", Repo: upstreamURL, Types: []string{"task"}, Priorities: []string{"medium"}}

	return e
}

func TestRouteHeldFindingsCardsIntoTheUpstreamProject(t *testing.T) {
	e := upstreamEnv(t,
		held(1, "app", "Verify hangs", "the verify step never returns when the worker restarts"),
		held(2, "app", "Leaky log", "the run log prints hunter2hunter2 in a stack trace of its own"),
	)
	e.ctl.cfg.Secrets = func() []string { return []string{"hunter2hunter2"} }

	require.NoError(t, e.ctl.pass(context.Background()))

	require.Len(t, e.cards.created, 1)
	assert.Equal(t, []string{frictionLabel}, e.cards.created[0].Labels)
	require.NotNil(t, e.cards.created[0].Autonomous)
	assert.False(t, *e.cards.created[0].Autonomous)

	f := e.store.finding(1)
	assert.Equal(t, state.FindingCarded, f.State)
	assert.Equal(t, "cm", f.OutputProject)
	assert.NotContains(t, f.Output, "https://")

	dropped := e.store.finding(2)
	assert.Equal(t, state.FindingDropped, dropped.State)
	assert.Equal(t, secretReason, dropped.Reason)
}

func TestRouteHeldFindingsCardsOnlyWithinTheBoardsRepo(t *testing.T) {
	e := upstreamEnv(t,
		held(1, "app", "Verify hangs", "the verify step never returns when the worker restarts"),
		held(2, "notes", "Clone stalls", "the clone step stalls whenever the remote answers slowly"),
	)
	e.cards.projects["app"] = &board.ProjectConfig{Name: "app", Prefix: "APP"}
	e.cards.projects["notes"] = &board.ProjectConfig{Name: "notes", Prefix: "NOTES"}
	repos := map[string]string{"cm": "team", "app": "team", "alpha": "team", "notes": "private"}
	e.ctl.cfg.RepoOf = func(project string) (string, bool) {
		r, ok := repos[project]

		return r, ok
	}

	require.NoError(t, e.ctl.pass(context.Background()))

	require.Len(t, e.cards.created, 1)
	assert.Equal(t, state.FindingCarded, e.store.finding(1).State)

	f := e.store.finding(2)
	require.Equal(t, state.FindingDrafted, f.State)

	title, body := decode(t, f.Output)
	assert.NotContains(t, title+body, "notes")
}

func TestRouteHeldFindingsHoldsAtTheUpstreamLimit(t *testing.T) {
	e := upstreamEnv(t, held(1, "app", "Verify hangs", "the verify step never returns when the worker restarts"))

	for i := range maxFrictionCardsPerDay {
		e.store.putFinding(state.Finding{
			ID: int64(100 + i), Project: "app", Kind: "code", State: state.FindingCarded, OutputProject: "cm", UpdatedAt: epoch,
		})
	}

	require.NoError(t, e.ctl.pass(context.Background()))

	f := e.store.finding(1)
	assert.Equal(t, state.FindingHeld, f.State)
	assert.Equal(t, limitReason, f.Reason)
	assert.Empty(t, e.cards.created)

	writes := e.store.outcomeWrites
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, writes, e.store.outcomeWrites, "a limit-held finding is not rewritten")
}

func TestRouteHeldFindingsDedupsWithinOnePass(t *testing.T) {
	text := "the verify step never returns when the worker restarts mid run"
	e := ecosystemEnv(t, true, true, held(1, "alpha", "Verify hangs", text), held(2, "alpha", "Verify hangs", text))

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.FindingDrafted, e.store.finding(1).State)
	dup := e.store.finding(2)
	assert.Equal(t, state.FindingDuplicate, dup.State)
	assert.Equal(t, "like finding 1", dup.Reason)
}

func TestRouteHeldFindingsBlocksACredentialedURL(t *testing.T) {
	const url = "https://ci:glpat-abcdefgh1234@git.acme.internal/acme/shop.git"

	e := ecosystemEnv(t, true, true, held(1, "shop", "Clone fails", "cloning "+url+" fails after the worker restarts mid run"))
	e.cards.projects["shop"] = &board.ProjectConfig{Name: "shop", Prefix: "SHOP", Repo: url}

	require.NoError(t, e.ctl.pass(context.Background()))

	f := e.store.finding(1)
	assert.Equal(t, state.FindingBlocked, f.State)
	assert.Equal(t, secretReason, f.Reason)
	assert.Empty(t, f.Output)
}

func TestRouteHeldFindingsReadsNothingWithoutHeldFindings(t *testing.T) {
	e := ecosystemEnv(t, true, true)
	before := e.cards.calls

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, before, e.cards.calls)
}
