package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const issueJSON = `{
  "id": "10042", "key": "PROJ-7",
  "fields": {
    "summary": "Add login",
    "issuetype": {"name": "Story", "hierarchyLevel": 0, "subtask": false},
    "status": {"name": "To Do", "statusCategory": {"key": "new"}},
    "priority": {"name": "High"},
    "project": {"key": "PROJ"},
    "components": [{"name": "Frontend"}, {"name": "API"}],
    "parent": {"key": "PROJ-1"},
    "description": {"type": "doc", "version": 1, "content": []}
  }
}`

func TestClient_Myself(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/3/myself", r.URL.Path)

		_, _ = io.WriteString(w, `{"accountId":"5b10","displayName":"Ada Lovelace","emailAddress":"ada@acme.com"}`)
	})

	acct, err := c.Myself(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Account{AccountID: "5b10", DisplayName: "Ada Lovelace", Email: "ada@acme.com"}, acct)
}

func TestClient_Issue(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Issue
	}{
		{
			name: "all fields",
			body: issueJSON,
			want: Issue{
				ID: "10042", Key: "PROJ-7", Summary: "Add login",
				IssueType:   IssueType{Name: "Story", HierarchyLevel: 0},
				Status:      Status{Name: "To Do", CategoryKey: "new"},
				Priority:    "High",
				ProjectKey:  "PROJ",
				Components:  []string{"Frontend", "API"},
				ParentKey:   "PROJ-1",
				Description: json.RawMessage(`{"type": "doc", "version": 1, "content": []}`),
			},
		},
		{
			name: "null description, no priority, parent, project or components",
			body: `{"id":"7","key":"OPS-3","fields":{"summary":"s","issuetype":{"name":"Sub-task","hierarchyLevel":-1,"subtask":true},
			"status":{"name":"Done","statusCategory":{"key":"done"}},"priority":null,"description":null}}`,
			want: Issue{
				ID: "7", Key: "OPS-3", Summary: "s",
				IssueType:  IssueType{Name: "Sub-task", HierarchyLevel: -1, Subtask: true},
				Status:     Status{Name: "Done", CategoryKey: "done"},
				ProjectKey: "OPS",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Query().Get("fields"), "description")

				_, _ = io.WriteString(w, tt.body)
			})

			got, err := c.Issue(t.Context(), tt.want.Key)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClient_InvalidKeyNeverCallsJira(t *testing.T) {
	var calls atomic.Int32

	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	for _, key := range []string{"../myself", "PROJ-1/transitions", "proj-1", ""} {
		_, err := c.Issue(t.Context(), key)
		require.ErrorIs(t, err, ErrInvalidRef)

		_, err = c.Transitions(t.Context(), key)
		require.ErrorIs(t, err, ErrInvalidRef)

		require.ErrorIs(t, c.Transition(t.Context(), key, "1", nil), ErrInvalidRef)
	}

	assert.Zero(t, calls.Load())
}

func TestClient_Search(t *testing.T) {
	tests := []struct {
		name       string
		pages      []string
		wantKeys   []string
		wantTokens []string
		wantErr    bool
	}{
		{
			name: "follows nextPageToken until isLast",
			pages: []string{
				`{"issues":[` + issueJSON + `],"nextPageToken":"t1","isLast":false}`,
				`{"issues":[{"id":"2","key":"PROJ-8","fields":{"summary":"b"}}],"isLast":true}`,
			},
			wantKeys:   []string{"PROJ-7", "PROJ-8"},
			wantTokens: []string{"", "t1"},
		},
		{
			name:       "stops when the token is absent and isLast is omitted",
			pages:      []string{`{"issues":[{"id":"2","key":"PROJ-8","fields":{}}]}`},
			wantKeys:   []string{"PROJ-8"},
			wantTokens: []string{""},
		},
		{
			name: "repeated token is an error",
			pages: []string{
				`{"issues":[],"nextPageToken":"t1"}`,
				`{"issues":[],"nextPageToken":"t1"}`,
			},
			wantTokens: []string{"", "t1"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				call   int
				tokens []string
			)

			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/rest/api/3/search/jql", r.URL.Path)

				var req struct {
					JQL           string   `json:"jql"`
					Fields        []string `json:"fields"`
					MaxResults    int      `json:"maxResults"`
					NextPageToken string   `json:"nextPageToken"`
				}

				assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				assert.Equal(t, "parent = PROJ-1", req.JQL)
				assert.Contains(t, req.Fields, "issuetype")
				assert.Equal(t, 100, req.MaxResults)

				tokens = append(tokens, req.NextPageToken)

				_, _ = io.WriteString(w, tt.pages[min(call, len(tt.pages)-1)])
				call++
			})

			got, err := c.Search(t.Context(), "parent = PROJ-1")
			assert.Equal(t, tt.wantTokens, tokens)

			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			keys := make([]string, 0, len(got))
			for _, is := range got {
				keys = append(keys, is.Key)
			}

			assert.Equal(t, tt.wantKeys, keys)
		})
	}
}

func TestClient_Transitions(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/3/issue/PROJ-7/transitions", r.URL.Path)
		assert.Equal(t, "transitions.fields", r.URL.Query().Get("expand"))

		_, _ = io.WriteString(w, `{"transitions":[
		{"id":"21","name":"Start","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}},
		{"id":"31","name":"Close","to":{"name":"Done","statusCategory":{"key":"done"}},
		 "fields":{"resolution":{"required":true},"comment":{"required":false}}}]}`)
	})

	got, err := c.Transitions(t.Context(), "PROJ-7")
	require.NoError(t, err)
	assert.Equal(t, []Transition{
		{ID: "21", Name: "Start", To: Status{Name: "In Progress", CategoryKey: "indeterminate"}, Fields: map[string]TransitionField{}},
		{
			ID: "31", Name: "Close", To: Status{Name: "Done", CategoryKey: "done"},
			Fields: map[string]TransitionField{"resolution": {Required: true}, "comment": {Required: false}},
		},
	}, got)
}

func TestClient_Transition(t *testing.T) {
	tests := []struct {
		name     string
		fields   map[string]any
		wantBody string
	}{
		{name: "no fields", wantBody: `{"transition":{"id":"31"}}`},
		{
			name:     "with resolution",
			fields:   map[string]any{"resolution": map[string]string{"name": "Done"}},
			wantBody: `{"fields":{"resolution":{"name":"Done"}},"transition":{"id":"31"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/rest/api/3/issue/PROJ-7/transitions", r.URL.Path)

				body, _ := io.ReadAll(r.Body)
				assert.JSONEq(t, tt.wantBody, string(body))
				w.WriteHeader(http.StatusNoContent)
			})

			require.NoError(t, c.Transition(t.Context(), "PROJ-7", "31", tt.fields))
		})
	}
}

func TestClient_SearchStopsAtPageCap(t *testing.T) {
	var calls atomic.Int64

	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)

		_, _ = io.WriteString(w, fmt.Sprintf(`{"issues":[],"nextPageToken":"t%d","isLast":false}`, n))
	})

	_, err := c.Search(t.Context(), "parent = PROJ-1")
	require.Error(t, err)
	assert.Equal(t, int64(maxSearchPages), calls.Load())
}
