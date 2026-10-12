package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
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
