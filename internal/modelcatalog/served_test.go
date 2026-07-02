package modelcatalog

import (
	"context"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
)

// seedServed primes a Builder with a raw catalog and a fresh cache stamp so
// Served() does not attempt a live refresh.
func seedServed(t *testing.T, b *Builder, catalog map[string]orEntry) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastCatalog = catalog
	b.cached = []protocol.CandidateModel{}
	b.cachedAt = time.Now()
}

func TestServedVendorScreening(t *testing.T) {
	catalog := map[string]orEntry{
		"anthropic/claude-sonnet-4.5": {ContextWindow: 200000},
		"qwen/qwen3-coder":            {ContextWindow: 131072}, // alibaba → qwen mapping
		"unlisted-vendor/model-x":     {ContextWindow: 8192},   // screened out
		"unlisted-vendor/favorite-y":  {ContextWindow: 8192},   // kept via favorites
		"openrouter/auto":             {ContextWindow: 2000000},
	}

	b := NewBuilder("", 0.65, nil, 0, WithFavorites([]string{
		"unlisted-vendor/favorite-y",
		"vendor-typo/not-in-catalog", // favorite absent from catalog: excluded
	}))
	seedServed(t, b, catalog)

	got := b.Served(context.Background())

	slugs := make([]string, len(got))
	for i, m := range got {
		slugs[i] = m.Slug
	}
	// Sorted, screened, auto + catalog-listed favorite included.
	assert.Equal(t, []string{
		"anthropic/claude-sonnet-4.5",
		"openrouter/auto",
		"qwen/qwen3-coder",
		"unlisted-vendor/favorite-y",
	}, slugs)
}

func TestServedAllowlistOverride(t *testing.T) {
	catalog := map[string]orEntry{
		"anthropic/claude-sonnet-4.5": {ContextWindow: 200000},
		"deepseek/deepseek-v4":        {ContextWindow: 131072},
	}
	// Config override: only anthropic is trusted.
	b := NewBuilder("", 0.65, []string{"anthropic"}, 0)
	seedServed(t, b, catalog)

	got := b.Served(context.Background())
	assert.Len(t, got, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4.5", got[0].Slug)
	assert.Equal(t, 200000, got[0].ContextWindow)
}

func TestServedEndpointLegUnfiltered(t *testing.T) {
	catalog := map[string]orEntry{
		"model-a": {ContextWindow: 100000},
		"model-b": {ContextWindow: 32000},
	}
	b := NewBuilder("", 0.65, nil, 0, WithEndpoint("http://endpoint.invalid", "", nil, nil))
	seedServed(t, b, catalog)

	got := b.Served(context.Background())
	assert.Len(t, got, 2) // operator-curated: no vendor screen
}

func TestServedEmptyAndNil(t *testing.T) {
	var nilB *Builder
	assert.Nil(t, nilB.Served(context.Background()))

	b := NewBuilder("", 0.65, nil, 0)
	seedServed(t, b, map[string]orEntry{})
	assert.Nil(t, b.Served(context.Background()))
}
