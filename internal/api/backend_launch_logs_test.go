package api

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/service"
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

// captureDefaultLogs routes slog.Default() to a Debug-level buffer. It must not
// be used from parallel tests.
func captureDefaultLogs(t *testing.T) *lockedBuffer {
	t.Helper()

	buf := &lockedBuffer{}
	prev := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return buf
}

// A claude_code card launch, accepted or failed at the webhook, never logs the
// OAuth token.
func TestLaunch_ClaudeCodeTokenStaysOutOfLogs(t *testing.T) {
	logs := captureDefaultLogs(t)

	h, svc, cb, _ := channelLaunchHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})

	holder, err := llmchannel.New(context.Background(), llmchannel.Config{
		OAuthToken:   logLeakCanary,
		Supported:    []string{config.BackendNameAgent},
		DefaultModel: "claude-sonnet-5-5",
	}, memChannelStore{}, events.NewBus())
	require.NoError(t, err)
	require.NoError(t, holder.Set(context.Background(), llmchannel.ModeClaudeCode, "human:test"))

	h.llmChannel = holder

	ctx := ctxlog.WithRequestID(context.Background(), "req-canary")

	card := createRunnable(t, svc, service.CreateCardInput{})
	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.NoError(t, err)
	assert.Equal(t, logLeakCanary, cb.lastTrigger(t).LLMEndpoint.OAuthToken, "the token reaches the backend payload")

	// A refused webhook logs on the same path, so the capture cannot be empty.
	cb.server.Close()

	failing := createRunnable(t, svc, service.CreateCardInput{})
	_, err = h.launch(ctx, "test-project", failing.ID, launchOptions{})
	require.Error(t, err)

	require.NotEmpty(t, logs.String(), "the launch path logged nothing, so the check proves nothing")
	assert.NotContains(t, logs.String(), logLeakCanary)
}
