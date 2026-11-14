package attention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Blueprints lists chat sessions; architect sessions carry their waits.
type Blueprints interface {
	ListSessions(ctx context.Context, f chat.SessionFilter) ([]chat.Session, error)
}

// Cards reads the boards.
type Cards interface {
	ListProjects(ctx context.Context) ([]board.ProjectConfig, error)
	ListCards(ctx context.Context, project string, f storage.CardFilter) ([]*board.Card, error)
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
}

// Playbooks reads playbooks and their runs.
type Playbooks interface {
	List(ctx context.Context) ([]service.PlaybookSummary, error)
	Get(ctx context.Context, id string) (*service.PlaybookDetail, error)
}

// Holds answers Sentinel's holds on a project's cards.
type Holds interface {
	CardHolds(ctx context.Context, project string) (map[string]sentinel.CardHold, error)
}

// Sources are what the list is collected from. A nil member is skipped;
// Cards is required.
type Sources struct {
	Blueprints Blueprints
	Cards      Cards
	Playbooks  Playbooks
	Holds      Holds
}

// collect reads every source into a Snapshot. ownOnly limits blueprints to
// the viewer's own. A failed source fails the whole collect: a partial list
// would be a wrong count. A project, playbook or card deleted mid-collect is
// skipped: nothing in it waits.
func collect(ctx context.Context, src Sources, viewer string, ownOnly bool) (Snapshot, error) {
	s := Snapshot{Cards: map[CardKey]*board.Card{}, Holds: map[CardKey]sentinel.CardHold{}}

	if src.Blueprints != nil {
		f := chat.SessionFilter{Kind: chat.KindArchitect}
		if ownOnly {
			f.CreatedBy = viewer
		}

		sessions, err := src.Blueprints.ListSessions(ctx, f)
		if err != nil {
			return Snapshot{}, fmt.Errorf("list blueprints: %w", err)
		}

		for _, b := range sessions {
			if b.WaitingKind == "" {
				continue
			}

			w := BlueprintWait{ID: b.ID, Title: b.Title, Owner: b.CreatedBy, Kind: b.WaitingKind}
			if b.WaitingSince != nil {
				w.Since = *b.WaitingSince
			}

			s.Blueprints = append(s.Blueprints, w)
		}
	}

	projects, err := src.Cards.ListProjects(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list projects: %w", err)
	}

	for _, p := range projects {
		parked, err := src.Cards.ListCards(ctx, p.Name, storage.CardFilter{WorkerStatus: "parked"})
		if errors.Is(err, storage.ErrProjectNotFound) {
			continue // deleted since ListProjects: nothing in it waits
		}

		if err != nil {
			return Snapshot{}, fmt.Errorf("list parked cards in %s: %w", p.Name, err)
		}

		for _, c := range parked {
			s.Cards[CardKey{Project: p.Name, ID: c.ID}] = c
		}

		if src.Holds == nil {
			continue
		}

		holds, err := src.Holds.CardHolds(ctx, p.Name)
		if err != nil {
			return Snapshot{}, fmt.Errorf("card holds in %s: %w", p.Name, err)
		}

		for id, h := range holds {
			s.Holds[CardKey{Project: p.Name, ID: id}] = h
		}
	}

	if src.Playbooks != nil {
		summaries, err := src.Playbooks.List(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("list playbooks: %w", err)
		}

		for _, sum := range summaries {
			if sum.RunStatus != board.RunStatusWaiting {
				continue
			}

			pb, err := src.Playbooks.Get(ctx, sum.ID)
			if errors.Is(err, storage.ErrPlaybookNotFound) {
				continue // deleted since List
			}

			if err != nil {
				return Snapshot{}, fmt.Errorf("get playbook %s: %w", sum.ID, err)
			}

			s.Playbooks = append(s.Playbooks, pb)
		}
	}

	need := make([]CardKey, 0, len(s.Holds)+len(s.Playbooks))
	for k := range s.Holds {
		need = append(need, k)
	}

	for _, pb := range s.Playbooks {
		if pb.Run == nil {
			continue
		}

		if k, ok := entryCard(pb); ok {
			need = append(need, k)
		}
	}

	for _, k := range need {
		if _, ok := s.Cards[k]; ok {
			continue
		}

		c, err := src.Cards.GetCard(ctx, k.Project, k.ID)

		switch {
		case errors.Is(err, storage.ErrCardNotFound), errors.Is(err, storage.ErrProjectNotFound):
			// Gone since the hold or run named it: no card item, and a run on it
			// stays its own playbook item with the run's reason.
		case err != nil:
			return Snapshot{}, fmt.Errorf("get card %s/%s: %w", k.Project, k.ID, err)
		default:
			s.Cards[k] = c
		}
	}

	return s, nil
}

// Service serves the waiting list and publishes attention.changed when the
// unscoped list changes.
type Service struct {
	bus   *events.Bus
	nudge chan struct{}
	src   Sources
	last  []byte // the last published list; a dedup memo, touched only by the worker
}

// New returns a Service that publishes on bus once started.
func New(bus *events.Bus) *Service {
	return &Service{bus: bus, nudge: make(chan struct{}, 1)}
}

// Notify asks for a recompute. It never blocks, so it is safe on any
// goroutine and before Start.
func (s *Service) Notify() {
	select {
	case s.nudge <- struct{}{}:
	default:
	}
}

// Start stores the sources and runs the worker until ctx ends. Call it once,
// before serving.
func (s *Service) Start(ctx context.Context, src Sources) {
	s.src = src
	ch, unsubscribe := s.bus.Subscribe()

	// The bus is drained apart from the worker so a slow collect never makes
	// the bus drop a trigger.
	go func() {
		defer unsubscribe()

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}

				if triggers(ev) {
					s.Notify()
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.nudge:
				s.recompute(ctx)
			}
		}
	}()

	s.Notify()
}

// List answers the waiting list viewer sees; ownOnly limits blueprints to
// the viewer's own.
func (s *Service) List(ctx context.Context, viewer string, ownOnly bool) ([]Item, error) {
	snap, err := collect(ctx, s.src, viewer, ownOnly)
	if err != nil {
		return nil, err
	}

	return Merge(snap, viewer), nil
}

// recompute publishes when the unscoped list differs from the last one
// published. The first success publishes too, so a tab that fetched while the
// baseline was pending or failing refetches; a failed collect keeps the key.
func (s *Service) recompute(ctx context.Context) {
	items, err := s.List(ctx, "", false)
	if err != nil {
		if ctx.Err() == nil {
			ctxlog.Logger(ctx).Warn("attention: collect failed", "error", err)
		}

		return
	}

	// The served list is the key, so a card item gaining its playbook ref publishes too.
	key, err := json.Marshal(items)
	if err != nil {
		ctxlog.Logger(ctx).Warn("attention: key the list", "error", err)

		return
	}

	if bytes.Equal(key, s.last) {
		return
	}

	s.last = key
	s.bus.Publish(events.Event{Type: events.AttentionChanged, Timestamp: time.Now().UTC()})
}

// triggers reports whether an event can change the waiting list. Agent log
// and usage traffic cannot; Sentinel's park entry can.
func triggers(ev events.Event) bool {
	switch ev.Type {
	case events.CardCreated, events.CardUpdated, events.CardStateChanged, events.CardDeleted,
		events.SentinelChanged, events.SyncCompleted, events.ProjectDeleted:
		return true
	case events.CardLogAdded:
		return ev.Agent == sentinel.Agent
	}

	t := string(ev.Type)

	return strings.HasPrefix(t, "worker.") || strings.HasPrefix(t, "playbook.")
}
