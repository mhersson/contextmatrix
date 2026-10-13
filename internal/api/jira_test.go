package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/jira"
)

type fakeJiraStatusClient struct {
	acct jira.Account
	err  error
}

func (f fakeJiraStatusClient) Myself(context.Context) (jira.Account, error) { return f.acct, f.err }

func TestJiraStatus(t *testing.T) {
	info := JiraInfo{SiteURL: "https://acme.atlassian.net", AuthMode: "basic", WritebackEnabled: true}

	tests := []struct {
		name   string
		client JiraStatusClient
		want   map[string]any
	}{
		{name: "not configured", client: nil, want: map[string]any{"configured": false}},
		{
			name:   "authenticated",
			client: fakeJiraStatusClient{acct: jira.Account{DisplayName: "Ada Lovelace"}},
			want: map[string]any{
				"configured": true, "authenticated": true, "account": "Ada Lovelace",
				"auth_mode": "basic", "writeback_enabled": true, "writeback_paused": false, "site_url": "https://acme.atlassian.net",
			},
		},
		{
			name:   "token rejected",
			client: fakeJiraStatusClient{err: fmt.Errorf("myself: %w", &jira.AuthError{})},
			want: map[string]any{
				"configured": true, "authenticated": false, "auth_mode": "basic", "writeback_enabled": true, "writeback_paused": false,
				"site_url": "https://acme.atlassian.net", "auth_error": "unauthorized",
			},
		},
		{
			name:   "site unreachable",
			client: fakeJiraStatusClient{err: errors.New("dial tcp: no route to host")},
			want: map[string]any{
				"configured": true, "authenticated": false, "auth_mode": "basic", "writeback_enabled": true, "writeback_paused": false,
				"site_url": "https://acme.atlassian.net", "auth_error": "unreachable",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &jiraHandlers{client: tt.client, info: info}

			w := httptest.NewRecorder()
			h.getStatus(w, httptest.NewRequest(http.MethodGet, "/api/jira/status", nil))
			require.Equal(t, http.StatusOK, w.Code)

			var got map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJiraAuthErrorClassifiesCaptcha(t *testing.T) {
	assert.Equal(t, "captcha_required", jiraAuthError(captchaErr()))
}

func captchaErr() error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Seraph-LoginReason", "AUTHENTICATION_DENIED")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	auth, _ := jira.NewAuthorizer("bearer", "", "t")
	_, err := jira.NewClient(srv.URL, auth, srv.Client()).Myself(context.Background())

	return err
}

type fakeWritebackStatus bool

func (f fakeWritebackStatus) Paused() bool { return bool(f) }

func TestJiraStatus_WritebackPaused(t *testing.T) {
	info := JiraInfo{SiteURL: "https://acme.atlassian.net", AuthMode: "basic", WritebackEnabled: true}

	tests := []struct {
		name      string
		writeback JiraWritebackStatus
		want      bool
	}{
		{name: "running", writeback: fakeWritebackStatus(false), want: false},
		{name: "paused", writeback: fakeWritebackStatus(true), want: true},
		{name: "writeback off", writeback: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &jiraHandlers{client: fakeJiraStatusClient{acct: jira.Account{DisplayName: "A"}}, info: info, writeback: tt.writeback}

			w := httptest.NewRecorder()
			h.getStatus(w, httptest.NewRequest(http.MethodGet, "/api/jira/status", nil))

			var got map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
			assert.Equal(t, tt.want, got["writeback_paused"])
		})
	}
}
