package modelcatalog

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix-protocol/selection"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// defaultClaudeCodeWindow is the window of a Claude Code ID the served catalog
// does not size.
const defaultClaudeCodeWindow = 1_000_000

// claudeCodeContextWindow is the window of a well-formed Claude Code ID, 0 for
// any other name. Only exact IDs match: a window lookup resolves no names.
func claudeCodeContextWindow(name string) int {
	if id, ok := selection.ClaudeCodeModelID(name); ok && id == name {
		return defaultClaudeCodeWindow
	}

	return 0
}

// claudeCodeIDs is the distinct Claude Code IDs among the AA rows: each row's
// family key, kept when it is itself a well-formed ID.
func claudeCodeIDs(aa []aaModel) []string {
	seen := map[string]bool{}

	var ids []string

	for _, row := range aa {
		key, _, _ := familyKeyParts(row.Slug)

		if id, ok := selection.ClaudeCodeModelID(key); ok && id == key && !seen[key] {
			seen[key] = true

			ids = append(ids, key)
		}
	}

	return ids
}

// servedWindow is the context window the served catalog carries for a Claude
// Code ID, resolved like entryFor (exact, bare name, snapshot key); 0 if none.
func servedWindow(served map[string]orEntry, names, snapshots map[string]string, id string) int {
	if e, ok := served[id]; ok {
		return e.ContextWindow
	}

	if slug, ok := names[id]; ok {
		return served[slug].ContextWindow
	}

	if slug, ok := snapshots[snapshotKey(id)]; ok {
		return served[slug].ContextWindow
	}

	return 0
}

// WithClaudeCode makes the Builder derive the Claude Code catalog from the
// Artificial Analysis fetch it already does. It needs the AA key.
func WithClaudeCode() BuilderOption {
	return func(b *Builder) { b.claudeCode = true }
}

// WithCatalogURLs overrides the Artificial Analysis and OpenRouter catalog
// URLs, for a mirror or a test server. An empty value keeps the default.
func WithCatalogURLs(aaURL, orURL string) BuilderOption {
	return func(b *Builder) {
		if aaURL != "" {
			b.aaEndpoint = aaURL
		}

		if orURL != "" {
			b.orEndpoint = orURL
		}
	}
}

// buildClaudeCode derives the Claude Code catalog from the AA rows and scores
// it. A served entry with a window sizes its ID. It returns the candidates
// (Creator blanked), the priced catalog, each candidate's provenance and how
// many catalog IDs joined a scored AA row.
func buildClaudeCode(aa []aaModel, served map[string]orEntry, floor float64) ([]protocol.CandidateModel, map[string]orEntry, map[string]CandidateProvenance, int) {
	names, snapshots := indexCatalogNames(served)
	ids := claudeCodeIDs(aa)
	static := make(map[string]orEntry, len(ids))

	for _, id := range ids {
		window := servedWindow(served, names, snapshots, id)
		if window <= 0 {
			window = defaultClaudeCodeWindow
		}

		static[id] = orEntry{ContextWindow: window, Tools: true}
	}

	scored, exclusions, listPrices := buildEndpointCandidates(aa, static, nil, floor, nil, "")

	cands := make([]protocol.CandidateModel, 0, len(scored))
	for _, s := range scored {
		c := s.Candidate
		c.Creator = ""
		cands = append(cands, c)
	}

	slices.SortFunc(cands, func(a, c protocol.CandidateModel) int {
		return strings.Compare(a.Slug, c.Slug)
	})

	applyAAListPrices(static, listPrices)

	return cands, static, scoredProvenance(scored), endpointServedScored(scored, exclusions)
}

// ClaudeCodeCandidates returns the Claude Code selection candidates, rated
// and priced from the shared AA fetch. Empty without an AA key or
// WithClaudeCode.
func (b *Builder) ClaudeCodeCandidates(ctx context.Context) []protocol.CandidateModel {
	if b == nil || !b.claudeCode {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return b.claudeCodeCached
}

// ClaudeCodeProvenance is Provenance for the Claude Code candidates, keyed by
// catalog ID. Nil without Claude Code data.
func (b *Builder) ClaudeCodeProvenance(ctx context.Context) map[string]CandidateProvenance {
	if b == nil || !b.claudeCode {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return maps.Clone(b.claudeCodeProvenance)
}

// ClaudeCodeStats is Stats for the Claude Code candidates: the same AA
// snapshot, with ServedScored counting the catalog IDs that joined a scored
// AA row. Zero without Claude Code data.
func (b *Builder) ClaudeCodeStats(ctx context.Context) CatalogStats {
	if b == nil || !b.claudeCode {
		return CatalogStats{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	if b.claudeCodeCached == nil {
		return CatalogStats{}
	}

	stats := b.stats
	stats.ServedScored = b.claudeCodeServedScored

	return stats
}

// ClaudeCodeRate returns the AA list price of a Claude Code catalog ID. ok is
// false for an unknown ID or a builder without Claude Code data.
func (b *Builder) ClaudeCodeRate(ctx context.Context, id string) (ModelPrice, bool) {
	if b == nil || !b.claudeCode {
		return ModelPrice{}, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	e, ok := b.claudeCodeCatalog[id]
	if !ok || (e.PromptPrice == 0 && e.CompletionPrice == 0) {
		return ModelPrice{}, false
	}

	return ModelPrice{
		Prompt:     e.PromptPrice,
		Completion: e.CompletionPrice,
		CacheRead:  e.CacheReadPrice,
		CacheWrite: e.CacheWritePrice,
	}, true
}

// ClaudeCodeKnown reports whether id is a candidate of the built Claude Code
// catalog (above the quality floor). It reads the cached catalog without
// refreshing; built is false on a nil receiver, without WithClaudeCode, and
// before the first successful refresh.
func (b *Builder) ClaudeCodeKnown(id string) (known, built bool) {
	if b == nil || !b.claudeCode {
		return false, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.claudeCodeCatalog == nil {
		return false, false
	}

	// Membership is the candidate set, which the quality floor has pruned;
	// claudeCodeCatalog also holds below-floor IDs for pricing.
	for _, c := range b.claudeCodeCached {
		if c.Slug == id {
			return true, true
		}
	}

	return false, true
}
