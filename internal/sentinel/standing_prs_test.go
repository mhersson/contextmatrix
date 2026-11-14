package sentinel

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

type fakeGitHub struct {
	pull       PullState
	pullErr    error
	file       string
	found      bool
	pullCalls  int
	fileCalls  int
	fileRef    string
	filePath   string
	fileFailed error
}

func (g *fakeGitHub) PullState(context.Context, string, string) (PullState, error) {
	g.pullCalls++

	return g.pull, g.pullErr
}

func (g *fakeGitHub) FileAt(_ context.Context, _, _, path, ref string) (string, bool, error) {
	g.fileCalls++
	g.filePath, g.fileRef = path, ref

	return g.file, g.found, g.fileFailed
}

func prEnv(t *testing.T, gh *fakeGitHub, rec state.StandingPR) *dispatchEnv {
	t.Helper()

	e := newPassEnv(t)
	e.ctl.SetGitHub(gh)

	rec.Project, rec.URL, rec.File = "alpha", "https://github.com/acme/alpha/pull/7", "AGENTS.md"
	require.NoError(t, e.store.PutStandingPR(context.Background(), rec))

	for _, f := range []state.Finding{
		{ID: 1, Text: "Run ./ci/verify first."},
		{ID: 2, Text: "Never touch generated files."},
	} {
		f.CardID = "ALPHA-00" + strconv.FormatInt(f.ID, 10)
		f.Project, f.Kind, f.State, f.RevisionID, f.Output = "alpha", "guidance", state.FindingProposed, 4, rec.URL
		e.store.putFinding(f)
	}

	return e
}

func (e *dispatchEnv) pr() state.StandingPR {
	p, _ := e.store.StandingPR(context.Background(), "alpha")

	return *p
}

func TestReadStandingPRsMarksAMergedPRsFindings(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Merged: true, MergeSHA: "m1"}, file: "Run ./ci/verify  first.\n", found: true}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1"})
	ch, unsubscribe := e.bus.Subscribe()
	t.Cleanup(unsubscribe)

	require.NoError(t, e.ctl.pass(context.Background()))

	cards, projects := changedEvents(ch)
	assert.Contains(t, projects, "alpha", "the saved read announces the project")
	assert.Subset(t, cards, []string{"alpha/ALPHA-001", "alpha/ALPHA-002"}, "each settled finding announces its card")
	assert.Equal(t, state.FindingMerged, e.store.finding(1).State)
	assert.Equal(t, state.FindingRejected, e.store.finding(2).State)
	assert.Equal(t, int64(4), e.store.finding(2).RevisionID)
	assert.Equal(t, "m1", gh.fileRef)
	assert.Equal(t, "AGENTS.md", gh.filePath)
	assert.Equal(t, state.PRMerged, e.pr().State)
	assert.Equal(t, epoch, e.pr().ReadAt)

	// A merged record is final.
	e.clk.Advance(time.Hour)
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 1, gh.pullCalls)
}

func TestReadStandingPRsRejectsEveryFindingWhenTheMergeDeletedTheFile(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Merged: true, MergeSHA: "m1"}}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1"})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.FindingRejected, e.store.finding(1).State)
	assert.Equal(t, state.FindingRejected, e.store.finding(2).State)
}

func TestReadStandingPRsRejectsAClosedPRsFindings(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{HeadSHA: "h1"}}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1"})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.FindingRejected, e.store.finding(1).State)
	assert.Equal(t, state.FindingRejected, e.store.finding(2).State)
	assert.Equal(t, 0, gh.fileCalls)
	assert.Equal(t, state.PRClosed, e.pr().State)

	// A closed record is final.
	e.clk.Advance(time.Hour)
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 1, gh.pullCalls)
}

func TestReadStandingPRsSeesAChangedHead(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "other"}}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1"})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.PRChangedOutside, e.pr().State)
	assert.Equal(t, state.FindingProposed, e.store.finding(1).State)
	assert.Equal(t, state.FindingProposed, e.store.finding(2).State)
}

func TestReadStandingPRsReadsAtMostEvery15Minutes(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Open: true, HeadSHA: "h1"}}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1", State: state.PROpen, ReadAt: epoch.Add(-5 * time.Minute)})

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 0, gh.pullCalls)

	e.clk.Advance(11 * time.Minute)
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 1, gh.pullCalls)

	// A forced read ignores the interval.
	got, err := e.ctl.readStandingPR(context.Background(), e.pr(), e.clk.Now(), true)
	require.NoError(t, err)
	assert.Equal(t, e.clk.Now(), got.ReadAt)
	assert.Equal(t, 2, gh.pullCalls)
}

func TestReadStandingPRsRetryAFailedReadAtMostEvery15Minutes(t *testing.T) {
	gh := &fakeGitHub{pullErr: errors.New("boom")}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1", State: state.PROpen, ReadAt: epoch.Add(-time.Hour)})

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 1, gh.pullCalls)
	assert.Equal(t, state.PROpen, e.pr().State)
	assert.Equal(t, epoch, e.pr().ReadAt)

	e.clk.Advance(time.Minute)
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 1, gh.pullCalls)

	e.clk.Advance(15 * time.Minute)
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, 2, gh.pullCalls)

	// A forced read always reads.
	_, err := e.ctl.readStandingPR(context.Background(), e.pr(), e.clk.Now(), true)
	require.Error(t, err)
	assert.Equal(t, 3, gh.pullCalls)
}

func TestReadStandingPRsLeaveFindingsProposedWhenTheFileReadFails(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Merged: true, MergeSHA: "m"}, fileFailed: errors.New("boom")}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1", State: state.PROpen})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.PROpen, e.pr().State)
	assert.Equal(t, state.FindingProposed, e.store.finding(1).State)
}

func TestReadStandingPRsCompareTheRedactedText(t *testing.T) {
	gh := &fakeGitHub{pull: PullState{Merged: true, MergeSHA: "m"}, file: "See a card first.\n", found: true}
	e := prEnv(t, gh, state.StandingPR{HeadSHA: "h1"})
	e.cards.projects["abc"] = &board.ProjectConfig{Name: "abc", Prefix: "ABC"}

	f := e.store.finding(1)
	f.Text = "See ABC-12 first."
	e.store.putFinding(f)

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.FindingMerged, e.store.finding(1).State)
	assert.Equal(t, state.FindingRejected, e.store.finding(2).State)
}

func TestReadStandingPRsWithoutGitHubReadsNothing(t *testing.T) {
	e := prEnv(t, &fakeGitHub{}, state.StandingPR{HeadSHA: "h1"})
	e.ctl.SetGitHub(nil)

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.pr().State)
	assert.Equal(t, state.FindingProposed, e.store.finding(1).State)
}
