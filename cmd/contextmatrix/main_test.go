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

	"github.com/mhersson/contextmatrix/internal/githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/gitops"
)

// TestMain drops the repository variables git exports to hooks, `rebase
// --exec` and `bisect run`: with GIT_DIR set, the tests' git calls in temp
// dirs would act on the repository running them.
func TestMain(m *testing.M) {
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX"} {
		_ = os.Unsetenv(k)
	}

	os.Exit(m.Run())
}

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

func TestSPAHandler_SecurityHeaders(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":          {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/index-abc.js": {Data: []byte(`console.log("app")`)},
	}
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := newSPAHandler(apiHandler, dist)

	for _, path := range []string{"/", "/projects/demo", "/projects/demo?card=DEMO-001", "/assets/index-abc.js", "/mockup-frame.html/x", "/projects/demo/mockup-frame.html"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			require.Equal(t, http.StatusOK, rec.Code)

			csp := cspDirectives(t, rec.Header().Get("Content-Security-Policy"))
			assert.Equal(t, "'self'", csp["script-src"])
			assert.Equal(t, "'self'", csp["frame-src"])
			assert.Equal(t, "'none'", csp["object-src"])
			assert.Equal(t, "'self'", csp["base-uri"])
			assert.Equal(t, "'none'", csp["frame-ancestors"])
			assert.Equal(t, "'self'", csp["form-action"])
			assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
			assert.Empty(t, rec.Header().Get("X-DNS-Prefetch-Control"), "the prefetch header is the shell's alone")
		})
	}
}

// The shell seals itself: an iframe that frames it without a sandbox attribute
// (an injected one, or a caller that forgot it) still gets an opaque origin and
// no fetch, subresource or framed-URL load.
func TestSPAHandler_MockupFrameIsSealed(t *testing.T) {
	shell := []byte("<!doctype html><script>/* shell */</script>")
	handler := newSPAHandler(http.NotFoundHandler(), fstest.MapFS{
		"index.html":        {Data: []byte("<!doctype html>")},
		"mockup-frame.html": {Data: shell},
	})

	for _, target := range []string{"/mockup-frame.html", "/mockup-frame.html?source=%3Cscript%3Ealert(1)%3C/script%3E"} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, shell, rec.Body.Bytes(), "the shell reads nothing from the request")
			assert.Equal(t, map[string]string{
				"default-src":     "'none'",
				"script-src":      "'unsafe-inline'",
				"style-src":       "'unsafe-inline'",
				"img-src":         "data:",
				"form-action":     "'none'",
				"base-uri":        "'none'",
				"frame-ancestors": "'self'",
				"sandbox":         "allow-scripts",
			}, cspDirectives(t, rec.Header().Get("Content-Security-Policy")))
			assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "off", rec.Header().Get("X-DNS-Prefetch-Control"))
			assert.Empty(t, rec.Header().Get("X-Frame-Options"))
		})
	}
}

// A structural check: the path the handler seals is the file the frontend ships.
func TestMockupFramePath_NamesTheShippedShell(t *testing.T) {
	_, err := os.Stat(filepath.Join("..", "..", "web", "public", strings.TrimPrefix(mockupFramePath, "/")))
	require.NoError(t, err)
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
