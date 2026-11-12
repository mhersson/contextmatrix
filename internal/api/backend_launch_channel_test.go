package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/llmchannel/llmchanneltest"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
)

func gatewayEndpoint() *protocol.LLMEndpoint {
	return &protocol.LLMEndpoint{Type: config.LLMEndpointTypeOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k"}
}

// channelLaunchHandlers wires a launch handler to a real holder and ops store.
func channelLaunchHandlers(t *testing.T, mode llmchannel.Mode, supported []string) (*backendHandlers, *service.CardService, *captureBackend, *opsqlite.Store) {
	t.Helper()

	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	t.Cleanup(cleanup)

	ops, err := opsqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ops.Close() })

	holder, err := llmchannel.New(context.Background(), llmchannel.Config{
		Endpoint:     gatewayEndpoint(),
		OAuthToken:   "tok",
		Supported:    supported,
		DefaultModel: "claude-sonnet-5-5",
	}, llmchanneltest.Store{}, events.NewBus())
	require.NoError(t, err)
	require.NoError(t, holder.Set(context.Background(), mode, "human:test"))

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)
	h.backendCfg.DefaultModel = "openrouter/some-model"
	h.backendCfg.Favorites = map[string]board.TierFavorites{
		"simple": {All: []string{"anthropic/claude-opus-5.5", "z-ai/glm-5.2"}},
	}
	h.llmChannel = holder
	h.cardRuns = ops
	h.catalog = &stubCatalog{
		candidates:       []protocol.CandidateModel{{Slug: "z-ai/glm-5.2"}},
		claudeCandidates: []protocol.CandidateModel{{Slug: "claude-sonnet-5-5"}},
	}

	return h, svc, cb, ops
}

func createRunnable(t *testing.T, svc *service.CardService, in service.CreateCardInput) *board.Card {
	t.Helper()

	in.Title, in.Type, in.Priority = "run me", "task", "medium"
	card, err := svc.CreateCard(context.Background(), "test-project", in)
	require.NoError(t, err)

	return card
}

func TestLaunch_ClaudeCodeMode(t *testing.T) {
	h, svc, cb, ops := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	card := createRunnable(t, svc, service.CreateCardInput{})

	_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	p := cb.lastTrigger(t)
	require.NotNil(t, p.LLMEndpoint)
	assert.Equal(t, protocol.LLMEndpointTypeClaudeCode, p.LLMEndpoint.Type)
	assert.Equal(t, "tok", p.LLMEndpoint.OAuthToken)
	assert.Empty(t, p.LLMEndpoint.BaseURL)
	assert.Empty(t, p.LLMEndpoint.APIKey)
	assert.Equal(t, "claude-sonnet-5-5", p.Model)

	require.NotNil(t, p.Selection)
	assert.Equal(t, []protocol.CandidateModel{{Slug: "claude-sonnet-5-5"}}, p.Selection.Candidates)
	require.Len(t, p.Selection.Favorites, 1)
	assert.Equal(t, []string{"claude-opus-5-5"}, p.Selection.Favorites[0].Models, "unmappable favorites are dropped")

	runs, err := ops.OpenCardRuns(context.Background())
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "claude_code", runs[0].Mode)
	assert.Equal(t, card.ID, runs[0].CardID)
}

func TestLaunch_BlacklistFollowsMode(t *testing.T) {
	blacklisted := []string{"anthropic/claude-haiku-5.5", "z-ai/glm-5.2", "anthropic/claude-opus-5.5"}

	tests := []struct {
		name string
		mode llmchannel.Mode
		want []string
	}{
		{"claude_code maps and drops unmappable", llmchannel.ModeClaudeCode, []string{"claude-haiku-5-5", "claude-opus-5-5"}},
		{"endpoint ships slugs as stored", llmchannel.ModeEndpoint, blacklisted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, svc, cb, _ := channelLaunchHandlers(t, tt.mode, []string{config.BackendNameAgent})
			h.blacklist = &stubBlacklist{slugs: blacklisted}
			card := createRunnable(t, svc, service.CreateCardInput{})

			_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
			require.NoError(t, err)

			p := cb.lastTrigger(t)
			require.NotNil(t, p.Selection)
			assert.Equal(t, tt.want, p.Selection.Blacklist)
		})
	}
}

func TestLaunch_EndpointModeKeepsToday(t *testing.T) {
	h, svc, cb, ops := channelLaunchHandlers(t, llmchannel.ModeEndpoint, []string{config.BackendNameAgent})
	card := createRunnable(t, svc, service.CreateCardInput{})

	_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	p := cb.lastTrigger(t)
	assert.Equal(t, gatewayEndpoint(), p.LLMEndpoint)
	assert.Equal(t, "openrouter/some-model", p.Model)
	assert.Equal(t, []protocol.CandidateModel{{Slug: "z-ai/glm-5.2"}}, p.Selection.Candidates)
	assert.Equal(t, []string{"anthropic/claude-opus-5.5", "z-ai/glm-5.2"}, p.Selection.Favorites[0].Models)

	runs, err := ops.OpenCardRuns(context.Background())
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "endpoint", runs[0].Mode)
}

func TestLaunch_AgentUnsupported(t *testing.T) {
	h, svc, cb, ops := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameChat})
	card := createRunnable(t, svc, service.CreateCardInput{})

	_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	p := cb.lastTrigger(t)
	assert.Equal(t, gatewayEndpoint(), p.LLMEndpoint)
	assert.Equal(t, "openrouter/some-model", p.Model)

	runs, err := ops.OpenCardRuns(context.Background())
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "endpoint", runs[0].Mode)
}

func TestLaunch_NoPinNote(t *testing.T) {
	h, svc, _, _ := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	card := createRunnable(t, svc, service.CreateCardInput{
		ModelCoder: "anthropic/claude-opus-5.5", ModelReviewer: "openai/gpt-5.4",
	})

	got, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	reloaded, err := svc.GetCard(context.Background(), "test-project", got.ID)
	require.NoError(t, err)

	for _, e := range reloaded.ActivityLog {
		assert.NotEqual(t, "pin-ignored", e.Action)
	}
}

func TestLaunch_WebhookFailureClosesRun(t *testing.T) {
	h, svc, _, ops := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	card := createRunnable(t, svc, service.CreateCardInput{})

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)

	h.backend = backend.NewClient(dead.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.Error(t, err)

	runs, err := ops.OpenCardRuns(context.Background())
	require.NoError(t, err)
	assert.Empty(t, runs)
}

type failingRuns struct{}

func (failingRuns) OpenCardRun(context.Context, string, string, string, time.Time) error {
	return errors.New("disk full")
}

func TestLaunch_RunRowFailureDoesNotFailLaunch(t *testing.T) {
	h, svc, _, _ := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	h.cardRuns = failingRuns{}
	card := createRunnable(t, svc, service.CreateCardInput{})

	_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{})
	require.NoError(t, err)
}
