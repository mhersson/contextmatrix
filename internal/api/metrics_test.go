package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/metrics"
)

// TestMetricsEndpoint verifies that after a sample API request hits the
// observe middleware, contextmatrix_http_requests_total and
// contextmatrix_http_request_duration_seconds appear in the promhttp output.
// A client only reaches /metrics via the admin listener (see
// cmd/contextmatrix/main.go), but the collector behaves the same when
// scraped from an isolated test registry.
func TestMetricsEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.Register(reg)

	svc, _, cleanup := testSetup(t)
	defer cleanup()

	ph := &projectHandlers{svc: svc, runnerEnabled: false}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/projects", ph.listProjects)

	apiServer := httptest.NewServer(chain(apiMux, requestID, observe))
	defer apiServer.Close()

	metricsServer := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer metricsServer.Close()

	resp, err := http.Get(apiServer.URL + "/api/projects")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	metricsResp, err := http.Get(metricsServer.URL)

	require.NoError(t, err)
	defer closeBody(t, metricsResp.Body)

	assert.Equal(t, http.StatusOK, metricsResp.StatusCode)

	body, err := io.ReadAll(metricsResp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.Contains(t, bodyStr, "contextmatrix_http_requests_total",
		"expected contextmatrix_http_requests_total in /metrics output")
	assert.Contains(t, bodyStr, "contextmatrix_http_request_duration_seconds",
		"expected contextmatrix_http_request_duration_seconds in /metrics output")
	assert.Contains(t, bodyStr, `path="GET /api/projects"`,
		"expected matched pattern label, got:\n%s", bodyStr)
}

// TestObserve_ExcludesMCPSSE verifies that GET /mcp — the MCP Streamable HTTP
// SSE endpoint — is skipped by the metrics middleware. A long-lived MCP
// session would otherwise dominate the REST latency histogram (multi-hour
// durations poison p95/p99) and bloat the request counter.
func TestObserve_ExcludesMCPSSE(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.Register(reg)

	// Stub MCP handler returns immediately so the observe middleware has a
	// chance to record metrics if the exclusion is not honored.
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	apiMux := http.NewServeMux()
	apiMux.Handle("GET /mcp", mcpHandler)
	apiMux.Handle("POST /mcp", mcpHandler)

	apiServer := httptest.NewServer(chain(apiMux, requestID, observe))
	defer apiServer.Close()

	// GET /mcp should NOT be observed.
	resp, err := http.Get(apiServer.URL + "/mcp")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// POST /mcp (a regular request-response round-trip) SHOULD be observed
	// so we still get visibility into MCP tool call latency.
	postResp, err := http.Post(apiServer.URL+"/mcp", "application/json", strings.NewReader("{}"))

	require.NoError(t, err)
	defer closeBody(t, postResp.Body)

	metricsServer := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer metricsServer.Close()

	metricsResp, err := http.Get(metricsServer.URL)

	require.NoError(t, err)
	defer closeBody(t, metricsResp.Body)

	body, err := io.ReadAll(metricsResp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.NotContains(t, bodyStr, `method="GET",path="GET /mcp"`,
		"GET /mcp must be excluded from the REST latency histogram and counter, got:\n%s", bodyStr)
	assert.Contains(t, bodyStr, `path="POST /mcp"`,
		"POST /mcp should still be observed (non-SSE request/response), got:\n%s", bodyStr)
}

// TestObserve_UnmatchedCollapsesLabel verifies that 404s and unknown paths
// land on a single constant-label series instead of creating one series per
// distinct URL. Without this, a path-scan attacker would blow up the registry.
func TestObserve_UnmatchedCollapsesLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.Register(reg)

	apiMux := http.NewServeMux()

	apiServer := httptest.NewServer(chain(apiMux, requestID, observe))
	defer apiServer.Close()

	for i := range 25 {
		path := apiServer.URL + "/does/not/exist/" + strings.Repeat("x", i)
		resp, err := http.Get(path)
		require.NoError(t, err)
		closeBody(t, resp.Body)
	}

	metricsServer := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer metricsServer.Close()

	metricsResp, err := http.Get(metricsServer.URL)

	require.NoError(t, err)
	defer closeBody(t, metricsResp.Body)

	body, err := io.ReadAll(metricsResp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	// Exactly one series per (method, status) for the unmatched bucket —
	// the full path must NEVER appear as a label value.
	assert.Contains(t, bodyStr, `path="unmatched"`,
		"expected unmatched path label, got:\n%s", bodyStr)
	assert.NotContains(t, bodyStr, "/does/not/exist/",
		"raw URL path leaked into metric labels (cardinality bomb)")
}
