package ctxlog_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// restoreSlogDefault captures the current slog.Default() and restores it at
// test cleanup. Without this, tests that swap in a custom default with
// slog.SetDefault(slog.New(slog.NewTextHandler(nil, nil))) crash subsequent
// tests whose handlers try to write to the nil io.Writer.
func restoreSlogDefault(t *testing.T) {
	t.Helper()

	orig := slog.Default()

	t.Cleanup(func() { slog.SetDefault(orig) })
}

func TestWithRequestID_Logger_roundtrip(t *testing.T) {
	restoreSlogDefault(t)

	var buf bytes.Buffer

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	const wantID = "test-request-id-abc123"

	ctx := ctxlog.WithRequestID(context.Background(), wantID)
	logger := ctxlog.Logger(ctx)
	require.NotNil(t, logger)

	logger.Info("hello from test")

	output := buf.String()
	assert.Contains(t, output, "request_id="+wantID,
		"log output should contain the request_id attribute")
	assert.Contains(t, output, "hello from test",
		"log output should contain the message")
}

func TestLogger_fallback(t *testing.T) {
	got := ctxlog.Logger(context.Background())
	assert.Equal(t, slog.Default(), got,
		"Logger on empty context should return slog.Default()")
}

func TestWithRequestID_differentIDs(t *testing.T) {
	restoreSlogDefault(t)

	var buf bytes.Buffer

	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctxA := ctxlog.WithRequestID(context.Background(), "id-AAA")
	ctxB := ctxlog.WithRequestID(context.Background(), "id-BBB")

	ctxlog.Logger(ctxA).Info("msg-for-AAA")
	ctxlog.Logger(ctxB).Info("msg-for-BBB")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)

	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "id-AAA")
	assert.Contains(t, joined, "id-BBB")
	assert.Contains(t, joined, "msg-for-AAA")
	assert.Contains(t, joined, "msg-for-BBB")
}
