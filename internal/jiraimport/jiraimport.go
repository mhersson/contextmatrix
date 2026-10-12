// Package jiraimport turns a Jira Cloud epic into a playbook of cards in
// existing projects: a read-only preview of the epic's tree, then a one-shot
// import. CM is the source of truth afterwards; nothing syncs back in.
package jiraimport

import (
	"context"
	"errors"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// JiraClient is the slice of *jira.Client the importer uses.
type JiraClient interface {
	Issue(ctx context.Context, key string) (jira.Issue, error)
	Search(ctx context.Context, jql string) ([]jira.Issue, error)
}

// Cards is the slice of the card service the importer uses.
type Cards interface {
	ListProjects(ctx context.Context) ([]board.ProjectConfig, error)
	GetProject(ctx context.Context, name string) (*board.ProjectConfig, error)
	ListCards(ctx context.Context, project string, filter storage.CardFilter) ([]*board.Card, error)
	CreateCard(ctx context.Context, project string, input service.CreateCardInput) (*board.Card, error)
}

// Playbooks is the slice of the playbook service the importer uses.
type Playbooks interface {
	FindBySource(ctx context.Context, system, externalID string) (*service.PlaybookDetail, error)
	Create(ctx context.Context, input service.CreatePlaybookInput) (*service.PlaybookDetail, error)
	AddEntry(ctx context.Context, id string, in service.PlaybookEntryInput, agentID string) (*service.PlaybookDetail, error)
	UpdateEntry(ctx context.Context, id, entryID string, in service.UpdateEntryInput, agentID string) (*service.PlaybookDetail, error)
}

// Config is the instance configuration the importer reads.
type Config struct {
	// SiteURL builds browse links and source URLs.
	SiteURL string
	// ProjectMap gives wizard defaults per Jira project key.
	ProjectMap map[string]config.JiraProjectMapping
}

var (
	// ErrNotEpic is returned when the requested issue is not epic-level.
	ErrNotEpic = errors.New("jira issue is not an epic")
	// ErrJiraUnavailable wraps a Jira failure that is not one of the jira
	// package's sentinels (5xx, network, timeout).
	ErrJiraUnavailable = errors.New("jira unavailable")
	// ErrInvalidRequest is returned for an import request that cannot run.
	ErrInvalidRequest = errors.New("invalid import request")
)

// Per-selection result statuses.
const (
	StatusCreated = "created"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// Service previews and imports epics.
type Service struct {
	jira      JiraClient
	cards     Cards
	playbooks Playbooks
	cfg       Config
}

// New builds the import service.
func New(client JiraClient, cards Cards, playbooks Playbooks, cfg Config) *Service {
	return &Service{jira: client, cards: cards, playbooks: playbooks, cfg: cfg}
}

func (s *Service) browseURL(key string) string {
	return strings.TrimRight(s.cfg.SiteURL, "/") + "/browse/" + key
}
