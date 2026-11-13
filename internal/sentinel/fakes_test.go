package sentinel

import (
	"context"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// fakeStore serves fixed settings and windows and counts settings reads, one
// per pass. It keeps heals with the store's version guard.
type fakeStore struct {
	mu        sync.Mutex
	settings  Settings
	windows   []protocol.SubscriptionUsage
	reads     int
	panicNext bool
	heals     map[int64]Heal
}

func (f *fakeStore) SaveHeal(_ context.Context, h *Heal) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if cur, ok := f.heals[h.ID]; !ok || cur.Version != h.Version {
		return ErrHealChanged
	}

	h.Version++
	f.heals[h.ID] = *h

	return nil
}

func (f *fakeStore) putHeal(h Heal) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.heals == nil {
		f.heals = make(map[int64]Heal)
	}

	f.heals[h.ID] = h
}

func (f *fakeStore) heal(id int64) Heal {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.heals[id]
}

func (f *fakeStore) SentinelSettings(context.Context) (Settings, time.Time, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads++

	if f.panicNext {
		f.panicNext = false

		panic("store failed")
	}

	return f.settings, time.Time{}, "", nil
}

func (f *fakeStore) SubscriptionWindows(context.Context) ([]protocol.SubscriptionUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.windows, nil
}

// Passes never record run-kind statuses; the intake tests use the real store.
func (f *fakeStore) RecordRunKindStatus(context.Context, RunKindReport) (int64, bool, error) {
	return 0, false, nil
}

func (f *fakeStore) AddFindings(context.Context, int64, string, string, []protocol.FrictionFinding, time.Time) error {
	return nil
}

func (f *fakeStore) passes() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads
}

func (f *fakeStore) set(s Settings) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.settings = s
}

// fakeChannel maps a backend kind to its effective mode; other kinds get "".
// It keeps no usage.
type fakeChannel map[llmchannel.Kind]llmchannel.Mode

func (f fakeChannel) Effective(kind llmchannel.Kind) llmchannel.Mode { return f[kind] }

func (f fakeChannel) RecordUsage(context.Context, []protocol.SubscriptionUsage) {}

// fakeBackend answers every run kind with err and keeps what it was sent;
// onSend runs before it answers.
type fakeBackend struct {
	err    error
	onSend func(protocol.RunKindPayload)
	sent   []protocol.RunKindPayload
}

func (f *fakeBackend) RunKind(_ context.Context, p protocol.RunKindPayload) error {
	if f.onSend != nil {
		f.onSend(p)
	}

	f.sent = append(f.sent, p)

	return f.err
}

func (f *fakeBackend) ListContainers(context.Context) ([]backend.ContainerInfo, error) {
	return nil, nil
}

func (f *fakeBackend) Health(context.Context) (protocol.HealthResponse, error) {
	return protocol.HealthResponse{}, nil
}

// fakeCards serves cards and projects from memory; ListCards filters by
// parent and state only.
type fakeCards struct {
	cards    []*board.Card
	projects map[string]*board.ProjectConfig
}

func (f *fakeCards) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	for _, c := range f.cards {
		if c.Project == project && c.ID == id {
			return c, nil
		}
	}

	return nil, storage.ErrCardNotFound
}

func (f *fakeCards) ListCards(_ context.Context, project string, filter storage.CardFilter) ([]*board.Card, error) {
	var out []*board.Card

	for _, c := range f.cards {
		if c.Project == project && (filter.Parent == "" || c.Parent == filter.Parent) &&
			(filter.State == "" || c.State == filter.State) {
			out = append(out, c)
		}
	}

	return out, nil
}

func (f *fakeCards) GetProject(_ context.Context, project string) (*board.ProjectConfig, error) {
	if p, ok := f.projects[project]; ok {
		return p, nil
	}

	return nil, storage.ErrProjectNotFound
}

// fakePlaybooks serves playbook details by ID.
type fakePlaybooks map[string]*service.PlaybookDetail

func (f fakePlaybooks) Get(_ context.Context, id string) (*service.PlaybookDetail, error) {
	if pb, ok := f[id]; ok {
		return pb, nil
	}

	return nil, storage.ErrPlaybookNotFound
}

func enabledSettings() Settings {
	s := DefaultSettings()
	s.Enabled = true

	return s
}
