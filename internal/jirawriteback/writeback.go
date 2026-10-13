// Package jirawriteback moves Jira issues to started and done as their
// imported cards progress. Card events only enqueue into an ops.db outbox; one
// worker drains it, so a CM transition never waits on Jira.
package jirawriteback

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/jira"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
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

// Cards is the slice of the card service the writeback uses.
type Cards interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
	ListProjects(ctx context.Context) ([]board.ProjectConfig, error)
	ListCards(ctx context.Context, project string, filter storage.CardFilter) ([]*board.Card, error)
	AddSystemLogEntry(ctx context.Context, project, id string, entry board.ActivityEntry) (*board.Card, error)
}

// Outbox is the slice of the op store that holds owed writes.
type Outbox interface {
	EnqueueJiraWriteback(ctx context.Context, project, cardID, kind, issueKey string, now int64) (bool, error)
	DueJiraWritebacks(ctx context.Context, now int64, limit int) ([]opsqlite.JiraWriteback, error)
	HasPendingJiraWriteback(ctx context.Context, project, cardID, kind string) (bool, error)
	FinishJiraWriteback(ctx context.Context, project, cardID, kind, state, lastErr string, now int64) error
	RetryJiraWriteback(ctx context.Context, project, cardID, kind string, attempts int, nextAttemptAt int64, lastErr string, now int64) error
}

// Options configures a Writeback.
type Options struct {
	SiteURL       string
	StatusTargets map[string]config.JiraStatusTarget
	Clock         clock.Clock
	Logger        *slog.Logger
}

// Writeback runs the event subscriber, the outbox worker and the sweep.
type Writeback struct {
	bus      *events.Bus
	cards    Cards
	api      JiraAPI
	outbox   Outbox
	resolver *resolver
	siteHost string
	// sitePath is the site URL's path without a trailing slash, usually "".
	sitePath string
	clk      clock.Clock
	logger   *slog.Logger
	wake     chan struct{}
	paused   atomic.Bool
	wg       sync.WaitGroup

	// Owned by the worker goroutine.
	probeAt    time.Time
	probeCount int
	// pauseWarned holds until a write succeeds, so a pause right after a
	// resume keeps backing off and stays quiet.
	pauseWarned bool
}

// New builds a Writeback; nothing runs until Start.
func New(bus *events.Bus, cards Cards, outbox Outbox, api JiraAPI, opts Options) (*Writeback, error) {
	u, err := url.Parse(opts.SiteURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("jira writeback: invalid site URL %q", opts.SiteURL)
	}

	clk := opts.Clock
	if clk == nil {
		clk = clock.Real()
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Writeback{
		bus:      bus,
		cards:    cards,
		api:      api,
		outbox:   outbox,
		resolver: newResolver(api, opts.StatusTargets),
		siteHost: u.Hostname(),
		sitePath: strings.TrimRight(u.Path, "/"),
		clk:      clk,
		logger:   logger,
		wake:     make(chan struct{}, 1),
	}, nil
}

// Start subscribes before returning, so no event published after Start is
// missed, then runs the subscriber, worker and sweep until ctx ends.
func (w *Writeback) Start(ctx context.Context) {
	ch, unsubscribe := w.bus.Subscribe()

	w.wg.Go(func() {
		defer unsubscribe()

		w.runSubscriber(ctx, ch)
	})
	w.wg.Go(func() { w.runWorker(ctx) })
	w.wg.Go(func() { w.runSweep(ctx) })
}

// Wait blocks until every goroutine Start launched has returned.
func (w *Writeback) Wait() { w.wg.Wait() }

// Paused reports whether draining is paused because Jira rejected the
// credentials.
func (w *Writeback) Paused() bool { return w.paused.Load() }

func (w *Writeback) nudge() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// jiraKey returns the issue key of a card imported from this instance's
// Jira site, or "". The host check stops a card sourced from another Jira
// site from moving the same issue key on this one; the path check stops a
// card whose link shows one issue from moving another.
func (w *Writeback) jiraKey(c *board.Card) string {
	if c == nil || c.Source == nil || c.Source.System != jira.SourceSystem || !jira.IsIssueKey(c.Source.ExternalID) {
		return ""
	}

	u, err := url.Parse(c.Source.ExternalURL)
	if err != nil || !strings.EqualFold(u.Hostname(), w.siteHost) || u.Path != w.sitePath+"/browse/"+c.Source.ExternalID {
		return ""
	}

	return c.Source.ExternalID
}

func (w *Writeback) enqueue(ctx context.Context, project, cardID, kind, key string) {
	inserted, err := w.outbox.EnqueueJiraWriteback(ctx, project, cardID, kind, key, w.clk.Now().Unix())
	if err != nil {
		w.logger.Warn("jira writeback: enqueue failed", "project", project, "card_id", cardID, "kind", kind, "error", err)

		return
	}

	if inserted {
		w.nudge()
	}
}
