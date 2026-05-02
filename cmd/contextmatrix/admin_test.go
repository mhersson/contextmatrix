package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProjectLister is a minimal projectLister for testing startProjectPumps.
type fakeProjectLister struct {
	projects []board.ProjectConfig
	err      error
}

func (f *fakeProjectLister) ListProjects(_ context.Context) ([]board.ProjectConfig, error) {
	return f.projects, f.err
}

// TestStartProjectPumps verifies the boot-time session pump seeding logic.
func TestStartProjectPumps(t *testing.T) {
	t.Run("seeds a session for each project", func(t *testing.T) {
		lister := &fakeProjectLister{
			projects: []board.ProjectConfig{
				{Name: "alpha"},
				{Name: "beta"},
			},
		}
		mgr := sessionlog.NewManager()
		t.Cleanup(func() { _ = mgr.Close(context.Background()) })

		startProjectPumps(context.Background(), lister, mgr)

		assert.True(t, mgr.HasProjectSession("alpha"), "expected active session for alpha")
		assert.True(t, mgr.HasProjectSession("beta"), "expected active session for beta")
	})

	t.Run("ListProjects error does not block boot", func(t *testing.T) {
		lister := &fakeProjectLister{err: errors.New("store unavailable")}
		mgr := sessionlog.NewManager()
		t.Cleanup(func() { _ = mgr.Close(context.Background()) })

		// Must return without panicking or blocking.
		startProjectPumps(context.Background(), lister, mgr)
	})

	t.Run("no projects means no sessions", func(t *testing.T) {
		lister := &fakeProjectLister{projects: nil}
		mgr := sessionlog.NewManager()
		t.Cleanup(func() { _ = mgr.Close(context.Background()) })

		startProjectPumps(context.Background(), lister, mgr)

		assert.False(t, mgr.HasProjectSession("nonexistent"))
	})
}

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
