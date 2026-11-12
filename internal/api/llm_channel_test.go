package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/llmchannel/llmchanneltest"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const llmChannelTestToken = "sk-ant-oat01-SECRET"

type fakeLLMChannelStore struct {
	mu       sync.Mutex
	runs     []sqlite.CardRun
	closed   []string
	sessions map[string]map[string]int
	usage    []sqlite.SubscriptionUsageRow
}

func (f *fakeLLMChannelStore) OpenCardRuns(context.Context) ([]sqlite.CardRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]sqlite.CardRun(nil), f.runs...), nil
}

// CloseCardRun honours the started-at bound against the stored runs, like the
// real store: a run that started after the bound is left open.
func (f *fakeLLMChannelStore) CloseCardRun(_ context.Context, project, cardID string, startedNotAfter, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, r := range f.runs {
		if r.Project == project && r.CardID == cardID && r.StartedAt.After(startedNotAfter) {
			return nil
		}
	}

	f.closed = append(f.closed, project+"/"+cardID)

	return nil
}

func (f *fakeLLMChannelStore) CountLiveSessionsByMode(context.Context) (map[string]map[string]int, error) {
	return f.sessions, nil
}

func (f *fakeLLMChannelStore) SubscriptionUsage(context.Context) ([]sqlite.SubscriptionUsageRow, error) {
	return f.usage, nil
}

type fakeLLMChannelCards map[string]string // "project/id" -> worker_status; absent = not found

func (f fakeLLMChannelCards) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	status, ok := f[project+"/"+id]
	if !ok {
		return nil, storage.ErrCardNotFound
	}

	return &board.Card{ID: id, Project: project, WorkerStatus: status}, nil
}

type llmChannelFixture struct {
	store  *fakeLLMChannelStore
	holder *llmchannel.Holder
	bus    *events.Bus
	server *httptest.Server
}

func newLLMChannelFixture(t *testing.T, multi bool, cards fakeLLMChannelCards, mutate func(*fakeLLMChannelStore)) *llmChannelFixture {
	t.Helper()

	bus := events.NewBus()

	holder, err := llmchannel.New(context.Background(), llmchannel.Config{
		Endpoint:     &protocol.LLMEndpoint{Type: config.LLMEndpointTypeOpenRouter, BaseURL: "https://secret-host.example/v1", APIKey: "k"},
		OAuthToken:   llmChannelTestToken,
		Supported:    []string{config.BackendNameAgent, config.BackendNameChat},
		DefaultModel: "claude-sonnet-5-5",
	}, llmchanneltest.Store{}, bus)
	require.NoError(t, err)

	store := &fakeLLMChannelStore{}
	if mutate != nil {
		mutate(store)
	}

	cfg := RouterConfig{
		LLMChannelState:   holder,
		LLMChannelStore:   store,
		LLMChannelCards:   cards,
		ConfiguredBackend: []string{config.BackendNameAgent, config.BackendNameChat, config.BackendNameArchitect},
		Bus:               bus,
	}

	return &llmChannelFixture{
		store:  store,
		holder: holder,
		bus:    bus,
		server: newSelectorAdminServer(t, cfg, multi),
	}
}

func getLLMChannel(t *testing.T, srv *httptest.Server, cookie *http.Cookie) (int, []byte, map[string]any) {
	t.Helper()

	resp, err := http.DefaultClient.Do(selectorRequest(t, http.MethodGet, srv.URL+"/api/llm-channel", nil, cookie))
	require.NoError(t, err)

	defer resp.Body.Close()

	var raw json.RawMessage

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))

	var body map[string]any

	_ = json.Unmarshal(raw, &body)

	return resp.StatusCode, raw, body
}

func TestGetLLMChannel_Body(t *testing.T) {
	seen := time.Unix(1_900_000_000, 0).UTC()
	resets := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)

	f := newLLMChannelFixture(t, false, fakeLLMChannelCards{
		"p/A-1": "running",
		"p/A-2": "queued",
		"p/A-3": "running",
	}, func(s *fakeLLMChannelStore) {
		s.runs = []sqlite.CardRun{
			{Project: "p", CardID: "A-1", Mode: "endpoint"},
			{Project: "p", CardID: "A-2", Mode: "claude_code"},
			{Project: "p", CardID: "A-3", Mode: "claude_code"},
		}
		s.sessions = map[string]map[string]int{
			"endpoint":    {config.BackendNameChat: 1},
			"claude_code": {config.BackendNameArchitect: 2},
		}
		s.usage = []sqlite.SubscriptionUsageRow{{
			SubscriptionUsage: protocol.SubscriptionUsage{Window: "seven_day", Utilization: 0.64, Status: "allowed", ResetsAt: resets},
			SeenAt:            seen,
		}}
	})
	require.NoError(t, f.holder.Set(context.Background(), llmchannel.ModeClaudeCode, "human:morten"))

	code, _, body := getLLMChannel(t, f.server, nil)
	require.Equal(t, http.StatusOK, code)

	assert.Equal(t, "claude_code", body["mode"])
	assert.Equal(t, true, body["switchable"])
	assert.Equal(t, true, body["claude_code"])
	assert.Equal(t, map[string]any{"type": "openrouter", "label": "OpenRouter"}, body["endpoint"])
	assert.Equal(t, "human:morten", body["updated_by"])
	assert.NotEmpty(t, body["updated_at"])

	assert.Equal(t, map[string]any{
		"endpoint":    map[string]any{"cards": float64(1), "chats": float64(1), "blueprints": float64(0)},
		"claude_code": map[string]any{"cards": float64(2), "chats": float64(0), "blueprints": float64(2)},
	}, body["in_flight"])

	assert.Equal(t, []any{map[string]any{"backend": "architect", "runs_on": "endpoint"}}, body["exceptions"])

	usage, ok := body["usage"].([]any)
	require.True(t, ok)
	require.Len(t, usage, 1)

	u := usage[0].(map[string]any)
	assert.Equal(t, "seven_day", u["window"])
	assert.InDelta(t, 0.64, u["utilization"], 1e-9)
	assert.Equal(t, "allowed", u["status"])
	assert.Equal(t, false, u["stale"])
	assert.Equal(t, resets.Format(time.RFC3339), u["resets_at"])
	assert.Equal(t, seen.Format(time.RFC3339), u["seen_at"])
}

func TestGetLLMChannel_StaleUsage(t *testing.T) {
	f := newLLMChannelFixture(t, false, nil, func(s *fakeLLMChannelStore) {
		s.usage = []sqlite.SubscriptionUsageRow{
			{SubscriptionUsage: protocol.SubscriptionUsage{Window: "five_hour", ResetsAt: time.Now().Add(-time.Hour)}, SeenAt: time.Now()},
			{SubscriptionUsage: protocol.SubscriptionUsage{Window: "seven_day", ResetsAt: time.Now().Add(time.Hour)}, SeenAt: time.Now()},
		}
	})

	_, _, body := getLLMChannel(t, f.server, nil)

	stale := map[string]any{}

	for _, u := range body["usage"].([]any) {
		m := u.(map[string]any)
		stale[m["window"].(string)] = m["stale"]
	}

	assert.Equal(t, true, stale["five_hour"])
	assert.Equal(t, false, stale["seven_day"])
}

func TestGetLLMChannel_NoTokenInBody(t *testing.T) {
	f := newLLMChannelFixture(t, false, nil, nil)
	require.NoError(t, f.holder.Set(context.Background(), llmchannel.ModeClaudeCode, "human:a"))

	_, raw, _ := getLLMChannel(t, f.server, nil)

	assert.NotContains(t, string(raw), llmChannelTestToken)
	assert.NotContains(t, string(raw), "secret-host")
}

func TestGetLLMChannel_NothingConfigured(t *testing.T) {
	holder, err := llmchannel.New(context.Background(), llmchannel.Config{}, llmchanneltest.Store{}, events.NewBus())
	require.NoError(t, err)

	srv := newSelectorAdminServer(t, RouterConfig{
		LLMChannelState: holder, LLMChannelStore: &fakeLLMChannelStore{},
	}, false)

	code, _, body := getLLMChannel(t, srv, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, body["switchable"])
	assert.Nil(t, body["endpoint"])
	assert.Equal(t, false, body["claude_code"])
	assert.Empty(t, body["exceptions"])
}

// Without an endpoint an unsupported backend has nowhere to run.
func TestGetLLMChannel_ClaudeCodeOnlyExceptionRunsOnNone(t *testing.T) {
	holder, err := llmchannel.New(context.Background(), llmchannel.Config{
		OAuthToken: llmChannelTestToken,
		Supported:  []string{config.BackendNameChat},
	}, llmchanneltest.Store{}, events.NewBus())
	require.NoError(t, err)

	srv := newSelectorAdminServer(t, RouterConfig{
		LLMChannelState:   holder,
		LLMChannelStore:   &fakeLLMChannelStore{},
		ConfiguredBackend: []string{config.BackendNameAgent, config.BackendNameChat},
	}, false)

	code, _, body := getLLMChannel(t, srv, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "claude_code", body["mode"])
	assert.Nil(t, body["endpoint"])
	assert.Equal(t, []any{map[string]any{"backend": "agent", "runs_on": "none"}}, body["exceptions"])
}

func putLLMChannel(t *testing.T, srv *httptest.Server, body any, cookie *http.Cookie) (*http.Response, map[string]any) {
	t.Helper()

	resp, err := http.DefaultClient.Do(selectorRequest(t, http.MethodPut, srv.URL+"/api/admin/llm-channel", body, cookie))
	require.NoError(t, err)

	defer resp.Body.Close()

	var out map[string]any

	_ = json.NewDecoder(resp.Body).Decode(&out)

	return resp, out
}

func TestPutLLMChannel(t *testing.T) {
	t.Run("switches and publishes", func(t *testing.T) {
		f := newLLMChannelFixture(t, false, nil, nil)

		ch, unsub := f.bus.Subscribe()
		defer unsub()

		resp, body := putLLMChannel(t, f.server, map[string]string{"mode": "claude_code"}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "claude_code", body["mode"])
		assert.Equal(t, "human:web", body["updated_by"])
		assert.Equal(t, llmchannel.ModeClaudeCode, f.holder.Mode())

		select {
		case e := <-ch:
			assert.Equal(t, events.LLMChannelChanged, e.Type)
		case <-time.After(time.Second):
			t.Fatal("no llm_channel.changed event")
		}
	})

	t.Run("agent id header attributes the change", func(t *testing.T) {
		f := newLLMChannelFixture(t, false, nil, nil)

		req := selectorRequest(t, http.MethodPut, f.server.URL+"/api/admin/llm-channel", map[string]string{"mode": "claude_code"}, nil)
		req.Header.Set("X-Agent-ID", "human:morten")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		_, by := f.holder.Updated()
		assert.Equal(t, "human:morten", by)
	})

	t.Run("unknown mode is 422", func(t *testing.T) {
		f := newLLMChannelFixture(t, false, nil, nil)
		resp, _ := putLLMChannel(t, f.server, map[string]string{"mode": "bogus"}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("unconfigured side is 409", func(t *testing.T) {
		holder, err := llmchannel.New(context.Background(), llmchannel.Config{
			Endpoint: &protocol.LLMEndpoint{Type: "openai", BaseURL: "https://x", APIKey: "k"},
		}, llmchanneltest.Store{}, events.NewBus())
		require.NoError(t, err)

		srv := newSelectorAdminServer(t, RouterConfig{LLMChannelState: holder, LLMChannelStore: &fakeLLMChannelStore{}}, false)
		resp, _ := putLLMChannel(t, srv, map[string]string{"mode": "claude_code"}, nil)
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("multi mode gates on admin", func(t *testing.T) {
		f := newLLMChannelFixture(t, true, nil, nil)

		bob := login(t, f.server, "bob", "bob password1")
		resp, _ := putLLMChannel(t, f.server, map[string]string{"mode": "claude_code"}, bob)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, llmchannel.ModeEndpoint, f.holder.Mode())

		root := login(t, f.server, "root", "root password1")
		resp, body := putLLMChannel(t, f.server, map[string]string{"mode": "claude_code"}, root)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "human:root", body["updated_by"])
		assert.Equal(t, llmchannel.ModeClaudeCode, f.holder.Mode())
	})

	t.Run("missing csrf header is rejected", func(t *testing.T) {
		f := newLLMChannelFixture(t, false, nil, nil)

		req := selectorRequest(t, http.MethodPut, f.server.URL+"/api/admin/llm-channel", map[string]string{"mode": "claude_code"}, nil)
		req.Header.Del("X-Requested-With")

		resp, err := rawHTTPClient().Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, llmchannel.ModeEndpoint, f.holder.Mode())
	})
}

func TestInFlight_ClosesFinishedRuns(t *testing.T) {
	f := newLLMChannelFixture(t, false, fakeLLMChannelCards{
		"p/A-1": "completed",
		"p/A-2": "failed",
		"p/A-3": "running",
	}, func(s *fakeLLMChannelStore) {
		s.runs = []sqlite.CardRun{
			{Project: "p", CardID: "A-1", Mode: "claude_code"},
			{Project: "p", CardID: "A-2", Mode: "claude_code"},
			{Project: "p", CardID: "A-3", Mode: "claude_code"},
			{Project: "p", CardID: "GONE-1", Mode: "claude_code"},
		}
	})

	_, _, body := getLLMChannel(t, f.server, nil)

	inFlight := body["in_flight"].(map[string]any)["claude_code"].(map[string]any)
	assert.InDelta(t, 1, inFlight["cards"], 0)
	assert.ElementsMatch(t, []string{"p/A-1", "p/A-2", "p/GONE-1"}, f.store.closed)
}

// cardsWithGoneProject reports every card of the "gone" project as a missing project.
type cardsWithGoneProject struct{ fakeLLMChannelCards }

func (c cardsWithGoneProject) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	if project == "gone" {
		return nil, fmt.Errorf("get card: %w", storage.ErrProjectNotFound)
	}

	return c.fakeLLMChannelCards.GetCard(ctx, project, id)
}

func TestInFlight_ClosesRunOfDeletedProject(t *testing.T) {
	var store *fakeLLMChannelStore

	f := newLLMChannelFixture(t, false, nil, func(s *fakeLLMChannelStore) {
		store = s
		s.runs = []sqlite.CardRun{
			{Project: "gone", CardID: "G-1", Mode: "claude_code"},
			{Project: "p", CardID: "A-1", Mode: "claude_code"},
		}
	})

	srv := newSelectorAdminServer(t, RouterConfig{
		LLMChannelState: f.holder, LLMChannelStore: store,
		LLMChannelCards: cardsWithGoneProject{fakeLLMChannelCards{"p/A-1": "running"}},
	}, false)

	_, _, body := getLLMChannel(t, srv, nil)

	inFlight := body["in_flight"].(map[string]any)["claude_code"].(map[string]any)
	assert.InDelta(t, 1, inFlight["cards"], 0)
	assert.Equal(t, []string{"gone/G-1"}, store.closed)
}

// cardsAfterRead swaps the run row the moment the reconcile reads the card,
// as a rerun opening between the read and the close would.
type cardsAfterRead struct {
	fakeLLMChannelCards
	onRead func()
}

func (c cardsAfterRead) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	card, err := c.fakeLLMChannelCards.GetCard(ctx, project, id)
	c.onRead()

	return card, err
}

func TestInFlight_RerunDuringReconcileStaysOpen(t *testing.T) {
	old := time.Unix(1_600_000_000, 0).UTC()
	rerun := old.Add(200 * time.Millisecond)

	var store *fakeLLMChannelStore

	f := newLLMChannelFixture(t, false, nil, func(s *fakeLLMChannelStore) {
		store = s
		s.runs = []sqlite.CardRun{{Project: "p", CardID: "A-1", Mode: "claude_code", StartedAt: old}}
	})

	// Rebuild the router with a cards double that opens the rerun mid-reconcile.
	cards := cardsAfterRead{
		fakeLLMChannelCards: fakeLLMChannelCards{"p/A-1": "completed"},
		onRead: func() {
			store.mu.Lock()
			store.runs = []sqlite.CardRun{{Project: "p", CardID: "A-1", Mode: "claude_code", StartedAt: rerun}}
			store.mu.Unlock()
		},
	}
	srv := newSelectorAdminServer(t, RouterConfig{
		LLMChannelState: f.holder, LLMChannelStore: store, LLMChannelCards: cards,
	}, false)

	getLLMChannel(t, srv, nil)

	assert.Empty(t, store.closed, "the rerun's row is not closed")
	assert.Len(t, store.runs, 1)
}
