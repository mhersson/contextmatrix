package main

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend/sessionlog"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/jira"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func TestWireJira(t *testing.T) {
	c, err := wireJira(config.JiraConfig{})
	require.NoError(t, err)
	assert.Nil(t, c, "unconfigured builds nothing")

	c, err = wireJira(config.JiraConfig{SiteURL: "https://acme.atlassian.net", AuthMode: "basic", Email: "me@acme.com", Token: "t"})
	require.NoError(t, err)
	assert.NotNil(t, c)

	_, err = wireJira(config.JiraConfig{SiteURL: "https://acme.atlassian.net", AuthMode: "basic", Token: "secret-t"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-t")
}

func TestUnknownJiraTargets(t *testing.T) {
	mapping := map[string]config.JiraProjectMapping{
		"PROJ": {Default: "api", Components: map[string]string{"Frontend": "web", "Ops": "ghost"}},
		"OPS":  {Default: "missing"},
	}
	projects := []board.ProjectConfig{{Name: "api"}, {Name: "web"}}

	assert.Equal(t, []string{"OPS -> missing", "PROJ -> ghost"}, unknownJiraTargets(mapping, projects))
}

type noCards struct{}

func (noCards) GetCard(context.Context, string, string) (*board.Card, error) {
	return nil, storage.ErrCardNotFound
}
func (noCards) ListProjects(context.Context) ([]board.ProjectConfig, error) { return nil, nil }
func (noCards) ListCards(context.Context, string, storage.CardFilter) ([]*board.Card, error) {
	return nil, nil
}

func (noCards) AddSystemLogEntry(context.Context, string, string, board.ActivityEntry) (*board.Card, error) {
	return nil, storage.ErrCardNotFound
}

func TestWireJiraWriteback_DisabledConstructsNothing(t *testing.T) {
	client, err := wireJira(config.JiraConfig{SiteURL: "https://acme.atlassian.net", AuthMode: "basic", Email: "e", Token: "t"})
	require.NoError(t, err)

	tests := []struct {
		name   string
		cfg    config.JiraConfig
		client *jira.Client
		want   bool
	}{
		{name: "disabled", cfg: config.JiraConfig{SiteURL: "https://acme.atlassian.net"}, client: client},
		{name: "enabled without client", cfg: config.JiraConfig{Writeback: config.JiraWritebackConfig{Enabled: true}}},
		{
			name:   "enabled",
			cfg:    config.JiraConfig{SiteURL: "https://acme.atlassian.net", Writeback: config.JiraWritebackConfig{Enabled: true}},
			client: client, want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			store, err := opsqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			wb, err := wireJiraWriteback(ctx, tt.cfg, tt.client, events.NewBus(), noCards{}, store)
			require.NoError(t, err)
			assert.Equal(t, tt.want, wb != nil)

			if wb != nil {
				cancel()
				wb.Wait()
			}
		})
	}
}

func TestRunShutdownSequence_JoinsJiraWriteback(t *testing.T) {
	client, err := wireJira(config.JiraConfig{SiteURL: "https://acme.atlassian.net", AuthMode: "basic", Email: "e", Token: "t"})
	require.NoError(t, err)

	tests := []struct {
		name    string
		enabled bool
	}{
		{name: "writeback running", enabled: true},
		{name: "writeback off", enabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx, appCancel := context.WithCancel(t.Context())
			defer appCancel()

			comps := shutdownComponents{
				HTTPServer: &http.Server{ReadHeaderTimeout: time.Second},
				SessionLog: sessionlog.NewManager(),
				HTTPCancel: func() {},
				AppCancel:  appCancel,
			}

			if tt.enabled {
				store, err := opsqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = store.Close() })

				cfg := config.JiraConfig{SiteURL: "https://acme.atlassian.net", Writeback: config.JiraWritebackConfig{Enabled: true}}
				wb, err := wireJiraWriteback(appCtx, cfg, client, events.NewBus(), noCards{}, store)
				require.NoError(t, err)
				require.NotNil(t, wb)

				comps.JiraWriteback = wb
			}

			done := make(chan error, 1)

			go func() { done <- runShutdownSequence(t.Context(), comps) }()

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not finish")
			}

			require.Error(t, appCtx.Err(), "app context is cancelled by phase 3")

			if comps.JiraWriteback != nil {
				joined := make(chan struct{})

				go func() { comps.JiraWriteback.Wait(); close(joined) }()

				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Fatal("writeback goroutines still running after shutdown")
				}
			}
		})
	}
}
