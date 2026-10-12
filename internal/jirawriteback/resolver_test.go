package jirawriteback

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/jira"
)

func TestResolver_Apply(t *testing.T) {
	tests := []struct {
		name       string
		start      jira.Status
		workflow   map[string][]jira.Transition
		targets    map[string]config.JiraStatusTarget
		kind       string
		want       outcome
		wantErr    error
		wantPosts  []string
		wantStatus string
		wantFields map[string]any // fields on the last post
	}{
		{
			name: "started from To Do", start: st("To Do", "new"), workflow: simpleWorkflow(), kind: KindStarted,
			want: outcomeDone, wantPosts: []string{"21"}, wantStatus: "In Progress",
		},
		{
			name: "started when already in progress", start: st("In Progress", "indeterminate"), workflow: simpleWorkflow(), kind: KindStarted,
			want: outcomeSkipped, wantStatus: "In Progress",
		},
		{
			name: "started never reopens a closed issue", start: st("Done", "done"), workflow: simpleWorkflow(), kind: KindStarted,
			want: outcomeSkipped, wantStatus: "Done",
		},
		{
			name: "done already done", start: st("Done", "done"), workflow: simpleWorkflow(), kind: KindDone,
			want: outcomeSkipped, wantStatus: "Done",
		},
		{
			name: "done sends default resolution when required", start: st("In Progress", "indeterminate"), workflow: simpleWorkflow(), kind: KindDone,
			want: outcomeDone, wantPosts: []string{"31"}, wantStatus: "Done",
			wantFields: map[string]any{"resolution": map[string]string{"name": "Done"}},
		},
		{
			name: "configured resolution", start: st("In Progress", "indeterminate"), workflow: simpleWorkflow(), kind: KindDone,
			targets: map[string]config.JiraStatusTarget{"PROJ": {Resolution: "Fixed"}},
			want:    outcomeDone, wantPosts: []string{"31"}, wantStatus: "Done",
			wantFields: map[string]any{"resolution": map[string]string{"name": "Fixed"}},
		},
		{
			name: "done walks three hops", start: st("To Do", "new"), workflow: reviewWorkflow(), kind: KindDone,
			want: outcomeDone, wantPosts: []string{"21", "41", "31"}, wantStatus: "Done",
		},
		{
			name: "ambiguous done needs a target", start: st("To Do", "new"), kind: KindDone,
			workflow: map[string][]jira.Transition{"To Do": {tr("31", "Done", "done"), tr("32", "Won't Do", "done")}},
			wantErr:  errNoTransition, wantStatus: "To Do",
		},
		{
			name: "configured name matches case-insensitively", start: st("To Do", "new"), kind: KindDone,
			workflow: map[string][]jira.Transition{"To Do": {tr("31", "Done", "done"), tr("32", "Won't Do", "done")}},
			targets:  map[string]config.JiraStatusTarget{"PROJ": {Done: "won't do"}},
			want:     outcomeDone, wantPosts: []string{"32"}, wantStatus: "Won't Do",
		},
		{
			name: "configured started reached through a To Do-category step", start: st("Backlog", "new"), kind: KindStarted,
			workflow: map[string][]jira.Transition{
				"Backlog":                  {tr("5", "Selected for Development", "new")},
				"Selected for Development": {tr("21", "In Progress", "indeterminate"), tr("22", "Blocked", "indeterminate")},
			},
			targets: map[string]config.JiraStatusTarget{"PROJ": {Started: "In Progress"}},
			want:    outcomeDone, wantPosts: []string{"5", "21"}, wantStatus: "In Progress",
		},
		{
			name: "no path", start: st("To Do", "new"), kind: KindStarted,
			workflow: map[string][]jira.Transition{"To Do": {}},
			wantErr:  errNoTransition, wantStatus: "To Do",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fj := newFakeJira(tt.start, tt.workflow)
			r := newResolver(fj, tt.targets)

			got, err := r.apply(t.Context(), "PROJ-1", tt.kind)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.Equal(t, tt.wantPosts, nilIfEmptyStrings(fj.postedIDs()))
			assert.Equal(t, tt.wantStatus, fj.status().Name)

			if tt.wantFields != nil {
				assert.Equal(t, tt.wantFields, fj.fields[len(fj.fields)-1])
			}
		})
	}
}

func TestResolver_RaceWithAnotherWriterCountsAsSuccess(t *testing.T) {
	fj := newFakeJira(st("In Progress", "indeterminate"), simpleWorkflow())
	fj.postErrOnce = &jira.StatusError{StatusCode: 400, Message: "transition not valid"}
	fj.onPostErr = func(f *fakeJira) { f.issue.Status = st("Done", "done") }

	got, err := newResolver(fj, nil).apply(t.Context(), "PROJ-1", KindDone)
	require.NoError(t, err)
	assert.Equal(t, outcomeDone, got)
}

func TestResolver_PostFailureOnFreshResolutionIsReturned(t *testing.T) {
	fj := newFakeJira(st("In Progress", "indeterminate"), simpleWorkflow())
	fj.postErrOnce = &jira.StatusError{StatusCode: 400, Message: "Field resolution is required"}

	_, err := newResolver(fj, nil).apply(t.Context(), "PROJ-1", KindDone)
	se, ok := errors.AsType[*jira.StatusError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, 400, se.StatusCode)
}

func TestResolver_CachesAndRecoversFromStaleCache(t *testing.T) {
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	r := newResolver(fj, nil)

	_, err := r.apply(t.Context(), "PROJ-1", KindStarted)
	require.NoError(t, err)

	fj.issue.Status = st("To Do", "new")
	_, err = r.apply(t.Context(), "PROJ-2", KindStarted)
	require.NoError(t, err)
	assert.Equal(t, 1, fj.transitionsCalls, "second issue in the same project/type/status reuses the cached transition")

	// The workflow changes under the cache: transition 21 is gone.
	fj.issue.Status = st("To Do", "new")
	fj.workflow["To Do"] = []jira.Transition{tr("22", "In Progress", "indeterminate")}

	got, err := r.apply(t.Context(), "PROJ-3", KindStarted)
	require.NoError(t, err)
	assert.Equal(t, outcomeDone, got)
	assert.Equal(t, []string{"21", "21", "21", "22"}, fj.postedIDs(), "stale cached id fails once, then resolves afresh")
}

func TestResolver_ReadErrorsPropagate(t *testing.T) {
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	fj.issueErr = fmt.Errorf("issue PROJ-1: %w", &jira.AuthError{})

	_, err := newResolver(fj, nil).apply(t.Context(), "PROJ-1", KindStarted)
	require.ErrorIs(t, err, jira.ErrUnauthorized)
	assert.Empty(t, fj.postedIDs())
}

func nilIfEmptyStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}

	return s
}
