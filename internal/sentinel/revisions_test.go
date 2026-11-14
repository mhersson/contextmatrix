package sentinel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

const (
	prURL  = "https://github.com/acme/alpha/pull/7"
	headH2 = "2222222222222222222222222222222222222222"
)

// fakeGuidanceBase fills a guidance_pr payload with a write token, or
// answers err.
type fakeGuidanceBase struct {
	err   error
	calls int
}

func (g *fakeGuidanceBase) base(_ context.Context, project string) (protocol.RunKindPayload, error) {
	g.calls++

	if g.err != nil {
		return protocol.RunKindPayload{}, g.err
	}

	return protocol.RunKindPayload{
		Kind: protocol.RunKindGuidancePR, Project: project, RepoURL: "https://github.com/acme/alpha.git", GitToken: "pr-tok",
	}, nil
}

// revisionEnv is a pass env with GitHub and a guidance base wired and each
// finding stored pending, its evidence set when empty.
func revisionEnv(t *testing.T, gh *fakeGitHub, findings ...state.Finding) (*dispatchEnv, *fakeGuidanceBase) {
	t.Helper()

	e := newPassEnv(t)
	gb := &fakeGuidanceBase{}

	e.ctl.SetGitHub(gh)
	e.ctl.SetGuidanceBase(gb.base)
	e.cards.projects["alpha"] = &board.ProjectConfig{Name: "alpha", Prefix: "ALPHA"}

	for _, f := range findings {
		f.State = state.FindingPending
		if f.Evidence == "" {
			f.Evidence = "seen in a run"
		}

		e.store.putFinding(f)
	}

	return e, gb
}

// putPR stores alpha's standing PR record at prURL with AGENTS.md.
func (e *dispatchEnv) putPR(t *testing.T, rec state.StandingPR) {
	t.Helper()

	rec.Project, rec.URL, rec.File = "alpha", prURL, "AGENTS.md"
	require.NoError(t, e.store.PutStandingPR(context.Background(), rec))
}

func revisionRows(e *dispatchEnv) []state.Heal {
	var rows []state.Heal

	for _, h := range e.store.allHeals() {
		if h.Kind == state.HealKindRevision {
			rows = append(rows, h)
		}
	}

	return rows
}

func revisionStatus(runID string, r protocol.GuidancePRResult) protocol.StatusCallbackPayload {
	return protocol.StatusCallbackPayload{
		Project: "alpha", Kind: protocol.RunKindGuidancePR, RunID: runID, WorkerStatus: "completed",
		Result: &protocol.RunKindResult{GuidancePR: &r},
	}
}

func TestRevisionPushedRecordsThePRAndProposesTheFindings(t *testing.T) {
	ctx := context.Background()
	e, _ := revisionEnv(t, &fakeGitHub{},
		guidance(1, "Run make generate before make test."), guidance(2, "Never edit files under gen/."))
	ch, unsubscribe := e.bus.Subscribe()
	t.Cleanup(unsubscribe)

	require.NoError(t, e.ctl.pass(ctx))

	assert.Contains(t, projectEvents(ch), "alpha", "opening the revision announces the project")
	require.Len(t, e.backend.sent, 1)
	p := e.backend.sent[0]
	assert.Equal(t, protocol.RunKindGuidancePR, p.Kind)
	assert.Empty(t, p.CardID, "a project-level kind")
	assert.Equal(t, "pr-tok", p.GitToken)
	assert.Zero(t, p.MaxCostUSD)
	require.NotNil(t, p.GuidancePR)
	assert.Empty(t, p.GuidancePR.ExpectHead, "no PR yet")
	assert.Equal(t, "Run make generate before make test.\n\nNever edit files under gen/.", p.GuidancePR.Section)
	assert.Equal(t, prTitle("alpha"), p.GuidancePR.Title)
	assert.Empty(t, p.GuidancePR.Allowed)

	rows := revisionRows(e)
	require.Len(t, rows, 1)
	assert.Equal(t, []int64{1, 2}, rows[0].SentFindings)
	assert.Empty(t, rows[0].CardID)
	assert.Equal(t, state.StageRevise, rows[0].Stage)
	assert.Empty(t, rows[0].Dispatch.Mode, "no model runs")

	e.clk.Advance(time.Minute)
	require.NoError(t, e.ctl.RunKindStatus(ctx, revisionStatus("gp-1", protocol.GuidancePRResult{
		Outcome: protocol.GuidancePushed, PRURL: prURL, HeadSHA: headH2, File: "AGENTS.md",
	})))
	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageRevised, e.store.heal(rows[0].ID).Stage)
	assert.Equal(t, state.StandingPR{
		Project: "alpha", URL: prURL, HeadSHA: headH2, File: "AGENTS.md", State: state.PROpen, PushedAt: epoch.Add(time.Minute),
	}, e.pr())

	for _, id := range []int64{1, 2} {
		f := e.store.finding(id)
		assert.Equal(t, state.FindingProposed, f.State)
		assert.Equal(t, rows[0].ID, f.RevisionID)
		assert.Equal(t, prURL, f.Output)
	}
}

func TestRevisionUnchangedKeepsThePushTime(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "h1"}}
	e, _ := revisionEnv(t, gh, guidance(1, "Run make generate before make test."))
	pushed := epoch.Add(-2 * time.Hour)
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PROpen, PushedAt: pushed, ReadAt: epoch})

	require.NoError(t, e.ctl.pass(ctx))
	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, "h1", e.backend.sent[0].GuidancePR.ExpectHead)

	require.NoError(t, e.ctl.RunKindStatus(ctx, revisionStatus("gp-1", protocol.GuidancePRResult{
		Outcome: protocol.GuidanceUnchanged, PRURL: prURL, HeadSHA: "h1", File: "AGENTS.md",
	})))
	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, pushed, e.pr().PushedAt)
	assert.Equal(t, state.FindingProposed, e.store.finding(1).State)
}

func TestRevisionRefusedRetriesAtOnceWithAllowed(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "h1"}}
	g1 := guidance(1, "Deploy only through deploy.example.net.")
	g2 := guidance(2, "Mirror every artefact to new.example.net.")
	g2.CardID = "ALPHA-002"
	e, _ := revisionEnv(t, gh, g1, g2)
	e.cards.cards = append(e.cards.cards,
		&board.Card{ID: "ALPHA-001", Project: "alpha", Title: "Ship it", Body: "The release goes out through deploy.example.net.\n"},
		&board.Card{ID: "ALPHA-002", Project: "alpha", Title: "Mirror the artefacts", Body: "Use the usual mirror.\n"},
	)
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PROpen, PushedAt: epoch.Add(-61 * time.Minute), ReadAt: epoch})

	require.NoError(t, e.ctl.pass(ctx))
	require.Len(t, e.backend.sent, 1)

	require.NoError(t, e.ctl.RunKindStatus(ctx, revisionStatus("gp-1", protocol.GuidancePRResult{
		Outcome: protocol.GuidanceRefused, AbsentFromRepo: []string{"deploy.example.net", "new.example.net"},
	})))
	e.ctl.TakeNudgeForTest()
	require.NoError(t, e.ctl.pass(ctx))

	refused := revisionRows(e)[0]
	assert.Equal(t, state.StageRevisionRefused, refused.Stage)
	assert.Equal(t, []string{"deploy.example.net"}, refused.Allowed)
	assert.Equal(t, state.FindingPending, e.store.finding(1).State)
	assert.Equal(t, state.FindingDropped, e.store.finding(2).State)
	assert.Equal(t, "introduces new.example.net", e.store.finding(2).Reason)
	assert.True(t, e.ctl.TakeNudgeForTest(), "the retry is decided at once")
	assert.Empty(t, e.cards.logged, "a revision never writes a card")

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 2, "the retry is not held by the hour")
	retry := e.backend.sent[1].GuidancePR
	assert.Equal(t, g1.Text, retry.Section)
	assert.Equal(t, []string{"deploy.example.net"}, retry.Allowed)
	assert.Equal(t, "h1", retry.ExpectHead)
	assert.Len(t, revisionRows(e), 2)
}

func TestARefusalKeepsTheEntriesItWasSentWith(t *testing.T) {
	ctx := context.Background()
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Deploy through deploy.example.net and check status.example.net."))
	e.cards.cards = append(e.cards.cards, &board.Card{
		ID: "ALPHA-001", Project: "alpha", Title: "Ship it", Body: "Use deploy.example.net, then status.example.net.\n",
	})

	at := epoch.Add(-time.Minute)
	e.store.putHeal(state.Heal{
		ID: 5, Kind: state.HealKindRevision, Project: "alpha", Stage: state.StageRevise, CreatedAt: at, UpdatedAt: at,
		SentFindings: []int64{1}, Allowed: []string{"deploy.example.net"},
		Dispatch: state.Dispatch{Kind: protocol.RunKindGuidancePR, At: &at, AnsweredRunID: "gp-2"},
		Revision: &protocol.GuidancePRResult{Outcome: protocol.GuidanceRefused, AbsentFromRepo: []string{"status.example.net"}},
	})

	require.NoError(t, e.ctl.pass(ctx))
	assert.Equal(t, []string{"deploy.example.net", "status.example.net"}, e.store.heal(5).Allowed)

	require.NoError(t, e.ctl.pass(ctx))
	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, []string{"deploy.example.net", "status.example.net"}, e.backend.sent[0].GuidancePR.Allowed)
}

func TestRevisionRedactsCardIDsFromSectionAndBody(t *testing.T) {
	f := guidance(1, "Read ABC-12 and ALPHA-3 before you touch the cache.")
	f.Evidence = "ABC-12 stalled twice on the cache"
	e, _ := revisionEnv(t, &fakeGitHub{}, f)
	e.cards.projects["abc"] = &board.ProjectConfig{Name: "abc", Prefix: "ABC"}

	require.NoError(t, e.ctl.pass(context.Background()))

	require.Len(t, e.backend.sent, 1)
	in := e.backend.sent[0].GuidancePR
	assert.Equal(t, "Read a card and a card before you touch the cache.", in.Section)

	for _, text := range []string{in.Section, in.Body} {
		assert.NotContains(t, text, "ABC-12")
		assert.NotContains(t, text, "ALPHA-3")
	}

	assert.Contains(t, in.Body, "a card stalled twice on the cache")
	assert.Equal(t, "Sentinel guidance for alpha", in.Title, "project names stay")
}

func TestRevisionWaitsOnAPRChangedOutsideSentinel(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "someone-else"}}
	e, _ := revisionEnv(t, gh, guidance(1, "Run make generate before make test."))
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PROpen, PushedAt: epoch.Add(-2 * time.Hour)})

	require.NoError(t, e.ctl.pass(ctx))
	assert.Equal(t, state.PRChangedOutside, e.pr().State)

	e.clk.Advance(3 * time.Hour)
	require.NoError(t, e.ctl.pass(ctx))

	assert.Empty(t, e.backend.sent)
	assert.Empty(t, revisionRows(e))
	assert.Equal(t, state.FindingPending, e.store.finding(1).State)

	gh.pull = PullState{}

	e.clk.Advance(prReadEvery)
	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1, "a closed PR frees the project")
	assert.Empty(t, e.backend.sent[0].GuidancePR.ExpectHead)
}

func TestRevisionStartsFreshAfterAMerge(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Merged: true, MergeSHA: "m1"}, file: "Never edit files under gen/.\n", found: true}
	e, _ := revisionEnv(t, gh, guidance(2, "Run make generate before make test."))
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PROpen, PushedAt: epoch.Add(-2 * time.Hour), ReadAt: epoch})
	e.store.putFinding(state.Finding{
		ID: 1, Project: "alpha", CardID: "ALPHA-001", Kind: protocol.FindingGuidance, Text: "Never edit files under gen/.",
		State: state.FindingProposed, RevisionID: 4, Output: prURL,
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, 1, gh.pullCalls, "a revision reads the PR first, however recent the last read")
	assert.Equal(t, state.FindingMerged, e.store.finding(1).State)

	require.Len(t, e.backend.sent, 1)
	in := e.backend.sent[0].GuidancePR
	assert.Empty(t, in.ExpectHead)
	assert.Equal(t, "Run make generate before make test.", in.Section, "merged guidance is on the base already")
}

func TestARevisionTheAgentRefusedEndsFailed(t *testing.T) {
	ctx := context.Background()
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."))
	e.backend.err = fmt.Errorf("%w: invalid_field: section", backend.ErrRunKindRejected)

	require.NoError(t, e.ctl.pass(ctx))

	rows := revisionRows(e)
	require.Len(t, rows, 1)
	assert.Equal(t, state.StageRevisionFailed, rows[0].Stage)
	assert.Equal(t, "the agent refused the guidance_pr run", rows[0].Reason)
	assert.Equal(t, state.Dispatch{}, rows[0].Dispatch)
	assert.Empty(t, e.cards.logged)
	assert.Equal(t, state.FindingPending, e.store.finding(1).State)

	e.clk.Advance(pushEvery - time.Second)
	require.NoError(t, e.ctl.pass(ctx))
	assert.Len(t, e.backend.sent, 1, "a failure holds the project for the hour")

	e.clk.Advance(time.Second)
	require.NoError(t, e.ctl.pass(ctx))
	assert.Len(t, e.backend.sent, 2)
}

func TestAnUnsentRevisionIsSentAgainOnItsOwnRow(t *testing.T) {
	ctx := context.Background()
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."))
	e.backend.err = backend.ErrRunKindUnknown

	require.NoError(t, e.ctl.pass(ctx))

	rows := revisionRows(e)
	require.Len(t, rows, 1)
	assert.Equal(t, state.StageRevise, rows[0].Stage, "nothing ends")
	assert.Equal(t, state.Dispatch{}, rows[0].Dispatch)

	e.backend.err = nil
	e.store.putFinding(state.Finding{
		ID: 2, Project: "alpha", CardID: "ALPHA-001", Kind: protocol.FindingGuidance, Text: "Never edit files under gen/.",
		Evidence: "e", State: state.FindingPending,
	})
	e.clk.Advance(noRunKindsHold - time.Second)
	require.NoError(t, e.ctl.pass(ctx))
	assert.Len(t, e.backend.sent, 1, "the kind is held")

	e.clk.Advance(time.Second)
	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 2)
	rows = revisionRows(e)
	require.Len(t, rows, 1, "the open row is reused")
	assert.Equal(t, []int64{1, 2}, rows[0].SentFindings, "re-packed")
	assert.NotNil(t, rows[0].Dispatch.At)
}

func TestAMintFailureEndsTheRevisionFailed(t *testing.T) {
	ctx := context.Background()
	e, gb := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."))
	gb.err = errors.New("the installation lacks pull_requests write")

	require.NoError(t, e.ctl.pass(ctx))

	assert.Empty(t, e.backend.sent)
	assert.Equal(t, state.FindingPending, e.store.finding(1).State)

	rows := revisionRows(e)
	require.Len(t, rows, 1)
	assert.Equal(t, state.StageRevisionFailed, rows[0].Stage)
	assert.Nil(t, rows[0].Dispatch.At)
	assert.Contains(t, rows[0].Reason, "the installation lacks pull_requests write", "the tab can say why")

	gb.err = nil

	e.clk.Advance(pushEvery - time.Second)

	require.NoError(t, e.ctl.pass(ctx))
	assert.Equal(t, 1, gb.calls, "no token is minted again within the hour")

	e.clk.Advance(time.Second)
	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1)
	rows = revisionRows(e)
	require.Len(t, rows, 2)
	assert.NotNil(t, rows[1].Dispatch.At)
}

func TestAFindingTooLongForTheSectionIsDropped(t *testing.T) {
	// Redaction lengthens it: "X-1" becomes "a card".
	long := guidance(1, strings.Repeat("X-1 ", maxSectionBytes/4))
	e, _ := revisionEnv(t, &fakeGitHub{}, long, guidance(2, "Run make generate before make test."))
	e.cards.projects["x"] = &board.ProjectConfig{Name: "x", Prefix: "X"}

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.FindingDropped, e.store.finding(1).State)
	assert.Equal(t, reasonTooLong, e.store.finding(1).Reason)

	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, "Run make generate before make test.", e.backend.sent[0].GuidancePR.Section)
	assert.Equal(t, []int64{2}, revisionRows(e)[0].SentFindings)
}

func TestAFullPRTakesNoNewFindingUntilItMerges(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "h1"}}
	e, _ := revisionEnv(t, gh, guidance(2, "Run make generate before make test."))
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PROpen, PushedAt: epoch.Add(-2 * time.Hour), ReadAt: epoch})
	e.store.putFinding(state.Finding{
		ID: 1, Project: "alpha", CardID: "ALPHA-001", Kind: protocol.FindingGuidance, Text: strings.Repeat("p", maxSectionBytes-10),
		State: state.FindingProposed, RevisionID: 4, Output: prURL,
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.backend.sent, "the section would propose nothing new")
	assert.Empty(t, revisionRows(e))
	assert.Equal(t, state.FindingPending, e.store.finding(2).State)
}

func TestAStaleRevisionReadsGitHubAgain(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "h1"}}
	e, _ := revisionEnv(t, gh, guidance(1, "Run make generate before make test."))
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PROpen, PushedAt: epoch.Add(-2 * time.Hour), ReadAt: epoch})

	require.NoError(t, e.ctl.pass(ctx))
	require.Len(t, e.backend.sent, 1)

	require.NoError(t, e.ctl.RunKindStatus(ctx, revisionStatus("gp-1", protocol.GuidancePRResult{Outcome: protocol.GuidanceStale})))

	gh.pull = PullState{Open: true, HeadSHA: "someone-else"}

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageRevisionStale, revisionRows(e)[0].Stage)
	assert.True(t, e.pr().ReadAt.IsZero(), "the next pass reads GitHub")
	assert.Equal(t, "h1", e.pr().HeadSHA, "stale stores nothing")

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.PRChangedOutside, e.pr().State)
	assert.Len(t, e.backend.sent, 1)
}

func TestARevisionThatNeverReportedEndsFailed(t *testing.T) {
	e, _ := revisionEnv(t, &fakeGitHub{})
	at := epoch.Add(-settleGrace)
	e.store.putHeal(state.Heal{
		ID: 5, Kind: state.HealKindRevision, Project: "alpha", Stage: state.StageRevise, CreatedAt: at, UpdatedAt: at,
		Dispatch: state.Dispatch{Kind: protocol.RunKindGuidancePR, At: &at},
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	row := e.store.heal(5)
	assert.Equal(t, state.StageRevisionFailed, row.Stage)
	assert.Equal(t, reasonRevisionUnreported, row.Reason)
	assert.False(t, inFlight(row.Dispatch), "a late report matches nothing")
	assert.Empty(t, e.store.charges(), "no model ran")
}

func TestRevisionsWithoutAGuidanceBaseSendNothing(t *testing.T) {
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."))
	e.ctl.SetGuidanceBase(nil)

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.backend.sent)
	assert.Empty(t, revisionRows(e))
	assert.Zero(t, e.backend.healths)
}

func TestRevisionsAreOffWithFriction(t *testing.T) {
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."))
	e.store.settings.Friction = false

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.backend.sent)
	assert.Empty(t, revisionRows(e))
}

func TestARetriedRefusalEndDecidesTheSame(t *testing.T) {
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(2, "Run make generate before make test."))
	e.cards.cards = append(e.cards.cards, &board.Card{ID: "ALPHA-001", Project: "alpha", Title: "Mirror", Body: "Use the usual mirror.\n"})

	// An earlier try at this end dropped finding 1, then failed before saving the
	// row; finding 3 merged while the run was in flight.
	e.store.putFinding(state.Finding{
		ID: 1, Project: "alpha", CardID: "ALPHA-001", Kind: protocol.FindingGuidance, Text: "Mirror to new.example.net.",
		State: state.FindingDropped, Reason: "introduces new.example.net", RevisionID: 5,
	})
	e.store.putFinding(state.Finding{
		ID: 3, Project: "alpha", CardID: "ALPHA-001", Kind: protocol.FindingGuidance, Text: "Fetch from old.example.net.",
		State: state.FindingMerged, RevisionID: 4,
	})

	at := epoch.Add(-time.Minute)
	e.store.putHeal(state.Heal{
		ID: 5, Kind: state.HealKindRevision, Project: "alpha", Stage: state.StageRevise, CreatedAt: at, UpdatedAt: at,
		SentFindings: []int64{1, 2, 3},
		Dispatch:     state.Dispatch{Kind: protocol.RunKindGuidancePR, At: &at, AnsweredRunID: "gp-1"},
		Revision: &protocol.GuidancePRResult{
			Outcome: protocol.GuidanceRefused, AbsentFromRepo: []string{"new.example.net", "old.example.net"},
		},
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.StageRevisionRefused, e.store.heal(5).Stage)
	assert.Equal(t, state.FindingPending, e.store.finding(2).State, "each entry is still traced to its finding")
	assert.Equal(t, "introduces new.example.net", e.store.finding(1).Reason)
	assert.Equal(t, state.FindingMerged, e.store.finding(3).State, "a settled finding keeps its state")
}

func TestAStaleRevisionReadsAReopenedPRAgain(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "h1"}}
	e, _ := revisionEnv(t, gh, guidance(1, "Run make generate before make test."))
	e.putPR(t, state.StandingPR{HeadSHA: "h1", State: state.PRClosed, PushedAt: epoch.Add(-2 * time.Hour), ReadAt: epoch})

	require.NoError(t, e.ctl.pass(ctx))
	require.Len(t, e.backend.sent, 1)
	assert.Empty(t, e.backend.sent[0].GuidancePR.ExpectHead, "a closed record is final and not read")

	require.NoError(t, e.ctl.RunKindStatus(ctx, revisionStatus("gp-1", protocol.GuidancePRResult{Outcome: protocol.GuidanceStale})))
	require.NoError(t, e.ctl.pass(ctx))
	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.PROpen, e.pr().State, "the reopened PR is read again")

	e.clk.Advance(pushEvery)
	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 2)
	assert.Equal(t, "h1", e.backend.sent[1].GuidancePR.ExpectHead)
}

func TestARevisionInFlightEndsWithFrictionOff(t *testing.T) {
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."))
	e.store.settings.Friction = false

	at := epoch.Add(-settleGrace)
	e.store.putHeal(state.Heal{
		ID: 5, Kind: state.HealKindRevision, Project: "alpha", Stage: state.StageRevise, CreatedAt: at, UpdatedAt: at,
		Dispatch: state.Dispatch{Kind: protocol.RunKindGuidancePR, At: &at},
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.StageRevisionFailed, e.store.heal(5).Stage)
	assert.Empty(t, e.backend.sent, "nothing new is sent while friction is off")

	lists := e.backend.lists

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, lists, e.backend.lists, "nothing in flight, so the agent is not listed")
}

func TestRevisionsShareTheWorkerSlots(t *testing.T) {
	beta := guidance(2, "Never edit files under gen/.")
	beta.Project, beta.CardID = "beta", "BETA-001"
	e, _ := revisionEnv(t, &fakeGitHub{}, guidance(1, "Run make generate before make test."), beta)
	e.cards.projects["beta"] = &board.ProjectConfig{Name: "beta", Prefix: "BETA"}

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Len(t, e.backend.sent, 1, "one free slot is left for run kinds")
	assert.Equal(t, 1, e.backend.healths, "the agent's health is read once")
}
