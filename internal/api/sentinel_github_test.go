package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/githubauth"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

func TestSentinelGitHubParsesThePRURL(t *testing.T) {
	owner, repo, n, err := parsePullURL("https://github.example.net/o/r/pull/7")
	require.NoError(t, err)
	assert.Equal(t, "o", owner)
	assert.Equal(t, "r", repo)
	assert.Equal(t, 7, n)

	for _, bad := range []string{"https://x/o/r/issues/7", "https://x/o/r/pull/x", "https://x/o/pull/7", "https://x/o/r/pull/7/files", ""} {
		_, _, _, err := parsePullURL(bad)
		assert.Error(t, err, bad)
	}
}

// githubAdapter is a sentinelGitHub reading through a test server that
// answers every request with handle.
func githubAdapter(t *testing.T, handle http.HandlerFunc) *sentinelGitHub {
	t.Helper()

	srv := httptest.NewServer(handle)
	t.Cleanup(srv.Close)

	p, err := githubauth.NewPATProvider("test-token")
	require.NoError(t, err)

	return &sentinelGitHub{providerForProject: func(context.Context, string) (githubauth.TokenGenerator, string, error) {
		return p, srv.URL, nil
	}}
}

const adapterPR = "https://github.com/o/r/pull/7"

func TestSentinelGitHubPullState(t *testing.T) {
	tests := []struct {
		name string
		pull map[string]any
		want sentinel.PullState
	}{
		{"open", map[string]any{"state": "open", "head": map[string]string{"sha": "h1"}}, sentinel.PullState{Open: true, HeadSHA: "h1"}},
		{
			"merged",
			map[string]any{"state": "closed", "merged": true, "merge_commit_sha": "m1", "head": map[string]string{"sha": "h1"}},
			sentinel.PullState{Merged: true, HeadSHA: "h1", MergeSHA: "m1"},
		},
		{"closed", map[string]any{"state": "closed", "head": map[string]string{"sha": "h1"}}, sentinel.PullState{HeadSHA: "h1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := githubAdapter(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/repos/o/r/pulls/7", r.URL.Path)

				_ = json.NewEncoder(w).Encode(tt.pull)
			})

			got, err := g.PullState(context.Background(), "alpha", adapterPR)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSentinelGitHubPullStateErrors(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		g := githubAdapter(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })

		_, err := g.PullState(context.Background(), "alpha", adapterPR)
		assert.Error(t, err, "status %d", code)
	}
}

func TestSentinelGitHubFileAt(t *testing.T) {
	t.Run("a file", func(t *testing.T) {
		g := githubAdapter(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/repos/o/r/contents/docs/AGENTS.md", r.URL.Path)
			assert.Equal(t, "m1", r.URL.Query().Get("ref"))

			_ = json.NewEncoder(w).Encode(map[string]string{
				"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("# Agents\n")),
			})
		})

		content, found, err := g.FileAt(context.Background(), "alpha", adapterPR, "docs/AGENTS.md", "m1")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "# Agents\n", content)
	})

	t.Run("a missing file is not found, not an error", func(t *testing.T) {
		g := githubAdapter(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })

		content, found, err := g.FileAt(context.Background(), "alpha", adapterPR, "AGENTS.md", "m1")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, content)
	})

	t.Run("any other failure stays an error", func(t *testing.T) {
		for _, code := range []int{http.StatusForbidden, http.StatusInternalServerError} {
			g := githubAdapter(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })

			_, found, err := g.FileAt(context.Background(), "alpha", adapterPR, "AGENTS.md", "m1")
			require.Error(t, err, "status %d", code)
			assert.False(t, found)
		}
	})
}
