package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/gitops"
)

func staticMainTestProvider(t *testing.T) githubauth.TokenGenerator {
	t.Helper()

	p, err := githubauth.NewPATProvider("test-token")
	require.NoError(t, err)

	return p
}

func cspDirectives(t *testing.T, header string) map[string]string {
	t.Helper()

	directives := map[string]string{}

	for part := range strings.SplitSeq(header, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}

		directives[fields[0]] = strings.Join(fields[1:], " ")
	}

	return directives
}

func TestSPAHandler_PolicyBlocksInlineScriptAndFrames(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":          {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/index-abc.js": {Data: []byte(`console.log("app")`)},
	}
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := newSPAHandler(apiHandler, dist)

	for _, path := range []string{"/", "/projects/demo", "/assets/index-abc.js"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			require.Equal(t, http.StatusOK, rec.Code)

			csp := cspDirectives(t, rec.Header().Get("Content-Security-Policy"))
			assert.Equal(t, "'self'", csp["script-src"])
			assert.Equal(t, "'none'", csp["frame-src"])
			assert.Equal(t, "'none'", csp["object-src"])
		})
	}
}

func TestDirHasGit_PresentAbsent(t *testing.T) {
	tmpDir := t.TempDir()

	// No .git yet - should return false.
	assert.False(t, dirHasGit(tmpDir), "dir without .git should return false")

	// Create .git directory.
	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))

	assert.True(t, dirHasGit(tmpDir), "dir with .git directory should return true")

	// Empty string - should return false.
	assert.False(t, dirHasGit(""), "empty string should return false")
}

// TestStartupPullTaskSkills_SwallowsError verifies that a pull failure
// (unreachable remote) is logged as a warning but does not panic or propagate.
func TestStartupPullTaskSkills_SwallowsError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found, skipping")
	}

	tmpDir := t.TempDir()

	mgr, err := gitops.NewManager(tmpDir, "", "test", staticMainTestProvider(t))
	require.NoError(t, err)

	// Add a bogus remote so hasRemote() returns true and PullFastForward is attempted.
	bogusURL := "https://127.0.0.1:1/does-not-exist.git"
	require.NoError(t, mgr.AddRemote(t.Context(), "origin", bogusURL))

	// Must not panic; error is swallowed with a Warn log.
	startupPullTaskSkills(true, bogusURL, mgr)
}
