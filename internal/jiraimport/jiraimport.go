// Package jiraimport turns a Jira Cloud epic into a playbook of cards in
// existing projects: a read-only preview of the epic's tree, then a one-shot
// import. CM is the source of truth afterwards; nothing syncs back in.
package jiraimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
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
	// Bus carries import progress to SSE clients; nil publishes nothing.
	Bus *events.Bus
	// Clock stamps import state and ages finished imports out; nil is the
	// real clock.
	Clock clock.Clock
}

var (
	// ErrNotEpic is returned when the requested issue is not epic-level.
	ErrNotEpic = errors.New("jira issue is not an epic")
	// ErrJiraUnavailable wraps a Jira failure that is not one of the jira
	// package's sentinels (5xx, network, timeout).
	ErrJiraUnavailable = errors.New("jira unavailable")
	// ErrInvalidRequest is returned for an import request that cannot run.
	ErrInvalidRequest = errors.New("invalid import request")
	// ErrPlaybookRunning is returned when the epic's playbook has a running
	// or waiting run: the import would extend a playbook mid-run.
	ErrPlaybookRunning = errors.New("the epic's playbook has an active run")
	// ErrNotAccepting is returned by Begin before Start or after Shutdown.
	ErrNotAccepting = errors.New("jira imports are not being accepted")
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
	clk       clock.Clock
	// timeout bounds one import from when it holds its epic.
	timeout time.Duration

	mu sync.Mutex
	// epics holds one single-slot semaphore per epic key.
	epics map[string]chan struct{}

	// runMu guards base, stop and imports; wg joins the import goroutines.
	runMu   sync.Mutex
	base    context.Context //nolint:containedctx // the imports' parent, set by Start and cancelled by Shutdown
	stop    context.CancelFunc
	imports map[string]*ImportState
	wg      sync.WaitGroup
}

// New builds the import service. Call Start before Begin.
func New(client JiraClient, cards Cards, playbooks Playbooks, cfg Config) *Service {
	clk := cfg.Clock
	if clk == nil {
		clk = clock.Real()
	}

	return &Service{
		jira: client, cards: cards, playbooks: playbooks, cfg: cfg, clk: clk, timeout: importTimeout,
		epics: map[string]chan struct{}{}, imports: map[string]*ImportState{},
	}
}

func (s *Service) browseURL(key string) string {
	return strings.TrimRight(s.cfg.SiteURL, "/") + "/browse/" + key
}

// lockEpic serialises imports of one epic on this instance, so two clicks
// cannot both create the playbook or the same card. The wait ends with ctx.
func (s *Service) lockEpic(ctx context.Context, key string) (func(), error) {
	s.mu.Lock()

	sem, ok := s.epics[key]
	if !ok {
		sem = make(chan struct{}, 1)
		s.epics[key] = sem
	}

	s.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for another import of %s: %w", key, context.Cause(ctx))
	}
}
