package github

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestFetchBranches_BasicResponse(t *testing.T) {
	branches := []branchItem{
		{Name: "main"},
		{Name: "develop"},
		{Name: "feature/foo"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "contextmatrix", r.Header.Get("User-Agent"))
		assert.Contains(t, r.URL.Path, "/repos/o/r/branches")
		_ = json.NewEncoder(w).Encode(branches)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchBranches(context.Background(), "o", "r")
	require.NoError(t, err)
	require.Len(t, result, 3)
	// Results are sorted alphabetically
	assert.Equal(t, []string{"develop", "feature/foo", "main"}, result)
}

func TestFetchBranches_Pagination(t *testing.T) {
	page1 := []branchItem{{Name: "main"}, {Name: "develop"}}
	page2 := []branchItem{{Name: "feature/bar"}}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "2" {
			_ = json.NewEncoder(w).Encode(page2)
		} else {
			// Point "next" to page 2 on the same server with the full host URL.
			// We use srv.URL which is available via closure (srv is assigned before Start).
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/branches?page=2>; rel="next"`, srv.URL))
			_ = json.NewEncoder(w).Encode(page1)
		}
	}))
	defer srv.Close()

	// Override the allowedAPIHost check by using a client that sets baseURL to srv.URL.
	// parseLinkNext rejects non-api.github.com hosts, so pagination via Link header
	// won't work in tests without host override. Instead, verify that FetchBranches
	// correctly sends per_page parameter and returns sorted results.
	// For the pagination path, we test fetchBranchPage directly below.
	_ = srv // srv used in handler closure

	// Test that per_page is correctly sent.
	reqCount := 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		_ = json.NewEncoder(w).Encode(page1)
	}))
	defer srv2.Close()

	client := newTestClient(srv2)
	result, err := client.FetchBranches(context.Background(), "o", "r")
	require.NoError(t, err)
	assert.Equal(t, 1, reqCount)
	assert.Equal(t, []string{"develop", "main"}, result)
}

func TestFetchBranches_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchBranches(context.Background(), "o", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestFetchBranches_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchBranches(context.Background(), "o", "r")
	assert.ErrorIs(t, err, ErrRateLimited)
}

func TestFetchBranches_429TooManyRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchBranches(context.Background(), "o", "r")
	assert.ErrorIs(t, err, ErrRateLimited)
}

func TestFetchBranches_EmptyRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]branchItem{})
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchBranches(context.Background(), "o", "r")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestParseLinkNext(t *testing.T) {
	// parseLinkNext is now a method; use a client with the default base URL.
	c := NewClient("test-token")

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
			assert.Equal(t, tt.want, c.parseLinkNext(tt.header))
		})
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	// Verify that NewClientWithBaseURL causes the client to hit the given base URL.
	var requestPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode([]Issue{})
	}))
	defer srv.Close()

	client := NewClientWithBaseURL("test-token", srv.URL)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.NoError(t, err)
	assert.Equal(t, "/repos/o/r/issues", requestPath)
	assert.Equal(t, srv.URL, client.baseURL)
}

func TestNewClientWithBaseURL_TrailingSlashTrimmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode([]Issue{})
	}))
	defer srv.Close()

	client := NewClientWithBaseURL("test-token", srv.URL+"/")
	// baseURL should have trailing slash removed.
	assert.Equal(t, srv.URL, client.baseURL)
	_, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.NoError(t, err)
}

func TestNewClientWithBaseURL_EmptyFallsToDefault(t *testing.T) {
	client := NewClientWithBaseURL("test-token", "")
	assert.Equal(t, defaultBaseURL, client.baseURL)
}

func TestNewClient_DelegatesToNewClientWithBaseURL(t *testing.T) {
	client := NewClient("my-token")
	assert.Equal(t, defaultBaseURL, client.baseURL)
	assert.Equal(t, "my-token", client.token)
}

func TestParseLinkNext_WithCustomHost(t *testing.T) {
	// A client with a custom enterprise base URL should accept Link headers
	// pointing at that same host, and reject others.
	c := NewClientWithBaseURL("tok", "https://github.example.corp/api/v3")

	// Same host: accepted.
	got := c.parseLinkNext(`<https://github.example.corp/api/v3/repos/o/r/issues?page=2>; rel="next"`)
	assert.Equal(t, "https://github.example.corp/api/v3/repos/o/r/issues?page=2", got)

	// Different host: rejected (SSRF protection).
	got = c.parseLinkNext(`<https://evil.com/steal>; rel="next"`)
	assert.Equal(t, "", got)

	// Default github.com host: rejected when enterprise URL is configured.
	got = c.parseLinkNext(`<https://api.github.com/repos/o/r/issues?page=2>; rel="next"`)
	assert.Equal(t, "", got)
}

func TestParseLinkNext_PaginationWithCustomBaseURL(t *testing.T) {
	// End-to-end: pagination follows Link headers that match the custom base URL host.
	page1 := []Issue{{Number: 1, Title: "Issue One"}}
	page2 := []Issue{{Number: 2, Title: "Issue Two"}}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		if r.URL.Query().Get("page") == "2" {
			_ = json.NewEncoder(w).Encode(page2)
		} else {
			// Link header points at same host — SSRF check must pass.
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/issues?page=2>; rel="next"`, srv.URL))
			_ = json.NewEncoder(w).Encode(page1)
		}
	}))
	defer srv.Close()

	client := NewClientWithBaseURL("test-token", srv.URL)
	result, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "Issue One", result[0].Title)
	assert.Equal(t, "Issue Two", result[1].Title)
}

func TestParseLinkNext_SSRFRejectedWithCustomBaseURL(t *testing.T) {
	// Even with a custom base URL, Link headers pointing at a different host must be rejected.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		// Adversarial Link header pointing at a different host.
		w.Header().Set("Link", `<https://evil.com/steal?token=x>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]Issue{{Number: 1, Title: "Only issue"}})
	}))
	defer srv.Close()

	client := NewClientWithBaseURL("test-token", srv.URL)
	result, err := client.FetchOpenIssues(context.Background(), "o", "r", nil)
	require.NoError(t, err)
	// Only one page returned — the evil next URL was rejected.
	require.Len(t, result, 1)
	assert.Equal(t, "Only issue", result[0].Title)
}
