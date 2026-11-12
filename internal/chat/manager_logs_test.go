package chat_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

const logLeakCanary = "sk-ant-oat01-LOGLEAKCANARY"

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func debugLogger() (*slog.Logger, *lockedBuffer) {
	buf := &lockedBuffer{}

	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func canaryChannel(t *testing.T, supported ...string) *llmchannel.Holder {
	t.Helper()

	h, err := llmchannel.New(context.Background(), llmchannel.Config{
		Endpoint:     channelEndpoint(),
		OAuthToken:   logLeakCanary,
		Supported:    supported,
		DefaultModel: "claude-sonnet-5-5",
	}, memChannelStore{}, events.NewBus())
	require.NoError(t, err)
	require.NoError(t, h.Set(context.Background(), llmchannel.ModeClaudeCode, "human:test"))

	return h
}

// A claude_code chat cold open never logs the OAuth token.
func TestOpenCold_ClaudeCodeTokenStaysOutOfLogs(t *testing.T) {
	ctx := context.Background()
	logger, logs := debugLogger()

	mgr, backend, _ := newManagerWithStubsAndConfig(t, chat.Config{
		IdleTTL:      time.Hour,
		DefaultModel: "chat/default",
		LLMChannel:   canaryChannel(t, config.BackendNameChat),
		Logger:       logger,
	})

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web"})
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	require.NotNil(t, backend.startOpts().LLMEndpoint)
	assert.Equal(t, protocol.LLMEndpointTypeClaudeCode, backend.startOpts().LLMEndpoint.Type)
	assert.Equal(t, logLeakCanary, backend.startOpts().LLMEndpoint.OAuthToken, "the token reaches the backend start")

	require.NotEmpty(t, logs.String(), "the cold open logged nothing, so the check proves nothing")
	assert.NotContains(t, logs.String(), logLeakCanary)
}

// A claude_code blueprint cold open and restart never log the OAuth token.
func TestArchitectStarts_ClaudeCodeTokenStaysOutOfLogs(t *testing.T) {
	ctx := context.Background()
	logger, logs := debugLogger()
	ch := canaryChannel(t, config.BackendNameChat, config.BackendNameArchitect)

	mgr, _, arch := newManagerWithArchitectStub(t, func(c *chat.Config) {
		c.LLMChannel = ch
		c.Logger = logger
	})

	bp := openArchitect(t, mgr)
	assert.Equal(t, logLeakCanary, arch.startOpts().LLMEndpoint.OAuthToken, "the token reaches the backend start")

	arch.mu.Lock()
	arch.sendErrSeq = []error{errSessionUnknown(), nil}
	arch.mu.Unlock()

	_, err := mgr.SendUserMessage(ctx, bp.ID, "approve")
	require.NoError(t, err)
	require.Equal(t, int64(2), arch.startCalls.Load(), "one restart")
	assert.Equal(t, logLeakCanary, arch.startOpts().LLMEndpoint.OAuthToken)

	require.NotEmpty(t, logs.String(), "the start paths logged nothing, so the check proves nothing")
	assert.NotContains(t, logs.String(), logLeakCanary)
}
