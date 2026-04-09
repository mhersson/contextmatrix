package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		email:      "test@example.com",
		token:      "test-token",
	}
}

func newTestClientBearer(srv *httptest.Server) *Client {
	return &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		token:      "test-pat",
	}
}

func TestFetchIssue_BasicResponse(t *testing.T) {
	issue := Issue{
		Key: "PROJ-42",
		Fields: IssueFields{
			Summary:   "Fix login bug",
			IssueType: NameField{Name: "Epic"},
			Priority:  &NameField{Name: "High"},
			Status:    NameField{Name: "In Progress"},
			Labels:    []string{"backend"},
			Components: []NameField{{Name: "auth-service"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/api/3/issue/PROJ-42")
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		assert.Equal(t, "contextmatrix", r.Header.Get("User-Agent"))
		// Basic Auth: email:token
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "test@example.com", user)
		assert.Equal(t, "test-token", pass)
		_ = json.NewEncoder(w).Encode(issue)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchIssue(context.Background(), "PROJ-42")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-42", result.Key)
	assert.Equal(t, "Fix login bug", result.Fields.Summary)
	assert.Equal(t, "Epic", result.Fields.IssueType.Name)
}

func TestFetchIssue_BearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-pat", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(Issue{Key: "PROJ-1"})
	}))
	defer srv.Close()

	client := newTestClientBearer(srv)
	result, err := client.FetchIssue(context.Background(), "PROJ-1")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-1", result.Key)
}

func TestFetchIssue_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchIssue(context.Background(), "PROJ-999")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFetchIssue_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchIssue(context.Background(), "PROJ-1")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestFetchIssue_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchIssue(context.Background(), "PROJ-1")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestFetchIssue_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.FetchIssue(context.Background(), "PROJ-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestFetchEpicChildren_BasicResponse(t *testing.T) {
	children := []Issue{
		{Key: "PROJ-10", Fields: IssueFields{Summary: "Task one"}},
		{Key: "PROJ-11", Fields: IssueFields{Summary: "Task two"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/api/3/search/jql")
		assert.Contains(t, r.URL.RawQuery, "PROJ-42")
		_ = json.NewEncoder(w).Encode(searchResult{
			StartAt:    0,
			MaxResults: 50,
			Total:      2,
			Issues:     children,
		})
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchEpicChildren(context.Background(), "PROJ-42")
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "PROJ-10", result[0].Key)
	assert.Equal(t, "PROJ-11", result[1].Key)
}

func TestFetchEpicChildren_Paginated(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		switch n {
		case 1:
			_ = json.NewEncoder(w).Encode(searchResult{
				StartAt: 0, MaxResults: 2, Total: 3,
				Issues: []Issue{
					{Key: "PROJ-1"},
					{Key: "PROJ-2"},
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(searchResult{
				StartAt: 2, MaxResults: 2, Total: 3,
				Issues: []Issue{
					{Key: "PROJ-3"},
				},
			})
		}
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchEpicChildren(context.Background(), "PROJ-42")
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestFetchEpicChildren_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{Total: 0})
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.FetchEpicChildren(context.Background(), "PROJ-42")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestPostComment_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/rest/api/3/issue/PROJ-1/comment")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body struct {
			Body string `json:"body"`
		}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Task completed", body.Body)

		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.PostComment(context.Background(), "PROJ-1", "Task completed")
	require.NoError(t, err)
}

func TestPostComment_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.PostComment(context.Background(), "PROJ-999", "comment")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPostComment_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.PostComment(context.Background(), "PROJ-1", "comment")
	assert.ErrorIs(t, err, ErrRateLimited)
}

func TestValidateURL_RejectsWrongHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.validateURL("https://evil.com/steal")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected host")
}
