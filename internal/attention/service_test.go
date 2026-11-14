package attention

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

var errSource = errors.New("source down")

type fakeBlueprints struct {
	sessions []chat.Session
	err      error
	filters  []chat.SessionFilter
}

func (f *fakeBlueprints) ListSessions(_ context.Context, sf chat.SessionFilter) ([]chat.Session, error) {
	f.filters = append(f.filters, sf)

	if f.err != nil {
		return nil, f.err
	}

	var out []chat.Session

	for _, s := range f.sessions {
		if (sf.Kind == "" || s.Kind == sf.Kind) && (sf.CreatedBy == "" || s.CreatedBy == sf.CreatedBy) {
			out = append(out, s)
		}
	}

	return out, nil
}

// fakeCards serves cards of project "p" and counts ListProjects calls, one per collect.
type fakeCards struct {
	mu                           sync.Mutex
	cards                        []*board.Card
	projectsErr, listErr, getErr error
	missingErrs                  map[string]error // per card id, ahead of getErr
	collects                     int
	gotCards                     []string
}

func newFakeCards(cards ...*board.Card) *fakeCards { return &fakeCards{cards: cards} }

func (f *fakeCards) add(c *board.Card) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cards = append(f.cards, c)
}

func (f *fakeCards) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.projectsErr = err
}

func (f *fakeCards) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.collects
}

func (f *fakeCards) ListProjects(context.Context) ([]board.ProjectConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.collects++

	if f.projectsErr != nil {
		return nil, f.projectsErr
	}

	return []board.ProjectConfig{{Name: "p"}}, nil
}

func (f *fakeCards) ListCards(_ context.Context, project string, cf storage.CardFilter) ([]*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}

	var out []*board.Card

	for _, c := range f.cards {
		if c.Project == project && (cf.WorkerStatus == "" || c.WorkerStatus == cf.WorkerStatus) {
			out = append(out, c)
		}
	}

	return out, nil
}

func (f *fakeCards) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.gotCards = append(f.gotCards, id)

	if err := f.missingErrs[id]; err != nil {
		return nil, err
	}

	if f.getErr != nil {
		return nil, f.getErr
	}

	for _, c := range f.cards {
		if c.Project == project && c.ID == id {
			return c, nil
		}
	}

	return nil, storage.ErrCardNotFound
}

type fakePlaybooks struct {
	details      []*service.PlaybookDetail
	vanished     []string // listed as waiting, gone by the time Get runs
	listErr, err error
}

func (f *fakePlaybooks) List(context.Context) ([]service.PlaybookSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	out := make([]service.PlaybookSummary, len(f.details))

	for i, d := range f.details {
		out[i] = service.PlaybookSummary{ID: d.ID}
		if d.Run != nil {
			out[i].RunStatus = d.Run.Status
		}
	}

	for _, id := range f.vanished {
		out = append(out, service.PlaybookSummary{ID: id, RunStatus: board.RunStatusWaiting})
	}

	return out, nil
}

func (f *fakePlaybooks) Get(_ context.Context, id string) (*service.PlaybookDetail, error) {
	if f.err != nil {
		return nil, f.err
	}

	for _, d := range f.details {
		if d.ID == id {
			return d, nil
		}
	}

	return nil, storage.ErrPlaybookNotFound
}

type fakeHolds struct {
	holds map[string]sentinel.CardHold
	err   error
}

func (f *fakeHolds) CardHolds(_ context.Context, project string) (map[string]sentinel.CardHold, error) {
	if f.err != nil {
		return nil, f.err
	}

	if project != "p" {
		return nil, nil
	}

	return f.holds, nil
}

func blueprint(id, owner, waiting string) chat.Session {
	since := t0

	return chat.Session{
		ID: id, Title: "bp " + id, Kind: chat.KindArchitect, CreatedBy: owner,
		WaitingKind: waiting, WaitingSince: &since,
	}
}

func TestCollect_ScopesBlueprintsWhenOwnOnly(t *testing.T) {
	bps := &fakeBlueprints{sessions: []chat.Session{
		blueprint("s1", "human:alice", "gate"),
		blueprint("s2", "human:bob", "question"),
		blueprint("s3", "human:alice", ""),
		{ID: "c1", Kind: chat.KindChat, CreatedBy: "human:alice"},
	}}
	src := Sources{Blueprints: bps, Cards: newFakeCards()}

	snap, err := collect(t.Context(), src, "human:alice", true)
	require.NoError(t, err)
	assert.Equal(t, []BlueprintWait{{ID: "s1", Title: "bp s1", Owner: "human:alice", Kind: "gate", Since: t0}}, snap.Blueprints)
	assert.Equal(t, chat.SessionFilter{Kind: chat.KindArchitect, CreatedBy: "human:alice"}, bps.filters[0])

	snap, err = collect(t.Context(), src, "human:alice", false)
	require.NoError(t, err)
	require.Len(t, snap.Blueprints, 2)
	assert.Equal(t, chat.SessionFilter{Kind: chat.KindArchitect}, bps.filters[1])
}

func TestCollect_LoadsHeldAndEntryCards(t *testing.T) {
	cards := newFakeCards(
		parkedCard("A-1"),
		&board.Card{ID: "A-2", Project: "p", State: "in_progress"},
		&board.Card{ID: "A-3", Project: "p", State: "in_progress"},
		&board.Card{ID: "A-4", Project: "p", State: "todo"},
	)
	cards.missingErrs = map[string]error{"GONE-1": storage.ErrCardNotFound, "GONE-2": storage.ErrProjectNotFound}
	holds := &fakeHolds{holds: map[string]sentinel.CardHold{
		"A-1": {Reason: "parked", Since: t0}, "A-2": {Healing: true}, "GONE-1": {Reason: "r", Since: t0},
	}}
	pbs := &fakePlaybooks{details: []*service.PlaybookDetail{
		waitingRun("pb-1", "One", "e1", "u", t0, cardEntry("e1", "A-3")),
		waitingRun("pb-2", "Two", "e1", "u", t0, cardEntry("e1", "GONE-2")),
		{ID: "pb-3", Run: &board.PlaybookRun{Status: board.RunStatusRunning, Entry: "e1"}, Entries: []service.PlaybookEntryDetail{cardEntry("e1", "A-4")}},
		{ID: "pb-4"},
	}}

	snap, err := collect(t.Context(), Sources{Cards: cards, Holds: holds, Playbooks: pbs}, "u", false)
	require.NoError(t, err)

	ids := make([]string, 0, len(snap.Cards))
	for k := range snap.Cards {
		ids = append(ids, k.ID)
	}

	assert.ElementsMatch(t, []string{"A-1", "A-2", "A-3"}, ids)
	assert.NotContains(t, cards.gotCards, "A-1", "a parked card is already loaded")
	assert.Len(t, snap.Holds, 3)

	pbIDs := make([]string, 0, len(snap.Playbooks))
	for _, pb := range snap.Playbooks {
		pbIDs = append(pbIDs, pb.ID)
	}

	assert.ElementsMatch(t, []string{"pb-1", "pb-2"}, pbIDs)
}

func TestCollect_SourceErrorFailsTheList(t *testing.T) {
	cases := map[string]func(*Sources){
		"blueprints":     func(s *Sources) { s.Blueprints.(*fakeBlueprints).err = errSource },
		"projects":       func(s *Sources) { s.Cards.(*fakeCards).projectsErr = errSource },
		"parked cards":   func(s *Sources) { s.Cards.(*fakeCards).listErr = errSource },
		"held card":      func(s *Sources) { s.Cards.(*fakeCards).getErr = errSource },
		"holds":          func(s *Sources) { s.Holds.(*fakeHolds).err = errSource },
		"playbook list":  func(s *Sources) { s.Playbooks.(*fakePlaybooks).listErr = errSource },
		"playbook entry": func(s *Sources) { s.Playbooks.(*fakePlaybooks).err = errSource },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			src := Sources{
				Blueprints: &fakeBlueprints{sessions: []chat.Session{blueprint("s1", "u", "gate")}},
				Cards:      newFakeCards(&board.Card{ID: "A-2", Project: "p", State: "todo"}),
				Holds:      &fakeHolds{holds: map[string]sentinel.CardHold{"A-2": {Reason: "r", Since: t0}}},
				Playbooks:  &fakePlaybooks{details: []*service.PlaybookDetail{waitingRun("pb", "P", "", "u", t0)}},
			}

			_, err := collect(t.Context(), src, "u", false)
			require.NoError(t, err)

			breakIt(&src)

			_, err = collect(t.Context(), src, "u", false)
			assert.ErrorIs(t, err, errSource)
		})
	}
}

func TestCollect_SkipsVanishedSources(t *testing.T) {
	pbs := &fakePlaybooks{
		details:  []*service.PlaybookDetail{waitingRun("pb-1", "One", "", "u", t0)},
		vanished: []string{"pb-gone"},
	}

	snap, err := collect(t.Context(), Sources{Cards: newFakeCards(), Playbooks: pbs}, "u", false)
	require.NoError(t, err)
	require.Len(t, snap.Playbooks, 1)
	assert.Equal(t, "pb-1", snap.Playbooks[0].ID)

	cards := newFakeCards(parkedCard("A-1"))
	cards.listErr = fmt.Errorf("list cards: %w", storage.ErrProjectNotFound)
	holds := &fakeHolds{holds: map[string]sentinel.CardHold{"A-1": {Reason: "r", Since: t0}}}

	snap, err = collect(t.Context(), Sources{Cards: cards, Holds: holds}, "u", false)
	require.NoError(t, err)
	assert.Empty(t, snap.Cards, "a vanished project holds nothing")
	assert.Empty(t, snap.Holds)
}

func TestCollect_SkipsNilSources(t *testing.T) {
	snap, err := collect(t.Context(), Sources{Cards: newFakeCards(parkedCard("A-1"))}, "u", true)
	require.NoError(t, err)
	assert.Len(t, snap.Cards, 1)
	assert.Empty(t, snap.Blueprints)
	assert.Empty(t, snap.Playbooks)
	assert.Empty(t, snap.Holds)
}

// attentionEvents drains what the bus delivered so far and keeps the attention ones.
func attentionEvents(ch <-chan events.Event) []events.Event {
	var out []events.Event

	for {
		select {
		case ev := <-ch:
			if ev.Type == events.AttentionChanged {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

// started runs a Service on src inside the bubble and returns the bus, a
// subscription to it, and the service, after the baseline computation.
func started(t *testing.T, src Sources) (*events.Bus, <-chan events.Event, *Service) {
	t.Helper()

	bus := events.NewBus()
	ch, unsubscribe := bus.Subscribe()
	t.Cleanup(unsubscribe)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	svc := New(bus)
	svc.Start(ctx, src)
	synctest.Wait()

	return bus, ch, svc
}

func TestService_PublishesOnlyWhenTheListChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cards := newFakeCards(parkedCard("A-1"))
		_, ch, svc := started(t, Sources{Cards: cards})
		require.Equal(t, 1, cards.calls(), "Start computes the baseline")
		assert.Len(t, attentionEvents(ch), 1, "the first computation publishes")

		svc.Notify()
		synctest.Wait()
		require.Equal(t, 2, cards.calls())
		assert.Empty(t, attentionEvents(ch), "the same list publishes nothing")

		cards.add(parkedCard("A-2"))
		svc.Notify()
		synctest.Wait()
		assert.Len(t, attentionEvents(ch), 1)
	})
}

func TestService_FailedCollectKeepsTheKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cards := newFakeCards(parkedCard("A-1"))
		_, ch, svc := started(t, Sources{Cards: cards})
		attentionEvents(ch)

		cards.setErr(errSource)
		svc.Notify()
		synctest.Wait()
		assert.Empty(t, attentionEvents(ch), "a failed collect publishes nothing")

		cards.setErr(nil)
		cards.add(parkedCard("A-2"))
		svc.Notify()
		synctest.Wait()
		assert.Len(t, attentionEvents(ch), 1, "the change is judged against the list before the failure")
	})
}

func TestService_FailedBaselinePublishesFirstSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cards := newFakeCards(parkedCard("A-1"))
		cards.setErr(errSource)

		_, ch, svc := started(t, Sources{Cards: cards})
		require.Equal(t, 1, cards.calls())
		assert.Empty(t, attentionEvents(ch))

		cards.setErr(nil)
		svc.Notify()
		synctest.Wait()
		assert.Len(t, attentionEvents(ch), 1, "a tab that fetched during the failure must refetch")
	})
}

func TestService_IgnoresAgentLogTraffic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cards := newFakeCards(parkedCard("A-1"))
		bus, _, _ := started(t, Sources{Cards: cards})

		bus.Publish(events.Event{Type: events.CardLogAdded, Project: "p", CardID: "A-1", Agent: "agent-1"})
		bus.Publish(events.Event{Type: events.CardUsageReported, Project: "p", CardID: "A-1", Agent: "agent-1"})
		synctest.Wait()
		assert.Equal(t, 1, cards.calls(), "agent traffic recomputes nothing")

		bus.Publish(events.Event{Type: events.CardLogAdded, Project: "p", CardID: "A-1", Agent: sentinel.Agent})
		synctest.Wait()
		assert.Equal(t, 2, cards.calls(), "Sentinel's park entry recomputes")
	})
}

func TestService_RecomputeTriggers(t *testing.T) {
	cases := map[events.EventType]bool{
		events.WorkerParked: true, events.WorkerStarted: true, events.CardCreated: true,
		events.CardUpdated: true, events.CardStateChanged: true, events.CardDeleted: true,
		events.PlaybookUpdated: true, events.PlaybookDeleted: true, events.SentinelChanged: true,
		events.SyncCompleted: true, events.ProjectDeleted: true,
		events.AttentionChanged: false, events.CardClaimed: false, events.SyncStarted: false,
		events.LLMChannelChanged: false, events.ProjectUpdated: false,
	}

	for typ, recomputes := range cases {
		t.Run(string(typ), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cards := newFakeCards()
				bus, _, _ := started(t, Sources{Cards: cards})

				bus.Publish(events.Event{Type: typ})
				synctest.Wait()

				want := 1
				if recomputes {
					want = 2
				}

				assert.Equal(t, want, cards.calls())
			})
		})
	}
}

func TestService_EventCarriesNoContent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bps := &fakeBlueprints{}
		_, ch, svc := started(t, Sources{Blueprints: bps, Cards: newFakeCards(parkedCard("A-1"))})
		attentionEvents(ch)

		bps.sessions = []chat.Session{blueprint("s1", "human:bob", "gate")}

		svc.Notify()
		synctest.Wait()

		got := attentionEvents(ch)
		require.Len(t, got, 1)
		assert.Empty(t, got[0].Project)
		assert.Empty(t, got[0].CardID)
		assert.Empty(t, got[0].Agent)
		assert.Nil(t, got[0].Data)
	})
}
