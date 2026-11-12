package llmchannel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/modelcatalog"
)

const testToken = "sk-ant-oat-secret"

type fakeStore struct {
	mu       sync.Mutex
	mode     string
	puts     []string
	putErr   error
	readErr  error
	usage    map[string]protocol.SubscriptionUsage
	usageErr error
}

func (f *fakeStore) RecordSubscriptionUsage(_ context.Context, u []protocol.SubscriptionUsage, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.usageErr != nil {
		return f.usageErr
	}

	if f.usage == nil {
		f.usage = map[string]protocol.SubscriptionUsage{}
	}

	for _, w := range u {
		f.usage[w.Window] = w
	}

	return nil
}

func (f *fakeStore) LLMChannel(context.Context) (string, time.Time, string, error) {
	return f.mode, time.Unix(100, 0).UTC(), "human:alice", f.readErr
}

func (f *fakeStore) PutLLMChannel(_ context.Context, mode, _ string) error {
	if f.putErr != nil {
		return f.putErr
	}

	f.mu.Lock()
	f.puts = append(f.puts, mode)
	f.mode = mode
	f.mu.Unlock()

	return nil
}

type fakeBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (b *fakeBus) Publish(e events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.events = append(b.events, e)
}

func endpoint() *protocol.LLMEndpoint {
	return &protocol.LLMEndpoint{Type: "openai", BaseURL: "https://gw.example", APIKey: "k"}
}

func TestNew_InitialMode(t *testing.T) {
	tests := []struct {
		name       string
		cfg        Config
		stored     string
		want       Mode
		switchable bool
	}{
		{"endpoint only", Config{Endpoint: endpoint()}, "", ModeEndpoint, false},
		{"token only", Config{OAuthToken: testToken}, "", ModeClaudeCode, false},
		{"both nothing stored", Config{Endpoint: endpoint(), OAuthToken: testToken}, "", ModeEndpoint, true},
		{"both stored claude_code", Config{Endpoint: endpoint(), OAuthToken: testToken}, "claude_code", ModeClaudeCode, true},
		{"stored claude_code no token", Config{Endpoint: endpoint()}, "claude_code", ModeEndpoint, false},
		{"stored endpoint no endpoint", Config{OAuthToken: testToken}, "endpoint", ModeClaudeCode, false},
		{"stored garbage", Config{Endpoint: endpoint(), OAuthToken: testToken}, "bogus", ModeEndpoint, true},
		{"nothing configured", Config{}, "", ModeEndpoint, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := New(context.Background(), tt.cfg, &fakeStore{mode: tt.stored}, &fakeBus{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, h.Mode())
			assert.Equal(t, tt.switchable, h.Switchable())
		})
	}
}

func TestNew_StoreError(t *testing.T) {
	_, err := New(context.Background(), Config{}, &fakeStore{readErr: errors.New("boom")}, &fakeBus{})
	require.Error(t, err)
}

func TestSet(t *testing.T) {
	ctx := context.Background()
	store, bus := &fakeStore{}, &fakeBus{}

	h, err := New(ctx, Config{Endpoint: endpoint(), OAuthToken: testToken}, store, bus)
	require.NoError(t, err)

	require.NoError(t, h.Set(ctx, ModeClaudeCode, "human:bob"))
	assert.Equal(t, ModeClaudeCode, h.Mode())
	assert.Equal(t, []string{"claude_code"}, store.puts)

	_, by := h.Updated()
	assert.Equal(t, "human:bob", by)

	require.Len(t, bus.events, 1)
	assert.Equal(t, events.LLMChannelChanged, bus.events[0].Type)
	assert.Empty(t, bus.events[0].Project)
	assert.Equal(t, "claude_code", bus.events[0].Data["mode"])

	require.ErrorIs(t, h.Set(ctx, Mode("bogus"), "x"), ErrUnknownMode)
	assert.Equal(t, ModeClaudeCode, h.Mode())
}

func TestSet_NotConfiguredPersistsNothing(t *testing.T) {
	ctx := context.Background()
	store, bus := &fakeStore{}, &fakeBus{}

	h, err := New(ctx, Config{Endpoint: endpoint()}, store, bus)
	require.NoError(t, err)

	require.ErrorIs(t, h.Set(ctx, ModeClaudeCode, "x"), ErrNotConfigured)
	assert.Empty(t, store.puts)
	assert.Empty(t, bus.events)
	assert.Equal(t, ModeEndpoint, h.Mode())
}

func TestSet_PersistFailureKeepsMode(t *testing.T) {
	ctx := context.Background()
	store, bus := &fakeStore{putErr: errors.New("disk")}, &fakeBus{}

	h, err := New(ctx, Config{Endpoint: endpoint(), OAuthToken: testToken}, store, bus)
	require.NoError(t, err)

	require.Error(t, h.Set(ctx, ModeClaudeCode, "x"))
	assert.Equal(t, ModeEndpoint, h.Mode())
	assert.Empty(t, bus.events)
}

func TestEffective_UnsupportedKind(t *testing.T) {
	h, err := New(context.Background(),
		Config{Endpoint: endpoint(), OAuthToken: testToken, Supported: []Kind{"chat"}},
		&fakeStore{mode: "claude_code"}, &fakeBus{})
	require.NoError(t, err)

	assert.Equal(t, ModeEndpoint, h.Effective("agent"))
	assert.Equal(t, ModeClaudeCode, h.Effective("chat"))
	assert.True(t, h.Supported("chat"))
	assert.False(t, h.Supported("agent"))
}

func TestEndpointFor_Sticky(t *testing.T) {
	h, err := New(context.Background(),
		Config{Endpoint: endpoint(), OAuthToken: testToken, Supported: []Kind{"chat"}},
		&fakeStore{}, &fakeBus{})
	require.NoError(t, err)
	require.Equal(t, ModeEndpoint, h.Mode())

	ep, m := h.EndpointFor("chat", ModeClaudeCode)
	assert.Equal(t, ModeClaudeCode, m)
	assert.Equal(t, &protocol.LLMEndpoint{Type: "claude_code", OAuthToken: testToken}, ep)

	ep, m = h.EndpointFor("agent", ModeClaudeCode)
	assert.Equal(t, ModeEndpoint, m)
	assert.Equal(t, endpoint(), ep)

	ep, m = h.EndpointFor("chat", "")
	assert.Equal(t, ModeEndpoint, m)
	assert.Equal(t, endpoint(), ep)
}

func TestEndpointFor_RecordedUnusableFallsBack(t *testing.T) {
	h, err := New(context.Background(),
		Config{Endpoint: endpoint(), Supported: []Kind{"chat"}}, &fakeStore{}, &fakeBus{})
	require.NoError(t, err)

	ep, m := h.EndpointFor("chat", ModeClaudeCode)
	assert.Equal(t, ModeEndpoint, m)
	assert.Equal(t, endpoint(), ep)
}

func TestEndpointFor_NoEndpointConfigured(t *testing.T) {
	h, err := New(context.Background(),
		Config{OAuthToken: testToken, Supported: []Kind{"chat"}}, &fakeStore{}, &fakeBus{})
	require.NoError(t, err)

	ep, m := h.EndpointFor("agent", "")
	assert.Nil(t, ep)
	assert.Equal(t, ModeEndpoint, m)
}

// stubCatalog is a ClaudeCodeCatalog over a fixed ID set.
type stubCatalog struct {
	built bool
	ids   []string
}

func (c stubCatalog) ClaudeCodeKnown(id string) (known, built bool) {
	return slices.Contains(c.ids, id), c.built
}

func TestResolveModel(t *testing.T) {
	built := stubCatalog{built: true, ids: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-2"}}

	tests := []struct {
		name    string
		catalog ClaudeCodeCatalog
		mode    Mode
		model   string
		want    string
	}{
		{"cc catalog id", built, ModeClaudeCode, "claude-opus-5-5", "claude-opus-5-5"},
		{"cc slug in catalog", built, ModeClaudeCode, "anthropic/claude-opus-5.5", "claude-opus-5-5"},
		{"cc catalog fable", built, ModeClaudeCode, "anthropic/claude-fable-5.2", "claude-fable-5-2"},
		{"cc stale slug outside catalog", built, ModeClaudeCode, "anthropic/claude-3-opus", "claude-sonnet-5-5"},
		{"cc unmappable", built, ModeClaudeCode, "openai/gpt-x", "claude-sonnet-5-5"},
		{"cc empty", built, ModeClaudeCode, "", "claude-sonnet-5-5"},
		{"cc no catalog stale slug maps", stubCatalog{}, ModeClaudeCode, "anthropic/claude-3-opus", "claude-3-opus"},
		{"cc nil catalog maps by form", nil, ModeClaudeCode, "anthropic/claude-3-opus", "claude-3-opus"},
		{"cc typed nil catalog maps by form", (*modelcatalog.Builder)(nil), ModeClaudeCode, "claude-opus-5-5", "claude-opus-5-5"},
		{"cc no catalog unmappable", stubCatalog{}, ModeClaudeCode, "openai/gpt-x", "claude-sonnet-5-5"},
		{"endpoint bare catalog id", built, ModeEndpoint, "claude-opus-5-5", "fallback/model"},
		{"endpoint bare claude id outside catalog", built, ModeEndpoint, "claude-3-opus", "fallback/model"},
		{"endpoint bare claude id without catalog", stubCatalog{}, ModeEndpoint, "claude-opus-5-5", "fallback/model"},
		{"endpoint slugged anthropic", built, ModeEndpoint, "anthropic/claude-opus-5-5", "anthropic/claude-opus-5-5"},
		{"endpoint other model", built, ModeEndpoint, "openai/gpt-x", "openai/gpt-x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := New(context.Background(),
				Config{Endpoint: endpoint(), OAuthToken: testToken, DefaultModel: "claude-sonnet-5-5", Catalog: tt.catalog},
				&fakeStore{}, &fakeBus{})
			require.NoError(t, err)

			assert.Equal(t, tt.want, h.ResolveModel(tt.mode, tt.model, "fallback/model"))
		})
	}
}

func TestHolder_TokenNeverInEvents(t *testing.T) {
	ctx := context.Background()
	bus := &fakeBus{}

	h, err := New(ctx, Config{Endpoint: endpoint(), OAuthToken: testToken}, &fakeStore{}, bus)
	require.NoError(t, err)
	require.NoError(t, h.Set(ctx, ModeClaudeCode, "human:a"))
	require.NoError(t, h.Set(ctx, ModeEndpoint, "human:a"))

	require.NotEmpty(t, bus.events)

	for _, e := range bus.events {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		assert.NotContains(t, string(b), testToken)
	}
}

func TestRecordUsage_DropsZeroResetsAt(t *testing.T) {
	ctx := context.Background()
	reset := time.Unix(2_000_000_000, 0).UTC()

	newHolder := func(t *testing.T) (*Holder, *fakeStore, *fakeBus) {
		t.Helper()

		st, bus := &fakeStore{}, &fakeBus{}
		h, err := New(ctx, Config{Endpoint: endpoint(), OAuthToken: testToken}, st, bus)
		require.NoError(t, err)

		return h, st, bus
	}

	t.Run("only the good window is stored", func(t *testing.T) {
		h, st, bus := newHolder(t)

		h.RecordUsage(ctx, []protocol.SubscriptionUsage{
			{Window: "seven_day", Utilization: 0.5, ResetsAt: reset},
			{Window: "five_hour", Utilization: 0.2, ResetsAt: time.Time{}},
		})

		assert.Len(t, st.usage, 1)
		assert.Contains(t, st.usage, "seven_day")
		require.Len(t, bus.events, 1)
		assert.Equal(t, events.LLMChannelChanged, bus.events[0].Type)
	})

	t.Run("a zero window leaves the earlier row unchanged", func(t *testing.T) {
		h, st, _ := newHolder(t)

		h.RecordUsage(ctx, []protocol.SubscriptionUsage{{Window: "seven_day", Utilization: 0.5, ResetsAt: reset}})
		h.RecordUsage(ctx, []protocol.SubscriptionUsage{{Window: "seven_day", Utilization: 0.9}})

		assert.InDelta(t, 0.5, st.usage["seven_day"].Utilization, 1e-9)
	})

	t.Run("all-zero stores and publishes nothing", func(t *testing.T) {
		h, st, bus := newHolder(t)

		h.RecordUsage(ctx, []protocol.SubscriptionUsage{{Window: "seven_day", Utilization: 0.9}})

		assert.Empty(t, st.usage)
		assert.Empty(t, bus.events)
	})

	t.Run("store error does not panic or publish", func(t *testing.T) {
		h, st, bus := newHolder(t)
		st.usageErr = errors.New("boom")

		h.RecordUsage(ctx, []protocol.SubscriptionUsage{{Window: "seven_day", ResetsAt: reset}})

		assert.Empty(t, bus.events)
	})
}

func TestSet_ConcurrentCallsStayConsistent(t *testing.T) {
	ctx := context.Background()
	st, bus := &fakeStore{}, &fakeBus{}

	h, err := New(ctx, Config{Endpoint: endpoint(), OAuthToken: testToken}, st, bus)
	require.NoError(t, err)

	var wg sync.WaitGroup

	for i := range 40 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			mode := ModeEndpoint
			if i%2 == 0 {
				mode = ModeClaudeCode
			}

			assert.NoError(t, h.Set(ctx, mode, "human:a"))
		}()

		go func() {
			defer wg.Done()

			_ = h.Mode()
			_, _ = h.Updated()
		}()
	}

	wg.Wait()

	st.mu.Lock()
	defer st.mu.Unlock()

	assert.Equal(t, string(h.Mode()), st.mode, "memory and store agree after overlapping Sets")

	bus.mu.Lock()
	defer bus.mu.Unlock()

	require.Len(t, bus.events, 40)
	assert.Equal(t, string(h.Mode()), bus.events[39].Data["mode"], "last event matches the final mode")
}

// A stale default with a below-floor AA row falls back to the configured
// default through a real Builder.
func TestResolveModel_RealBuilderBelowFloor(t *testing.T) {
	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"slug":"claude-opus-5-5","model_creator":{"name":"Anthropic"},
			 "evaluations":{"artificial_analysis_intelligence_index":60,"artificial_analysis_coding_index":null},
			 "pricing":{"price_1m_input_tokens":5,"price_1m_output_tokens":25,"price_1m_cache_hit_tokens":0.5,"price_1m_cache_write_tokens":6.25}},
			{"slug":"claude-3-opus","model_creator":{"name":"Anthropic"},
			 "evaluations":{"artificial_analysis_intelligence_index":6,"artificial_analysis_coding_index":null},
			 "pricing":{"price_1m_input_tokens":15,"price_1m_output_tokens":75,"price_1m_cache_hit_tokens":1.5,"price_1m_cache_write_tokens":18.75}}
		],"tier":"free","intelligence_index_version":5,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false}}`))
	}))
	t.Cleanup(aaSrv.Close)

	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(orSrv.Close)

	b := modelcatalog.NewBuilder("aa-key", 0.5, nil, time.Hour, modelcatalog.WithClaudeCode(),
		modelcatalog.WithCatalogURLs(aaSrv.URL, orSrv.URL))
	require.Len(t, b.ClaudeCodeCandidates(context.Background()), 1)

	h, err := New(context.Background(),
		Config{Endpoint: endpoint(), OAuthToken: testToken, DefaultModel: "claude-sonnet-5-5", Catalog: b},
		&fakeStore{}, &fakeBus{})
	require.NoError(t, err)

	assert.Equal(t, "claude-sonnet-5-5", h.ResolveModel(ModeClaudeCode, "anthropic/claude-3-opus", ""))
	assert.Equal(t, "claude-opus-5-5", h.ResolveModel(ModeClaudeCode, "anthropic/claude-opus-5.5", ""))
}
