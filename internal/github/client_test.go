package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		httpClient: srv.Client(),
		token:      "test-token",
		baseURL:    srv.URL,
	}
}

func TestFetchOpenIssues_BasicResponse(t *testing.T) {
	issues := []Issue{
		{Number: 1, Title: "Bug report", Body: "Details", HTMLURL: "https://github.com/o/r/issues/1",
			Labels: []Label{{Name: "bug"}}},
		{Number: 2, Title: "Feature request", Body: "More details", HTMLURL: "https://github.com/o/r/issues/2"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "contextmatrix", r.Header.Get("User-Agent"))
		assert.Contains(t, r.URL.Path, "/repos/o/r/issues")
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "Bug report", result[0].Title)
	assert.Equal(t, "Feature request", result[1].Title)
}

func TestFetchOpenIssues_ExcludesPullRequests(t *testing.T) {
	pr := &struct{}{}
	issues := []Issue{
		{Number: 1, Title: "Real issue"},
		{Number: 2, Title: "A PR", PullRequest: pr},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Real issue", result[0].Title)
}

func TestFetchOpenIssues_LabelFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		labels := r.URL.Query().Get("labels")
		assert.Equal(t, "bug,good first issue", labels)
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode([]Issue{})
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", []string{"bug", "good first issue"})
	require.NoError(t, err)
}

func TestFetchOpenIssues_URLEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Owner/repo with special chars should be path-escaped.
		assert.Contains(t, r.RequestURI, "/repos/my%20org/my%20repo/issues")
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode([]Issue{})
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchOpenIssues(context.Background(), "my org", "my repo", nil)
	require.NoError(t, err)
}

func TestFetchOpenIssues_RateLimitedMidPage(t *testing.T) {
	// When remaining=0 on a 200, the current page data should still be returned
	// along with an ErrRateLimited error.
	issues := []Issue{{Number: 1, Title: "Valid issue"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	assert.ErrorIs(t, err, ErrRateLimited)
	// The valid data from this page should still be returned.
	require.Len(t, result, 1)
	assert.Equal(t, "Valid issue", result[0].Title)
}

func TestFetchOpenIssues_403Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	assert.ErrorIs(t, err, ErrRateLimited)
}

func TestFetchOpenIssues_429TooManyRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	assert.ErrorIs(t, err, ErrRateLimited)
}

func TestFetchOpenIssues_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.Contains(t, err.Error(), "internal error")
}

func TestFetchOpenIssues_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestParseLinkNext(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "with next",
			header: `<https://api.github.com/repos/o/r/issues?page=2>; rel="next", <https://api.github.com/repos/o/r/issues?page=5>; rel="last"`,
			want:   "https://api.github.com/repos/o/r/issues?page=2",
		},
		{
			name:   "no next",
			header: `<https://api.github.com/repos/o/r/issues?page=5>; rel="last"`,
			want:   "",
		},
		{
			name:   "empty header",
			header: "",
			want:   "",
		},
		{
			name:   "non-github host rejected",
			header: `<https://evil.com/steal?token=x>; rel="next"`,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseLinkNext(tt.header))
		})
	}
}
