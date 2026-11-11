package llmchannel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/events"
)

const testToken = "sk-ant-oat-secret"

type fakeStore struct {
	mode    string
	puts    []string
	putErr  error
	readErr error
}

func (f *fakeStore) LLMChannel(context.Context) (string, time.Time, string, error) {
	return f.mode, time.Unix(100, 0).UTC(), "human:alice", f.readErr
}

func (f *fakeStore) PutLLMChannel(_ context.Context, mode, _ string) error {
	if f.putErr != nil {
		return f.putErr
	}

	f.puts = append(f.puts, mode)
	f.mode = mode

	return nil
}

type fakeBus struct{ events []events.Event }

func (b *fakeBus) Publish(e events.Event) { b.events = append(b.events, e) }

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

func TestResolveModel(t *testing.T) {
	h, err := New(context.Background(),
		Config{Endpoint: endpoint(), OAuthToken: testToken, DefaultModel: "claude-sonnet-5-5"},
		&fakeStore{}, &fakeBus{})
	require.NoError(t, err)

	tests := []struct {
		name  string
		mode  Mode
		model string
		want  string
	}{
		{"cc known id", ModeClaudeCode, "claude-opus-5-5", "claude-opus-5-5"},
		{"cc unknown model", ModeClaudeCode, "openai/gpt-x", "claude-sonnet-5-5"},
		{"cc empty model", ModeClaudeCode, "", "claude-sonnet-5-5"},
		{"endpoint bare cc id", ModeEndpoint, "claude-haiku-5-5", "fallback/model"},
		{"endpoint slugged anthropic", ModeEndpoint, "anthropic/claude-haiku-5-5", "anthropic/claude-haiku-5-5"},
		{"endpoint other model", ModeEndpoint, "openai/gpt-x", "openai/gpt-x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
