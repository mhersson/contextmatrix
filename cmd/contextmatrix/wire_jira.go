package main

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/jira"
)

// wireJira builds the Jira client when jira.site_url is set. This is the only
// place the Jira token is read.
func wireJira(cfg config.JiraConfig) (*jira.Client, error) {
	if !cfg.Configured() {
		return nil, nil
	}

	auth, err := jira.NewAuthorizer(cfg.AuthMode, cfg.Email, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("jira authorizer: %w", err)
	}

	return jira.NewClient(cfg.ResolvedAPIBaseURL(), auth, nil), nil
}

// unknownJiraTargets lists "JIRAKEY -> project" pairs in jira.project_map
// that name no CM project. They are wizard defaults only, so main warns and
// carries on.
func unknownJiraTargets(mapping map[string]config.JiraProjectMapping, projects []board.ProjectConfig) []string {
	known := make(map[string]bool, len(projects))
	for _, p := range projects {
		known[p.Name] = true
	}

	var unknown []string

	for jiraKey, m := range mapping {
		targets := []string{m.Default}
		for _, t := range m.Components {
			targets = append(targets, t)
		}

		for _, t := range targets {
			if t != "" && !known[t] {
				unknown = append(unknown, jiraKey+" -> "+t)
			}
		}
	}

	slices.Sort(unknown)

	return slices.Compact(unknown)
}

func warnUnknownJiraTargets(mapping map[string]config.JiraProjectMapping, projects []board.ProjectConfig) {
	for _, u := range unknownJiraTargets(mapping, projects) {
		slog.Warn("jira.project_map target is not a CM project", "mapping", u)
	}
}
