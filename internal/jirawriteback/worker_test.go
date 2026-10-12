package jirawriteback

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/jira"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

func seed(t *testing.T, store *opsqlite.Store, card, kind string) {
	t.Helper()

	_, err := store.EnqueueJiraWriteback(context.Background(), "api", card, kind, "PROJ-1", testStart.Unix())
	require.NoError(t, err)
}

func oneCard(state string) *fakeCards {
	return newFakeCards(map[string]*board.Card{"api/API-001": jiraCard("API-001", "PROJ-1", state, testStart)})
}

func TestWorker_Outcomes(t *testing.T) {
	tests := []struct {
		name       string
		start      jira.Status
		issueErr   error
		attempts   int // preset attempts on the row
		wantState  string
		wantTries  int
		wantLogged bool
		wantPaused bool
		minDelay   time.Duration // lower bound on next_attempt_at - now for retries
		noPath     bool          // workflow offers no way to the target
	}{
		{name: "success", start: st("To Do", "new"), wantState: "done"},
		{name: "already there is skipped", start: st("Done", "done"), wantState: "skipped"},
		{
			name: "5xx retries", start: st("To Do", "new"), issueErr: &jira.StatusError{StatusCode: 503, Message: "down"},
			wantState: "pending", wantTries: 1, minDelay: 48 * time.Second,
		},
		{
			name: "transport error retries", start: st("To Do", "new"), issueErr: fmt.Errorf("issue: dial tcp: i/o timeout"),
			wantState: "pending", wantTries: 1, minDelay: 48 * time.Second,
		},
		{
			name: "429 honours Retry-After", start: st("To Do", "new"), issueErr: &jira.RateLimitError{RetryAfter: 3 * time.Hour},
			wantState: "pending", wantTries: 1, minDelay: 3 * time.Hour,
		},
		{
			name: "4xx is permanent", start: st("To Do", "new"), issueErr: &jira.StatusError{StatusCode: 400, Message: "bad request"},
			wantState: "failed", wantLogged: true,
		},
		{
			name: "404 is permanent", start: st("To Do", "new"), issueErr: fmt.Errorf("issue: %w", jira.ErrNotFound),
			wantState: "failed", wantLogged: true,
		},
		{
			name: "gives up after the last attempt", start: st("To Do", "new"), issueErr: &jira.StatusError{StatusCode: 502, Message: "bad gateway"},
			attempts: maxAttempts - 1, wantState: "failed", wantTries: maxAttempts - 1, wantLogged: true,
		},
		{
			name: "no transition path is permanent", start: st("To Do", "new"), issueErr: nil,
			wantState: "failed", wantLogged: true, noPath: true,
		},
		{
			name: "401 pauses without ageing", start: st("To Do", "new"), issueErr: fmt.Errorf("issue: %w", &jira.AuthError{}),
			wantState: "pending", wantPaused: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cards := oneCard(board.StateInProgress)
			fj := newFakeJira(tt.start, simpleWorkflow())
			fj.issueErr = tt.issueErr

			if tt.noPath {
				fj.workflow = map[string][]jira.Transition{}
			}

			w, store, clk := newTestWriteback(t, cards, fj)

			seed(t, store, "API-001", KindStarted)

			if tt.attempts > 0 {
				require.NoError(t, store.RetryJiraWriteback(context.Background(), "api", "API-001", KindStarted,
					tt.attempts, testStart.Unix(), "earlier", testStart.Unix()))
			}

			w.drain(t.Context())

			row, ok := rowState(t, store, "api", "API-001", KindStarted)
			require.True(t, ok)
			assert.Equal(t, tt.wantState, row.State)
			assert.Equal(t, tt.wantTries, row.Attempts)
			assert.Equal(t, tt.wantPaused, w.Paused())

			if tt.minDelay > 0 {
				assert.GreaterOrEqual(t, row.NextAttemptAt-clk.Now().Unix(), int64(tt.minDelay/time.Second))
			}

			logs := cards.loggedLines()
			if tt.wantLogged {
				require.Len(t, logs, 1)
				assert.Equal(t, systemAgent, logs[0].entry.Agent)
				assert.Equal(t, "jira-writeback", logs[0].entry.Action)
				assert.NotEmpty(t, row.LastError)
			} else {
				assert.Empty(t, logs)
			}
		})
	}
}

func TestWorker_StartedSupersededByPendingDone(t *testing.T) {
	cards := oneCard(board.StateDone)
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	w, store, _ := newTestWriteback(t, cards, fj)

	seed(t, store, "API-001", KindStarted)
	seed(t, store, "API-001", KindDone)

	w.drain(t.Context())

	started, _ := rowState(t, store, "api", "API-001", KindStarted)
	done, _ := rowState(t, store, "api", "API-001", KindDone)
	assert.Equal(t, "skipped", started.State)
	assert.Equal(t, "done", done.State)
	assert.Equal(t, []string{"31"}, fj.postedIDs(), "straight to Done, no detour through In Progress")
}

func TestWorker_PauseResumesWhenCredentialsWorkAgain(t *testing.T) {
	cards := oneCard(board.StateInProgress)
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	fj.issueErr = fmt.Errorf("issue: %w", &jira.AuthError{})
	fj.myselfErr = fmt.Errorf("myself: %w", &jira.AuthError{})
	w, store, clk := newTestWriteback(t, cards, fj)

	seed(t, store, "API-001", KindStarted)
	w.drain(t.Context())
	require.True(t, w.Paused())

	clk.Advance(2 * time.Minute)
	w.drain(t.Context()) // probe still rejected
	require.True(t, w.Paused())

	row, _ := rowState(t, store, "api", "API-001", KindStarted)
	assert.Equal(t, 0, row.Attempts, "paused rows do not age")

	fj.mu.Lock()
	fj.issueErr, fj.myselfErr = nil, nil
	fj.mu.Unlock()

	clk.Advance(maxProbeDelay)
	w.drain(t.Context())

	assert.False(t, w.Paused())

	row, _ = rowState(t, store, "api", "API-001", KindStarted)
	assert.Equal(t, "done", row.State)
}

func TestWorker_FailureOnDeletedCardStillFinishesRow(t *testing.T) {
	cards := newFakeCards(map[string]*board.Card{}) // card deleted after enqueue
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	fj.issueErr = fmt.Errorf("issue: %w", jira.ErrNotFound)
	w, store, _ := newTestWriteback(t, cards, fj)

	seed(t, store, "API-001", KindStarted)
	seed(t, store, "API-002", KindStarted)

	w.drain(t.Context())

	for _, id := range []string{"API-001", "API-002"} {
		row, _ := rowState(t, store, "api", id, KindStarted)
		assert.Equal(t, "failed", row.State, id)
	}
}

func TestWorker_ShutdownLeavesRowPending(t *testing.T) {
	cards := oneCard(board.StateDone)
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	w, store, _ := newTestWriteback(t, cards, fj)

	// A done row goes straight to the Jira call (no pending-done lookup), so
	// the cut call is what this exercises.
	seed(t, store, "API-001", KindDone)
	row := mustDue(t, store)[0]

	ctx, cancel := context.WithCancel(t.Context())
	fj.issueErr = context.Canceled

	cancel()

	assert.False(t, w.process(ctx, row))

	got, _ := rowState(t, store, "api", "API-001", KindDone)
	assert.Equal(t, "pending", got.State)
	assert.Equal(t, 0, got.Attempts)
}

func mustDue(t *testing.T, store *opsqlite.Store) []opsqlite.JiraWriteback {
	t.Helper()

	rows, err := store.DueJiraWritebacks(context.Background(), testStart.Unix(), 10)
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	return rows
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "short unchanged", in: "abc", want: "abc"},
		{name: "cut at the limit", in: strings.Repeat("a", maxReason+10), want: strings.Repeat("a", maxReason)},
		{
			name: "multi-byte character straddling the cut is dropped",
			in:   strings.Repeat("a", maxReason-1) + "é", want: strings.Repeat("a", maxReason-1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.in)
			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got))
		})
	}
}

func TestWorker_UnauthorizedWritesKeepProbeBackoffGrowing(t *testing.T) {
	cards := oneCard(board.StateInProgress)
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	// Myself and reads pass; only the write is rejected (no write scope).
	fj.postErr = fmt.Errorf("transition: %w", &jira.AuthError{})
	w, store, clk := newTestWriteback(t, cards, fj)

	seed(t, store, "API-001", KindStarted)

	var delays []time.Duration

	w.drain(t.Context())
	require.True(t, w.Paused())

	for range 8 {
		before := clk.Now()
		delays = append(delays, w.probeAt.Sub(before))
		clk.Advance(w.probeAt.Sub(before))
		w.drain(t.Context()) // probe passes, resumes, the POST 401s again
		require.True(t, w.Paused())
	}

	assert.Equal(t, time.Minute, delays[0])
	assert.Equal(t, maxProbeDelay, delays[len(delays)-1], "backoff reaches the cap")

	for i := 1; i < len(delays); i++ {
		assert.GreaterOrEqual(t, delays[i], delays[i-1], "backoff never shrinks")
	}

	assert.LessOrEqual(t, len(fj.postedIDs()), 9, "one POST per probe, spaced by the growing delay")
	assert.True(t, w.pauseWarned, "the pause is announced once until a write succeeds")

	row, _ := rowState(t, store, "api", "API-001", KindStarted)
	assert.Equal(t, "pending", row.State)
	assert.Equal(t, 0, row.Attempts)
}

func TestWorker_SuccessAfterResumeResetsProbeBackoff(t *testing.T) {
	cards := oneCard(board.StateInProgress)
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	fj.postErr = fmt.Errorf("transition: %w", &jira.AuthError{})
	w, store, clk := newTestWriteback(t, cards, fj)

	seed(t, store, "API-001", KindStarted)

	w.drain(t.Context())

	for range 3 {
		clk.Advance(w.probeAt.Sub(clk.Now()))
		w.drain(t.Context())
	}

	require.Positive(t, w.probeCount)

	fj.mu.Lock()
	fj.postErr = nil
	fj.mu.Unlock()

	clk.Advance(w.probeAt.Sub(clk.Now()))
	w.drain(t.Context())

	require.False(t, w.Paused())

	row, _ := rowState(t, store, "api", "API-001", KindStarted)
	assert.Equal(t, "done", row.State)
	assert.Equal(t, 0, w.probeCount)
	assert.False(t, w.pauseWarned)
}

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		base    time.Duration
	}{
		{1, time.Minute},
		{2, 2 * time.Minute},
		{3, 4 * time.Minute},
		{4, 8 * time.Minute},
		{5, 16 * time.Minute},
		{6, 32 * time.Minute},
		{7, time.Hour},
		{20, time.Hour},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt %d", tt.attempt), func(t *testing.T) {
			for range 50 {
				d := retryDelay(tt.attempt, nil)
				assert.GreaterOrEqual(t, d, time.Duration(float64(tt.base)*0.8))
				assert.LessOrEqual(t, d, time.Duration(float64(tt.base)*1.2))
			}
		})
	}
}
