package api

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/runner"
)

// sseHTTPClient is a dedicated client for SSE upstream proxying.
// Timeout must be zero so that long-lived streaming connections are not
// terminated by a per-request deadline.
var sseHTTPClient = &http.Client{Timeout: 0}

// streamRunnerLogs handles GET /api/runner/logs?project=
// It proxies the runner's SSE log stream to the browser.
func (h *runnerHandlers) streamRunnerLogs(w http.ResponseWriter, r *http.Request) {
	// 1. Assert Flusher — required for SSE.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "streaming not supported", "")
		return
	}

	// 2. Read optional project query param.
	project := r.URL.Query().Get("project")

	// 3. Clear write deadline — SSE connections are long-lived and must survive
	// past the server's WriteTimeout (see events.go for the full explanation).
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("runner SSE could not clear write deadline", "error", err)
	}

	// 4. Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// writeSSEError writes a terminal SSE error event and flushes.
	writeSSEError := func(msg string) {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"content\":%q}\n\n", msg)
		flusher.Flush()
	}

	// 5. Build upstream URL.
	upstreamURL := h.runnerCfg.URL + "/logs"
	if project != "" {
		upstreamURL += "?project=" + url.QueryEscape(project)
	}

	// 6. Build HMAC-signed GET request.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		slog.Error("runner logs: failed to create upstream request", "error", err)
		writeSSEError("runner unavailable")
		return
	}
	sigHeader, tsHeader := runner.SignRequestHeaders(h.runnerCfg.APIKey, []byte{})
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	// 7 & 8. Use dedicated zero-timeout client; issue request using browser context.
	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		slog.Error("runner logs: upstream request failed", "error", err)
		writeSSEError("runner unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// 9. Non-200 upstream → error event and close.
	if resp.StatusCode != http.StatusOK {
		slog.Warn("runner logs: upstream returned non-200", "status", resp.StatusCode)
		writeSSEError("runner unavailable")
		return
	}

	slog.Info("runner SSE proxy connected",
		"project", project,
		"remote_addr", r.RemoteAddr,
	)

	// 10. Read upstream body line-by-line with a 1MB scanner buffer.
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1<<20)

	// 11 & 12. Forward SSE data and keepalive comment lines; flush after each.
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") || strings.HasPrefix(line, ":") {
			if _, err := fmt.Fprintf(w, "%s\n\n", line); err != nil {
				slog.Debug("runner SSE: browser write failed", "error", err)
				return
			}
			flusher.Flush()
		}

		// 13. Check browser disconnect between lines.
		select {
		case <-r.Context().Done():
			slog.Info("runner SSE proxy: browser disconnected",
				"project", project,
				"remote_addr", r.RemoteAddr,
			)
			return
		default:
		}
	}

	// Scanner stopped — upstream closed or errored.
	if err := scanner.Err(); err != nil {
		// Context cancellation from browser disconnect causes the body read to
		// return an error. That is handled above by the select on r.Context().Done()
		// between scan iterations; if we reach here after ctx is done, it is benign.
		if r.Context().Err() != nil {
			slog.Info("runner SSE proxy: browser disconnected",
				"project", project,
				"remote_addr", r.RemoteAddr,
			)
			return
		}
		slog.Error("runner logs: upstream scanner error", "error", err)
	}
	writeSSEError("runner connection lost")
}
