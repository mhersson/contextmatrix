package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdminMux_Pprof exercises the pprof endpoints served by newAdminMux via
// httptest so we do not have to race a real listener. Binding to an ephemeral
// port and waiting for it to accept via a dial loop is flaky on loaded CI.
func TestAdminMux_Pprof(t *testing.T) {
	srv := httptest.NewServer(newAdminMux())
	t.Cleanup(srv.Close)

	endpoints := []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine",
		"/debug/pprof/heap",
		"/debug/pprof/cmdline",
		"/debug/pprof/symbol",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(srv.URL + ep)
			require.NoError(t, err)

			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

// TestAdminMux_MetricsExposed verifies that the admin mux serves the
// Prometheus text format at /metrics.
func TestAdminMux_MetricsExposed(t *testing.T) {
	srv := httptest.NewServer(newAdminMux())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/metrics")
	require.NoError(t, err)

	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), "# HELP",
		"expected Prometheus text format, got:\n%s", body)
}

// TestAdminMux_DoesNotMountDefaultServeMux ensures the admin mux is narrowly
// scoped: something registered on http.DefaultServeMux does NOT leak through
// the admin listener.
//
// Swap http.DefaultServeMux out for a fresh one for the duration of the test
// so that a re-run (e.g. `go test -count=2`) does not panic on the duplicate
// pattern registration that http.HandleFunc would otherwise produce.
func TestAdminMux_DoesNotMountDefaultServeMux(t *testing.T) {
	originalMux := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()

	t.Cleanup(func() { http.DefaultServeMux = originalMux })

	// Register a unique handler on the (swapped) DefaultServeMux. If the admin
	// mux ever regressed to delegating to DefaultServeMux this would start
	// serving.
	probePath := "/admin-mux-leak-probe-" + strings.ReplaceAll(t.Name(), "/", "-")

	http.HandleFunc(probePath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	srv := httptest.NewServer(newAdminMux())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + probePath)
	require.NoError(t, err)

	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"admin mux should not serve paths registered on http.DefaultServeMux")
}
