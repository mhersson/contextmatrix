package sentinel

import (
	"context"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

// fakeStore serves fixed settings and windows and counts settings reads, one
// per pass.
type fakeStore struct {
	mu        sync.Mutex
	settings  Settings
	windows   []protocol.SubscriptionUsage
	reads     int
	panicNext bool
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
type fakeChannel map[llmchannel.Kind]llmchannel.Mode

func (f fakeChannel) Effective(kind llmchannel.Kind) llmchannel.Mode { return f[kind] }

func enabledSettings() Settings {
	s := DefaultSettings()
	s.Enabled = true

	return s
}
