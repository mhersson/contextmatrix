// Package jirawriteback moves Jira issues to started and done as their
// imported cards progress. Card events only enqueue into an ops.db outbox; one
// worker drains it, so a CM transition never waits on Jira.
package jirawriteback

import (
	"context"

	"github.com/mhersson/contextmatrix/internal/jira"
)

// Writeback kinds, one outbox row each per card.
const (
	KindStarted = "started"
	KindDone    = "done"
)

// JiraAPI is the slice of the Jira client the writeback uses.
type JiraAPI interface {
	Myself(ctx context.Context) (jira.Account, error)
	Issue(ctx context.Context, key string) (jira.Issue, error)
	Transitions(ctx context.Context, key string) ([]jira.Transition, error)
	Transition(ctx context.Context, key, transitionID string, fields map[string]any) error
}
