package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/runner"
)

// makeRunnerHandlers returns a runnerHandlers wired to the given runner URL and API key.
func makeRunnerHandlers(runnerURL, apiKey string) *runnerHandlers {
	return &runnerHandlers{
		runner: runner.NewClient(runnerURL, apiKey),
		runnerCfg: config.RunnerConfig{
			URL:    runnerURL,
			APIKey: apiKey,
		},
	}
}

// TestStreamRunnerLogs_NoFlusher verifies that a 500 is returned when the
// ResponseWriter does not implement http.Flusher.
func TestStreamRunnerLogs_NoFlusher(t *testing.T) {
	h := makeRunnerHandlers("http://127.0.0.1:1", "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	w := &mockNonFlushingWriter{}

	h.streamRunnerLogs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.code)
	assert.Contains(t, string(w.body), "streaming not supported")
}

// TestStreamProjectSession_NoManager verifies that a 204 is returned for the
// project path when no session manager is configured.
func TestStreamProjectSession_NoManager(t *testing.T) {
	rh := &runnerHandlers{sessionManager: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs?project=myproject", nil)
	rec := newFlushRecorder()

	rh.streamRunnerLogs(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}
