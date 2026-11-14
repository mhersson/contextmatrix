package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/githubauth"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

// mintRecorder records the contents-read and the write repo mints apart.
type mintRecorder struct {
	readErr               error
	readCalls, writeCalls atomic.Int32
	readRepo              atomic.Value
}

func (m *mintRecorder) GenerateToken(context.Context) (string, time.Time, error) {
	return "broad-tok", time.Time{}, nil
}

func (m *mintRecorder) GenerateRepoToken(context.Context, string) (string, time.Time, error) {
	m.writeCalls.Add(1)

	return "write-tok", time.Time{}, nil
}

func (m *mintRecorder) GenerateRepoReadToken(_ context.Context, repo string) (string, time.Time, error) {
	m.readCalls.Add(1)
	m.readRepo.Store(repo)

	return "read-tok", time.Now().Add(time.Hour), m.readErr
}

func providerOf(g githubauth.TokenGenerator, err error) func(context.Context, string) (githubauth.TokenGenerator, string, error) {
	return func(context.Context, string) (githubauth.TokenGenerator, string, error) { return g, "", err }
}

func TestRunKindBaseUsesReadOnlyToken(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	t.Cleanup(cleanup)

	h := newLaunchHandlers(t, svc, newCaptureBackend(t))
	mints := &mintRecorder{}
	h.providerForProject = providerOf(mints, nil)
	card := createRunnable(t, svc, service.CreateCardInput{BaseBranch: "playbook/rollout"})

	p, _, err := h.runKindBase(context.Background(), "test-project", card.ID, true)
	require.NoError(t, err)

	assert.Equal(t, "read-tok", p.GitToken)
	assert.Equal(t, int32(1), mints.readCalls.Load())
	assert.Zero(t, mints.writeCalls.Load(), "a run kind never gets a write token")
	assert.Equal(t, "project", mints.readRepo.Load())

	assert.Equal(t, card.ID, p.CardID)
	assert.Equal(t, "test-project", p.Project)
	assert.Equal(t, "https://github.com/example/project.git", p.RepoURL)
	assert.Equal(t, "playbook/rollout", p.BaseBranch)
	assert.Equal(t, "my-worker:latest", p.WorkerImage)
	assert.Empty(t, p.Kind, "the caller names the kind")
}

func TestRunKindBaseFailsClosedOnBrokenBinding(t *testing.T) {
	tests := []struct {
		name     string
		provider func(context.Context, string) (githubauth.TokenGenerator, string, error)
		want     error
	}{
		{"binding unresolvable", providerOf(nil, auth.ErrCredentialUnavailable), auth.ErrCredentialUnavailable},
		{"mint refused", providerOf(&mintRecorder{readErr: errors.New("github: 403")}, nil), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
			t.Cleanup(cleanup)

			h := newLaunchHandlers(t, svc, newCaptureBackend(t))
			h.providerForProject = tc.provider
			card := createRunnable(t, svc, service.CreateCardInput{})

			p, _, err := h.runKindBase(context.Background(), "test-project", card.ID, true)
			require.Error(t, err)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}

			assert.Empty(t, p.GitToken)

			after, err := svc.GetCard(context.Background(), "test-project", card.ID)
			require.NoError(t, err)
			assert.Empty(t, after.WorkerStatus, "a run kind never marks the card")
		})
	}
}

func TestRunKindBaseCheckHasNoEndpoint(t *testing.T) {
	h, svc, _, _ := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	card := createRunnable(t, svc, service.CreateCardInput{})

	p, mode, err := h.runKindBase(context.Background(), "test-project", card.ID, false)
	require.NoError(t, err)

	assert.Empty(t, p.Model)
	assert.Nil(t, p.Selection)
	assert.Nil(t, p.LLMEndpoint)
	assert.Empty(t, mode)
	assert.Equal(t, "https://github.com/example/project.git", p.RepoURL, "the common fields are still filled")
}

func TestRunKindBaseModelKindChoosesAsLaunchDoes(t *testing.T) {
	h, svc, cb, _ := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	card := createRunnable(t, svc, service.CreateCardInput{})

	p, mode, err := h.runKindBase(context.Background(), "test-project", card.ID, true)
	require.NoError(t, err)

	_, err = h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	trigger := cb.lastTrigger(t)
	assert.Equal(t, llmchannel.ModeClaudeCode, mode)
	assert.Equal(t, trigger.Model, p.Model)
	assert.Equal(t, trigger.Selection, p.Selection)
	assert.Equal(t, trigger.LLMEndpoint, p.LLMEndpoint)
	require.NotNil(t, p.LLMEndpoint)
}

func TestSentinelRelaunchRecordsSentinelOrigin(t *testing.T) {
	h, svc, _, ops := channelLaunchHandlers(t, llmchannel.ModeEndpoint, []string{config.BackendNameAgent})
	ctx := context.Background()
	card := createRunnable(t, svc, service.CreateCardInput{Autonomous: new(true)})

	require.NoError(t, ops.OpenCardRun(ctx, "test-project", card.ID, "endpoint", time.Now(),
		state.LaunchFacts{Autonomous: true, Origin: state.OriginREST}))
	require.NoError(t, ops.IncrementAttempts(ctx, "test-project", card.ID))

	require.NoError(t, h.sentinelRelaunch(ctx, "test-project", card.ID))

	run, err := ops.CardRunRow(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, state.OriginSentinel, run.Launch.Origin)
	assert.Equal(t, 1, run.Attempts, "a Sentinel rerun keeps the attempt it counted")
}
